package bot

// Do'stlarni taklif qilish (referal) tizimi.
//
// Har bir foydalanuvchining shaxsiy havolasi bor: t.me/<bot>?start=ref_<Telegram ID>.
// Botga birinchi marta shu havola orqali kirgan foydalanuvchi uni taklif
// qilganga bog'lanadi (bitta foydalanuvchini faqat bir kishi taklif qilgan
// bo'lishi mumkin). Taklif qilingan foydalanuvchi buyurtmani haqiqiy pul
// bilan to'laganda (chek tasdiqlanganda) taklif qilganga 1 ta bepul imkoniyat
// (bonus) beriladi — har bir do'st uchun faqat bir marta, do'stlar soni
// cheklanmagan.
//
// Bonus tasdiqlash bosqichida foydalanuvchi tanlovi bilan ishlatiladi:
// tayyor dizayndagi istalgan ishni to'liq qoplaydi, ✨ maxsus dizaynda —
// Referral.CustomMax so'mgacha (qolganini foydalanuvchi to'laydi).
// Buyurtma bekor qilinsa yoki tayyorlanmasdan tashlab ketilsa, bonus
// balansga qaytadi (storage.ReleaseOrder).

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/storage"
)

// refPrefix — /start parametridagi taklif havolasi belgisi: "ref_<Telegram ID>".
const refPrefix = "ref_"

// shareText — "Do'stlarga yuborish" tugmasi bilan havolaga qo'shiladigan matn.
const shareText = "Taqdimot, mustaqil ish, referat va amaliy topshiriqni bir necha daqiqada tayyorlab beradigan bot 👇"

// Taklif tizimi xabarlari.
const (
	msgReferralOff     = "Do'stlarni taklif qilish hozircha mavjud emas."
	msgOwnReferralLink = "🔗 Bu sizning shaxsiy taklif havolangiz — uni do'stlaringizga yuboring. " +
		"Ular shu havola orqali botga kirib, buyurtmasini to'lasa, sizga 🎁 bepul hujjat beriladi. Batafsil — /taklif"
	msgReferralJoined = "👋 Havolangiz orqali yangi do'st qo'shildi: <b>%s</b>.\n\n" +
		"U birinchi buyurtmasini to'lagach, sizga 🎁 <b>1 ta bepul hujjat</b> beriladi."
	msgReferralRewarded = "🎉 <b>Taklifingiz ishladi!</b> Do'stingiz buyurtmasini to'ladi — sizga 🎁 <b>1 ta bepul hujjat</b> berildi.\n\n" +
		"Bepul imkoniyatlaringiz: <b>%d ta</b>. Ishlatish uchun buyurtmani tasdiqlashda «🎁 Bonus bilan tasdiqlash» tugmasini bosing — /start"
	msgNoCredits   = "Bepul imkoniyatingiz qolmagan. Buyurtmani to'lab tasdiqlashingiz mumkin."
	referralButton = "🎁 Do'stlarni taklif qilish — bepul hujjat oling"
)

// referralOn taklif tizimi yoqilganini bildiradi (faqat to'lov yoqilgan bo'lsa).
func (h *Handler) referralOn() bool {
	return h.paymentsOn() && h.opts.Referral.Enabled
}

// referralLink foydalanuvchining shaxsiy taklif havolasi. Bot nomi hali
// ma'lum bo'lmasa (bot ulanmagan) — bo'sh satr.
func (h *Handler) referralLink(telegramID int64) string {
	if h.bot == nil || h.bot.Me == nil || h.bot.Me.Username == "" {
		return ""
	}
	return referralLinkFor(h.bot.Me.Username, telegramID)
}

func referralLinkFor(botUsername string, telegramID int64) string {
	return "https://t.me/" + botUsername + "?start=" + refPrefix + strconv.FormatInt(telegramID, 10)
}

// parseRefPayload /start parametridan taklif qilgan foydalanuvchining
// Telegram ID sini ajratadi ("ref_123" → 123).
func parseRefPayload(payload string) (int64, bool) {
	v, ok := strings.CutPrefix(strings.TrimSpace(payload), refPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// shareURL havolani Telegram'da do'stlarga yuborish oynasini ochadigan manzil.
func shareURL(link string) string {
	return "https://t.me/share/url?url=" + url.QueryEscape(link) + "&text=" + url.QueryEscape(shareText)
}

// bonusCover bonus buyurtmaning qancha qismini qoplashi: maxsus dizaynda —
// Referral.CustomMax so'mgacha, tayyor dizaynlarda — to'liq narx.
func (h *Handler) bonusCover(kind string, total int) int {
	if _, custom := storage.KindTariff(kind); custom {
		return max(0, min(total, h.opts.Referral.CustomMax))
	}
	return total
}

// bonusTerms bonus nimani qoplashi haqida qisqa matn (HTML).
func (h *Handler) bonusTerms() string {
	s := "✅ Tayyor dizayndagi istalgan ish (taqdimot, mustaqil ish, referat, amaliy topshiriq) — <b>to'liq bepul</b>"
	if h.customOn() {
		if m := h.opts.Referral.CustomMax; m > 0 {
			s += fmt.Sprintf("\n✨ Maxsus dizaynda — <b>%s gacha</b> bepul (qolganini to'laysiz)", formatSum(m))
		} else {
			s += "\n✨ Maxsus dizayn bonusga kirmaydi"
		}
	}
	return s
}

// referralText /taklif ekrani: shartlar, havola va foydalanuvchi ko'rsatkichlari.
func (h *Handler) referralText(link string, invited, rewarded, credits int) string {
	var b strings.Builder
	b.WriteString("🎁 <b>Do'stlaringizni taklif qiling — bepul hujjat oling!</b>\n\n")
	b.WriteString("Havolangizni do'stlaringizga yuboring. Do'stingiz botga shu havola orqali birinchi marta kirib, " +
		"buyurtmasini to'lasa — sizga <b>1 ta bepul hujjat</b> beriladi. Cheklov yo'q: nechta do'st — shuncha bepul hujjat.\n\n")
	b.WriteString(h.bonusTerms() + "\n\n")
	fmt.Fprintf(&b, "🔗 <b>Sizning havolangiz:</b>\n<code>%s</code>\n\n", html.EscapeString(link))
	fmt.Fprintf(&b, "👥 Taklif qilganlaringiz: <b>%d</b> · ✅ buyurtma qilganlar: <b>%d</b>\n", invited, rewarded)
	fmt.Fprintf(&b, "🎁 Bepul imkoniyatlaringiz: <b>%d ta</b>", credits)
	if credits > 0 {
		b.WriteString("\n<i>Ishlatish uchun buyurtmani tasdiqlashda «🎁 Bonus bilan tasdiqlash» tugmasini bosing — /start</i>")
	}
	return b.String()
}

// announcementText — admin e'loni: har bir foydalanuvchiga o'z havolasi bilan.
func (h *Handler) announcementText(link string) string {
	return "🎁 <b>Do'stlaringizni taklif qiling — bepul hujjat oling!</b>\n\n" +
		"Endi har bir do'stingiz uchun sovg'a bor: quyidagi shaxsiy havolangizni do'stlaringizga yuboring. " +
		"Do'stingiz botga shu havola orqali kirib, birinchi buyurtmasini to'lagach, sizga <b>1 ta bepul hujjat</b> beriladi.\n\n" +
		"👥 10 ta do'st — 10 ta bepul hujjat, 100 ta do'st — 100 ta! Cheklov yo'q.\n" +
		h.bonusTerms() + "\n\n" +
		"🔗 <b>Sizning havolangiz:</b>\n<code>" + html.EscapeString(link) + "</code>\n\n" +
		"👇 «Do'stlarga yuborish» tugmasini bosing va havolani guruhlaringizga ulashing."
}

// referralShareKeyboard "Do'stlarga yuborish" tugmasi.
func referralShareKeyboard(link string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.URL("📤 Do'stlarga yuborish", shareURL(link))))
	return m
}

// announcementKeyboard e'lon tugmalari: havolani ulashish va bonuslar ekrani.
func announcementKeyboard(link string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(m.URL("📤 Do'stlarga yuborish", shareURL(link))),
		m.Row(m.Data("🎁 Mening bonuslarim", cbReferral)),
	)
	return m
}

// referralKeyboard "🎁 Do'stlarni taklif qilish" tugmasi (taklif tizimi
// o'chirilgan bo'lsa — nil, tugmasiz xabar).
func (h *Handler) referralKeyboard() *tele.ReplyMarkup {
	if !h.referralOn() {
		return nil
	}
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data(referralButton, cbReferral)))
	return m
}

// onReferral /taklif buyrug'i va "🎁 Do'stlarni taklif qilish" tugmasi:
// shaxsiy havola, shartlar va foydalanuvchining takliflari va bonuslari.
// FSM holatiga ta'sir qilmaydi — buyurtma jarayonining istalgan bosqichida ishlaydi.
func (h *Handler) onReferral(c tele.Context) error {
	if c.Callback() != nil {
		_ = c.Respond()
	}
	link := h.referralLink(c.Sender().ID)
	if !h.referralOn() || link == "" {
		return c.Send(msgReferralOff)
	}
	ctx, cancel := requestContext()
	defer cancel()
	u, err := h.ensureUser(ctx, c)
	if err != nil {
		return err
	}
	invited, rewarded, err := h.billing.ReferralStats(ctx, u.ID)
	if err != nil {
		return err
	}
	return c.Send(h.referralText(link, invited, rewarded, u.FreeCredits), referralShareKeyboard(link), tele.NoPreview)
}

// handleReferralStart /start ref_<ID> havolasini qayta ishlaydi (foydalanuvchi
// qulfi ostida, sessiya yaratilgandan keyin chaqiriladi). isNew — foydalanuvchi
// botga shu /start bilan birinchi marta kirdi: faqat shunday foydalanuvchi
// taklif qilganga bog'lanadi. Xatolar logga yoziladi — /start to'xtamaydi.
func (h *Handler) handleReferralStart(ctx context.Context, c tele.Context, referrerTG int64, isNew bool) {
	sender := c.Sender()
	if referrerTG == sender.ID {
		if err := c.Send(msgOwnReferralLink); err != nil {
			h.log.Debug("failed to send own referral link notice", zap.Error(err))
		}
		return
	}
	if !isNew {
		return
	}
	referrer, err := h.billing.AttachReferral(ctx, referrerTG, sender.ID, sender.Username, sender.FirstName)
	if err != nil {
		h.log.Error("failed to attach referral", append(userFields(c), zap.Int64("referrer_id", referrerTG), zap.Error(err))...)
		return
	}
	if referrer == nil {
		return
	}
	h.log.Info("referral attached", append(userFields(c), zap.Int64("referrer_id", referrer.TelegramUserID))...)
	h.notifyUser(referrer.TelegramUserID, fmt.Sprintf(msgReferralJoined, html.EscapeString(displayName(sender))))
}

// displayName foydalanuvchining ko'rinadigan nomi: "Ism (@username)".
func displayName(u *tele.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	switch {
	case name != "" && u.Username != "":
		return name + " (@" + u.Username + ")"
	case name != "":
		return name
	case u.Username != "":
		return "@" + u.Username
	}
	return "yangi foydalanuvchi"
}

// rewardReferrer buyurtmani to'lagan foydalanuvchini taklif qilganga mukofot
// beradi va uni xabardor qiladi. Mukofot faqat haqiqiy pul to'langan
// buyurtma uchun (bonus hisobidan to'liq bepul buyurtma hisoblanmaydi) va har
// bir taklif uchun bir marta beriladi — qayta chaqirish xavfsiz (to'lov
// tasdiqlanganda va zaxira sifatida fayl yetkazilganda chaqiriladi).
func (h *Handler) rewardReferrer(o *storage.Order) {
	if o == nil || h.billing == nil || !h.referralOn() || o.Price <= 0 || o.Price < h.opts.Referral.MinPayment {
		return
	}
	ctx, cancel := requestContext()
	defer cancel()
	reward, err := h.billing.RewardReferral(ctx, o.UserID, o.ID)
	if err != nil {
		h.log.Error("failed to reward referrer", zap.Uint("order_id", o.ID), zap.Error(err))
		return
	}
	if reward == nil {
		return
	}
	h.log.Info("referral rewarded",
		zap.Int64("referrer_id", reward.ReferrerTelegramID),
		zap.Int64("referee_id", o.TelegramUserID),
		zap.Uint("order_id", o.ID),
		zap.Int("credits", reward.Credits))
	h.notifyUser(reward.ReferrerTelegramID, fmt.Sprintf(msgReferralRewarded, reward.Credits))
}

// notifyUser foydalanuvchiga xabar yuboradi (u botni bloklagan bo'lishi
// mumkin — xato faqat logga yoziladi).
func (h *Handler) notifyUser(telegramID int64, text string, opts ...any) {
	if h.bot == nil {
		return
	}
	if _, err := h.bot.Send(&tele.Chat{ID: telegramID}, text, opts...); err != nil {
		h.log.Warn("failed to notify user", zap.Int64("user_id", telegramID), zap.Error(err))
	}
}

// releaseOrder foydalanuvchi voz kechgan buyurtmani bekor qiladi: to'lov
// kutilayotgan buyurtma yoki bonus hisobidan to'liq bepul, lekin
// yetkazilmagan buyurtma. Bonus ishlatilgan bo'lsa, u balansga qaytadi.
// Haqiqiy pul to'langan buyurtmaga tegilmaydi — u keyingi buyurtmada
// ishlatiladi (FindCredit).
func (h *Handler) releaseOrder(ctx context.Context, s *storage.Session) {
	if s.OrderID == nil || h.billing == nil {
		return
	}
	released, err := h.billing.ReleaseOrder(ctx, *s.OrderID)
	if err != nil {
		h.log.Warn("failed to release order", zap.Uint("order_id", *s.OrderID), zap.Error(err))
		return
	}
	if released {
		h.log.Info("order released", zap.Int64("user_id", s.UserID), zap.Uint("order_id", *s.OrderID))
	}
}
