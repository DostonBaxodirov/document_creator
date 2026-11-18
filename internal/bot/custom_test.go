package bot

import (
	"context"
	"slices"
	"strings"
	"testing"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

// testTariffs — skrinshotdagi narxlar bo'yicha tariflar.
func testTariffs() map[string]CustomTariff {
	return map[string]CustomTariff{
		config.TariffStandard: {PriceSlide: 300, ImagePercent: 40, TextModel: "gpt-4o-mini", ImageModel: "gpt-image-1", ImageQuality: "low"},
		config.TariffOptimal:  {PriceSlide: 500, ImagePercent: 40, TextModel: "gpt-4.1-mini", ImageModel: "gpt-image-1", ImageQuality: "medium"},
		config.TariffPremium:  {PriceSlide: 1700, ImagePercent: 50, TextModel: "gpt-4o", ImageModel: "gpt-image-1", ImageQuality: "high"},
	}
}

func customHandler(fb *fakeBilling) *Handler {
	return NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{
		Limits:  Limits{MinSlides: 5, MaxSlides: 50, MinPages: 5, MaxPages: 30},
		Images:  fakeImages{},
		Billing: fb,
		Payment: PaymentOptions{Enabled: true, PriceSlidePhoto: 300, PriceSlidePlain: 250, PriceDocument: 7000, FreePhoto: 1, FreePlain: 1},
		Custom:  testTariffs(),
	})
}

// TestCustomOptionsMatchGenerator — AI promptidagi variantlar generator
// chiza oladigan variantlar bilan bir xil (aks holda AI tanlovi e'tiborsiz qoladi).
func TestCustomOptionsMatchGenerator(t *testing.T) {
	pairs := map[string][2][]string{
		"cover":        {ai.DesignCovers, generator.CustomCovers},
		"header":       {ai.DesignHeaders, generator.CustomHeaders},
		"cards":        {ai.DesignCards, generator.CustomCards},
		"corners":      {ai.DesignCorners, generator.CustomCorners},
		"markers":      {ai.DesignMarkers, generator.CustomMarkers},
		"motif":        {ai.DesignMotifs, generator.CustomMotifs},
		"photo_side":   {ai.DesignSides, generator.CustomSides},
		"heading_font": {ai.DesignHeadingFonts, generator.CustomHeadingFonts},
		"body_font":    {ai.DesignBodyFonts, generator.CustomBodyFonts},
	}
	for name, p := range pairs {
		if !slices.Equal(p[0], p[1]) {
			t.Errorf("%s: ai %v ≠ generator %v", name, p[0], p[1])
		}
	}
}

func TestCustomPricing(t *testing.T) {
	fb := &fakeBilling{used: map[string]int{}}
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
	if h.freeLimit(storage.AIKind(config.TariffPremium)) != 0 {
		t.Error("AI maxsus dizaynda bepul urinish bo'lmasligi kerak")
	}
	o := &storage.Order{ID: 7, WorkType: string(ai.WorkPresentation), Kind: storage.AIKind(config.TariffOptimal), Price: 10000, Volume: 20}
	if txt := h.paymentText(o, true); strings.Contains(txt, "Bepul sinov") || !strings.Contains(txt, "AI maxsus dizayn — 🥈 Optimal") {
		t.Errorf("to'lov matni:\n%s", txt)
	}
	if l := orderLabel(o.WorkType, o.Kind); l != "Slayd (taqdimot), ✨ AI maxsus dizayn — 🥈 Optimal" {
		t.Errorf("orderLabel = %q", l)
	}
}

func TestTariffPromptAndKeyboard(t *testing.T) {
	h := customHandler(&fakeBilling{})
	s := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeCustom, Topic: "Okean ekologiyasi", Volume: 20}
	p := h.tariffPrompt(s)
	for _, want := range []string{"Okean ekologiyasi", "Standart", "6 000 so'm", "Optimal", "10 000 so'm", "Premium", "34 000 so'm",
		"8 ta AI rasm", "10 ta AI rasm", "bepul urinishlarga kirmaydi"} {
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
	if p := designPrompt(true); !strings.Contains(p, "AI maxsus dizayn") || len([]rune(p)) > 1024 {
		t.Errorf("dizayn matni (%d belgi):\n%s", len([]rune(p)), p)
	}
}

func TestCustomSummaryAndKind(t *testing.T) {
	s := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeCustom, Tariff: config.TariffPremium, Topic: "T", Volume: 10}
	if !isCustom(s) || orderKind(s) != "ai_premium" {
		t.Errorf("isCustom=%v kind=%s", isCustom(s), orderKind(s))
	}
	if txt := summaryText(s); !strings.Contains(txt, "AI maxsus dizayn · 🥇 Premium") {
		t.Errorf("xulosada tarif yo'q:\n%s", txt)
	}
	s.WorkType = string(ai.WorkIndependent)
	if isCustom(s) || strings.Contains(summaryText(s), "Dizayn") {
		t.Error("hujjatda AI dizayn bo'lmasligi kerak")
	}
	resetDraft(s)
	if s.Tariff != "" {
		t.Error("resetDraft tarifni tozalashi kerak")
	}
}

func TestCustomImageCount(t *testing.T) {
	cases := []struct{ slides, percent, want int }{
		{20, 40, 8}, {20, 50, 10}, {5, 40, 2}, {50, 50, 25}, {3, 10, 1}, {10, 0, 1},
	}
	for _, c := range cases {
		if got := customImageCount(c.slides, c.percent); got != c.want {
			t.Errorf("%d slayd × %d%% = %d, kutilgan %d", c.slides, c.percent, got, c.want)
		}
	}
}

func TestCustomPhotoTargets(t *testing.T) {
	spec := generator.SampleCustomSpecs()[0]
	deck := generator.SampleCustomDeck(spec)
	pres := &ai.Presentation{}
	for i := range deck.Slides {
		img := "photo"
		if i == 0 {
			img = "" // tavsifsiz slayd tanlanmaydi
		}
		pres.Slides = append(pres.Slides, ai.Slide{Image: img})
	}
	design := generator.DesignFor(generator.ThemeCustom, &spec)
	got := customPhotoTargets(design, pres, &deck, 4)
	if len(got) != 4 || slices.Contains(got, 0) || !slices.IsSorted(got) {
		t.Errorf("rasmli slaydlar: %v", got)
	}
}

func TestCustomSpecFromAI(t *testing.T) {
	d := &ai.Design{Name: "Okean", Colors: ai.DesignColors{Background: "#FFFFFF", Primary: "#123456"},
		HeadingFont: "Georgia", Cover: "full", ImageStyle: "cool tones", SignatureLayouts: []string{"hub"}}
	sp := customSpec(d)
	if sp.Name != "Okean" || sp.Background != "#FFFFFF" || sp.Primary != "#123456" || sp.Cover != "full" ||
		sp.PhotoStyle != "cool tones" || !slices.Equal(sp.Signature, []string{"hub"}) {
		t.Errorf("spetsifikatsiya: %+v", sp)
	}
	if c := customCaption(d, config.TariffOptimal); c != "✨ Okean (AI dizayn, 🥈 Optimal)" {
		t.Errorf("customCaption = %q", c)
	}
	if c := customCaption(&ai.Design{}, config.TariffStandard); c != "✨ AI maxsus dizayn (🥉 Standart)" {
		t.Errorf("customCaption (nomsiz) = %q", c)
	}
}

func TestGuideText(t *testing.T) {
	h := customHandler(&fakeBilling{})
	g := h.guideText()
	for _, want := range []string{"qo'llanma", "/start", "/cancel", "5–50 slayd", "5–30 sahifa", "250 so'm", "7 000 so'm",
		"Birinchi 1 ta rasmli va 1 ta rasmsiz ish — <b>bepul</b>", "AI maxsus dizayn bepul urinishlarga kirmaydi", "AI maxsus dizayn — tariflar", "Standart</b> — 300 so'm/slayd", "Premium</b> — 1 700 so'm/slayd",
		"20 slayd: Standart — 6 000 so'm, Optimal — 10 000 so'm, Premium — 34 000 so'm", "To'lov", "<b>3. Narxlar</b>", "<b>4. ✨"} {
		if !strings.Contains(g, want) {
			t.Errorf("qo'llanmada %q yo'q", want)
		}
	}
	if n := visibleLen(g); n > 4096 {
		t.Errorf("qo'llanma Telegram chegarasidan uzun: %d", n)
	}

	// To'lov va AI dizayn o'chirilgan: narxlar va tariflar bo'limi yo'q.
	h = NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{Limits: Limits{MinSlides: 5, MaxSlides: 50, MinPages: 5, MaxPages: 30}})
	g = h.guideText()
	for _, unwanted := range []string{"Narxlar", "so'm", "AI maxsus dizayn", "To'lov"} {
		if strings.Contains(g, unwanted) {
			t.Errorf("o'chirilgan holatda qo'llanmada %q bo'lmasligi kerak", unwanted)
		}
	}
	if !strings.Contains(customHandler(&fakeBilling{}).pricingText(), "AI maxsus dizayn (har bir slayd): Standart — 300 so'm") {
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
