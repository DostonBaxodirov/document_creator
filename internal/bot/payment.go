package bot

// To'lov oqimi: tasdiqlangan buyurtma uchun narxni aniqlash (oldin to'langan
// buyurtma yoki pullik), karta ma'lumotlarini yuborish,
// chek rasmini AI orqali tekshirish va shubhali cheklarni adminga yuborish.

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/payment"
	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/textutil"
	"telegram-presentation-bot/internal/tz"
)

// BillingStore — foydalanuvchilar, buyurtmalar va to'lovlar ombori.
type BillingStore interface {
	EnsureUser(ctx context.Context, telegramID int64, username, firstName string) (*storage.User, error)
	SetAdmin(ctx context.Context, telegramID int64) (bool, error)
	IsAdmin(ctx context.Context, telegramID int64) (bool, error)
	ListAdmins(ctx context.Context) ([]storage.User, error)

	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// CountFreeUsed(ctx context.Context, userID uint, kind string) (int, error)
	FindCredit(ctx context.Context, userID uint, kind string, price int) (*storage.Order, error)
	CreateOrder(ctx context.Context, o *storage.Order) error
	SaveOrder(ctx context.Context, o *storage.Order) error
	GetOrder(ctx context.Context, id uint) (*storage.Order, error)
	SetOrderStatus(ctx context.Context, id uint, from, to string) (bool, error)

	SubmitPayment(ctx context.Context, p *storage.Payment) error
	ReviewPayment(ctx context.Context, paymentID uint, approve bool, adminID int64) (*storage.Payment, *storage.Order, error)
	GetPayment(ctx context.Context, id uint) (*storage.Payment, error)
	OrderPayment(ctx context.Context, orderID uint) (*storage.Payment, error)
	ReceiptSeen(ctx context.Context, transactionID, fileUniqueID string) (bool, error)
	ListPayments(ctx context.Context, since *time.Time) ([]storage.Payment, error)
	ListPaymentsByStatus(ctx context.Context, status string, limit int) ([]storage.Payment, int, error)
}

// ReceiptVerifier — chek rasmini o'qiydigan AI (internal/payment).
type ReceiptVerifier interface {
	Read(ctx context.Context, image []byte, exp payment.Expectation) (*payment.Receipt, error)
}

const (
	maxReceiptBytes = 10 << 20        // chek rasmi hajmi chegarasi
	receiptTimeout  = 3 * time.Minute // chekni yuklab olish va AI tekshiruvi uchun
	fileTypePhoto   = "photo"
	fileTypeDoc     = "document"
)

// To'lovga oid xabarlar.
const (
	msgReviewWait      = "⏳ Chekingiz tekshirilmoqda. Natija tez orada shu yerga keladi."
	msgReceiptChecking = "🔍 Chek tekshirilmoqda, biroz kuting..."
	msgReceiptToAdmin  = "🕵️ Chekingiz admin tekshiruviga yuborildi. Tasdiqlangach, ishingiz avtomatik tayyorlanadi."
	msgReceiptInvalid  = "⚠️ Yuborilgan rasm to'lov chekiga o'xshamaydi.\n\n" +
		"Iltimos, to'lov amalga oshirilgani ko'rinib turgan chek skrinshotini yuboring yoki /cancel bilan bekor qiling."
	msgReceiptRejected = "❌ Chekingiz admin tomonidan tasdiqlanmadi.\n\n" +
		"To'g'ri chekni yuboring yoki /cancel bilan bekor qiling."
	msgReceiptError       = "😔 Chekni tekshirishda xatolik yuz berdi. Iltimos, chekni qayta yuboring."
	msgReceiptPhotoOnly   = "🖼 Iltimos, chekni rasm (skrinshot) ko'rinishida yuboring."
	msgReceiptTooLarge    = "⚠️ Fayl juda katta. Chekning skrinshotini yuboring."
	msgReceiptInterrupted = "⚠️ Bot qayta ishga tushirildi va chekingiz tekshirilmay qoldi. Iltimos, chekni qayta yuboring."
	msgPaymentApproved    = "✅ <b>To'lov tasdiqlandi!</b> Rahmat."
	msgPaymentByAdmin     = "✅ <b>To'lovingiz admin tomonidan tasdiqlandi!</b> Rahmat."
	msgCreditSaved        = "✅ To'lovingiz tasdiqlandi. Keyingi buyurtmangizda qayta to'lov so'ralmaydi — /start"
	msgPaidRetry          = "✅ To'lovingiz tasdiqlangan. Ishni tayyorlash uchun «Qayta urinish» tugmasini bosing."
)

// paymentsOn to'lov talab qilinishini bildiradi.
func (h *Handler) paymentsOn() bool {
	return h.opts.Payment.Enabled && h.billing != nil
}

// orderKind buyurtmaning narx toifasini aniqlaydi: maxsus dizayn (taqdimot
// yoki Word ish) — "ai_<tarif>", rasmli dizayndagi taqdimot — "photo",
// rasmsiz taqdimot va tayyor dizayndagi Word ishlar — "plain".
func orderKind(s *storage.Session) string {
	if isCustom(s) {
		return storage.AIKind(s.Tariff)
	}
	if !ai.WorkType(s.WorkType).IsDocument() && generator.ThemeHasPhotos(s.Design) {
		return storage.KindPhoto
	}
	return storage.KindPlain
}

// kindOf buyurtma toifasini handler sozlamalari bilan aniqlaydi: rasm
// provayderi ulanmagan bo'lsa, rasmli dizayn ham rasmsiz chiqadi va rasmsiz
// narxda hisoblanadi.
func (h *Handler) kindOf(s *storage.Session) string {
	k := orderKind(s)
	if k == storage.KindPhoto && (h.opts.Images == nil || !h.opts.Images.Available()) {
		return storage.KindPlain
	}
	return k
}

// slideRate bitta slayd narxi: AI maxsus dizaynda — tarif narxi, rasmli
// dizaynda — PriceSlidePhoto, aks holda — PriceSlidePlain.
func (h *Handler) slideRate(kind string) int {
	if tariff, ok := storage.KindTariff(kind); ok {
		if t, ok := h.customTariff(tariff); ok {
			return t.PriceSlide
		}
	}
	if kind == storage.KindPhoto {
		return h.opts.Payment.PriceSlidePhoto
	}
	return h.opts.Payment.PriceSlidePlain
}

// pageRate mustaqil ish va referatning bitta sahifa narxi: maxsus dizaynda —
// tarif narxi, aks holda — PricePage.
func (h *Handler) pageRate(kind string) int {
	if tariff, ok := storage.KindTariff(kind); ok {
		if t, ok := h.customTariff(tariff); ok {
			return t.PricePage
		}
	}
	return h.opts.Payment.PricePage
}

// perPage Word ish narxi sahifalar soniga bog'liqligini bildiradi (mustaqil
// ish va referat; amaliy topshiriq — qat'iy narx).
func perPage(workType string) bool {
	w := ai.WorkType(workType)
	return w.IsDocument() && w != ai.WorkPractical
}

// price buyurtma narxini hisoblaydi: taqdimot — slaydlar soni × bitta slayd
// narxi, mustaqil ish va referat — sahifalar soni × bitta sahifa narxi,
// amaliy topshiriq — qat'iy narx.
func (h *Handler) price(s *storage.Session, kind string) int {
	switch {
	case perPage(s.WorkType):
		return max(s.Volume, 1) * h.pageRate(kind)
	case ai.WorkType(s.WorkType).IsDocument():
		return h.opts.Payment.PriceDocument
	}
	return max(s.Volume, 1) * h.slideRate(kind)
}

// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
//
// // freeLimit toifa uchun bepul urinishlar soni (AI maxsus dizayn — bepul emas).
// func (h *Handler) freeLimit(kind string) int {
// 	if _, ok := storage.KindTariff(kind); ok {
// 		return 0
// 	}
// 	if kind == storage.KindPhoto {
// 		return h.opts.Payment.FreePhoto
// 	}
// 	return h.opts.Payment.FreePlain
// }

// pricingText narxlar haqida qisqa matn (xush kelibsiz va yordam xabarlari uchun).
func (h *Handler) pricingText() string {
	if !h.paymentsOn() {
		return ""
	}
	p := h.opts.Payment
	s := fmt.Sprintf("\n\n💳 <b>Narxlar:</b> taqdimot — har bir slayd %s (📷 rasmli dizaynda — %s), "+
		"mustaqil ish va referat — har bir sahifa %s, amaliy topshiriq — %s.",
		formatSum(p.PriceSlidePlain), formatSum(p.PriceSlidePhoto), formatSum(p.PricePage), formatSum(p.PriceDocument))
	if h.customOn() {
		var parts []string
		for _, name := range h.customTariffs() {
			t, _ := h.customTariff(name)
			parts = append(parts, fmt.Sprintf("%s — %s/slayd, %s/sahifa", tariffViews[name].name,
				formatSum(t.PriceSlide), formatSum(t.PricePage)))
		}
		s += "\n✨ Maxsus dizayn: " + strings.Join(parts, "; ") + "."
	}
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// if p.FreePhoto > 0 || p.FreePlain > 0 {
	// 	s += fmt.Sprintf("\n🎁 Birinchi %d ta rasmli va %d ta rasmsiz ish — <b>bepul</b>!", p.FreePhoto, p.FreePlain)
	// }
	s += "\nℹ️ Batafsil qo'llanma — /help"
	return s
}

// ---------------------------------------------------------------------------
// Narxni aniqlash va buyurtma yaratish
// ---------------------------------------------------------------------------

type quoteMode int

const (
	quotePaid   quoteMode = iota // to'lov kerak
	quoteCredit                  // oldin to'langan, lekin yetkazilmagan buyurtma bor
	// quoteFree — bepul urinish (hozircha o'chirilgan — barcha ishlar pullik)
)

// quote — buyurtma narxi.
type quote struct {
	mode   quoteMode
	kind   string
	price  int
	detail string // narx tarkibi: "10 slayd × 300 so'm", "15 sahifa × 200 so'm" (amaliy topshiriqda — bo'sh)
	credit *storage.Order
}

// line tasdiqlash xulosasidagi narx qatori.
func (q *quote) line() string {
	switch q.mode {
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// case quoteFree:
	// 	return "💰 <b>Narx:</b> 🎁 Bepul (sinov imkoniyati)"
	case quoteCredit:
		return "💰 <b>Narx:</b> ✅ Oldin to'langan"
	}
	line := "💰 <b>Narx:</b> " + formatSum(q.price)
	if q.detail != "" {
		line += " <i>(" + q.detail + ")</i>"
	}
	return line
}

// quote foydalanuvchi uchun joriy buyurtma narxini aniqlaydi.
func (h *Handler) quote(ctx context.Context, s *storage.Session, u *storage.User) (*quote, error) {
	kind := h.kindOf(s)
	q := &quote{kind: kind, price: h.price(s, kind)}
	switch {
	case perPage(s.WorkType):
		q.detail = fmt.Sprintf("%d sahifa × %s", max(s.Volume, 1), formatSum(h.pageRate(kind)))
	case !ai.WorkType(s.WorkType).IsDocument():
		q.detail = fmt.Sprintf("%d slayd × %s", max(s.Volume, 1), formatSum(h.slideRate(kind)))
	}
	credit, err := h.billing.FindCredit(ctx, u.ID, kind, q.price)
	if err != nil {
		return nil, err
	}
	if credit != nil {
		q.mode, q.credit = quoteCredit, credit
		return q, nil
	}
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// used, err := h.billing.CountFreeUsed(ctx, u.ID, kind)
	// if err != nil {
	// 	return nil, err
	// }
	// if used < h.freeLimit(kind) {
	// 	q.mode = quoteFree
	// }
	return q, nil
}

func (h *Handler) ensureUser(ctx context.Context, c tele.Context) (*storage.User, error) {
	u := c.Sender()
	return h.billing.EnsureUser(ctx, u.ID, u.Username, u.FirstName)
}

// priceLine tasdiqlash xulosasi uchun narx qatori (to'lov o'chirilgan yoki
// xato bo'lsa — bo'sh satr; xulosa narxsiz ham ko'rsatiladi).
func (h *Handler) priceLine(c tele.Context, s *storage.Session) string {
	if !h.paymentsOn() {
		return ""
	}
	ctx, cancel := requestContext()
	defer cancel()
	u, err := h.ensureUser(ctx, c)
	if err == nil {
		var q *quote
		if q, err = h.quote(ctx, s, u); err == nil {
			return q.line()
		}
	}
	h.log.Warn("failed to quote order", zap.Int64("user_id", s.UserID), zap.Error(err))
	return ""
}

// authorize tasdiqlangan buyurtma uchun to'lovni hal qiladi (sessiya
// "generating" holatida). true qaytarsa — generatsiyani boshlash mumkin
// (s.OrderID qo'yilgan va saqlangan); false — to'lov so'raldi va sessiya
// "await_payment" holatiga o'tkazildi.
func (h *Handler) authorize(ctx context.Context, c tele.Context, s *storage.Session) (proceed bool, note string, err error) {
	u, err := h.ensureUser(ctx, c)
	if err != nil {
		return false, "", err
	}
	q, err := h.quote(ctx, s, u)
	if err != nil {
		return false, "", err
	}

	switch q.mode {
	case quoteCredit:
		o := q.credit
		fillOrder(o, s)
		if err := h.billing.SaveOrder(ctx, o); err != nil {
			return false, "", err
		}
		s.OrderID = &o.ID
		note = "✅ Oldin to'langan buyurtmangiz hisobidan."
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// case quoteFree:
	// 	now := time.Now().UTC()
	// 	o := newOrder(u, s, q.kind, 0)
	// 	o.IsFree, o.IsPaid, o.Status, o.PaidAt = true, true, storage.OrderPaid, &now
	// 	if err := h.billing.CreateOrder(ctx, o); err != nil {
	// 		return false, "", err
	// 	}
	// 	s.OrderID = &o.ID
	// 	note = "🎁 Bepul sinov imkoniyatidan foydalanildi."
	default:
		o := newOrder(u, s, q.kind, q.price)
		if err := h.billing.CreateOrder(ctx, o); err != nil {
			return false, "", err
		}
		s.OrderID = &o.ID
		s.State = string(StatePayment)
		if err := h.sessions.Save(ctx, s); err != nil {
			if _, cerr := h.billing.SetOrderStatus(ctx, o.ID, storage.OrderPending, storage.OrderCancelled); cerr != nil {
				h.log.Warn("failed to cancel orphan order", zap.Uint("order_id", o.ID), zap.Error(cerr))
			}
			return false, "", err
		}
		h.log.Info("payment requested", zap.Int64("user_id", s.UserID), zap.Uint("order_id", o.ID),
			zap.String("kind", o.Kind), zap.Int("price", o.Price))
		_ = c.Respond(&tele.CallbackResponse{Text: "To'lov kerak 💳"})
		h.edit(c, summaryText(s)+"\n"+q.line())
		return false, "", c.Send(h.paymentText(o, true), paymentKeyboard())
	}

	if err := h.sessions.Save(ctx, s); err != nil {
		return false, "", err
	}
	return true, note, nil
}

func newOrder(u *storage.User, s *storage.Session, kind string, price int) *storage.Order {
	o := &storage.Order{
		UserID:         u.ID,
		TelegramUserID: u.TelegramUserID,
		Kind:           kind,
		Price:          price,
		Status:         storage.OrderPending,
	}
	fillOrder(o, s)
	return o
}

// fillOrder buyurtmaga sessiyadagi ma'lumotlar nusxasini yozadi.
func fillOrder(o *storage.Order, s *storage.Session) {
	o.WorkType = s.WorkType
	o.Topic = s.Topic
	o.Volume = s.Volume
	o.Design = s.Design
	o.Language = s.Language
	o.Requirements = s.Requirements
	o.StudentName = s.StudentName
	o.University = s.University
	o.TeacherName = s.TeacherName
}

// orderLabel buyurtmaning qisqa tavsifi: "Slayd (taqdimot), rasmli dizayn".
func orderLabel(workType, kind string) string {
	name := workType
	if opt, ok := findWorkType(ai.WorkType(workType)); ok {
		name = opt.Name
	}
	if tariff, ok := storage.KindTariff(kind); ok {
		return name + ", ✨ Maxsus dizayn — " + tariffName(tariff)
	}
	if ai.WorkType(workType).IsDocument() {
		return name
	}
	if kind == storage.KindPhoto {
		return name + ", rasmli dizayn"
	}
	return name + ", rasmsiz dizayn"
}

// paymentText to'lov ko'rsatmasi: buyurtma, summa, karta va chek yuborish tartibi.
// intro — birinchi marta ko'rsatilmoqda (bepul urinishlar o'chirilgani uchun
// hozircha matnga ta'sir qilmaydi).
func (h *Handler) paymentText(o *storage.Order, intro bool) string {
	p := h.opts.Payment
	var b strings.Builder
	b.WriteString("💳 <b>To'lov</b>\n\n")
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// if intro && h.freeLimit(o.Kind) > 0 {
	// 	fmt.Fprintf(&b, "Bepul sinov imkoniyatidan foydalanib bo'lgansiz (har bir foydalanuvchiga %d ta rasmli va %d ta rasmsiz ish bepul beriladi).\n\n",
	// 		p.FreePhoto, p.FreePlain)
	// }
	fmt.Fprintf(&b, "🧾 <b>Buyurtma #%d:</b> %s\n", o.ID, html.EscapeString(orderLabel(o.WorkType, o.Kind)))
	fmt.Fprintf(&b, "💰 <b>Summa:</b> %s", formatSum(o.Price))
	switch {
	case o.Volume <= 0:
	case perPage(o.WorkType):
		fmt.Fprintf(&b, " <i>(%d sahifa)</i>", o.Volume)
	case !ai.WorkType(o.WorkType).IsDocument():
		fmt.Fprintf(&b, " <i>(%d slayd)</i>", o.Volume)
	}
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "💳 <b>Karta:</b> <code>%s</code>\n", formatCard(p.CardNumber))
	if p.CardHolder != "" {
		fmt.Fprintf(&b, "👤 <b>Karta egasi:</b> %s\n", html.EscapeString(p.CardHolder))
	}
	b.WriteString("\n📸 To'lovni amalga oshirgach, <b>chek skrinshotini</b> shu yerga rasm qilib yuboring.\n" +
		"<i>Chekda summa, qabul qiluvchi karta raqamining oxirgi 4 raqami, sana-vaqt va to'lov holati ko'rinib tursin.</i>\n\n" +
		"Chek tasdiqlangach, ishingiz avtomatik tayyorlanadi. Bekor qilish — /cancel")
	return b.String()
}

func paymentKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("❌ Bekor qilish", cbCancel)))
	return m
}

// sendPaymentInfo to'lov kutilayotganda (matn yuborilganda) ko'rsatmani qayta yuboradi.
func (h *Handler) sendPaymentInfo(ctx context.Context, c tele.Context, s *storage.Session) error {
	if s.OrderID != nil {
		if o, err := h.billing.GetOrder(ctx, *s.OrderID); err == nil {
			return c.Send(h.paymentText(o, false), paymentKeyboard())
		}
	}
	// Buyurtma topilmadi — xavfsiz tiklash.
	resetDraft(s)
	s.State = string(StateIdle)
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}
	return c.Send(msgIdle, removeKeyboard())
}

// replyInReview chek tekshirilayotgan paytda kelgan xabarga javob beradi.
// To'lov allaqachon tasdiqlangan, lekin generatsiya boshlanmay qolgan bo'lsa
// (masalan, texnik xato), foydalanuvchi "Qayta urinish" bosqichiga qaytariladi
// — u tekshiruv holatida qotib qolmaydi.
func (h *Handler) replyInReview(ctx context.Context, c tele.Context, s *storage.Session) error {
	if s.OrderID != nil && h.billing != nil {
		o, err := h.billing.GetOrder(ctx, *s.OrderID)
		if err == nil && o.Status == storage.OrderPaid {
			ok, err := h.sessions.CompareAndSwapState(ctx, s.UserID, string(StateReview), string(StateConfirm))
			if err != nil {
				return err
			}
			if ok {
				return c.Send(msgPaidRetry, retryKeyboard())
			}
		}
	}
	return c.Send(msgReviewWait)
}

// cancelPendingOrder to'lov kutilayotgan buyurtmani bekor qiladi.
func (h *Handler) cancelPendingOrder(ctx context.Context, s *storage.Session) {
	if s.OrderID == nil || h.billing == nil {
		return
	}
	if _, err := h.billing.SetOrderStatus(ctx, *s.OrderID, storage.OrderPending, storage.OrderCancelled); err != nil {
		h.log.Warn("failed to cancel pending order", zap.Uint("order_id", *s.OrderID), zap.Error(err))
	}
}

// ---------------------------------------------------------------------------
// Chekni qabul qilish va tekshirish
// ---------------------------------------------------------------------------

// onPhoto rasm: to'lov kutilayotgan bo'lsa — chek sifatida tekshiriladi.
func (h *Handler) onPhoto(c tele.Context) error {
	ph := c.Message().Photo
	if ph == nil {
		return c.Send(msgTextOnly)
	}
	return h.handleReceipt(c, ph.File, fileTypePhoto)
}

// onDocument fayl: to'lov kutilayotgan bo'lsa va rasm bo'lsa — chek sifatida tekshiriladi.
func (h *Handler) onDocument(c tele.Context) error {
	d := c.Message().Document
	if d == nil {
		return c.Send(msgTextOnly)
	}
	if !strings.HasPrefix(strings.ToLower(d.MIME), "image/") {
		if h.waitingReceipt(c) {
			return c.Send(msgReceiptPhotoOnly)
		}
		return c.Send(msgTextOnly)
	}
	return h.handleReceipt(c, d.File, fileTypeDoc)
}

// waitingReceipt foydalanuvchidan chek kutilayotganini bildiradi.
func (h *Handler) waitingReceipt(c tele.Context) bool {
	if !h.paymentsOn() {
		return false
	}
	ctx, cancel := requestContext()
	defer cancel()
	s, err := h.sessions.Get(ctx, c.Sender().ID)
	return err == nil && State(s.State) == StatePayment
}

// receiptJob — tekshirilayotgan chek.
type receiptJob struct {
	userID   int64
	chatID   int64
	username string
	order    *storage.Order
	file     tele.File
	fileType string
	msgID    int
}

// handleReceipt chekni qabul qiladi: holatni atomar "payment_review" ga
// o'tkazadi (bir vaqtda yuborilgan ikki chek ikki marta tekshirilmaydi) va
// tekshiruvni alohida goroutine'da boshlaydi.
func (h *Handler) handleReceipt(c tele.Context, file tele.File, fileType string) error {
	if !h.paymentsOn() {
		return c.Send(msgTextOnly)
	}
	job, reply, err := h.acceptReceipt(c, file, fileType)
	if err != nil {
		return err
	}
	if job == nil {
		return c.Send(reply)
	}
	if err := c.Send(msgReceiptChecking); err != nil {
		h.log.Debug("failed to send checking message", zap.Error(err))
	}
	h.wg.Add(1)
	go h.checkReceipt(job)
	return nil
}

// acceptReceipt qulf ostida holatni tekshiradi. job nil bo'lsa, foydalanuvchiga reply yuboriladi.
func (h *Handler) acceptReceipt(c tele.Context, file tele.File, fileType string) (*receiptJob, string, error) {
	userID := c.Sender().ID
	defer h.lock(userID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return nil, "", err
	}
	switch State(s.State) {
	case StatePayment:
	case StateReview:
		return nil, msgReviewWait, nil
	default:
		return nil, msgTextOnly, nil
	}
	if file.FileSize > maxReceiptBytes {
		return nil, msgReceiptTooLarge, nil
	}
	if s.OrderID == nil {
		return nil, msgIdle, h.resetToIdle(ctx, s)
	}
	o, err := h.billing.GetOrder(ctx, *s.OrderID)
	if err != nil {
		return nil, "", err
	}
	if o.Status != storage.OrderPending {
		// Kutilmagan holat (masalan, buyurtma bekor qilingan): sessiyani tiklaymiz.
		// Buyurtma to'langan bo'lsa, u keyingi buyurtmada ishlatiladi (FindCredit).
		h.log.Warn("receipt for order in unexpected status", zap.Uint("order_id", o.ID), zap.String("status", o.Status))
		return nil, msgIdle, h.resetToIdle(ctx, s)
	}
	ok, err := h.sessions.CompareAndSwapState(ctx, userID, string(StatePayment), string(StateReview))
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, msgReviewWait, nil
	}
	return &receiptJob{
		userID:   userID,
		chatID:   s.ChatID,
		username: c.Sender().Username,
		order:    o,
		file:     file,
		fileType: fileType,
		msgID:    c.Message().ID,
	}, "", nil
}

func (h *Handler) resetToIdle(ctx context.Context, s *storage.Session) error {
	resetDraft(s)
	s.State = string(StateIdle)
	return h.sessions.Save(ctx, s)
}

// checkReceipt chekni yuklab oladi, AI ga o'qitadi, qaror chiqaradi va
// natijaga ko'ra generatsiyani boshlaydi, chekni adminga yuboradi yoki
// foydalanuvchidan boshqa chek so'raydi.
func (h *Handler) checkReceipt(j *receiptJob) {
	defer h.wg.Done()
	log := h.log.With(zap.Int64("user_id", j.userID), zap.Uint("order_id", j.order.ID))
	chat := &tele.Chat{ID: j.chatID}

	defer func() {
		if r := recover(); r != nil {
			log.Error("panic recovered in receipt check", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
			h.returnToPayment(j.order, msgReceiptError)
		}
	}()

	ctx, cancel := context.WithTimeout(h.jobsCtx, receiptTimeout)
	defer cancel()
	_ = h.bot.Notify(chat, tele.Typing)

	exp := payment.Expectation{Amount: j.order.Price, CardLast4: h.opts.Payment.CardLast4()}
	var (
		receipt *payment.Receipt
		aiErr   error
	)
	img, err := h.downloadFile(j.file)
	switch {
	case err != nil:
		aiErr = fmt.Errorf("chek rasmini yuklab bo'lmadi: %w", err)
	case h.verifier == nil:
		aiErr = errors.New("AI tekshiruvi ulanmagan (OPENAI_API_KEY)")
	default:
		receipt, aiErr = h.verifier.Read(ctx, img, exp)
	}
	if aiErr != nil {
		log.Warn("receipt AI check failed", zap.Error(aiErr))
	}

	txID := ""
	if receipt != nil {
		txID = textutil.TruncateHard(receipt.TransactionID.String(), 128)
	}
	seen, err := h.billing.ReceiptSeen(ctx, txID, j.file.UniqueID)
	if err != nil {
		log.Error("failed to check receipt duplicates", zap.Error(err))
		h.returnToPayment(j.order, msgReceiptError)
		return
	}

	d := payment.Decide(payment.Input{
		Receipt:        receipt,
		Err:            aiErr,
		ExpectedAmount: j.order.Price,
		CardLast4:      exp.CardLast4,
		OrderCreatedAt: j.order.CreatedAt,
		Now:            time.Now(),
		Seen:           seen,
	})

	p := &storage.Payment{
		UserID:            j.order.UserID,
		TelegramUserID:    j.order.TelegramUserID,
		Username:          j.username,
		OrderID:           j.order.ID,
		Kind:              j.order.Kind,
		Amount:            j.order.Price,
		Status:            paymentStatus(d.Verdict),
		CheckFileID:       j.file.FileID,
		CheckFileUniqueID: j.file.UniqueID,
		CheckFileType:     j.fileType,
		CheckMessageID:    j.msgID,
		TransactionID:     txID,
		DetectedPaidAt:    d.PaidAt,
	}
	reasons := d.Reasons
	if receipt != nil {
		p.DetectedAmount = receipt.Amount.Int()
		p.DetectedLast4 = textutil.LastDigits(receipt.CardLast4.String(), 4)
		p.AIRaw = receipt.Raw
		if n := strings.TrimSpace(receipt.Notes); n != "" && d.Verdict != payment.Approved {
			reasons = append(reasons, "Tekshiruv izohi: "+n)
		}
	}
	p.Reason = textutil.TruncateHard(strings.Join(reasons, "\n"), 1000)

	if err := h.billing.SubmitPayment(ctx, p); err != nil {
		log.Error("failed to save payment", zap.Error(err))
		h.returnToPayment(j.order, msgReceiptError)
		return
	}
	log.Info("receipt checked",
		zap.Uint("payment_id", p.ID),
		zap.String("verdict", string(d.Verdict)),
		zap.Int("expected", p.Amount),
		zap.Int("detected", p.DetectedAmount),
		zap.Strings("reasons", d.Reasons))

	switch d.Verdict {
	case payment.Approved:
		if err := h.startPaidJob(j.userID, j.order.ID, msgPaymentApproved); err != nil {
			log.Error("failed to start paid job", zap.Error(err))
		}
	case payment.Suspicious:
		if _, err := h.bot.Send(chat, msgReceiptToAdmin); err != nil {
			log.Warn("failed to notify user about review", zap.Error(err))
		}
	default:
		h.returnToPayment(j.order, msgReceiptInvalid)
	}
	// Har bir chek (tasdiqlangan, shubhali yoki rad etilgan) adminlarga
	// yuboriladi; shubhalisi — ✅/❌ tugmalari bilan.
	h.notifyPayment(p, j.order)
}

func paymentStatus(v payment.Verdict) string {
	switch v {
	case payment.Approved:
		return storage.PaymentApproved
	case payment.Suspicious:
		return storage.PaymentReview
	}
	return storage.PaymentRejected
}

// downloadFile Telegram serveridan faylni yuklab oladi (hajmi cheklangan).
func (h *Handler) downloadFile(f tele.File) ([]byte, error) {
	rc, err := h.bot.File(&f)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxReceiptBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReceiptBytes {
		return nil, errors.New("fayl juda katta")
	}
	return data, nil
}

// startPaidJob to'lov tasdiqlangach generatsiyani boshlaydi. Foydalanuvchi
// endi shu buyurtmani kutmayotgan bo'lsa, to'lov keyingi buyurtma uchun
// saqlanib qoladi (FindCredit).
func (h *Handler) startPaidJob(userID int64, orderID uint, notice string) error {
	defer h.lock(userID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.sessions.Get(ctx, userID)
	if err != nil {
		return err
	}
	chat := &tele.Chat{ID: s.ChatID}
	if State(s.State) != StateReview || s.OrderID == nil || *s.OrderID != orderID {
		_, err := h.bot.Send(chat, msgCreditSaved)
		return err
	}
	ok, err := h.sessions.CompareAndSwapState(ctx, userID, string(StateReview), string(StateGenerating))
	if err != nil || !ok {
		return err
	}
	s.State = string(StateGenerating)

	waitMsg := msgGeneratingDeck
	if ai.WorkType(s.WorkType).IsDocument() {
		waitMsg = msgGeneratingDoc
	}
	if _, err := h.bot.Send(chat, notice+"\n\n"+waitMsg); err != nil {
		h.log.Warn("failed to send payment approved message", zap.Int64("user_id", userID), zap.Error(err))
	}
	// Vazifa qulf ochilishidan oldin ro'yxatga olinadi (/cancel uni topishi uchun).
	h.startJob(s)
	return nil
}

// returnToPayment foydalanuvchini yana chek yuborish bosqichiga qaytaradi
// (chek emas, admin rad etdi yoki texnik xato). Foydalanuvchi bu buyurtmani
// endi kutmayotgan bo'lsa, buyurtma bekor qilinadi.
func (h *Handler) returnToPayment(o *storage.Order, text string) {
	userID := o.TelegramUserID
	defer h.lock(userID)()
	ctx, cancel := requestContext()
	defer cancel()
	log := h.log.With(zap.Int64("user_id", userID), zap.Uint("order_id", o.ID))

	s, err := h.sessions.Get(ctx, userID)
	if err != nil {
		log.Error("failed to load session", zap.Error(err))
		return
	}
	chat := &tele.Chat{ID: s.ChatID}
	if State(s.State) == StateReview && s.OrderID != nil && *s.OrderID == o.ID {
		if _, err := h.sessions.CompareAndSwapState(ctx, userID, string(StateReview), string(StatePayment)); err != nil {
			log.Error("failed to return to payment state", zap.Error(err))
		}
		if _, err := h.bot.Send(chat, text, paymentKeyboard()); err != nil {
			log.Warn("failed to send message", zap.Error(err))
		}
		return
	}
	if _, err := h.billing.SetOrderStatus(ctx, o.ID, storage.OrderPending, storage.OrderCancelled); err != nil {
		log.Warn("failed to cancel order", zap.Error(err))
	}
	if _, err := h.bot.Send(chat, fmt.Sprintf("❌ Buyurtma #%d bo'yicha chek tasdiqlanmadi.", o.ID)); err != nil {
		log.Warn("failed to send message", zap.Error(err))
	}
}

// ---------------------------------------------------------------------------
// Admin tekshiruvi
// ---------------------------------------------------------------------------

func (h *Handler) onPayApprove(c tele.Context) error { return h.onPayDecision(c, true) }
func (h *Handler) onPayReject(c tele.Context) error  { return h.onPayDecision(c, false) }

// onPayDecision admin ✅/❌ tugmasini bosganda chaqiriladi.
func (h *Handler) onPayDecision(c tele.Context, approve bool) error {
	if h.billing == nil {
		return c.Respond()
	}
	ctx, cancel := requestContext()
	defer cancel()

	adminID := c.Sender().ID
	isAdmin, err := h.billing.IsAdmin(ctx, adminID)
	if err != nil {
		return err
	}
	if !isAdmin {
		return c.Respond(&tele.CallbackResponse{Text: "Bu amal faqat admin uchun."})
	}
	id, err := strconv.ParseUint(c.Data(), 10, 64)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}

	p, o, err := h.billing.ReviewPayment(ctx, uint(id), approve, adminID)
	if errors.Is(err, storage.ErrConflict) {
		if cur, gerr := h.billing.GetPayment(ctx, uint(id)); gerr == nil {
			order, _ := h.billing.GetOrder(ctx, cur.OrderID)
			h.editCaption(c, reviewCaption(cur, order))
		}
		return c.Respond(&tele.CallbackResponse{Text: "Bu chek allaqachon ko'rib chiqilgan."})
	}
	if err != nil {
		return err
	}

	h.log.Info("payment reviewed by admin",
		zap.Uint("payment_id", p.ID), zap.Uint("order_id", o.ID),
		zap.Int64("admin_id", adminID), zap.Bool("approved", approve))
	if approve {
		_ = c.Respond(&tele.CallbackResponse{Text: "Tasdiqlandi ✅"})
	} else {
		_ = c.Respond(&tele.CallbackResponse{Text: "Rad etildi ❌"})
	}
	h.editCaption(c, reviewCaption(p, o))

	if approve {
		return h.startPaidJob(o.TelegramUserID, o.ID, msgPaymentByAdmin)
	}
	h.returnToPayment(o, msgReceiptRejected)
	return nil
}

// editCaption rasm/fayl xabari izohini yangilaydi (inline tugmalar olib tashlanadi).
func (h *Handler) editCaption(c tele.Context, caption string) {
	if err := c.EditCaption(caption); err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to edit caption", zap.Error(err))
	}
}

// checkMedia chek rasmini Telegram file_id orqali qayta yuborish uchun tayyorlaydi.
func checkMedia(p *storage.Payment, caption string) tele.Sendable {
	f := tele.File{FileID: p.CheckFileID}
	if p.CheckFileType == fileTypeDoc {
		return &tele.Document{File: f, Caption: caption}
	}
	return &tele.Photo{File: f, Caption: caption}
}

func reviewKeyboard(paymentID uint) *tele.ReplyMarkup {
	id := strconv.FormatUint(uint64(paymentID), 10)
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("✅ Tasdiqlash", cbPayApprove, id), m.Data("❌ Rad etish", cbPayReject, id)))
	return m
}

// reviewCaption chek haqida admin uchun ma'lumot (Telegram izoh chegarasi — 1024 belgi).
func reviewCaption(p *storage.Payment, o *storage.Order) string {
	e := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, "🧾 <b>Chek #%d</b> · %s\n", p.ID, paymentStatusLabel(p.Status))
	user := fmt.Sprintf("ID %d · userId %d", p.TelegramUserID, p.UserID)
	if p.Username != "" {
		user = "@" + e(p.Username) + " · " + user
	}
	fmt.Fprintf(&b, "👤 %s\n", user)
	if o != nil {
		fmt.Fprintf(&b, "📌 Buyurtma #%d: %s%s\n📝 %s\n", o.ID, e(orderLabel(o.WorkType, o.Kind)),
			volumeSuffix(o.WorkType, o.Volume), e(textutil.TruncateHard(o.Topic, 80)))
	}
	fmt.Fprintf(&b, "💰 Kutilgan summa: <b>%s</b>\n", formatSum(p.Amount))

	detected := []string{"summa: " + orDash(p.DetectedAmount > 0, formatSum(p.DetectedAmount))}
	detected = append(detected, "karta: "+orDash(p.DetectedLast4 != "", "****"+e(p.DetectedLast4)))
	if p.DetectedPaidAt != nil {
		detected = append(detected, p.DetectedPaidAt.In(tz.Tashkent).Format("02.01.2006 15:04"))
	}
	fmt.Fprintf(&b, "🔎 Chekda: %s\n", strings.Join(detected, " · "))
	if p.TransactionID != "" {
		fmt.Fprintf(&b, "🔖 Tranzaksiya: <code>%s</code>\n", e(textutil.TruncateHard(p.TransactionID, 40)))
	}
	fmt.Fprintf(&b, "🕒 Yuborilgan: %s\n", p.CreatedAt.In(tz.Tashkent).Format("02.01.2006 15:04"))

	var tail string
	switch {
	case p.ReviewedAt != nil && p.ReviewedBy != nil:
		tail = fmt.Sprintf("\n%s — admin %d, %s", paymentStatusLabel(p.Status), *p.ReviewedBy,
			p.ReviewedAt.In(tz.Tashkent).Format("02.01.2006 15:04"))
	case p.Status == storage.PaymentApproved:
		tail = "\n🤖 Avtomatik tasdiqlandi — ish tayyorlanmoqda."
	case p.Status == storage.PaymentRejected:
		tail = "\n🤖 Avtomatik rad etildi — foydalanuvchidan boshqa chek so'raldi."
	case p.Status == storage.PaymentReview:
		tail = "\n👇 Tasdiqlang yoki rad eting."
	}

	// Sabablar izohning qolgan joyiga sig'adigan qilib qisqartiriladi: izoh
	// chegaradan oshsa, Telegram uni rad etadi va chek adminga umuman bormaydi.
	if p.Reason != "" {
		const header = "\n⚠️ <b>Sabablar:</b>\n"
		left := maxCaptionUnits - visibleLen(b.String()+header+tail)
		if left > 3 {
			b.WriteString(header)
			for _, r := range strings.Split(p.Reason, "\n") {
				if left <= 3 {
					break
				}
				r = truncateUnits(r, left-3) // "• " va qator oxiri uchun 3 birlik
				b.WriteString("• " + e(r) + "\n")
				left -= utf16Len(r) + 3
			}
		}
	}
	b.WriteString(tail)
	return strings.TrimRight(b.String(), "\n")
}

// maxCaptionUnits — rasm izohi uzunligi chegarasi (Telegram'da 1024),
// zaxira bilan.
const maxCaptionUnits = 1000

// visibleLen HTML matn Telegram'da ko'rinadigan uzunligi: teglar olib
// tashlanadi, HTML belgilari (&amp; ...) ochiladi va UTF-16 birliklarida
// sanaladi (Telegram chegaralari shu birliklarda; emoji — 2 birlik).
func visibleLen(s string) int {
	var plain strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>' && inTag:
			inTag = false
		case !inTag:
			plain.WriteRune(r)
		}
	}
	return utf16Len(html.UnescapeString(plain.String()))
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// truncateUnits oddiy matnni (HTML emas) n ta UTF-16 birligigacha qisqartiradi.
func truncateUnits(s string, n int) string {
	if utf16Len(s) <= n {
		return s
	}
	used := 1 // "…"
	for i, r := range s {
		if used+utf16.RuneLen(r) > n {
			return s[:i] + "…"
		}
		used += utf16.RuneLen(r)
	}
	return s
}

// volumeSuffix buyurtma hajmi: " · 20 slayd" yoki " · 12 sahifa" (hajm noma'lum bo'lsa — bo'sh).
func volumeSuffix(workType string, volume int) string {
	switch {
	case volume <= 0:
		return ""
	case ai.WorkType(workType).IsDocument():
		return fmt.Sprintf(" · %d sahifa", volume)
	}
	return fmt.Sprintf(" · %d slayd", volume)
}

func paymentStatusLabel(status string) string {
	switch status {
	case storage.PaymentApproved:
		return "✅ tasdiqlangan"
	case storage.PaymentReview:
		return "⏳ tekshiruvda"
	case storage.PaymentRejected:
		return "❌ rad etilgan"
	}
	return status
}

func orDash(ok bool, v string) string {
	if ok {
		return v
	}
	return "—"
}

// ---------------------------------------------------------------------------
// Qayta ishga tushganda tiklash
// ---------------------------------------------------------------------------

// recoverReviews chek tekshiruvi o'rtasida uzilib qolgan sessiyalarni tiklaydi:
// chek hali saqlanmagan bo'lsa — foydalanuvchidan qayta yuborish so'raladi;
// to'lov tasdiqlangan, lekin generatsiya boshlanmagan bo'lsa — "Qayta urinish"
// tugmasi yuboriladi (to'langan buyurtma FindCredit orqali topiladi).
func (h *Handler) recoverReviews(ctx context.Context) {
	if h.billing == nil {
		return
	}
	list, err := h.sessions.ListByState(ctx, string(StateReview))
	if err != nil {
		h.log.Error("failed to list sessions in review", zap.Error(err))
		return
	}
	for _, s := range list {
		chat := &tele.Chat{ID: s.ChatID}
		if s.OrderID == nil {
			_, _ = h.sessions.CompareAndSwapState(ctx, s.UserID, string(StateReview), string(StateIdle))
			continue
		}
		o, err := h.billing.GetOrder(ctx, *s.OrderID)
		if err != nil {
			h.log.Warn("failed to load order for review recovery", zap.Uint("order_id", *s.OrderID), zap.Error(err))
			continue
		}
		var (
			to     State
			text   string
			markup *tele.ReplyMarkup
		)
		switch o.Status {
		case storage.OrderPending:
			to, text, markup = StatePayment, msgReceiptInterrupted, paymentKeyboard()
		case storage.OrderPaid:
			to, text, markup = StateConfirm, msgInterrupted, retryKeyboard()
		default:
			continue // admin qarorini kutmoqda
		}
		ok, err := h.sessions.CompareAndSwapState(ctx, s.UserID, string(StateReview), string(to))
		if err != nil || !ok {
			continue
		}
		if _, err := h.bot.Send(chat, text, markup); err != nil {
			h.log.Warn("failed to notify user about interrupted receipt check", zap.Int64("user_id", s.UserID), zap.Error(err))
		}
	}
}

// ---------------------------------------------------------------------------
// Formatlash
// ---------------------------------------------------------------------------

// formatSum summani "10 000 so'm" ko'rinishida qaytaradi.
func formatSum(n int) string {
	return groupDigits(strconv.Itoa(n), 3, true) + " so'm"
}

// formatCard karta raqamini 4 tadan guruhlaydi: "8600 1234 5678 9012".
func formatCard(card string) string {
	return groupDigits(textutil.LastDigits(card, 32), 4, false)
}

// groupDigits raqamlarni bo'sh joy bilan guruhlaydi: fromRight — o'ngdan (summalar uchun).
func groupDigits(d string, size int, fromRight bool) string {
	neg := strings.HasPrefix(d, "-")
	d = strings.TrimPrefix(d, "-")
	var parts []string
	if fromRight {
		for len(d) > size {
			parts = append([]string{d[len(d)-size:]}, parts...)
			d = d[:len(d)-size]
		}
		parts = append([]string{d}, parts...)
	} else {
		for len(d) > size {
			parts = append(parts, d[:size])
			d = d[size:]
		}
		parts = append(parts, d)
	}
	out := strings.Join(parts, " ")
	if neg {
		out = "-" + out
	}
	return out
}
