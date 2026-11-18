package generator

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/lang"
)

// allDocStyles — barcha tayyor dizaynlar va maxsus dizayn namunalari uslublari.
func allDocStyles() map[string]docStyle {
	out := map[string]docStyle{}
	for _, t := range docThemes {
		out[t.ID] = t.style
	}
	for _, sp := range SampleDocSpecs() {
		out["custom:"+sp.Name] = sp.style()
	}
	// Eng "og'ir" kombinatsiya: keng shrift, ramkali blok va bezak.
	out["custom:wide-frame"] = (&DocSpec{HeadingFont: "Century Gothic", Title: docTitleFrame, Ornament: docOrnamentDiamond}).style()
	out["custom:wide-band"] = (&DocSpec{HeadingFont: "Segoe UI", Title: docTitleBand, Ornament: docOrnamentDots}).style()
	return out
}

func titleSample(topic, uni, student, teacher string) titleTexts {
	l := lang.Get("").Labels
	return titleTexts{
		header:     l.Ministry,
		university: uni,
		workType:   l.TitleIndependent,
		topic:      l.Topic + ": " + topic,
		student:    l.Student + ": " + student,
		teacher:    l.Teacher + ": " + teacher,
		year:       "2026-yil",
	}
}

// TestTitleAlwaysFits — botda ruxsat etilgan eng uzun qiymatlarda ham titul
// bitta sahifaga sig'adi (kerak bo'lsa mavzu shrifti kichrayadi), qisqa
// qiymatlarda esa yil sahifa tagiga tushadi va mavzu 16 pt bo'lib qoladi.
func TestTitleAlwaysFits(t *testing.T) {
	short := titleSample("Raqamli iqtisodiyot", "TDIU", "Aliyev Vali", "Karimova D.")
	long := titleSample(
		"O'zbekiston Respublikasida raqamli iqtisodiyotni rivojlantirishning nazariy-metodologik asoslari, xorijiy tajriba va uni milliy iqtisodiyotga tatbiq etishning ustuvor yo'nalishlari hamda istiqbollari",
		"Mirzo Ulug'bek nomidagi O'zbekiston Milliy universitetining Jizzax filiali Iqtisodiyot va axborot texnologiyalari fakulteti",
		"Abdurahmonova Mohinur Shavkatjon qizi, 301-21 guruh talabasi", "i.f.n., dotsent Karimova Dilnoza Abdullayevna")
	// Bot ruxsat bergan chegaralar: mavzu 500, universitet 150, F.I.Sh. 100 belgi.
	maxed := titleSample(strings.Repeat("Raqamlashtirish jarayonlari ", 18)[:500], strings.Repeat("Universitet fakulteti ", 7)[:150],
		strings.Repeat("Abdurahmonova ", 8)[:100], strings.Repeat("Karimova ", 12)[:100])

	const minTotal = titleMinGapWork + titleMinGapSign + titleMinGapYear
	for name, st := range allDocStyles() {
		for kind, tt := range map[string]titleTexts{"short": short, "long": long, "max": maxed} {
			lay := planTitle(tt, st)
			used := measureTitle(tt, st, lay.topicSize) + lay.gapWork + lay.gapSign + lay.gapYear
			if used > docTextHeight-titleSafetyMargin {
				t.Errorf("%s/%s: titul sig'maydi: %d > %d (%+v)", name, kind, used, docTextHeight-titleSafetyMargin, lay)
			}
			if lay.gapWork+lay.gapSign+lay.gapYear < minTotal {
				t.Errorf("%s/%s: oraliqlar juda kichik: %+v", name, kind, lay)
			}
			if kind == "short" {
				if lay.topicSize != titleTopicSizes[0] {
					t.Errorf("%s: qisqa mavzu 16 pt bo'lishi kerak: %+v", name, lay)
				}
				if used != docTextHeight-titleSafetyMargin {
					t.Errorf("%s: qisqa titulda yil sahifa tagida bo'lishi kerak: %d", name, used)
				}
			}
		}
		if lay := planTitle(maxed, st); lay.topicSize == titleTopicSizes[0] {
			t.Errorf("%s: 500 belgili mavzuda shrift kichrayishi kerak: %+v", name, lay)
		}
	}
}

func TestEstimateLines(t *testing.T) {
	if n := estimateLines("", 14, docTextWidth, 1, 0); n != 1 {
		t.Errorf("bo'sh matn — 1 qator, olindi %d", n)
	}
	if n := estimateLines("Raqamli iqtisodiyot", 16, docTextWidth, 1, 0); n != 1 {
		t.Errorf("qisqa mavzu 1 qatorga sig'adi, olindi %d", n)
	}
	text := strings.Repeat("iqtisodiyot ", 40)
	narrow, wide := estimateLines(text, 14, docTextWidth/2, 1, 0), estimateLines(text, 14, docTextWidth, 1, 0)
	if wide < 3 || narrow < 2*wide-1 {
		t.Errorf("tor ustunda qatorlar ko'proq bo'lishi kerak: %d / %d", narrow, wide)
	}
	if n := estimateLines(strings.Repeat("x", 400), 14, docTextWidth, 1, 0); n < 2 {
		t.Errorf("qatorga sig'maydigan so'z bo'linishi kerak, olindi %d", n)
	}
	for _, f := range DocHeadingFonts {
		if _, ok := docFontMetrics[f]; !ok {
			t.Errorf("%s shriftining o'lchamlari yo'q", f)
		}
		if _, ok := docFonts[f]; !ok {
			t.Errorf("%s shrifti shriftlar jadvalida yo'q", f)
		}
	}
}

// TestTitlePageRender — hujjatda tanlangan oraliqlar va mavzu shrifti qo'llanadi.
func TestTitlePageRender(t *testing.T) {
	p := SamplePaper(DocThemeClassic, nil, false)
	p.Topic = strings.Repeat("Raqamlashtirish jarayonlari ", 18)[:500]
	doc := buildDocParts(t, p)[docDocumentPath]
	if !strings.Contains(doc, `<w:sz w:val="28"/><w:szCs w:val="28"/></w:rPr><w:t xml:space="preserve">Mavzu: `) &&
		!strings.Contains(doc, `<w:sz w:val="26"/><w:szCs w:val="26"/></w:rPr><w:t xml:space="preserve">Mavzu: `) &&
		!strings.Contains(doc, `<w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr><w:t xml:space="preserve">Mavzu: `) {
		t.Error("uzun mavzu kichikroq shriftda chiqishi kerak")
	}
	short := buildDocParts(t, SamplePaper(DocThemeClassic, nil, false))[docDocumentPath]
	if !strings.Contains(short, `<w:sz w:val="32"/><w:szCs w:val="32"/></w:rPr><w:t xml:space="preserve">Mavzu: `) {
		t.Error("oddiy mavzu 16 pt bo'lishi kerak")
	}
	if strings.Contains(short, `w:before="3400"`) {
		t.Error("oraliqlar endi qat'iy emas, hisoblanadi")
	}
}

// TestBandTextShading — oq matnli lentalarda matnning o'ziga ham fon beriladi
// (paragraf fonini chizmaydigan Quick Look'da matn ko'rinsin).
func TestBandTextShading(t *testing.T) {
	modern := buildDocParts(t, SamplePaper(DocThemeModern, nil, false))[docDocumentPath]
	if !strings.Contains(modern, `<w:szCs w:val="44"/><w:shd w:val="clear" w:color="auto" w:fill="1F4E79"/></w:rPr>`) {
		t.Error("Zamonaviy titulidagi ish turi matnida lenta foni yo'q")
	}
	spec := SampleDocSpecs()[1] // lentali sarlavhalar
	styles := buildDocParts(t, SamplePaper("", &spec, false))[docStylesPath]
	if !strings.Contains(styles, `<w:color w:val="FFFFFF"/><w:shd w:val="clear" w:color="auto" w:fill="123B6D"/></w:rPr>`) {
		t.Error("lentali sarlavha uslubida matn foni yo'q")
	}
	styles = buildDocParts(t, SamplePaper(DocThemeClassic, nil, false))[docStylesPath]
	heading := styles[strings.Index(styles, `w:styleId="Heading1"`):]
	if heading = heading[:strings.Index(heading, "</w:style>")]; strings.Contains(heading, "<w:shd") {
		t.Error("lentasiz dizaynda sarlavhaga fon berilmasligi kerak")
	}
}

// TestTitleHeaderLanguage — standart vazirlik nomi hujjat tilida chiqadi,
// qo'lda yozilgan nom esa o'zgarishsiz.
func TestTitleHeaderLanguage(t *testing.T) {
	build := func(header, code string) string {
		g, err := NewDOCX(t.TempDir(), header, zap.NewNop())
		if err != nil {
			t.Fatal(err)
		}
		p := SamplePaper(DocThemeClassic, nil, false)
		p.Lang = code
		return g.renderDocument(p, docStyleFor(p.Design, nil), make([]*docImage, len(p.Chapters)))
	}
	def := lang.Get("").Labels.Ministry
	for _, l := range lang.All() {
		doc := build(def, string(l.Code))
		if !strings.Contains(doc, xmlEscape(l.Labels.Ministry)) {
			t.Errorf("%s: vazirlik nomi shu tilda bo'lishi kerak", l.Code)
		}
		if l.Code != lang.UzLatn && strings.Contains(doc, xmlEscape(def)) {
			t.Errorf("%s: o'zbekcha vazirlik nomi qolib ketgan", l.Code)
		}
	}
	// Apostrof boshqacha yozilgan standart nom ham taniladi.
	if doc := build(strings.ReplaceAll(def, "'", "’"), string(lang.Ru)); !strings.Contains(doc, "МИНИСТЕРСТВО") {
		t.Error("apostrofi boshqacha standart nom tarjima qilinishi kerak")
	}
	if doc := build("SAMARQAND DAVLAT UNIVERSITETI", string(lang.Ru)); !strings.Contains(doc, "SAMARQAND DAVLAT UNIVERSITETI") {
		t.Error("qo'lda yozilgan nom o'zgarmasligi kerak")
	}
	if doc := build("", string(lang.Ru)); strings.Contains(doc, "МИНИСТЕРСТВО") {
		t.Error("bo'sh sozlamada vazirlik qatori chiqmasligi kerak")
	}
}

func TestDocPackageExtras(t *testing.T) {
	spec := SampleDocSpecs()[2] // Century Gothic
	parts := buildDocParts(t, SamplePaper("", &spec, false))
	fonts := parts[docFontTablePath]
	wellFormed(t, "fontTable.xml", fonts)
	for _, want := range []string{`w:name="Times New Roman"`, `w:name="Courier New"`, `w:name="Century Gothic"`, `<w:family w:val="swiss"/>`} {
		if !strings.Contains(fonts, want) {
			t.Errorf("shriftlar jadvalida %q yo'q", want)
		}
	}
	if !strings.Contains(parts[docDocumentRels], `Target="fontTable.xml"`) || !strings.Contains(parts[docContentTypes], `/word/fontTable.xml`) {
		t.Error("shriftlar jadvali paketga ulanmagan")
	}
	if !strings.Contains(parts[docStylesPath], `<w:ind w:left="567" w:hanging="567"/><w:jc w:val="left"/>`) {
		t.Error("adabiyotlar chapga tekislanishi kerak")
	}
	if !strings.Contains(parts[docDocumentPath], `<w:ind w:left="709" w:hanging="425"/></w:pPr><w:r><w:t xml:space="preserve">1.</w:t></w:r><w:r><w:tab/></w:r>`) {
		t.Error("reja bandlari raqam + tab va osilgan xatboshi bilan bo'lishi kerak")
	}
}

// TestEstimatePages — ContentWords bo'yicha yozilgan hujjat taxminan so'ralgan
// sahifalar sonida baholanadi.
func TestEstimatePages(t *testing.T) {
	g, err := NewDOCX(t.TempDir(), "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	for _, pages := range []int{8, 15, 30} {
		words := g.ContentWords(pages)
		p := SamplePaper(DocThemeClassic, nil, false)
		p.Introduction, p.Conclusion = nil, nil
		p.Chapters = []Section{{Title: "Bob", Paragraphs: []Paragraph{{Text: strings.TrimSpace(strings.Repeat("so'z ", words))}}}}
		if got := g.EstimatePages(p); got != pages {
			t.Errorf("%d sahifalik hajm %d sahifa deb baholandi", pages, got)
		}
	}
	if _, err := g.Build(context.Background(), SamplePaper(DocThemeClassic, nil, true)); err != nil {
		t.Fatal(err)
	}
}
