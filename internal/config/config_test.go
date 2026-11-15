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
