package ai

import (
	"strings"
	"testing"

	"telegram-presentation-bot/internal/design"
)

func TestParseDocDesign(t *testing.T) {
	d, err := parseDocDesign("```json\n" + `{"name": " Ipak yo'li ", "primary": "#7A2E1D", "accent": "#C08A2E", "heading_font": "Georgia",
		"title": "Frame", "heading": "UNDERLINE", "border": "thinthick", "ornament": "diamond", "image_style": "warm sepia"}` + "\n```")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Ipak yo'li" || d.Title != "frame" || d.Heading != "underline" || d.score() != docDesignScoreComplete {
		t.Errorf("dizayn o'qilmadi: %+v score=%d", d, d.score())
	}
	if d, _ := parseDocDesign(`{"name": "X", "primary": "red", "title": "nope"}`); d == nil || d.score() != 0 {
		t.Errorf("noto'g'ri qiymatlar ball bermasligi kerak: %+v", d)
	}
	if _, err := parseDocDesign(`{"name": "X"}`); err == nil {
		t.Error("dizayn ma'lumotlarisiz javob xato bo'lishi kerak")
	}
}

func TestDocDesignPrompt(t *testing.T) {
	p := buildDocDesignPrompt(DesignRequest{Topic: "Ipak yo'li", Language: "Uzbek (Latin script)", Requirements: "yashil ranglarda"})
	for _, want := range []string{"Ipak yo'li", "Uzbek (Latin script)", `"title"`, "thinthick", "yashil ranglarda", "image_style"} {
		if !strings.Contains(p, want) {
			t.Errorf("promptda %q yo'q", want)
		}
	}
}

func TestReferatPrompts(t *testing.T) {
	req := DocumentRequest{WorkType: WorkReferat, Topic: "Ipak yo'li", Figures: true}
	if !WorkReferat.Valid() || !WorkReferat.IsDocument() || !WorkReferat.HasDesigns() || WorkPractical.HasDesigns() {
		t.Error("referat ish turi xususiyatlari noto'g'ri")
	}
	plan := buildOutlinePrompt(req, 4)
	for _, want := range []string{"referat", "main scientific approaches", `"figure"`, "no text, letters"} {
		if !strings.Contains(plan, want) {
			t.Errorf("reja promptida %q yo'q", want)
		}
	}
	if strings.Contains(buildOutlinePrompt(DocumentRequest{WorkType: WorkReferat, Topic: "X"}, 4), `"figure"`) {
		t.Error("rasm so'ralmasa promptda figure bo'lmasligi kerak")
	}
	section := buildSectionPrompt(req, &outline{Chapters: []outlineChapter{{Title: "A"}}}, section{kind: sectionChapter, number: 1, title: "A", words: 300})
	if !strings.Contains(section, "analytical review") {
		t.Error("referat bo'limi uslubi analitik sharh bo'lishi kerak")
	}
	if s := outlineSchema(true); schemaProp(schemaItems(schemaProp(s, "chapters")), "figure") == nil {
		t.Error("sxemada figure bo'lishi kerak")
	}
}

func TestParseOutlineFigures(t *testing.T) {
	o, err := parseOutline(`{"chapters": [
		{"title": "Birinchi bob", "points": ["a"], "figure": {"caption": "1-rasm. Karvon yo'li.", "prompt": "A caravan in the desert"}},
		{"title": "Ikkinchi bob", "points": ["b"], "figure": {"caption": "Izoh", "prompt": " "}}
	], "references": []}`, 5)
	if err != nil {
		t.Fatal(err)
	}
	if f := o.Chapters[0].Figure; f == nil || f.Prompt != "A caravan in the desert" || strings.HasSuffix(f.Caption, ".") {
		t.Errorf("rasm g'oyasi o'qilmadi: %+v", f)
	}
	if o.Chapters[1].Figure != nil {
		t.Error("tavsifsiz rasm g'oyasi tashlab yuborilishi kerak")
	}
	if n := PlanChapters(2690); n != 5 {
		t.Errorf("PlanChapters(2690) = %d, kutilgan 5", n)
	}
}

// TestDocDesignSpec — AI Word dizayni generator spetsifikatsiyasiga to'liq o'tadi.
func TestDocDesignSpec(t *testing.T) {
	d := &DocDesign{Name: "Ipak yo'li", Primary: "#7A2E1D", Accent: "#C08A2E", HeadingFont: "Georgia",
		Title: "frame", Heading: "underline", Border: "thinthick", Ornament: "diamond", ImageStyle: "warm sepia"}
	want := design.DocSpec{Name: "Ipak yo'li", Primary: "#7A2E1D", Accent: "#C08A2E", HeadingFont: "Georgia",
		Title: "frame", Heading: "underline", Border: "thinthick", Ornament: "diamond"}
	if got := d.Spec(); got != want {
		t.Errorf("spetsifikatsiya: %+v, kutilgan %+v", got, want)
	}
}
