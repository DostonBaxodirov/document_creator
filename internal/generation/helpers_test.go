package generation

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
)

// testTariffs — maxsus dizayn tariflari (narxlarsiz — ular bot paketida).
func testTariffs() map[string]Tariff {
	return map[string]Tariff{
		config.TariffStandard: {ImagePercent: 40, TextModel: "gpt-4o-mini", ImageModel: "gpt-image-1", ImageQuality: "low"},
		config.TariffOptimal:  {ImagePercent: 40, TextModel: "gpt-4.1-mini", ImageModel: "gpt-image-1", ImageQuality: "medium"},
		config.TariffPremium:  {ImagePercent: 50, TextModel: "gpt-4o", ImageModel: "gpt-image-1", ImageQuality: "high"},
	}
}

// testProgress — bosqichlarni xotirada yozib boradigan Progress (Telegram'siz).
// report va done bir vaqtda bir nechta goroutine'dan chaqirilishi mumkin.
type testProgress struct {
	mu     sync.Mutex
	stages []Stage
	open   int // yakunlanmagan bosqichlar soni
}

func (p *testProgress) Stage(st Stage) (report func(done, total int), done func()) {
	p.mu.Lock()
	p.stages = append(p.stages, st)
	p.open++
	p.mu.Unlock()
	var once sync.Once
	return func(int, int) {}, func() {
		once.Do(func() {
			p.mu.Lock()
			p.open--
			p.mu.Unlock()
		})
	}
}

// icons ro'yxatdan o'tgan bosqichlar belgilari (tartib bilan) va yakunlanmagan bosqichlar soni.
func (p *testProgress) icons() (icons []string, open int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, st := range p.stages {
		icons = append(icons, st.Icon)
	}
	return icons, p.open
}

// fakeDocAI — Word ish matni va dizaynini qaytaradigan AI. Haqiqiy client
// kabi rejadan keyin (bo'limlar "yozilishidan" oldin) OnOutline ni chaqiradi.
type fakeDocAI struct {
	ContentGenerator
	// sections nil bo'lmasa — OnOutline dan keyin bo'limlar shu kanal
	// yopilguncha "yoziladi" (yoki ctx bekor qilinguncha).
	sections chan struct{}

	mu     sync.Mutex
	docReq ai.DocumentRequest
}

func (f *fakeDocAI) GenerateDocument(ctx context.Context, req ai.DocumentRequest) (*ai.Document, error) {
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
	if req.OnOutline != nil {
		ideas := make([]*ai.FigureIdea, len(doc.Chapters))
		for i, ch := range doc.Chapters {
			ideas[i] = ch.Figure
		}
		req.OnOutline(ideas)
	}
	if f.sections != nil {
		select {
		case <-f.sections:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
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

func (s *sampleImages) Generate(_ context.Context, reqs []images.Request, onDone func(done, total int)) []images.Result {
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
