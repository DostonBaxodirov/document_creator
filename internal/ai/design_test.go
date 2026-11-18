package ai

import (
	"slices"
	"strings"
	"testing"
)

func TestParseDesign(t *testing.T) {
	raw := "```json\n" + `{"name": " **Chuqur okean** ", "mood": "calm, scientific",
		"colors": {"background": "#F3F7FA", "surface": "#FFF", "primary": "#0B4F6C", "secondary": "#01A7C2",
		"accent": "#F4A259", "text": "#102A43", "muted": "#52667A"},
		"heading_font": "Georgia", "body_font": "Calibri", "cover": "Split", "header": "BAR", "cards": "shadow",
		"corners": "rounded", "markers": "circle", "motif": "waves", "photo_side": "right",
		"image_style": "clean documentary photography", "signature_layouts": ["history", "hub", "agenda", "hub"]}` + "\n```"
	d, err := parseDesign(raw)
	if err != nil {
		t.Fatalf("parseDesign: %v", err)
	}
	if d.Name != "Chuqur okean" || d.Cover != "split" || d.Header != "bar" {
		t.Errorf("tozalanmagan: name=%q cover=%q header=%q", d.Name, d.Cover, d.Header)
	}
	// "history" — timeline taxallusi; "agenda" imzo maketi bo'la olmaydi; takror tashlanadi.
	if want := []string{LayoutTimeline, LayoutHub}; !slices.Equal(d.SignatureLayouts, want) {
		t.Errorf("imzo maketlari %v, kutilgan %v", d.SignatureLayouts, want)
	}
	if d.validColors() != 7 || d.score() != designScoreComplete {
		t.Errorf("ranglar %d, ball %d", d.validColors(), d.score())
	}
	if b := d.Brief(); b.Name != d.Name || b.ImageStyle != d.ImageStyle {
		t.Errorf("Brief: %+v", b)
	}

	// Yarim to'liq javob — ball past (qayta urinish uchun), lekin xato emas.
	d, err = parseDesign(`{"colors": {"background": "white", "primary": "#123456"}, "cover": "full"}`)
	if err != nil || d.score() >= designScoreComplete {
		t.Errorf("yarim javob: err=%v score=%d", err, d.score())
	}
	if _, err := parseDesign(`{"foo": 1}`); err == nil {
		t.Error("dizaynsiz javob xato berishi kerak")
	}
	if _, err := parseDesign(`not json`); err == nil {
		t.Error("JSON bo'lmagan javob xato berishi kerak")
	}
}

func TestDesignPrompt(t *testing.T) {
	p := buildDesignPrompt(DesignRequest{Topic: "Amir Temur davlati", Language: "Uzbek (Latin script)", Requirements: `ko'k ranglarda """ bo'lsin`})
	for _, want := range []string{`Topic: "Amir Temur davlati"`, "Presentation language: Uzbek", "JSON shape:", `"image_style"`,
		"ko'k ranglarda", `"split"`, `"underline"`, `"waves"`, "Century Gothic", `"statement"`} {
		if !strings.Contains(p, want) {
			t.Errorf("dizayn promptida %q yo'q", want)
		}
	}
	if strings.Count(p, `"""`) != 2 {
		t.Errorf("talablar bloki buzilgan:\n%s", p)
	}
	if !strings.Contains(designSystemPrompt, "JSON") {
		t.Error("tizim promptida JSON so'zi bo'lishi shart (OpenAI JSON rejimi)")
	}
	if p := buildDesignPrompt(DesignRequest{Topic: "X"}); strings.Contains(p, "requirements") || strings.Contains(p, "Presentation language") {
		t.Errorf("bo'sh til va talablarda qo'shimcha qatorlar bo'lmasligi kerak:\n%s", p)
	}

	// Sxema: barcha maydonlar majburiy, variantlar ro'yxati bilan.
	s := designSchema()
	if len(s.Required) != len(s.Properties) {
		t.Errorf("sxemada %d ta maydon, %d tasi majburiy", len(s.Properties), len(s.Required))
	}
	if !slices.Equal(s.Properties["cover"].Enum, DesignCovers) || !slices.Equal(s.Properties["heading_font"].Enum, DesignHeadingFonts) {
		t.Error("sxema variantlari ro'yxatlar bilan mos emas")
	}
}

func TestCustomDeckPrompts(t *testing.T) {
	brief := &DesignBrief{Name: "Chuqur okean", Mood: "calm", ImageStyle: "cool blue documentary photography."}
	req := PresentationRequest{Topic: "Okeanlar", SlideCount: 10, Images: true, Custom: brief,
		Layouts: slideLayouts, Signature: []string{LayoutHub, LayoutTimeline}}
	plan := &deckOutline{Slides: []plannedSlide{{Title: "Reja", Layout: "agenda", Focus: "f"}, {Title: "B", Layout: "hub", Focus: "g"}}}
	prompts := map[string]string{
		"deck":    buildDeckPrompt(req),
		"outline": buildDeckOutlinePrompt(req),
		"batch":   buildSlidesBatchPrompt(req, plan, 0, 2),
	}
	for name, p := range prompts {
		if name != "batch" {
			for _, want := range []string{"custom visual design", `"Chuqur okean": calm`, `Use "bullets" on at most 2 slide(s)`, `exactly one "statement" slide`} {
				if !strings.Contains(p, want) {
					t.Errorf("%s: %q yo'q:\n%s", name, want, p)
				}
			}
			if strings.Contains(p, "use at least 4 different layouts") {
				t.Errorf("%s: oddiy dizayn qoidasi maxsus dizaynda qolmasligi kerak", name)
			}
		}
		if name != "outline" && !strings.Contains(p, "(cool blue documentary photography)") {
			t.Errorf("%s: rasm uslubi qoidasi yo'q:\n%s", name, p)
		}
	}
	// Oddiy dizaynda maxsus qoidalar yo'q.
	req.Custom = nil
	if p := buildDeckPrompt(req); strings.Contains(p, "custom visual design") || strings.Contains(p, "art direction") {
		t.Errorf("oddiy dizaynda maxsus qoidalar bo'lmasligi kerak")
	}
	// Kichik taqdimotda "statement" va bullets cheklovi talab qilinmaydi.
	if r := customDeckRules(4, brief); strings.Contains(r, "statement") || strings.Contains(r, "at most") {
		t.Errorf("4 slayd: %s", r)
	}
	// Kichik taqdimot: imzo maketlari talabi qoida va tekshiruvda bir xil; tekshiruv ro'yxati bitta.
	small := PresentationRequest{Topic: "T", SlideCount: 5, Images: true, Custom: brief, Layouts: slideLayouts,
		Signature: []string{LayoutTimeline, LayoutPyramid, LayoutStatement}}
	p := buildDeckPrompt(small)
	if !strings.Contains(p, "Use at least 1 of them") || !strings.Contains(p, "at least 1 of the signature layouts") {
		t.Errorf("imzo maketlari talabi mos emas:\n%s", p)
	}
	if strings.Count(p, "Before answering") != 1 {
		t.Errorf("tekshiruv ro'yxati bitta bo'lishi kerak:\n%s", p)
	}
}

func TestCustomChecks(t *testing.T) {
	brief := &DesignBrief{Name: "X"}
	req := PresentationRequest{Topic: "T", SlideCount: 10, Custom: brief, Layouts: slideLayouts, Signature: []string{LayoutHub, LayoutTimeline}}
	plan := &deckOutline{Slides: []plannedSlide{{Title: "A", Layout: "agenda"}, {Title: "B", Layout: "hub"}}}
	outline := buildDeckOutlinePrompt(req)
	if !strings.Contains(outline, `(1) there are exactly 10 slides; (2) each signature layout ("hub", "timeline")`) ||
		!strings.Contains(outline, `exactly one "statement" slide`) {
		t.Errorf("reja tekshiruvi yo'q:\n%s", outline)
	}
	for name, p := range map[string]string{"deck": buildDeckPrompt(req), "batch": buildSlidesBatchPrompt(req, plan, 0, 2)} {
		if !strings.Contains(p, "never estimate percentages") || !strings.Contains(p, `"matrix" exactly 4`) {
			t.Errorf("%s: mazmun tekshiruvi yo'q:\n%s", name, p)
		}
	}
	req.Requirements = "talab"
	if p := buildSlidesBatchPrompt(req, plan, 0, 2); !strings.HasSuffix(p, "\"\"\"") {
		t.Error("talablar bloki promptning oxirida qolishi kerak")
	}
	req.Custom = nil
	if p := buildDeckOutlinePrompt(req) + buildDeckPrompt(req); strings.Contains(p, "Before answering") {
		t.Error("oddiy dizaynda tekshiruv ro'yxati bo'lmasligi kerak")
	}
}
