package bot

import (
	"strconv"
	"strings"
	"testing"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/storage"
)

func TestVolumeRoutesToDesign(t *testing.T) {
	st := steps[StateVolume]
	cases := map[ai.WorkType]State{
		ai.WorkPresentation: StateDesign,
		ai.WorkIndependent:  StateDesign,
		ai.WorkReferat:      StateDesign,
		ai.WorkPractical:    StateStudent, // amaliy topshiriq — doim Klassik
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
	kb := designKeyboard(false)
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
	// Rasmli dizayn tugmalarida 📷 bor, rasmsizlarida — yo'q.
	k := 0
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			if has := strings.Contains(b.Text, "📷"); has != themes[k].Photos {
				t.Errorf("tugma %q: 📷 = %v, kutilgan %v", b.Text, has, themes[k].Photos)
			}
			k++
		}
	}
}

func TestDesignPromptAndSummary(t *testing.T) {
	p := designPrompt(true)
	for i, th := range generator.Themes() {
		if !strings.Contains(p, strconv.Itoa(i+1)+". "+th.Emoji+" <b>"+th.Name+"</b>") {
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

func TestImageTier(t *testing.T) {
	h := &Handler{opts: Options{DefaultImageTier: images.TierStandard}}
	cases := map[string]images.Tier{
		"":          images.TierStandard,
		"premium":   images.TierPremium,
		" Premium ": images.TierPremium,
		"standard":  images.TierStandard,
		"noma'lum":  images.TierStandard,
	}
	for plan, want := range cases {
		if got := h.imageTier(&storage.Session{Plan: plan}); got != want {
			t.Errorf("Plan=%q: %s, kutilgan %s", plan, got, want)
		}
	}
	h.opts.DefaultImageTier = images.TierPremium
	if got := h.imageTier(&storage.Session{}); got != images.TierPremium {
		t.Errorf("standart tarif premium bo'lsa, Plan bo'sh foydalanuvchi premium olishi kerak: %s", got)
	}
	h.opts.DefaultImageTier = ""
	if got := h.imageTier(&storage.Session{}); got != images.TierStandard {
		t.Errorf("sozlanmagan holatda standard bo'lishi kerak: %s", got)
	}
}

func TestProgressText(t *testing.T) {
	if got := progressText(0, 5, imageStage); !strings.HasPrefix(got, "🖼 <b>Rasmlar chizilmoqda...</b>") || !strings.HasSuffix(got, "0/5") {
		t.Errorf("rasm bosqichi = %q", got)
	}
	if got := progressText(3, 3, textStage("Slaydlar yozilmoqda...")); !strings.Contains(got, "Fayl tayyorlanmoqda") {
		t.Errorf("yakuniy bosqich = %q", got)
	}
	if got := progressText(1, 3, textStage("Slaydlar yozilmoqda...")); !strings.HasPrefix(got, "✍️ <b>Slaydlar yozilmoqda...</b>") {
		t.Errorf("yozish bosqichi = %q", got)
	}
}

func TestDesignPromptMarksPhotoThemes(t *testing.T) {
	p := designPrompt(false)
	for _, th := range generator.Themes() {
		line := th.Emoji + " <b>" + th.Name + "</b>"
		if th.Photos {
			line += " 📷"
		}
		if !strings.Contains(p, line) {
			t.Errorf("dizayn matnida %q yo'q", line)
		}
	}
	s := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeOcean, Topic: "T", Volume: 10}
	if !strings.Contains(summaryText(s), "Okean 📷") {
		t.Errorf("rasmli dizayn xulosada belgilanmagan:\n%s", summaryText(s))
	}
}

func TestTopicLengthLimit(t *testing.T) {
	apply := steps[StateTopic].apply
	// 500-belgi bo'sh joy bo'lmasligi kerak (validateText chetdagi bo'sh joylarni qirqadi).
	ok := []rune(strings.Repeat("Raqamli o'zbek iqtisodiyoti ", 30))[:maxTopicRunes]
	s := &storage.Session{}
	if err := apply(Limits{}, s, string(ok)); err != nil || len([]rune(s.Topic)) != maxTopicRunes {
		t.Errorf("%d belgilik mavzu qabul qilinishi kerak: %v", maxTopicRunes, err)
	}
	if err := apply(Limits{}, &storage.Session{}, string(ok)+"x"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("%d belgidan uzun mavzu rad etilishi kerak: %v", maxTopicRunes, err)
	}
}
