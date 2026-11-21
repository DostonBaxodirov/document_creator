// Package images AI rasm modeli yordamida taqdimot slaydlari va Word ishlar
// uchun rasm chizadi.
//
// Rasm provayderi bitta — OpenAI Images API (gpt-image-*; model va sifat
// OPENAI_IMAGE_MODEL, OPENAI_IMAGE_QUALITY bilan beriladi). Service
// so'rovlarni parallel bajaradi, vaqtinchalik xatoda bir marta qayta urinadi
// va natijani JPEG ko'rinishiga keltiradi.
//
// Fayllar:
//   - images.go — umumiy turlar va Service;
//   - openai.go — OpenAI Images API (gpt-image-*) klienti.
package images

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // PNG javoblarni o'qish uchun (masalan, OpenAI-mos API output_format ni e'tiborsiz qoldirsa)
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/httpx"
)

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

// Service rasm provayderi bilan ishlaydi: so'rovlarni parallel bajaradi,
// vaqtinchalik xatoda qayta urinadi va natijani JPEG ga keltiradi.
// Bir vaqtning o'zida bir nechta goroutine'dan chaqirilishi mumkin.
type Service struct {
	gen      Generator // nil — provayder ulanmagan
	parallel int
	timeout  time.Duration
	log      *zap.Logger

	// retryDelay — vaqtinchalik xatodan keyingi pauza (testlarda qisqartiriladi).
	retryDelay time.Duration
}

// NewService berilgan provayder bilan Service yaratadi (gen nil bo'lsa —
// provayder ulanmagan, Available() false).
func NewService(gen Generator, parallel int, timeout time.Duration, log *zap.Logger) *Service {
	return &Service{
		gen:        gen,
		parallel:   max(1, parallel),
		timeout:    timeout,
		log:        log.Named("images"),
		retryDelay: 2 * time.Second,
	}
}

// New konfiguratsiyaga ko'ra OpenAI rasm provayderini yaratadi. OPENAI_API_KEY
// berilmagan bo'lsa, provayder ulanmaydi va Available() false.
func New(ai config.AIConfig, cfg config.ImagesConfig, log *zap.Logger) (*Service, error) {
	var gen Generator
	if ai.OpenAIAPIKey == "" {
		log.Warn("OPENAI_API_KEY berilmagan — OpenAI rasm provayderi ulanmadi")
	} else {
		gen = NewOpenAI(ai.OpenAIBaseURL, ai.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIQuality, cfg.Timeout)
	}

	s := NewService(gen, cfg.Parallel, cfg.Timeout, log)
	provider := "-"
	if gen != nil {
		provider = gen.Name()
	}
	log.Info("image service initialised", zap.String("provider", provider))
	return s, nil
}

// Available rasm provayderi ulanganini bildiradi.
func (s *Service) Available() bool { return s != nil && s.gen != nil }

// Generate so'rovlarni ko'pi bilan parallel tadan bajaradi. Natijalar
// so'rovlar tartibida qaytariladi; chizilmagan rasm o'rnida Data nil bo'ladi.
// onDone (ixtiyoriy) har bir rasm tugaganda chaqiriladi.
func (s *Service) Generate(ctx context.Context, reqs []Request, onDone func(done, total int)) []Result {
	out := make([]Result, len(reqs))
	if !s.Available() || len(reqs) == 0 {
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

			out[i] = s.one(ctx, r)
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
// doimiy xatoda qayta urinmaydi (chizilmagan rasm — Data nil).
func (s *Service) one(ctx context.Context, r Request) Result {
	g := s.gen
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
		if ctx.Err() != nil || httpx.IsPermanent(err) {
			return Result{}
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
