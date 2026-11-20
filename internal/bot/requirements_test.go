package bot

import (
	"strings"
	"testing"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/lang"
	"telegram-presentation-bot/internal/storage"
)

func TestFlowIncludesRequirementsBeforeConfirm(t *testing.T) {
	s := &storage.Session{}
	if got := steps[StateTeacher].nextState(s); got != StateRequirements {
		t.Errorf("tekshiruvchidan keyin %s, kutilgan %s", got, StateRequirements)
	}
	if got := steps[StateRequirements].nextState(s); got != StateConfirm {
		t.Errorf("talablardan keyin %s, kutilgan %s", got, StateConfirm)
	}
}

func TestRequirementsStep(t *testing.T) {
	st := steps[StateRequirements]

	if got := st.suggest(config.LimitsConfig{}, &storage.Session{}); len(got) != 1 || got[0] != skipRequirements {
		t.Errorf("suggest = %v, kutilgan o'tkazib yuborish tugmasi", got)
	}
	for _, skip := range []string{skipRequirements, "yo'q", "Yo‘q", "  YOQ. ", "-", "нет", "No"} {
		s := &storage.Session{Requirements: "eski"}
		if err := st.apply(config.LimitsConfig{}, s, skip); err != nil || s.Requirements != "" {
			t.Errorf("%q: talablar tozalanishi kerak (err=%v, got=%q)", skip, err, s.Requirements)
		}
	}

	s := &storage.Session{}
	if err := st.apply(config.LimitsConfig{}, s, "  Ko'proq   amaliy\nmisollar bo'lsin  "); err != nil || s.Requirements != "Ko'proq amaliy misollar bo'lsin" {
		t.Errorf("oddiy talab saqlanishi kerak: err=%v, got=%q", err, s.Requirements)
	}
	long := strings.Repeat("a", maxRequirementsRunes+1)
	if err := st.apply(config.LimitsConfig{}, &storage.Session{}, long); err == nil {
		t.Errorf("%d belgidan uzun talab rad etilishi kerak", maxRequirementsRunes)
	}
	if err := st.apply(config.LimitsConfig{}, &storage.Session{}, "12"); err == nil {
		t.Errorf("harfsiz qisqa javob rad etilishi kerak")
	}
}

// TestPersonNameStep — F.I.Sh. maydoniga ilmiy daraja, bir nechta talaba va
// guruh raqami yozish mumkin; havola, @username va faqat raqamlar — yo'q.
func TestPersonNameStep(t *testing.T) {
	for _, ok := range []string{
		"Aliyev Vali", "O'tkir G‘ulomov", "i.f.n., dotsent Karimova D.A.",
		"Aliyev Vali, Karimov Sardor", "Aliyeva M. (301-21 guruh)",
	} {
		if v, err := validatePersonName(ok, "F.I.Sh."); err != nil || v == "" {
			t.Errorf("%q qabul qilinishi kerak: %v", ok, err)
		}
	}
	for _, bad := range []string{"https://t.me/x", "@aliyev", "+998 90 123 45 67", "12 345", "Al1"} {
		if _, err := validatePersonName(bad, "F.I.Sh."); err == nil {
			t.Errorf("%q rad etilishi kerak", bad)
		}
	}
	s := &storage.Session{}
	if err := steps[StateTeacher].apply(config.LimitsConfig{}, s, "i.f.n., dotsent Karimova D."); err != nil || s.TeacherName != "i.f.n., dotsent Karimova D." {
		t.Errorf("tekshiruvchi darajasi bilan saqlanishi kerak: err=%v, got=%q", err, s.TeacherName)
	}
}

func TestTopicPromptHintsLanguage(t *testing.T) {
	p := steps[StateTopic].prompt
	if strings.Contains(p(config.LimitsConfig{}, &storage.Session{}), "🌐") {
		t.Errorf("o'zbek (lotin) tilida til maslahati bo'lmasligi kerak")
	}
	if got := p(config.LimitsConfig{}, &storage.Session{Language: string(lang.Ru)}); !strings.Contains(got, "Русский") {
		t.Errorf("ruscha ishda til maslahati yo'q:\n%s", got)
	}
}

func TestSummaryShowsLanguageAndRequirements(t *testing.T) {
	s := &storage.Session{WorkType: string(ai.WorkIndependent), Topic: "T", Volume: 10}
	got := summaryText(s)
	if !strings.Contains(got, "Til:</b> O&#39;zbekcha (lotin)") {
		t.Errorf("til tanlanmagan bo'lsa o'zbekcha ko'rsatilishi kerak:\n%s", got)
	}
	if strings.Contains(got, "Qo'shimcha talablar") {
		t.Errorf("talab bo'lmasa qatori ham bo'lmasligi kerak")
	}

	s.Language = string(lang.En)
	s.Requirements = "<b>formulalar</b> bilan " + strings.Repeat("x", 500)
	got = summaryText(s)
	if !strings.Contains(got, "Til:</b> English") {
		t.Errorf("tanlangan til ko'rsatilmagan:\n%s", got)
	}
	if !strings.Contains(got, "Qo'shimcha talablar:</b> &lt;b&gt;formulalar") {
		t.Errorf("talablar HTML-escape qilinib ko'rsatilishi kerak:\n%s", got)
	}
	if strings.Contains(got, strings.Repeat("x", 400)) {
		t.Errorf("uzun talablar xulosada qisqartirilishi kerak")
	}
}

func TestLanguageKeyboardAndWorkTitles(t *testing.T) {
	kb := languageKeyboard()
	var n int
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			if _, ok := lang.Find(b.Data); !ok || b.Unique != cbLanguage {
				t.Errorf("noto'g'ri tugma: %+v", b)
			}
			n++
		}
	}
	if n != len(lang.All()) {
		t.Errorf("%d ta til tugmasi, kutilgan %d", n, len(lang.All()))
	}

	ru := lang.Get(string(lang.Ru))
	for _, c := range []struct {
		wt   ai.WorkType
		want string
	}{{ai.WorkPresentation, "ПРЕЗЕНТАЦИЯ"}, {ai.WorkIndependent, "САМОСТОЯТЕЛЬНАЯ РАБОТА"}, {ai.WorkPractical, "ПРАКТИЧЕСКОЕ ЗАДАНИЕ"}} {
		opt, _ := findWorkType(c.wt)
		if got := opt.Title(ru); got != c.want {
			t.Errorf("%s: %q, kutilgan %q", c.wt, got, c.want)
		}
	}
	if opt, _ := findWorkType(ai.WorkIndependent); opt.Title(lang.Get("")) != "MUSTAQIL ISH" {
		t.Errorf("standart nom o'zbekcha bo'lishi kerak")
	}
}

func TestResetDraftClearsLanguageAndRequirements(t *testing.T) {
	s := &storage.Session{Language: "ru", Requirements: "r", StudentName: "Aliyev Vali"}
	resetDraft(s)
	if s.Language != "" || s.Requirements != "" {
		t.Errorf("resetDraft til va talablarni tozalashi kerak: %+v", s)
	}
	if s.StudentName == "" {
		t.Errorf("profil ma'lumotlari saqlanishi kerak")
	}
}
