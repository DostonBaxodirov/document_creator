package generator

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// buildDocParts hujjatni yaratib, ichidagi fayllarni qaytaradi.
func buildDocParts(t *testing.T, p Paper) map[string]string {
	t.Helper()
	g, err := NewDOCX(t.TempDir(), "VAZIRLIK", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	path, err := g.Build(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(data)
	}
	return out
}

// wellFormed XML to'g'ri tuzilganini tekshiradi.
func wellFormed(t *testing.T, name, s string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(s))
	for {
		if _, err := d.Token(); err == io.EOF {
			return
		} else if err != nil {
			t.Fatalf("%s: XML buzilgan: %v", name, err)
		}
	}
}

func TestDocThemesBuild(t *testing.T) {
	if len(DocThemes()) < 4 {
		t.Fatalf("kamida 4 ta tayyor dizayn bo'lishi kerak")
	}
	seen := map[string]bool{}
	for _, th := range DocThemes() {
		if seen[th.ID] || !strings.HasPrefix(th.ID, "doc_") || th.Name == "" || th.Emoji == "" || th.Description == "" {
			t.Errorf("dizayn ma'lumotlari noto'g'ri: %+v", th)
		}
		seen[th.ID] = true
		if _, ok := FindTheme(th.ID); ok {
			t.Errorf("%s taqdimot mavzusi ID si bilan to'qnashadi", th.ID)
		}
		parts := buildDocParts(t, SamplePaper(th.ID, nil, false))
		for _, name := range []string{docDocumentPath, docStylesPath, docFooterPath, docDocumentRels, docContentTypes} {
			wellFormed(t, th.ID+"/"+name, parts[name])
		}
		// Asosiy matn har doim OTM talablarida.
		if !strings.Contains(parts[docStylesPath], `w:ascii="Times New Roman"`) || !strings.Contains(parts[docStylesPath], `<w:sz w:val="28"/>`) {
			t.Errorf("%s: asosiy matn Times New Roman 14 bo'lishi kerak", th.ID)
		}
	}
	frame := buildDocParts(t, SamplePaper(DocThemeFrame, nil, false))[docDocumentPath]
	if !strings.Contains(frame, `<w:pgBorders w:offsetFrom="page"><w:top w:val="double"`) {
		t.Error("Ramkali dizaynda qo'sh chiziqli ramka bo'lishi kerak")
	}
	classic := buildDocParts(t, SamplePaper(DocThemeClassic, nil, false))
	if strings.Contains(classic[docDocumentPath], "pgBorders") || strings.Contains(classic[docStylesPath], "<w:color ") {
		t.Error("Klassik dizayn ramkasiz va qora bo'lishi kerak")
	}
	if got := buildDocParts(t, SamplePaper("", nil, false))[docStylesPath]; got != classic[docStylesPath] {
		t.Error("bo'sh dizayn Klassik bo'lishi kerak")
	}
}

func TestDocFigures(t *testing.T) {
	for i, sp := range SampleDocSpecs() {
		parts := buildDocParts(t, SamplePaper("", &sp, true))
		doc := parts[docDocumentPath]
		wellFormed(t, "document.xml", doc)
		wellFormed(t, "rels", parts[docDocumentRels])
		if n := strings.Count(doc, "<wp:inline"); n != 2 {
			t.Errorf("%d: 2 ta rasm kutilgan, bor %d", i, n)
		}
		for _, want := range []string{"1-rasm.", "2-rasm.", "Raqamli iqtisodiyotning asosiy unsurlari", `r:embed="rIdImg1"`, `r:embed="rIdImg2"`} {
			if !strings.Contains(doc, want) {
				t.Errorf("%d: hujjatda %q yo'q", i, want)
			}
		}
		if parts["word/media/image1.jpeg"] == "" || parts["word/media/image2.jpeg"] == "" {
			t.Errorf("%d: rasm fayllari paketda yo'q", i)
		}
		if !strings.Contains(parts[docDocumentRels], `Target="media/image2.jpeg"`) {
			t.Errorf("%d: rasm bog'lanishi yo'q", i)
		}
	}
	// Buzilgan rasm — tashlab yuboriladi, hujjat baribir yaratiladi.
	p := SamplePaper(DocThemeModern, nil, false)
	p.Chapters[1].Figure = &Figure{Data: []byte("not an image"), Caption: "x"}
	if doc := buildDocParts(t, p)[docDocumentPath]; strings.Contains(doc, "<wp:inline") {
		t.Error("o'qib bo'lmaydigan rasm qo'yilmasligi kerak")
	}
}

func TestDocSpecNormalization(t *testing.T) {
	st := docSpecStyle(&DocSpec{Primary: "#FFFF00", Accent: "zzz", HeadingFont: "comic sans", Title: "BAND", Heading: "weird", Border: "double", Ornament: ""})
	if contrast(st.primary, "FFFFFF") < 4.5 {
		t.Errorf("och sariq rang oq qog'ozda o'qiladigan qilib to'qlashtirilishi kerak: %s", st.primary)
	}
	if st.accent != "8C6D1F" || st.headingFont != "" {
		t.Errorf("noto'g'ri rang/shrift standart qiymatga almashishi kerak: %+v", st)
	}
	if st.title != docTitleBand || st.heading != docHeadingCenter || st.border != docBorderDouble || st.ornament != docOrnamentNone {
		t.Errorf("variantlar normallashtirilmagan: %+v", st)
	}
	if got := docSpecStyle(&DocSpec{Primary: "#1F4E79", HeadingFont: "georgia"}); got.primary != "1F4E79" || got.headingFont != "Georgia" {
		t.Errorf("yaroqli qiymatlar saqlanishi kerak: %+v", got)
	}
	if _, ok := FindDocTheme(DocThemeAcademic); !ok {
		t.Errorf("%s Word dizayni topilmadi", DocThemeAcademic)
	}
	if _, ok := FindDocTheme("okean"); ok {
		t.Error("slayd dizayni Word dizayni sifatida topilmasligi kerak")
	}
}
