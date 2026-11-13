package generator

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.uber.org/zap"
)

const testTemplate = "../../templates/template.pptx"

func newTestPPTX(t *testing.T) *PPTX {
	t.Helper()
	g, err := NewPPTX(testTemplate, t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("NewPPTX: %v", err)
	}
	return g
}

// readPackage .pptx ichidagi barcha fayllarni o'qiydi.
func readPackage(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	defer zr.Close()
	out := make(map[string][]byte)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		out[f.Name] = data
	}
	return out
}

// checkWellFormed paketdagi barcha XML fayllar to'g'ri tuzilganini tekshiradi.
func checkWellFormed(t *testing.T, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		dec := xml.NewDecoder(bytes.NewReader(data))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s: XML buzilgan: %v", name, err)
			}
		}
	}
}

var sldIDRe = regexp.MustCompile(`<p:sldId id="\d+" r:id="(rId\d+)"/>`)

func checkStructure(t *testing.T, files map[string][]byte, wantSlides int) {
	t.Helper()
	ids := sldIDRe.FindAllStringSubmatch(string(files[presentationPath]), -1)
	if len(ids) != wantSlides {
		t.Fatalf("presentation.xml: %d ta slayd, kutilgan %d", len(ids), wantSlides)
	}
	rels := string(files[presentationRels])
	ct := string(files[contentTypesPath])
	for i, m := range ids {
		if !strings.Contains(rels, `Id="`+m[1]+`"`) {
			t.Errorf("presentation.xml.rels da %s yo'q", m[1])
		}
		slide := "ppt/slides/slide" + itoa(i+1) + ".xml"
		if _, ok := files[slide]; !ok {
			t.Errorf("%s topilmadi", slide)
		}
		if _, ok := files[slideRelsPath(slide)]; !ok {
			t.Errorf("%s topilmadi", slideRelsPath(slide))
		}
		if !strings.Contains(ct, `PartName="/`+slide+`"`) {
			t.Errorf("[Content_Types].xml da %s yo'q", slide)
		}
	}
	if strings.Count(rels, relTypeSlide+`"`) != wantSlides {
		t.Errorf("presentation.xml.rels dagi slayd bog'lanishlari soni noto'g'ri")
	}
	if !strings.Contains(string(files[presentationPath]), `<p:sldSz cx="12192000" cy="6858000"/>`) {
		t.Errorf("slayd o'lchami 16:9 emas")
	}
}

func itoa(n int) string { return strings.TrimLeft(pad2(n), "0") }

func buildAndRead(t *testing.T, g *PPTX, d Deck) map[string][]byte {
	t.Helper()
	path, err := g.Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files := readPackage(t, path)
	if dir := os.Getenv("PREVIEW_DIR"); dir != "" {
		// Ko'z bilan tekshirish uchun: PREVIEW_DIR=/tmp/x go test ./internal/generator
		data, _ := os.ReadFile(path)
		_ = os.WriteFile(filepath.Join(dir, t.Name()[strings.LastIndex(t.Name(), "/")+1:]+".pptx"), data, 0o644)
	}
	return files
}

func TestBuildAllThemes(t *testing.T) {
	g := newTestPPTX(t)
	for _, info := range Themes() {
		t.Run(info.ID, func(t *testing.T) {
			d := SampleDeck(info.ID)
			files := buildAndRead(t, g, d)
			checkWellFormed(t, files)
			checkStructure(t, files, len(d.Slides)+extraSlides)

			th := string(files["ppt/theme/theme1.xml"])
			theme := themeByID(info.ID)
			if !strings.Contains(th, `<a:clrScheme name="`+info.Name+`">`) {
				t.Errorf("theme1.xml rang sxemasi mavzuga moslanmagan")
			}
			if !strings.Contains(th, `<a:latin typeface="`+theme.headFont+`"/>`) {
				t.Errorf("theme1.xml sarlavha shrifti mavzuga moslanmagan")
			}
			cover := string(files["ppt/slides/slide1.xml"])
			for _, want := range []string{"Sun&apos;iy intellektning", "Aliyev Vali", "Karimova Dilnoza", "2026"} {
				if !strings.Contains(cover, want) {
					t.Errorf("titul slaydda %q yo'q", want)
				}
			}
		})
	}
}

func TestUnknownThemeFallsBack(t *testing.T) {
	g := newTestPPTX(t)
	files := buildAndRead(t, g, SampleDeck("mavjud-emas"))
	if !strings.Contains(string(files["ppt/theme/theme1.xml"]), `name="`+themeByID(DefaultTheme).Name+`"`) {
		t.Errorf("noma'lum mavzu standart mavzuga almashmadi")
	}
}

// TestStressDeck chekka holatlar: juda uzun matn, maxsus belgilar, kirill
// alifbosi, ma'lumot yetishmaydigan maketlar va noma'lum maket.
func TestStressDeck(t *testing.T) {
	long := strings.Repeat("Juda uzun va batafsil izoh matni bo'lib, u kartochkaga sig'ishi uchun kichraytirilishi kerak. ", 6)
	d := SampleDeck(ThemeModern)
	d.Topic = `Kompyuter tarmoqlari & <xavfsizlik> "asoslari" — ` + strings.Repeat("juda uzun mavzu nomi ", 8)
	d.Subtitle = "Подзаголовок на кириллице"
	d.Slides = []Slide{
		{Layout: LayoutAgenda, Title: "Reja", Items: []Item{{Heading: "A"}, {Heading: "B"}, {Heading: "C"}, {Heading: "D"}, {Heading: "E"}, {Heading: "F"}, {Heading: "G"}, {Heading: "H"}, {Heading: "I"}}},
		{Layout: LayoutCards, Title: "Olti kartochka", Lead: long, Items: []Item{{Heading: "Bir", Text: long}, {Heading: "Ikki", Text: "Qisqa"}, {Heading: "Uch", Text: long}, {Heading: "To'rt", Text: "x"}, {Heading: "Besh", Text: long}, {Heading: "Olti", Text: long}}},
		{Layout: LayoutProcess, Title: "Olti bosqich", Items: []Item{{Heading: "1", Text: long}, {Heading: "2"}, {Heading: "3"}, {Heading: "4"}, {Heading: "5"}, {Heading: "6", Text: long}}},
		{Layout: LayoutFacts, Title: "Qiymatsiz faktlar", Items: []Item{{Heading: "Bir", Text: "a"}, {Heading: "Ikki", Text: "b"}}},
		{Layout: LayoutFacts, Title: "Ikki fakt", Highlight: long, Items: []Item{{Value: "100%", Heading: "Bir"}, {Value: "1991", Heading: "Ikki", Text: long}}},
		{Layout: LayoutComparison, Title: "Bitta tomon", Items: []Item{{Heading: "Faqat bitta", Points: []string{"a", "b"}}}},
		{Layout: LayoutComparison, Title: "Matnli taqqoslash", Items: []Item{{Heading: "Chap", Text: long}, {Heading: "O'ng", Points: []string{long, long, long}}}},
		{Layout: LayoutDefinition, Title: "Ta'rifsiz", Items: []Item{{Heading: "X", Text: "y"}}},
		{Layout: LayoutDefinition, Title: "Faqat ta'rif", Highlight: long},
		{Layout: "noma'lum", Title: "Noma'lum maket", Items: []Item{{Text: "Sarlavhasiz punkt"}}},
		{Layout: LayoutBullets, Title: strings.Repeat("Juda uzun sarlavha ", 6), Highlight: "Faqat asosiy fikr"},
		{Layout: LayoutConclusion, Title: "Xulosa", Items: []Item{{Heading: "Bir", Text: long}}, Highlight: long},
		{Layout: LayoutConclusion, Title: "Bo'sh xulosa", Highlight: "Faqat yakuniy fikr"},
	}
	g := newTestPPTX(t)
	for _, info := range Themes() {
		t.Run(info.ID, func(t *testing.T) {
			d.Theme = info.ID
			files := buildAndRead(t, g, d)
			checkWellFormed(t, files)
			checkStructure(t, files, len(d.Slides)+extraSlides)
		})
	}
}

func TestNormalizeSlide(t *testing.T) {
	cases := []struct {
		name string
		in   Slide
		want string
	}{
		{"unknown", Slide{Layout: "x", Items: []Item{{Text: "a"}}}, LayoutBullets},
		{"comparison one side", Slide{Layout: LayoutComparison, Items: []Item{{Heading: "a"}}}, LayoutBullets},
		{"facts without values", Slide{Layout: LayoutFacts, Items: []Item{{Heading: "a"}, {Heading: "b"}}}, LayoutCards},
		{"facts too long value", Slide{Layout: LayoutFacts, Items: []Item{{Value: "juda uzun qiymat matni", Heading: "a"}, {Value: "1", Heading: "b"}}}, LayoutCards},
		{"facts ok", Slide{Layout: LayoutFacts, Items: []Item{{Value: "1991", Heading: "a"}, {Value: "75%", Heading: "b"}}}, LayoutFacts},
		{"definition without highlight", Slide{Layout: LayoutDefinition, Items: []Item{{Heading: "a"}}}, LayoutBullets},
		{"only highlight", Slide{Layout: LayoutBullets, Highlight: "h"}, LayoutDefinition},
		{"single card", Slide{Layout: LayoutCards, Items: []Item{{Heading: "a"}}}, LayoutBullets},
		{"empty items dropped", Slide{Layout: LayoutProcess, Items: []Item{{}, {Heading: "a"}, {}}}, LayoutBullets},
	}
	for _, tc := range cases {
		if got := normalizeSlide(tc.in).Layout; got != tc.want {
			t.Errorf("%s: layout = %q, kutilgan %q", tc.name, got, tc.want)
		}
	}
}

func TestFitScale(t *testing.T) {
	short := []para{{runs: []run{{text: "Qisqa matn", size: 14, font: "Calibri"}}}}
	if k := fitScale(short, rect{0, 0, 4, 2}, defaultInset, 0.5, 1.3); k < 1.25 {
		t.Errorf("qisqa matn kattalashmadi: %.2f", k)
	}
	long := []para{{runs: []run{{text: strings.Repeat("uzun matn ", 200), size: 14, font: "Calibri"}}}}
	if k := fitScale(long, rect{0, 0, 4, 2}, defaultInset, 0.5, 1.3); k > 0.6 {
		t.Errorf("uzun matn kichraymadi: %.2f", k)
	}
	word := []para{{runs: []run{{text: "Moslashuvchanlikning", size: 22, bold: true, font: "Century Gothic"}}}}
	if k := fitScale(word, rect{0, 0, 2, 3}, defaultInset, 0.4, 1.3); measureText(word, 1.8, k) == 0 || k >= 1 {
		t.Errorf("uzun so'z tor ustunga sig'ishi uchun kichraymadi: %.2f", k)
	}
}

func TestContentSlides(t *testing.T) {
	g := newTestPPTX(t)
	for total, want := range map[int]int{5: 3, 3: 1, 2: 1, 25: 23} {
		if got := g.ContentSlides(total); got != want {
			t.Errorf("ContentSlides(%d) = %d, kutilgan %d", total, got, want)
		}
	}
}
