package generator

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestCustomDesignsBuild — barcha namunaviy maxsus dizaynlar (har bir titul,
// sarlavha, kartochka va motiv varianti) to'g'ri .pptx bo'lib yig'iladi;
// rasmlar titulda va tanlangan slaydlarda bor.
func TestCustomDesignsBuild(t *testing.T) {
	t.Parallel()
	g := newTestPPTX(t)
	specs := SampleCustomSpecs()
	// Har bir variantni kamida bir marta sinash uchun qo'shimcha kombinatsiyalar.
	for i, cover := range CustomCovers {
		for j, header := range CustomHeaders {
			sp := specs[(i+j)%len(specs)]
			sp.Cover, sp.Header = cover, header
			sp.Cards = CustomCards[(i*3+j)%len(CustomCards)]
			sp.Motif = CustomMotifs[(i+j*2)%len(CustomMotifs)]
			sp.Markers = CustomMarkers[(i+j)%len(CustomMarkers)]
			sp.Corners = CustomCorners[(i+j)%len(CustomCorners)]
			sp.PhotoSide = CustomSides[j%len(CustomSides)]
			sp.Name = fmt.Sprintf("%s-%s", cover, header)
			specs = append(specs, sp)
		}
	}
	for _, sp := range specs {
		t.Run(sp.Name, func(t *testing.T) {
			t.Parallel()
			d := SampleCustomDeck(sp)
			files := buildAndRead(t, g, d)
			checkWellFormed(t, files)
			checkStructure(t, files, len(d.Slides)+extraSlides)
			if th := string(files["ppt/theme/theme1.xml"]); !strings.Contains(th, `name="`+xmlEscape(sp.Name)+`"`) {
				t.Errorf("rang sxemasi nomi maxsus dizayn nomiga moslanmagan")
			}
			if n, _ := checkPictures(t, files, "ppt/slides/slide1.xml"); n == 0 {
				t.Errorf("titulda rasm yo'q")
			}
			withPhotos := 0
			for i := range d.Slides {
				if n, _ := checkPictures(t, files, fmt.Sprintf("ppt/slides/slide%d.xml", i+2)); n > 0 {
					withPhotos++
				}
			}
			if want := len(d.Slides) * 2 / 5; withPhotos != want {
				t.Errorf("%d ta slaydda rasm, kutilgan %d", withPhotos, want)
			}
		})
	}
}

// TestCustomWithoutPictures — rasm chizilmasa ham maxsus dizayn yig'iladi.
func TestCustomWithoutPictures(t *testing.T) {
	g := newTestPPTX(t)
	for _, sp := range SampleCustomSpecs() {
		d := SampleCustomDeck(sp)
		d.CoverImage = nil
		for i := range d.Slides {
			d.Slides[i].Image = nil
		}
		files := buildAndRead(t, g, d)
		checkWellFormed(t, files)
	}
	// Spetsifikatsiyasiz (Theme == ThemeCustom, Custom == nil) — standart qiymatlar bilan.
	d := SampleDeck(DefaultTheme)
	d.Theme = ThemeCustom
	checkWellFormed(t, buildAndRead(t, g, d))
}

// TestCustomPaletteContrast — AI noto'g'ri (kontrasti past) ranglar bersa,
// generator ularni o'qiladigan qilib tuzatadi.
func TestCustomPaletteContrast(t *testing.T) {
	cases := []CustomSpec{
		// Oq fonda deyarli oq matn va sariq asosiy rang.
		{Background: "#FFFFFF", Surface: "#FFFFFF", Text: "#EEEEEE", Muted: "#F5F5F5", Primary: "#FFFF00", Secondary: "#FFF7AA", Accent: "#FAFAFA"},
		// To'q fonda to'q matn va to'q ranglar.
		{Background: "#0A0A0A", Surface: "#F0F0F0", Text: "#111111", Muted: "#222222", Primary: "#101040", Secondary: "#202020", Accent: "#300000"},
		// Noto'g'ri formatlar — standart qiymatlar.
		{Background: "blue", Surface: "#12", Text: "rgb(0,0,0)", Primary: "", Accent: "#GGGGGG"},
		// O'rtacha kulrang fon.
		{Background: "#808080", Text: "#888888", Primary: "#7F7F7F"},
	}
	for i, sp := range cases {
		pal, cc, dark := customPalette(sp.normalized())
		check := func(name, fg, bg string, min float64) {
			if c := contrast(fg, bg); c < min-0.01 {
				t.Errorf("holat %d: %s kontrasti %.2f < %.1f (%s / %s)", i, name, c, min, fg, bg)
			}
		}
		check("matn/fon", pal.text, pal.bg, 7)
		check("matn/kartochka", pal.text, pal.surface, 7)
		check("ikkinchi matn/fon", pal.muted, pal.bg, 4.5)
		check("asosiy rang/fon", pal.primary, pal.bg, 3)
		check("panel/oq matn", cc.panel, "FFFFFF", 4.5)
		check("qoplama/oq matn", cc.shade, "FFFFFF", 7)
		for k, s := range pal.series {
			check(fmt.Sprintf("series[%d]/yozuv", k), s, pal.onSeries, 3)
		}
		if dark != darkBg(pal.bg) {
			t.Errorf("holat %d: dark=%v, fon %s", i, dark, pal.bg)
		}
		for _, c := range append([]string{pal.bg, pal.surface, pal.text, pal.muted, pal.primary, pal.accent, pal.line}, pal.series...) {
			if _, ok := parseHex(c); !ok || len(c) != 6 {
				t.Errorf("holat %d: noto'g'ri rang %q", i, c)
			}
		}
	}
}

// TestCustomKeepsGoodColors — yaxshi palitra o'zgarishsiz qoladi (AI tanlovi hurmat qilinadi).
func TestCustomKeepsGoodColors(t *testing.T) {
	sp := SampleCustomSpecs()[0]
	pal, _, dark := customPalette(sp.normalized())
	if dark {
		t.Fatal("och fon to'q deb topildi")
	}
	for name, pair := range map[string][2]string{
		"bg": {pal.bg, "F3F7FA"}, "text": {pal.text, "102A43"}, "primary": {pal.primary, "0B4F6C"}, "surface": {pal.surface, "FFFFFF"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s o'zgartirildi: %s, kutilgan %s", name, pair[0], pair[1])
		}
	}
}

func TestCustomNormalized(t *testing.T) {
	sp := (&CustomSpec{
		Cover: "SPLIT ", Header: "zigzag", HeadingFont: "georgia", BodyFont: "Comic Sans MS",
		Signature: []string{"timeline", "agenda", "TIMELINE", "matrix", "hub", "cycle"},
	}).normalized()
	if sp.Cover != CoverSplit || sp.Header != HeaderBar {
		t.Errorf("variantlar: cover=%q header=%q", sp.Cover, sp.Header)
	}
	if sp.HeadingFont != "Georgia" || sp.BodyFont != "Calibri" {
		t.Errorf("shriftlar: %q / %q", sp.HeadingFont, sp.BodyFont)
	}
	if want := []string{LayoutTimeline, LayoutMatrix, LayoutHub}; !slices.Equal(sp.Signature, want) {
		t.Errorf("imzo maketlari %v, kutilgan %v", sp.Signature, want)
	}
	if def := (*CustomSpec)(nil).normalized(); def.Name == "" || def.Cover == "" || def.Motif == "" {
		t.Errorf("nil spetsifikatsiya standart qiymatlarsiz: %+v", def)
	}
	// Barcha shriftlar uchun kenglik o'lchangan.
	for _, f := range append(slices.Clone(CustomHeadingFonts), CustomBodyFonts...) {
		if _, ok := fontWidths[f]; !ok {
			t.Errorf("%s shrifti kengligi fontWidths da yo'q", f)
		}
	}
}

func TestCustomDesign(t *testing.T) {
	sp := SampleCustomSpecs()[1]
	d := DesignFor("", &sp)
	if !d.Custom() || !d.Photos() || d.Name() != sp.Name {
		t.Fatalf("maxsus dizayn: custom=%v photos=%v name=%q", d.Custom(), d.Photos(), d.Name())
	}
	st := d.Structure()
	for _, l := range append(slices.Clone(coreLayouts), customSignatureLayouts...) {
		if !slices.Contains(st.Layouts, l) {
			t.Errorf("maxsus dizayn tuzilishida %q maketi yo'q", l)
		}
	}
	if !slices.Equal(st.Signature, sp.Signature) {
		t.Errorf("imzo maketlari %v, kutilgan %v", st.Signature, sp.Signature)
	}
	p := d.PhotoPrompt("A robot in a lab.")
	if !strings.HasPrefix(p, "A robot in a lab. Style: "+sp.PhotoStyle) || !strings.Contains(p, "no text") {
		t.Errorf("rasm prompti: %q", p)
	}
	if !DesignFor(ThemeCustom, nil).Custom() {
		t.Error("ThemeCustom ID si maxsus dizayn bo'lishi kerak")
	}
	if DesignFor(ThemeAcademic, nil).Custom() || DesignFor("yoq", nil).Photos() {
		t.Error("tayyor dizayn maxsus yoki noma'lum dizayn rasmli bo'lib qoldi")
	}
	if _, ok := FindTheme(ThemeCustom); ok {
		t.Error("maxsus dizayn tayyor dizaynlar ro'yxatida bo'lmasligi kerak")
	}
}

func TestPhotoSlides(t *testing.T) {
	d := DesignFor(ThemeCustom, nil)
	deck := SampleCustomDeck(SampleCustomSpecs()[0])
	slides := deck.Slides
	for n := 0; n <= len(slides)+2; n++ {
		got := d.PhotoSlides(slides, n)
		want := min(n, len(slides))
		if len(got) != want {
			t.Fatalf("n=%d: %d ta slayd, kutilgan %d", n, len(got), want)
		}
		if !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != len(got) {
			t.Errorf("n=%d: tartiblanmagan yoki takroriy: %v", n, got)
		}
		// Yon panelga sig'adigan slaydlar ustuvor.
		fits := 0
		for _, i := range slides {
			if photoFits(normalizeSlide(i)) {
				fits++
			}
		}
		if n <= fits {
			for _, i := range got {
				if !photoFits(normalizeSlide(slides[i])) {
					t.Errorf("n=%d: %d-slayd yon panelga sig'maydi, sig'adiganlari yetarli edi", n, i)
				}
			}
		}
	}
	if DesignFor(ThemeAcademic, nil).PhotoSlides(slides, 3) != nil {
		t.Error("rasmsiz dizaynda rasmli slaydlar tanlanmasligi kerak")
	}
}

func TestParseHex(t *testing.T) {
	for in, want := range map[string]string{"#1a2B3c": "1A2B3C", "abc": "AABBCC", " #FFF ": "FFFFFF"} {
		if got, ok := parseHex(in); !ok || got != want {
			t.Errorf("parseHex(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "#12", "#1234567", "red", "#GGGGGG"} {
		if _, ok := parseHex(in); ok {
			t.Errorf("parseHex(%q) xato berishi kerak", in)
		}
	}
	if c := contrast("000000", "FFFFFF"); c < 20.9 || c > 21.1 {
		t.Errorf("qora/oq kontrasti %.2f", c)
	}
}
