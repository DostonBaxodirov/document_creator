package generation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
)

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

func TestBuildPaperCustomReferat(t *testing.T) {
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
	svc := New(Config{AI: failAI{}, Papers: docx, Tariffs: tariffs})

	o := Order{WorkType: ai.WorkReferat, Design: generator.ThemeCustom, Tariff: config.TariffPremium,
		Topic: "Ipak yo'li tarixi", Volume: 15, Language: "uz", StudentName: "Aliyev Vali", University: "TDU", TeacherName: "Karimova D."}
	progress := &testProgress{}
	out, err := svc.Build(context.Background(), o, progress)
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
		if !strings.Contains(out.Summary, want) {
			t.Errorf("admin xulosasida %q yo'q: %s", want, out.Summary)
		}
	}
	if !strings.Contains(out.Caption, "Referat · 15 sahifa") {
		t.Errorf("fayl izohi: %s", out.Caption)
	}
	if out.MIME != docxMIME || out.FileName != "Ipak_yo'li_tarixi.docx" {
		t.Errorf("fayl turi va nomi: %q, %q", out.MIME, out.FileName)
	}
	// Matn, dizayn va rasmlar bosqichlari jarayonda ko'rsatiladi va hammasi yakunlanadi.
	icons, open := progress.icons()
	for _, want := range []string{TextStage("").Icon, DesignStage.Icon, DocImageStage.Icon} {
		if !slices.Contains(icons, want) {
			t.Errorf("jarayonda %q bosqichi yo'q: %v", want, icons)
		}
	}
	if open != 0 {
		t.Errorf("Build tugaganda barcha bosqichlar yakunlanishi kerak, ochiq: %d", open)
	}

	parts := docxParts(t, out.Path)
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

// signalImages — Generate birinchi marta chaqirilganda called ni yopadigan sampleImages.
type signalImages struct {
	*sampleImages
	once   sync.Once
	called chan struct{}
}

func (s *signalImages) Generate(ctx context.Context, reqs []images.Request, onDone func(done, total int)) []images.Result {
	s.once.Do(func() { close(s.called) })
	return s.sampleImages.Generate(ctx, reqs, onDone)
}

// failDesignAI — Word dizaynini yarata olmaydigan AI.
type failDesignAI struct{ *fakeDocAI }

func (failDesignAI) GenerateDocDesign(context.Context, ai.DesignRequest) (*ai.DocDesign, error) {
	return nil, errors.New("dizayn modeli javob bermadi")
}

// customPaperService maxsus dizayndagi referat uchun servis: barcha
// tariflar gen va imgs bilan.
func customPaperService(t *testing.T, gen ContentGenerator, imgs ImageService) (*Service, Order) {
	t.Helper()
	docx, err := generator.NewDOCX(t.TempDir(), "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	tariffs := testTariffs()
	for k, v := range tariffs {
		v.AI, v.Images = gen, imgs
		tariffs[k] = v
	}
	svc := New(Config{AI: failAI{}, Papers: docx, Tariffs: tariffs})
	o := Order{WorkType: ai.WorkReferat, Design: generator.ThemeCustom, Tariff: config.TariffPremium,
		Topic: "Ipak yo'li tarixi", Volume: 15, Language: "uz"}
	return svc, o
}

// TestBuildPaperFiguresWhileWriting — maxsus dizaynda rasmlar reja tayyor
// bo'lishi bilan, bo'limlar yozilishi bilan parallel chiziladi.
func TestBuildPaperFiguresWhileWriting(t *testing.T) {
	gen := &fakeDocAI{sections: make(chan struct{})}
	imgs := &signalImages{sampleImages: &sampleImages{}, called: make(chan struct{})}
	svc, o := customPaperService(t, gen, imgs)

	type result struct {
		out *Output
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := svc.Build(context.Background(), o, &testProgress{})
		done <- result{out, err}
	}()

	// Bo'limlar hali "yozilmoqda" (sections yopilmagan) — rasmlar esa chizila boshlagan bo'lishi kerak.
	select {
	case <-imgs.called:
	case r := <-done:
		t.Fatalf("buildPaper bo'limlar yozilishini kutmasdan tugadi: %v", r.err)
	case <-time.After(10 * time.Second):
		t.Fatal("rasmlar bo'limlar yozilishi bilan parallel chizilmadi")
	}
	close(gen.sections)

	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !strings.Contains(r.out.Summary, "🖼 3/3 rasm") {
		t.Errorf("3 ta rasm joylashishi kerak: %s", r.out.Summary)
	}
	imgs.mu.Lock()
	defer imgs.mu.Unlock()
	if len(imgs.prompts) != 3 || !strings.Contains(imgs.prompts[0], "warm sepia photography") {
		t.Errorf("3 ta rasm dizayn uslubi bilan chizilishi kerak: %v", imgs.prompts)
	}
}

// TestBuildPaperFallbackWithoutOutline — generator OnOutline ni chaqirmasa,
// rasmlar matndan keyin hujjat boblaridagi g'oyalar bo'yicha chiziladi.
func TestBuildPaperFallbackWithoutOutline(t *testing.T) {
	imgs := &sampleImages{}
	svc, o := customPaperService(t, noOutlineAI{&fakeDocAI{}}, imgs)
	out, err := svc.Build(context.Background(), o, &testProgress{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs.prompts) != 3 || !strings.Contains(imgs.prompts[0], "warm sepia photography") {
		t.Errorf("3 ta rasm dizayn uslubi bilan chizilishi kerak: %v", imgs.prompts)
	}
	if !strings.Contains(out.Summary, "🖼 3/3 rasm") {
		t.Errorf("3 ta rasm joylashishi kerak: %s", out.Summary)
	}
	if doc := docxParts(t, out.Path)["word/document.xml"]; !strings.Contains(doc, "3-rasm.") || !strings.Contains(doc, "Rasm izohi") {
		t.Error("rasmlar izohi bilan hujjatga qo'shilishi kerak")
	}
}

// noOutlineAI — OnOutline ni e'tiborsiz qoldiradigan generator.
type noOutlineAI struct{ *fakeDocAI }

func (a noOutlineAI) GenerateDocument(ctx context.Context, req ai.DocumentRequest) (*ai.Document, error) {
	req.OnOutline = nil
	return a.fakeDocAI.GenerateDocument(ctx, req)
}

// TestBuildPaperDesignError — dizayn xatosi AI xatosi sifatida qaytadi,
// qolgan bosqichlar to'xtatiladi va rasmlar chizilmaydi.
func TestBuildPaperDesignError(t *testing.T) {
	for name, sections := range map[string]chan struct{}{
		"matn tugagach":       nil,
		"matn yozilayotganda": make(chan struct{}), // yopilmaydi: matn faqat bekor qilinib to'xtaydi
	} {
		t.Run(name, func(t *testing.T) {
			imgs := &sampleImages{}
			svc, o := customPaperService(t, failDesignAI{&fakeDocAI{sections: sections}}, imgs)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			out, err := svc.Build(ctx, o, &testProgress{})
			if err == nil {
				t.Fatalf("xato kutilgan edi, natija: %+v", out)
			}
			if !errors.Is(err, ErrAI) || !strings.Contains(err.Error(), "dizayn modeli javob bermadi") {
				t.Errorf("dizayn xatosi ErrAI bilan qaytishi kerak: %v", err)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("matn yozish dizayn xatosidan keyin to'xtatilishi kerak: %v", err)
			}
			imgs.mu.Lock()
			defer imgs.mu.Unlock()
			if len(imgs.prompts) != 0 {
				t.Errorf("dizayn bo'lmasa rasmlar chizilmasligi kerak: %v", imgs.prompts)
			}
		})
	}
}

func TestBuildPaperReadyDesign(t *testing.T) {
	docx, err := generator.NewDOCX(t.TempDir(), "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	gen := &fakeDocAI{}
	svc := New(Config{AI: gen, Papers: docx})

	o := Order{WorkType: ai.WorkIndependent, Design: generator.DocThemeFrame, Topic: "Mavzu", Volume: 10}
	out, err := svc.Build(context.Background(), o, &testProgress{})
	if err != nil {
		t.Fatal(err)
	}
	if gen.docReq.Figures {
		t.Error("tayyor dizaynda rasmlar so'ralmasligi kerak")
	}
	if !strings.Contains(out.Caption, "🖼 Ramkali") {
		t.Errorf("fayl izohida dizayn nomi yo'q: %s", out.Caption)
	}
	if doc := docxParts(t, out.Path)["word/document.xml"]; !strings.Contains(doc, `<w:pgBorders`) || !strings.Contains(doc, "MUSTAQIL ISH") {
		t.Error("Ramkali dizaynda sahifa ramkasi bo'lishi kerak")
	}

	// Amaliy topshiriq: buyurtmada dizayn qolib ketgan bo'lsa ham — Klassik.
	o = Order{WorkType: ai.WorkPractical, Design: generator.DocThemeFrame, Topic: "Mavzu", Volume: 10}
	if out, err = svc.Build(context.Background(), o, &testProgress{}); err != nil {
		t.Fatal(err)
	}
	if doc := docxParts(t, out.Path)["word/document.xml"]; strings.Contains(doc, `<w:pgBorders`) {
		t.Error("amaliy topshiriq Klassik uslubda bo'lishi kerak")
	}
}
