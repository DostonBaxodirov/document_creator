package payment

import (
	"fmt"
	"strings"
	"time"

	"telegram-presentation-bot/internal/textutil"
)

// Tashkent — O'zbekiston vaqti (UTC+5, yozgi vaqt yo'q). Cheklardagi vaqt shu
// mintaqada yoziladi. FixedZone tizimda tzdata bo'lishini talab qilmaydi.
var Tashkent = time.FixedZone("UZT", 5*60*60)

// Verdict — chek bo'yicha qaror.
type Verdict string

const (
	Approved   Verdict = "approved"   // hammasi mos — avtomatik tasdiqlanadi
	Suspicious Verdict = "suspicious" // shubhali — admin qaror qiladi
	Invalid    Verdict = "invalid"    // rasm umuman chek emas — boshqa rasm so'raladi
)

// Qaror chegaralari.
const (
	minConfidence     = 0.8              // avtomatik tasdiqlash uchun AI ishonchi
	invalidConfidence = 0.7              // "chek emas" degan xulosaga ishonish chegarasi
	clockSkew         = 10 * time.Minute // chek vaqti va server vaqti orasidagi zaxira
)

// Input — qaror uchun ma'lumotlar.
type Input struct {
	Receipt        *Receipt // nil — AI chekni o'qiy olmadi
	Err            error    // AI xatosi (Receipt nil bo'lganda)
	ExpectedAmount int      // so'mda
	CardLast4      string
	OrderCreatedAt time.Time
	Now            time.Time
	Seen           bool // chek (rasm yoki tranzaksiya raqami) oldin yuborilgan
}

// Decision — qaror va uning sabablari (admin xabarida ko'rsatiladi).
type Decision struct {
	Verdict Verdict
	Reasons []string
	PaidAt  *time.Time // chekdan o'qilgan vaqt (UTC), o'qilmagan bo'lsa nil
}

// Decide chekni qat'iy qoidalar bo'yicha baholaydi. Avtomatik tasdiqlash
// uchun barcha shartlar bajarilishi kerak: chek, holat muvaffaqiyatli, summa
// kutilganidan kam emas, qabul qiluvchi karta mos, AI ishonchi yetarli, chek
// vaqti buyurtmadan keyin va kelajakda emas, chek oldin ishlatilmagan.
func Decide(in Input) Decision {
	r := in.Receipt
	if r == nil {
		reason := "Chekni avtomatik tekshirib bo'lmadi"
		if in.Err != nil {
			reason += ": " + in.Err.Error()
		}
		return Decision{Verdict: Suspicious, Reasons: []string{reason}}
	}

	conf := float64(r.Confidence)
	if !r.IsReceipt {
		if conf >= invalidConfidence && !in.Seen {
			return Decision{Verdict: Invalid, Reasons: []string{"Rasm to'lov cheki emas"}}
		}
		return Decision{Verdict: Suspicious, Reasons: []string{"Rasm chek deb tanilmadi (ishonch past)"}}
	}

	var reasons []string
	add := func(format string, args ...any) { reasons = append(reasons, fmt.Sprintf(format, args...)) }

	if !r.Success {
		add("To'lov holati muvaffaqiyatli emas")
	}

	switch amount := r.Amount.Int(); {
	case amount <= 0:
		add("Summa o'qilmadi")
	case amount < in.ExpectedAmount:
		add("Summa kam: %d so'm (kutilgan %d so'm)", amount, in.ExpectedAmount)
	}

	if c := strings.ToLower(strings.TrimSpace(r.Currency)); c != "" && !isUZS(c) {
		add("Valyuta: %s", r.Currency)
	}

	switch last4 := textutil.LastDigits(r.CardLast4.String(), 4); {
	case last4 == "":
		add("Qabul qiluvchi karta raqami ko'rinmadi")
	case last4 != textutil.LastDigits(in.CardLast4, 4):
		add("Karta mos emas: ****%s (kutilgan ****%s)", last4, textutil.LastDigits(in.CardLast4, 4))
	}

	if conf < minConfidence {
		add("Tekshiruv ishonchi past (%.2f)", conf)
	}

	paidAt, dateOnly, ok := parsePaidAt(r.PaidAt)
	var paidUTC *time.Time
	switch {
	case strings.TrimSpace(r.PaidAt) == "":
		add("Chek sanasi o'qilmadi")
	case !ok:
		add("Chek sanasi tushunarsiz: %q", r.PaidAt)
	default:
		u := paidAt.UTC()
		paidUTC = &u
		earliest, latest := in.OrderCreatedAt.Add(-clockSkew), in.Now.Add(clockSkew)
		if dateOnly {
			// Faqat sana bo'lsa — kun bo'yicha solishtiramiz.
			earliest = startOfDay(in.OrderCreatedAt.In(Tashkent))
			latest = startOfDay(in.Now.In(Tashkent)).Add(24*time.Hour - time.Nanosecond)
		}
		if paidAt.Before(earliest) {
			add("Chek buyurtmadan oldingi vaqtga tegishli (%s)", paidAt.In(Tashkent).Format("02.01.2006 15:04"))
		}
		if paidAt.After(latest) {
			add("Chek vaqti kelajakda (%s)", paidAt.In(Tashkent).Format("02.01.2006 15:04"))
		}
	}

	if in.Seen {
		add("Bu chek oldin yuborilgan (takroriy chek)")
	}

	if len(reasons) == 0 {
		return Decision{Verdict: Approved, PaidAt: paidUTC}
	}
	return Decision{Verdict: Suspicious, Reasons: reasons, PaidAt: paidUTC}
}

func isUZS(c string) bool {
	switch strings.NewReplacer("'", "", "‘", "", "’", "", "ʻ", "", "`", "", ".", "").Replace(c) {
	case "uzs", "som", "sum", "сум", "сўм":
		return true
	}
	return false
}

// paidAtLayouts — chekdagi sana-vaqt formatlari (model "YYYY-MM-DD HH:MM" ga
// o'girishi so'ralgan, qolganlari — zaxira).
var paidAtLayouts = []string{
	"2006-01-02 15:04",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
	"02.01.2006 15:04",
	"02.01.2006 15:04:05",
	"02/01/2006 15:04",
	"02-01-2006 15:04",
}

var dateOnlyLayouts = []string{"2006-01-02", "02.01.2006", "02/01/2006"}

// parsePaidAt chek vaqtini Toshkent vaqti sifatida o'qiydi.
func parsePaidAt(s string) (t time.Time, dateOnly, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false, false
	}
	for _, l := range paidAtLayouts {
		if t, err := time.ParseInLocation(l, s, Tashkent); err == nil {
			return t, false, true
		}
	}
	for _, l := range dateOnlyLayouts {
		if t, err := time.ParseInLocation(l, s, Tashkent); err == nil {
			return t, true, true
		}
	}
	return time.Time{}, false, false
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
