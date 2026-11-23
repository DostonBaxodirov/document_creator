// Package bot Telegram bilan ishlash qatlami: buyruqlar, FSM bo'yicha
// marshrutlash, generatsiya vazifalari navbati va xatoliklarni boshqarish.
//
// Handler tashqi qatlamlarga (baza, to'lov, statistika) faqat shu paketda
// e'lon qilingan interfeyslar orqali bog'lanadi — bu ularni testlarda
// almashtirishni osonlashtiradi. Hujjatning o'zini (AI matni, rasmlar,
// PPTX/DOCX) internal/generation yaratadi: Handler sessiyani buyurtmaga
// aylantiradi, jarayonni Telegram xabarida ko'rsatadi va tayyor faylni yuboradi.
//
// Natija fayl turi ish turiga bog'liq: taqdimot — .pptx, mustaqil ish va
// amaliy topshiriq — .docx.
package bot

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generation"
	"telegram-presentation-bot/internal/storage"
)

// Builders — fayl generatorlari.
type Builders struct {
	Slides generation.SlidesBuilder
	Papers generation.PaperBuilder
}

// Options — handler sozlamalari.
type Options struct {
	Limits            config.LimitsConfig
	MaxConcurrentJobs int
	JobTimeout        time.Duration
	// DesignPreview — dizayn mavzulari ko'rinishi rasmi (ixtiyoriy). Fayl
	// bo'lmasa, dizayn tanlash xabari rasmsiz yuboriladi.
	DesignPreview string
	// DocDesignPreview — Word dizaynlari ko'rinishi rasmi (ixtiyoriy).
	DocDesignPreview string
	// Images — AI rasm generatsiyasi (ixtiyoriy). nil yoki provayder ulanmagan
	// bo'lsa, rasmli dizaynlar rasm o'rnida bezakli fon bilan chiqadi.
	Images generation.ImageService

	// Billing — foydalanuvchilar, buyurtmalar va to'lovlar ombori. nil bo'lsa,
	// to'lov va admin imkoniyatlari o'chiriladi.
	Billing BillingStore
	// Verifier — chekni o'qiydigan AI (ixtiyoriy). nil bo'lsa, barcha cheklar
	// admin tekshiruviga yuboriladi.
	Verifier ReceiptVerifier
	// Payment — narxlar, karta va admin paroli (bepul urinishlar hozircha o'chirilgan).
	Payment config.PaymentConfig
	// Referral — do'stlarni taklif qilish tizimi (faqat to'lov yoqilgan bo'lsa ishlaydi).
	Referral config.ReferralConfig
	// Broadcasts — admin e'lonlari ombori (ixtiyoriy). nil bo'lsa, /taklif_elon ishlamaydi.
	Broadcasts BroadcastStore

	// Custom — AI maxsus dizayn tariflari (config.Tariff* → sozlamalar).
	// Bo'sh bo'lsa, "✨ Maxsus dizayn" tugmasi ko'rsatilmaydi.
	Custom map[string]CustomTariff

	// Stats — tashrif buyuruvchilar statistikasi (ixtiyoriy). nil bo'lsa,
	// tashriflar yozilmaydi va /statistika ishlamaydi.
	Stats StatsStore
	// Backup — bazaning kunlik zaxira nusxasi adminlarga (ixtiyoriy).
	Backup BackupOptions
}

const (
	requestTimeout = 10 * time.Second // bitta update uchun baza amallari vaqti
	lockStripes    = 64               // foydalanuvchi qulflari soni (xotira cheklangan bo'lishi uchun)
)

// Handler — Telegram update'larini qayta ishlovchi asosiy struktura.
type Handler struct {
	bot      *tele.Bot
	sessions SessionStore
	gen      *generation.Service // hujjat generatsiyasi (AI, rasmlar, PPTX/DOCX)
	log      *zap.Logger
	opts     Options
	billing  BillingStore
	verifier ReceiptVerifier
	stats    StatsStore
	visits   visitTracker // bugun tashrifi yozilgan foydalanuvchilar
	bcast    BroadcastStore
	bcRuns   sync.Map   // e'lon ID -> *broadcastRun (yuborilayotgan e'lonlar)
	backupMu sync.Mutex // bir vaqtda faqat bitta nusxa olinadi

	jobsCtx context.Context // barcha generatsiyalar uchun ota kontekst (to'xtashda bekor qilinadi)
	slots   chan struct{}   // semafor: bir vaqtdagi generatsiyalar sonini cheklaydi
	wg      sync.WaitGroup  // ishlayotgan generatsiyalarni kutish uchun
	running sync.Map        // userID -> *job

	// previewID — Telegram serveriga bir marta yuklangan dizaynlar rasmi
	// identifikatori (keyingi safar fayl qayta yuklanmaydi).
	previewMu    sync.Mutex
	previewID    string
	docPreviewID string // Word dizaynlari rasmi

	// Bir foydalanuvchining update'lari parallel kelganda (telebot handlerlarni
	// alohida goroutine'larda ishga tushiradi) FSM holati buzilmasligi uchun
	// "o'qish → o'zgartirish → saqlash" ketma-ketligi qulf ostida bajariladi.
	locks [lockStripes]sync.Mutex
}

// NewHandler yangi Handler yaratadi. jobsCtx bekor qilinganda barcha
// ishlayotgan generatsiyalar to'xtatiladi.
func NewHandler(jobsCtx context.Context, sessions SessionStore, aiClient generation.ContentGenerator,
	builders Builders, log *zap.Logger, opts Options) *Handler {
	if opts.MaxConcurrentJobs < 1 {
		opts.MaxConcurrentJobs = 1
	}
	log = log.Named("bot")
	// Admin tekshiruvlari va adminlarga xabarlar ro'yxatni keshdan oladi.
	billing := opts.Billing
	if billing != nil {
		billing = newAdminCache(billing, log, time.Now)
	}
	// Generatsiyaga tariflarning narxsiz qismi beriladi (narxlar — shu paketda).
	tariffs := make(map[string]generation.Tariff, len(opts.Custom))
	for name, t := range opts.Custom {
		tariffs[name] = t.Tariff
	}
	gen := generation.New(generation.Config{
		AI:      aiClient,
		Slides:  builders.Slides,
		Papers:  builders.Papers,
		Images:  opts.Images,
		Tariffs: tariffs,
		Log:     log,
	})
	return &Handler{
		sessions: sessions,
		gen:      gen,
		log:      log,
		opts:     opts,
		billing:  billing,
		verifier: opts.Verifier,
		stats:    opts.Stats,
		bcast:    opts.Broadcasts,
		jobsCtx:  jobsCtx,
		slots:    make(chan struct{}, opts.MaxConcurrentJobs),
	}
}

// Register middleware va barcha marshrutlarni botga ulaydi.
// Middleware'lar Handle dan OLDIN ulanishi shart (telebot talabi).
func (h *Handler) Register(b *tele.Bot) {
	h.bot = b

	b.Use(h.recoverMiddleware, h.privateOnly, h.trackVisits, h.logUpdates)

	b.Handle("/start", h.onStart)
	b.Handle("/cancel", h.onCancel)
	b.Handle("/help", h.onHelp)
	b.Handle("/report", h.onReport)
	b.Handle("/chek", h.onChek)
	b.Handle("/tekshiruv", h.onPendingChecks)
	b.Handle("/statistika", h.onStats)
	b.Handle("/foydalanuvchilar", h.onUsers)
	b.Handle("/backup", h.onBackup)
	b.Handle("/taklif", h.onReferral)
	b.Handle("/taklif_elon", h.onBroadcast)
	b.Handle(tele.OnText, h.onText)
	b.Handle(tele.OnPhoto, h.onPhoto)       // to'lov cheki
	b.Handle(tele.OnDocument, h.onDocument) // fayl ko'rinishidagi chek
	b.Handle(tele.OnMedia, h.onMedia)

	b.Handle(&tele.Btn{Unique: cbWorkType}, h.onWorkType)
	b.Handle(&tele.Btn{Unique: cbLanguage}, h.onLanguage)
	b.Handle(&tele.Btn{Unique: cbDesign}, h.onDesign)
	b.Handle(&tele.Btn{Unique: cbTariff}, h.onTariff)
	b.Handle(&tele.Btn{Unique: cbConfirm}, h.onConfirm)
	b.Handle(&tele.Btn{Unique: cbConfirmBonus}, h.onConfirmBonus)
	b.Handle(&tele.Btn{Unique: cbReferral}, h.onReferral)
	b.Handle(&tele.Btn{Unique: cbBcStart}, h.onBroadcastStart)
	b.Handle(&tele.Btn{Unique: cbBcCancel}, h.onBroadcastCancel)
	b.Handle(&tele.Btn{Unique: cbBcStop}, h.onBroadcastStop)
	b.Handle(&tele.Btn{Unique: cbRestart}, h.onRestart)
	b.Handle(&tele.Btn{Unique: cbCancel}, h.onCancelButton)
	b.Handle(&tele.Btn{Unique: cbPayApprove}, h.onPayApprove)
	b.Handle(&tele.Btn{Unique: cbPayReject}, h.onPayReject)
	b.Handle(&tele.Btn{Unique: cbReportPeriod}, h.onReportPeriod)
	b.Handle(&tele.Btn{Unique: cbReportFormat}, h.onReportFormat)
	b.Handle(&tele.Btn{Unique: cbUsersPeriod}, h.onUsersPeriod)
	b.Handle(&tele.Btn{Unique: cbUsersFormat}, h.onUsersFormat)

	if err := b.SetCommands(h.userCommands()); err != nil {
		h.log.Warn("failed to set bot commands", zap.Error(err))
	}
}

// userCommands oddiy foydalanuvchilar buyruqlar menyusi.
func (h *Handler) userCommands() []tele.Command {
	cmds := []tele.Command{
		{Text: "start", Description: "Yangi ish buyurtma qilish"},
		{Text: "cancel", Description: "Joriy jarayonni bekor qilish"},
		{Text: "help", Description: "Qo'llanma va tariflar"},
	}
	if h.referralOn() {
		cmds = append(cmds, tele.Command{Text: "taklif", Description: "Do'stlarni taklif qilish — bepul hujjat"})
	}
	return cmds
}

// OnError telebot handlerlari qaytargan xatolarni qayta ishlaydi
// (tele.Settings.OnError ga beriladi).
func (h *Handler) OnError(err error, c tele.Context) {
	if c == nil {
		h.log.Error("telebot error", zap.Error(err))
		return
	}
	h.log.Error("handler error", append(userFields(c), zap.Error(err))...)
	if c.Callback() != nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "Xatolik yuz berdi, qayta urinib ko'ring."})
	}
	_ = c.Send(msgInternalError)
}

// Wait ishlayotgan generatsiyalar tugashini timeout bilan kutadi.
// Hammasi tugagan bo'lsa true qaytaradi.
func (h *Handler) Wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// recoverMiddleware handler ichidagi panic'ni ushlaydi: bot qotib qolmaydi,
// xato stack bilan logga yoziladi, foydalanuvchi esa xabardor qilinadi.
func (h *Handler) recoverMiddleware(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				h.log.Error("panic recovered in handler",
					append(userFields(c), zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))...)
				if c.Callback() != nil {
					_ = c.Respond()
				}
				_ = c.Send(msgInternalError)
				err = nil
			}
		}()
		return next(c)
	}
}

// privateOnly bot faqat shaxsiy chatlarda ishlashini ta'minlaydi.
func (h *Handler) privateOnly(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if c.Sender() == nil || c.Chat() == nil || c.Chat().Type != tele.ChatPrivate {
			return nil
		}
		return next(c)
	}
}

// logUpdates har bir update'ni debug darajasida logga yozadi.
func (h *Handler) logUpdates(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if ce := h.log.Check(zap.DebugLevel, "update"); ce != nil {
			fields := userFields(c)
			if cb := c.Callback(); cb != nil {
				fields = append(fields, zap.String("callback", cb.Unique), zap.String("data", c.Data()))
			} else {
				fields = append(fields, zap.String("text", c.Text()))
			}
			ce.Write(fields...)
		}
		return next(c)
	}
}

// ---------------------------------------------------------------------------
// Yordamchi funksiyalar
// ---------------------------------------------------------------------------

// lock foydalanuvchi bo'yicha qulfni oladi va uni ochuvchi funksiyani qaytaradi:
//
//	defer h.lock(userID)()
func (h *Handler) lock(userID int64) func() {
	m := &h.locks[uint64(userID)%lockStripes]
	m.Lock()
	return m.Unlock
}

// loadSession sessiyani oladi va chat/username o'zgargan bo'lsa yangilaydi.
func (h *Handler) loadSession(ctx context.Context, c tele.Context) (*storage.Session, error) {
	u, chat := c.Sender(), c.Chat()
	s, err := h.sessions.GetOrCreate(ctx, u.ID, chat.ID, u.Username)
	if err != nil {
		return nil, err
	}
	s.ChatID, s.Username = chat.ID, u.Username
	return s, nil
}

// cas sessiya holatini atomar ravishda from dan to ga o'tkazadi.
func (h *Handler) cas(ctx context.Context, userID int64, from, to State) (bool, error) {
	return h.sessions.CompareAndSwapState(ctx, userID, string(from), string(to))
}

// listByState berilgan holatdagi sessiyalarni qaytaradi.
func (h *Handler) listByState(ctx context.Context, state State) ([]storage.Session, error) {
	return h.sessions.ListByState(ctx, string(state))
}

func requestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), requestTimeout)
}

func userFields(c tele.Context) []zap.Field {
	if c == nil || c.Sender() == nil {
		return nil
	}
	return []zap.Field{zap.Int64("user_id", c.Sender().ID), zap.String("username", c.Sender().Username)}
}
