// Package config ilovaning barcha sozlamalarini muhit o'zgaruvchilaridan
// (va mavjud bo'lsa .env faylidan) o'qiydi va tekshiradi.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/joho/godotenv"
)

// AI provayderlari nomlari.
const (
	ProviderGemini   = "gemini"
	ProviderOpenAI   = "openai"
	ProviderDeepSeek = "deepseek"
)

// Rasm provayderlari nomlari (IMAGE_STANDARD_PROVIDER, IMAGE_PREMIUM_PROVIDER).
const (
	ImageProviderOpenAI = "openai"
	ImageProviderGemini = "gemini"
	ImageProviderNone   = "none"
)

// Rasm tariflari: standard — arzon, premium — sifatli.
const (
	TierStandard = "standard"
	TierPremium  = "premium"
)

// DisabledValue — ixtiyoriy matnli sozlamani o'chirish uchun qiymat
// (bo'sh qiymat standart qiymat bilan almashtiriladi, shuning uchun "-" ishlatiladi).
const DisabledValue = "-"

// Config — ilovaning to'liq konfiguratsiyasi.
type Config struct {
	Env       string // "production" yoki "development" (log formatiga ta'sir qiladi)
	LogLevel  string // debug | info | warn | error
	Telegram  TelegramConfig
	AI        AIConfig
	Images    ImagesConfig
	DB        DBConfig
	Generator GeneratorConfig
	Limits    LimitsConfig
	Payment   PaymentConfig
	Custom    CustomConfig
	Backup    BackupConfig
}

// TelegramConfig — Telegram bot sozlamalari.
type TelegramConfig struct {
	Token           string
	PollTimeout     time.Duration // long polling kutish vaqti
	ShutdownTimeout time.Duration // to'xtashda ishlayotgan vazifalarni kutish vaqti
}

// AIConfig — sun'iy intellekt provayderi sozlamalari.
type AIConfig struct {
	Provider string // "gemini", "openai" yoki "deepseek"

	GeminiAPIKey string
	GeminiModel  string

	OpenAIAPIKey  string
	OpenAIModel   string
	OpenAIBaseURL string // OpenAI bilan mos (compatible) API manzili
	// OpenAIStructured — javob formati Structured Outputs (json_schema, strict);
	// API qo'llamasa, avtomatik json_object rejimiga o'tiladi.
	OpenAIStructured bool

	DeepSeekAPIKey   string
	DeepSeekModel    string
	DeepSeekBaseURL  string
	DeepSeekThinking bool // fikrlash (thinking) rejimi: sekinroq va qimmatroq, lekin chuqurroq

	Temperature      *float32      // nil bo'lsa — provayderning standart qiymati ishlatiladi
	MaxTokens        int           // javob uzunligi chegarasi (0 — provayderning standart qiymati)
	Timeout          time.Duration // bitta so'rov uchun maksimal vaqt
	MaxRetries       int           // xato bo'lganda qayta urinishlar soni
	ParallelRequests int           // hujjat bo'limlarini yozishda bir vaqtdagi so'rovlar soni
	SlidesPerRequest int           // katta taqdimotda bitta so'rovda yoziladigan slaydlar soni (3..10)
}

// ImagesConfig — rasmli dizaynlar uchun AI rasm generatsiyasi sozlamalari.
// API kalitlari AIConfig dan olinadi (OPENAI_API_KEY, GEMINI_API_KEY).
type ImagesConfig struct {
	StandardProvider string // arzon tarif provayderi: openai | gemini | none
	PremiumProvider  string // sifatli tarif provayderi: openai | gemini | none
	DefaultTier      string // tarifi belgilanmagan foydalanuvchilar uchun: standard | premium

	OpenAIModel   string // masalan gpt-image-2
	OpenAIQuality string // low | medium | high | auto
	GeminiModel   string // masalan gemini-nano-banana-2.1
	GeminiSize    string // 1K | 2K | 4K; bo'sh — modelning standart qiymati

	Timeout  time.Duration // bitta rasm uchun maksimal vaqt
	Parallel int           // bir vaqtda chiziladigan rasmlar soni (bitta taqdimot ichida)
	Fallback bool          // asosiy provayder ishlamasa, boshqa tarif provayderiga o'tish
}

// AI maxsus dizayn tariflari. Qiymatlar bazada (sessions.tariff,
// orders.tariff) saqlanadi — o'zgartirmang.
const (
	TariffStandard = "standard"
	TariffOptimal  = "optimal"
	TariffPremium  = "premium"
)

// Tariffs — tariflar tartibi (arzonidan qimmatiga).
var Tariffs = []string{TariffStandard, TariffOptimal, TariffPremium}

// CustomConfig — AI maxsus dizayn: AI mavzuga mos dizaynni o'zi yaratadi,
// slaydlarni tarif modeli yozadi va rasmlarni tarif sifatida chizadi.
// Matn ham, rasm ham OpenAI orqali (OPENAI_API_KEY, OPENAI_BASE_URL).
type CustomConfig struct {
	Enabled    bool
	ImageModel string // masalan gpt-image-1 (barcha tariflar uchun umumiy)
	Tiers      map[string]CustomTier
}

// CustomTier — bitta tarif sozlamalari.
type CustomTier struct {
	PriceSlide   int    // bitta slayd narxi, so'mda (titul va yakuniy slayd ham sanaladi)
	PricePage    int    // Word ish (mustaqil ish, referat): bitta sahifa narxi, so'mda
	TextModel    string // slaydlar matni va dizayn konsepsiyasini yozadigan model
	ImageQuality string // low | medium | high
	// ImagePercent — rasmlar soni: taqdimotda slaydlar sonining (titul rasmi
	// ham kiradi; 20 slayd va 40% — 8 ta rasm), Word ishda boblar sonining
	// necha foizi (5 bob va 40% — 2 ta rasm).
	ImagePercent int
}

// Tier tarif sozlamalarini qaytaradi.
func (c CustomConfig) Tier(name string) (CustomTier, bool) {
	t, ok := c.Tiers[name]
	return t, ok
}

// customConfig maxsus dizayn sozlamalarini o'qiydi. Standart qiymatlar
// OpenAI narxlariga mos (20 slaydli taqdimot uchun taxminiy xarajat):
//
//	standard — gpt-4o-mini   + gpt-image-1 low,    8 rasm  ≈ $0.09
//	optimal  — gpt-4.1-mini  + gpt-image-1 medium, 8 rasm  ≈ $0.35–0.39
//	premium  — gpt-4o        + gpt-image-1 high,   10 rasm ≈ $1.72
func customConfig(p *parser) CustomConfig {
	tier := func(prefix string, price, pagePrice int, model, quality string, percent int) CustomTier {
		return CustomTier{
			PriceSlide:   p.int(prefix+"_PRICE", price),
			PricePage:    p.int(prefix+"_PAGE_PRICE", pagePrice),
			TextModel:    p.str(prefix+"_TEXT_MODEL", model),
			ImageQuality: strings.ToLower(p.str(prefix+"_IMAGE_QUALITY", quality)),
			ImagePercent: p.int(prefix+"_IMAGE_PERCENT", percent),
		}
	}
	return CustomConfig{
		Enabled:    p.bool("CUSTOM_DESIGN_ENABLED", true),
		ImageModel: p.str("CUSTOM_IMAGE_MODEL", "gpt-image-1"),
		Tiers: map[string]CustomTier{
			TariffStandard: tier("CUSTOM_STANDARD", 300, 400, "gpt-4o-mini", "low", 40),
			TariffOptimal:  tier("CUSTOM_OPTIMAL", 500, 700, "gpt-4.1-mini", "medium", 40),
			TariffPremium:  tier("CUSTOM_PREMIUM", 1700, 1500, "gpt-4o", "high", 50),
		},
	}
}

// validate maxsus dizayn sozlamalarini tekshiradi (faqat yoqilgan bo'lsa).
// OPENAI_API_KEY bo'lmasa xato emas — bot maxsus dizaynni o'chirib, ogohlantiradi.
func (c CustomConfig) validate() []error {
	if !c.Enabled {
		return nil
	}
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	for _, name := range Tariffs {
		t := c.Tiers[name]
		key := "CUSTOM_" + strings.ToUpper(name)
		if t.PriceSlide <= 0 {
			add("%s_PRICE musbat bo'lishi kerak", key)
		}
		if t.PricePage <= 0 {
			add("%s_PAGE_PRICE musbat bo'lishi kerak", key)
		}
		if t.TextModel == "" {
			add("%s_TEXT_MODEL majburiy", key)
		}
		switch t.ImageQuality {
		case "low", "medium", "high", "auto":
		default:
			add("%s_IMAGE_QUALITY noto'g'ri: %q (low, medium, high yoki auto bo'lishi kerak)", key, t.ImageQuality)
		}
		if t.ImagePercent < 0 || t.ImagePercent > 100 {
			add("%s_IMAGE_PERCENT 0 dan 100 gacha bo'lishi kerak", key)
		}
	}
	return errs
}

// DBConfig — PostgreSQL ulanish sozlamalari.
type DBConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnectRetries  int           // baza tayyor bo'lmasa necha marta qayta urinish
	ConnectDelay    time.Duration // urinishlar orasidagi pauza
}

// DSN PostgreSQL uchun URL ko'rinishidagi ulanish satrini qaytaradi.
// Parol url.UserPassword orqali kodlanadi, shuning uchun maxsus belgilar xavfsiz.
func (d DBConfig) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   net.JoinHostPort(d.Host, strconv.Itoa(d.Port)),
		Path:   "/" + d.Name,
	}
	q := u.Query()
	q.Set("sslmode", d.SSLMode)
	q.Set("TimeZone", "UTC")
	u.RawQuery = q.Encode()
	return u.String()
}

// BackupConfig — bazaning kunlik zaxira nusxasi: har kuni belgilangan vaqtda
// (Toshkent) pg_dump bilan olinadi va barcha adminlarga Telegram orqali yuboriladi.
type BackupConfig struct {
	Enabled bool
	Hour    int           // yuborish vaqti (Toshkent): soat
	Minute  int           // va daqiqa
	PgDump  string        // pg_dump buyrug'i (PATH dagi nomi yoki to'liq yo'li)
	Timeout time.Duration // bitta nusxa olish uchun maksimal vaqt
}

// backupConfig BACKUP_* sozlamalarini o'qiydi. BACKUP_TIME — "HH:MM".
func backupConfig(p *parser) BackupConfig {
	c := BackupConfig{
		Enabled: p.bool("BACKUP_ENABLED", true),
		Hour:    3,
		PgDump:  p.str("BACKUP_PG_DUMP", "pg_dump"),
		Timeout: p.duration("BACKUP_TIMEOUT", 10*time.Minute),
	}
	if raw := p.str("BACKUP_TIME", ""); raw != "" {
		t, err := time.Parse("15:04", raw)
		if err != nil {
			p.errs = append(p.errs, fmt.Errorf("BACKUP_TIME HH:MM ko'rinishida bo'lishi kerak (masalan 03:00), berilgan: %q", raw))
		} else {
			c.Hour, c.Minute = t.Hour(), t.Minute()
		}
	}
	return c
}

// GeneratorConfig — fayl generatsiyasi (PPTX/DOCX) va vazifalar navbati sozlamalari.
type GeneratorConfig struct {
	TemplatePath      string        // .pptx asosiy paketi (slayd master va mavzu); dizaynlar koddan chiziladi
	OutputDir         string        // vaqtinchalik fayllar papkasi (bo'sh bo'lsa — os.TempDir())
	DocumentHeader    string        // .docx titul varag'ining eng yuqori qatori (bo'sh bo'lsa — chiqarilmaydi)
	MaxConcurrentJobs int           // bir vaqtda bajariladigan generatsiyalar soni
	JobTimeout        time.Duration // bitta generatsiya (AI + PPTX + yuborish) uchun maksimal vaqt
}

// LimitsConfig — foydalanuvchi kiritadigan qiymatlar chegaralari.
type LimitsConfig struct {
	MinSlides int // taqdimot: umumiy slaydlar soni (titul va yakuniy slayd bilan)
	MaxSlides int
	MinPages  int // Word ishlar: umumiy sahifalar soni (titul, reja, adabiyotlar bilan)
	MaxPages  int
}

// PaymentConfig — pullik xizmat sozlamalari: narxlar, to'lov kartasi, chekni
// AI orqali tekshirish va admin paroli. Bepul urinishlar (FREE_PHOTO,
// FREE_PLAIN) hozircha o'chirilgan — barcha ishlar pullik.
type PaymentConfig struct {
	Enabled    bool   // false bo'lsa — barcha ishlar bepul (to'lov so'ralmaydi)
	CardNumber string // to'lov qabul qilinadigan karta raqami (foydalanuvchiga ko'rsatiladi)
	CardHolder string // karta egasi (ixtiyoriy)

	// Taqdimot narxi slaydlar soniga qarab hisoblanadi: slaydlar × bitta slayd narxi.
	PriceSlidePhoto int // rasmli dizayndagi bitta slayd narxi, so'mda
	PriceSlidePlain int // rasmsiz dizayndagi bitta slayd narxi, so'mda
	PriceDocument   int // amaliy topshiriq (Word) narxi, so'mda (qat'iy)
	PricePage       int // mustaqil ish va referat: bitta sahifa narxi, so'mda
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// FreePhoto       int // har bir foydalanuvchiga beriladigan bepul rasmli urinishlar soni
	// FreePlain       int // bepul rasmsiz urinishlar soni

	// Chekni o'qish uchun OpenAI vision modeli. API kaliti va manzili
	// AIConfig dan (OPENAI_API_KEY, OPENAI_BASE_URL) olinadi.
	VisionModel  string
	CheckTimeout time.Duration

	// AdminPassword — botga shu matnni yuborgan foydalanuvchi admin bo'ladi.
	AdminPassword string
}

// CardLast4 karta raqamining oxirgi 4 raqamini qaytaradi.
func (p PaymentConfig) CardLast4() string {
	d := digitsOnly(p.CardNumber)
	if len(d) < 4 {
		return d
	}
	return d[len(d)-4:]
}

// digitsOnly satrdan faqat raqamlarni qoldiradi.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// paymentConfig to'lov sozlamalarini o'qiydi. Vision modeli ko'rsatilmasa,
// matn uchun tanlangan OpenAI modeli ishlatiladi.
func paymentConfig(p *parser, ai AIConfig) PaymentConfig {
	return PaymentConfig{
		Enabled:         p.bool("PAYMENT_ENABLED", true),
		CardNumber:      p.str("PAYMENT_CARD_NUMBER", ""),
		CardHolder:      p.str("PAYMENT_CARD_HOLDER", ""),
		PriceSlidePhoto: p.int("PRICE_SLIDE_PHOTO", 300),
		PriceSlidePlain: p.int("PRICE_SLIDE_PLAIN", 250),
		// Eski .env lar uchun: PRICE_DOCUMENT berilmasa, PRICE_PLAIN olinadi.
		PriceDocument: p.int("PRICE_DOCUMENT", p.int("PRICE_PLAIN", 7000)),
		PricePage:     p.int("PRICE_PAGE", 200),
		// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
		// FreePhoto:     p.int("FREE_PHOTO", 1),
		// FreePlain:     p.int("FREE_PLAIN", 1),
		VisionModel:   p.str("PAYMENT_VISION_MODEL", ai.OpenAIModel),
		CheckTimeout:  p.duration("PAYMENT_CHECK_TIMEOUT", 60*time.Second),
		AdminPassword: p.str("ADMIN_PASSWORD", ""),
	}
}

// validate to'lov sozlamalarini tekshiradi (faqat to'lov yoqilgan bo'lsa).
func (c PaymentConfig) validate() []error {
	if !c.Enabled {
		return nil
	}
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if n := len(digitsOnly(c.CardNumber)); n < 16 || n > 19 {
		add("PAYMENT_ENABLED=true uchun PAYMENT_CARD_NUMBER majburiy (16–19 ta raqam)")
	}
	if c.PriceSlidePhoto <= 0 || c.PriceSlidePlain <= 0 || c.PriceDocument <= 0 || c.PricePage <= 0 {
		add("PRICE_SLIDE_PHOTO, PRICE_SLIDE_PLAIN, PRICE_DOCUMENT va PRICE_PAGE musbat bo'lishi kerak")
	}
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// if c.FreePhoto < 0 || c.FreePlain < 0 {
	// 	add("FREE_PHOTO va FREE_PLAIN manfiy bo'lishi mumkin emas")
	// }
	if utf8.RuneCountInString(c.AdminPassword) < 8 {
		add("PAYMENT_ENABLED=true uchun ADMIN_PASSWORD majburiy (kamida 8 belgi)")
	}
	if c.CheckTimeout <= 0 {
		add("PAYMENT_CHECK_TIMEOUT musbat bo'lishi kerak")
	}
	return errs
}

// Load .env faylini (agar mavjud bo'lsa) yuklaydi, muhit o'zgaruvchilarini o'qiydi
// va konfiguratsiyani tekshiradi. Muhitda allaqachon o'rnatilgan qiymatlar
// .env dagi qiymatlardan ustun turadi.
func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("config: .env faylini o'qib bo'lmadi: %w", err)
	}

	p := &parser{}
	aiCfg := aiConfig(p)
	cfg := &Config{
		Env:      p.str("APP_ENV", "production"),
		LogLevel: p.str("LOG_LEVEL", "info"),
		Telegram: TelegramConfig{
			Token:           p.str("TELEGRAM_BOT_TOKEN", ""),
			PollTimeout:     p.duration("TELEGRAM_POLL_TIMEOUT", 10*time.Second),
			ShutdownTimeout: p.duration("SHUTDOWN_TIMEOUT", 60*time.Second),
		},
		AI:      aiCfg,
		Images:  imagesConfig(p),
		Payment: paymentConfig(p, aiCfg),
		Custom:  customConfig(p),
		Backup:  backupConfig(p),
		DB: DBConfig{
			Host:            p.str("DB_HOST", "localhost"),
			Port:            p.int("DB_PORT", 5432),
			User:            p.str("DB_USER", "postgres"),
			Password:        p.str("DB_PASSWORD", "postgres"),
			Name:            p.str("DB_NAME", "presentation_bot"),
			SSLMode:         p.str("DB_SSLMODE", "disable"),
			MaxOpenConns:    p.int("DB_MAX_OPEN_CONNS", 10),
			MaxIdleConns:    p.int("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: p.duration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
			ConnectRetries:  p.int("DB_CONNECT_RETRIES", 10),
			ConnectDelay:    p.duration("DB_CONNECT_DELAY", 3*time.Second),
		},
		Generator: GeneratorConfig{
			TemplatePath:      p.str("TEMPLATE_PATH", "templates/template.pptx"),
			OutputDir:         p.str("OUTPUT_DIR", ""),
			DocumentHeader:    p.optionalStr("DOCUMENT_HEADER", "O'ZBEKISTON RESPUBLIKASI OLIY TA'LIM, FAN VA INNOVATSIYALAR VAZIRLIGI"),
			MaxConcurrentJobs: p.int("MAX_CONCURRENT_JOBS", 4),
			JobTimeout:        p.duration("JOB_TIMEOUT", 15*time.Minute),
		},
		Limits: LimitsConfig{
			MinSlides: p.int("MIN_SLIDES", 5),
			MaxSlides: p.int("MAX_SLIDES", 50),
			MinPages:  p.int("MIN_PAGES", 5),
			MaxPages:  p.int("MAX_PAGES", 30),
		},
	}

	// Avval formatdagi xatolar (masalan, "abc" son o'rnida), keyin mantiqiy tekshiruvlar.
	if len(p.errs) > 0 {
		return nil, fmt.Errorf("config: %w", errors.Join(p.errs...))
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// aiConfig matn generatsiyasi (AI provayder) sozlamalarini o'qiydi.
func aiConfig(p *parser) AIConfig {
	return AIConfig{
		Provider: strings.ToLower(p.str("AI_PROVIDER", ProviderGemini)),

		GeminiAPIKey: p.str("GEMINI_API_KEY", ""),
		GeminiModel:  p.str("GEMINI_MODEL", "gemini-flash-latest"),

		OpenAIAPIKey:     p.str("OPENAI_API_KEY", ""),
		OpenAIModel:      p.str("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIBaseURL:    strings.TrimRight(p.str("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
		OpenAIStructured: p.bool("AI_OPENAI_STRUCTURED", true),

		DeepSeekAPIKey:   p.str("DEEPSEEK_API_KEY", ""),
		DeepSeekModel:    p.str("DEEPSEEK_MODEL", "deepseek-flash"),
		DeepSeekBaseURL:  strings.TrimRight(p.str("DEEPSEEK_BASE_URL", "https://api.deepseek.com"), "/"),
		DeepSeekThinking: p.bool("DEEPSEEK_THINKING", false),

		Temperature:      p.optionalFloat32("AI_TEMPERATURE"),
		MaxTokens:        p.int("AI_MAX_TOKENS", 0),
		Timeout:          p.duration("AI_TIMEOUT", 120*time.Second),
		MaxRetries:       p.int("AI_MAX_RETRIES", 2),
		ParallelRequests: p.int("AI_PARALLEL_REQUESTS", 3),
		SlidesPerRequest: p.int("AI_SLIDES_PER_REQUEST", 6),
	}
}

// imagesConfig rasm generatsiyasi sozlamalarini o'qiydi.
func imagesConfig(p *parser) ImagesConfig {
	return ImagesConfig{
		StandardProvider: strings.ToLower(p.str("IMAGE_STANDARD_PROVIDER", ImageProviderOpenAI)),
		// Hozircha premium tarif ham OpenAI'da: Gemini rasm modeli billingsiz
		// ishlamaydi. Gemini'ga qaytish uchun .env da IMAGE_PREMIUM_PROVIDER=gemini.
		PremiumProvider: strings.ToLower(p.str("IMAGE_PREMIUM_PROVIDER", ImageProviderOpenAI)),
		DefaultTier:     strings.ToLower(p.str("IMAGE_DEFAULT_TIER", TierStandard)),

		OpenAIModel:   p.str("OPENAI_IMAGE_MODEL", "gpt-image-2"),
		OpenAIQuality: strings.ToLower(p.str("OPENAI_IMAGE_QUALITY", "low")),
		GeminiModel:   p.str("GEMINI_IMAGE_MODEL", "gemini-nano-banana-2.1"),
		GeminiSize:    strings.ToUpper(p.str("GEMINI_IMAGE_SIZE", "")),

		Timeout:  p.duration("IMAGE_TIMEOUT", 90*time.Second),
		Parallel: p.int("IMAGE_PARALLEL_REQUESTS", 4),
		Fallback: p.bool("IMAGE_FALLBACK", true),
	}
}

// LoadImages faqat rasm generatsiyasi uchun kerakli sozlamalarni (AI kalitlari
// va rasm sozlamalari) o'qiydi. Telegram va baza sozlamalarini talab qilmaydi —
// cmd/preview kabi yordamchi vositalar uchun.
func LoadImages() (AIConfig, ImagesConfig, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return AIConfig{}, ImagesConfig{}, fmt.Errorf("config: .env faylini o'qib bo'lmadi: %w", err)
	}
	p := &parser{}
	ai, img := aiConfig(p), imagesConfig(p)
	errs := append(p.errs, img.validate()...)
	if len(errs) > 0 {
		return ai, img, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return ai, img, nil
}

// validate rasm sozlamalarini tekshiradi.
func (c ImagesConfig) validate() []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	for key, v := range map[string]string{"IMAGE_STANDARD_PROVIDER": c.StandardProvider, "IMAGE_PREMIUM_PROVIDER": c.PremiumProvider} {
		switch v {
		case ImageProviderOpenAI, ImageProviderGemini, ImageProviderNone:
		default:
			add("%s noto'g'ri: %q (openai, gemini yoki none bo'lishi kerak)", key, v)
		}
	}
	if c.DefaultTier != TierStandard && c.DefaultTier != TierPremium {
		add("IMAGE_DEFAULT_TIER noto'g'ri: %q (standard yoki premium bo'lishi kerak)", c.DefaultTier)
	}
	switch c.OpenAIQuality {
	case "low", "medium", "high", "auto":
	default:
		add("OPENAI_IMAGE_QUALITY noto'g'ri: %q (low, medium, high yoki auto bo'lishi kerak)", c.OpenAIQuality)
	}
	switch c.GeminiSize {
	case "", "1K", "2K", "4K":
	default:
		add("GEMINI_IMAGE_SIZE noto'g'ri: %q (1K, 2K, 4K yoki bo'sh bo'lishi kerak)", c.GeminiSize)
	}
	if c.Timeout <= 0 {
		add("IMAGE_TIMEOUT musbat bo'lishi kerak")
	}
	if c.Parallel < 1 || c.Parallel > 10 {
		add("IMAGE_PARALLEL_REQUESTS 1 dan 10 gacha bo'lishi kerak")
	}
	return errs
}

// validate qiymatlarning mantiqiy to'g'riligini tekshiradi.
func (c *Config) validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.Telegram.Token == "" {
		add("TELEGRAM_BOT_TOKEN majburiy")
	}

	switch c.AI.Provider {
	case ProviderGemini:
		if c.AI.GeminiAPIKey == "" {
			add("AI_PROVIDER=gemini uchun GEMINI_API_KEY majburiy")
		}
	case ProviderOpenAI:
		if c.AI.OpenAIAPIKey == "" {
			add("AI_PROVIDER=openai uchun OPENAI_API_KEY majburiy")
		}
	case ProviderDeepSeek:
		if c.AI.DeepSeekAPIKey == "" {
			add("AI_PROVIDER=deepseek uchun DEEPSEEK_API_KEY majburiy")
		}
	default:
		add("AI_PROVIDER noto'g'ri: %q (gemini, openai yoki deepseek bo'lishi kerak)", c.AI.Provider)
	}
	if c.AI.MaxRetries < 0 {
		add("AI_MAX_RETRIES manfiy bo'lishi mumkin emas")
	}
	if c.AI.Timeout <= 0 {
		add("AI_TIMEOUT musbat bo'lishi kerak")
	}
	if t := c.AI.Temperature; t != nil && (*t < 0 || *t > 2) {
		add("AI_TEMPERATURE 0 va 2 oralig'ida bo'lishi kerak")
	}
	if c.AI.MaxTokens < 0 {
		add("AI_MAX_TOKENS manfiy bo'lishi mumkin emas")
	}
	if c.AI.ParallelRequests < 1 || c.AI.ParallelRequests > 10 {
		add("AI_PARALLEL_REQUESTS 1 dan 10 gacha bo'lishi kerak")
	}
	if c.AI.SlidesPerRequest < 3 || c.AI.SlidesPerRequest > 10 {
		add("AI_SLIDES_PER_REQUEST 3 dan 10 gacha bo'lishi kerak")
	}

	errs = append(errs, c.Images.validate()...)
	errs = append(errs, c.Payment.validate()...)
	errs = append(errs, c.Custom.validate()...)

	if c.DB.Port <= 0 || c.DB.Port > 65535 {
		add("DB_PORT noto'g'ri: %d", c.DB.Port)
	}
	if c.DB.Name == "" {
		add("DB_NAME majburiy")
	}
	if c.DB.ConnectRetries < 1 {
		add("DB_CONNECT_RETRIES kamida 1 bo'lishi kerak")
	}

	if c.Backup.Enabled && c.Backup.Timeout <= 0 {
		add("BACKUP_TIMEOUT musbat bo'lishi kerak")
	}

	if c.Generator.TemplatePath == "" {
		add("TEMPLATE_PATH majburiy")
	}
	if c.Generator.MaxConcurrentJobs < 1 {
		add("MAX_CONCURRENT_JOBS kamida 1 bo'lishi kerak")
	}
	if c.Generator.JobTimeout <= 0 {
		add("JOB_TIMEOUT musbat bo'lishi kerak")
	}

	errs = append(errs, c.Limits.validate()...)

	return errors.Join(errs...)
}

// validate slayd/sahifa chegaralarining faqat mantiqiy to'g'riligini tekshiradi.
// Yuqori va quyi chegara erkin: generatorlar kichik qiymatda ham ishlaydi
// (taqdimotda kamida 1 ta mazmunli slayd, hujjatda kamida 200 so'z matn).
func (l LimitsConfig) validate() []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if l.MinSlides < 1 {
		add("MIN_SLIDES kamida 1 bo'lishi kerak")
	}
	if l.MaxSlides < l.MinSlides {
		add("MAX_SLIDES (%d) MIN_SLIDES (%d) dan kichik bo'lmasligi kerak", l.MaxSlides, l.MinSlides)
	}
	if l.MinPages < 1 {
		add("MIN_PAGES kamida 1 bo'lishi kerak")
	}
	if l.MaxPages < l.MinPages {
		add("MAX_PAGES (%d) MIN_PAGES (%d) dan kichik bo'lmasligi kerak", l.MaxPages, l.MinPages)
	}
	return errs
}

// parser muhit o'zgaruvchilarini tiplarga o'giradi va format xatolarini yig'adi,
// shunda foydalanuvchi barcha xatolarni bir vaqtda ko'radi.
type parser struct {
	errs []error
}

func (p *parser) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

// optionalStr str kabi, lekin DisabledValue ("-") berilsa bo'sh satr qaytaradi.
func (p *parser) optionalStr(key, def string) string {
	if v := p.str(key, def); v != DisabledValue {
		return v
	}
	return ""
}

func (p *parser) bool(key string, def bool) bool {
	raw := p.str(key, "")
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s true yoki false bo'lishi kerak, berilgan: %q", key, raw))
		return def
	}
	return v
}

func (p *parser) int(key string, def int) int {
	raw := p.str(key, "")
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s butun son bo'lishi kerak, berilgan: %q", key, raw))
		return def
	}
	return v
}

func (p *parser) duration(key string, def time.Duration) time.Duration {
	raw := p.str(key, "")
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s davomiylik bo'lishi kerak (masalan 30s, 5m), berilgan: %q", key, raw))
		return def
	}
	return v
}

func (p *parser) optionalFloat32(key string) *float32 {
	raw := p.str(key, "")
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 32)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s son bo'lishi kerak, berilgan: %q", key, raw))
		return nil
	}
	f := float32(v)
	return &f
}
