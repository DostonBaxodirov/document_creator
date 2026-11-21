package generation

// paper.go — Word ish (.docx): mustaqil ish, referat va amaliy topshiriq.
// Maxsus dizaynda tarif modeli mavzuga mos uslub yaratadi va matnni yozadi
// (ikkalasi parallel), boblarning bir qismiga esa tarif sifatida rasm
// chiziladi — rasmlar reja tayyor bo'lishi bilan, matn bilan parallel.

import (
	"context"
	"fmt"
	"html"
	"math"
	"runtime/debug"
	"strconv"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/lang"
)

// buildPaper Word ishni (mustaqil ish, referat, amaliy topshiriq) .docx
// ko'rinishida yaratadi. Hujjat bir necha AI so'rovi bilan yoziladi, shuning
// uchun jarayon holati progress orqali ko'rsatib boriladi.
//
// Maxsus dizaynda tarif modeli mavzuga mos uslub yaratadi va matnni yozadi
// (ikkalasi parallel), boblarning bir qismiga esa tarif sifatida rasm
// chiziladi (matn hajmi rasmlar egallaydigan joyga qarab kamaytiriladi) —
// rasmlar reja tayyor bo'lishi bilan, matn bilan parallel.
func (s *Service) buildPaper(ctx context.Context, progress Progress, o Order, log *zap.Logger) (*Output, error) {
	b := s.papers
	li := lang.Get(o.Language)

	var (
		spec     *generator.DocSpec
		aiDesign *ai.DocDesign
		tariff   Tariff
		words    = b.ContentWords(o.Volume)
		figures  int
	)
	custom := o.custom()
	if custom {
		var ok bool
		if tariff, ok = s.tariffs[o.Tariff]; !ok {
			return nil, fmt.Errorf("maxsus dizayn tarifi topilmadi: %q", o.Tariff)
		}
		if s.docImagesOn(tariff) {
			words, figures = docPlan(words, b.FigureWords(), tariff.ImagePercent)
		}
	}

	req := ai.DocumentRequest{
		WorkType:     o.WorkType,
		Topic:        o.Topic,
		Words:        words,
		Language:     li.Prompt,
		Requirements: o.Requirements,
		Figures:      figures > 0,
	}
	var (
		doc   *ai.Document
		drawn *docFigures // matn bilan parallel chizilgan rasmlar (nil — chizish boshlanmagan)
	)
	if custom {
		cp, err := writeCustomPaper(ctx, progress, log, o, tariff, req, figures)
		if err != nil {
			return nil, err
		}
		doc, aiDesign, spec, drawn = cp.doc, cp.design, cp.spec, cp.figures
	} else {
		report, stopProgress := progress.Stage(TextStage("Bo'limlar yozilmoqda..."))
		req.OnProgress = report
		var err error
		doc, err = s.ai.GenerateDocument(ctx, req)
		stopProgress()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAI, err)
		}
	}

	paper := generator.Paper{
		Topic:        o.Topic,
		WorkType:     workTypeTitle(o.WorkType, li),
		StudentName:  o.StudentName,
		University:   o.University,
		TeacherName:  o.TeacherName,
		Year:         strconv.Itoa(time.Now().Year()),
		Lang:         string(li.Code),
		Introduction: toParagraphs(doc.Introduction),
		Conclusion:   toParagraphs(doc.Conclusion),
		References:   doc.References,
		Chapters:     make([]generator.Section, 0, len(doc.Chapters)),
		Custom:       spec,
	}
	if o.WorkType.HasDesigns() {
		paper.Design = o.Design
	}
	for _, ch := range doc.Chapters {
		paper.Chapters = append(paper.Chapters, generator.Section{Title: ch.Title, Paragraphs: toParagraphs(ch.Paragraphs)})
	}
	var pics picturesInfo
	if figures > 0 {
		if drawn != nil {
			pics = drawn.place(log, &paper)
		} else {
			// Generator OnOutline ni chaqirmagan — rasmlar matndan keyin,
			// hujjat boblaridagi g'oyalar bo'yicha chiziladi.
			imageStyle := ""
			if aiDesign != nil {
				imageStyle = aiDesign.ImageStyle
			}
			pics = addDocFigures(ctx, progress, log, doc, &paper, tariff.Images, imageStyle, figures)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	path, err := b.Build(ctx, paper)
	if err != nil {
		return nil, fmt.Errorf("docx: %w", err)
	}

	designName := ""
	switch {
	case custom:
		designName = customCaptionName(aiDesign.Name, o.Tariff)
	case o.WorkType.HasDesigns():
		if t, ok := generator.FindDocTheme(o.Design); ok {
			designName = t.Emoji + " " + t.Name
		}
	}
	// Admin uchun: so'ralgan va taxminan chiqqan sahifalar soni (farq katta
	// bo'lsa — matn hajmini tekshirish kerak).
	estimated := b.EstimatePages(paper)
	caption := fmt.Sprintf("📎 <b>%s</b>\n%s · %d sahifa", html.EscapeString(o.Topic), html.EscapeString(WorkTypeName(o.WorkType)), o.Volume)
	summary := fmt.Sprintf("%d sahifa (≈%d chiqdi) · %d bob · %d manba", o.Volume, estimated, len(paper.Chapters), len(paper.References))
	if designName != "" {
		caption += " · " + html.EscapeString(designName)
		summary += " · " + designName
	}
	if custom {
		summary += fmt.Sprintf(" · %s, %s:%s", tariff.TextModel, tariff.ImageModel, tariff.ImageQuality)
	}
	if pics.requested > 0 {
		summary += fmt.Sprintf(" · 🖼 %d/%d rasm", pics.placed, pics.requested)
	}

	return &Output{
		Path:     path,
		FileName: documentFileName(o.Topic, ".docx"),
		MIME:     docxMIME,
		Caption:  caption,
		Summary:  summary,
		LogFields: []zap.Field{
			zap.Int("pages_requested", o.Volume),
			zap.Int("pages_estimated", estimated),
			zap.Int("chapters", len(paper.Chapters)),
			zap.Int("references", len(paper.References)),
			zap.String("design", o.Design),
			zap.Int("figures", pics.placed),
		},
	}, nil
}

// customDocImageCount Word ishdagi rasmlar soni: taxminiy boblar sonining
// percent foizi, kamida 1 (percent 0 bo'lsa — rasmsiz).
func customDocImageCount(contentWords, percent int) int {
	if percent <= 0 {
		return 0
	}
	chapters := ai.PlanChapters(contentWords)
	return min(chapters, max(1, int(math.Round(float64(chapters)*float64(percent)/100))))
}

// docPlan maxsus dizayndagi Word ish uchun matn hajmi (so'zlarda) va rasmlar
// soni. Rasmlar matn o'rnini egallaydi, shuning uchun matn qisqartiriladi;
// qisqargan matnda boblar kamroq bo'lishi mumkin — rasmlar soni boblar
// sonidan oshmaydi (tarif matnida ko'rsatilgan son hujjatdagiga teng bo'lsin).
func docPlan(contentWords, figureWords, percent int) (words, figures int) {
	figures = customDocImageCount(contentWords, percent)
	if figures == 0 {
		return contentWords, 0
	}
	shrink := func(n int) int { return max(contentWords/2, contentWords-n*figureWords) }
	figures = min(figures, ai.PlanChapters(shrink(figures)))
	return shrink(figures), figures
}

// docImagesOn maxsus dizayndagi Word ishga rasm chizish mumkinligini bildiradi.
func (s *Service) docImagesOn(t Tariff) bool {
	return t.Images != nil && t.Images.Available() && s.papers != nil
}

// customDocDesign AI dan Word dizayni konsepsiyasini oladi va uni generator
// spetsifikatsiyasiga aylantiradi.
func customDocDesign(ctx context.Context, progress Progress, log *zap.Logger, o Order, t Tariff, language string) (*ai.DocDesign, *generator.DocSpec, error) {
	d, err := designStep(progress, log, "custom doc design generated", func() (*ai.DocDesign, error) {
		return t.AI.GenerateDocDesign(ctx, ai.DesignRequest{Topic: o.Topic, Language: language, Requirements: o.Requirements})
	}, func(d *ai.DocDesign) []zap.Field {
		return []zap.Field{zap.String("name", d.Name), zap.String("title", d.Title), zap.String("heading", d.Heading),
			zap.String("border", d.Border), zap.String("primary", d.Primary)}
	})
	if err != nil {
		return nil, nil, err
	}
	spec := d.Spec()
	return d, &spec, nil
}

// customPaper — maxsus dizayndagi Word ish uchun AI natijalari.
type customPaper struct {
	doc    *ai.Document
	design *ai.DocDesign
	spec   *generator.DocSpec
	// figures — matn bilan parallel chizilgan rasmlar; nil — chizish
	// boshlanmagan (rasmlar so'ralmagan yoki generator OnOutline ni chaqirmagan).
	figures *docFigures
}

// writeCustomPaper maxsus dizayndagi Word ishning AI bosqichlarini parallel
// bajaradi:
//   - dizayn konsepsiyasi matnga bog'liq emas — matn bilan bir vaqtda yaratiladi;
//   - rasm g'oyalari rejada bo'ladi: reja tayyor bo'lishi bilan (OnOutline)
//     rasmlar bo'limlar yozilishini kutmasdan chiziladi — faqat rasm uslubi
//     uchun dizayn tayyor bo'lishi kutiladi.
//
// Birorta bosqich xato bilan tugasa, qolganlari to'xtatiladi va birinchi
// sodir bo'lgan xato qaytariladi (AI xatolari ErrAI bilan belgilangan).
func writeCustomPaper(ctx context.Context, progress Progress, log *zap.Logger, o Order,
	t Tariff, req ai.DocumentRequest, figures int) (*customPaper, error) {
	g, gctx := errgroup.WithContext(ctx)
	out := &customPaper{}

	// Matn — eng uzun bosqich: uning qatori jarayon xabarida birinchi turadi,
	// dizayn va rasmlar qatorlari ostida paydo bo'lib, tugagach yo'qoladi.
	report, stopText := progress.Stage(TextStage("Bo'limlar yozilmoqda..."))
	defer stopText()

	// Dizayn konsepsiyasi.
	designDone := make(chan struct{})
	goRecover(g, log, "dizayn", func() error {
		defer close(designDone)
		d, spec, err := customDocDesign(gctx, progress, log, o, t, req.Language)
		if err != nil {
			return err
		}
		out.design, out.spec = d, spec
		return nil
	})

	// Rasmlar: reja tayyor bo'lgach boshlanadi (bitta goroutine).
	if figures > 0 {
		req.OnOutline = func(ideas []*ai.FigureIdea) {
			f := planDocFigures(ideas, len(ideas), figures)
			out.figures = f
			if len(f.targets) == 0 {
				log.Warn("AI returned no figure ideas")
				return
			}
			goRecover(g, log, "rasmlar", func() error {
				// Rasm uslubi dizayndan olinadi — dizayn tayyor bo'lishini kutamiz.
				select {
				case <-designDone:
				case <-gctx.Done():
					return nil
				}
				// Dizayn (yoki boshqa bosqich) xatosi guruhda qayd etilgan — rasmlar chizilmaydi.
				if out.design == nil || gctx.Err() != nil {
					return nil
				}
				f.draw(gctx, progress, t.Images, out.design.ImageStyle)
				return nil
			})
		}
	}

	// Matn — asosiy yo'lda.
	req.OnProgress = report
	doc, err := t.AI.GenerateDocument(gctx, req)
	stopText()
	if err != nil {
		// Matn xatosi ham guruhga beriladi: qolgan bosqichlar to'xtatiladi va
		// birinchi sodir bo'lgan xato qaytariladi (dizayn xatosi tufayli matn
		// yozish bekor qilingan bo'lsa — dizayn xatosi).
		g.Go(func() error { return fmt.Errorf("%w: %w", ErrAI, err) })
		return nil, g.Wait()
	}
	// Dizayn va (boshlangan bo'lsa) rasmlar tugashini kutamiz.
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out.doc = doc
	return out, nil
}

// goRecover fn ni guruhda ishga tushiradi va undagi panic'ni xatoga aylantiradi:
// errgroup panic'ni Wait ga uzatmaydi, goroutine'dagi panic esa butun botni
// to'xtatib qo'yardi (chaqiruvchining recover'i faqat o'z goroutine'ini himoya qiladi).
func goRecover(g *errgroup.Group, log *zap.Logger, stage string, fn func() error) {
	g.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered in paper stage", zap.String("stage", stage),
					zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
				err = fmt.Errorf("%s bosqichida panic: %v", stage, r)
			}
		}()
		return fn()
	})
}

// addDocFigures rasm g'oyasi bor boblardan n tasiga (teng oraliqda) rasm
// chizadi va ularni hujjatga qo'shadi. Chizilmagan rasm o'rnida bob rasmsiz qoladi.
//
// Matn yozilgandan keyin chaqiriladi — generator OnOutline ni chaqirmagan
// holat uchun (odatda rasmlar matn bilan parallel chiziladi: writeCustomPaper).
func addDocFigures(ctx context.Context, progress Progress, log *zap.Logger, doc *ai.Document,
	paper *generator.Paper, svc ImageService, imageStyle string, n int) picturesInfo {
	ideas := make([]*ai.FigureIdea, len(doc.Chapters))
	for i, ch := range doc.Chapters {
		ideas[i] = ch.Figure
	}
	f := planDocFigures(ideas, len(paper.Chapters), n)
	if len(f.targets) == 0 || svc == nil {
		log.Warn("AI returned no figure ideas")
		return picturesInfo{requested: len(f.targets)}
	}
	f.draw(ctx, progress, svc, imageStyle)
	return f.place(log, paper)
}

// docFigures — Word ish boblariga chiziladigan rasmlar.
type docFigures struct {
	ideas   []*ai.FigureIdea // bob indeksi bo'yicha rasm g'oyalari (nil — g'oya yo'q)
	targets []int            // rasm chiziladigan boblar (ideas indekslari)
	results []images.Result  // targets tartibida; chizilmagan rasm o'rnida Data nil
	took    time.Duration
}

// planDocFigures rasm g'oyasi bor boblardan n tasini teng oraliqda tanlaydi;
// chapters — hujjatdagi boblar soni (undan tashqaridagi g'oyalar olinmaydi).
func planDocFigures(ideas []*ai.FigureIdea, chapters, n int) *docFigures {
	var cand []int
	for i, f := range ideas {
		if f != nil && f.Prompt != "" && i < chapters {
			cand = append(cand, i)
		}
	}
	return &docFigures{ideas: ideas, targets: spreadPick(cand, n)}
}

// draw tanlangan boblar uchun rasmlarni dizayn uslubida chizadi (holati
// jarayon xabarida alohida qatorda ko'rsatiladi).
func (f *docFigures) draw(ctx context.Context, progress Progress, svc ImageService, imageStyle string) {
	reqs := make([]images.Request, len(f.targets))
	for k, i := range f.targets {
		prompt := f.ideas[i].Prompt
		if imageStyle != "" {
			prompt += ". Style: " + imageStyle
		}
		reqs[k] = images.Request{Prompt: prompt + ". No text, letters, numbers or logos in the image.", Shape: images.Landscape}
	}
	report, stop := progress.Stage(DocImageStage)
	report(0, len(reqs))
	started := time.Now()
	f.results = svc.Generate(ctx, reqs, report)
	stop()
	f.took = time.Since(started)
}

// place chizilgan rasmlarni hujjat boblariga (bob indeksi bo'yicha) izohi
// bilan joylaydi. Chizilmagan rasm o'rnida bob rasmsiz qoladi.
func (f *docFigures) place(log *zap.Logger, paper *generator.Paper) picturesInfo {
	info := picturesInfo{requested: len(f.targets)}
	if len(f.targets) == 0 {
		return info // rasm g'oyalari yo'q (ogohlantirish rasm tanlanganda yozilgan)
	}
	for k, r := range f.results {
		if r.Data == nil {
			continue
		}
		i := f.targets[k]
		if i >= len(paper.Chapters) {
			continue // generator rejadagidan kam bob qaytargan (OnOutline kontrakti buzilgan)
		}
		paper.Chapters[i].Figure = &generator.Figure{Data: r.Data, Caption: f.ideas[i].Caption}
		info.placed++
		if info.provider == "" {
			info.provider = r.Provider
		}
	}
	log.Info("figures generated", zap.Int("requested", info.requested), zap.Int("placed", info.placed),
		zap.String("provider", info.provider), zap.Duration("took", f.took))
	return info
}

// spreadPick list dan k ta elementni teng oraliqda tanlaydi (tartib saqlanadi).
func spreadPick(list []int, k int) []int {
	if k <= 0 || len(list) == 0 {
		return nil
	}
	if k >= len(list) {
		return list
	}
	out := make([]int, k)
	for j := range out {
		out[j] = list[(2*j+1)*len(list)/(2*k)]
	}
	return out
}

func toParagraphs(in []ai.Paragraph) []generator.Paragraph {
	out := make([]generator.Paragraph, len(in))
	for i, p := range in {
		out[i] = generator.Paragraph{Text: p.Text, Code: p.Code}
	}
	return out
}
