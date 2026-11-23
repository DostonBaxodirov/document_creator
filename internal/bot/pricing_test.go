package bot

import (
	"testing"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/storage"
)

// TestPriceFor — har bir ish turi × narx toifasi (customHandler narxlari:
// rasmli slayd 300, rasmsiz 250, sahifa 200, amaliy topshiriq 7 000;
// tariflar testTariffs dan).
func TestPriceFor(t *testing.T) {
	h := customHandler(&fakeBilling{})
	const (
		pres  = string(ai.WorkPresentation)
		indep = string(ai.WorkIndependent)
		ref   = string(ai.WorkReferat)
		prac  = string(ai.WorkPractical)
	)
	cases := []struct {
		work, kind string
		volume     int
		want       priceInfo
		detail     string
	}{
		{pres, storage.KindPhoto, 10, priceInfo{storage.KindPhoto, 300, 10, "slayd", 3000}, "10 slayd × 300 so'm"},
		{pres, storage.KindPlain, 10, priceInfo{storage.KindPlain, 250, 10, "slayd", 2500}, "10 slayd × 250 so'm"},
		{pres, "ai_standard", 10, priceInfo{"ai_standard", 300, 10, "slayd", 3000}, "10 slayd × 300 so'm"},
		{pres, "ai_optimal", 10, priceInfo{"ai_optimal", 500, 10, "slayd", 5000}, "10 slayd × 500 so'm"},
		{pres, "ai_premium", 10, priceInfo{"ai_premium", 1700, 10, "slayd", 17000}, "10 slayd × 1 700 so'm"},

		{indep, storage.KindPhoto, 15, priceInfo{storage.KindPhoto, 200, 15, "sahifa", 3000}, "15 sahifa × 200 so'm"},
		{indep, storage.KindPlain, 15, priceInfo{storage.KindPlain, 200, 15, "sahifa", 3000}, "15 sahifa × 200 so'm"},
		{indep, "ai_standard", 15, priceInfo{"ai_standard", 400, 15, "sahifa", 6000}, "15 sahifa × 400 so'm"},
		{indep, "ai_optimal", 15, priceInfo{"ai_optimal", 700, 15, "sahifa", 10500}, "15 sahifa × 700 so'm"},
		{indep, "ai_premium", 15, priceInfo{"ai_premium", 1500, 15, "sahifa", 22500}, "15 sahifa × 1 500 so'm"},

		{ref, storage.KindPhoto, 12, priceInfo{storage.KindPhoto, 200, 12, "sahifa", 2400}, "12 sahifa × 200 so'm"},
		{ref, storage.KindPlain, 12, priceInfo{storage.KindPlain, 200, 12, "sahifa", 2400}, "12 sahifa × 200 so'm"},
		{ref, "ai_standard", 12, priceInfo{"ai_standard", 400, 12, "sahifa", 4800}, "12 sahifa × 400 so'm"},
		{ref, "ai_optimal", 12, priceInfo{"ai_optimal", 700, 12, "sahifa", 8400}, "12 sahifa × 700 so'm"},
		{ref, "ai_premium", 12, priceInfo{"ai_premium", 1500, 12, "sahifa", 18000}, "12 sahifa × 1 500 so'm"},

		// Amaliy topshiriq — qat'iy narx, hajm va toifadan qat'i nazar.
		{prac, storage.KindPhoto, 8, priceInfo{storage.KindPhoto, 7000, 0, "", 7000}, ""},
		{prac, storage.KindPlain, 8, priceInfo{storage.KindPlain, 7000, 0, "", 7000}, ""},
		{prac, "ai_standard", 8, priceInfo{"ai_standard", 7000, 0, "", 7000}, ""},
		{prac, "ai_optimal", 8, priceInfo{"ai_optimal", 7000, 0, "", 7000}, ""},
		{prac, "ai_premium", 8, priceInfo{"ai_premium", 7000, 0, "", 7000}, ""},

		// Hajm 0 — kamida 1 birlik.
		{pres, storage.KindPhoto, 0, priceInfo{storage.KindPhoto, 300, 1, "slayd", 300}, "1 slayd × 300 so'm"},
		{pres, storage.KindPlain, 0, priceInfo{storage.KindPlain, 250, 1, "slayd", 250}, "1 slayd × 250 so'm"},
		{pres, "ai_standard", 0, priceInfo{"ai_standard", 300, 1, "slayd", 300}, "1 slayd × 300 so'm"},
		{pres, "ai_optimal", 0, priceInfo{"ai_optimal", 500, 1, "slayd", 500}, "1 slayd × 500 so'm"},
		{pres, "ai_premium", 0, priceInfo{"ai_premium", 1700, 1, "slayd", 1700}, "1 slayd × 1 700 so'm"},
		{indep, storage.KindPhoto, 0, priceInfo{storage.KindPhoto, 200, 1, "sahifa", 200}, "1 sahifa × 200 so'm"},
		{indep, storage.KindPlain, 0, priceInfo{storage.KindPlain, 200, 1, "sahifa", 200}, "1 sahifa × 200 so'm"},
		{indep, "ai_standard", 0, priceInfo{"ai_standard", 400, 1, "sahifa", 400}, "1 sahifa × 400 so'm"},
		{indep, "ai_optimal", 0, priceInfo{"ai_optimal", 700, 1, "sahifa", 700}, "1 sahifa × 700 so'm"},
		{indep, "ai_premium", 0, priceInfo{"ai_premium", 1500, 1, "sahifa", 1500}, "1 sahifa × 1 500 so'm"},
		{ref, storage.KindPhoto, 0, priceInfo{storage.KindPhoto, 200, 1, "sahifa", 200}, "1 sahifa × 200 so'm"},
		{ref, storage.KindPlain, 0, priceInfo{storage.KindPlain, 200, 1, "sahifa", 200}, "1 sahifa × 200 so'm"},
		{ref, "ai_standard", 0, priceInfo{"ai_standard", 400, 1, "sahifa", 400}, "1 sahifa × 400 so'm"},
		{ref, "ai_optimal", 0, priceInfo{"ai_optimal", 700, 1, "sahifa", 700}, "1 sahifa × 700 so'm"},
		{ref, "ai_premium", 0, priceInfo{"ai_premium", 1500, 1, "sahifa", 1500}, "1 sahifa × 1 500 so'm"},
		{prac, storage.KindPhoto, 0, priceInfo{storage.KindPhoto, 7000, 0, "", 7000}, ""},
		{prac, storage.KindPlain, 0, priceInfo{storage.KindPlain, 7000, 0, "", 7000}, ""},
		{prac, "ai_standard", 0, priceInfo{"ai_standard", 7000, 0, "", 7000}, ""},
		{prac, "ai_optimal", 0, priceInfo{"ai_optimal", 7000, 0, "", 7000}, ""},
		{prac, "ai_premium", 0, priceInfo{"ai_premium", 7000, 0, "", 7000}, ""},
	}
	for _, c := range cases {
		got := h.priceFor(c.work, c.volume, c.kind)
		if got != c.want {
			t.Errorf("priceFor(%s, %d, %s) = %+v, kutilgan %+v", c.work, c.volume, c.kind, got, c.want)
		}
		if d := got.detail(); d != c.detail {
			t.Errorf("priceFor(%s, %d, %s).detail() = %q, kutilgan %q", c.work, c.volume, c.kind, d, c.detail)
		}
		s := &storage.Session{WorkType: c.work, Volume: c.volume}
		if p := h.price(s, c.kind); p != c.want.Total {
			t.Errorf("price(%s, %d, %s) = %d, priceFor bilan mos emas (%d)", c.work, c.volume, c.kind, p, c.want.Total)
		}
	}
}
