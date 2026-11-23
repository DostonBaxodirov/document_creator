package bot

// pricing.go — narx hisobining yagona manbai: buyurtma narxi, uning tarkibi
// ("10 slayd × 300 so'm") va bitta birlik narxi shu yerda hisoblanadi.

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generation"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

// Narx birliklari (foydalanuvchiga ko'rinadi).
const (
	unitSlide = "slayd"
	unitPage  = "sahifa"
)

// priceInfo — buyurtma narxi va uning tarkibi.
type priceInfo struct {
	Kind  string // narx toifasi: "photo", "plain", "ai_<tarif>"
	Rate  int    // bitta birlik narxi (amaliy topshiriqda — butun ish narxi)
	Units int    // birliklar soni (amaliy topshiriqda — 0)
	Unit  string // "slayd", "sahifa" yoki "" (amaliy topshiriq — qat'iy narx)
	Total int    // jami narx, so'm
}

// priceUnit ish turining narx birligi: taqdimot — slayd, mustaqil ish va
// referat — sahifa, amaliy topshiriq — "" (qat'iy narx). Noma'lum ish turi
// taqdimot sifatida hisoblanadi.
func priceUnit(w ai.WorkType) string {
	switch {
	case !w.IsDocument():
		return unitSlide
	case w == ai.WorkPractical:
		return ""
	}
	return unitPage
}

// rate bitta birlik narxi: taqdimotda — bitta slayd, mustaqil ish va
// referatda — bitta sahifa, amaliy topshiriqda — qat'iy narx.
func (h *Handler) rate(w ai.WorkType, kind string) int {
	switch priceUnit(w) {
	case unitSlide:
		return h.slideRate(kind)
	case unitPage:
		return h.pageRate(kind)
	}
	return h.opts.Payment.PriceDocument
}

// priceFor buyurtma narxini hisoblaydi: taqdimot — slaydlar soni × bitta
// slayd narxi, mustaqil ish va referat — sahifalar soni × bitta sahifa
// narxi, amaliy topshiriq — qat'iy narx. Hajm kamida 1 deb olinadi.
func (h *Handler) priceFor(workType string, volume int, kind string) priceInfo {
	w := ai.WorkType(workType)
	p := priceInfo{Kind: kind, Rate: h.rate(w, kind), Unit: priceUnit(w)}
	if p.Unit == "" {
		p.Total = p.Rate
		return p
	}
	p.Units = max(volume, 1)
	p.Total = p.Units * p.Rate
	return p
}

// detail narx tarkibi: "10 slayd × 300 so'm", "15 sahifa × 200 so'm"
// (amaliy topshiriqda — bo'sh satr).
func (p priceInfo) detail() string {
	if p.Unit == "" {
		return ""
	}
	return fmt.Sprintf("%d %s × %s", p.Units, p.Unit, formatSum(p.Rate))
}

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
	if !isDoc(s) && generator.ThemeHasPhotos(s.Design) {
		return storage.KindPhoto
	}
	return storage.KindPlain
}

// kindOf buyurtma toifasini handler sozlamalari bilan aniqlaydi: rasm
// provayderi ulanmagan bo'lsa, rasmli dizayn ham rasmsiz chiqadi va rasmsiz
// narxda hisoblanadi.
func (h *Handler) kindOf(s *storage.Session) string {
	k := orderKind(s)
	if k == storage.KindPhoto && !h.gen.PhotosAvailable() {
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

// price buyurtma narxi (hisob — priceFor).
func (h *Handler) price(s *storage.Session, kind string) int {
	return h.priceFor(s.WorkType, s.Volume, kind).Total
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
	s := fmt.Sprintf("\n\n💳 <b>Narxlar:</b> taqdimot — har bir slayd %s (📷 rasmli dizaynda — %s), "+
		"mustaqil ish va referat — har bir sahifa %s, amaliy topshiriq — %s.",
		formatSum(h.rate(ai.WorkPresentation, storage.KindPlain)), formatSum(h.rate(ai.WorkPresentation, storage.KindPhoto)),
		formatSum(h.rate(ai.WorkIndependent, storage.KindPlain)), formatSum(h.rate(ai.WorkPractical, storage.KindPlain)))
	if h.customOn() {
		var parts []string
		for _, name := range h.customTariffs() {
			kind := storage.AIKind(name)
			parts = append(parts, fmt.Sprintf("%s — %s/slayd, %s/sahifa", tariffViews[name].name,
				formatSum(h.rate(ai.WorkPresentation, kind)), formatSum(h.rate(ai.WorkIndependent, kind))))
		}
		s += "\n✨ Maxsus dizayn: " + strings.Join(parts, "; ") + "."
	}
	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// if p := h.opts.Payment; p.FreePhoto > 0 || p.FreePlain > 0 {
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
	p := h.priceFor(s.WorkType, s.Volume, kind)
	q := &quote{kind: kind, price: p.Total, detail: p.detail()}
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
		return name + ", ✨ Maxsus dizayn — " + generation.TariffName(tariff)
	}
	if ai.WorkType(workType).IsDocument() {
		return name
	}
	if kind == storage.KindPhoto {
		return name + ", rasmli dizayn"
	}
	return name + ", rasmsiz dizayn"
}
