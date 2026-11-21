package generation

import (
	"slices"
	"strings"
	"testing"
	"unicode"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
)

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

func TestCustomCaption(t *testing.T) {
	if c := customCaptionName("Okean", config.TariffOptimal); c != "✨ Okean (maxsus dizayn, 🥈 Optimal)" {
		t.Errorf("customCaptionName = %q", c)
	}
	if c := customCaptionName("", config.TariffStandard); c != "✨ Maxsus dizayn (🥉 Standart)" {
		t.Errorf("customCaptionName (nomsiz) = %q", c)
	}
}

// TestNoAIMention — fayl izohida (foydalanuvchiga ko'rinadi) "AI" va
// "sun'iy intellekt" so'zlari bo'lmasligi kerak.
func TestNoAIMention(t *testing.T) {
	txt := customCaptionName("", config.TariffOptimal)
	for _, w := range strings.FieldsFunc(txt, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if w == "AI" || w == "Ai" {
			t.Errorf("caption matnida \"AI\" bor:\n%s", txt)
		}
	}
	if strings.Contains(strings.ToLower(txt), "intellekt") {
		t.Errorf("caption matnida \"sun'iy intellekt\" bor:\n%s", txt)
	}
}
