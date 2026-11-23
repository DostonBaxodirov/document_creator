package bot

import (
	"context"
	"slices"
	"strings"
	"testing"
	"unicode"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generation"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

// testTariffs — skrinshotdagi narxlar bo'yicha tariflar.
func testTariffs() map[string]CustomTariff {
	return map[string]CustomTariff{
		config.TariffStandard: {PriceSlide: 300, PricePage: 400, Tariff: generation.Tariff{ImagePercent: 40, TextModel: "gpt-4o-mini", ImageModel: "gpt-image-1", ImageQuality: "low"}},
		config.TariffOptimal:  {PriceSlide: 500, PricePage: 700, Tariff: generation.Tariff{ImagePercent: 40, TextModel: "gpt-4.1-mini", ImageModel: "gpt-image-1", ImageQuality: "medium"}},
		config.TariffPremium:  {PriceSlide: 1700, PricePage: 1500, Tariff: generation.Tariff{ImagePercent: 50, TextModel: "gpt-4o", ImageModel: "gpt-image-1", ImageQuality: "high"}},
	}
}

func customHandler(fb *fakeBilling) *Handler {
	return NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{
		Limits:  config.LimitsConfig{MinSlides: 5, MaxSlides: 50, MinPages: 5, MaxPages: 30},
		Images:  fakeImages{},
		Billing: fb,
		Payment: config.PaymentConfig{Enabled: true, PriceSlidePhoto: 300, PriceSlidePlain: 250, PriceDocument: 7000, PricePage: 200},
		Custom:  testTariffs(),
	})
}

func TestCustomPricing(t *testing.T) {
	fb := &fakeBilling{}
	h := customHandler(fb)
	u := &storage.User{ID: 1, TelegramUserID: 42}
	cases := map[string]int{config.TariffStandard: 6000, config.TariffOptimal: 10000, config.TariffPremium: 34000}
	for tariff, want := range cases {
		s := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeCustom, Tariff: tariff, Volume: 20}
		if k := h.kindOf(s); k != storage.AIKind(tariff) {
			t.Errorf("%s: toifa %s", tariff, k)
		}
		q, err := h.quote(context.Background(), s, u)
		if err != nil {
			t.Fatal(err)
		}
		// Bepul urinishlar ishlatilmagan bo'lsa ham AI maxsus dizayn pullik.
		if q.mode != quotePaid || q.price != want {
			t.Errorf("%s: 20 slayd %+v, kutilgan %d so'm (pullik)", tariff, q, want)
		}
		if !strings.Contains(q.line(), "20 slayd × "+formatSum(h.opts.Custom[tariff].PriceSlide)) {
			t.Errorf("%s: narx qatori %q", tariff, q.line())
		}
	}
	o := &storage.Order{ID: 7, WorkType: string(ai.WorkPresentation), Kind: storage.AIKind(config.TariffOptimal), Price: 10000, Volume: 20}
	if txt := h.paymentText(o, true); strings.Contains(txt, "Bepul sinov") || !strings.Contains(txt, "Maxsus dizayn — 🥈 Optimal") {
		t.Errorf("to'lov matni:\n%s", txt)
	}
	if l := orderLabel(o.WorkType, o.Kind); l != "Slayd (taqdimot), ✨ Maxsus dizayn — 🥈 Optimal" {
		t.Errorf("orderLabel = %q", l)
	}
}

func TestTariffPromptAndKeyboard(t *testing.T) {
	h := customHandler(&fakeBilling{})
	s := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeCustom, Topic: "Okean ekologiyasi", Volume: 20}
	p := h.tariffPrompt(s)
	for _, want := range []string{"Okean ekologiyasi", "Standart", "6 000 so'm", "Optimal", "10 000 so'm", "Premium", "34 000 so'm",
		"8 ta rasm", "10 ta rasm"} {
		if !strings.Contains(p, want) {
			t.Errorf("tarif matnida %q yo'q:\n%s", want, p)
		}
	}
	kb := h.tariffKeyboard(s)
	var data []string
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			if b.Unique != cbTariff {
				t.Errorf("tugma callback'i: %q", b.Unique)
			}
			data = append(data, b.Data)
		}
	}
	if want := []string{config.TariffStandard, config.TariffOptimal, config.TariffPremium, tariffBack}; !slices.Equal(data, want) {
		t.Errorf("tarif tugmalari %v, kutilgan %v", data, want)
	}
	if !strings.Contains(kb.InlineKeyboard[2][0].Text, "Premium — 34 000 so'm") {
		t.Errorf("premium tugmasi: %q", kb.InlineKeyboard[2][0].Text)
	}

	// Dizayn tugmalari: AI maxsus dizayn birinchi qatorda (faqat yoqilgan bo'lsa).
	if kb := designKeyboard(true); kb.InlineKeyboard[0][0].Data != generator.ThemeCustom {
		t.Errorf("birinchi tugma AI maxsus dizayn bo'lishi kerak: %+v", kb.InlineKeyboard[0][0])
	}
	for _, row := range designKeyboard(false).InlineKeyboard {
		for _, b := range row {
			if b.Data == generator.ThemeCustom {
				t.Error("o'chirilgan AI dizayn tugmasi ko'rsatilmasligi kerak")
			}
		}
	}
	if p := designPrompt(true); !strings.Contains(p, "Maxsus dizayn") || len([]rune(p)) > 1024 {
		t.Errorf("dizayn matni (%d belgi):\n%s", len([]rune(p)), p)
	}
}

func TestCustomSummaryAndKind(t *testing.T) {
	s := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeCustom, Tariff: config.TariffPremium, Topic: "T", Volume: 10}
	if !isCustom(s) || orderKind(s) != "ai_premium" {
		t.Errorf("isCustom=%v kind=%s", isCustom(s), orderKind(s))
	}
	if txt := summaryText(s); !strings.Contains(txt, "Maxsus dizayn · 🥇 Premium") {
		t.Errorf("xulosada tarif yo'q:\n%s", txt)
	}
	for _, w := range []ai.WorkType{ai.WorkIndependent, ai.WorkReferat} {
		s.WorkType = string(w)
		if !isCustom(s) || orderKind(s) != "ai_premium" || !strings.Contains(summaryText(s), "Maxsus dizayn · 🥇 Premium") {
			t.Errorf("%s: maxsus dizayn bo'lishi kerak: %s", w, summaryText(s))
		}
	}
	s.WorkType = string(ai.WorkPractical)
	if isCustom(s) || strings.Contains(summaryText(s), "Dizayn") {
		t.Error("amaliy topshiriqda dizayn bo'lmasligi kerak")
	}
	s.WorkType, s.Design = string(ai.WorkReferat), generator.DocThemeAcademic
	if isCustom(s) || orderKind(s) != storage.KindPlain || !strings.Contains(summaryText(s), "Dizayn:</b> 🎓 Akademik") {
		t.Errorf("tayyor Word dizayni xulosada ko'rinishi kerak:\n%s", summaryText(s))
	}
	s.Design = generator.ThemeCustom
	resetDraft(s)
	if s.Tariff != "" {
		t.Error("resetDraft tarifni tozalashi kerak")
	}
}

func TestGuideText(t *testing.T) {
	h := customHandler(&fakeBilling{})
	g := h.guideText()
	for _, want := range []string{"qo'llanma", "/start", "/cancel", "5–50 slayd", "5–30 sahifa", "250 so'm", "7 000 so'm",
		"Maxsus dizayn — tariflar", "Standart</b> — 300 so'm/slayd", "Premium</b> — 1 700 so'm/slayd",
		"20 slayd: Standart — 6 000 so'm, Optimal — 10 000 so'm, Premium — 34 000 so'm", "To'lov", "<b>3. Narxlar</b>", "<b>4. ✨"} {
		if !strings.Contains(g, want) {
			t.Errorf("qo'llanmada %q yo'q", want)
		}
	}
	if n := visibleLen(g); n > 4096 {
		t.Errorf("qo'llanma Telegram chegarasidan uzun: %d", n)
	}

	// To'lov va AI dizayn o'chirilgan: narxlar va tariflar bo'limi yo'q.
	h = NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{Limits: config.LimitsConfig{MinSlides: 5, MaxSlides: 50, MinPages: 5, MaxPages: 30}})
	g = h.guideText()
	for _, unwanted := range []string{"Narxlar", "so'm", "Maxsus dizayn", "To'lov"} {
		if strings.Contains(g, unwanted) {
			t.Errorf("o'chirilgan holatda qo'llanmada %q bo'lmasligi kerak", unwanted)
		}
	}
	if !strings.Contains(customHandler(&fakeBilling{}).pricingText(), "Maxsus dizayn: Standart — 300 so'm/slayd, 400 so'm/sahifa") {
		t.Errorf("qisqa narxlar matnida tariflar yo'q:\n%s", customHandler(&fakeBilling{}).pricingText())
	}
}

func TestReportCountsCustom(t *testing.T) {
	st := computeStats([]storage.Payment{
		{Status: storage.PaymentApproved, Kind: storage.KindPhoto, Amount: 3000},
		{Status: storage.PaymentApproved, Kind: storage.AIKind(config.TariffPremium), Amount: 34000},
		{Status: storage.PaymentApproved, Kind: storage.KindPlain, Amount: 7000},
	})
	if st.custom.count != 1 || st.custom.sum != 34000 || st.photo.count != 1 || st.plain.count != 1 || st.approved.sum != 44000 {
		t.Errorf("hisobot: %+v", st)
	}
}

// TestNoAIMention — foydalanuvchiga ko'rinadigan matnlarda "AI" va
// "sun'iy intellekt" so'zlari bo'lmasligi kerak.
func TestNoAIMention(t *testing.T) {
	h := customHandler(&fakeBilling{})
	s := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeCustom, Topic: "Okean", Volume: 20, Tariff: config.TariffPremium}
	texts := map[string]string{
		"welcome":   msgWelcome,
		"noCustom":  msgNoCustomCfg,
		"label":     customLabel,
		"guide":     h.guideText(),
		"pricing":   h.pricingText(),
		"design":    designPrompt(true),
		"docDesign": docDesignPrompt(true),
		"tariff":    h.tariffPrompt(s),
		"summary":   summaryText(s),
		"orderName": orderLabel(s.WorkType, storage.AIKind(config.TariffOptimal)),
	}
	for st, step := range steps {
		if step.prompt != nil {
			texts["step "+string(st)] = step.prompt(h.opts.Limits, s)
		}
	}
	for _, v := range tariffViews {
		texts["tariff "+v.name] = v.about
	}
	for name, txt := range texts {
		low := strings.ToLower(txt)
		for _, w := range strings.FieldsFunc(txt, func(r rune) bool { return !unicode.IsLetter(r) }) {
			if w == "AI" || w == "Ai" {
				t.Errorf("%s matnida \"AI\" bor:\n%s", name, txt)
			}
		}
		if strings.Contains(low, "intellekt") {
			t.Errorf("%s matnida \"sun'iy intellekt\" bor:\n%s", name, txt)
		}
	}
}
