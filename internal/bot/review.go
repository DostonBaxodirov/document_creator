package bot

import (
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"unicode/utf16"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/textutil"
	"telegram-presentation-bot/internal/tz"
)

// ---------------------------------------------------------------------------
// Admin tekshiruvi
// ---------------------------------------------------------------------------

func (h *Handler) onPayApprove(c tele.Context) error { return h.onPayDecision(c, true) }
func (h *Handler) onPayReject(c tele.Context) error  { return h.onPayDecision(c, false) }

// onPayDecision admin ✅/❌ tugmasini bosganda chaqiriladi.
func (h *Handler) onPayDecision(c tele.Context, approve bool) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	ctx, cancel := requestContext()
	defer cancel()

	adminID := c.Sender().ID
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
		err := h.startPaidJob(o.TelegramUserID, o.ID, msgPaymentByAdmin)
		h.rewardReferrer(o) // taklif qilganga mukofot (ish boshlangach)
		return err
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
	if o != nil && o.Bonus() {
		fmt.Fprintf(&b, "🎁 Taklif bonusi: −%s (to'liq narx %s)\n", formatSum(o.Discount), formatSum(o.Value()))
	}

	detected := []string{"summa: " + orDash(p.DetectedAmount > 0, formatSum(p.DetectedAmount))}
	detected = append(detected, "karta: "+orDash(p.DetectedLast4 != "", "****"+e(p.DetectedLast4)))
	if p.DetectedPaidAt != nil {
		detected = append(detected, p.DetectedPaidAt.In(tz.Tashkent).Format(dateTimeLayout))
	}
	fmt.Fprintf(&b, "🔎 Chekda: %s\n", strings.Join(detected, " · "))
	if p.TransactionID != "" {
		fmt.Fprintf(&b, "🔖 Tranzaksiya: <code>%s</code>\n", e(textutil.TruncateHard(p.TransactionID, 40)))
	}
	fmt.Fprintf(&b, "🕒 Yuborilgan: %s\n", p.CreatedAt.In(tz.Tashkent).Format(dateTimeLayout))

	var tail string
	switch {
	case p.ReviewedAt != nil && p.ReviewedBy != nil:
		tail = fmt.Sprintf("\n%s — admin %d, %s", paymentStatusLabel(p.Status), *p.ReviewedBy,
			p.ReviewedAt.In(tz.Tashkent).Format(dateTimeLayout))
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
