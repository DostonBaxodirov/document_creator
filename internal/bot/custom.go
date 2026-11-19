package bot

// custom.go — AI maxsus dizayn: foydalanuvchi tayyor dizaynlar o'rniga
// "✨ Maxsus dizayn" ni tanlaydi va tarifni (Standart, Optimal, Premium)
// belgilaydi. Generatsiyada AI avval mavzuga mos dizayn konsepsiyasini
// (ranglar, shriftlar, kompozitsiya, rasmlar uslubi) yaratadi, so'ng tarif
// modeli slaydlarni yozadi va tarif sifatida AI rasmlar chiziladi.
//
// Har bir tarif — o'z matn modeli (ContentGenerator), rasm xizmati
// (ImageService, kvadrat rasmlar) va bitta slayd narxi.

import (
	"context"
	"fmt"
	"html"
	"math"
	"strings"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/storage"
)

// CustomTariff — AI maxsus dizayn tarifi.
type CustomTariff struct {
	PriceSlide int // bitta slayd narxi, so'm
	PricePage  int // Word ish (mustaqil ish, referat): bitta sahifa narxi, so'm
	// ImagePercent — rasmlar soni: taqdimotda slaydlar sonining (titul rasmi
	// bilan), Word ishda boblar sonining necha foizi.
	ImagePercent int
	// AI — dizayn konsepsiyasi va slaydlarni yozadigan model.
	AI ContentGenerator
	// Images — rasm xizmati (images.TierStandard orqali chaqiriladi). nil
	// bo'lsa — rasmlarsiz (dizayn rasm o'rnida bezakli fon bilan chiqadi).
	Images ImageService
	// Admin xabari va loglar uchun.
	TextModel, ImageModel, ImageQuality string
}

// tariffView — tarifning foydalanuvchiga ko'rinishi.
type tariffView struct {
	emoji, name, about string
}

var tariffViews = map[string]tariffView{
	config.TariffStandard: {"🥉", "Standart", "tez va tejamkor — aniq tuzilma, toza dizayn, mavzuga mos rasmlar"},
	config.TariffOptimal:  {"🥈", "Optimal", "narx va sifat muvozanati — boyroq va chuqurroq matn, yuqori sifatli rasmlar"},
	config.TariffPremium:  {"🥇", "Premium", "maksimal sifat — eng chuqur va puxta matn, HD rasmlar"},
}

// tariffName — "🥇 Premium" (noma'lum tarif — o'zi).
func tariffName(t string) string {
	if v, ok := tariffViews[t]; ok {
		return v.emoji + " " + v.name
	}
	return t
}

const (
	cbTariff       = "tariff"
	tariffBack     = "back" // tarif tugmasi: tayyor dizaynlar ro'yxatiga qaytish
	customLabel    = "✨ Maxsus dizayn"
	msgTariffHint  = "👇 Iltimos, quyidagi tugmalardan biri bilan tarifni tanlang."
	msgNoCustomCfg = "Maxsus dizayn hozircha mavjud emas. Tayyor dizaynlardan birini tanlang."
)

// customOn AI maxsus dizayn yoqilganini bildiradi.
func (h *Handler) customOn() bool { return len(h.opts.Custom) > 0 }

// customTariff tarif sozlamalarini qaytaradi.
func (h *Handler) customTariff(name string) (CustomTariff, bool) {
	t, ok := h.opts.Custom[name]
	return t, ok
}

// customTariffs mavjud tariflar (arzonidan qimmatiga).
func (h *Handler) customTariffs() []string {
	var out []string
	for _, name := range config.Tariffs {
		if _, ok := h.opts.Custom[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// isCustom buyurtma maxsus dizaynda (taqdimot yoki mustaqil ish/referat)
// ekanini bildiradi.
func isCustom(s *storage.Session) bool {
	w := ai.WorkType(s.WorkType)
	return s.Design == generator.ThemeCustom && (!w.IsDocument() || w.HasDesigns())
}

// customDocImageCount Word ishdagi rasmlar soni: taxminiy boblar sonining
// percent foizi, kamida 1 (percent 0 bo'lsa — rasmsiz).
func customDocImageCount(contentWords, percent int) int {
	if percent <= 0 {
		return 0
	}
	chapters := ai.PlanChapters(contentWords)
	return min(chapters, max(1, int(math.Round(float64(chapters)*float64(percent)/100))))
}

// docPlan maxsus dizayndagi Word ish uchun matn hajmi (so'zlarda) va rasmlar
// soni. Rasmlar matn o'rnini egallaydi, shuning uchun matn qisqartiriladi;
// qisqargan matnda boblar kamroq bo'lishi mumkin — rasmlar soni boblar
// sonidan oshmaydi (tarif matnida ko'rsatilgan son hujjatdagiga teng bo'lsin).
func docPlan(contentWords, figureWords, percent int) (words, figures int) {
	figures = customDocImageCount(contentWords, percent)
	if figures == 0 {
		return contentWords, 0
	}
	shrink := func(n int) int { return max(contentWords/2, contentWords-n*figureWords) }
	figures = min(figures, ai.PlanChapters(shrink(figures)))
	return shrink(figures), figures
}

// docImagesOn maxsus dizayndagi Word ishga rasm chizish mumkinligini bildiradi.
func (h *Handler) docImagesOn(t CustomTariff) bool {
	return t.Images != nil && t.Images.Available() && h.builders.Papers != nil
}

// customDocImages Word ish uchun rasmlar soni (rasm xizmati ulanmagan bo'lsa — 0).
func (h *Handler) customDocImages(s *storage.Session, t CustomTariff) int {
	if !h.docImagesOn(t) {
		return 0
	}
	b := h.builders.Papers
	_, n := docPlan(b.ContentWords(s.Volume), b.FigureWords(), t.ImagePercent)
	return n
}

// customPrice maxsus dizayndagi buyurtma narxi (tarif tugmalari va matni uchun).
func (h *Handler) customPrice(s *storage.Session, t CustomTariff) int {
	if ai.WorkType(s.WorkType).IsDocument() {
		return t.PricePage * max(s.Volume, 1)
	}
	return t.PriceSlide * max(s.Volume, 1)
}

// customImageCount taqdimotdagi AI rasmlar soni (titul rasmi bilan): slaydlar
// sonining percent foizi, kamida 1 (titul).
func customImageCount(slides, percent int) int {
	return max(1, int(math.Round(float64(slides)*float64(percent)/100)))
}

// ---------------------------------------------------------------------------
// Tarif tanlash
// ---------------------------------------------------------------------------

// tariffPrompt tarif tanlash xabari: har bir tarif narxi (slaydlar soni
// bo'yicha), tavsifi va rasmlar soni.
func (h *Handler) tariffPrompt(s *storage.Session) string {
	doc := ai.WorkType(s.WorkType).IsDocument()
	var b strings.Builder
	b.WriteString("✨ <b>Maxsus dizayn</b>\n\n")
	topic := html.EscapeString(truncateRunes(s.Topic, 120))
	if doc {
		fmt.Fprintf(&b, "«%s» mavzusiga mos <b>noyob dizayn noldan yaratiladi</b>: "+
			"sarlavhalar rangi va shrifti, titul varaq kompozitsiyasi, ramka va bezaklar, boblarda mavzuga mos rasmlar. "+
			"Matnni tarif modeli yozadi; asosiy matn OTM talablariga mos qoladi (Times New Roman 14, interval 1,5).\n\n", topic)
		fmt.Fprintf(&b, "<b>Tarifni tanlang</b> (%d sahifa):\n", s.Volume)
	} else {
		fmt.Fprintf(&b, "«%s» mavzusiga mos <b>noyob dizayn noldan yaratiladi</b>: "+
			"ranglar palitrasi, shriftlar, titul kompozitsiyasi, bezaklar va mavzuga mos rasmlar. "+
			"Slaydlar tuzilishi ham mazmunga qarab tanlanadi (vaqt chizig'i, sikl, sxema, SWOT ...).\n\n", topic)
		fmt.Fprintf(&b, "<b>Tarifni tanlang</b> (%d ta slayd):\n", s.Volume)
	}
	for _, name := range h.customTariffs() {
		t, _ := h.customTariff(name)
		v := tariffViews[name]
		fmt.Fprintf(&b, "\n%s <b>%s</b>", v.emoji, v.name)
		if h.paymentsOn() {
			rate, unit := t.PriceSlide, "slayd"
			if doc {
				rate, unit = t.PricePage, "sahifa"
			}
			fmt.Fprintf(&b, " — %s/%s → <b>%s</b>", formatSum(rate), unit, formatSum(h.customPrice(s, t)))
		}
		images := customImageCount(s.Volume, t.ImagePercent)
		if doc {
			images = h.customDocImages(s, t)
		}
		if images > 0 {
			fmt.Fprintf(&b, "\n     %s; %d ta rasm\n", v.about, images)
		} else {
			fmt.Fprintf(&b, "\n     %s\n", v.about)
		}
	}
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// if h.paymentsOn() {
	// 	b.WriteString("\n<i>Maxsus dizayn bepul urinishlarga kirmaydi.</i>")
	// }
	return b.String()
}

// tariffKeyboard tarif tugmalari (narxi bilan) va tayyor dizaynlarga qaytish tugmasi.
func (h *Handler) tariffKeyboard(s *storage.Session) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var rows []tele.Row
	for _, name := range h.customTariffs() {
		t, _ := h.customTariff(name)
		label := tariffName(name)
		if h.paymentsOn() {
			label += " — " + formatSum(h.customPrice(s, t))
		}
		rows = append(rows, m.Row(m.Data(label, cbTariff, name)))
	}
	rows = append(rows, m.Row(m.Data("⬅️ Tayyor dizaynlar", cbTariff, tariffBack)))
	m.Inline(rows...)
	return m
}

// askTariff AI maxsus dizayn tanlanganda tarif tanlash xabarini yuboradi.
func (h *Handler) askTariff(c tele.Context, s *storage.Session) error {
	return c.Send(h.tariffPrompt(s), h.tariffKeyboard(s))
}

// onTariff tarif tanlanganda (yoki "Tayyor dizaynlar" bosilganda) chaqiriladi.
func (h *Handler) onTariff(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	if State(s.State) != StateTariff {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}

	if c.Data() == tariffBack {
		s.Design, s.Tariff = "", ""
		s.State = string(StateDesign)
		if err := h.sessions.Save(ctx, s); err != nil {
			return err
		}
		_ = c.Respond()
		h.removeButtons(c)
		return h.askDesign(c, s)
	}

	name := c.Data()
	if _, ok := h.customTariff(name); !ok {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	s.Tariff = name
	s.State = string(StateStudent)
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}

	_ = c.Respond(&tele.CallbackResponse{Text: tariffViews[name].name})
	h.edit(c, fmt.Sprintf("🎨 Dizayn: <b>%s</b> · tarif: <b>%s</b>", customLabel, tariffName(name)))
	return h.ask(c, s, StateStudent)
}

// ---------------------------------------------------------------------------
// Generatsiya
// ---------------------------------------------------------------------------

// designStage — dizayn konsepsiyasini yaratish bosqichi.
var designStage = progressStage{icon: "🎨", first: "Mavzuga mos dizayn yaratilmoqda...", writing: "Dizayn yaratilmoqda...", last: "Dizayn tayyor"}

// customDesign AI dan dizayn konsepsiyasini oladi va uni generator
// spetsifikatsiyasiga aylantiradi.
func (h *Handler) customDesign(ctx context.Context, chat *tele.Chat, log *zap.Logger, s *storage.Session, t CustomTariff, language string) (*ai.Design, *generator.CustomSpec, error) {
	report, stop := h.startProgress(ctx, chat, log, designStage)
	report(0, 1)
	started := time.Now()
	d, err := t.AI.GenerateDesign(ctx, ai.DesignRequest{Topic: s.Topic, Language: language, Requirements: s.Requirements})
	stop()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: dizayn: %w", errAI, err)
	}
	log.Info("custom design generated",
		zap.String("name", d.Name),
		zap.String("mood", d.Mood),
		zap.String("cover", d.Cover),
		zap.String("header", d.Header),
		zap.String("background", d.Colors.Background),
		zap.String("primary", d.Colors.Primary),
		zap.Duration("took", time.Since(started)))
	return d, customSpec(d), nil
}

// customSpec AI dizaynini generator spetsifikatsiyasiga o'giradi (qiymatlarni
// tekshirish va tuzatish — generator vazifasi).
func customSpec(d *ai.Design) *generator.CustomSpec {
	return &generator.CustomSpec{
		Name:        d.Name,
		Background:  d.Colors.Background,
		Surface:     d.Colors.Surface,
		Primary:     d.Colors.Primary,
		Secondary:   d.Colors.Secondary,
		Accent:      d.Colors.Accent,
		Text:        d.Colors.Text,
		Muted:       d.Colors.Muted,
		HeadingFont: d.HeadingFont,
		BodyFont:    d.BodyFont,
		Cover:       d.Cover,
		Header:      d.Header,
		Cards:       d.Cards,
		Corners:     d.Corners,
		Markers:     d.Markers,
		Motif:       d.Motif,
		PhotoSide:   d.PhotoSide,
		PhotoStyle:  d.ImageStyle,
		Signature:   d.SignatureLayouts,
	}
}

// picturePlan — taqdimotga qaysi rasmlar qanday chizilishi.
type picturePlan struct {
	design generator.Design
	svc    ImageService
	tier   images.Tier
	// limit > 0 — rasmlar soni chegarasi (titul rasmi bilan; AI maxsus
	// dizayn): rasmlar slaydlarga teng taqsimlanadi va kvadrat chiziladi.
	// 0 — rasm sig'adigan barcha slaydlarga (tayyor rasmli dizaynlar).
	limit int
}

// customPhotoTargets AI maxsus dizaynda rasm qo'yiladigan slaydlar:
// rasm tavsifi bor slaydlardan n tasi (dizayn tanlaydi — yon panelga
// sig'adiganlari ustuvor, teng oraliqda).
func customPhotoTargets(design generator.Design, pres *ai.Presentation, deck *generator.Deck, n int) []int {
	var (
		cand  []generator.Slide
		index []int
	)
	for i, sl := range pres.Slides {
		if sl.Image != "" && i < len(deck.Slides) {
			cand = append(cand, deck.Slides[i])
			index = append(index, i)
		}
	}
	picked := design.PhotoSlides(cand, n)
	out := make([]int, len(picked))
	for k, i := range picked {
		out[k] = index[i]
	}
	return out
}

// customCaption fayl izohidagi dizayn nomi: "✨ Chuqur okean (maxsus dizayn, 🥇 Premium)".
func customCaption(d *ai.Design, tariff string) string {
	name := ""
	if d != nil {
		name = d.Name
	}
	return customCaptionName(name, tariff)
}

// docCustomCaption — Word ishdagi maxsus dizayn nomi (customCaption kabi).
func docCustomCaption(d *ai.DocDesign, tariff string) string {
	name := ""
	if d != nil {
		name = d.Name
	}
	return customCaptionName(name, tariff)
}

func customCaptionName(name, tariff string) string {
	if name != "" {
		return fmt.Sprintf("✨ %s (maxsus dizayn, %s)", name, tariffName(tariff))
	}
	return fmt.Sprintf("✨ Maxsus dizayn (%s)", tariffName(tariff))
}

// customDocDesign AI dan Word dizayni konsepsiyasini oladi va uni generator
// spetsifikatsiyasiga aylantiradi.
func (h *Handler) customDocDesign(ctx context.Context, chat *tele.Chat, log *zap.Logger, s *storage.Session, t CustomTariff, language string) (*ai.DocDesign, *generator.DocSpec, error) {
	report, stop := h.startProgress(ctx, chat, log, designStage)
	report(0, 1)
	started := time.Now()
	d, err := t.AI.GenerateDocDesign(ctx, ai.DesignRequest{Topic: s.Topic, Language: language, Requirements: s.Requirements})
	stop()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: dizayn: %w", errAI, err)
	}
	log.Info("custom doc design generated",
		zap.String("name", d.Name),
		zap.String("title", d.Title),
		zap.String("heading", d.Heading),
		zap.String("border", d.Border),
		zap.String("primary", d.Primary),
		zap.Duration("took", time.Since(started)))
	return d, &generator.DocSpec{
		Name: d.Name, Primary: d.Primary, Accent: d.Accent, HeadingFont: d.HeadingFont,
		Title: d.Title, Heading: d.Heading, Border: d.Border, Ornament: d.Ornament,
	}, nil
}

// ---------------------------------------------------------------------------
// Qo'llanma (birinchi /start va /help)
// ---------------------------------------------------------------------------

// guideText foydalanish va tariflar bo'yicha to'liq qo'llanma (joriy narx va
// sozlamalar bilan). Telegram xabari chegarasiga (4096) sig'adi.
func (h *Handler) guideText() string {
	l := h.opts.Limits
	p := h.opts.Payment
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
			formatSum(p.PriceSlidePlain), formatSum(p.PriceSlidePhoto))
		fmt.Fprintf(&b, "📚 Mustaqil ish va referat: har bir sahifa — %s\n", formatSum(p.PricePage))
		fmt.Fprintf(&b, "🛠 Amaliy topshiriq: %s\n", formatSum(p.PriceDocument))
		// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
		// if p.FreePhoto > 0 || p.FreePlain > 0 {
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
			fmt.Fprintf(&b, "%s <b>%s</b>", v.emoji, v.name)
			if h.paymentsOn() {
				fmt.Fprintf(&b, " — %s/slayd, %s/sahifa", formatSum(t.PriceSlide), formatSum(t.PricePage))
				examples = append(examples, fmt.Sprintf("%s — %s", v.name, formatSum(t.PriceSlide*sample)))
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
