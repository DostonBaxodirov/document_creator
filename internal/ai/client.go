// Package ai sun'iy intellekt (Google Gemini, OpenAI yoki DeepSeek) yordamida
// taqdimot slaydlari va hujjat (mustaqil ish, amaliy topshiriq) matnini
// JSON ko'rinishida generatsiya qiladi.
//
// Tashqi kod faqat Client interfeysi bilan ishlaydi; qaysi provayder
// ishlatilishi konfiguratsiya orqali tanlanadi.
//
// Fayllar:
//   - client.go    — umumiy turlar, qayta urinish mantiqi va taqdimot generatsiyasi;
//   - document.go  — hujjat generatsiyasi (reja → bo'limlar parallel yoziladi);
//   - providers.go — Gemini va OpenAI-mos (OpenAI, DeepSeek) API klientlari.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"google.golang.org/genai"

	"telegram-presentation-bot/internal/config"
)

// WorkType — ish turi. Qiymatlari bazada saqlanadi, shuning uchun o'zgartirmang.
type WorkType string

const (
	WorkPresentation WorkType = "presentation" // Slayd (taqdimot) → .pptx
	WorkIndependent  WorkType = "independent"  // Mustaqil ish → .docx
	WorkPractical    WorkType = "practical"    // Amaliy topshiriq → .docx
	WorkReferat      WorkType = "referat"      // Referat → .docx
)

// Valid ish turi ma'lum qiymatlardan biri ekanini tekshiradi.
func (w WorkType) Valid() bool {
	switch w {
	case WorkPresentation, WorkIndependent, WorkPractical, WorkReferat:
		return true
	}
	return false
}

// IsDocument ish turi matnli hujjat (.docx) ekanini bildiradi.
func (w WorkType) IsDocument() bool {
	return w == WorkIndependent || w == WorkPractical || w == WorkReferat
}

// HasDesigns Word ish uchun dizayn tanlanishini bildiradi (mustaqil ish va
// referat; amaliy topshiriq — doim Klassik uslubda).
func (w WorkType) HasDesigns() bool {
	return w == WorkIndependent || w == WorkReferat
}

// Client — matn generatsiya qiluvchi servis.
type Client interface {
	// GeneratePresentation taqdimot slaydlarini yaratadi.
	GeneratePresentation(ctx context.Context, req PresentationRequest) (*Presentation, error)
	// GenerateDocument mustaqil ish yoki amaliy topshiriq matnini yaratadi.
	GenerateDocument(ctx context.Context, req DocumentRequest) (*Document, error)
	// GenerateDesign mavzuga mos maxsus dizayn konsepsiyasini yaratadi.
	GenerateDesign(ctx context.Context, req DesignRequest) (*Design, error)
	// GenerateDocDesign Word hujjat uchun maxsus dizayn konsepsiyasini yaratadi.
	GenerateDocDesign(ctx context.Context, req DesignRequest) (*DocDesign, error)
}

// task — provayderga yuboriladigan bitta so'rov.
type task struct {
	system string
	user   string
	// schema — Gemini structured output uchun javob sxemasi. OpenAI-mos
	// provayderlar uni e'tiborsiz qoldiradi (ular JSON rejimida ishlaydi,
	// javob shakli esa promptda tavsiflanadi).
	schema *genai.Schema
}

// provider — aniq bir AI API bilan aloqa.
type provider interface {
	name() string
	// complete so'rovni yuborib, xom matnli (JSON) javobni qaytaradi.
	complete(ctx context.Context, t task) (string, error)
}

// permanentError — qayta urinish foyda bermaydigan xato (noto'g'ri API kalit,
// noto'g'ri model nomi, balans yetarli emas, kontent bloklangan va h.k.).
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func permanent(err error) error { return &permanentError{err: err} }

func isPermanent(err error) bool {
	var pe *permanentError
	return errors.As(err, &pe)
}

// client — provayderdan mustaqil umumiy mantiq: prompt, qayta urinish, JSON parse.
type client struct {
	p          provider
	timeout    time.Duration
	maxRetries int
	parallel   int // hujjat bo'limlari uchun bir vaqtdagi so'rovlar soni
	log        *zap.Logger
	// sleep — xatodan keyingi kechikish (nil bo'lsa sleepBackoff). Testlar
	// haqiqatan kutmasligi uchun almashtiriladi.
	sleep func(ctx context.Context, retry int) error
}

// maxIncompleteRetries — javob o'qilgan, lekin to'liq emas (ball want dan kam)
// bo'lganda qayta urinishlar soni. AI_MAX_RETRIES ga bog'liq emas; bunday
// qayta urinish kechikishsiz, darhol qilinadi.
const maxIncompleteRetries = 1

// New konfiguratsiyaga ko'ra kerakli provayder bilan Client yaratadi.
func New(ctx context.Context, cfg config.AIConfig, log *zap.Logger) (Client, error) {
	var (
		p   provider
		err error
	)
	switch cfg.Provider {
	case config.ProviderGemini:
		p, err = newGemini(ctx, cfg)
	case config.ProviderOpenAI:
		p = newOpenAI(cfg)
	case config.ProviderDeepSeek:
		p = newDeepSeek(cfg)
	default:
		err = fmt.Errorf("noma'lum AI provayder: %q", cfg.Provider)
	}
	if err != nil {
		return nil, fmt.Errorf("ai: %w", err)
	}

	log.Info("AI client initialised",
		zap.String("provider", p.name()),
		zap.Int("parallel_requests", cfg.ParallelRequests))
	return &client{
		p:          p,
		timeout:    cfg.Timeout,
		maxRetries: cfg.MaxRetries,
		parallel:   max(1, cfg.ParallelRequests),
		log:        log.Named("ai"),
	}, nil
}

// NewOpenAIModel OpenAI (yoki OPENAI_BASE_URL dagi mos API) bilan ishlaydigan,
// modeli aniq berilgan Client yaratadi. AI maxsus dizayn tariflari uchun
// (standard, optimal, premium) har biriga o'z modeli bilan alohida klient
// yaratiladi; asosiy AI_PROVIDER ga bog'liq emas.
func NewOpenAIModel(cfg config.AIConfig, model string, log *zap.Logger) Client {
	cfg.OpenAIModel = model
	// AI_MAX_TOKENS asosiy provayder uchun sozlangan bo'lishi mumkin (masalan,
	// DeepSeek'ning katta chegarasi) — OpenAI modeli uni rad etmasligi uchun
	// modelning o'z standart chegarasi ishlatiladi.
	cfg.MaxTokens = 0
	p := newOpenAI(cfg)
	log.Info("AI client initialised", zap.String("provider", p.name()), zap.String("purpose", "custom design"))
	return &client{
		p:          p,
		timeout:    cfg.Timeout,
		maxRetries: cfg.MaxRetries,
		parallel:   max(1, cfg.ParallelRequests),
		log:        log.Named("ai").With(zap.String("model", model)),
	}
}

// generateJSON so'rovni yuboradi, javobni parse qiladi va kerak bo'lsa qayta uriniladi.
//
// parse natija bilan birga uning "to'liqlik balli"ni (slaydlar soni, so'zlar
// soni va h.k.) qaytaradi. Ball want dan kam bo'lmasa — natija darhol
// qaytariladi; aks holda kechikishsiz ko'pi bilan maxIncompleteRetries marta
// qayta uriniladi va eng yuqori balli natija saqlab qo'yiladi. Vaqtinchalik
// xatolar (tarmoq, 429, 5xx, buzilgan JSON) bo'lganda eksponensial kechikish
// bilan maxRetries martagacha qayta uriniladi, doimiy xatolarda — to'xtaladi.
func generateJSON[T any](ctx context.Context, c *client, op string, t task, want int,
	parse func(raw string) (T, int, error)) (T, error) {
	var (
		zero      T
		best      T
		bestScore = -1
		lastErr   error
		// errRetries — xato sababli qayta urinishlar, incompleteRetries —
		// to'liq bo'lmagan natija sababli; har biri o'z chegarasiga ega.
		errRetries, incompleteRetries int
		// needBackoff — keyingi urinishdan oldin kutish kerakmi (oldingisi xato bo'lgan).
		needBackoff bool
	)
	log := c.log.With(zap.String("op", op))

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if needBackoff {
				if err := c.backoff(ctx, errRetries); err != nil {
					break
				}
			} else if ctx.Err() != nil {
				break
			}
		}

		started := time.Now()
		raw, err := c.completeWithTimeout(ctx, t)
		if err != nil {
			lastErr = err
			log.Warn("AI request failed",
				zap.Int("attempt", attempt+1),
				zap.Duration("took", time.Since(started)),
				zap.Bool("permanent", isPermanent(err)),
				zap.Error(err))
			if isPermanent(err) || ctx.Err() != nil || errRetries >= c.maxRetries {
				break
			}
			errRetries++
			needBackoff = true
			continue
		}

		val, score, err := parse(raw)
		if err != nil {
			lastErr = fmt.Errorf("AI javobini o'qib bo'lmadi: %w", err)
			log.Warn("AI returned invalid JSON",
				zap.Int("attempt", attempt+1),
				zap.String("raw_prefix", truncateRunes(raw, 300)),
				zap.Error(err))
			if errRetries >= c.maxRetries {
				break
			}
			errRetries++
			needBackoff = true
			continue
		}

		log.Info("AI content generated",
			zap.Int("attempt", attempt+1),
			zap.Duration("took", time.Since(started)),
			zap.Int("score", score),
			zap.Int("want", want))

		if score >= want {
			return val, nil
		}
		if score > bestScore {
			best, bestScore = val, score
		}
		lastErr = fmt.Errorf("AI natijasi to'liq emas (%d/%d)", score, want)
		if incompleteRetries >= maxIncompleteRetries {
			break
		}
		incompleteRetries++
		needBackoff = false
	}

	if bestScore >= 0 {
		log.Warn("using incomplete AI result", zap.Int("score", bestScore), zap.Int("want", want))
		return best, nil
	}
	if lastErr == nil {
		lastErr = ctx.Err()
	}
	return zero, fmt.Errorf("ai (%s, %s): %w", c.p.name(), op, lastErr)
}

// completeWithTimeout har bir urinishga alohida timeout beradi.
func (c *client) completeWithTimeout(ctx context.Context, t task) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.p.complete(ctx, t)
}

// backoff retry-chi xatodan keyingi qayta urinishdan oldin kutadi (c.sleep
// berilmagan bo'lsa — sleepBackoff).
func (c *client) backoff(ctx context.Context, retry int) error {
	if c.sleep != nil {
		return c.sleep(ctx, retry)
	}
	return sleepBackoff(ctx, retry)
}

// sleepBackoff 2s, 4s, 8s ... (+ tasodifiy jitter) kutadi; kontekst bekor bo'lsa to'xtaydi.
func sleepBackoff(ctx context.Context, attempt int) error {
	base := time.Duration(1<<min(attempt-1, 4)) * 2 * time.Second
	jitter := time.Duration(rand.Int64N(int64(time.Second)))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(base + jitter):
		return nil
	}
}

// ---------------------------------------------------------------------------
// Umumiy prompt qoidalari
// ---------------------------------------------------------------------------

// Promptlar ingliz tilida: modellar ko'rsatmalarga aniqroq amal qiladi.
// Kontent tili esa mavzu tiliga qarab belgilanadi (odatda o'zbek tili).
// "JSON" so'zi albatta bo'lishi kerak — DeepSeek/OpenAI JSON rejimi buni talab qiladi.

// finalTextRules — matn tayyor, haqiqiy ish ko'rinishida bo'lishi uchun qoidalar
// (ikkala tizim promptiga qo'shiladi; sanitize.go dagi filtr — ikkinchi qatlam).
const finalTextRules = `- Write as the author of the work. The text must read as a finished academic work that is ready to be submitted as is.
- Never refer to yourself, and never say or imply that the text was written or generated by artificial intelligence, a language model or a chatbot. (If the topic itself is about artificial intelligence, discussing it as a subject is fine.)
- Never add notes, disclaimers, warnings or advice addressed to the reader (for example, to check, verify, edit, update or replace anything).
- Never use placeholders such as [Name], [year], XX or ___: always write concrete, final text.
- No conversational phrases such as "Of course" or "Here is".`

// languageRules — kontent tili qoidasi (ikkala tizim promptida). Til
// foydalanuvchi tanlovi bo'yicha topshiriqda beriladi (languageRule).
const languageRules = `- Write ALL content in the content language given in the task; if none is given, use the language of the topic. ` +
	`For Uzbek in the Latin script use the correct letters (o', g', sh, ch); for Uzbek in the Cyrillic script use ў, қ, ғ, ҳ.`

// languageRule topshiriqdagi kontent tili qatori (language — inglizcha nom,
// masalan "Russian"; bo'sh bo'lsa — qator qo'shilmaydi).
func languageRule(language string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		return ""
	}
	return fmt.Sprintf("Content language: %s. Write every text field in %s, even if the topic is written in another language.\n", language, language)
}

// maxRequirementsRunes — promptga qo'shiladigan qo'shimcha talablar uzunligi chegarasi.
const maxRequirementsRunes = 800

// requirementsBlock foydalanuvchining qo'shimcha talablarini promptga alohida
// blok sifatida qo'shadi. Talablar format, hajm va til qoidalarini bekor
// qila olmaydi — bu promptda aniq aytiladi.
func requirementsBlock(req string) string {
	fenced := fencedRequirements(req)
	if fenced == "" {
		return ""
	}
	return "Additional requirements from the student. Follow them as long as they do not conflict with the JSON format, " +
		"the requested number of slides or chapters, the content language or the rules above:\n" + fenced
}

// fencedRequirements talablarni """ ... """ bloki ichida qaytaradi (bo'sh
// bo'lsa — bo'sh satr).
func fencedRequirements(req string) string {
	// Foydalanuvchi matni blok chegarasini (""") buzmasligi kerak. Bir marta
	// almashtirish yetmaydi: `"""""` → `"""`.
	for strings.Contains(req, `"""`) {
		req = strings.ReplaceAll(req, `"""`, `"`)
	}
	req = strings.TrimSpace(req)
	if req == "" {
		return ""
	}
	return "\"\"\"\n" + truncateRunes(req, maxRequirementsRunes) + "\n\"\"\""
}

// writeRequirements talablar blokini promptning oxiriga bo'sh qator bilan
// ajratib qo'shadi (talab bo'lmasa — hech narsa qo'shilmaydi).
func writeRequirements(b *strings.Builder, req string) {
	block := requirementsBlock(req)
	if block == "" {
		return
	}
	switch s := b.String(); {
	case strings.HasSuffix(s, "\n\n"):
	case strings.HasSuffix(s, "\n"):
		b.WriteString("\n")
	default:
		b.WriteString("\n\n")
	}
	b.WriteString(block)
}

func stringArraySchema() *genai.Schema {
	return &genai.Schema{Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}}
}

// ---------------------------------------------------------------------------
// JSON parse va normalizatsiya
// ---------------------------------------------------------------------------

// decodeJSON AI javobidan JSON ni ajratib oladi. Modellar ba'zan JSON ni
// ```json ... ``` ichiga o'raydi yoki obyekt o'rniga faqat massivni qaytaradi:
// massiv bo'lsa arr ga, obyekt bo'lsa obj ga yoziladi (arr nil bo'lsa — massiv xato).
func decodeJSON(raw string, obj, arr any) error {
	s := stripCodeFence(strings.TrimSpace(raw))
	objStart, arrStart := strings.IndexByte(s, '{'), strings.IndexByte(s, '[')

	if arr != nil && arrStart >= 0 && (objStart < 0 || arrStart < objStart) {
		end := strings.LastIndexByte(s, ']')
		if end <= arrStart {
			return errors.New("JSON massiv yopilmagan")
		}
		return json.Unmarshal([]byte(s[arrStart:end+1]), arr)
	}

	end := strings.LastIndexByte(s, '}')
	if objStart < 0 || end <= objStart {
		return errors.New("javobda JSON obyekt topilmadi")
	}
	return json.Unmarshal([]byte(s[objStart:end+1]), obj)
}

func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(s)
	return strings.TrimSpace(strings.TrimSuffix(s, "```"))
}

var (
	// "1. ", "2) ", "- ", "• ", "* ", "– ", "## " kabi qo'lda qo'yilgan prefikslar.
	listPrefixRe = regexp.MustCompile(`^(?:[-•*–—▪●◦]+|#{1,6}|\d{1,2}[.)])\s+`)
	spacesRe     = regexp.MustCompile(`\s+`)
	// Faqat qalin matn va kod belgilari olib tashlanadi ("C#" kabi so'zlar buzilmasligi uchun).
	markdownRe = strings.NewReplacer("**", "", "`", "")
)

// cleanText bo'sh joylarni siqadi, markdown va ro'yxat belgilarini olib tashlaydi.
func cleanText(s string) string {
	s = markdownRe.Replace(s)
	s = spacesRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = listPrefixRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// truncateRunes matnni so'z chegarasida qirqadi va "…" qo'shadi.
func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	r := []rune(s)[:limit]
	cut := string(r)
	if i := strings.LastIndexByte(cut, ' '); i > len(cut)/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-") + "…"
}
