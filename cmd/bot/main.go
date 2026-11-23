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
	"telegram-presentation-bot/internal/backup"
	"telegram-presentation-bot/internal/bot"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generation"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/health"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/payment"
	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/tz"
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
	// Versiyali migratsiyalar (goose); SIGTERM kelsa qulfni kutish to'xtatiladi.
	if err := storage.Migrate(db.WithContext(ctx)); err != nil {
		return err
	}
	sessions := storage.NewSessionRepository(db)
	billing := storage.NewBillingRepository(db)
	stats := storage.NewStatsRepository(db, tz.Tashkent)
	broadcasts := storage.NewBroadcastRepository(db)

	// 4. AI klient va fayl generatorlari: taqdimot — .pptx (shablon asosida),
	// mustaqil ish, referat va amaliy topshiriq — .docx.
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

	// Rasmli dizaynlar uchun AI rasm generatsiyasi (bitta provayder — OpenAI).
	// Kalit bo'lmasa — rasmlarsiz.
	imageSvc, err := images.New(cfg.AI, cfg.Images, log)
	if err != nil {
		return err
	}
	var imageService generation.ImageService
	if imageSvc.Available() {
		imageService = imageSvc
	} else {
		log.Warn("rasm provayderi ulanmagan (OPENAI_API_KEY) — rasmli dizaynlar rasmsiz chiqadi")
	}

	// AI maxsus dizayn: har bir tarif — o'z OpenAI matn modeli va rasm sifati.
	customTariffs := newCustomTariffs(cfg, log)

	// To'lov cheklarini OpenAI vision modeli o'qiydi. Kalit bo'lmasa — barcha
	// cheklar admin tekshiruviga yuboriladi.
	var verifier bot.ReceiptVerifier
	if cfg.Payment.Enabled {
		if cfg.AI.OpenAIAPIKey != "" {
			verifier = payment.NewVerifier(cfg.AI.OpenAIBaseURL, cfg.AI.OpenAIAPIKey, cfg.Payment.VisionModel, cfg.Payment.CheckTimeout)
			log.Info("payments enabled",
				zap.String("vision_model", cfg.Payment.VisionModel),
				zap.Int("price_slide_photo", cfg.Payment.PriceSlidePhoto),
				zap.Int("price_slide_plain", cfg.Payment.PriceSlidePlain),
				zap.Int("price_document", cfg.Payment.PriceDocument),
				zap.Int("price_page", cfg.Payment.PricePage))
		} else {
			log.Warn("OPENAI_API_KEY yo'q — to'lov cheklari AI siz, faqat admin tomonidan tekshiriladi")
		}
	} else {
		log.Warn("to'lov o'chirilgan (PAYMENT_ENABLED=false) — barcha ishlar bepul")
	}

	// Kunlik backup: pg_dump bilan olingan baza nusxasi har kuni adminlarga yuboriladi.
	var backupOpts bot.BackupOptions
	if cfg.Backup.Enabled {
		dumper := backup.New(cfg.DB, cfg.Backup.PgDump, "", cfg.Backup.Timeout)
		if err := dumper.Check(); err != nil {
			log.Warn("kunlik backup o'chirildi", zap.Error(err))
		} else {
			backupOpts = bot.BackupOptions{Dumper: dumper, Hour: cfg.Backup.Hour, Minute: cfg.Backup.Minute}
			log.Info("daily backup enabled", zap.String("time", fmt.Sprintf("%02d:%02d", cfg.Backup.Hour, cfg.Backup.Minute)))
		}
	}

	// 5. Handler. Generatsiyalar signal kontekstidan alohida kontekstda ishlaydi:
	// to'xtash signali kelganda ular darhol uzilmaydi, balki ShutdownTimeout
	// davomida yakunlanishiga imkon beriladi.
	jobsCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()

	handler := bot.NewHandler(jobsCtx, sessions, aiClient, bot.Builders{Slides: pptx, Papers: docx}, log, bot.Options{
		Limits:            cfg.Limits,
		MaxConcurrentJobs: cfg.Generator.MaxConcurrentJobs,
		JobTimeout:        cfg.Generator.JobTimeout,
		// Dizaynlar ko'rinishi rasmi shablon yonida turadi (go run ./cmd/preview bilan yangilanadi).
		DesignPreview:    filepath.Join(filepath.Dir(cfg.Generator.TemplatePath), "designs.png"),
		DocDesignPreview: filepath.Join(filepath.Dir(cfg.Generator.TemplatePath), "doc_designs.png"),
		Images:           imageService,
		Billing:          billing,
		Verifier:         verifier,
		Payment:          cfg.Payment,
		Referral:         cfg.Referral,
		Broadcasts:       broadcasts,
		Custom:           customTariffs,
		Stats:            stats,
		Backup:           backupOpts,
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

	// Oldingi ishga tushirishda yakunlanmay qolgan generatsiyalar va admin
	// e'lonlarini tiklash.
	handler.RecoverInterrupted(ctx)
	// Adminlar menyusidagi buyruqlarni joriy ro'yxat bilan yangilash.
	handler.RefreshAdminCommands(ctx)

	// Sog'lik fayli (Docker HEALTHCHECK): baza javob bersa har 15 soniyada
	// yangilanadi; to'xtash signalida tekshiruv ham to'xtaydi.
	if cfg.HealthFile != "" {
		sqlDB, err := db.DB()
		if err != nil {
			return fmt.Errorf("health: baza ulanishini olib bo'lmadi: %w", err)
		}
		go health.New(sqlDB, cfg.HealthFile, log).Run(ctx)
	}

	go b.Start()
	go handler.RunBackups(ctx)
	log.Info("bot started",
		zap.String("username", b.Me.Username),
		zap.String("ai_provider", cfg.AI.Provider),
		zap.Int("max_concurrent_jobs", cfg.Generator.MaxConcurrentJobs),
		zap.String("health_file", cfg.HealthFile))

	// 7. To'xtash signalini kutish.
	<-ctx.Done()
	log.Info("shutdown signal received, stopping bot")

	b.Stop() // yangi update'lar qabul qilinmaydi
	// E'lonlar kutilmaydi: jarayoni saqlanadi va keyingi ishga tushishda davom etadi.
	handler.PauseBroadcasts()

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

// newCustomTariffs AI maxsus dizayn tariflarini yaratadi: har bir tarif uchun
// o'z matn modeli (dizayn konsepsiyasi va slaydlar) hamda o'z sifatidagi rasm
// modeli. Ikkalasi ham OpenAI'da (OPENAI_API_KEY); kalit bo'lmasa yoki
// CUSTOM_DESIGN_ENABLED=false bo'lsa — nil (tugma ko'rsatilmaydi).
func newCustomTariffs(cfg *config.Config, log *zap.Logger) map[string]bot.CustomTariff {
	if !cfg.Custom.Enabled {
		log.Info("AI maxsus dizayn o'chirilgan (CUSTOM_DESIGN_ENABLED=false)")
		return nil
	}
	if cfg.AI.OpenAIAPIKey == "" {
		log.Warn("OPENAI_API_KEY yo'q — AI maxsus dizayn o'chirildi")
		return nil
	}
	out := make(map[string]bot.CustomTariff, len(config.Tariffs))
	for _, name := range config.Tariffs {
		t, ok := cfg.Custom.Tier(name)
		if !ok {
			continue
		}
		gen := images.NewOpenAI(cfg.AI.OpenAIBaseURL, cfg.AI.OpenAIAPIKey, cfg.Custom.ImageModel, t.ImageQuality, cfg.Images.Timeout)
		// Har bir tarifning o'z xizmati: boshqa (qimmatroq yoki arzonroq) sifatga o'tib ketmaydi.
		svc := images.NewService(gen, cfg.Images.Parallel, cfg.Images.Timeout, log.With(zap.String("tariff", name)))
		out[name] = bot.CustomTariff{
			PriceSlide: t.PriceSlide,
			PricePage:  t.PricePage,
			Tariff: generation.Tariff{
				ImagePercent: t.ImagePercent,
				AI:           ai.NewOpenAIModel(cfg.AI, t.TextModel, log),
				Images:       svc,
				TextModel:    t.TextModel,
				ImageModel:   cfg.Custom.ImageModel,
				ImageQuality: t.ImageQuality,
			},
		}
		log.Info("custom design tariff",
			zap.String("tariff", name),
			zap.Int("price_slide", t.PriceSlide),
			zap.Int("price_page", t.PricePage),
			zap.String("text_model", t.TextModel),
			zap.String("image", cfg.Custom.ImageModel+":"+t.ImageQuality),
			zap.Int("image_percent", t.ImagePercent))
	}
	return out
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
