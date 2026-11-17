// Telegram Presentation Bot — dasturning kirish nuqtasi.
//
// Bu yerda barcha bog'liqliklar (konfiguratsiya, logger, baza, AI klient,
// PPTX va DOCX generatorlari, Telegram bot) yaratiladi va bir-biriga ulanadi
// (Dependency Injection), so'ng bot ishga tushiriladi va SIGINT/SIGTERM
// signallarida xavfsiz to'xtatiladi.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/bot"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/payment"
	"telegram-presentation-bot/internal/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. Konfiguratsiya.
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// 2. Logger.
	log, err := newLogger(cfg.Env, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer func() { _ = log.Sync() }()

	// SIGINT (Ctrl+C) yoki SIGTERM (docker stop) kelganda ctx bekor qilinadi.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 3. Ma'lumotlar bazasi.
	db, err := storage.Connect(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := storage.Close(db); err != nil {
			log.Warn("failed to close database", zap.Error(err))
		}
	}()
	if err := storage.Migrate(db); err != nil {
		return err
	}
	sessions := storage.NewSessionRepository(db)
	billing := storage.NewBillingRepository(db)

	// 4. AI klient va fayl generatorlari: taqdimot — .pptx (shablon asosida),
	// mustaqil ish va amaliy topshiriq — .docx.
	aiClient, err := ai.New(ctx, cfg.AI, log)
	if err != nil {
		return err
	}
	pptx, err := generator.NewPPTX(cfg.Generator.TemplatePath, cfg.Generator.OutputDir, log)
	if err != nil {
		return err
	}
	docx, err := generator.NewDOCX(cfg.Generator.OutputDir, cfg.Generator.DocumentHeader, log)
	if err != nil {
		return err
	}

	// Rasmli dizaynlar uchun AI rasm generatsiyasi: arzon tarif (standard) va
	// sifatli tarif (premium) provayderlari. Kalit bo'lmasa — rasmlarsiz.
	imageSvc, err := images.New(ctx, cfg.AI, cfg.Images, log)
	if err != nil {
		return err
	}
	var imageService bot.ImageService
	if imageSvc.Available() {
		imageService = imageSvc
	} else {
		log.Warn("rasm provayderlari ulanmagan (OPENAI_API_KEY / GEMINI_API_KEY) — rasmli dizaynlar rasmsiz chiqadi")
	}

	// To'lov cheklarini OpenAI vision modeli o'qiydi. Kalit bo'lmasa — barcha
	// cheklar admin tekshiruviga yuboriladi.
	var verifier bot.ReceiptVerifier
	if cfg.Payment.Enabled {
		if cfg.AI.OpenAIAPIKey != "" {
			verifier = payment.NewVerifier(cfg.AI.OpenAIBaseURL, cfg.AI.OpenAIAPIKey, cfg.Payment.VisionModel, cfg.Payment.CheckTimeout)
			log.Info("payments enabled",
				zap.String("vision_model", cfg.Payment.VisionModel),
				zap.Int("price_photo", cfg.Payment.PricePhoto),
				zap.Int("price_plain", cfg.Payment.PricePlain))
		} else {
			log.Warn("OPENAI_API_KEY yo'q — to'lov cheklari AI siz, faqat admin tomonidan tekshiriladi")
		}
	} else {
		log.Warn("to'lov o'chirilgan (PAYMENT_ENABLED=false) — barcha ishlar bepul")
	}

	// 5. Handler. Generatsiyalar signal kontekstidan alohida kontekstda ishlaydi:
	// to'xtash signali kelganda ular darhol uzilmaydi, balki ShutdownTimeout
	// davomida yakunlanishiga imkon beriladi.
	jobsCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()

	handler := bot.NewHandler(jobsCtx, sessions, aiClient, bot.Builders{Slides: pptx, Papers: docx}, log, bot.Options{
		Limits: bot.Limits{
			MinSlides: cfg.Limits.MinSlides,
			MaxSlides: cfg.Limits.MaxSlides,
			MinPages:  cfg.Limits.MinPages,
			MaxPages:  cfg.Limits.MaxPages,
		},
		MaxConcurrentJobs: cfg.Generator.MaxConcurrentJobs,
		JobTimeout:        cfg.Generator.JobTimeout,
		// Dizaynlar ko'rinishi rasmi shablon yonida turadi (go run ./cmd/preview bilan yangilanadi).
		DesignPreview:    filepath.Join(filepath.Dir(cfg.Generator.TemplatePath), "designs.png"),
		Images:           imageService,
		DefaultImageTier: images.Tier(cfg.Images.DefaultTier),
		Billing:          billing,
		Verifier:         verifier,
		Payment: bot.PaymentOptions{
			Enabled:       cfg.Payment.Enabled,
			CardNumber:    cfg.Payment.CardNumber,
			CardHolder:    cfg.Payment.CardHolder,
			PricePhoto:    cfg.Payment.PricePhoto,
			PricePlain:    cfg.Payment.PricePlain,
			FreePhoto:     cfg.Payment.FreePhoto,
			FreePlain:     cfg.Payment.FreePlain,
			AdminPassword: cfg.Payment.AdminPassword,
		},
	})

	// 6. Telegram bot.
	b, err := tele.NewBot(tele.Settings{
		Token:     cfg.Telegram.Token,
		Poller:    &tele.LongPoller{Timeout: cfg.Telegram.PollTimeout},
		ParseMode: tele.ModeHTML,
		OnError:   handler.OnError,
		// Handlerlar parallel ishlaydi; bir foydalanuvchi ichidagi tartibni
		// Handler o'zining qulflari bilan ta'minlaydi.
		Synchronous: false,
	})
	if err != nil {
		return fmt.Errorf("telegram: botni yaratib bo'lmadi: %w", err)
	}
	handler.Register(b)

	// Oldingi ishga tushirishda yakunlanmay qolgan generatsiyalarni tiklash.
	handler.RecoverInterrupted(ctx)
	// Adminlar menyusidagi buyruqlarni joriy ro'yxat bilan yangilash.
	handler.RefreshAdminCommands(ctx)

	go b.Start()
	log.Info("bot started",
		zap.String("username", b.Me.Username),
		zap.String("ai_provider", cfg.AI.Provider),
		zap.Int("max_concurrent_jobs", cfg.Generator.MaxConcurrentJobs))

	// 7. To'xtash signalini kutish.
	<-ctx.Done()
	log.Info("shutdown signal received, stopping bot")

	b.Stop() // yangi update'lar qabul qilinmaydi

	if !handler.Wait(cfg.Telegram.ShutdownTimeout) {
		log.Warn("shutdown timeout reached, cancelling running jobs")
		cancelJobs()
		if !handler.Wait(15 * time.Second) {
			log.Error("some jobs did not finish in time")
		}
	}

	log.Info("bot stopped gracefully")
	return nil
}

// newLogger muhitga mos zap logger yaratadi: production — JSON,
// development — rangli, o'qish uchun qulay format.
func newLogger(env, level string) (*zap.Logger, error) {
	lvl, err := zapcore.ParseLevel(level)
	if err != nil {
		return nil, fmt.Errorf("LOG_LEVEL noto'g'ri: %q", level)
	}

	var zcfg zap.Config
	if env == "development" {
		zcfg = zap.NewDevelopmentConfig()
		zcfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		zcfg = zap.NewProductionConfig()
		zcfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}
	zcfg.Level = zap.NewAtomicLevelAt(lvl)
	return zcfg.Build()
}
