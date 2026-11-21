package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

// ---------------------------------------------------------------------------
// Qo'llanma (birinchi /start va /help)
// ---------------------------------------------------------------------------

// guideText foydalanish va tariflar bo'yicha to'liq qo'llanma (joriy narx va
// sozlamalar bilan). Telegram xabari chegarasiga (4096) sig'adi.
func (h *Handler) guideText() string {
	l := h.opts.Limits
	var b strings.Builder
	sec := 2 // 1- va 2-bo'lim qotirilgan; qolganlari to'lov va AI dizayn yoqilganiga bog'liq
	section := func(title string) {
		sec++
		fmt.Fprintf(&b, "<b>%d. %s</b>\n", sec, title)
	}
	b.WriteString("📖 <b>Botdan foydalanish bo'yicha qo'llanma</b>\n\n")

	b.WriteString("<b>1. Nima tayyorlab beraman?</b>\n")
	fmt.Fprintf(&b, "📊 <b>Taqdimot</b> — PowerPoint (.pptx), %d–%d slayd\n", l.MinSlides, l.MaxSlides)
	fmt.Fprintf(&b, "📚 <b>Mustaqil ish</b>, 📝 <b>referat</b> va 🛠 <b>amaliy topshiriq</b> — Word (.docx), %d–%d sahifa\n", l.MinPages, l.MaxPages)
	b.WriteString("🌐 Tillar: o'zbekcha (lotin va kirill), ruscha, inglizcha\n\n")

	b.WriteString("<b>2. Buyurtma berish tartibi</b>\n")
	b.WriteString("1️⃣ /start → ish turini va tilini tanlang\n")
	b.WriteString("2️⃣ Mavzuni yozing va hajmni (slaydlar yoki sahifalar soni) kiriting\n")
	fmt.Fprintf(&b, "3️⃣ Dizayn: taqdimotga %d ta, mustaqil ish va referatga %d ta tayyor dizayn", len(generator.Themes()), len(generator.DocThemes()))
	if h.customOn() {
		b.WriteString(" yoki <b>" + customLabel + "</b>")
	}
	b.WriteString("\n4️⃣ Ism-familiya, universitet, o'qituvchi va qo'shimcha talablar (masalan: «ko'proq misollar»)\n")
	b.WriteString("5️⃣ Ma'lumotlarni tasdiqlang")
	if h.paymentsOn() {
		b.WriteString(" va to'lang")
	}
	b.WriteString(" — tayyor fayl 1–5 daqiqada keladi\n\n")

	if h.paymentsOn() {
		section("Narxlar")
		fmt.Fprintf(&b, "📊 Tayyor dizayndagi taqdimot: har bir slayd — %s (📷 rasmli dizaynda — %s)\n",
			formatSum(h.rate(ai.WorkPresentation, storage.KindPlain)), formatSum(h.rate(ai.WorkPresentation, storage.KindPhoto)))
		fmt.Fprintf(&b, "📚 Mustaqil ish va referat: har bir sahifa — %s\n", formatSum(h.rate(ai.WorkIndependent, storage.KindPlain)))
		fmt.Fprintf(&b, "🛠 Amaliy topshiriq: %s\n", formatSum(h.rate(ai.WorkPractical, storage.KindPlain)))
		// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
		// if p := h.opts.Payment; p.FreePhoto > 0 || p.FreePlain > 0 {
		// 	fmt.Fprintf(&b, "🎁 Birinchi %d ta rasmli va %d ta rasmsiz ish — <b>bepul</b>\n", p.FreePhoto, p.FreePlain)
		// }
		b.WriteString("\n")
	}

	if h.customOn() {
		section("✨ Maxsus dizayn — tariflar")
		b.WriteString("Mavzungizga mos noyob dizayn noldan yaratiladi: ranglar, shriftlar, kompozitsiya va mavzuga mos rasmlar " +
			"(taqdimot, mustaqil ish va referat uchun).\n")
		const sample = 20
		var examples []string
		for _, name := range h.customTariffs() {
			t, _ := h.customTariff(name)
			v := tariffViews[name]
			kind := storage.AIKind(name)
			fmt.Fprintf(&b, "%s <b>%s</b>", v.emoji, v.name)
			if h.paymentsOn() {
				fmt.Fprintf(&b, " — %s/slayd, %s/sahifa", formatSum(h.rate(ai.WorkPresentation, kind)), formatSum(h.rate(ai.WorkIndependent, kind)))
				examples = append(examples, fmt.Sprintf("%s — %s", v.name, formatSum(h.priceFor(string(ai.WorkPresentation), sample, kind).Total)))
			}
			fmt.Fprintf(&b, "\n     %s; slaydlar yoki boblarning ~%d%% ida rasm\n", v.about, t.ImagePercent)
		}
		if len(examples) > 0 {
			fmt.Fprintf(&b, "<i>Masalan, %d slayd: %s.</i>\n", sample, strings.Join(examples, ", "))
			// b.WriteString("<i>Maxsus dizayn bepul urinishlarga kirmaydi.</i>\n") — bepul urinishlar hozircha o'chirilgan
		}
		b.WriteString("\n")
	}

	if h.paymentsOn() {
		b.WriteString("<b>💳 To'lov</b>\n")
		b.WriteString("Ko'rsatilgan kartaga o'tkazing va chek skrinshotini yuboring — chek avtomatik tekshiriladi, tasdiqlangach ish darhol tayyorlanadi. " +
			"Biror xato bo'lsa, to'lovingiz saqlanib qoladi: «Qayta urinish» tugmasini bosing yoki keyingi buyurtmada ishlatiladi.\n\n")
	}

	b.WriteString("<b>Buyruqlar:</b> /start — yangi buyurtma, /cancel — bekor qilish, /help — shu qo'llanma")
	return b.String()
}

// sendGuideOnce qo'llanmani foydalanuvchiga birinchi marta yuboradi va
// yuborilgan vaqtni sessiyaga yozadi (saqlash chaqiruvchining vazifasi emas —
// shu yerda saqlanadi).
func (h *Handler) sendGuideOnce(ctx context.Context, c tele.Context, s *storage.Session) {
	if s.GuideSentAt != nil {
		return
	}
	if err := c.Send(h.guideText(), removeKeyboard()); err != nil {
		h.log.Warn("failed to send guide", zap.Int64("user_id", s.UserID), zap.Error(err))
		return
	}
	now := time.Now().UTC()
	s.GuideSentAt = &now
	if err := h.sessions.Save(ctx, s); err != nil {
		h.log.Warn("failed to save guide flag", zap.Int64("user_id", s.UserID), zap.Error(err))
	}
}
