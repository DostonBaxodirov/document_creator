// Package generation hujjat generatsiyasi: AI dan matn (maxsus dizaynda —
// dizayn konsepsiyasi ham) va rasmlar olinadi, so'ng tayyor fayl — taqdimot
// (.pptx) yoki Word ish (.docx) — yaratiladi.
//
// Paket Telegram'ga bog'liq emas: jarayon holati Progress interfeysi orqali
// ko'rsatiladi, natija esa Output ko'rinishida qaytariladi — faylni
// foydalanuvchiga yuborish chaqiruvchining (internal/bot) vazifasi.
//
// Service tashqi qatlamlarga (AI, PPTX/DOCX generatorlari, rasm xizmati)
// faqat shu paketda e'lon qilingan interfeyslar orqali bog'lanadi — bu ularni
// testlarda almashtirishni osonlashtiradi.
package generation

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
)

// ContentGenerator — matnni yaratuvchi AI servis.
type ContentGenerator interface {
	GeneratePresentation(ctx context.Context, req ai.PresentationRequest) (*ai.Presentation, error)
	GenerateDocument(ctx context.Context, req ai.DocumentRequest) (*ai.Document, error)
	GenerateDesign(ctx context.Context, req ai.DesignRequest) (*ai.Design, error)
	GenerateDocDesign(ctx context.Context, req ai.DesignRequest) (*ai.DocDesign, error)
}

// SlidesBuilder — taqdimot (.pptx) faylini yaratuvchi generator.
type SlidesBuilder interface {
	Build(ctx context.Context, deck generator.Deck) (string, error)
	// ContentSlides umumiy slaydlar sonidan nechtasini AI yozishini qaytaradi.
	ContentSlides(total int) int
}

// PaperBuilder — Word ish (mustaqil ish, referat, amaliy topshiriq) .docx
// faylini yaratuvchi generator.
type PaperBuilder interface {
	Build(ctx context.Context, paper generator.Paper) (string, error)
	// ContentWords sahifalar sonidan asosiy matnning taxminiy hajmini (so'zlarda) hisoblaydi.
	ContentWords(pages int) int
	// FigureWords bitta rasm matn o'rnida egallaydigan joy (so'zlarda).
	FigureWords() int
	// EstimatePages tayyor hujjatning taxminiy sahifalar soni.
	EstimatePages(paper generator.Paper) int
}

// ImageService — rasmli dizaynlar uchun AI rasm generatsiyasi (internal/images).
type ImageService interface {
	// Available rasm provayderi ulanganini bildiradi.
	Available() bool
	// Generate rasmlarni chizadi; natijalar so'rovlar tartibida, chizilmagan
	// rasm o'rnida Data nil bo'ladi.
	Generate(ctx context.Context, reqs []images.Request, onDone func(done, total int)) []images.Result
}

const (
	pptxMIME = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	docxMIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

// ErrAI AI bosqichidagi xatolarni belgilaydi (foydalanuvchiga mos xabar tanlash uchun).
var ErrAI = errors.New("ai")

// Order — generatsiya buyurtmasi: foydalanuvchi to'ldirgan ma'lumotlar.
type Order struct {
	WorkType ai.WorkType
	Topic    string
	Language string // lang kodi; bo'sh — o'zbekcha (lotin)
	// Requirements — foydalanuvchining qo'shimcha talablari (AI ga to'liq yuboriladi).
	Requirements string
	// Design — tayyor dizayn ID si yoki generator.ThemeCustom (maxsus dizayn).
	Design string
	// Tariff — maxsus dizayn tarifi (Service tariflaridan biri).
	Tariff                               string
	StudentName, University, TeacherName string
	// Volume — slaydlar (taqdimotda) yoki sahifalar (Word ishda) soni.
	Volume int
}

// custom buyurtma maxsus dizaynda ekanini bildiradi.
func (o Order) custom() bool { return IsCustom(o.WorkType, o.Design) }

// IsCustom buyurtma maxsus dizaynda (taqdimot yoki mustaqil ish/referat)
// ekanini bildiradi.
func IsCustom(w ai.WorkType, design string) bool {
	return design == generator.ThemeCustom && (!w.IsDocument() || w.HasDesigns())
}

// Output — tayyor fayl va uni foydalanuvchiga yuborish uchun ma'lumotlar.
type Output struct {
	Path     string // vaqtinchalik fayl (yuborilgach chaqiruvchi o'chiradi)
	FileName string // foydalanuvchiga ko'rinadigan fayl nomi
	MIME     string
	Caption  string // fayl izohi (HTML)
	Summary  string // admin uchun natija: "20 ta slayd · 🖼 19/19 rasm (...)"
	// LogFields — natija haqida log maydonlari (slaydlar, rasmlar soni va h.k.).
	LogFields []zap.Field
}

// Tariff — AI maxsus dizayn tarifining generatsiya sozlamalari (narxlar —
// bot va config da).
type Tariff struct {
	// AI — dizayn konsepsiyasi va matnni yozadigan model.
	AI ContentGenerator
	// Images — tarif sifatidagi rasm xizmati. nil bo'lsa — rasmlarsiz
	// (dizayn rasm o'rnida bezakli fon bilan chiqadi).
	Images ImageService
	// ImagePercent — rasmlar soni: taqdimotda slaydlar sonining (titul rasmi
	// bilan), Word ishda boblar sonining necha foizi.
	ImagePercent int
	// Admin xabari va loglar uchun.
	TextModel, ImageModel, ImageQuality string
}

// Config — Service sozlamalari.
type Config struct {
	// AI — tayyor dizayndagi ishlar matnini yozadigan model.
	AI     ContentGenerator
	Slides SlidesBuilder
	Papers PaperBuilder
	// Images — tayyor rasmli dizaynlar uchun rasm xizmati (ixtiyoriy). nil yoki
	// provayder ulanmagan bo'lsa, rasmli dizaynlar rasm o'rnida bezakli fon bilan chiqadi.
	Images ImageService
	// Tariffs — AI maxsus dizayn tariflari (nomi bo'yicha).
	Tariffs map[string]Tariff
	// Log — WithLogger bilan logger berilmagan Build chaqiruvlari uchun (nil — loglarsiz).
	Log *zap.Logger
}

// Service — buyurtma bo'yicha taqdimot yoki Word ish faylini yaratadi.
type Service struct {
	ai      ContentGenerator
	slides  SlidesBuilder
	papers  PaperBuilder
	images  ImageService
	tariffs map[string]Tariff
	log     *zap.Logger
}

// New sozlamalar bo'yicha Service yaratadi.
func New(cfg Config) *Service {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{
		ai:      cfg.AI,
		slides:  cfg.Slides,
		papers:  cfg.Papers,
		images:  cfg.Images,
		tariffs: cfg.Tariffs,
		log:     log,
	}
}

// loggerKey — Build loggerining kontekstdagi kaliti.
type loggerKey struct{}

// WithLogger ctx ga Build loglari uchun logger qo'shadi (masalan, foydalanuvchi
// va buyurtma maydonlari bilan). Logger berilmasa — Config.Log.
func WithLogger(ctx context.Context, log *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, log)
}

// logger Build loglari uchun logger: kontekstdagisi yoki Service'niki.
func (s *Service) logger(ctx context.Context) *zap.Logger {
	if log, ok := ctx.Value(loggerKey{}).(*zap.Logger); ok && log != nil {
		return log
	}
	return s.log
}

// Build buyurtma bo'yicha faylni yaratadi: taqdimot — .pptx, mustaqil ish,
// referat va amaliy topshiriq — .docx. Bosqichlar holati p orqali
// ko'rsatiladi. AI xatolari ErrAI bilan belgilanadi.
func (s *Service) Build(ctx context.Context, o Order, p Progress) (*Output, error) {
	if _, ok := workTypeNames[o.WorkType]; !ok {
		return nil, fmt.Errorf("noma'lum ish turi: %q", o.WorkType)
	}
	log := s.logger(ctx)
	if o.WorkType.IsDocument() {
		return s.buildPaper(ctx, p, o, log)
	}
	return s.buildSlides(ctx, p, o, log)
}

// PhotosAvailable tayyor rasmli dizaynlar uchun rasm provayderi ulanganini
// bildiradi (ulanmagan bo'lsa, rasmli dizayn ham rasmsiz chiqadi).
func (s *Service) PhotosAvailable() bool {
	return s.images != nil && s.images.Available()
}

// CustomImages maxsus dizayndagi buyurtmaga tarif bo'yicha chiziladigan
// rasmlar soni: taqdimotda — slaydlar sonining tarif foizi (titul rasmi
// bilan), Word ishda — boblar sonining tarif foizi (rasm xizmati ulanmagan
// bo'lsa — 0). Noma'lum tarif — 0.
func (s *Service) CustomImages(w ai.WorkType, volume int, tariff string) int {
	t, ok := s.tariffs[tariff]
	if !ok {
		return 0
	}
	if !w.IsDocument() {
		return customImageCount(volume, t.ImagePercent)
	}
	if !s.docImagesOn(t) {
		return 0
	}
	_, n := docPlan(s.papers.ContentWords(volume), s.papers.FigureWords(), t.ImagePercent)
	return n
}
