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
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // PNG javoblarni o'qish uchun (Gemini PNG qaytaradi)
	"math/rand/v2"
	"sync"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/config"
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

// permanentError — qayta urinish foyda bermaydigan xato (noto'g'ri kalit,
// balans yetarli emas, kontent filtri va h.k.).
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func permanent(err error) error { return &permanentError{err: err} }

func isPermanent(err error) bool {
	var pe *permanentError
	return errors.As(err, &pe)
}

// isPermanentStatus HTTP status qayta urinish bilan tuzalmasligini bildiradi
// (4xx, 429 va 408 dan tashqari).
func isPermanentStatus(code int) bool {
	return code >= 400 && code < 500 && code != 429 && code != 408
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
		sem  = make(chan struct{}, s.parallel)
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
	)
	for i, r := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					s.log.Error("panic in image generation", zap.Any("panic", p))
				}
			}()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			out[i] = s.one(ctx, chain, r)
			if onDone != nil {
				mu.Lock()
				done++
				onDone(done, len(reqs))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
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
			if attempt > 0 && !sleep(ctx, s.retryDelay) {
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
				err = permanent(fmt.Errorf("rasm formati noto'g'ri: %w", err))
			}
			log.Warn("image generation failed",
				zap.Int("attempt", attempt+1),
				zap.Duration("took", time.Since(started)),
				zap.Bool("permanent", isPermanent(err)),
				zap.Error(err))
			if ctx.Err() != nil {
				return Result{}
			}
			if isPermanent(err) {
				break
			}
		}
	}
	return Result{}
}

// call bitta urinishga alohida timeout beradi.
func (s *Service) call(ctx context.Context, g Generator, r Request) ([]byte, error) {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	return g.Generate(ctx, r.Prompt, r.Shape)
}

// sleep d (+ tasodifiy jitter) kutadi; kontekst bekor bo'lsa false qaytaradi.
func sleep(ctx context.Context, d time.Duration) bool {
	if d > 0 {
		d += time.Duration(rand.Int64N(int64(d)/2 + 1))
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
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
