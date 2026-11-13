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
	if got := p.Slides[1].Layout; got != LayoutProcess {
		t.Errorf("timeline -> %q, kutilgan process", got)
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
		r := structureRules(n)
		if n >= 3 && (!strings.Contains(r, `"agenda"`) || !strings.Contains(r, `"conclusion"`)) {
			t.Errorf("n=%d: reja yoki xulosa talabi yo'q", n)
		}
		if n >= 6 && !strings.Contains(r, "at least 4 different layouts") {
			t.Errorf("n=%d: maketlar xilma-xilligi talabi yo'q", n)
		}
	}
	if p := buildSlidesBatchPrompt("Mavzu", &deckOutline{Slides: []plannedSlide{{Title: "Reja", Layout: "agenda", Focus: "f"}, {Title: "B", Layout: "cards", Focus: "g"}}}, 1, 2); !strings.Contains(p, "slides 2-2") || !strings.Contains(p, "1. [agenda] Reja") {
		t.Errorf("batch prompt: %s", p)
	}
}
