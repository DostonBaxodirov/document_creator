package generation

import (
	"context"
	"strings"
	"testing"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/lang"
)

func TestWorkTypeTitles(t *testing.T) {
	ru := lang.Get(string(lang.Ru))
	for _, c := range []struct {
		wt   ai.WorkType
		want string
	}{{ai.WorkPresentation, "ПРЕЗЕНТАЦИЯ"}, {ai.WorkIndependent, "САМОСТОЯТЕЛЬНАЯ РАБОТА"}, {ai.WorkPractical, "ПРАКТИЧЕСКОЕ ЗАДАНИЕ"}} {
		if got := workTypeTitle(c.wt, ru); got != c.want {
			t.Errorf("%s: %q, kutilgan %q", c.wt, got, c.want)
		}
	}
	if workTypeTitle(ai.WorkIndependent, lang.Get("")) != "MUSTAQIL ISH" {
		t.Errorf("standart nom o'zbekcha bo'lishi kerak")
	}
}

// TestBuildUnknownWorkType — noma'lum ish turi AI chaqirilmasdan rad etiladi.
func TestBuildUnknownWorkType(t *testing.T) {
	s := New(Config{AI: failAI{}})
	out, err := s.Build(context.Background(), Order{WorkType: ai.WorkType("thesis"), Topic: "Mavzu", Volume: 10}, &testProgress{})
	if err == nil || out != nil || !strings.Contains(err.Error(), "noma'lum ish turi") {
		t.Errorf("noma'lum ish turi rad etilishi kerak: %+v, %v", out, err)
	}
}
