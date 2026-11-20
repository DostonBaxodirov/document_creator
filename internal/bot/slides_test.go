package bot

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/storage"
)

// captureSlides — taqdimotni faylga yozmasdan, generatorga berilgan Deck ni saqlaydigan builder.
type captureSlides struct {
	dir string

	mu   sync.Mutex
	deck generator.Deck
}

func (c *captureSlides) Build(_ context.Context, d generator.Deck) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deck = d
	return filepath.Join(c.dir, "taqdimot.pptx"), nil
}

func (*captureSlides) ContentSlides(total int) int { return total - 2 }

// outlinePlanFixture — soxta generator rejasi: 2-slayd (Shaharlar) generator
// javobida yo'q, 3-slaydda (Savdo) rasm tavsifi yo'q.
var outlinePlanFixture = ai.OutlinePlan{
	CoverImage: "The Registan square at dawn",
	Slides: []ai.PlannedSlide{
		{Title: "Reja", Layout: ai.LayoutAgenda, Image: "An old map of the Silk Road"},
		{Title: "Karvonlar", Layout: ai.LayoutProcess, Image: "A camel caravan crossing the desert"},
		{Title: "Shaharlar", Layout: ai.LayoutBullets, Image: "Ancient city walls of Khiva"},
		{Title: "Savdo", Layout: ai.LayoutCards},
		{Title: "Xulosa", Layout: ai.LayoutConclusion, Image: "Sunset over Samarkand domes"},
	},
}

// outlineAI — taqdimot yozadigan soxta generator. outline true bo'lsa,
// haqiqiy client kabi avval OnOutline ni chaqiradi, so'ng slaydlar release
// yopilguncha "yoziladi" (yoki ctx bekor qilinguncha) va aralash tartibda,
// bittasi tushib qolgan holda qaytadi (rasm tavsiflarisiz — ular rejada).
// outline false bo'lsa (kichik taqdimot kabi) — slaydlar tartib bilan, rasm
// tavsiflari slaydlarda.
type outlineAI struct {
	ContentGenerator
	outline bool
	release chan struct{} // nil — kutmaydi
	err     error         // OnOutline dan keyin qaytariladigan xato

	mu  sync.Mutex
	req ai.PresentationRequest
}

func (f *outlineAI) GeneratePresentation(ctx context.Context, req ai.PresentationRequest) (*ai.Presentation, error) {
	f.mu.Lock()
	f.req = req
	f.mu.Unlock()
	slide := func(j int) ai.Slide {
		p := outlinePlanFixture.Slides[j]
		return ai.Slide{Layout: p.Layout, Title: p.Title, Lead: "Kirish.", PlanIndex: j,
			Items: []ai.SlideItem{{Heading: "h", Text: "t"}, {Heading: "h2", Text: "t2"}, {Heading: "h3", Text: "t3"}}}
	}
	pres := &ai.Presentation{Title: "Ipak yo'li", Subtitle: "Tarix"}
	if !f.outline || req.OnOutline == nil {
		pres.Image = outlinePlanFixture.CoverImage
		for j := range outlinePlanFixture.Slides {
			s := slide(j)
			s.Image = outlinePlanFixture.Slides[j].Image
			pres.Slides = append(pres.Slides, s)
		}
		return pres, nil
	}
	req.OnOutline(outlinePlanFixture)
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	for _, j := range []int{0, 3, 1, 4} { // 2 (Shaharlar) yozilmagan
		pres.Slides = append(pres.Slides, slide(j))
	}
	return pres, nil
}

// promptImages — har bir rasmni alohida kenglikda chizadigan xizmat: slayddagi
// rasm qaysi prompt bo'yicha chizilganini uning kengligidan bilish mumkin.
type promptImages struct {
	called chan struct{} // birinchi Generate chaqiruvida yopiladi
	once   sync.Once

	mu     sync.Mutex
	widths map[string]int
	shapes map[string]images.Shape
}

func newPromptImages() *promptImages {
	return &promptImages{called: make(chan struct{}), widths: map[string]int{}, shapes: map[string]images.Shape{}}
}

func (*promptImages) Available() bool { return true }

func (p *promptImages) Generate(_ context.Context, _ images.Tier, reqs []images.Request, onDone func(done, total int)) []images.Result {
	p.once.Do(func() { close(p.called) })
	out := make([]images.Result, len(reqs))
	for i, r := range reqs {
		p.mu.Lock()
		w := 120 + 8*len(p.widths)
		p.widths[r.Prompt], p.shapes[r.Prompt] = w, r.Shape
		p.mu.Unlock()
		out[i] = images.Result{Data: generator.SamplePicture(w, 80, "7A2E1D", "C08A2E").Data, Provider: "fake"}
		if onDone != nil {
			onDone(i+1, len(reqs))
		}
	}
	return out
}

// slidesHandler rasmli taqdimot uchun handler (tayyor dizayn, rasm xizmati imgs).
func slidesHandler(t *testing.T, gen ContentGenerator, imgs ImageService, design string) (*Handler, *captureSlides, *storage.Session, workTypeOption) {
	t.Helper()
	b, _ := fakeTelegram(t, "")
	cs := &captureSlides{dir: t.TempDir()}
	h := NewHandler(context.Background(), nil, gen, Builders{Slides: cs}, zap.NewNop(), Options{Images: imgs})
	h.bot = b
	s := &storage.Session{WorkType: string(ai.WorkPresentation), Design: design, Topic: "Ipak yo'li tarixi",
		Volume: len(outlinePlanFixture.Slides) + 2, Language: "uz"}
	opt, _ := findWorkType(ai.WorkPresentation)
	return h, cs, s, opt
}

// TestBuildSlidesPicturesFromOutline — rasmli taqdimotda rasmlar reja tayyor
// bo'lishi bilan, slaydlar yozilishini kutmasdan chiziladi va slaydlarga
// PlanIndex bo'yicha joylanadi (generator slaydlarni aralash tartibda va
// bittasini tushirib qaytarganda ham).
func TestBuildSlidesPicturesFromOutline(t *testing.T) {
	gen := &outlineAI{outline: true, release: make(chan struct{})}
	imgs := newPromptImages()
	h, cs, s, opt := slidesHandler(t, gen, imgs, generator.ThemeOcean)

	type result struct {
		out *output
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := h.buildSlides(context.Background(), testBoard(t, h.bot), s, opt, zap.NewNop())
		done <- result{out, err}
	}()

	// Slaydlar hali "yozilmoqda" (release yopilmagan) — rasmlar esa chizila boshlagan bo'lishi kerak.
	select {
	case <-imgs.called:
	case r := <-done:
		t.Fatalf("buildSlides slaydlar yozilishini kutmasdan tugadi: %v", r.err)
	case <-time.After(10 * time.Second):
		t.Fatal("rasmlar slaydlar yozilishi bilan parallel chizilmadi")
	}
	close(gen.release)

	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	gen.mu.Lock()
	if !gen.req.Images || gen.req.OnOutline == nil {
		t.Errorf("rasmli dizaynda rasm tavsiflari va OnOutline so'ralishi kerak: images=%v", gen.req.Images)
	}
	gen.mu.Unlock()

	// Titul + tavsifi bor 4 ta reja slaydi so'raladi; yozilmagan Shaharlar slaydining rasmi tashlanadi.
	if !strings.Contains(r.out.summary, "🖼 4/5 rasm (standard, fake)") || !strings.Contains(r.out.caption, "🖼 4 ta rasm") {
		t.Errorf("4/5 rasm joylashishi kerak: %s | %s", r.out.summary, r.out.caption)
	}

	design := generator.DesignFor(generator.ThemeOcean, nil)
	imgs.mu.Lock()
	defer imgs.mu.Unlock()
	if len(imgs.widths) != 5 {
		t.Errorf("5 ta rasm so'ralishi kerak: %v", imgs.widths)
	}
	// Shakl maket bo'yicha: reja va punktlar — yon panel (tik), jarayon va xulosa — banner (yotiq).
	wantShapes := map[string]images.Shape{
		outlinePlanFixture.CoverImage:      images.Landscape,
		outlinePlanFixture.Slides[0].Image: images.Portrait,
		outlinePlanFixture.Slides[1].Image: images.Landscape,
		outlinePlanFixture.Slides[2].Image: images.Portrait,
		outlinePlanFixture.Slides[4].Image: images.Landscape,
	}
	for desc, shape := range wantShapes {
		if got, ok := imgs.shapes[design.PhotoPrompt(desc)]; !ok || got != shape {
			t.Errorf("%q: shakl %v (so'ralgan: %v), kutilgan %v", desc, got, ok, shape)
		}
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()
	deck := cs.deck
	if deck.CoverImage == nil || deck.CoverImage.W != imgs.widths[design.PhotoPrompt(outlinePlanFixture.CoverImage)] {
		t.Errorf("titul rasmi noto'g'ri: %+v", deck.CoverImage)
	}
	// Generator tartibi: Reja, Savdo, Karvonlar, Xulosa.
	wantDesc := []string{outlinePlanFixture.Slides[0].Image, "", outlinePlanFixture.Slides[1].Image, outlinePlanFixture.Slides[4].Image}
	if len(deck.Slides) != len(wantDesc) {
		t.Fatalf("slaydlar soni %d, kutilgan %d", len(deck.Slides), len(wantDesc))
	}
	for i, desc := range wantDesc {
		pic := deck.Slides[i].Image
		switch {
		case desc == "" && pic != nil:
			t.Errorf("%s: rasm tavsifi yo'q slaydga rasm qo'yilmasligi kerak", deck.Slides[i].Title)
		case desc != "" && (pic == nil || pic.W != imgs.widths[design.PhotoPrompt(desc)]):
			t.Errorf("%s: slaydga o'z reja slaydining rasmi qo'yilmagan: %+v", deck.Slides[i].Title, pic)
		}
	}
}

// TestBuildSlidesFallbackWithoutOutline — generator OnOutline ni chaqirmasa
// (kichik taqdimot), rasmlar matndan keyin slaydlardagi tavsiflar bo'yicha chiziladi.
func TestBuildSlidesFallbackWithoutOutline(t *testing.T) {
	imgs := newPromptImages()
	h, cs, s, opt := slidesHandler(t, &outlineAI{}, imgs, generator.ThemeOcean)
	out, err := h.buildSlides(context.Background(), testBoard(t, h.bot), s, opt, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	// Titul + tavsifi bor 4 ta slayd.
	if !strings.Contains(out.summary, "🖼 5/5 rasm") {
		t.Errorf("5 ta rasm joylashishi kerak: %s", out.summary)
	}
	design := generator.DesignFor(generator.ThemeOcean, nil)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i, sl := range cs.deck.Slides {
		desc := outlinePlanFixture.Slides[i].Image
		if desc == "" {
			if sl.Image != nil {
				t.Errorf("%s: rasm tavsifi yo'q slaydga rasm qo'yilmasligi kerak", sl.Title)
			}
			continue
		}
		if sl.Image == nil || sl.Image.W != imgs.widths[design.PhotoPrompt(desc)] {
			t.Errorf("%s: slayd rasmi noto'g'ri: %+v", sl.Title, sl.Image)
		}
	}
}

// TestBuildSlidesPlainNoOutline — rasmsiz dizaynda (yoki rasm xizmati
// ulanmagan bo'lsa) OnOutline o'rnatilmaydi va rasm tavsiflari so'ralmaydi.
func TestBuildSlidesPlainNoOutline(t *testing.T) {
	for name, c := range map[string]struct {
		design string
		imgs   ImageService
	}{
		"rasmsiz dizayn":    {generator.ThemeAcademic, newPromptImages()},
		"rasm xizmati yo'q": {generator.ThemeOcean, nil},
	} {
		gen := &outlineAI{outline: true}
		h, _, s, opt := slidesHandler(t, gen, c.imgs, c.design)
		if _, err := h.buildSlides(context.Background(), testBoard(t, h.bot), s, opt, zap.NewNop()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if gen.req.Images || gen.req.OnOutline != nil {
			t.Errorf("%s: rasm tavsiflari va OnOutline so'ralmasligi kerak", name)
		}
	}
}

// blockImages — ctx bekor qilinguncha "chizadigan" rasm xizmati.
type blockImages struct {
	called    chan struct{}
	cancelled chan struct{}
}

func (*blockImages) Available() bool { return true }

func (b *blockImages) Generate(ctx context.Context, _ images.Tier, reqs []images.Request, _ func(done, total int)) []images.Result {
	close(b.called)
	<-ctx.Done()
	close(b.cancelled)
	return make([]images.Result, len(reqs))
}

// TestBuildSlidesTextErrorStopsPictures — reja tayyor bo'lib, slaydlar
// yozilmay qolsa, fonda chizilayotgan rasmlar to'xtatiladi va AI xatosi qaytadi.
func TestBuildSlidesTextErrorStopsPictures(t *testing.T) {
	gen := &outlineAI{outline: true, release: make(chan struct{}), err: errors.New("slaydlar yozilmadi")}
	imgs := &blockImages{called: make(chan struct{}), cancelled: make(chan struct{})}
	h, _, s, opt := slidesHandler(t, gen, imgs, generator.ThemeOcean)

	done := make(chan error, 1)
	go func() {
		_, err := h.buildSlides(context.Background(), testBoard(t, h.bot), s, opt, zap.NewNop())
		done <- err
	}()
	select {
	case <-imgs.called:
	case <-time.After(10 * time.Second):
		t.Fatal("rasmlar chizila boshlamadi")
	}
	close(gen.release)

	select {
	case err := <-done:
		if !errors.Is(err, errAI) || !strings.Contains(err.Error(), "slaydlar yozilmadi") {
			t.Errorf("AI xatosi qaytishi kerak: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("buildSlides fondagi rasmlarni to'xtatmay osilib qoldi")
	}
	select {
	case <-imgs.cancelled:
	default:
		t.Error("rasm chizish konteksti bekor qilinishi kerak")
	}
}

// TestPlanOutlinePictures — reja bo'yicha rasm so'rovlari: AI maxsus dizaynda
// limit ta kvadrat rasm (titul bilan), slaydlar maket bo'yicha tanlanadi;
// titul tavsifi bo'lmasa — birinchi slayd tavsifi olinadi.
func TestPlanOutlinePictures(t *testing.T) {
	spec := generator.SampleCustomSpecs()[0]
	design := generator.DesignFor(generator.ThemeCustom, &spec)
	op := ai.OutlinePlan{CoverImage: "Cover", Slides: []ai.PlannedSlide{
		{Title: "A", Layout: ai.LayoutAgenda, Image: "a"},
		{Title: "B", Layout: ai.LayoutProcess, Image: "b"},
		{Title: "C", Layout: ai.LayoutBullets}, // yon panelga sig'adi, lekin tavsifi yo'q
		{Title: "D", Layout: ai.LayoutCards, Image: "d"},
		{Title: "E", Layout: ai.LayoutTimeline, Image: "e"},
		{Title: "F", Layout: ai.LayoutConclusion, Image: "f"},
	}}
	p := planOutlinePictures(op, picturePlan{design: design, tier: images.TierStandard, limit: 3})
	// Tavsifi bor slaydlar: A, B, D, E, F — yon panelga sig'adiganlari (A, D) ustuvor.
	if !slices.Equal(p.targets, []int{-1, 0, 3}) {
		t.Errorf("rasm joylari %v, kutilgan [-1 0 3]", p.targets)
	}
	for k, r := range p.reqs {
		if r.Shape != images.Square {
			t.Errorf("maxsus dizaynda %d-rasm kvadrat bo'lishi kerak: %v", k, r.Shape)
		}
	}
	if len(p.reqs) == 3 && (p.reqs[0].Prompt != design.PhotoPrompt("Cover") || p.reqs[2].Prompt != design.PhotoPrompt("d")) {
		t.Errorf("promptlar: %q, %q", p.reqs[0].Prompt, p.reqs[2].Prompt)
	}

	op.CoverImage = ""
	ready := generator.DesignFor(generator.ThemePanorama, nil)
	p = planOutlinePictures(op, picturePlan{design: ready, tier: images.TierStandard})
	if len(p.reqs) != 6 || p.targets[0] != -1 || p.reqs[0].Prompt != ready.PhotoPrompt("a") || p.reqs[0].Shape != images.Landscape {
		t.Fatalf("titul rasmi birinchi slayd tavsifidan, yotiq bo'lishi kerak: %+v", p.reqs)
	}
	for _, r := range p.reqs[1:] {
		if r.Shape != images.Landscape {
			t.Errorf("banner rasmli dizaynda barcha rasmlar yotiq bo'lishi kerak: %v", r.Shape)
		}
	}
}
