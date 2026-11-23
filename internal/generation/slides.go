package generation

// slides.go — taqdimot (.pptx): AI matni, rasmli dizaynda rasmlar va AI
// maxsus dizaynda (custom) mavzuga mos dizayn konsepsiyasi.

import (
	"context"
	"fmt"
	"html"
	"math"
	"runtime/debug"
	"strconv"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/lang"
)

// buildSlides taqdimotni yaratadi: titul va yakuniy slaydlarni generator
// o'zi chizadi, AI faqat mazmunli slaydlarni yozadi. Katta taqdimot bir necha
// so'rov bilan yoziladi — jarayon holati progress orqali ko'rsatiladi.
//
// AI maxsus dizaynda avval tarif modeli mavzuga mos dizayn konsepsiyasini
// yaratadi, slaydlarni ham shu model yozadi, rasmlar esa tarif sifatida
// slaydlarning bir qismiga chiziladi.
//
// Rasmli dizaynda katta taqdimot rasmlari reja tayyor bo'lishi bilan
// (OnOutline), slaydlar matni yozilishini kutmasdan chiziladi.
func (s *Service) buildSlides(ctx context.Context, progress Progress, o Order, log *zap.Logger) (*Output, error) {
	b := s.slides
	content := b.ContentSlides(o.Volume)
	li := lang.Get(o.Language)

	var (
		gen      = s.ai
		spec     *generator.CustomSpec
		aiDesign *ai.Design
		plan     = picturePlan{svc: s.images}
		tariff   Tariff
	)
	custom := o.custom()
	if custom {
		var ok bool
		if tariff, ok = s.tariffs[o.Tariff]; !ok {
			return nil, fmt.Errorf("AI maxsus dizayn tarifi topilmadi: %q", o.Tariff)
		}
		gen = tariff.AI
		var err error
		if aiDesign, spec, err = customDesign(ctx, progress, log, o, tariff, li.Prompt); err != nil {
			return nil, err
		}
		plan = picturePlan{svc: tariff.Images, limit: customImageCount(o.Volume, tariff.ImagePercent)}
	}
	plan.design = generator.DesignFor(o.Design, spec)
	photos := plan.design.Photos() && plan.svc != nil && plan.svc.Available()

	// Prompt tanlangan dizayn tuzilishiga moslanadi: AI dizayn chizadigan
	// maketlardan tanlaydi, uning imzo maketlarini ishlatadi va matn hajmini
	// dizayndagi joyga moslaydi.
	structure := plan.design.Structure()
	report, stopProgress := progress.Stage(TextStage("Slaydlar yozilmoqda..."))
	req := ai.PresentationRequest{
		Topic: o.Topic, SlideCount: content, OnProgress: report, Images: photos,
		Layouts: structure.Layouts, Signature: structure.Signature, Compact: structure.Compact,
		Language: li.Prompt, Requirements: o.Requirements,
		OutlineTitle: li.Labels.Plan, ConclusionTitle: li.Labels.Conclusion,
		Custom: aiDesign.Brief(),
	}
	// Katta taqdimotda rasm tavsiflari rejada keladi: rasmlar reja tayyor
	// bo'lishi bilan, slaydlar matni yozilishi bilan parallel chiziladi.
	var drawn *slidePictures // nil — chizish boshlanmagan (OnOutline chaqirilmagan)
	if photos {
		req.OnOutline = func(op ai.OutlinePlan) {
			if drawn == nil {
				drawn = planOutlinePictures(op, plan)
				drawn.start(ctx, progress, log, plan.svc)
			}
		}
	}
	pres, err := gen.GeneratePresentation(ctx, req)
	stopProgress()
	if err != nil {
		if drawn != nil {
			drawn.stop() // matnsiz rasmlar kerak emas
		}
		return nil, fmt.Errorf("%w: %w", ErrAI, err)
	}

	deck := generator.Deck{
		Topic:       o.Topic,
		Subtitle:    pres.Subtitle,
		WorkType:    workTypeTitle(o.WorkType, li),
		StudentName: o.StudentName,
		University:  o.University,
		TeacherName: o.TeacherName,
		Year:        strconv.Itoa(time.Now().Year()),
		Theme:       o.Design,
		Custom:      spec,
		Lang:        string(li.Code),
		Slides:      make([]generator.Slide, 0, len(pres.Slides)),
	}
	for _, sl := range pres.Slides {
		items := make([]generator.Item, len(sl.Items))
		for i, it := range sl.Items {
			items[i] = generator.Item{Heading: it.Heading, Text: it.Text, Value: it.Value, Points: it.Points}
		}
		deck.Slides = append(deck.Slides, generator.Slide{
			Layout: sl.Layout, Title: sl.Title, Lead: sl.Lead, Items: items, Highlight: sl.Highlight,
		})
	}
	var pics picturesInfo
	if photos {
		if drawn != nil {
			pics = drawn.placeByPlan(log, pres, &deck)
		} else {
			// OnOutline chaqirilmagan (kichik taqdimot — rejasiz) — rasmlar
			// matndan keyin, slaydlardagi tavsiflar bo'yicha chiziladi.
			pics = addPictures(ctx, progress, log, pres, &deck, plan)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	path, err := b.Build(ctx, deck)
	if err != nil {
		return nil, fmt.Errorf("pptx: %w", err)
	}

	total := len(deck.Slides) + (o.Volume - content) // + titul va yakuniy slayd
	designName := o.Design
	if t, ok := generator.FindTheme(o.Design); ok {
		designName = t.Emoji + " " + t.Name
	}
	if custom {
		designName = customCaptionName(aiDesign.Name, o.Tariff)
	}
	caption := fmt.Sprintf("📎 <b>%s</b>\n%s · %d ta slayd · %s",
		html.EscapeString(o.Topic), html.EscapeString(WorkTypeName(o.WorkType)), total, html.EscapeString(designName))
	if pics.placed > 0 {
		caption += fmt.Sprintf(" · 🖼 %d ta rasm", pics.placed)
	}
	summary := fmt.Sprintf("%d ta slayd · %s", total, designName)
	fields := []zap.Field{zap.Int("slides", total), zap.String("design", o.Design)}
	if custom {
		summary += fmt.Sprintf(" · %s, %s:%s", tariff.TextModel, tariff.ImageModel, tariff.ImageQuality)
		fields = append(fields, zap.String("tariff", o.Tariff), zap.String("design_name", aiDesign.Name))
	}
	if photos {
		provider := "—"
		if pics.provider != "" {
			provider = pics.provider
		}
		summary += fmt.Sprintf(" · 🖼 %d/%d rasm (%s)", pics.placed, pics.requested, provider)
		fields = append(fields,
			zap.Int("images", pics.placed),
			zap.Int("images_requested", pics.requested),
			zap.String("image_provider", pics.provider))
	}
	return &Output{
		Path:      path,
		FileName:  documentFileName(o.Topic, ".pptx"),
		MIME:      pptxMIME,
		Caption:   caption,
		Summary:   summary,
		LogFields: fields,
	}, nil
}

// picturesInfo — taqdimotga (yoki Word ishga) qo'shilgan rasmlar haqida (log va izoh uchun).
type picturesInfo struct {
	requested, placed int
	provider          string
}

// customImageCount taqdimotdagi AI rasmlar soni (titul rasmi bilan): slaydlar
// sonining percent foizi, kamida 1 (titul).
func customImageCount(slides, percent int) int {
	return max(1, int(math.Round(float64(slides)*float64(percent)/100)))
}

// designStep dizayn konsepsiyasini yaratish bosqichini bajaradi: holatini
// jarayon xabarida ko'rsatadi va natijani (fields) ketgan vaqt bilan logga yozadi.
func designStep[D any](progress Progress, log *zap.Logger, msg string, gen func() (D, error), fields func(D) []zap.Field) (D, error) {
	report, stop := progress.Stage(DesignStage)
	report(0, 1)
	started := time.Now()
	d, err := gen()
	stop()
	if err != nil {
		var zero D
		return zero, fmt.Errorf("%w: dizayn: %w", ErrAI, err)
	}
	log.Info(msg, append(fields(d), zap.Duration("took", time.Since(started)))...)
	return d, nil
}

// customDesign AI dan dizayn konsepsiyasini oladi va uni generator
// spetsifikatsiyasiga aylantiradi.
func customDesign(ctx context.Context, progress Progress, log *zap.Logger, o Order, t Tariff, language string) (*ai.Design, *generator.CustomSpec, error) {
	d, err := designStep(progress, log, "custom design generated", func() (*ai.Design, error) {
		return t.AI.GenerateDesign(ctx, ai.DesignRequest{Topic: o.Topic, Language: language, Requirements: o.Requirements})
	}, func(d *ai.Design) []zap.Field {
		return []zap.Field{zap.String("name", d.Name), zap.String("mood", d.Mood), zap.String("cover", d.Cover),
			zap.String("header", d.Header), zap.String("background", d.Colors.Background), zap.String("primary", d.Colors.Primary)}
	})
	if err != nil {
		return nil, nil, err
	}
	spec := d.Spec()
	return d, &spec, nil
}

// picturePlan — taqdimotga qaysi rasmlar qanday chizilishi.
type picturePlan struct {
	design generator.Design
	svc    ImageService
	// limit > 0 — rasmlar soni chegarasi (titul rasmi bilan; AI maxsus
	// dizayn): rasmlar slaydlarga teng taqsimlanadi va kvadrat chiziladi.
	// 0 — rasm sig'adigan barcha slaydlarga (tayyor rasmli dizaynlar).
	limit int
}

// coverShape titul rasmi shakli: AI maxsus dizaynda — kvadrat, aks holda yotiq.
func (p picturePlan) coverShape() images.Shape {
	if p.limit > 0 {
		return images.Square
	}
	return images.Landscape
}

// customPhotoTargets AI maxsus dizaynda rasm qo'yiladigan slaydlar:
// rasm tavsifi bor slaydlardan n tasi (dizayn tanlaydi — yon panelga
// sig'adiganlari ustuvor, teng oraliqda).
func customPhotoTargets(design generator.Design, pres *ai.Presentation, deck *generator.Deck, n int) []int {
	var (
		cand  []generator.Slide
		index []int
	)
	for i, sl := range pres.Slides {
		if sl.Image != "" && i < len(deck.Slides) {
			cand = append(cand, deck.Slides[i])
			index = append(index, i)
		}
	}
	picked := design.PhotoSlides(cand, n)
	out := make([]int, len(picked))
	for k, i := range picked {
		out[k] = index[i]
	}
	return out
}

// addPictures AI yozgan rasm tavsiflari bo'yicha rasmlarni chizdiradi va
// taqdimotga joylaydi: titul (va yakuniy) slayd uchun bitta rasm va
// mazmunli slaydlar uchun rasmlar. Tayyor rasmli dizaynda — rasm sig'adigan
// barcha slaydlarga (yon panel uchun tik, banner uchun yotiq rasm); AI maxsus
// dizaynda — plan.limit ta kvadrat rasm, slaydlarga teng taqsimlangan.
// Chizilmagan rasm o'rnida slayd rasmsiz (titulda — bezakli fon bilan) chiqadi.
//
// Matn yozilgandan keyin chaqiriladi — generator OnOutline ni chaqirmagan
// holat uchun (katta taqdimotda rasmlar reja bo'yicha, matn bilan parallel
// chiziladi: planOutlinePictures).
func addPictures(ctx context.Context, progress Progress, log *zap.Logger, pres *ai.Presentation, deck *generator.Deck, plan picturePlan) picturesInfo {
	design := plan.design
	p := &slidePictures{}

	cover := pres.Image
	for i := 0; cover == "" && i < len(pres.Slides); i++ {
		cover = pres.Slides[i].Image
	}
	if cover != "" {
		p.add(design, cover, plan.coverShape(), -1)
	}
	if plan.limit > 0 {
		for _, i := range customPhotoTargets(design, pres, deck, plan.limit-len(p.reqs)) {
			p.add(design, pres.Slides[i].Image, images.Square, i)
		}
	} else {
		for i, sl := range pres.Slides {
			if sl.Image == "" || i >= len(deck.Slides) {
				continue
			}
			// Tepadagi banner uchun — yotiq rasm, yon panel uchun — tik rasm.
			slideShape := images.Portrait
			if design.PhotoWide(deck.Slides[i]) {
				slideShape = images.Landscape
			}
			p.add(design, sl.Image, slideShape, i)
		}
	}
	if len(p.reqs) == 0 {
		log.Warn("AI returned no image descriptions")
		return picturesInfo{}
	}
	p.draw(ctx, progress, plan.svc)
	return p.place(log, deck, func(i int) (int, bool) { return i, true })
}

// slidePictures — taqdimotga chiziladigan rasmlar: so'rovlar, ular
// qo'yiladigan joylar va natijalar.
type slidePictures struct {
	reqs    []images.Request
	targets []int           // reqs tartibida: -1 — titul rasmi, aks holda slayd indeksi
	results []images.Result // reqs tartibida; chizilmagan rasm o'rnida Data nil
	took    time.Duration

	// Fonda chizishda (start): cancel chizishni to'xtatadi, done — tugadi.
	cancel context.CancelFunc
	done   chan struct{}
}

// add rasm so'rovini qo'shadi (prompt AI tavsifi va dizayn uslubidan).
func (p *slidePictures) add(design generator.Design, desc string, shape images.Shape, target int) {
	p.reqs = append(p.reqs, images.Request{Prompt: design.PhotoPrompt(desc), Shape: shape})
	p.targets = append(p.targets, target)
}

// planOutlinePictures katta taqdimot rejasidagi rasm tavsiflaridan rasm
// so'rovlarini tuzadi (target — reja slaydi indeksi): titul rasmi va tayyor
// rasmli dizaynda — tavsifi bor barcha reja slaydlari (shakli maket
// bo'yicha), AI maxsus dizaynda — plan.limit ta kvadrat rasm, slaydlarga
// teng taqsimlangan. Slaydlar matni hali yozilmagan — rasm shakli va
// rasmli slaydlar faqat maketga qarab tanlanadi.
func planOutlinePictures(op ai.OutlinePlan, plan picturePlan) *slidePictures {
	design := plan.design
	p := &slidePictures{}

	cover := op.CoverImage
	for i := 0; cover == "" && i < len(op.Slides); i++ {
		cover = op.Slides[i].Image
	}
	if cover != "" {
		p.add(design, cover, plan.coverShape(), -1)
	}
	if plan.limit > 0 {
		var (
			index   []int // tavsifi bor reja slaydlari
			layouts []string
		)
		for i, sl := range op.Slides {
			if sl.Image != "" {
				index = append(index, i)
				layouts = append(layouts, sl.Layout)
			}
		}
		for _, k := range design.PhotoSlidesByLayout(layouts, plan.limit-len(p.reqs)) {
			p.add(design, op.Slides[index[k]].Image, images.Square, index[k])
		}
	} else {
		for i, sl := range op.Slides {
			if sl.Image == "" {
				continue
			}
			// Tepadagi banner uchun — yotiq rasm, yon panel uchun — tik rasm.
			slideShape := images.Portrait
			if design.PhotoWideLayout(sl.Layout) {
				slideShape = images.Landscape
			}
			p.add(design, sl.Image, slideShape, i)
		}
	}
	return p
}

// start rasmlarni fonda chiza boshlaydi (tez qaytadi — OnOutline ichida
// chaqiriladi). Natijani placeByPlan kutadi; stop chizishni to'xtatadi.
func (p *slidePictures) start(ctx context.Context, progress Progress, log *zap.Logger, svc ImageService) {
	p.done = make(chan struct{})
	if len(p.reqs) == 0 {
		log.Warn("AI returned no image descriptions")
		close(p.done)
		return
	}
	ctx, p.cancel = context.WithCancel(ctx)
	go func() {
		defer close(p.done)
		defer p.cancel()
		// Goroutine'dagi panic butun botni to'xtatib qo'yardi.
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered in slide pictures", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
			}
		}()
		p.draw(ctx, progress, svc)
	}()
}

// stop fondagi chizishni to'xtatadi va tugashini kutadi.
func (p *slidePictures) stop() {
	if p.cancel != nil {
		p.cancel()
	}
	<-p.done
}

// draw rasmlarni chizadi (holati jarayon xabarida alohida qatorda ko'rsatiladi).
func (p *slidePictures) draw(ctx context.Context, progress Progress, svc ImageService) {
	report, stop := progress.Stage(ImageStage)
	defer stop()
	report(0, len(p.reqs))
	started := time.Now()
	p.results = svc.Generate(ctx, p.reqs, report)
	p.took = time.Since(started)
}

// placeByPlan fonda chizilgan rasmlar tugashini kutadi va ularni slaydlarga
// Slide.PlanIndex bo'yicha joylaydi. Generator yozmagan reja slaydining
// rasmi tashlanadi.
func (p *slidePictures) placeByPlan(log *zap.Logger, pres *ai.Presentation, deck *generator.Deck) picturesInfo {
	<-p.done
	if len(p.reqs) == 0 {
		return picturesInfo{} // ogohlantirish start da yozilgan
	}
	slideOf := make(map[int]int, len(pres.Slides)) // reja slaydi → deck.Slides indeksi
	for i, sl := range pres.Slides {
		if _, ok := slideOf[sl.PlanIndex]; !ok && i < len(deck.Slides) {
			slideOf[sl.PlanIndex] = i
		}
	}
	return p.place(log, deck, func(j int) (int, bool) {
		i, ok := slideOf[j]
		return i, ok
	})
}

// place chizilgan rasmlarni taqdimotga joylaydi: slide target ni
// deck.Slides indeksiga aylantiradi (false — slayd yo'q, rasm tashlanadi).
// Chizilmagan rasm o'rnida slayd rasmsiz (titulda — bezakli fon bilan) chiqadi.
func (p *slidePictures) place(log *zap.Logger, deck *generator.Deck, slide func(target int) (int, bool)) picturesInfo {
	info := picturesInfo{requested: len(p.reqs)}
	for k, r := range p.results {
		if r.Data == nil {
			continue
		}
		i := -1
		if p.targets[k] >= 0 {
			var ok bool
			if i, ok = slide(p.targets[k]); !ok {
				continue
			}
		}
		pic, err := generator.NewPicture(r.Data)
		if err != nil {
			log.Warn("generated image rejected", zap.Error(err))
			continue
		}
		if i < 0 {
			deck.CoverImage = pic
		} else {
			deck.Slides[i].Image = pic
		}
		info.placed++
		info.provider = r.Provider
	}
	log.Info("images generated",
		zap.Int("placed", info.placed),
		zap.Int("requested", info.requested),
		zap.String("provider", info.provider),
		zap.Duration("took", p.took))
	return info
}
