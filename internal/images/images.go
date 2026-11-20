// Package images AI rasm modellari yordamida taqdimot slaydlari uchun rasm chizadi.
//
// Rasm modellari ikki tarifga bo'lingan:
//   - standard (arzon)  — OpenAI gpt-image-2 (low sifat);
//   - premium (sifatli) — hozircha ham OpenAI; Gemini Nano Banana'ga
//     IMAGE_PREMIUM_PROVIDER=gemini bilan qaytarish mumkin.
//
// Qaysi tarifga qaysi provayder ulanishi konfiguratsiyada belgilanadi
// (IMAGE_STANDARD_PROVIDER, IMAGE_PREMIUM_PROVIDER). Service so'rovlarni
// parallel bajaradi, vaqtinchalik xatoda qayta urinadi, kerak bo'lsa boshqa
// tarif provayderiga o'tadi va natijani JPEG ko'rinishiga keltiradi.
//
// Fayllar:
//   - images.go — umumiy turlar, Service va tarif marshrutlash;
//   - openai.go — OpenAI Images API (gpt-image-*) klienti;
//   - gemini.go — Gemini rasm modeli (Nano Banana) klienti.
package images

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // PNG javoblarni o'qish uchun (Gemini PNG qaytaradi)
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/httpx"
)

// Tier — rasm sifati tarifi. Qiymatlari bazada (Session.Plan) saqlanadi.
type Tier string

const (
	TierStandard Tier = config.TierStandard // arzon
	TierPremium  Tier = config.TierPremium  // sifatli
)

// Valid tarif ma'lum qiymatlardan biri ekanini tekshiradi.
func (t Tier) Valid() bool { return t == TierStandard || t == TierPremium }

// Shape — rasm yo'nalishi.
type Shape int

const (
	Landscape Shape = iota // yotiq (titul va yakuniy slayd foni)
	Portrait               // tik (mazmunli slayd yonidagi panel)
	// Square — kvadrat (1024x1024). AI maxsus dizaynda ishlatiladi: kvadrat
	// rasm yotiq/tik rasmdan ~1.5 barobar arzon, slaydga esa "cover" usulida
	// qirqib joylanadi (generator.coverCrop).
	Square
)

func (s Shape) String() string {
	switch s {
	case Portrait:
		return "portrait"
	case Square:
		return "square"
	}
	return "landscape"
}

// Request — bitta rasm so'rovi.
type Request struct {
	Prompt string
	Shape  Shape
}

// Result — bitta rasm natijasi. Data nil bo'lsa — rasm chizilmadi.
type Result struct {
	Data     []byte // JPEG
	Provider string // rasmni chizgan provayder (log va xarajat hisobi uchun)
}

// Generator — aniq bir rasm modeli bilan aloqa.
type Generator interface {
	Name() string
	// Generate rasmni chizadi va uning baytlarini (JPEG yoki PNG) qaytaradi.
	Generate(ctx context.Context, prompt string, shape Shape) ([]byte, error)
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// Service tariflar bo'yicha rasm provayderlarini boshqaradi.
// Bir vaqtning o'zida bir nechta goroutine'dan chaqirilishi mumkin.
type Service struct {
	tiers    map[Tier]Generator
	parallel int
	timeout  time.Duration
	fallback bool // asosiy provayder ishlamasa, boshqa tarif provayderiga o'tish
	log      *zap.Logger

	// retryDelay — vaqtinchalik xatodan keyingi pauza (testlarda qisqartiriladi).
	retryDelay time.Duration
}

// NewService berilgan tarif provayderlari bilan Service yaratadi
// (nil generatorlar e'tiborsiz qoldiriladi).
func NewService(tiers map[Tier]Generator, parallel int, timeout time.Duration, fallback bool, log *zap.Logger) *Service {
	t := make(map[Tier]Generator, len(tiers))
	for tier, g := range tiers {
		if g != nil {
			t[tier] = g
		}
	}
	return &Service{
		tiers:      t,
		parallel:   max(1, parallel),
		timeout:    timeout,
		fallback:   fallback,
		log:        log.Named("images"),
		retryDelay: 2 * time.Second,
	}
}

// New konfiguratsiyaga ko'ra provayderlarni yaratadi. Kaliti berilmagan
// provayder ulanmaydi; birorta ham provayder ulanmasa, Available() false.
func New(ctx context.Context, ai config.AIConfig, cfg config.ImagesConfig, log *zap.Logger) (*Service, error) {
	built := make(map[string]Generator)
	build := func(provider string) (Generator, error) {
		if g, ok := built[provider]; ok {
			return g, nil
		}
		var g Generator
		switch provider {
		case config.ImageProviderOpenAI:
			if ai.OpenAIAPIKey == "" {
				log.Warn("OPENAI_API_KEY berilmagan — OpenAI rasm provayderi ulanmadi")
				return nil, nil
			}
			g = NewOpenAI(ai.OpenAIBaseURL, ai.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIQuality, cfg.Timeout)
		case config.ImageProviderGemini:
			if ai.GeminiAPIKey == "" {
				log.Warn("GEMINI_API_KEY berilmagan — Gemini rasm provayderi ulanmadi")
				return nil, nil
			}
			gm, err := NewGemini(ctx, ai.GeminiAPIKey, cfg.GeminiModel, cfg.GeminiSize)
			if err != nil {
				return nil, err
			}
			g = gm
		default: // config.ImageProviderNone
			return nil, nil
		}
		built[provider] = g
		return g, nil
	}

	tiers := make(map[Tier]Generator, 2)
	for tier, provider := range map[Tier]string{TierStandard: cfg.StandardProvider, TierPremium: cfg.PremiumProvider} {
		g, err := build(provider)
		if err != nil {
			return nil, fmt.Errorf("images: %w", err)
		}
		if g != nil {
			tiers[tier] = g
		}
	}

	s := NewService(tiers, cfg.Parallel, cfg.Timeout, cfg.Fallback, log)
	log.Info("image service initialised",
		zap.String("standard", s.Provider(TierStandard)),
		zap.String("premium", s.Provider(TierPremium)),
		zap.String("default_tier", cfg.DefaultTier),
		zap.Bool("fallback", cfg.Fallback))
	return s, nil
}

// Available kamida bitta rasm provayderi ulanganini bildiradi.
func (s *Service) Available() bool { return s != nil && len(s.tiers) > 0 }

// Provider tarif uchun asosiy provayder nomini qaytaradi ("-" — ulanmagan).
func (s *Service) Provider(tier Tier) string {
	if c := s.chain(tier); len(c) > 0 {
		return c[0].Name()
	}
	return "-"
}

// chain tarif uchun provayderlar ketma-ketligi: avval o'z provayderi
// (bo'lmasa — boshqa tarifniki), so'ng zaxira sifatida boshqa tarif provayderi.
func (s *Service) chain(tier Tier) []Generator {
	if s == nil {
		return nil
	}
	var out []Generator
	add := func(g Generator) {
		for _, x := range out {
			if x == g {
				return
			}
		}
		out = append(out, g)
	}
	if g, ok := s.tiers[tier]; ok {
		add(g)
	}
	for _, t := range []Tier{TierStandard, TierPremium} {
		if g, ok := s.tiers[t]; ok {
			add(g)
		}
	}
	return out
}

// Generate so'rovlarni ko'pi bilan parallel tadan bajaradi. Natijalar
// so'rovlar tartibida qaytariladi; chizilmagan rasm o'rnida Data nil bo'ladi.
// onDone (ixtiyoriy) har bir rasm tugaganda chaqiriladi.
func (s *Service) Generate(ctx context.Context, tier Tier, reqs []Request, onDone func(done, total int)) []Result {
	out := make([]Result, len(reqs))
	chain := s.chain(tier)
	if len(chain) == 0 || len(reqs) == 0 {
		return out
	}

	var (
		// Funksiyalar doim nil qaytaradi (chizilmagan rasm — Data nil), shuning
		// uchun guruh konteksti kerak emas: bitta rasm xatosi qolganlarini to'xtatmaydi.
		g    errgroup.Group
		mu   sync.Mutex
		done int
	)
	g.SetLimit(s.parallel)
	for i, r := range reqs {
		if ctx.Err() != nil {
			break
		}
		// Go bo'sh joy chiqquncha kutadi (SetLimit).
		g.Go(func() error {
			defer func() {
				if p := recover(); p != nil {
					s.log.Error("panic in image generation", zap.Any("panic", p))
				}
			}()
			// Navbat kutilayotganda kontekst bekor qilingan bo'lishi mumkin.
			if ctx.Err() != nil {
				return nil
			}

			out[i] = s.one(ctx, chain, r)
			if onDone != nil {
				mu.Lock()
				done++
				onDone(done, len(reqs))
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait() // doim nil
	return out
}

// one bitta rasmni chizadi: vaqtinchalik xatoda bir marta qayta urinadi,
// doimiy xatoda (yoki ikkinchi muvaffaqiyatsizlikda) keyingi provayderga o'tadi.
func (s *Service) one(ctx context.Context, chain []Generator, r Request) Result {
	for i, g := range chain {
		if i > 0 && !s.fallback {
			break
		}
		log := s.log.With(zap.String("provider", g.Name()), zap.String("shape", r.Shape.String()))
		for attempt := range 2 {
			if attempt > 0 && !httpx.Sleep(ctx, httpx.Backoff(attempt, s.retryDelay, s.retryDelay)) {
				return Result{}
			}
			started := time.Now()
			data, err := s.call(ctx, g, r)
			if err == nil {
				var jpg []byte
				if jpg, err = toJPEG(data); err == nil {
					log.Info("image generated",
						zap.Int("attempt", attempt+1),
						zap.Int("bytes", len(jpg)),
						zap.Duration("took", time.Since(started)))
					return Result{Data: jpg, Provider: g.Name()}
				}
				err = httpx.Permanent(fmt.Errorf("rasm formati noto'g'ri: %w", err))
			}
			log.Warn("image generation failed",
				zap.Int("attempt", attempt+1),
				zap.Duration("took", time.Since(started)),
				zap.Bool("permanent", httpx.IsPermanent(err)),
				zap.Error(err))
			if ctx.Err() != nil {
				return Result{}
			}
			if httpx.IsPermanent(err) {
				break
			}
		}
	}
	return Result{}
}

// defaultTimeout — Service timeout berilmaganda (<= 0) bitta urinish uchun
// muddat (IMAGE_TIMEOUT standart qiymati bilan bir xil). Umumiy HTTP klientda
// timeout yo'q, shuning uchun har bir so'rov kontekst muddati bilan cheklanishi shart.
const defaultTimeout = 90 * time.Second

// call bitta urinishga alohida timeout beradi.
func (s *Service) call(ctx context.Context, g Generator, r Request) ([]byte, error) {
	timeout := s.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return g.Generate(ctx, r.Prompt, r.Shape)
}

// jpegQuality — PNG dan o'girishdagi JPEG sifati.
const jpegQuality = 85

// toJPEG rasmni JPEG ko'rinishiga keltiradi: JPEG o'zgarishsiz qaytariladi,
// PNG esa qayta kodlanadi (PNG fotosuratlar bir necha barobar katta bo'ladi).
func toJPEG(data []byte) ([]byte, error) {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if format == "jpeg" {
		return data, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
