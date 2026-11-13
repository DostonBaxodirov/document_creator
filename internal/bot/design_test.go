package bot

import (
	"strings"
	"testing"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

func TestVolumeRoutesToDesignOnlyForPresentation(t *testing.T) {
	st := steps[StateVolume]
	cases := map[ai.WorkType]State{
		ai.WorkPresentation: StateDesign,
		ai.WorkIndependent:  StateStudent,
		ai.WorkPractical:    StateStudent,
	}
	for wt, want := range cases {
		if got := st.nextState(&storage.Session{WorkType: string(wt)}); got != want {
			t.Errorf("%s: keyingi qadam %s, kutilgan %s", wt, got, want)
		}
	}
	if got := steps[StateTopic].nextState(&storage.Session{}); got != StateVolume {
		t.Errorf("route'siz qadam next ni qaytarishi kerak, olindi %s", got)
	}
}

func TestDesignKeyboard(t *testing.T) {
	kb := designKeyboard()
	var ids []string
	for _, row := range kb.InlineKeyboard {
		if len(row) > 2 {
			t.Errorf("qatorda %d ta tugma (ko'pi bilan 2)", len(row))
		}
		for _, b := range row {
			if b.Unique != cbDesign {
				t.Errorf("tugma callback'i noto'g'ri: %q", b.Unique)
			}
			ids = append(ids, b.Data)
		}
	}
	themes := generator.Themes()
	if len(ids) != len(themes) {
		t.Fatalf("%d ta tugma, kutilgan %d", len(ids), len(themes))
	}
	for i, id := range ids {
		if id != themes[i].ID {
			t.Errorf("tugma %d: %q, kutilgan %q", i, id, themes[i].ID)
		}
	}
}

func TestDesignPromptAndSummary(t *testing.T) {
	p := designPrompt()
	for i, th := range generator.Themes() {
		if !strings.Contains(p, th.Name) || !strings.Contains(p, string(rune('1'+i))+". ") {
			t.Errorf("dizayn matnida %d. %s yo'q", i+1, th.Name)
		}
	}
	if len([]rune(p)) > 1024 {
		t.Errorf("rasm izohi Telegram chegarasidan (1024) uzun: %d", len([]rune(p)))
	}

	s := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeModern, Topic: "T", Volume: 10}
	if !strings.Contains(summaryText(s), "Dizayn:</b> 💜 Zamonaviy") {
		t.Errorf("xulosada dizayn yo'q:\n%s", summaryText(s))
	}
	s.WorkType = string(ai.WorkIndependent)
	if strings.Contains(summaryText(s), "Dizayn") {
		t.Errorf("hujjat xulosasida dizayn bo'lmasligi kerak")
	}
}
