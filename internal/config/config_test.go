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
	if c.StandardProvider != ImageProviderOpenAI || c.PremiumProvider != ImageProviderGemini ||
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

func TestPaymentConfig(t *testing.T) {
	for _, k := range []string{"PAYMENT_ENABLED", "PAYMENT_CARD_NUMBER", "PAYMENT_CARD_HOLDER", "PRICE_PHOTO", "PRICE_PLAIN",
		"FREE_PHOTO", "FREE_PLAIN", "PAYMENT_VISION_MODEL", "PAYMENT_CHECK_TIMEOUT", "ADMIN_PASSWORD"} {
		t.Setenv(k, "")
	}
	p := &parser{}
	c := paymentConfig(p, AIConfig{OpenAIModel: "gpt-x"})
	if len(p.errs) > 0 {
		t.Fatal(p.errs)
	}
	if !c.Enabled || c.PricePhoto != 10000 || c.PricePlain != 7000 || c.FreePhoto != 1 || c.FreePlain != 1 || c.VisionModel != "gpt-x" {
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

	t.Setenv("PRICE_PLAIN", "0")
	t.Setenv("FREE_PHOTO", "-1")
	if errs := paymentConfig(&parser{}, AIConfig{}).validate(); len(errs) != 2 {
		t.Errorf("narx va bepul urinish xatolari kutilgan edi: %v", errs)
	}

	t.Setenv("PAYMENT_ENABLED", "false")
	t.Setenv("PAYMENT_CARD_NUMBER", "")
	t.Setenv("ADMIN_PASSWORD", "")
	if errs := paymentConfig(&parser{}, AIConfig{}).validate(); len(errs) > 0 {
		t.Errorf("o'chirilgan to'lov tekshirilmasligi kerak: %v", errs)
	}
}
