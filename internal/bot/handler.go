// Package bot Telegram bilan ishlash qatlami: buyruqlar, FSM bo'yicha
// marshrutlash, generatsiya vazifalari navbati va xatoliklarni boshqarish.
//
// Handler tashqi qatlamlarga (baza, AI, PPTX/DOCX generatorlari) faqat shu
// paketda e'lon qilingan interfeyslar orqali bog'lanadi — bu ularni testlarda
// almashtirishni osonlashtiradi.
//
// Natija fayl turi ish turiga bog'liq: taqdimot — .pptx, mustaqil ish va
// amaliy topshiriq — .docx.
package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/lang"
	"telegram-presentation-bot/internal/storage"
)

// SessionStore — foydalanuvchi sessiyalari ombori.
type SessionStore interface {
	GetOrCreate(ctx context.Context, userID, chatID int64, username string) (*storage.Session, error)
	Get(ctx context.Context, userID int64) (*storage.Session, error)
	Save(ctx context.Context, s *storage.Session) error
	CompareAndSwapState(ctx context.Context, userID int64, from, to string) (bool, error)
	ListByState(ctx context.Context, state string) ([]storage.Session, error)
}

// ContentGenerator — matnni yaratuvchi AI servis.
type ContentGenerator interface {
	GeneratePresentation(ctx context.Context, req ai.PresentationRequest) (*ai.Presentation, error)
	GenerateDocument(ctx context.Context, req ai.DocumentRequest) (*ai.Document, error)
	GenerateDesign(ctx context.Context, req ai.DesignRequest) (*ai.Design, error)
}

// SlidesBuilder — taqdimot (.pptx) faylini yaratuvchi generator.
type SlidesBuilder interface {
	Build(ctx context.Context, deck generator.Deck) (string, error)
	// ContentSlides umumiy slaydlar sonidan nechtasini AI yozishini qaytaradi.
	ContentSlides(total int) int
}

// PaperBuilder — mustaqil ish / amaliy topshiriq (.docx) faylini yaratuvchi generator.
type PaperBuilder interface {
	Build(ctx context.Context, paper generator.Paper) (string, error)
	// ContentWords sahifalar sonidan asosiy matnning taxminiy hajmini (so'zlarda) hisoblaydi.
	ContentWords(pages int) int
}

// ImageService — rasmli dizaynlar uchun AI rasm generatsiyasi (internal/images).
type ImageService interface {
	// Available kamida bitta rasm provayderi ulanganini bildiradi.
	Available() bool
	// Generate rasmlarni tarif provayderi bilan chizadi; natijalar so'rovlar
	// tartibida, chizilmagan rasm o'rnida Data nil bo'ladi.
	Generate(ctx context.Context, tier images.Tier, reqs []images.Request, onDone func(done, total int)) []images.Result
}

// Builders — fayl generatorlari.
type Builders struct {
	Slides SlidesBuilder
	Papers PaperBuilder
}

// Options — handler sozlamalari.
type Options struct {
	Limits            Limits
	MaxConcurrentJobs int
	JobTimeout        time.Duration
	// DesignPreview — dizayn mavzulari ko'rinishi rasmi (ixtiyoriy). Fayl
	// bo'lmasa, dizayn tanlash xabari rasmsiz yuboriladi.
	DesignPreview string
	// Images — AI rasm generatsiyasi (ixtiyoriy). nil yoki provayder ulanmagan
	// bo'lsa, rasmli dizaynlar rasm o'rnida bezakli fon bilan chiqadi.
	Images ImageService
	// DefaultImageTier — tarifi (Session.Plan) belgilanmagan foydalanuvchilar
	// uchun rasm tarifi.
	DefaultImageTier images.Tier

	// Billing — foydalanuvchilar, buyurtmalar va to'lovlar ombori. nil bo'lsa,
	// to'lov va admin imkoniyatlari o'chiriladi.
	Billing BillingStore
	// Verifier — chekni o'qiydigan AI (ixtiyoriy). nil bo'lsa, barcha cheklar
	// admin tekshiruviga yuboriladi.
	Verifier ReceiptVerifier
	// Payment — narxlar, bepul urinishlar, karta va admin paroli.
	Payment PaymentOptions

	// Custom — AI maxsus dizayn tariflari (config.Tariff* → sozlamalar).
	// Bo'sh bo'lsa, "✨ AI maxsus dizayn" tugmasi ko'rsatilmaydi.
	Custom map[string]CustomTariff
}

const (
	pptxMIME       = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	docxMIME       = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	requestTimeout = 10 * time.Second // bitta update uchun baza amallari vaqti
	lockStripes    = 64               // foydalanuvchi qulflari soni (xotira cheklangan bo'lishi uchun)
)

// Foydalanuvchiga yuboriladigan matnlar.
const (
	msgWelcome = "👋 Assalomu alaykum, <b>%s</b>!\n\n" +
		"Men sun'iy intellekt yordamida tayyorlab beraman:\n" +
		"📊 <b>taqdimot</b> — PowerPoint (.pptx): tayyor dizaynlarda yoki ✨ AI mavzungizga mos yaratgan maxsus dizaynda\n" +
		"📚 <b>mustaqil ish</b> va 🛠 <b>amaliy topshiriq</b> — Word (.docx)\n\n" +
		"Bir nechta savolga javob bering — qolganini o'zim qilaman ✨"
	msgChooseWorkType = "📌 <b>Ish turini tanlang:</b>"
	msgPressButton    = "👇 Iltimos, quyidagi tugmalardan birini tanlang."
	msgIdle           = "Yangi ish buyurtma qilish uchun /start buyrug'ini bosing."
	msgBusy           = "⏳ Ishingiz tayyorlanmoqda, biroz kuting. To'xtatish uchun — /cancel"
	msgUnknownCommand = "🤔 Noma'lum buyruq. Mavjud buyruqlar: /start, /cancel, /help"
	msgTextOnly       = "✍️ Iltimos, javobni matn ko'rinishida yuboring."
	msgInternalError  = "😔 Kutilmagan xatolik yuz berdi. Iltimos, birozdan so'ng qayta urinib ko'ring yoki /start bosing."
	msgStaleButton    = "Bu tugma eskirgan. Yangi buyurtma uchun /start bosing."
	msgCancelled      = "❌ Bekor qilindi. Yangi ish uchun /start bosing."
	msgNothingCancel  = "Bekor qiladigan jarayon yo'q. Yangi ish uchun /start bosing."
	msgStopping       = "⏹ Generatsiya to'xtatilmoqda..."
	msgQueued         = "🕒 Hozir so'rovlar ko'p — siz navbatdasiz. Ishingiz tez orada boshlanadi."
	msgGeneratingDeck = "⏳ <b>Tayyorlanmoqda...</b> Odatda 1–2 daqiqa vaqt oladi."
	msgGeneratingDoc  = "⏳ <b>Tayyorlanmoqda...</b> Hujjat bo'limma-bo'lim yoziladi, odatda 2–5 daqiqa vaqt oladi."
	msgDone           = "✅ <b>Tayyor!</b> Omad tilayman 🎓\n\nYana ish kerak bo'lsa — /start"
	msgJobCancelled   = "⏹ Generatsiya bekor qilindi. Yangi ish uchun /start bosing."
	msgInterrupted    = "⚠️ Bot qayta ishga tushirildi va ishingiz yakunlanmay qoldi.\n" +
		"Ma'lumotlaringiz saqlangan — «Qayta urinish» tugmasini bosing."
	msgDesignHint = "👇 Iltimos, quyidagi tugmalardan biri bilan dizaynni tanlang."
	msgChooseLang = "🌐 <b>Ish qaysi tilda yozilsin?</b>\n\n<i>Matn, titul varaq va sarlavhalar shu tilda bo'ladi.</i>"
)

// errAI AI bosqichidagi xatolarni belgilaydi (foydalanuvchiga mos xabar tanlash uchun).
var errAI = errors.New("ai")

// job — ishlayotgan generatsiya vazifasi (foydalanuvchi /cancel bilan to'xtatishi uchun).
type job struct {
	cancel context.CancelFunc
	byUser atomic.Bool
}

// Handler — Telegram update'larini qayta ishlovchi asosiy struktura.
type Handler struct {
	bot      *tele.Bot
	sessions SessionStore
	ai       ContentGenerator
	builders Builders
	log      *zap.Logger
	opts     Options
	billing  BillingStore
	verifier ReceiptVerifier

	jobsCtx context.Context // barcha generatsiyalar uchun ota kontekst (to'xtashda bekor qilinadi)
	slots   chan struct{}   // semafor: bir vaqtdagi generatsiyalar sonini cheklaydi
	wg      sync.WaitGroup  // ishlayotgan generatsiyalarni kutish uchun
	running sync.Map        // userID -> *job

	// previewID — Telegram serveriga bir marta yuklangan dizaynlar rasmi
	// identifikatori (keyingi safar fayl qayta yuklanmaydi).
	previewMu sync.Mutex
	previewID string

	// Bir foydalanuvchining update'lari parallel kelganda (telebot handlerlarni
	// alohida goroutine'larda ishga tushiradi) FSM holati buzilmasligi uchun
	// "o'qish → o'zgartirish → saqlash" ketma-ketligi qulf ostida bajariladi.
	locks [lockStripes]sync.Mutex
}

// NewHandler yangi Handler yaratadi. jobsCtx bekor qilinganda barcha
// ishlayotgan generatsiyalar to'xtatiladi.
func NewHandler(jobsCtx context.Context, sessions SessionStore, aiClient ContentGenerator,
	builders Builders, log *zap.Logger, opts Options) *Handler {
	if opts.MaxConcurrentJobs < 1 {
		opts.MaxConcurrentJobs = 1
	}
	return &Handler{
		sessions: sessions,
		ai:       aiClient,
		builders: builders,
		log:      log.Named("bot"),
		opts:     opts,
		billing:  opts.Billing,
		verifier: opts.Verifier,
		jobsCtx:  jobsCtx,
		slots:    make(chan struct{}, opts.MaxConcurrentJobs),
	}
}

// Register middleware va barcha marshrutlarni botga ulaydi.
// Middleware'lar Handle dan OLDIN ulanishi shart (telebot talabi).
func (h *Handler) Register(b *tele.Bot) {
	h.bot = b

	b.Use(h.recoverMiddleware, h.privateOnly, h.logUpdates)

	b.Handle("/start", h.onStart)
	b.Handle("/cancel", h.onCancel)
	b.Handle("/help", h.onHelp)
	b.Handle("/report", h.onReport)
	b.Handle("/chek", h.onChek)
	b.Handle("/tekshiruv", h.onPendingChecks)
	b.Handle(tele.OnText, h.onText)
	b.Handle(tele.OnPhoto, h.onPhoto)       // to'lov cheki
	b.Handle(tele.OnDocument, h.onDocument) // fayl ko'rinishidagi chek
	b.Handle(tele.OnMedia, h.onMedia)

	b.Handle(&tele.Btn{Unique: cbWorkType}, h.onWorkType)
	b.Handle(&tele.Btn{Unique: cbLanguage}, h.onLanguage)
	b.Handle(&tele.Btn{Unique: cbDesign}, h.onDesign)
	b.Handle(&tele.Btn{Unique: cbTariff}, h.onTariff)
	b.Handle(&tele.Btn{Unique: cbConfirm}, h.onConfirm)
	b.Handle(&tele.Btn{Unique: cbRestart}, h.onRestart)
	b.Handle(&tele.Btn{Unique: cbCancel}, h.onCancelButton)
	b.Handle(&tele.Btn{Unique: cbPayApprove}, h.onPayApprove)
	b.Handle(&tele.Btn{Unique: cbPayReject}, h.onPayReject)
	b.Handle(&tele.Btn{Unique: cbReportPeriod}, h.onReportPeriod)
	b.Handle(&tele.Btn{Unique: cbReportFormat}, h.onReportFormat)

	if err := b.SetCommands([]tele.Command{
		{Text: "start", Description: "Yangi ish buyurtma qilish"},
		{Text: "cancel", Description: "Joriy jarayonni bekor qilish"},
		{Text: "help", Description: "Qo'llanma va tariflar"},
	}); err != nil {
		h.log.Warn("failed to set bot commands", zap.Error(err))
	}
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

// RecoverInterrupted bot to'satdan to'xtab qolgan (crash, deploy) paytda
// "generating" holatida qolib ketgan sessiyalarni tasdiqlash holatiga
// qaytaradi va foydalanuvchilarga qayta urinish tugmasini yuboradi.
func (h *Handler) RecoverInterrupted(ctx context.Context) {
	list, err := h.sessions.ListByState(ctx, string(StateGenerating))
	if err != nil {
		h.log.Error("failed to list interrupted jobs", zap.Error(err))
		return
	}
	recovered := 0
	for _, s := range list {
		ok, err := h.sessions.CompareAndSwapState(ctx, s.UserID, string(StateGenerating), string(StateConfirm))
		if err != nil || !ok {
			continue
		}
		recovered++
		if _, err := h.bot.Send(&tele.Chat{ID: s.ChatID}, msgInterrupted, retryKeyboard()); err != nil {
			h.log.Warn("failed to notify user about interrupted job", zap.Int64("user_id", s.UserID), zap.Error(err))
		}
	}
	if recovered > 0 {
		h.log.Info("recovered interrupted jobs", zap.Int("count", recovered))
	}
	h.recoverReviews(ctx)
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
// Buyruqlar
// ---------------------------------------------------------------------------

// onStart yangi buyurtmani boshlaydi.
func (h *Handler) onStart(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	switch State(s.State) {
	case StateGenerating:
		return c.Send(msgBusy)
	case StateReview:
		return h.replyInReview(ctx, c, s)
	case StatePayment:
		h.cancelPendingOrder(ctx, s)
	}
	if err := h.beginFlow(ctx, s); err != nil {
		return err
	}

	name := c.Sender().FirstName
	if name == "" {
		name = "do'stim"
	}
	// Birinchi /start da — to'liq qo'llanma (foydalanish tartibi va tariflar),
	// keyingilarida — qisqa narxlar ro'yxati.
	first := s.GuideSentAt == nil
	welcome := fmt.Sprintf(msgWelcome, html.EscapeString(name))
	if !first {
		welcome += h.pricingText()
	}
	if err := c.Send(welcome, removeKeyboard()); err != nil {
		return err
	}
	if first {
		h.sendGuideOnce(ctx, c, s)
	}
	return c.Send(msgChooseWorkType, workTypeKeyboard())
}

// onHelp to'liq qo'llanmani (foydalanish tartibi va tariflar) yuboradi.
func (h *Handler) onHelp(c tele.Context) error {
	return c.Send(h.guideText())
}

// onCancel /cancel buyrug'i.
func (h *Handler) onCancel(c tele.Context) error {
	return c.Send(h.cancelFlow(c), removeKeyboard())
}

// onCancelButton "❌ Bekor qilish" inline tugmasi.
func (h *Handler) onCancelButton(c tele.Context) error {
	_ = c.Respond()
	h.removeButtons(c)
	return c.Send(h.cancelFlow(c), removeKeyboard())
}

// cancelFlow joriy jarayonni bekor qiladi va foydalanuvchiga javob matnini qaytaradi.
func (h *Handler) cancelFlow(c tele.Context) string {
	userID := c.Sender().ID
	defer h.lock(userID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		h.log.Error("cancel: failed to load session", zap.Int64("user_id", userID), zap.Error(err))
		return msgInternalError
	}

	switch State(s.State) {
	case StateGenerating:
		// Generatsiya o'z goroutine'ida ishlaydi: uni kontekst orqali to'xtatamiz,
		// holatni esa runJob o'zi "idle" ga o'tkazadi.
		if v, ok := h.running.Load(userID); ok {
			j := v.(*job)
			j.byUser.Store(true)
			j.cancel()
			return msgStopping
		}
		// Vazifa topilmadi (masalan, jarayon to'satdan to'xtagan) — holatni tiklaymiz.
	case StateIdle, "":
		return msgNothingCancel
	case StateReview:
		// Pul allaqachon o'tkazilgan — chek tekshiruvi tugashini kutamiz.
		return msgReviewWait
	case StatePayment:
		h.cancelPendingOrder(ctx, s)
	}

	resetDraft(s)
	s.State = string(StateIdle)
	if err := h.sessions.Save(ctx, s); err != nil {
		h.log.Error("cancel: failed to save session", zap.Int64("user_id", userID), zap.Error(err))
		return msgInternalError
	}
	return msgCancelled
}

// ---------------------------------------------------------------------------
// Matnli xabarlar (FSM router)
// ---------------------------------------------------------------------------

// onText foydalanuvchi holatiga qarab matnni tegishli qadamga yo'naltiradi.
func (h *Handler) onText(c tele.Context) error {
	text := c.Text()
	// Admin paroli istalgan holatda qabul qilinadi.
	if ok, err := h.tryAdminLogin(c, text); ok {
		return err
	}
	if strings.HasPrefix(text, "/chek_") { // hisobotdagi bosiladigan havola: /chek_12
		return h.showCheck(c, strings.TrimPrefix(text, "/chek_"))
	}
	if strings.HasPrefix(text, "/") {
		return c.Send(msgUnknownCommand)
	}

	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}

	state := State(s.State)
	switch state {
	case StateIdle, "":
		return c.Send(msgIdle, removeKeyboard())
	case StateWorkType:
		return c.Send(msgPressButton, workTypeKeyboard())
	case StateLanguage:
		return c.Send(msgPressButton, languageKeyboard())
	case StateDesign:
		return c.Send(msgDesignHint, designKeyboard(h.customOn()))
	case StateTariff:
		return c.Send(msgTariffHint, h.tariffKeyboard(s))
	case StateConfirm:
		return h.sendSummary(c, s)
	case StatePayment:
		return h.sendPaymentInfo(ctx, c, s)
	case StateReview:
		return h.replyInReview(ctx, c, s)
	case StateGenerating:
		return c.Send(msgBusy)
	}

	st, ok := steps[state]
	if !ok {
		// Bazada noma'lum holat (masalan, eski versiyadan qolgan) — xavfsiz tiklash.
		h.log.Warn("unknown session state, resetting", zap.Int64("user_id", s.UserID), zap.String("state", s.State))
		resetDraft(s)
		s.State = string(StateIdle)
		if err := h.sessions.Save(ctx, s); err != nil {
			return err
		}
		return c.Send(msgIdle, removeKeyboard())
	}

	if err := st.apply(h.opts.Limits, s, text); err != nil {
		var ie *inputError
		if errors.As(err, &ie) {
			return c.Send("⚠️ " + html.EscapeString(ie.msg))
		}
		return err
	}

	next := st.nextState(s)
	s.State = string(next)
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}

	switch next {
	case StateConfirm:
		if err := c.Send("👌 Ma'lumotlar qabul qilindi.", removeKeyboard()); err != nil {
			return err
		}
		return h.sendSummary(c, s)
	case StateDesign:
		if err := c.Send(fmt.Sprintf("👌 Slaydlar soni: <b>%d</b>", s.Volume), removeKeyboard()); err != nil {
			return err
		}
		return h.askDesign(c)
	}
	return h.ask(c, s, next)
}

// onMedia rasm, stiker va boshqa media xabarlarga javob beradi.
func (h *Handler) onMedia(c tele.Context) error {
	return c.Send(msgTextOnly)
}

// ---------------------------------------------------------------------------
// Inline tugmalar
// ---------------------------------------------------------------------------

// onWorkType ish turi tanlanganda chaqiriladi.
func (h *Handler) onWorkType(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	if State(s.State) != StateWorkType {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}

	opt, ok := findWorkType(ai.WorkType(c.Data()))
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}

	s.WorkType = string(opt.Type)
	s.State = string(StateLanguage)
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}

	_ = c.Respond(&tele.CallbackResponse{Text: opt.Name})
	h.edit(c, fmt.Sprintf("✅ Ish turi: <b>%s</b>", html.EscapeString(opt.Name)))
	return c.Send(msgChooseLang, languageKeyboard())
}

// onLanguage ish tili tanlanganda chaqiriladi.
func (h *Handler) onLanguage(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	if State(s.State) != StateLanguage {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	l, ok := lang.Find(c.Data())
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}

	s.Language = string(l.Code)
	s.State = string(StateTopic)
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}

	_ = c.Respond(&tele.CallbackResponse{Text: l.Name})
	h.edit(c, fmt.Sprintf("🌐 Til: <b>%s</b>", html.EscapeString(l.Name)))
	return h.ask(c, s, StateTopic)
}

// onDesign taqdimot dizayni tanlanganda chaqiriladi.
func (h *Handler) onDesign(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	if State(s.State) != StateDesign {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	if c.Data() == generator.ThemeCustom {
		if !h.customOn() {
			return c.Respond(&tele.CallbackResponse{Text: msgNoCustomCfg, ShowAlert: true})
		}
		s.Design, s.Tariff = generator.ThemeCustom, ""
		s.State = string(StateTariff)
		if err := h.sessions.Save(ctx, s); err != nil {
			return err
		}
		_ = c.Respond(&tele.CallbackResponse{Text: customLabel})
		h.editPrompt(c, "🎨 Dizayn: <b>"+customLabel+"</b>")
		return h.askTariff(c, s)
	}
	theme, ok := generator.FindTheme(c.Data())
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}

	s.Design = theme.ID
	s.State = string(StateStudent)
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}

	_ = c.Respond(&tele.CallbackResponse{Text: theme.Name})
	h.editPrompt(c, fmt.Sprintf("🎨 Dizayn: <b>%s %s</b>", theme.Emoji, html.EscapeString(theme.Name)))
	return h.ask(c, s, StateStudent)
}

// askDesign dizayn tanlash xabarini yuboradi: dizaynlar ko'rinishi rasmi
// (bo'lsa) va har bir dizayn uchun tugma.
func (h *Handler) askDesign(c tele.Context) error {
	text := designPrompt(h.customOn())
	keyboard := designKeyboard(h.customOn())
	h.previewMu.Lock()
	cached := h.previewID
	h.previewMu.Unlock()

	var file tele.File
	switch {
	case cached != "":
		file = tele.File{FileID: cached}
	case h.opts.DesignPreview != "" && fileExists(h.opts.DesignPreview):
		file = tele.FromDisk(h.opts.DesignPreview)
	default:
		return c.Send(text, keyboard)
	}

	// Tarmoq so'rovi qulfsiz bajariladi: birinchi yuklashlar bir vaqtga to'g'ri
	// kelsa, rasm ikki marta yuklanishi mumkin — bu zararsiz.
	msg, err := h.bot.Send(c.Recipient(), &tele.Photo{File: file, Caption: text}, keyboard)
	h.previewMu.Lock()
	defer h.previewMu.Unlock()
	if err != nil {
		// Rasmni yuborib bo'lmasa (masalan, file_id eskirgan) — rasmsiz davom etamiz.
		h.log.Warn("failed to send design preview", zap.Error(err))
		if h.previewID == cached {
			h.previewID = ""
		}
		return c.Send(text, keyboard)
	}
	if msg != nil && msg.Photo != nil && h.previewID == "" {
		h.previewID = msg.Photo.FileID
	}
	return nil
}

// designPrompt dizayn tanlash xabari matni (custom — AI maxsus dizayn yoqilgan).
// Rasm izohi bo'lib yuboriladi — Telegram chegarasi 1024 belgi.
func designPrompt(custom bool) string {
	var b strings.Builder
	b.WriteString("🎨 <b>Taqdimot dizaynini tanlang:</b>\n\n")
	for i, t := range generator.Themes() {
		photo := ""
		if t.Photos {
			photo = " 📷"
		}
		fmt.Fprintf(&b, "%d. %s <b>%s</b>%s — %s\n", i+1, t.Emoji, html.EscapeString(t.Name), photo, html.EscapeString(t.Description))
	}
	b.WriteString("\n📷 — mavzuga mos AI rasmlar bilan.\n")
	if custom {
		b.WriteString("✨ <b>AI maxsus dizayn</b> — AI mavzungizga mos noyob dizaynni noldan yaratadi (3 ta tarif).")
	} else {
		b.WriteString("<i>Har bir dizaynning o'z tuzilishi bor: vaqt chizig'i, piramida, SWOT, sikl, sxema va boshqa maketlar.</i>")
	}
	return b.String()
}

// onRestart "Qaytadan to'ldirish" tugmasi — buyurtmani boshidan boshlaydi.
func (h *Handler) onRestart(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	switch State(s.State) {
	case StateGenerating:
		return c.Respond(&tele.CallbackResponse{Text: "Ishingiz allaqachon tayyorlanmoqda."})
	case StateReview:
		return c.Respond(&tele.CallbackResponse{Text: "Chekingiz tekshirilmoqda."})
	case StatePayment:
		h.cancelPendingOrder(ctx, s)
	}
	if err := h.beginFlow(ctx, s); err != nil {
		return err
	}

	_ = c.Respond()
	h.removeButtons(c)
	return c.Send(msgChooseWorkType, workTypeKeyboard())
}

// onConfirm ma'lumotlar tasdiqlanganda (yoki "Qayta urinish" bosilganda) generatsiyani boshlaydi.
func (h *Handler) onConfirm(c tele.Context) error {
	userID := c.Sender().ID
	ctx, cancel := requestContext()
	defer cancel()

	// Atomar o'tish: tugma ikki marta bosilsa ham generatsiya faqat bir marta boshlanadi.
	ok, err := h.sessions.CompareAndSwapState(ctx, userID, string(StateConfirm), string(StateGenerating))
	if err != nil {
		return err
	}
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Bu so'rov allaqachon qabul qilingan yoki eskirgan."})
	}

	s, err := h.sessions.Get(ctx, userID)
	if err != nil {
		_, _ = h.sessions.CompareAndSwapState(ctx, userID, string(StateGenerating), string(StateConfirm))
		return err
	}

	// To'lov: bepul urinish, oldin to'langan buyurtma yoki to'lov so'rash.
	note := ""
	if h.paymentsOn() {
		proceed, n, err := h.authorize(ctx, c, s)
		if err != nil {
			_, _ = h.sessions.CompareAndSwapState(ctx, userID, string(StateGenerating), string(StateConfirm))
			return err
		}
		if !proceed {
			return nil // to'lov ko'rsatmasi yuborildi, chek kutilmoqda
		}
		note = "\n" + n
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "Qabul qilindi ✅"})
	waitMsg := msgGeneratingDeck
	if ai.WorkType(s.WorkType).IsDocument() {
		waitMsg = msgGeneratingDoc
	}
	h.edit(c, summaryText(s)+note+"\n\n"+waitMsg)

	h.wg.Add(1)
	go h.runJob(s)
	return nil
}

// ---------------------------------------------------------------------------
// Generatsiya vazifasi
// ---------------------------------------------------------------------------

// runJob AI → fayl (PPTX/DOCX) → Telegram zanjirini alohida goroutine'da bajaradi.
func (h *Handler) runJob(s *storage.Session) {
	defer h.wg.Done()

	chat := &tele.Chat{ID: s.ChatID}
	log := h.log.With(
		zap.Int64("user_id", s.UserID),
		zap.String("work_type", s.WorkType),
		zap.Int("volume", s.Volume))

	ctx, cancel := context.WithTimeout(h.jobsCtx, h.opts.JobTimeout)
	defer cancel()

	j := &job{cancel: cancel}
	h.running.Store(s.UserID, j)
	defer h.running.Delete(s.UserID)

	// Goroutine ichidagi panic butun dasturni to'xtatmasligi va foydalanuvchi
	// "generating" holatida qotib qolmasligi uchun.
	defer func() {
		if r := recover(); r != nil {
			log.Error("panic recovered in generation job", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
			h.finishWithError(s, chat, log, fmt.Errorf("panic: %v", r), false)
		}
	}()

	started := time.Now()
	log.Info("generation started")

	out, err := h.generate(ctx, chat, s, log)
	if out != nil {
		defer removeFile(out.path, log) // vaqtinchalik fayl har qanday holatda o'chiriladi
	}
	if err != nil {
		h.finishWithError(s, chat, log, err, j.byUser.Load())
		return
	}

	took := time.Since(started)
	log.Info("generation finished", zap.Duration("took", took))
	h.finishWithSuccess(s, chat, log, out, took)
}

// generate navbatni kutadi, AI dan matn oladi, faylni (PPTX yoki DOCX) yaratadi
// va foydalanuvchiga yuboradi. Fayl yaratilgan bo'lsa, xato bo'lganda ham
// u qaytariladi (o'chirish uchun).
func (h *Handler) generate(ctx context.Context, chat *tele.Chat, s *storage.Session, log *zap.Logger) (*output, error) {
	// 1. Navbat: bo'sh joy bo'lmasa, foydalanuvchini ogohlantirib kutamiz.
	select {
	case h.slots <- struct{}{}:
	default:
		_, _ = h.bot.Send(chat, msgQueued)
		select {
		case h.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer func() { <-h.slots }()

	stopAction := h.keepUploadAction(ctx, chat)
	defer stopAction()

	opt, ok := findWorkType(ai.WorkType(s.WorkType))
	if !ok {
		return nil, fmt.Errorf("noma'lum ish turi: %q", s.WorkType)
	}

	// 2. AI + fayl: ish turiga qarab taqdimot yoki hujjat.
	var (
		out *output
		err error
	)
	if opt.Type.IsDocument() {
		out, err = h.buildPaper(ctx, chat, s, opt, log)
	} else {
		out, err = h.buildSlides(ctx, chat, s, opt, log)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}

	// 3. Telegram'ga hujjat sifatida yuborish.
	doc := &tele.Document{
		File:     tele.FromDisk(out.path),
		FileName: out.fileName,
		MIME:     out.mime,
		Caption:  out.caption,
	}
	if _, err := h.bot.Send(chat, doc); err != nil {
		return out, fmt.Errorf("telegram: hujjatni yuborib bo'lmadi: %w", err)
	}
	log.Info("document sent", append([]zap.Field{zap.String("file_name", doc.FileName)}, out.logFields...)...)
	return out, nil
}

// output — tayyor fayl va uni Telegram'ga yuborish uchun ma'lumotlar.
type output struct {
	path      string
	fileName  string
	mime      string
	caption   string
	summary   string // admin uchun natija: "20 ta slayd · 🖼 19/19 rasm (...)"
	logFields []zap.Field
}

// buildSlides taqdimotni yaratadi: titul va yakuniy slaydlarni generator
// o'zi chizadi, AI faqat mazmunli slaydlarni yozadi. Katta taqdimot bir necha
// so'rov bilan yoziladi — jarayon holati alohida xabarda ko'rsatiladi.
//
// AI maxsus dizaynda (custom.go) avval tarif modeli mavzuga mos dizayn
// konsepsiyasini yaratadi, slaydlarni ham shu model yozadi, rasmlar esa
// tarif sifatida slaydlarning bir qismiga chiziladi.
func (h *Handler) buildSlides(ctx context.Context, chat *tele.Chat, s *storage.Session, opt workTypeOption, log *zap.Logger) (*output, error) {
	b := h.builders.Slides
	content := b.ContentSlides(s.Volume)
	li := lang.Get(s.Language)

	var (
		gen      = h.ai
		spec     *generator.CustomSpec
		aiDesign *ai.Design
		plan     = picturePlan{svc: h.opts.Images, tier: h.imageTier(s)}
		tariff   CustomTariff
	)
	custom := isCustom(s)
	if custom {
		var ok bool
		if tariff, ok = h.customTariff(s.Tariff); !ok {
			return nil, fmt.Errorf("AI maxsus dizayn tarifi topilmadi: %q", s.Tariff)
		}
		gen = tariff.AI
		var err error
		if aiDesign, spec, err = h.customDesign(ctx, chat, log, s, tariff, li.Prompt); err != nil {
			return nil, err
		}
		plan = picturePlan{svc: tariff.Images, tier: images.TierStandard, limit: customImageCount(s.Volume, tariff.ImagePercent)}
	}
	plan.design = generator.DesignFor(s.Design, spec)
	photos := plan.design.Photos() && plan.svc != nil && plan.svc.Available()

	// Prompt tanlangan dizayn tuzilishiga moslanadi: AI dizayn chizadigan
	// maketlardan tanlaydi, uning imzo maketlarini ishlatadi va matn hajmini
	// dizayndagi joyga moslaydi.
	structure := plan.design.Structure()
	report, stopProgress := h.startProgress(ctx, chat, log, textStage("Slaydlar yozilmoqda..."))
	pres, err := gen.GeneratePresentation(ctx, ai.PresentationRequest{
		Topic: s.Topic, SlideCount: content, OnProgress: report, Images: photos,
		Layouts: structure.Layouts, Signature: structure.Signature, Compact: structure.Compact,
		Language: li.Prompt, Requirements: s.Requirements,
		OutlineTitle: li.Labels.Plan, ConclusionTitle: li.Labels.Conclusion,
		Custom: aiDesign.Brief(),
	})
	stopProgress()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAI, err)
	}

	deck := generator.Deck{
		Topic:       s.Topic,
		Subtitle:    pres.Subtitle,
		WorkType:    opt.Title(li),
		StudentName: s.StudentName,
		University:  s.University,
		TeacherName: s.TeacherName,
		Year:        strconv.Itoa(time.Now().Year()),
		Theme:       s.Design,
		Custom:      spec,
		Lang:        string(li.Code),
		Slides:      make([]generator.Slide, 0, len(pres.Slides)),
	}
	for _, sl := range pres.Slides {
		items := make([]generator.Item, len(sl.Items))
		for i, it := range sl.Items {
			items[i] = generator.Item{Heading: it.Heading, Text: it.Text, Value: it.Value, Points: it.Points}
		}
		deck.Slides = append(deck.Slides, generator.Slide{
			Layout: sl.Layout, Title: sl.Title, Lead: sl.Lead, Items: items, Highlight: sl.Highlight,
		})
	}
	var pics picturesInfo
	if photos {
		pics = h.addPictures(ctx, chat, log, pres, &deck, plan)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	path, err := b.Build(ctx, deck)
	if err != nil {
		return nil, fmt.Errorf("pptx: %w", err)
	}

	total := len(deck.Slides) + (s.Volume - content) // + titul va yakuniy slayd
	designName := s.Design
	if t, ok := generator.FindTheme(s.Design); ok {
		designName = t.Emoji + " " + t.Name
	}
	if custom {
		designName = customCaption(aiDesign, s.Tariff)
	}
	caption := fmt.Sprintf("📎 <b>%s</b>\n%s · %d ta slayd · %s",
		html.EscapeString(s.Topic), html.EscapeString(opt.Name), total, html.EscapeString(designName))
	if pics.placed > 0 {
		caption += fmt.Sprintf(" · 🖼 %d ta rasm", pics.placed)
	}
	summary := fmt.Sprintf("%d ta slayd · %s", total, designName)
	fields := []zap.Field{zap.Int("slides", total), zap.String("design", s.Design)}
	if custom {
		summary += fmt.Sprintf(" · %s, %s:%s", tariff.TextModel, tariff.ImageModel, tariff.ImageQuality)
		fields = append(fields, zap.String("tariff", s.Tariff), zap.String("design_name", aiDesign.Name))
	}
	if photos {
		summary += fmt.Sprintf(" · 🖼 %d/%d rasm (%s, %s)", pics.placed, pics.requested,
			pics.tier, orDash(pics.provider != "", pics.provider))
		fields = append(fields,
			zap.Int("images", pics.placed),
			zap.Int("images_requested", pics.requested),
			zap.String("image_tier", string(pics.tier)),
			zap.String("image_provider", pics.provider))
	}
	return &output{
		path:      path,
		fileName:  documentFileName(s.Topic, ".pptx"),
		mime:      pptxMIME,
		caption:   caption,
		summary:   summary,
		logFields: fields,
	}, nil
}

// picturesInfo — taqdimotga qo'shilgan rasmlar haqida (log va izoh uchun).
type picturesInfo struct {
	requested, placed int
	tier              images.Tier
	provider          string
}

// imageTier foydalanuvchining rasm tarifini aniqlaydi. Tarif shu yerda —
// bitta joyda — hal qilinadi: pullik xizmat qo'shilganda foydalanuvchi
// sotib olgan tarif (Session.Plan) shu funksiya orqali qo'llanadi.
func (h *Handler) imageTier(s *storage.Session) images.Tier {
	if t := images.Tier(strings.ToLower(strings.TrimSpace(s.Plan))); t.Valid() {
		return t
	}
	if h.opts.DefaultImageTier.Valid() {
		return h.opts.DefaultImageTier
	}
	return images.TierStandard
}

// addPictures AI yozgan rasm tavsiflari bo'yicha rasmlarni chizdiradi va
// taqdimotga joylaydi: titul (va yakuniy) slayd uchun bitta rasm va
// mazmunli slaydlar uchun rasmlar. Tayyor rasmli dizaynda — rasm sig'adigan
// barcha slaydlarga (yon panel uchun tik, banner uchun yotiq rasm); AI maxsus
// dizaynda — plan.limit ta kvadrat rasm, slaydlarga teng taqsimlangan.
// Chizilmagan rasm o'rnida slayd rasmsiz (titulda — bezakli fon bilan) chiqadi.
func (h *Handler) addPictures(ctx context.Context, chat *tele.Chat, log *zap.Logger, pres *ai.Presentation, deck *generator.Deck, plan picturePlan) picturesInfo {
	info := picturesInfo{tier: plan.tier}
	design := plan.design

	var (
		reqs    []images.Request
		targets []int // -1 — titul rasmi, aks holda deck.Slides indeksi
	)
	cover := pres.Image
	for i := 0; cover == "" && i < len(pres.Slides); i++ {
		cover = pres.Slides[i].Image
	}
	coverShape := images.Landscape
	if plan.limit > 0 {
		coverShape = images.Square
	}
	if cover != "" {
		reqs = append(reqs, images.Request{Prompt: design.PhotoPrompt(cover), Shape: coverShape})
		targets = append(targets, -1)
	}
	if plan.limit > 0 {
		for _, i := range customPhotoTargets(design, pres, deck, plan.limit-len(reqs)) {
			reqs = append(reqs, images.Request{Prompt: design.PhotoPrompt(pres.Slides[i].Image), Shape: images.Square})
			targets = append(targets, i)
		}
	} else {
		for i, sl := range pres.Slides {
			if sl.Image == "" || i >= len(deck.Slides) {
				continue
			}
			// Tepadagi banner uchun — yotiq rasm, yon panel uchun — tik rasm.
			slideShape := images.Portrait
			if design.PhotoWide(deck.Slides[i]) {
				slideShape = images.Landscape
			}
			reqs = append(reqs, images.Request{Prompt: design.PhotoPrompt(sl.Image), Shape: slideShape})
			targets = append(targets, i)
		}
	}
	info.requested = len(reqs)
	if len(reqs) == 0 {
		log.Warn("AI returned no image descriptions")
		return info
	}

	report, stop := h.startProgress(ctx, chat, log, imageStage)
	report(0, len(reqs))
	started := time.Now()
	results := plan.svc.Generate(ctx, info.tier, reqs, report)
	stop()

	for k, r := range results {
		if r.Data == nil {
			continue
		}
		pic, err := generator.NewPicture(r.Data)
		if err != nil {
			log.Warn("generated image rejected", zap.Error(err))
			continue
		}
		if targets[k] < 0 {
			deck.CoverImage = pic
		} else {
			deck.Slides[targets[k]].Image = pic
		}
		info.placed++
		info.provider = r.Provider
	}
	log.Info("images generated",
		zap.Int("placed", info.placed),
		zap.Int("requested", info.requested),
		zap.String("tier", string(info.tier)),
		zap.String("provider", info.provider),
		zap.Duration("took", time.Since(started)))
	return info
}

// buildPaper mustaqil ish yoki amaliy topshiriqni (.docx) yaratadi. Hujjat
// bir necha AI so'rovi bilan yoziladi, shuning uchun jarayon holati
// foydalanuvchiga alohida xabarda ko'rsatib boriladi.
func (h *Handler) buildPaper(ctx context.Context, chat *tele.Chat, s *storage.Session, opt workTypeOption, log *zap.Logger) (*output, error) {
	b := h.builders.Papers

	li := lang.Get(s.Language)
	report, stopProgress := h.startProgress(ctx, chat, log, textStage("Bo'limlar yozilmoqda..."))
	doc, err := h.ai.GenerateDocument(ctx, ai.DocumentRequest{
		WorkType:     opt.Type,
		Topic:        s.Topic,
		Words:        b.ContentWords(s.Volume),
		OnProgress:   report,
		Language:     li.Prompt,
		Requirements: s.Requirements,
	})
	stopProgress()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAI, err)
	}

	paper := generator.Paper{
		Topic:        s.Topic,
		WorkType:     opt.Title(li),
		StudentName:  s.StudentName,
		University:   s.University,
		TeacherName:  s.TeacherName,
		Year:         strconv.Itoa(time.Now().Year()),
		Lang:         string(li.Code),
		Introduction: toParagraphs(doc.Introduction),
		Conclusion:   toParagraphs(doc.Conclusion),
		References:   doc.References,
		Chapters:     make([]generator.Section, 0, len(doc.Chapters)),
	}
	for _, ch := range doc.Chapters {
		paper.Chapters = append(paper.Chapters, generator.Section{Title: ch.Title, Paragraphs: toParagraphs(ch.Paragraphs)})
	}
	path, err := b.Build(ctx, paper)
	if err != nil {
		return nil, fmt.Errorf("docx: %w", err)
	}

	return &output{
		path:     path,
		fileName: documentFileName(s.Topic, ".docx"),
		mime:     docxMIME,
		caption: fmt.Sprintf("📎 <b>%s</b>\n%s · %d sahifa",
			html.EscapeString(s.Topic), html.EscapeString(opt.Name), s.Volume),
		summary: fmt.Sprintf("%d sahifa · %d bob · %d manba", s.Volume, len(paper.Chapters), len(paper.References)),
		logFields: []zap.Field{
			zap.Int("pages_requested", s.Volume),
			zap.Int("chapters", len(paper.Chapters)),
			zap.Int("references", len(paper.References)),
		},
	}, nil
}

func toParagraphs(in []ai.Paragraph) []generator.Paragraph {
	out := make([]generator.Paragraph, len(in))
	for i, p := range in {
		out[i] = generator.Paragraph{Text: p.Text, Code: p.Code}
	}
	return out
}

// startProgress hujjat yozilayotganda bosqichlar holatini bitta xabarda
// ko'rsatib boradi: birinchi holatda xabar yuboriladi, keyin tahrirlanadi,
// stop chaqirilganda esa o'chiriladi.
//
// report AI goroutine'laridan chaqiriladi va hech qachon bloklanmaydi:
// navbatda faqat eng so'nggi holat saqlanadi, Telegram'ga esa ko'pi bilan
// 2 soniyada bir marta murojaat qilinadi (API cheklovlariga tushmaslik uchun).
//
// stage — jarayon bosqichlari nomlari (textStage, imageStage).
func (h *Handler) startProgress(ctx context.Context, chat *tele.Chat, log *zap.Logger, stage progressStage) (report func(done, total int), stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	updates := make(chan [2]int, 1)
	finished := make(chan struct{})

	report = func(done, total int) {
		v := [2]int{done, total}
		for {
			select {
			case updates <- v:
				return
			default:
				// Eski holatni tashlab yuboramiz — faqat oxirgisi muhim.
				select {
				case <-updates:
				default:
				}
			}
		}
	}

	go func() {
		defer close(finished)
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic in progress loop", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
			}
		}()

		var msg *tele.Message
		defer func() {
			if msg != nil {
				if err := h.bot.Delete(msg); err != nil {
					log.Debug("failed to delete progress message", zap.Error(err))
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case v := <-updates:
				text := progressText(v[0], v[1], stage)
				var err error
				if msg == nil {
					msg, err = h.bot.Send(chat, text)
				} else {
					_, err = h.bot.Edit(msg, text)
				}
				if err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
					log.Debug("failed to update progress message", zap.Error(err))
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}()

	stop = func() {
		cancel()
		<-finished
	}
	return report, stop
}

// progressStage — jarayon xabaridagi belgi va bosqichlar nomlari.
type progressStage struct {
	icon    string
	first   string // done == 0
	writing string // 0 < done < total
	last    string // done == total
}

// textStage — AI matn yozish bosqichlari (writing — "Bo'limlar yozilmoqda..." kabi).
func textStage(writing string) progressStage {
	return progressStage{icon: "✍️", first: "Reja tuzilmoqda...", writing: writing, last: "Fayl tayyorlanmoqda..."}
}

// imageStage — rasmli dizaynda rasmlarni chizish bosqichi.
var imageStage = progressStage{icon: "🖼", first: "Rasmlar chizilmoqda...", writing: "Rasmlar chizilmoqda...", last: "Slaydlar tayyorlanmoqda..."}

// progressText jarayon holati matni: "✍️ Bo'limlar yozilmoqda... ▰▰▰▱▱▱▱▱▱▱ 3/8".
func progressText(done, total int, st progressStage) string {
	total = max(total, 1)
	done = min(max(done, 0), total)

	const width = 10
	filled := done * width / total
	bar := strings.Repeat("▰", filled) + strings.Repeat("▱", width-filled)

	stage := st.writing
	switch {
	case done == 0:
		stage = st.first
	case done == total:
		stage = st.last
	}
	return fmt.Sprintf("%s <b>%s</b>\n%s %d/%d", st.icon, stage, bar, done, total)
}

// finishWithSuccess sessiyani yakunlaydi, statistikani yangilaydi va xarid
// haqida adminlarga xabar beradi.
func (h *Handler) finishWithSuccess(s *storage.Session, chat *tele.Chat, log *zap.Logger, out *output, took time.Duration) {
	ctx, cancel := requestContext()
	defer cancel()

	if s.OrderID != nil && h.billing != nil {
		if _, err := h.billing.SetOrderStatus(ctx, *s.OrderID, storage.OrderPaid, storage.OrderDelivered); err != nil {
			log.Error("failed to mark order delivered", zap.Uint("order_id", *s.OrderID), zap.Error(err))
		}
	}
	// Xabar sessiya tozalanishidan oldin tayyorlanadi (buyurtma ma'lumotlari kerak).
	notice := h.purchaseNotice(ctx, s, out, took, log)

	now := time.Now().UTC()
	resetDraft(s)
	s.State = string(StateIdle)
	s.GeneratedCount++
	s.LastGeneratedAt = &now
	if err := h.sessions.Save(ctx, s); err != nil {
		log.Error("failed to save session after success", zap.Error(err))
	}
	if _, err := h.bot.Send(chat, msgDone); err != nil {
		log.Warn("failed to send done message", zap.Error(err))
	}
	if notice != "" {
		h.toAdmins(notice)
	}
}

// finishWithError xatoni logga yozadi va foydalanuvchiga tushunarli xabar yuboradi.
// Foydalanuvchi o'zi bekor qilgan bo'lsa — sessiya tozalanadi, aks holda
// ma'lumotlar saqlanib, "Qayta urinish" tugmasi taklif qilinadi.
func (h *Handler) finishWithError(s *storage.Session, chat *tele.Chat, log *zap.Logger, err error, byUser bool) {
	ctx, cancel := requestContext()
	defer cancel()

	if byUser {
		// To'langan buyurtma "paid" holatida qoladi va keyingi buyurtmada
		// qayta to'lovsiz ishlatiladi (FindCredit).
		log.Info("generation cancelled by user")
		resetDraft(s)
		s.State = string(StateIdle)
		if err := h.sessions.Save(ctx, s); err != nil {
			log.Error("failed to save session after cancel", zap.Error(err))
		}
		_, _ = h.bot.Send(chat, msgJobCancelled)
		return
	}

	log.Error("generation failed", zap.Error(err))
	if _, err := h.sessions.CompareAndSwapState(ctx, s.UserID, string(StateGenerating), string(StateConfirm)); err != nil {
		log.Error("failed to restore confirm state", zap.Error(err))
	}

	var msg string
	switch {
	case h.jobsCtx.Err() != nil:
		msg = "🔧 Bot texnik sabablarga ko'ra qayta ishga tushirilmoqda. Bir necha daqiqadan so'ng «Qayta urinish» tugmasini bosing."
	case errors.Is(err, context.DeadlineExceeded):
		msg = "⌛ Tayyorlash juda uzoq davom etdi. Iltimos, qayta urinib ko'ring yoki hajmni (slaydlar/sahifalar sonini) kamaytiring."
	case errors.Is(err, errAI):
		msg = "😔 Sun'iy intellekt xizmatida vaqtinchalik muammo yuz berdi. Iltimos, birozdan so'ng qayta urinib ko'ring."
	default:
		msg = "😔 Faylni tayyorlashda xatolik yuz berdi. Iltimos, qayta urinib ko'ring."
	}
	if _, err := h.bot.Send(chat, msg, retryKeyboard()); err != nil {
		log.Warn("failed to send error message", zap.Error(err))
	}
	// Buyurtma (to'langan yoki bepul) tayyorlanmay qolgani haqida adminlarga.
	// Bot to'xtatilayotganda yubormaymiz — qayta ishga tushgach tiklanadi.
	if h.jobsCtx.Err() == nil {
		if notice := h.failureNotice(ctx, s, err, log); notice != "" {
			h.toAdmins(notice)
		}
	}
}

// keepUploadAction generatsiya davomida "hujjat yuborilmoqda..." statusini
// ko'rsatib turadi (Telegram uni ~5 soniyadan keyin o'chiradi).
func (h *Handler) keepUploadAction(ctx context.Context, chat *tele.Chat) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				h.log.Error("panic in chat action loop", zap.Any("panic", r))
			}
		}()
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			_ = h.bot.Notify(chat, tele.UploadingDocument)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return cancel
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

// beginFlow buyurtmani noldan boshlaydi (profil ma'lumotlari saqlanadi).
func (h *Handler) beginFlow(ctx context.Context, s *storage.Session) error {
	resetDraft(s)
	s.State = string(StateWorkType)
	return h.sessions.Save(ctx, s)
}

// ask berilgan qadam savolini (kerak bo'lsa javob tugmalari bilan) yuboradi.
func (h *Handler) ask(c tele.Context, s *storage.Session, state State) error {
	st, ok := steps[state]
	if !ok {
		return fmt.Errorf("qadam topilmadi: %s", state)
	}
	markup := removeKeyboard()
	if st.suggest != nil {
		if options := st.suggest(h.opts.Limits, s); len(options) > 0 {
			markup = replyKeyboard(options)
		}
	}
	return c.Send(st.prompt(h.opts.Limits, s), markup)
}

// sendSummary yig'ilgan ma'lumotlarni tasdiqlash tugmalari bilan yuboradi.
func (h *Handler) sendSummary(c tele.Context, s *storage.Session) error {
	text := summaryText(s)
	if line := h.priceLine(c, s); line != "" {
		text += "\n" + line
	}
	return c.Send(text+"\n\nHammasi to'g'rimi?", confirmKeyboard())
}

// edit callback xabarini tahrirlaydi (inline tugmalar ham olib tashlanadi).
// "message is not modified" kabi zararsiz xatolar e'tiborsiz qoldiriladi.
func (h *Handler) edit(c tele.Context, text string) {
	if err := c.Edit(text); err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to edit message", zap.Error(err))
	}
}

// editPrompt callback xabarini tahrirlaydi: rasmli xabarda — izohini,
// oddiy xabarda — matnini (inline tugmalar ham olib tashlanadi).
func (h *Handler) editPrompt(c tele.Context, text string) {
	var err error
	if m := c.Message(); m != nil && m.Photo != nil {
		err = c.EditCaption(text)
	} else {
		err = c.Edit(text)
	}
	if err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to edit prompt message", zap.Error(err))
	}
}

// removeButtons callback xabaridagi inline tugmalarni olib tashlaydi.
func (h *Handler) removeButtons(c tele.Context) {
	if c.Message() == nil {
		return
	}
	if _, err := h.bot.EditReplyMarkup(c.Message(), nil); err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to remove buttons", zap.Error(err))
	}
}

// summaryText buyurtma ma'lumotlarini HTML ko'rinishida qaytaradi.
func summaryText(s *storage.Session) string {
	return "📋 <b>Ma'lumotlarni tekshiring</b>\n\n" + orderDetails(s, maxSummaryRequirements)
}

// orderDetails buyurtma tafsilotlari (ish turi, til, mavzu, hajm, fayl,
// talaba ma'lumotlari va talablar) — foydalanuvchi xulosasi va admin xabari
// uchun. Talablar maxRequirements belgigacha qisqartiriladi.
func orderDetails(s *storage.Session, maxRequirements int) string {
	name := s.WorkType
	if opt, ok := findWorkType(ai.WorkType(s.WorkType)); ok {
		name = opt.Name
	}
	volume, format := "🔢 <b>Slaydlar soni:</b>", "PowerPoint (.pptx)"
	if ai.WorkType(s.WorkType).IsDocument() {
		volume, format = "📄 <b>Sahifalar soni:</b>", "Word (.docx)"
	}
	e := html.EscapeString
	if isCustom(s) {
		format += "\n🎨 <b>Dizayn:</b> " + customLabel + " · " + tariffName(s.Tariff)
	} else if !ai.WorkType(s.WorkType).IsDocument() {
		if t, ok := generator.FindTheme(s.Design); ok {
			format += "\n🎨 <b>Dizayn:</b> " + t.Emoji + " " + e(t.Name)
			if t.Photos {
				format += " 📷 (AI rasmlar bilan)"
			}
		}
	}
	text := fmt.Sprintf("📌 <b>Ish turi:</b> %s\n"+
		"🌐 <b>Til:</b> %s\n"+
		"📝 <b>Mavzu:</b> %s\n"+
		"%s %d\n"+
		"📎 <b>Fayl:</b> %s\n"+
		"👤 <b>Talaba:</b> %s\n"+
		"🏛 <b>Universitet:</b> %s\n"+
		"👨‍🏫 <b>Tekshiruvchi:</b> %s",
		e(name), e(lang.Get(s.Language).Name), e(s.Topic), volume, s.Volume, format,
		e(s.StudentName), e(s.University), e(s.TeacherName))
	if s.Requirements != "" {
		text += "\n✍️ <b>Qo'shimcha talablar:</b> " + e(truncateRunes(s.Requirements, maxRequirements))
	}
	return text
}

// maxSummaryRequirements — xulosada ko'rsatiladigan talablar uzunligi
// (to'liq matn AI ga baribir yuboriladi).
const maxSummaryRequirements = 300

func workTypeKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	rows := make([]tele.Row, 0, len(workTypes))
	for _, o := range workTypes {
		rows = append(rows, m.Row(m.Data(o.Button, cbWorkType, string(o.Type))))
	}
	m.Inline(rows...)
	return m
}

// languageKeyboard ish tilini tanlash tugmalari (qatorda 2 tadan).
func languageKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	langs := lang.All()
	rows := make([]tele.Row, 0, (len(langs)+1)/2)
	for i := 0; i < len(langs); i += 2 {
		var btns []tele.Btn
		for _, l := range langs[i:min(i+2, len(langs))] {
			btns = append(btns, m.Data(l.Button, cbLanguage, string(l.Code)))
		}
		rows = append(rows, m.Row(btns...))
	}
	m.Inline(rows...)
	return m
}

// designKeyboard dizayn tanlash tugmalari (qatorda 2 tadan); custom bo'lsa —
// birinchi qatorda "✨ AI maxsus dizayn" tugmasi.
func designKeyboard(custom bool) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	themes := generator.Themes()
	rows := make([]tele.Row, 0, (len(themes)+1)/2+1)
	if custom {
		rows = append(rows, m.Row(m.Data(customLabel+" — mavzuga mos", cbDesign, generator.ThemeCustom)))
	}
	for i := 0; i < len(themes); i += 2 {
		var btns []tele.Btn
		for j, t := range themes[i:min(i+2, len(themes))] {
			label := fmt.Sprintf("%d. %s %s", i+j+1, t.Emoji, t.Name)
			if t.Photos {
				label += " 📷" // rasmli dizayn tugmada ham ko'rinsin
			}
			btns = append(btns, m.Data(label, cbDesign, t.ID))
		}
		rows = append(rows, m.Row(btns...))
	}
	m.Inline(rows...)
	return m
}

func confirmKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(m.Data("✅ Tasdiqlash", cbConfirm)),
		m.Row(m.Data("🔄 Qaytadan to'ldirish", cbRestart), m.Data("❌ Bekor qilish", cbCancel)),
	)
	return m
}

func retryKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("🔄 Qayta urinish", cbConfirm), m.Data("❌ Bekor qilish", cbCancel)))
	return m
}

// replyKeyboard javob variantlari klaviaturasi: qisqa variantlar (sonlar)
// qatorda 3 tadan, uzunlari — har biri alohida qatorda.
func replyKeyboard(options []string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{ResizeKeyboard: true, OneTimeKeyboard: true}
	perRow := 1
	short := true
	for _, o := range options {
		if len([]rune(o)) > 4 {
			short = false
			break
		}
	}
	if short {
		perRow = 3
	}

	var rows []tele.Row
	for i := 0; i < len(options); i += perRow {
		var btns []tele.Btn
		for _, o := range options[i:min(i+perRow, len(options))] {
			btns = append(btns, m.Text(o))
		}
		rows = append(rows, m.Row(btns...))
	}
	m.Reply(rows...)
	return m
}

func removeKeyboard() *tele.ReplyMarkup {
	return &tele.ReplyMarkup{RemoveKeyboard: true}
}

var unsafeFileChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

// documentFileName mavzudan xavfsiz fayl nomi yasaydi: "Suniy_intellekt....pptx".
// ext — kengaytma (".pptx" yoki ".docx").
func documentFileName(topic, ext string) string {
	name := unsafeFileChars.ReplaceAllString(topic, " ")
	name = strings.Join(strings.Fields(name), "_")
	if r := []rune(name); len(r) > 60 {
		name = strings.TrimRight(string(r[:60]), "_.-")
	}
	if name == "" {
		name = "hujjat"
		if ext == ".pptx" {
			name = "taqdimot"
		}
	}
	return name + ext
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// removeFile vaqtinchalik faylni o'chiradi.
func removeFile(path string, log *zap.Logger) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn("failed to remove temp file", zap.String("path", path), zap.Error(err))
	}
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
