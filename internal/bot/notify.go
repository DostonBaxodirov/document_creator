package bot

// Adminlarga xabarlar: har bir chek (to'lov) — rasmi va tekshiruv natijasi
// bilan, har bir xarid — fayl yetkazilganda (tayyor faylning o'zi bilan) yoki
// buyurtma tayyorlanmay qolganda buyurtmaning to'liq tafsilotlari va to'lov
// ma'lumoti bilan.

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/textutil"
	"telegram-presentation-bot/internal/tz"
)

// maxNoticeErrRunes — admin xabaridagi xato matni uzunligi.
const maxNoticeErrRunes = 500

// toAdmins xabarni barcha adminlarga yuboradi va nechta adminga yetkazilganini
// qaytaradi. Xatolar logga yoziladi — foydalanuvchi oqimi to'xtamaydi.
func (h *Handler) toAdmins(what any, opts ...any) int {
	return h.eachAdmin(func(chat *tele.Chat) error {
		_, err := h.bot.Send(chat, what, opts...)
		return err
	})
}

// purchaseToAdmins xarid xabarini va unga javob sifatida foydalanuvchiga
// yuborilgan faylni barcha adminlarga yuboradi. Fayl copyMessage bilan
// foydalanuvchi chatidagi xabardan nusxalanadi — qayta yuklanmaydi va
// saqlanmaydi. Faylni nusxalab bo'lmasa, xabarning o'zi baribir yetkaziladi.
func (h *Handler) purchaseToAdmins(notice string, file *tele.StoredMessage, log *zap.Logger) int {
	return h.eachAdmin(func(chat *tele.Chat) error {
		msg, err := h.bot.Send(chat, notice)
		if err != nil {
			return err
		}
		if file == nil {
			return nil
		}
		// Ovozsiz: xabarning o'zi allaqachon bildirishnoma berdi.
		opts := &tele.SendOptions{ReplyTo: msg, AllowWithoutReply: true, DisableNotification: true}
		if _, err := h.bot.Copy(chat, file, opts); err != nil {
			log.Warn("failed to copy delivered file to admin", zap.Int64("admin_id", chat.ID), zap.Error(err))
		}
		return nil
	})
}

// eachAdmin send ni har bir admin chati uchun chaqiradi va muvaffaqiyatlilar
// sonini qaytaradi. Xatolar logga yoziladi.
func (h *Handler) eachAdmin(send func(chat *tele.Chat) error) int {
	if h.billing == nil || h.bot == nil {
		return 0
	}
	ctx, cancel := requestContext()
	defer cancel()
	admins, err := h.billing.ListAdmins(ctx)
	if err != nil {
		h.log.Error("failed to list admins", zap.Error(err))
		return 0
	}
	sent := 0
	for _, a := range admins {
		if err := send(&tele.Chat{ID: a.TelegramUserID}); err != nil {
			h.log.Warn("failed to notify admin", zap.Int64("admin_id", a.TelegramUserID), zap.Error(err))
			continue
		}
		sent++
	}
	return sent
}

// notifyPayment chekni rasmi va tekshiruv natijasi bilan barcha adminlarga
// yuboradi. Shubhali chekka ✅/❌ tugmalari qo'shiladi.
func (h *Handler) notifyPayment(p *storage.Payment, o *storage.Order) {
	var opts []any
	if p.Status == storage.PaymentReview {
		opts = append(opts, reviewKeyboard(p.ID))
	}
	if h.toAdmins(checkMedia(p, reviewCaption(p, o)), opts...) == 0 && p.Status == storage.PaymentReview {
		h.log.Warn("no admin received payment for review", zap.Uint("payment_id", p.ID))
	}
}

// purchase — admin xabari uchun xarid ma'lumotlari.
type purchase struct {
	session *storage.Session
	order   *storage.Order   // nil — to'lov tizimi o'chirilgan yoki buyurtma o'qilmadi
	payment *storage.Payment // pullik buyurtmaning tasdiqlangan cheki
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// freeUsed  int              // bepul buyurtmada: ishlatilgan bepul urinishlar (shu buyurtma bilan)
	// freeLimit int
}

// loadPurchase sessiyadagi buyurtmani va uning to'lovini o'qiydi. Xatolar
// logga yoziladi — xabar topilgan ma'lumotlar bilan baribir yuboriladi.
func (h *Handler) loadPurchase(ctx context.Context, s *storage.Session, log *zap.Logger) purchase {
	pc := purchase{session: s}
	if s.OrderID == nil || h.billing == nil {
		return pc
	}
	o, err := h.billing.GetOrder(ctx, *s.OrderID)
	if err != nil {
		log.Warn("failed to load order for admin notice", zap.Uint("order_id", *s.OrderID), zap.Error(err))
		return pc
	}
	pc.order = o
	// Bepul buyurtmalar endi yaratilmaydi, lekin bazada oldingi (hali
	// yetkazilmagan) bepul buyurtmalar qolgan bo'lishi mumkin — ularda chek yo'q.
	if o.IsFree {
		// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
		// pc.freeLimit = h.freeLimit(o.Kind)
		// if pc.freeUsed, err = h.billing.CountFreeUsed(ctx, o.UserID, o.Kind); err != nil {
		// 	log.Warn("failed to count free orders for admin notice", zap.Error(err))
		// }
		return pc
	}
	if pc.payment, err = h.billing.OrderPayment(ctx, o.ID); err != nil {
		log.Warn("failed to load order payment for admin notice", zap.Uint("order_id", o.ID), zap.Error(err))
	}
	return pc
}

// purchaseNotice yetkazilgan ish haqida admin xabari (ombor ulanmagan bo'lsa — bo'sh).
func (h *Handler) purchaseNotice(ctx context.Context, s *storage.Session, out *output, took time.Duration, log *zap.Logger) string {
	if h.billing == nil {
		return ""
	}
	return purchaseText(h.loadPurchase(ctx, s, log), out, took, time.Now())
}

// failureNotice tayyorlanmay qolgan buyurtma haqida admin xabari. Buyurtmasiz
// (to'lov o'chirilgan) ishlar uchun — bo'sh.
func (h *Handler) failureNotice(ctx context.Context, s *storage.Session, jobErr error, log *zap.Logger) string {
	if h.billing == nil || s.OrderID == nil {
		return ""
	}
	return failureText(h.loadPurchase(ctx, s, log), jobErr)
}

// purchaseText: mijoz, buyurtmaning barcha tafsilotlari, to'lov, natija va vaqt.
func purchaseText(pc purchase, out *output, took time.Duration, now time.Time) string {
	e := html.EscapeString
	var b strings.Builder
	if pc.order != nil {
		fmt.Fprintf(&b, "🛒 <b>Xarid — buyurtma #%d</b> · ✅ yetkazildi\n", pc.order.ID)
	} else {
		b.WriteString("🛒 <b>Yangi ish</b> · ✅ yetkazildi\n")
	}
	b.WriteString(customerLine(pc.session.UserID, pc.session.Username) + "\n\n")
	b.WriteString(orderDetails(pc.session, maxRequirementsRunes) + "\n\n")
	b.WriteString(paymentLine(pc) + "\n")
	if out != nil {
		fmt.Fprintf(&b, "📦 <b>Natija:</b> %s · %s\n", e(out.FileName), e(out.Summary))
	}
	fmt.Fprintf(&b, "⏱ <b>Tayyorlandi:</b> %s · 🕒 %s", formatDuration(took),
		now.In(tz.Tashkent).Format(dateTimeLayout))
	return b.String()
}

// failureText: qaysi buyurtma, kim uchun, qancha to'langan va xato sababi.
func failureText(pc purchase, jobErr error) string {
	s := pc.session
	e := html.EscapeString
	kind := orderKind(s)
	if pc.order != nil {
		kind = pc.order.Kind
	}
	errText := "noma'lum xato"
	if jobErr != nil {
		errText = jobErr.Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "⚠️ <b>Buyurtma #%d tayyorlanmadi</b>\n", *s.OrderID)
	b.WriteString(customerLine(s.UserID, s.Username) + "\n")
	fmt.Fprintf(&b, "📌 %s%s\n📝 %s\n", e(orderLabel(s.WorkType, kind)), volumeSuffix(s.WorkType, s.Volume),
		e(textutil.TruncateHard(s.Topic, 200)))
	b.WriteString(paymentLine(pc) + "\n")
	fmt.Fprintf(&b, "❗ <b>Xato:</b> <code>%s</code>\n\n", e(textutil.TruncateHard(errText, maxNoticeErrRunes)))
	b.WriteString("Foydalanuvchiga «Qayta urinish» tugmasi yuborildi, to'lovi saqlanib qoladi.")
	return b.String()
}

// customerLine: "👤 Mijoz: @user · ID 123 · profil" (profil — Telegram'dagi sahifasiga havola).
func customerLine(userID int64, username string) string {
	user := fmt.Sprintf("ID <code>%d</code> · <a href=\"tg://user?id=%d\">profil</a>", userID, userID)
	if username != "" {
		user = "@" + html.EscapeString(username) + " · " + user
	}
	return "👤 <b>Mijoz:</b> " + user
}

// paymentLine buyurtma qanday to'langani: bepul sinov, chek (kim tasdiqlagan)
// yoki to'lov tizimi o'chirilgan.
func paymentLine(pc purchase) string {
	const label = "💰 <b>To'lov:</b> "
	o, p := pc.order, pc.payment
	switch {
	case o == nil && pc.session.OrderID != nil:
		return label + fmt.Sprintf("buyurtma #%d ma'lumotini o'qib bo'lmadi", *pc.session.OrderID)
	case o == nil:
		return label + "to'lov tizimi o'chirilgan (bepul)"
	case o.IsFree: // bepul urinishlar o'chirilishidan oldin yaratilgan buyurtma
		line := label + "🎁 bepul sinov"
		// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
		// if pc.freeLimit > 0 && pc.freeUsed > 0 {
		// 	line += fmt.Sprintf(" (%d/%d)", pc.freeUsed, pc.freeLimit)
		// }
		return line
	case p == nil:
		return label + "<b>" + formatSum(o.Price) + "</b> · to'langan"
	}
	line := fmt.Sprintf("%s<b>%s</b> · chek #%d · %s", label, formatSum(p.Amount), p.ID, approvedBy(p))
	if p.DetectedAmount > 0 && p.DetectedAmount != p.Amount {
		line += " · chekda " + formatSum(p.DetectedAmount)
	}
	return line + fmt.Sprintf(" · /chek_%d", p.ID)
}

func approvedBy(p *storage.Payment) string {
	if p.ReviewedBy != nil {
		return fmt.Sprintf("admin %d tasdiqlagan", *p.ReviewedBy)
	}
	return "avtomatik tasdiqlangan"
}
