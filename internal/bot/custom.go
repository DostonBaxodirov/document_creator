package bot

// custom.go — AI maxsus dizayn: foydalanuvchi tayyor dizaynlar o'rniga
// "✨ Maxsus dizayn" ni tanlaydi va tarifni (Standart, Optimal, Premium)
// belgilaydi. Generatsiyada (internal/generation) AI avval mavzuga mos dizayn
// konsepsiyasini (ranglar, shriftlar, kompozitsiya, rasmlar uslubi) yaratadi,
// so'ng tarif modeli slaydlarni yozadi va tarif sifatida AI rasmlar chiziladi.
//
// Har bir tarif — o'z matn modeli, rasm xizmati (kvadrat rasmlar) va narxi
// (bitta slayd va bitta sahifa narxi — shu paketda).

import (
	"fmt"
	"html"
	"strings"

	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generation"
	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/textutil"
)

// CustomTariff — AI maxsus dizayn tarifi: narxlari va generatsiya sozlamalari
// (matn modeli, rasm xizmati, rasmlar foizi).
type CustomTariff struct {
	PriceSlide int // bitta slayd narxi, so'm
	PricePage  int // Word ish (mustaqil ish, referat): bitta sahifa narxi, so'm
	generation.Tariff
}

// tariffView — tarifning foydalanuvchiga ko'rinishi (belgi va nom —
// generation'dagi yagona manbadan).
type tariffView struct {
	emoji, name, about string
}

// newTariffView tarif ko'rinishini tavsifi bilan yaratadi.
func newTariffView(tariff, about string) tariffView {
	emoji, name := generation.TariffLabel(tariff)
	return tariffView{emoji: emoji, name: name, about: about}
}

var tariffViews = map[string]tariffView{
	config.TariffStandard: newTariffView(config.TariffStandard, "tez va tejamkor — aniq tuzilma, toza dizayn, mavzuga mos rasmlar"),
	config.TariffOptimal:  newTariffView(config.TariffOptimal, "narx va sifat muvozanati — boyroq va chuqurroq matn, yuqori sifatli rasmlar"),
	config.TariffPremium:  newTariffView(config.TariffPremium, "maksimal sifat — eng chuqur va puxta matn, HD rasmlar"),
}

const (
	cbTariff    = "tariff"
	tariffBack  = "back" // tarif tugmasi: tayyor dizaynlar ro'yxatiga qaytish
	customLabel = "✨ Maxsus dizayn"
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
	return generation.IsCustom(ai.WorkType(s.WorkType), s.Design)
}

// customPrice maxsus dizayndagi buyurtma narxi (tarif tugmalari uchun; hisob — priceFor).
func (h *Handler) customPrice(s *storage.Session, tariff string) int {
	return h.priceFor(s.WorkType, s.Volume, storage.AIKind(tariff)).Total
}

// ---------------------------------------------------------------------------
// Tarif tanlash
// ---------------------------------------------------------------------------

// tariffPrompt tarif tanlash xabari: har bir tarif narxi (slaydlar soni
// bo'yicha), tavsifi va rasmlar soni.
func (h *Handler) tariffPrompt(s *storage.Session) string {
	doc := isDoc(s)
	var b strings.Builder
	b.WriteString("✨ <b>Maxsus dizayn</b>\n\n")
	topic := html.EscapeString(textutil.TruncateHard(s.Topic, 120))
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
		v := tariffViews[name]
		fmt.Fprintf(&b, "\n%s <b>%s</b>", v.emoji, v.name)
		if h.paymentsOn() {
			p := h.priceFor(s.WorkType, s.Volume, storage.AIKind(name))
			fmt.Fprintf(&b, " — %s/%s → <b>%s</b>", formatSum(p.Rate), p.Unit, formatSum(p.Total))
		}
		// Rasmlar soni generatsiyadagi bilan bir xil hisoblanadi (Word ishda —
		// rasm xizmati ulanmagan bo'lsa 0).
		images := h.gen.CustomImages(ai.WorkType(s.WorkType), s.Volume, name)
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
		label := generation.TariffName(name)
		if h.paymentsOn() {
			label += " — " + formatSum(h.customPrice(s, name))
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
	h.edit(c, fmt.Sprintf("🎨 Dizayn: <b>%s</b> · tarif: <b>%s</b>", customLabel, generation.TariffName(name)))
	return h.ask(c, s, StateStudent)
}
