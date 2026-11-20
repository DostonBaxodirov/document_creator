package ai

import (
	"fmt"
	"strings"
	"testing"
)

func TestParsePresentation(t *testing.T) {
	raw := "```json\n" + `{
	  "title": "**Sun'iy intellekt**",
	  "subtitle": "Imkoniyatlar va xavflar.",
	  "slides": [
	    {"layout": "agenda", "title": "Reja", "lead": "", "items": [
	      {"heading": "1. Tushuncha", "text": "Ta'riflar"},
	      {"heading": "", "text": ""}
	    ]},
	    {"layout": "Timeline", "title": "Tarix", "lead": "Kirish gapi.", "items": [
	      {"heading": "1956-yil: Dartmut", "text": "Atama paydo bo'ldi."}
	    ]},
	    {"layout": "comparison", "title": "Taqqoslash", "lead": "L", "items": [
	      {"heading": "Afzalliklar", "text": "", "points": ["- Tezlik", "", "Aniqlik"]},
	      {"heading": "Kamchiliklar", "text": "", "points": ["Narx"]}
	    ], "highlight": "Muvozanat muhim."},
	    {"layout": "bullets", "title": "Eski format", "bullets": ["• Birinchi punkt", "Ikkinchi punkt"]},
	    {"layout": "cards", "title": "", "items": [{"heading": "Sarlavhasiz slayd", "text": "tashlanadi"}]},
	    {"layout": "definition", "title": "Ta'rif", "lead": "L", "items": [], "highlight": "Aniq ta'rif."}
	  ]
	}` + "\n```"

	p, err := parsePresentation(raw)
	if err != nil {
		t.Fatalf("parsePresentation: %v", err)
	}
	if p.Title != "Sun'iy intellekt" {
		t.Errorf("title = %q", p.Title)
	}
	if p.Subtitle != "Imkoniyatlar va xavflar" {
		t.Errorf("subtitle = %q", p.Subtitle)
	}
	if len(p.Slides) != 5 {
		t.Fatalf("slaydlar soni = %d, kutilgan 5 (sarlavhasiz slayd tashlanadi)", len(p.Slides))
	}
	if s := p.Slides[0]; len(s.Items) != 1 || s.Items[0].Heading != "Tushuncha" {
		t.Errorf("agenda items = %+v", s.Items)
	}
	if got := p.Slides[1].Layout; got != LayoutTimeline {
		t.Errorf("Timeline -> %q, kutilgan timeline", got)
	}
	if got := p.Slides[1].Items[0].Heading; got != "1956-yil: Dartmut" {
		t.Errorf("sana bilan boshlangan sarlavha buzildi: %q", got)
	}
	if pts := p.Slides[2].Items[0].Points; strings.Join(pts, "|") != "Tezlik|Aniqlik" {
		t.Errorf("points = %q", pts)
	}
	if s := p.Slides[3]; len(s.Items) != 2 || s.Items[0].Text != "Birinchi punkt" || s.Bullets != nil {
		t.Errorf("eski format items = %+v", s.Items)
	}
	if s := p.Slides[4]; s.Layout != LayoutDefinition || s.Highlight == "" {
		t.Errorf("faqat ta'rifli slayd = %+v", s)
	}
}

func TestParseSlidesArray(t *testing.T) {
	// Ba'zi modellar obyekt o'rniga to'g'ridan-to'g'ri massiv qaytaradi.
	s, err := parseSlides(`[{"layout":"cards","title":"A","lead":"","items":[{"heading":"h","text":"t"}]}]`)
	if err != nil || len(s) != 1 || s[0].Title != "A" {
		t.Fatalf("parseSlides = %+v, %v", s, err)
	}
}

func TestSplitBatches(t *testing.T) {
	cases := map[int]string{
		6:  "[[0 6]]",
		7:  "[[0 3] [3 7]]",
		12: "[[0 6] [6 12]]",
		23: "[[0 5] [5 11] [11 17] [17 23]]",
	}
	for n, want := range cases {
		if got := fmt.Sprint(splitBatches(n, slidesPerRequest)); got != want {
			t.Errorf("splitBatches(%d) = %s, kutilgan %s", n, got, want)
		}
	}
}

func TestFitSlidesKeepsConclusion(t *testing.T) {
	p := &Presentation{Slides: []Slide{{Title: "1"}, {Title: "2"}, {Title: "3"}, {Title: "Xulosa"}}}
	p.fit(3)
	if len(p.Slides) != 3 || p.Slides[2].Title != "Xulosa" {
		t.Errorf("fit = %+v", p.Slides)
	}
}

func TestStructureRules(t *testing.T) {
	for _, n := range []int{1, 2, 3, 8} {
		r := structureRules(n, PresentationRequest{})
		if n >= 3 && (!strings.Contains(r, `"agenda"`) || !strings.Contains(r, `"conclusion"`)) {
			t.Errorf("n=%d: reja yoki xulosa talabi yo'q", n)
		}
		if n >= 6 && !strings.Contains(r, "at least 4 different layouts") {
			t.Errorf("n=%d: maketlar xilma-xilligi talabi yo'q", n)
		}
	}
	if p := buildSlidesBatchPrompt(PresentationRequest{Topic: "Mavzu"}, &deckOutline{Slides: []plannedSlide{{Title: "Reja", Layout: "agenda", Focus: "f"}, {Title: "B", Layout: "cards", Focus: "g"}}}, 1, 2); !strings.Contains(p, "slides 2-2") || !strings.Contains(p, "1. [agenda] Reja") {
		t.Errorf("batch prompt: %s", p)
	}
}

func TestImageDescriptions(t *testing.T) {
	long := strings.Repeat("word ", 100)
	raw := `{"title": "T", "subtitle": "S", "image": "  Students   in a **library**  ",
	  "slides": [{"layout": "bullets", "title": "A", "lead": "L", "items": [{"heading": "h", "text": "t"}], "image": "` + long + `"}]}`
	p, err := parsePresentation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Image != "Students in a library" {
		t.Errorf("image = %q", p.Image)
	}
	if n := len([]rune(p.Slides[0].Image)); n > maxImageRunes+1 {
		t.Errorf("slayd rasm tavsifi qirqilmadi: %d", n)
	}

	plain := buildDeckPrompt(PresentationRequest{Topic: "Mavzu", SlideCount: 5})
	withImages := buildDeckPrompt(PresentationRequest{Topic: "Mavzu", SlideCount: 5, Images: true})
	if strings.Contains(plain, `"image"`) {
		t.Errorf("oddiy dizayn promptida rasm talabi bo'lmasligi kerak")
	}
	if !strings.Contains(withImages, `"image": string`) || !strings.Contains(withImages, "realistic photograph") {
		t.Errorf("rasmli dizayn promptida rasm talabi yo'q:\n%s", withImages)
	}
	outline := buildDeckOutlinePrompt(PresentationRequest{Topic: "Mavzu", SlideCount: 10, Images: true})
	if !strings.Contains(outline, `"subtitle": string, "image": string`) {
		t.Errorf("reja promptida titul rasmi yo'q")
	}
	batch := buildSlidesBatchPrompt(PresentationRequest{Topic: "Mavzu", Images: true}, &deckOutline{Slides: []plannedSlide{{Title: "A", Layout: "cards", Focus: "f"}}}, 0, 1)
	if !strings.Contains(batch, `"highlight": string, "image": string`) {
		t.Errorf("guruh promptida rasm maydoni yo'q")
	}

	if s := deckSchema(true, slideLayouts); schemaProp(s, "image") == nil || schemaProp(slideItem(s), "image") == nil {
		t.Errorf("rasmli sxemada image maydoni yo'q")
	}
	if s := deckSchema(false, slideLayouts); schemaProp(s, "image") != nil || schemaProp(slideItem(s), "image") != nil {
		t.Errorf("oddiy sxemada image maydoni bo'lmasligi kerak")
	}
}

// TestDesignAwarePrompt — prompt tanlangan dizayn tuzilishiga moslanadi.
func TestDesignAwarePrompt(t *testing.T) {
	req := PresentationRequest{
		Topic: "Mavzu", SlideCount: 8,
		Layouts:   []string{LayoutBullets, LayoutCards, LayoutTimeline, LayoutMatrix, "noma'lum"},
		Signature: []string{LayoutMatrix, LayoutTimeline, LayoutHub},
	}
	if got := strings.Join(req.layouts(), ","); got != "agenda,conclusion,bullets,cards,timeline,matrix" {
		t.Errorf("layouts = %s (reja va xulosa qo'shilishi, noma'lumi tashlanishi kerak)", got)
	}
	if got := strings.Join(req.signature(), ","); got != "matrix,timeline" {
		t.Errorf("signature = %s (ruxsat etilmagan hub tashlanishi kerak)", got)
	}

	p := buildDeckPrompt(req)
	for _, want := range []string{`- "timeline":`, `- "matrix":`, `- "agenda":`, `signature layouts: "matrix", "timeline". Use at least 2 of them`} {
		if !strings.Contains(p, want) {
			t.Errorf("promptda %q yo'q:\n%s", want, p)
		}
	}
	for _, unwanted := range []string{`- "hub":`, `- "pyramid":`, `- "facts":`} {
		if strings.Contains(p, unwanted) {
			t.Errorf("promptda ruxsat etilmagan maket bor: %q", unwanted)
		}
	}
	if enum := schemaEnum(schemaProp(slideItem(deckSchema(false, req.layouts())), "layout")); len(enum) != 6 {
		t.Errorf("sxemadagi maketlar = %v", enum)
	}
	if enum := schemaEnum(schemaProp(slideItem(deckOutlineSchema(false, req.layouts())), "layout")); len(enum) != 6 {
		t.Errorf("reja sxemasidagi maketlar = %v", enum)
	}

	// Bo'sh Layouts — barcha maketlar, imzo talabi yo'q.
	all := buildDeckPrompt(PresentationRequest{Topic: "Mavzu", SlideCount: 8})
	if !strings.Contains(all, `- "pyramid":`) || strings.Contains(all, "signature layouts") {
		t.Errorf("standart prompt noto'g'ri:\n%s", all)
	}

	// Ixcham dizayn — so'zlar soni kamayadi.
	compact := layoutGuide([]string{LayoutBullets}, true)
	if !strings.Contains(compact, `(15-26 words)`) || !strings.Contains(compact, "little room for text") {
		t.Errorf("ixcham prompt:\n%s", compact)
	}
	if got := scaleWords("text (6-14 words), points 3-5 points", 0.75); got != "text (5-11 words), points 3-5 points" {
		t.Errorf("scaleWords = %q", got)
	}
}

func TestNewLayoutAliases(t *testing.T) {
	cases := map[string]string{
		"SWOT": LayoutMatrix, "chronology": LayoutTimeline, "hierarchy": LayoutPyramid,
		"loop": LayoutCycle, "mindmap": LayoutHub, "quote": LayoutStatement, "statement": LayoutStatement,
	}
	for in, want := range cases {
		if got := normalizeLayout(in); got != want {
			t.Errorf("normalizeLayout(%q) = %q, kutilgan %q", in, got, want)
		}
	}
}

func TestDropForeignSlides(t *testing.T) {
	plan := &deckOutline{Slides: []plannedSlide{{Title: "Reja"}, {Title: "Qonunchilik"}, {Title: "Harbiy  strategiya"}, {Title: "Xulosa"}}}
	got := dropForeignSlides([]Slide{{Title: "qonunchilik"}, {Title: "Harbiy strategiya"}, {Title: "Xulosa"}, {Title: "Yangi nom"}}, plan, 2, 4)
	var titles []string
	for _, s := range got {
		titles = append(titles, s.Title)
	}
	// "qonunchilik" — oldingi guruh slaydi (tashlanadi); reja sarlavhasidan farq qilgan slayd qoladi.
	if strings.Join(titles, "|") != "Harbiy strategiya|Xulosa|Yangi nom" {
		t.Errorf("natija: %v", titles)
	}
}

func TestConclusionLast(t *testing.T) {
	layouts := func(s []Slide) string {
		var out []string
		for _, x := range s {
			out = append(out, x.Layout)
		}
		return strings.Join(out, " ")
	}
	p := &Presentation{Slides: []Slide{{Layout: "agenda"}, {Layout: "conclusion"}, {Layout: "statement"}}}
	p.fit(3)
	if got := layouts(p.Slides); got != "agenda statement conclusion" {
		t.Errorf("xulosa oxirga ko'chmadi: %s", got)
	}
	o := &deckOutline{Slides: []plannedSlide{{Layout: "agenda"}, {Layout: "conclusion"}, {Layout: "cards"}, {Layout: "conclusion"}}}
	o.fit(4)
	if o.Slides[3].Layout != "conclusion" || o.Slides[1].Layout != "conclusion" {
		t.Errorf("oxiri xulosa bo'lsa, tartib o'zgarmasligi kerak: %+v", o.Slides)
	}
	if !strings.Contains(buildDeckOutlinePrompt(PresentationRequest{Topic: "T", SlideCount: 10}), `"n" from 1 to 10 without gaps: slide 10 is the conclusion`) {
		t.Error("reja promptida raqamlash qoidasi yo'q")
	}
	if s := slideItem(deckOutlineSchema(false, slideLayouts)); schemaProp(s, "n") == nil {
		t.Error("reja sxemasida n maydoni yo'q")
	}
}
