package config

import (
	"strings"
	"testing"
)

func TestImagesConfigDefaultsAndValidation(t *testing.T) {
	for _, k := range []string{"IMAGE_STANDARD_PROVIDER", "IMAGE_PREMIUM_PROVIDER", "IMAGE_DEFAULT_TIER",
		"OPENAI_IMAGE_MODEL", "OPENAI_IMAGE_QUALITY", "GEMINI_IMAGE_MODEL", "GEMINI_IMAGE_SIZE",
		"IMAGE_TIMEOUT", "IMAGE_PARALLEL_REQUESTS", "IMAGE_FALLBACK"} {
		t.Setenv(k, "")
	}
	p := &parser{}
	c := imagesConfig(p)
	if len(p.errs) > 0 || len(c.validate()) > 0 {
		t.Fatalf("standart sozlamalar yaroqli bo'lishi kerak: %v %v", p.errs, c.validate())
	}
	if c.StandardProvider != ImageProviderOpenAI || c.PremiumProvider != ImageProviderOpenAI ||
		c.DefaultTier != TierStandard || c.OpenAIModel != "gpt-image-2" || c.OpenAIQuality != "low" || !c.Fallback {
		t.Errorf("standart qiymatlar noto'g'ri: %+v", c)
	}

	t.Setenv("IMAGE_STANDARD_PROVIDER", "midjourney")
	t.Setenv("IMAGE_DEFAULT_TIER", "gold")
	t.Setenv("OPENAI_IMAGE_QUALITY", "ultra")
	t.Setenv("GEMINI_IMAGE_SIZE", "8k")
	t.Setenv("IMAGE_PARALLEL_REQUESTS", "0")
	errs := imagesConfig(&parser{}).validate()
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	for _, want := range []string{"IMAGE_STANDARD_PROVIDER", "IMAGE_DEFAULT_TIER", "OPENAI_IMAGE_QUALITY", "GEMINI_IMAGE_SIZE", "IMAGE_PARALLEL_REQUESTS"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s xatosi kutilgan edi:\n%s", want, joined)
		}
	}
}

// TestAIOpenAIStructured — AI_OPENAI_STRUCTURED: standart true, false bilan o'chadi.
func TestAIOpenAIStructured(t *testing.T) {
	t.Setenv("AI_OPENAI_STRUCTURED", "")
	p := &parser{}
	if c := aiConfig(p); len(p.errs) > 0 || !c.OpenAIStructured {
		t.Fatalf("standart qiymat true bo'lishi kerak: %v", p.errs)
	}
	t.Setenv("AI_OPENAI_STRUCTURED", "false")
	p = &parser{}
	if c := aiConfig(p); len(p.errs) > 0 || c.OpenAIStructured {
		t.Fatalf("false berilganda o'chishi kerak: %v", p.errs)
	}
}

// TestAISlidesPerRequest — AI_SLIDES_PER_REQUEST: standart 6, ruxsat etilgan
// oraliq 3..10, son bo'lmasa — format xatosi.
func TestAISlidesPerRequest(t *testing.T) {
	t.Setenv("AI_SLIDES_PER_REQUEST", "")
	p := &parser{}
	if c := aiConfig(p); len(p.errs) > 0 || c.SlidesPerRequest != 6 {
		t.Fatalf("standart qiymat 6 bo'lishi kerak: %d %v", c.SlidesPerRequest, p.errs)
	}

	for v, valid := range map[string]bool{"3": true, "6": true, "10": true, "2": false, "11": false, "0": false, "-4": false} {
		t.Setenv("AI_SLIDES_PER_REQUEST", v)
		p := &parser{}
		cfg := &Config{AI: aiConfig(p)}
		if len(p.errs) > 0 {
			t.Fatalf("%s: %v", v, p.errs)
		}
		err := cfg.validate()
		if hasErr := err != nil && strings.Contains(err.Error(), "AI_SLIDES_PER_REQUEST"); hasErr == valid {
			t.Errorf("AI_SLIDES_PER_REQUEST=%s: yaroqli=%v kutilgan, xato: %v", v, valid, err)
		}
	}

	t.Setenv("AI_SLIDES_PER_REQUEST", "olti")
	p = &parser{}
	aiConfig(p)
	if len(p.errs) != 1 || !strings.Contains(p.errs[0].Error(), "AI_SLIDES_PER_REQUEST") {
		t.Errorf("format xatosi kutilgan edi: %v", p.errs)
	}
}

func TestLimitsValidation(t *testing.T) {
	// Kichik va katta qiymatlar ruxsat etiladi — chegaralar to'liq sozlamalarga bog'liq.
	for _, l := range []LimitsConfig{
		{MinSlides: 1, MaxSlides: 1, MinPages: 1, MaxPages: 1},
		{MinSlides: 2, MaxSlides: 200, MinPages: 2, MaxPages: 150},
	} {
		if errs := l.validate(); len(errs) > 0 {
			t.Errorf("%+v yaroqli bo'lishi kerak: %v", l, errs)
		}
	}

	errs := LimitsConfig{MinSlides: 0, MaxSlides: 10, MinPages: 20, MaxPages: 10}.validate()
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	for _, want := range []string{"MIN_SLIDES", "MAX_PAGES"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s xatosi kutilgan edi:\n%s", want, joined)
		}
	}
}

func TestPaymentConfig(t *testing.T) {
	for _, k := range []string{"PAYMENT_ENABLED", "PAYMENT_CARD_NUMBER", "PAYMENT_CARD_HOLDER", "PRICE_SLIDE_PHOTO", "PRICE_SLIDE_PLAIN", "PRICE_DOCUMENT",
		"PAYMENT_VISION_MODEL", "PAYMENT_CHECK_TIMEOUT", "ADMIN_PASSWORD"} {
		t.Setenv(k, "")
	}
	p := &parser{}
	c := paymentConfig(p, AIConfig{OpenAIModel: "gpt-x"})
	if len(p.errs) > 0 {
		t.Fatal(p.errs)
	}
	if !c.Enabled || c.PriceSlidePhoto != 300 || c.PriceSlidePlain != 250 || c.PriceDocument != 7000 || c.VisionModel != "gpt-x" {
		t.Errorf("standart qiymatlar noto'g'ri: %+v", c)
	}
	// Yoqilgan, lekin karta va parol yo'q — xato.
	joined := ""
	for _, e := range c.validate() {
		joined += e.Error() + "\n"
	}
	for _, want := range []string{"PAYMENT_CARD_NUMBER", "ADMIN_PASSWORD"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s xatosi kutilgan edi:\n%s", want, joined)
		}
	}

	t.Setenv("PAYMENT_CARD_NUMBER", "8600 1234 5678 9012")
	t.Setenv("ADMIN_PASSWORD", "maxfiy-parol-123")
	c = paymentConfig(&parser{}, AIConfig{})
	if errs := c.validate(); len(errs) > 0 {
		t.Errorf("to'g'ri sozlamalar xato berdi: %v", errs)
	}
	if c.CardLast4() != "9012" {
		t.Errorf("CardLast4 = %q", c.CardLast4())
	}

	t.Setenv("PRICE_DOCUMENT", "6000")
	if c := paymentConfig(&parser{}, AIConfig{}); c.PriceDocument != 6000 {
		t.Errorf("PRICE_DOCUMENT o'qilmadi: %d", c.PriceDocument)
	}

	t.Setenv("PRICE_SLIDE_PLAIN", "0")
	if errs := paymentConfig(&parser{}, AIConfig{}).validate(); len(errs) != 1 {
		t.Errorf("narx xatosi kutilgan edi: %v", errs)
	}

	t.Setenv("PAYMENT_ENABLED", "false")
	t.Setenv("PAYMENT_CARD_NUMBER", "")
	t.Setenv("ADMIN_PASSWORD", "")
	if errs := paymentConfig(&parser{}, AIConfig{}).validate(); len(errs) > 0 {
		t.Errorf("o'chirilgan to'lov tekshirilmasligi kerak: %v", errs)
	}
}

func TestCustomConfig(t *testing.T) {
	keys := []string{"CUSTOM_DESIGN_ENABLED", "CUSTOM_IMAGE_MODEL"}
	for _, tier := range []string{"STANDARD", "OPTIMAL", "PREMIUM"} {
		for _, f := range []string{"PRICE", "PAGE_PRICE", "TEXT_MODEL", "IMAGE_QUALITY", "IMAGE_PERCENT"} {
			keys = append(keys, "CUSTOM_"+tier+"_"+f)
		}
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
	p := &parser{}
	c := customConfig(p)
	if len(p.errs) > 0 || len(c.validate()) > 0 {
		t.Fatalf("standart sozlamalar yaroqli bo'lishi kerak: %v %v", p.errs, c.validate())
	}
	if !c.Enabled || c.ImageModel != "gpt-image-1" {
		t.Errorf("standart qiymatlar: %+v", c)
	}
	want := map[string]CustomTier{
		TariffStandard: {PriceSlide: 300, PricePage: 400, TextModel: "gpt-4o-mini", ImageQuality: "low", ImagePercent: 40},
		TariffOptimal:  {PriceSlide: 500, PricePage: 700, TextModel: "gpt-4.1-mini", ImageQuality: "medium", ImagePercent: 40},
		TariffPremium:  {PriceSlide: 1700, PricePage: 1500, TextModel: "gpt-4o", ImageQuality: "high", ImagePercent: 50},
	}
	for _, name := range Tariffs {
		if got, ok := c.Tier(name); !ok || got != want[name] {
			t.Errorf("%s: %+v, kutilgan %+v", name, got, want[name])
		}
	}

	t.Setenv("CUSTOM_PREMIUM_PRICE", "2000")
	t.Setenv("CUSTOM_OPTIMAL_IMAGE_QUALITY", "HD")
	t.Setenv("CUSTOM_STANDARD_IMAGE_PERCENT", "150")
	c = customConfig(&parser{})
	if c.Tiers[TariffPremium].PriceSlide != 2000 {
		t.Errorf("narx o'qilmadi: %d", c.Tiers[TariffPremium].PriceSlide)
	}
	if errs := c.validate(); len(errs) != 2 {
		t.Errorf("rasm sifati va foiz xatolari kutilgan edi: %v", errs)
	}
	t.Setenv("CUSTOM_DESIGN_ENABLED", "false")
	if errs := customConfig(&parser{}).validate(); len(errs) > 0 {
		t.Errorf("o'chirilgan maxsus dizayn tekshirilmasligi kerak: %v", errs)
	}
}

func TestBackupConfig(t *testing.T) {
	for _, k := range []string{"BACKUP_ENABLED", "BACKUP_TIME", "BACKUP_PG_DUMP", "BACKUP_TIMEOUT"} {
		t.Setenv(k, "")
	}
	p := &parser{}
	c := backupConfig(p)
	if !c.Enabled || c.Hour != 3 || c.Minute != 0 || c.PgDump != "pg_dump" || c.Timeout <= 0 || len(p.errs) > 0 {
		t.Fatalf("standart qiymatlar: %+v %v", c, p.errs)
	}

	t.Setenv("BACKUP_TIME", "21:45")
	t.Setenv("BACKUP_ENABLED", "false")
	p = &parser{}
	if c = backupConfig(p); c.Enabled || c.Hour != 21 || c.Minute != 45 || len(p.errs) > 0 {
		t.Errorf("BACKUP_TIME=21:45: %+v %v", c, p.errs)
	}

	for _, bad := range []string{"25:00", "3", "03:60", "kechasi"} {
		t.Setenv("BACKUP_TIME", bad)
		p = &parser{}
		if backupConfig(p); len(p.errs) != 1 || !strings.Contains(p.errs[0].Error(), "BACKUP_TIME") {
			t.Errorf("BACKUP_TIME=%q xato berishi kerak: %v", bad, p.errs)
		}
	}
}
