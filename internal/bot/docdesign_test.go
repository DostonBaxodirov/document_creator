package bot

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/storage"
)

// TestDocOptionsMatchGenerator — AI Word dizayni variantlari generator
// chiza oladigan variantlar bilan bir xil.
func TestDocOptionsMatchGenerator(t *testing.T) {
	pairs := map[string][2][]string{
		"title":        {ai.DocTitles, generator.DocTitles},
		"heading":      {ai.DocHeadings, generator.DocHeadings},
		"border":       {ai.DocBorders, generator.DocBorders},
		"ornament":     {ai.DocOrnaments, generator.DocOrnaments},
		"heading_font": {ai.DocHeadingFonts, generator.DocHeadingFonts},
	}
	for name, p := range pairs {
		if !slices.Equal(p[0], p[1]) {
			t.Errorf("%s: ai %v ≠ generator %v", name, p[0], p[1])
		}
	}
}

func TestDocDesignPromptAndKeyboard(t *testing.T) {
	p := docDesignPrompt(true)
	if n := len([]rune(p)); n > 1024 {
		t.Errorf("rasm izohi Telegram chegarasidan uzun: %d", n)
	}
	for _, th := range generator.DocThemes() {
		if !strings.Contains(p, th.Name) {
			t.Errorf("tanlash matnida %q yo'q", th.Name)
		}
	}
	kb := docDesignKeyboard(true)
	if first := kb.InlineKeyboard[0][0]; first.Data != generator.ThemeCustom {
		t.Errorf("birinchi tugma maxsus dizayn bo'lishi kerak: %+v", first)
	}
	var ids []string
	for _, row := range kb.InlineKeyboard[1:] {
		for _, b := range row {
			ids = append(ids, b.Data)
		}
	}
	if len(ids) != len(generator.DocThemes()) {
		t.Errorf("tugmalar soni %d, dizaynlar %d", len(ids), len(generator.DocThemes()))
	}
	if strings.Contains(docDesignKeyboard(false).InlineKeyboard[0][0].Data, generator.ThemeCustom) {
		t.Error("maxsus dizayn o'chiq bo'lsa tugmasi bo'lmasligi kerak")
	}
}

func TestDocTariffs(t *testing.T) {
	docx, err := generator.NewDOCX(t.TempDir(), "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	tariffs := testTariffs()
	for k, v := range tariffs {
		v.Images = fakeImages{}
		tariffs[k] = v
	}
	h := NewHandler(context.Background(), nil, nil, Builders{Papers: docx}, zap.NewNop(), Options{
		Billing: &fakeBilling{},
		Payment: PaymentOptions{Enabled: true, PriceSlidePhoto: 300, PriceSlidePlain: 250, PriceDocument: 7000, PricePage: 200},
		Custom:  tariffs,
	})
	s := &storage.Session{WorkType: string(ai.WorkReferat), Design: generator.ThemeCustom, Tariff: config.TariffPremium, Topic: "Ipak yo'li tarixi", Volume: 15}

	// 15 sahifa: ~5 bob → premium (50%) — 3 ta, standart (40%) — 2 ta rasm.
	p := h.tariffPrompt(s)
	for _, want := range []string{"15 sahifa", "1 500 so'm/sahifa", "22 500 so'm", "400 so'm/sahifa", "6 000 so'm", "3 ta rasm", "2 ta rasm", "titul varaq"} {
		if !strings.Contains(p, want) {
			t.Errorf("tarif matnida %q yo'q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "slayd") {
		t.Errorf("Word ish tarif matnida slayd so'zi bo'lmasligi kerak:\n%s", p)
	}
	kb := h.tariffKeyboard(s)
	if got := kb.InlineKeyboard[2][0].Text; !strings.Contains(got, "22 500 so'm") {
		t.Errorf("premium tugmasi narxi: %q", got)
	}
	if got := h.price(s, h.kindOf(s)); got != 22500 {
		t.Errorf("maxsus dizayndagi 15 sahifali referat 22 500 so'm bo'lishi kerak, olindi %d", got)
	}
	if l := orderLabel(s.WorkType, h.kindOf(s)); l != "Referat, ✨ Maxsus dizayn — 🥇 Premium" {
		t.Errorf("buyurtma nomi: %q", l)
	}
}

func TestCustomDocImageCount(t *testing.T) {
	for _, c := range []struct{ words, percent, want int }{
		{2690, 50, 3}, {2690, 40, 2}, {2690, 0, 0}, {300, 40, 1}, {20000, 100, 6},
	} {
		if got := customDocImageCount(c.words, c.percent); got != c.want {
			t.Errorf("customDocImageCount(%d, %d) = %d, kutilgan %d", c.words, c.percent, got, c.want)
		}
	}
	// Rasmlar soni qisqargan matndagi boblar sonidan oshmaydi.
	for _, c := range []struct{ words, percent int }{{2690, 100}, {700, 100}, {6365, 100}, {2690, 50}, {1000, 0}} {
		words, figures := docPlan(c.words, 110, c.percent)
		if chapters := ai.PlanChapters(words); figures > chapters {
			t.Errorf("docPlan(%d, %d): %d rasm > %d bob", c.words, c.percent, figures, chapters)
		}
		if want := max(c.words/2, c.words-figures*110); words != want {
			t.Errorf("docPlan(%d, %d): matn %d, kutilgan %d", c.words, c.percent, words, want)
		}
		if c.percent == 0 && (figures != 0 || words != c.words) {
			t.Errorf("rasmsiz tarifda matn qisqarmasligi kerak: %d, %d", words, figures)
		}
	}
	if got := spreadPick([]int{0, 1, 2, 3, 4}, 3); !slices.Equal(got, []int{0, 2, 4}) {
		t.Errorf("spreadPick: %v", got)
	}
	if got := spreadPick([]int{1, 3}, 5); !slices.Equal(got, []int{1, 3}) {
		t.Errorf("spreadPick ko'proq so'ralsa — hammasi: %v", got)
	}
}

// fakeDocAI — Word ish matni va dizaynini qaytaradigan AI.
type fakeDocAI struct {
	ContentGenerator
	mu     sync.Mutex
	docReq ai.DocumentRequest
}

func (f *fakeDocAI) GenerateDocument(_ context.Context, req ai.DocumentRequest) (*ai.Document, error) {
	f.mu.Lock()
	f.docReq = req
	f.mu.Unlock()
	doc := &ai.Document{
		Introduction: []ai.Paragraph{{Text: "Kirish matni."}},
		Conclusion:   []ai.Paragraph{{Text: "Xulosa matni."}},
		References:   []string{"Muallif A. Kitob. — Toshkent, 2020."},
	}
	for i := range ai.PlanChapters(req.Words) {
		ch := ai.Chapter{Title: "Bob " + string(rune('A'+i)), Paragraphs: []ai.Paragraph{{Text: "Birinchi paragraf."}, {Text: "Ikkinchi paragraf."}}}
		if req.Figures {
			ch.Figure = &ai.FigureIdea{Caption: "Rasm izohi " + string(rune('A'+i)), Prompt: "A caravan on the Silk Road"}
		}
		doc.Chapters = append(doc.Chapters, ch)
	}
	return doc, nil
}

func (f *fakeDocAI) GenerateDocDesign(context.Context, ai.DesignRequest) (*ai.DocDesign, error) {
	return &ai.DocDesign{Name: "Ipak yo'li", Primary: "#7A2E1D", Accent: "#C08A2E", HeadingFont: "Georgia",
		Title: "frame", Heading: "underline", Border: "thinthick", Ornament: "diamond", ImageStyle: "warm sepia photography"}, nil
}

// failAI — chaqirilmasligi kerak bo'lgan standart AI.
type failAI struct{ ContentGenerator }

func (failAI) GenerateDocument(context.Context, ai.DocumentRequest) (*ai.Document, error) {
	return nil, errors.New("standart model chaqirilmasligi kerak edi")
}

// sampleImages — har bir so'rovga namunaviy JPEG qaytaradigan rasm xizmati.
type sampleImages struct {
	mu      sync.Mutex
	prompts []string
}

func (*sampleImages) Available() bool { return true }

func (s *sampleImages) Generate(_ context.Context, _ images.Tier, reqs []images.Request, onDone func(done, total int)) []images.Result {
	out := make([]images.Result, len(reqs))
	for i, r := range reqs {
		s.mu.Lock()
		s.prompts = append(s.prompts, r.Prompt)
		s.mu.Unlock()
		out[i] = images.Result{Data: generator.SamplePicture(600, 400, "7A2E1D", "C08A2E").Data, Provider: "fake"}
		if onDone != nil {
			onDone(i+1, len(reqs))
		}
	}
	return out
}

// docxParts .docx ichidagi fayllar (yo'l → matn).
func docxParts(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(data)
	}
	return out
}

func TestBuildPaperCustomReferat(t *testing.T) {
	b, _ := fakeTelegram(t, "")
	docx, err := generator.NewDOCX(t.TempDir(), "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	gen, imgs := &fakeDocAI{}, &sampleImages{}
	tariffs := testTariffs()
	for k, v := range tariffs {
		v.AI, v.Images = gen, imgs
		tariffs[k] = v
	}
	h := NewHandler(context.Background(), nil, failAI{}, Builders{Papers: docx}, zap.NewNop(), Options{Custom: tariffs})
	h.bot = b

	s := &storage.Session{WorkType: string(ai.WorkReferat), Design: generator.ThemeCustom, Tariff: config.TariffPremium,
		Topic: "Ipak yo'li tarixi", Volume: 15, Language: "uz", StudentName: "Aliyev Vali", University: "TDU", TeacherName: "Karimova D."}
	opt, _ := findWorkType(ai.WorkReferat)
	out, err := h.buildPaper(context.Background(), testBoard(t, b), s, opt, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	if !gen.docReq.Figures || gen.docReq.WorkType != ai.WorkReferat {
		t.Errorf("maxsus dizaynda rasm g'oyalari so'ralishi kerak: %+v", gen.docReq)
	}
	if full := docx.ContentWords(15); gen.docReq.Words != full-3*docx.FigureWords() {
		t.Errorf("matn hajmi 3 ta rasm joyiga kamaytirilishi kerak: %d (to'liq %d)", gen.docReq.Words, full)
	}
	if len(imgs.prompts) != 3 || !strings.Contains(imgs.prompts[0], "warm sepia photography") {
		t.Errorf("3 ta rasm dizayn uslubi bilan chizilishi kerak: %v", imgs.prompts)
	}
	for _, want := range []string{"🖼 3/3 rasm", "Ipak yo'li (maxsus dizayn, 🥇 Premium)", "15 sahifa (≈"} {
		if !strings.Contains(out.summary, want) {
			t.Errorf("admin xulosasida %q yo'q: %s", want, out.summary)
		}
	}
	if !strings.Contains(out.caption, "Referat · 15 sahifa") {
		t.Errorf("fayl izohi: %s", out.caption)
	}

	parts := docxParts(t, out.path)
	doc := parts["word/document.xml"]
	for _, want := range []string{"REFERAT", "1-rasm.", "3-rasm.", "Rasm izohi", "<wp:inline"} {
		if !strings.Contains(doc, want) {
			t.Errorf("hujjatda %q yo'q", want)
		}
	}
	media := 0
	for name := range parts {
		if strings.HasPrefix(name, "word/media/") {
			media++
		}
	}
	if media != 3 {
		t.Errorf("hujjatda 3 ta rasm bo'lishi kerak, bor: %d", media)
	}
	if !strings.Contains(doc, "thinThickSmallGap") || !strings.Contains(parts["word/styles.xml"], "Georgia") {
		t.Error("AI dizayni (ramka, sarlavha shrifti) qo'llanmagan")
	}
}

func TestBuildPaperReadyDesign(t *testing.T) {
	b, _ := fakeTelegram(t, "")
	docx, err := generator.NewDOCX(t.TempDir(), "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	gen := &fakeDocAI{}
	h := NewHandler(context.Background(), nil, gen, Builders{Papers: docx}, zap.NewNop(), Options{})
	h.bot = b

	s := &storage.Session{WorkType: string(ai.WorkIndependent), Design: generator.DocThemeFrame, Topic: "Mavzu", Volume: 10}
	opt, _ := findWorkType(ai.WorkIndependent)
	out, err := h.buildPaper(context.Background(), testBoard(t, b), s, opt, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if gen.docReq.Figures {
		t.Error("tayyor dizaynda rasmlar so'ralmasligi kerak")
	}
	if !strings.Contains(out.caption, "🖼 Ramkali") {
		t.Errorf("fayl izohida dizayn nomi yo'q: %s", out.caption)
	}
	if doc := docxParts(t, out.path)["word/document.xml"]; !strings.Contains(doc, `<w:pgBorders`) || !strings.Contains(doc, "MUSTAQIL ISH") {
		t.Error("Ramkali dizaynda sahifa ramkasi bo'lishi kerak")
	}

	// Amaliy topshiriq: sessiyada dizayn qolib ketgan bo'lsa ham — Klassik.
	s = &storage.Session{WorkType: string(ai.WorkPractical), Design: generator.DocThemeFrame, Topic: "Mavzu", Volume: 10}
	opt, _ = findWorkType(ai.WorkPractical)
	if out, err = h.buildPaper(context.Background(), testBoard(t, b), s, opt, zap.NewNop()); err != nil {
		t.Fatal(err)
	}
	if doc := docxParts(t, out.path)["word/document.xml"]; strings.Contains(doc, `<w:pgBorders`) {
		t.Error("amaliy topshiriq Klassik uslubda bo'lishi kerak")
	}
}
