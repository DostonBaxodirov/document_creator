package ai

// presentation.go — taqdimot slaydlari generatsiyasi.
//
// Har bir slayd o'z maketiga (layout) ega: reja, punktlar, kartochkalar,
// bosqichlar, taqqoslash, faktlar, ta'rif yoki xulosa — va har bir element
// sarlavha hamda izohdan iborat. Shu tufayli slaydlar ham boy mazmunli,
// ham turli ko'rinishda chiqadi.
//
// Slaydlar soni kam bo'lsa (slidesPerRequest gacha) — bitta so'rov. Ko'p
// bo'lsa — avval reja (har bir slayd sarlavhasi, maketi va mazmuni), so'ng
// slaydlar guruhlab parallel yoziladi: bu katta taqdimotlarni modelning
// javob uzunligi chegarasiga sig'diradi.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/genai"
)

// Slayd maketlari — generator paketidagi Layout* nomlari bilan bir xil.
const (
	LayoutAgenda     = "agenda"
	LayoutBullets    = "bullets"
	LayoutCards      = "cards"
	LayoutComparison = "comparison"
	LayoutProcess    = "process"
	LayoutFacts      = "facts"
	LayoutDefinition = "definition"
	LayoutConclusion = "conclusion"
)

var slideLayouts = []string{
	LayoutAgenda, LayoutBullets, LayoutCards, LayoutComparison,
	LayoutProcess, LayoutFacts, LayoutDefinition, LayoutConclusion,
}

// layoutAliases — modellar ba'zan maket nomini o'zgacha yozadi.
var layoutAliases = map[string]string{
	"outline": LayoutAgenda, "plan": LayoutAgenda, "reja": LayoutAgenda,
	"list": LayoutBullets, "bullet": LayoutBullets, "points": LayoutBullets,
	"grid": LayoutCards, "card": LayoutCards, "columns": LayoutCards,
	"compare": LayoutComparison, "versus": LayoutComparison, "vs": LayoutComparison,
	"timeline": LayoutProcess, "steps": LayoutProcess, "stages": LayoutProcess,
	"stats": LayoutFacts, "numbers": LayoutFacts, "figures": LayoutFacts,
	"quote": LayoutDefinition, "term": LayoutDefinition,
	"summary": LayoutConclusion, "xulosa": LayoutConclusion,
}

// PresentationRequest — taqdimot generatsiyasi uchun kiruvchi ma'lumotlar.
type PresentationRequest struct {
	Topic      string
	SlideCount int // generatsiya qilinishi kerak bo'lgan MAZMUNLI slaydlar soni
	// OnProgress — katta taqdimotda reja va har bir slaydlar guruhi tayyor
	// bo'lganda chaqiriladi (ixtiyoriy; kichik taqdimotda chaqirilmaydi).
	OnProgress func(done, total int)
}

// SlideItem — slayddagi bitta element.
type SlideItem struct {
	Heading string   `json:"heading"`
	Text    string   `json:"text"`
	Value   string   `json:"value,omitempty"`  // "facts": katta raqam yoki qiymat
	Points  []string `json:"points,omitempty"` // "comparison": ichki punktlar
}

// Slide — bitta slayd.
type Slide struct {
	Layout    string      `json:"layout"`
	Title     string      `json:"title"`
	Lead      string      `json:"lead"` // sarlavha ostidagi kirish gapi
	Items     []SlideItem `json:"items"`
	Highlight string      `json:"highlight"` // asosiy fikr, ta'rif yoki yakuniy xulosa
	// Bullets — model sxemadan chetga chiqib eski formatda javob bersa (zaxira).
	Bullets []string `json:"bullets,omitempty"`
}

// Presentation — AI qaytargan taqdimot tarkibi.
type Presentation struct {
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle"`
	Slides   []Slide `json:"slides"`
}

// Slayd matni cheklovlari: slaydga sig'maydigan darajada uzun javoblarni qirqamiz.
const (
	slidesPerRequest  = 6
	maxSlideItems     = 8
	maxItemPoints     = 6
	maxTitleRunes     = 120
	maxSubtitleRunes  = 140
	maxLeadRunes      = 280
	maxHeadingRunes   = 100
	maxItemTextRunes  = 420
	maxValueRunes     = 16
	maxPointRunes     = 220
	maxHighlightRunes = 480
	maxFocusRunes     = 320
)

// GeneratePresentation slaydlarni yaratadi. Slaydlar soni kam bo'lsa qayta
// uriniladi; barcha urinishlarda kam bo'lsa — eng to'liq natija qaytariladi.
func (c *client) GeneratePresentation(ctx context.Context, req PresentationRequest) (*Presentation, error) {
	if strings.TrimSpace(req.Topic) == "" {
		return nil, errors.New("ai: mavzu bo'sh")
	}
	if req.SlideCount < 1 {
		return nil, fmt.Errorf("ai: slaydlar soni noto'g'ri: %d", req.SlideCount)
	}
	if req.SlideCount <= slidesPerRequest {
		return c.generateDeck(ctx, req)
	}
	return c.generateDeckInBatches(ctx, req)
}

// generateDeck butun taqdimotni bitta so'rov bilan yozadi.
func (c *client) generateDeck(ctx context.Context, req PresentationRequest) (*Presentation, error) {
	t := task{system: presentationSystemPrompt, user: buildDeckPrompt(req), schema: deckSchema()}
	return generateJSON(ctx, c, "presentation", t, req.SlideCount, func(raw string) (*Presentation, int, error) {
		pres, err := parsePresentation(raw)
		if err != nil {
			return nil, 0, err
		}
		pres.fit(req.SlideCount)
		return pres, len(pres.Slides), nil
	})
}

// generateDeckInBatches avval rejani, so'ng slaydlarni guruhlab parallel yozadi.
func (c *client) generateDeckInBatches(ctx context.Context, req PresentationRequest) (*Presentation, error) {
	n := req.SlideCount
	batches := splitBatches(n, slidesPerRequest)
	progress := &progressReporter{fn: req.OnProgress, total: 1 + len(batches)}
	progress.report()

	plan, err := generateJSON(ctx, c, "presentation_outline", task{
		system: presentationSystemPrompt,
		user:   buildDeckOutlinePrompt(req),
		schema: deckOutlineSchema(),
	}, n, func(raw string) (*deckOutline, int, error) {
		o, err := parseDeckOutline(raw)
		if err != nil {
			return nil, 0, err
		}
		o.fit(n)
		return o, len(o.Slides), nil
	})
	if err != nil {
		return nil, err
	}
	// Reja kamroq slayd qaytargan bo'lishi mumkin — guruhlarni qayta hisoblaymiz.
	batches = splitBatches(len(plan.Slides), slidesPerRequest)
	progress.setTotal(1 + len(batches))
	progress.advance()

	results := make([][]Slide, len(batches))
	err = forEachParallel(ctx, len(batches), c.parallel, func(ctx context.Context, i int) error {
		b := batches[i]
		want := b[1] - b[0]
		slides, err := generateJSON(ctx, c, "presentation_slides", task{
			system: presentationSystemPrompt,
			user:   buildSlidesBatchPrompt(req.Topic, plan, b[0], b[1]),
			schema: slidesBatchSchema(),
		}, want, func(raw string) ([]Slide, int, error) {
			s, err := parseSlides(raw)
			if err != nil {
				return nil, 0, err
			}
			if len(s) > want {
				s = s[:want]
			}
			return s, len(s), nil
		})
		if err != nil {
			return fmt.Errorf("slaydlar %d-%d: %w", b[0]+1, b[1], err)
		}
		results[i] = slides
		progress.advance()
		return nil
	})
	if err != nil {
		return nil, err
	}

	pres := &Presentation{Title: plan.Title, Subtitle: plan.Subtitle}
	for _, s := range results {
		pres.Slides = append(pres.Slides, s...)
	}
	if len(pres.Slides) == 0 {
		return nil, errors.New("ai: birorta ham slayd yozilmadi")
	}
	c.log.Info("AI presentation generated in batches",
		zap.Int("slides", len(pres.Slides)),
		zap.Int("requested", n),
		zap.Int("batches", len(batches)))
	return pres, nil
}

// splitBatches n ta slaydni ko'pi bilan size tadan teng guruhlarga bo'ladi:
// [[0,6], [6,12], ...] (yarim ochiq oraliqlar).
func splitBatches(n, size int) [][2]int {
	if n <= 0 {
		return nil
	}
	k := (n + size - 1) / size
	out := make([][2]int, 0, k)
	start := 0
	for i := range k {
		end := start + (n-start)/(k-i)
		out = append(out, [2]int{start, end})
		start = end
	}
	return out
}

// forEachParallel fn ni 0..n-1 uchun ko'pi bilan parallel tadan bajaradi.
// Birorta xato bo'lsa, qolganlari to'xtatiladi va birinchi xato qaytariladi.
func forEachParallel(ctx context.Context, n, parallel int, fn func(ctx context.Context, i int) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		sem      = make(chan struct{}, max(1, parallel))
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	fail := func(err error) {
		errOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Goroutine ichidagi panic tashqi recover'ga yetib bormaydi.
			defer func() {
				if r := recover(); r != nil {
					fail(fmt.Errorf("ai: panic: %v", r))
				}
			}()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if err := fn(ctx, i); err != nil {
				fail(err)
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// ---------------------------------------------------------------------------
// Promptlar
// ---------------------------------------------------------------------------

const presentationSystemPrompt = `You are an expert academic writer and presentation designer who prepares rich, informative presentation slides for university students in Uzbekistan.

Strict output rules:
- Respond with ONE valid JSON object and nothing else: no markdown fences, no comments, no extra text.
- Write ALL content in the same language as the topic. If the topic is in Uzbek, use the Uzbek Latin alphabet with correct letters (o', g', sh, ch).
- Content must be accurate, substantive, neutral and academic: concrete facts, definitions, mechanisms, causes and effects, real examples. Avoid vague, generic filler sentences.
- Never invent statistics, quotations, URLs or bibliographic references. Use only numbers and dates that are well established.
- Do not put markdown (*, **, #), emojis, bullet symbols or numbering at the start of any text field.
- Slide titles are short and specific (at most 8 words). Never repeat the same slide title.
` + finalTextRules

// layoutGuide — har bir maket uchun talablar (barcha taqdimot promptlarida).
const layoutGuide = `Slide layouts (the "layout" field) and the content each one needs:
- "agenda": the outline. 4-6 items: "heading" = section name (2-6 words), "text" = what the section covers (6-14 words).
- "bullets": 3-5 items: "heading" = a key idea (2-6 words), "text" = its explanation with a concrete detail or example (20-35 words). Optional "highlight": the main insight of the slide (15-30 words).
- "cards": 3-4 parallel aspects, types, functions or directions: "heading" (2-5 words), "text" (20-35 words).
- "process": 3-5 sequential steps, stages or historical periods in order: "heading" (2-5 words, may start with a date or period), "text" (15-25 words).
- "comparison": exactly 2 items contrasting two sides (advantages vs disadvantages, before vs after, two approaches): "heading" (1-4 words), "points" = 3-5 points (6-15 words each), "text" may be empty. "highlight": a balanced one-sentence conclusion (15-30 words).
- "facts": 3-4 key figures: "value" = a short, well-established number, date, percentage or quantity (at most 10 characters, e.g. "1991", "75%", "3 ta"), "heading" = what it denotes (2-5 words), "text" = context (10-20 words). Use only figures you are certain about; if the topic has no such figures, choose another layout.
- "definition": "highlight" = a precise definition of the key concept (25-45 words); 2-4 items with its key features: "heading" (2-4 words), "text" (10-20 words).
- "conclusion": 3-5 key takeaways: "heading" (2-6 words), "text" (12-25 words); "highlight" = a final concluding statement (15-30 words).
Every slide except the agenda has "lead": one sentence (12-25 words) that introduces the slide's main idea and does not repeat the items.
Fields a layout does not use are left empty.`

const slideJSONShape = `{"layout": string, "title": string, "lead": string, "items": [{"value": string, "heading": string, "text": string, "points": [string]}], "highlight": string}`

// structureRules — taqdimot tuzilishi (n — mazmunli slaydlar soni).
func structureRules(n int) string {
	var b strings.Builder
	b.WriteString("Structure:\n")
	switch {
	case n >= 3:
		b.WriteString("1. Slide 1 is the outline: layout \"agenda\", titled \"Reja\" (use the equivalent word if the topic is not in Uzbek, e.g. \"План\" or \"Outline\"); its items name the main sections that follow.\n")
		fmt.Fprintf(&b, "2. Slides 2-%d are the main sections, covering the topic logically from fundamentals to details; each slide focuses on one idea and they do not overlap. "+
			"A \"definition\" slide fits well right after the outline.\n", n-1)
		fmt.Fprintf(&b, "3. Slide %d is the conclusion: layout \"conclusion\", titled \"Xulosa\" (or its equivalent).\n", n)
		if n >= 6 {
			b.WriteString("Choose for each main slide the layout that best fits its content and vary them: use at least 4 different layouts, and never the same layout on more than 2 consecutive slides.\n")
		} else {
			b.WriteString("Choose for each main slide the layout that best fits its content; avoid repeating the same layout.\n")
		}
	case n == 2:
		b.WriteString("1. Slide 1 explains the core of the topic (layout \"definition\" or \"bullets\").\n")
		b.WriteString("2. Slide 2 is the conclusion: layout \"conclusion\", titled \"Xulosa\" (or its equivalent).\n")
	default:
		b.WriteString("1. A single slide giving a complete overview of the topic (layout \"bullets\" with a \"highlight\").\n")
	}
	return b.String()
}

// buildDeckPrompt — butun taqdimot bitta so'rovda.
func buildDeckPrompt(req PresentationRequest) string {
	n := req.SlideCount
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	fmt.Fprintf(&b, "Number of slides in \"slides\": exactly %d\n\n", n)
	b.WriteString(structureRules(n))
	b.WriteString("\n" + layoutGuide + "\n\n")
	b.WriteString(`JSON shape: {"title": string, "subtitle": string, "slides": [` + slideJSONShape + `, ...]}` + "\n")
	b.WriteString(`The top-level "title" is a polished version of the topic; "subtitle" is a short phrase (4-10 words) that clarifies the scope of the presentation.`)
	return b.String()
}

// buildDeckOutlinePrompt — katta taqdimot rejasi.
func buildDeckOutlinePrompt(req PresentationRequest) string {
	n := req.SlideCount
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	fmt.Fprintf(&b, "Plan a presentation with exactly %d slides (do not write their content yet).\n\n", n)
	b.WriteString(structureRules(n))
	b.WriteString("\n" + layoutGuide + "\n\n")
	b.WriteString(`For each slide give "title", "layout" and "focus": one sentence (12-25 words) naming exactly what the slide will cover — specific sub-topics, facts or examples — so that slides do not overlap.` + "\n")
	b.WriteString(`JSON shape: {"title": string, "subtitle": string, "slides": [{"title": string, "layout": string, "focus": string}, ...]}` + "\n")
	b.WriteString(`The top-level "title" is a polished version of the topic; "subtitle" is a short phrase (4-10 words) that clarifies the scope of the presentation.`)
	return b.String()
}

// buildSlidesBatchPrompt — rejadagi [from, to) slaydlar mazmuni.
func buildSlidesBatchPrompt(topic string, plan *deckOutline, from, to int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %q\n\nPresentation plan (all slides):\n", strings.TrimSpace(topic))
	for i, s := range plan.Slides {
		fmt.Fprintf(&b, "%d. [%s] %s — %s\n", i+1, s.Layout, s.Title, s.Focus)
	}
	fmt.Fprintf(&b, "\nWrite the full content of slides %d-%d only (%d slides), exactly as planned: same order, titles and layouts. "+
		"Stay within each slide's focus and do not repeat what other slides cover.\n\n", from+1, to, to-from)
	b.WriteString(layoutGuide + "\n\n")
	b.WriteString(`JSON shape: {"slides": [` + slideJSONShape + `, ...]}`)
	return b.String()
}

// ---------------------------------------------------------------------------
// JSON sxemalar (Gemini structured output)
// ---------------------------------------------------------------------------

func slideSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"layout": {Type: genai.TypeString, Format: "enum", Enum: slideLayouts},
			"title":  {Type: genai.TypeString},
			"lead":   {Type: genai.TypeString},
			"items": {Type: genai.TypeArray, Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"value":   {Type: genai.TypeString},
					"heading": {Type: genai.TypeString},
					"text":    {Type: genai.TypeString},
					"points":  stringArraySchema(),
				},
				Required:         []string{"heading", "text"},
				PropertyOrdering: []string{"value", "heading", "text", "points"},
			}},
			"highlight": {Type: genai.TypeString},
		},
		Required:         []string{"layout", "title", "lead", "items"},
		PropertyOrdering: []string{"layout", "title", "lead", "items", "highlight"},
	}
}

func deckSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"title":    {Type: genai.TypeString},
			"subtitle": {Type: genai.TypeString},
			"slides":   {Type: genai.TypeArray, Items: slideSchema()},
		},
		Required:         []string{"title", "subtitle", "slides"},
		PropertyOrdering: []string{"title", "subtitle", "slides"},
	}
}

func slidesBatchSchema() *genai.Schema {
	return &genai.Schema{
		Type:       genai.TypeObject,
		Properties: map[string]*genai.Schema{"slides": {Type: genai.TypeArray, Items: slideSchema()}},
		Required:   []string{"slides"},
	}
}

func deckOutlineSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"title":    {Type: genai.TypeString},
			"subtitle": {Type: genai.TypeString},
			"slides": {Type: genai.TypeArray, Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"title":  {Type: genai.TypeString},
					"layout": {Type: genai.TypeString, Format: "enum", Enum: slideLayouts},
					"focus":  {Type: genai.TypeString},
				},
				Required:         []string{"title", "layout", "focus"},
				PropertyOrdering: []string{"title", "layout", "focus"},
			}},
		},
		Required:         []string{"title", "subtitle", "slides"},
		PropertyOrdering: []string{"title", "subtitle", "slides"},
	}
}

// ---------------------------------------------------------------------------
// Parse va normalizatsiya
// ---------------------------------------------------------------------------

// parsePresentation AI javobini Presentation ga aylantiradi va tekshiradi.
func parsePresentation(raw string) (*Presentation, error) {
	var pres Presentation
	if err := decodeJSON(raw, &pres, &pres.Slides); err != nil {
		return nil, err
	}
	pres.Title = truncateRunes(stripMeta(cleanText(pres.Title)), maxTitleRunes)
	pres.Subtitle = truncateRunes(strings.TrimRight(stripMeta(cleanText(pres.Subtitle)), "."), maxSubtitleRunes)
	pres.Slides = normalizeSlides(pres.Slides)
	if len(pres.Slides) == 0 {
		return nil, errors.New("javobda birorta ham to'liq slayd yo'q")
	}
	return &pres, nil
}

// parseSlides guruh javobini ({"slides": [...]}) o'qiydi.
func parseSlides(raw string) ([]Slide, error) {
	var out struct {
		Slides []Slide `json:"slides"`
	}
	if err := decodeJSON(raw, &out, &out.Slides); err != nil {
		return nil, err
	}
	slides := normalizeSlides(out.Slides)
	if len(slides) == 0 {
		return nil, errors.New("javobda birorta ham to'liq slayd yo'q")
	}
	return slides, nil
}

// normalizeSlides matnlarni tozalaydi va to'liq bo'lmagan slaydlarni tashlab yuboradi.
func normalizeSlides(in []Slide) []Slide {
	out := in[:0]
	for _, s := range in {
		if s.normalize() {
			out = append(out, s)
		}
	}
	return out
}

// normalize matnlarni tozalaydi (xizmat gaplari ham — sanitize.go), bo'sh
// elementlarni olib tashlaydi va uzunlik cheklovlarini qo'llaydi.
// Slayd ishlatishga yaroqli bo'lsa true qaytaradi.
func (s *Slide) normalize() bool {
	s.Layout = normalizeLayout(s.Layout)
	s.Title = truncateRunes(stripMeta(cleanText(s.Title)), maxTitleRunes)
	s.Lead = truncateRunes(stripMeta(cleanText(s.Lead)), maxLeadRunes)
	s.Highlight = truncateRunes(stripMeta(cleanText(s.Highlight)), maxHighlightRunes)

	items := make([]SlideItem, 0, len(s.Items))
	for _, it := range s.Items {
		it.Heading = truncateRunes(strings.TrimRight(stripMeta(cleanText(it.Heading)), ":;,"), maxHeadingRunes)
		it.Text = truncateRunes(stripMeta(cleanText(it.Text)), maxItemTextRunes)
		it.Value = truncateRunes(cleanText(it.Value), maxValueRunes)
		points := make([]string, 0, len(it.Points))
		for _, p := range it.Points {
			if p = stripMeta(cleanText(p)); p != "" {
				points = append(points, truncateRunes(p, maxPointRunes))
			}
			if len(points) == maxItemPoints {
				break
			}
		}
		it.Points = points
		if it.Heading == "" && it.Text == "" && len(it.Points) == 0 {
			continue
		}
		items = append(items, it)
		if len(items) == maxSlideItems {
			break
		}
	}
	// Eski format: faqat punktlar ro'yxati.
	if len(items) == 0 {
		for _, b := range s.Bullets {
			if b = stripMeta(cleanText(b)); b != "" {
				items = append(items, SlideItem{Text: truncateRunes(b, maxItemTextRunes)})
			}
			if len(items) == maxSlideItems {
				break
			}
		}
	}
	s.Items, s.Bullets = items, nil
	return s.Title != "" && (len(s.Items) > 0 || s.Highlight != "")
}

func normalizeLayout(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	for _, known := range slideLayouts {
		if l == known {
			return l
		}
	}
	if a, ok := layoutAliases[l]; ok {
		return a
	}
	return LayoutBullets
}

// fit slaydlar sonini kerakli miqdorga keltiradi. Ortiqcha bo'lsa — oxirgi
// (xulosa) slaydni saqlagan holda o'rtadagilarni qisqartiradi.
func (p *Presentation) fit(want int) {
	p.Slides = fitSlides(p.Slides, want)
}

func fitSlides[T any](s []T, want int) []T {
	if len(s) <= want {
		return s
	}
	if want == 1 {
		return s[:1]
	}
	last := s[len(s)-1]
	return append(s[:want-1], last)
}

// ---------------------------------------------------------------------------
// Katta taqdimot rejasi
// ---------------------------------------------------------------------------

type plannedSlide struct {
	Title  string `json:"title"`
	Layout string `json:"layout"`
	Focus  string `json:"focus"`
}

type deckOutline struct {
	Title    string         `json:"title"`
	Subtitle string         `json:"subtitle"`
	Slides   []plannedSlide `json:"slides"`
}

func parseDeckOutline(raw string) (*deckOutline, error) {
	var o deckOutline
	if err := decodeJSON(raw, &o, &o.Slides); err != nil {
		return nil, err
	}
	o.Title = truncateRunes(stripMeta(cleanText(o.Title)), maxTitleRunes)
	o.Subtitle = truncateRunes(strings.TrimRight(stripMeta(cleanText(o.Subtitle)), "."), maxSubtitleRunes)
	slides := o.Slides[:0]
	for _, s := range o.Slides {
		s.Title = truncateRunes(cleanText(s.Title), maxTitleRunes)
		s.Layout = normalizeLayout(s.Layout)
		s.Focus = truncateRunes(cleanText(s.Focus), maxFocusRunes)
		if s.Title != "" {
			slides = append(slides, s)
		}
	}
	o.Slides = slides
	if len(o.Slides) == 0 {
		return nil, errors.New("rejada birorta ham slayd yo'q")
	}
	return &o, nil
}

func (o *deckOutline) fit(want int) { o.Slides = fitSlides(o.Slides, want) }
