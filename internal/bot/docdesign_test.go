package bot

import (
	"context"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generation"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

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
		Payment: config.PaymentConfig{Enabled: true, PriceSlidePhoto: 300, PriceSlidePlain: 250, PriceDocument: 7000, PricePage: 200},
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

// fakeDocAI — Word ish matni va dizaynini qaytaradigan AI. Haqiqiy client
// kabi rejadan keyin (bo'limlar "yozilishidan" oldin) OnOutline ni chaqiradi.
type fakeDocAI struct {
	generation.ContentGenerator
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
