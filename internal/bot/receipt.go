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
	"strings"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/payment"
	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/textutil"
)

const (
	maxReceiptBytes = 10 << 20        // chek rasmi hajmi chegarasi
	receiptTimeout  = 3 * time.Minute // chekni yuklab olish va AI tekshiruvi uchun
	fileTypePhoto   = "photo"
	fileTypeDoc     = "document"
)

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
	if o.Bonus() {
		fmt.Fprintf(&b, "\n🎁 <b>Bonus:</b> −%s <i>(to'liq narx %s)</i>", formatSum(o.Discount), formatSum(o.Value()))
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

// sendPaymentInfo to'lov kutilayotganda (matn yuborilganda) ko'rsatmani qayta yuboradi.
func (h *Handler) sendPaymentInfo(ctx context.Context, c tele.Context, s *storage.Session) error {
	if s.OrderID != nil {
		if o, err := h.billing.GetOrder(ctx, *s.OrderID); err == nil {
			return c.Send(h.paymentText(o, false), paymentKeyboard())
		}
	}
	// Buyurtma topilmadi — xavfsiz tiklash.
	if err := h.resetToIdle(ctx, s); err != nil {
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
			ok, err := h.cas(ctx, s.UserID, StateReview, StateConfirm)
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
	ok, err := h.cas(ctx, userID, StatePayment, StateReview)
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
		// Taklif qilinganning to'lovi — taklif qilganga mukofot (ish boshlangach).
		h.rewardReferrer(j.order)
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
	ok, err := h.cas(ctx, userID, StateReview, StateGenerating)
	if err != nil || !ok {
		return err
	}
	s.State = string(StateGenerating)

	if _, err := h.bot.Send(chat, notice+"\n\n"+waitMessage(s.WorkType)); err != nil {
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
		if _, err := h.cas(ctx, userID, StateReview, StatePayment); err != nil {
			log.Error("failed to return to payment state", zap.Error(err))
		}
		if _, err := h.bot.Send(chat, text, paymentKeyboard()); err != nil {
			log.Warn("failed to send message", zap.Error(err))
		}
		return
	}
	// Bonus bilan qisman to'lanadigan buyurtmada bonus qaytariladi.
	if _, err := h.billing.ReleaseOrder(ctx, o.ID); err != nil {
		log.Warn("failed to cancel order", zap.Error(err))
	}
	if _, err := h.bot.Send(chat, fmt.Sprintf("❌ Buyurtma #%d bo'yicha chek tasdiqlanmadi.", o.ID)); err != nil {
		log.Warn("failed to send message", zap.Error(err))
	}
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
	list, err := h.listByState(ctx, StateReview)
	if err != nil {
		h.log.Error("failed to list sessions in review", zap.Error(err))
		return
	}
	for _, s := range list {
		chat := &tele.Chat{ID: s.ChatID}
		if s.OrderID == nil {
			_, _ = h.cas(ctx, s.UserID, StateReview, StateIdle)
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
		ok, err := h.cas(ctx, s.UserID, StateReview, to)
		if err != nil || !ok {
			continue
		}
		if _, err := h.bot.Send(chat, text, markup); err != nil {
			h.log.Warn("failed to notify user about interrupted receipt check", zap.Int64("user_id", s.UserID), zap.Error(err))
		}
	}
}
