package ai

// presentation.go — taqdimot slaydlari generatsiyasi.
//
// Har bir slayd o'z maketiga (layout) ega: reja, punktlar, kartochkalar,
// bosqichlar, taqqoslash, faktlar, ta'rif, xulosa, vaqt chizig'i, piramida,
// matritsa, sikl, markaziy tushuncha yoki tezis — va har bir element sarlavha
// hamda izohdan iborat. Shu tufayli slaydlar ham boy mazmunli, ham turli
// ko'rinishda chiqadi.
//
// Prompt tanlangan dizayn tuzilishiga moslanadi (PresentationRequest.Layouts,
// Signature, Compact): AI faqat dizayn chizadigan maketlardan tanlaydi,
// dizaynning imzo maketlarini albatta ishlatadi va matn hajmini dizayndagi
// joyga moslaydi (katta shriftli dizaynlarda qisqaroq).
//
// Slaydlar soni kam bo'lsa (slidesPerRequest gacha) — bitta so'rov. Ko'p
// bo'lsa — avval reja (har bir slayd sarlavhasi, maketi va mazmuni), so'ng
// slaydlar guruhlab parallel yoziladi: bu katta taqdimotlarni modelning
// javob uzunligi chegarasiga sig'diradi.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
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
	LayoutTimeline   = "timeline"
	LayoutPyramid    = "pyramid"
	LayoutMatrix     = "matrix"
	LayoutCycle      = "cycle"
	LayoutHub        = "hub"
	LayoutStatement  = "statement"
)

// slideLayouts — barcha maketlar (generator ularning hammasini chiza oladi).
var slideLayouts = []string{
	LayoutAgenda, LayoutBullets, LayoutCards, LayoutComparison,
	LayoutProcess, LayoutFacts, LayoutDefinition, LayoutConclusion,
	LayoutTimeline, LayoutPyramid, LayoutMatrix, LayoutCycle, LayoutHub, LayoutStatement,
}

// layoutAliases — modellar ba'zan maket nomini o'zgacha yozadi.
var layoutAliases = map[string]string{
	"outline": LayoutAgenda, "plan": LayoutAgenda, "reja": LayoutAgenda,
	"list": LayoutBullets, "bullet": LayoutBullets, "points": LayoutBullets,
	"grid": LayoutCards, "card": LayoutCards, "columns": LayoutCards,
	"compare": LayoutComparison, "versus": LayoutComparison, "vs": LayoutComparison,
	"steps": LayoutProcess, "stages": LayoutProcess,
	"stats": LayoutFacts, "numbers": LayoutFacts, "figures": LayoutFacts,
	"term":    LayoutDefinition,
	"summary": LayoutConclusion, "xulosa": LayoutConclusion,
	"chronology": LayoutTimeline, "history": LayoutTimeline, "dates": LayoutTimeline,
	"hierarchy": LayoutPyramid, "levels": LayoutPyramid, "triangle": LayoutPyramid,
	"swot": LayoutMatrix, "quadrant": LayoutMatrix, "quadrants": LayoutMatrix, "2x2": LayoutMatrix,
	"loop": LayoutCycle, "circle": LayoutCycle, "circular": LayoutCycle,
	"mindmap": LayoutHub, "radial": LayoutHub, "spokes": LayoutHub, "center": LayoutHub,
	"quote": LayoutStatement, "thesis": LayoutStatement, "key-idea": LayoutStatement, "big-idea": LayoutStatement,
}

// PresentationRequest — taqdimot generatsiyasi uchun kiruvchi ma'lumotlar.
type PresentationRequest struct {
	Topic      string
	SlideCount int // generatsiya qilinishi kerak bo'lgan MAZMUNLI slaydlar soni
	// OnProgress — katta taqdimotda reja va har bir slaydlar guruhi tayyor
	// bo'lganda chaqiriladi (ixtiyoriy; kichik taqdimotda chaqirilmaydi).
	OnProgress func(done, total int)
	// Images — rasmli dizayn: AI har bir slayd (va titul) uchun rasm
	// tavsifini ham yozadi ("image" maydoni). Oddiy dizaynlarda false.
	Images bool

	// Dizayn tuzilishi (generator.ThemeStructure). Layouts — AI tanlashi
	// mumkin bo'lgan maketlar (bo'sh bo'lsa — barchasi); Signature — dizaynga
	// xos maketlar, AI ularni ishlatishi kerak; Compact — dizaynda matn uchun
	// joy kam, AI qisqaroq yozadi.
	Layouts   []string
	Signature []string
	Compact   bool

	// Language — kontent tili (inglizcha nom, masalan "Russian"); bo'sh bo'lsa,
	// mavzu tilida yoziladi.
	Language string
	// Requirements — foydalanuvchining qo'shimcha talablari (ixtiyoriy).
	Requirements string
	// OutlineTitle va ConclusionTitle — reja va xulosa slaydlari sarlavhalari
	// (tanlangan tilda). Bo'sh bo'lsa — "Reja" / "Xulosa" yoki ularning
	// mavzu tilidagi muqobili.
	OutlineTitle    string
	ConclusionTitle string

	// Custom — AI maxsus dizayni (ixtiyoriy): tuzilish vizualroq va xilma-xil
	// bo'ladi, rasm tavsiflari dizaynning rasm uslubiga moslanadi.
	Custom *DesignBrief
}

// layouts so'rov uchun ruxsat etilgan maketlar (noma'lumlari tashlanadi).
func (r PresentationRequest) layouts() []string {
	var out []string
	for _, l := range r.Layouts {
		if slices.Contains(slideLayouts, l) && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return slideLayouts
	}
	// Reja va xulosa tuzilish uchun doim kerak.
	for _, l := range []string{LayoutConclusion, LayoutAgenda} {
		if !slices.Contains(out, l) {
			out = append([]string{l}, out...)
		}
	}
	return out
}

// signature dizaynning imzo maketlari (ruxsat etilganlari).
func (r PresentationRequest) signature() []string {
	allowed := r.layouts()
	var out []string
	for _, l := range r.Signature {
		if slices.Contains(allowed, l) && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
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
	// Image — slaydni bezaydigan rasm tavsifi (inglizcha; faqat Images rejimida).
	Image string `json:"image,omitempty"`
	// Bullets — model sxemadan chetga chiqib eski formatda javob bersa (zaxira).
	Bullets []string `json:"bullets,omitempty"`
}

// Presentation — AI qaytargan taqdimot tarkibi.
type Presentation struct {
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle"`
	Image    string  `json:"image,omitempty"` // titul slayd rasmi tavsifi (Images rejimida)
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
	maxImageRunes     = 300
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
	t := task{system: presentationSystemPrompt, user: buildDeckPrompt(req), schema: deckSchema(req.Images, req.layouts())}
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
		schema: deckOutlineSchema(req.Images, req.layouts()),
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
			user:   buildSlidesBatchPrompt(req, plan, b[0], b[1]),
			schema: slidesBatchSchema(req.Images, req.layouts()),
		}, want, func(raw string) ([]Slide, int, error) {
			s, err := parseSlides(raw)
			if err != nil {
				return nil, 0, err
			}
			s = dropForeignSlides(s, plan, b[0], b[1])
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

	pres := &Presentation{Title: plan.Title, Subtitle: plan.Subtitle, Image: plan.Image}
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

// dropForeignSlides guruh javobidan boshqa guruhga rejalashtirilgan slaydlarni
// olib tashlaydi: modellar ba'zan guruhni oldingi guruhning oxirgi slaydidan
// boshlaydi va taqdimotda bir slayd ikki marta chiqadi. Sarlavhasi faqat
// [from, to) dan tashqaridagi reja slaydiga mos keladigan slayd tashlanadi;
// natija kam bo'lsa, generateJSON qayta so'raydi.
func dropForeignSlides(slides []Slide, plan *deckOutline, from, to int) []Slide {
	key := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	inside, outside := map[string]bool{}, map[string]bool{}
	for i, p := range plan.Slides {
		if i >= from && i < to {
			inside[key(p.Title)] = true
		} else {
			outside[key(p.Title)] = true
		}
	}
	out := slides[:0]
	for _, s := range slides {
		if k := key(s.Title); outside[k] && !inside[k] {
			continue
		}
		out = append(out, s)
	}
	return out
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
- Content must be accurate, substantive, neutral and academic: concrete facts, definitions, mechanisms, causes and effects, real examples. Avoid vague, generic filler sentences.
- Never invent statistics, quotations, URLs or bibliographic references. Use only numbers and dates that are well established.
- Do not put markdown (*, **, #), emojis, bullet symbols or numbering at the start of any text field.
- Slide titles are short and specific (at most 8 words). Never repeat the same slide title.
` + languageRules + "\n" + finalTextRules

// layoutGuides — har bir maket uchun talablar (prompt dizayn maketlaridan yig'iladi).
var layoutGuides = map[string]string{
	LayoutAgenda:     `"agenda": the outline. 4-6 items: "heading" = section name (2-6 words), "text" = what the section covers (6-14 words).`,
	LayoutBullets:    `"bullets": 3-5 items: "heading" = a key idea (2-6 words), "text" = its explanation with a concrete detail or example (20-35 words). Optional "highlight": the main insight of the slide (15-30 words).`,
	LayoutCards:      `"cards": 3-4 parallel aspects, types, functions or directions: "heading" (2-5 words), "text" (20-35 words).`,
	LayoutProcess:    `"process": 3-5 sequential steps or stages in order: "heading" (2-5 words), "text" (15-25 words).`,
	LayoutComparison: `"comparison": exactly 2 items contrasting two sides (advantages vs disadvantages, before vs after, two approaches): "heading" (1-4 words), "points" = 3-5 points (6-15 words each), "text" may be empty. "highlight": a balanced one-sentence conclusion (15-30 words).`,
	LayoutFacts:      `"facts": 3-4 key figures: "value" = a short, well-established number, date, percentage or quantity (at most 10 characters, e.g. "1991", "75%", "3 ta"), "heading" = what it denotes (2-5 words), "text" = context (10-20 words). Use only figures you are certain about; if the topic has no such figures, choose another layout.`,
	LayoutDefinition: `"definition": "highlight" = a precise definition of the key concept (25-45 words); 2-4 items with its key features: "heading" (2-4 words), "text" (10-20 words).`,
	LayoutConclusion: `"conclusion": 3-5 key takeaways: "heading" (2-6 words), "text" (12-25 words); "highlight" = a final concluding statement (15-30 words).`,
	LayoutTimeline:   `"timeline": 3-6 dated events or periods in chronological order: "value" = the date or period (at most 12 characters, e.g. "1991", "XIX asr", "2010-2015"), "heading" = the event (2-6 words), "text" = its significance (12-22 words). Use only well-established dates.`,
	LayoutPyramid:    `"pyramid": 3-5 levels of a hierarchy, ordered from the top (the narrowest, highest or most specific level) down to the base (the broadest, foundational level): "heading" (1-4 words), "text" (10-18 words).`,
	LayoutMatrix:     `"matrix": exactly 4 items forming a 2x2 classification, e.g. a SWOT analysis (strengths, weaknesses, opportunities, threats) or four types: "heading" (1-4 words), "text" (15-28 words). Optional "highlight": a 1-3 word label for the centre of the matrix (e.g. "SWOT").`,
	LayoutCycle:      `"cycle": 3-6 stages of a repeating process in order, where the last stage leads back to the first: "heading" (1-4 words), "text" (8-16 words); "highlight" = the name of the cycle (1-3 words).`,
	LayoutHub:        `"hub": a central concept with 4-6 components, factors or areas around it: "highlight" = the central concept (1-3 words); items: "heading" (1-4 words), "text" (8-16 words).`,
	LayoutStatement:  `"statement": one key thesis of the topic shown in large type: "highlight" = the statement itself (15-30 words; a precise claim in your own words, not a quotation); 0-3 items supporting it: "heading" (1-4 words), "text" (6-14 words).`,
}

// compactWords — ixcham dizaynlarda so'zlar sonini kamaytirish koeffitsiyenti.
const compactWords = 0.75

var wordRangeRe = regexp.MustCompile(`(\d+)-(\d+) words`)

// scaleWords "20-35 words" kabi oraliqlarni k marta o'zgartiradi.
func scaleWords(s string, k float64) string {
	return wordRangeRe.ReplaceAllStringFunc(s, func(m string) string {
		p := wordRangeRe.FindStringSubmatch(m)
		lo, _ := strconv.Atoi(p[1])
		hi, _ := strconv.Atoi(p[2])
		nlo := max(1, int(math.Round(float64(lo)*k)))
		nhi := max(nlo+1, int(math.Round(float64(hi)*k)))
		return fmt.Sprintf("%d-%d words", nlo, nhi)
	})
}

// layoutGuide — ruxsat etilgan maketlar uchun talablar (barcha taqdimot promptlarida).
func layoutGuide(layouts []string, compact bool) string {
	var b strings.Builder
	b.WriteString(`Slide layouts (the "layout" field) — use only these — and the content each one needs:` + "\n")
	for _, l := range slideLayouts {
		if slices.Contains(layouts, l) {
			b.WriteString("- " + layoutGuides[l] + "\n")
		}
	}
	b.WriteString("Every slide except the agenda has \"lead\": one sentence (12-25 words) that introduces the slide's main idea and does not repeat the items.\n")
	b.WriteString("Fields a layout does not use are left empty.")
	out := b.String()
	if compact {
		out = scaleWords(out, compactWords) + "\nThis design uses large type and leaves little room for text: keep every text short and within these word limits."
	}
	return out
}

// slideJSONShape — bitta slayd JSON shakli (images — rasm tavsifi maydoni bilan).
func slideJSONShape(images bool) string {
	img := ""
	if images {
		img = `, "image": string`
	}
	return `{"layout": string, "title": string, "lead": string, "items": [{"value": string, "heading": string, "text": string, "points": [string]}], "highlight": string` + img + `}`
}

// imageRule — rasmli dizaynda rasm tavsiflari uchun talablar. Tavsif ingliz
// tilida: rasm modellari uni eng aniq tushunadi.
const imageRule = `Every slide also has "image": an English description (12-30 words) of a realistic photograph that illustrates the slide's content: a concrete subject, setting and composition. ` +
	`No text, letters, numbers, captions, logos, charts, diagrams or real people's names in the picture. Do not repeat the same scene on different slides.`

// writeImageRules rasmli dizaynda rasm tavsiflari qoidalarini qo'shadi
// (maxsus dizaynda — uning rasm uslubiga moslash talabi bilan).
func writeImageRules(b *strings.Builder, req PresentationRequest) {
	if !req.Images {
		return
	}
	b.WriteString(imageRule + "\n")
	if req.Custom != nil {
		b.WriteString(customImageRule(req.Custom) + "\n")
	}
}

// coverImageRule — titul slayd rasmi.
const coverImageRule = `The top-level "image" is an English description (12-30 words) of one striking realistic photograph that represents the whole topic, with no text in the picture.`

// signatureHints — imzo maketiga mos mazmun (AI mazmunni maketga moslashi uchun).
var signatureHints = map[string]string{
	LayoutTimeline:  "a history or chronology becomes a timeline",
	LayoutPyramid:   "a hierarchy of levels becomes a pyramid",
	LayoutMatrix:    "a four-part classification or SWOT becomes a matrix",
	LayoutCycle:     "a repeating process becomes a cycle",
	LayoutHub:       "components or factors around one concept become a hub",
	LayoutStatement: "the central thesis becomes a statement",
}

// structureTitles reja va xulosa slaydlari sarlavhalari uchun prompt bo'laklari.
// Sarlavha berilmagan bo'lsa — o'zbekcha nom va mavzu tilidagi muqobili.
func (r PresentationRequest) structureTitles() (outline, conclusion string) {
	outline = `titled "Reja" (use the equivalent word if the topic is not in Uzbek, e.g. "План" or "Outline")`
	if t := strings.TrimSpace(r.OutlineTitle); t != "" {
		outline = fmt.Sprintf("titled %q", t)
	}
	conclusion = `titled "Xulosa" (or its equivalent)`
	if t := strings.TrimSpace(r.ConclusionTitle); t != "" {
		conclusion = fmt.Sprintf("titled %q", t)
	}
	return outline, conclusion
}

// structureRules — taqdimot tuzilishi (n — mazmunli slaydlar soni).
func structureRules(n int, req PresentationRequest) string {
	signature := req.signature()
	outlineTitle, conclusionTitle := req.structureTitles()
	var b strings.Builder
	b.WriteString("Structure:\n")
	switch {
	case n >= 3:
		fmt.Fprintf(&b, "1. Slide 1 is the outline: layout \"agenda\", %s; its items name the main sections that follow.\n", outlineTitle)
		fmt.Fprintf(&b, "2. Slides 2-%d are the main sections, covering the topic logically from fundamentals to details; each slide focuses on one idea and they do not overlap. "+
			"A \"definition\" slide fits well right after the outline.\n", n-1)
		fmt.Fprintf(&b, "3. Slide %d is the conclusion: layout \"conclusion\", %s.\n", n, conclusionTitle)
		switch {
		case req.Custom != nil:
			b.WriteString("Choose for each main slide the layout that best fits its content.\n")
			b.WriteString(customDeckRules(n, req.Custom) + "\n")
		case n >= 6:
			b.WriteString("Choose for each main slide the layout that best fits its content and vary them: use at least 4 different layouts, and never the same layout on more than 2 consecutive slides.\n")
		default:
			b.WriteString("Choose for each main slide the layout that best fits its content; avoid repeating the same layout.\n")
		}
		if k := signatureMinimum(n, signature); k > 0 {
			quoted := make([]string, len(signature))
			var hints []string
			for i, l := range signature {
				quoted[i] = `"` + l + `"`
				if h, ok := signatureHints[l]; ok {
					hints = append(hints, h)
				}
			}
			fmt.Fprintf(&b, "The chosen slide design is built around these signature layouts: %s. Use at least %d of them, on slides whose content naturally fits them", strings.Join(quoted, ", "), k)
			if len(hints) > 0 {
				fmt.Fprintf(&b, " (shape the content accordingly: %s)", strings.Join(hints, "; "))
			}
			b.WriteString(".\n")
		}
	case n == 2:
		b.WriteString("1. Slide 1 explains the core of the topic (layout \"definition\" or \"bullets\").\n")
		fmt.Fprintf(&b, "2. Slide 2 is the conclusion: layout \"conclusion\", %s.\n", conclusionTitle)
	default:
		b.WriteString("1. A single slide giving a complete overview of the topic (layout \"bullets\" with a \"highlight\").\n")
	}
	return b.String()
}

// signatureMinimum — taqdimotda kamida nechta imzo maketi ishlatilishi kerak
// (n — mazmunli slaydlar soni; asosiy slaydlarning yarmigacha). 0 — talab yo'q.
func signatureMinimum(n int, signature []string) int {
	main := n - 2
	if n < 3 || main < 2 || len(signature) == 0 {
		return 0
	}
	return min(len(signature), max(1, main/2))
}

// buildDeckPrompt — butun taqdimot bitta so'rovda.
func buildDeckPrompt(req PresentationRequest) string {
	n := req.SlideCount
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	b.WriteString(languageRule(req.Language))
	fmt.Fprintf(&b, "Number of slides in \"slides\": exactly %d\n\n", n)
	b.WriteString(structureRules(n, req))
	b.WriteString("\n" + layoutGuide(req.layouts(), req.Compact) + "\n")
	writeImageRules(&b, req)
	b.WriteString("\n")
	top := ""
	if req.Images {
		top = `, "image": string`
	}
	b.WriteString(`JSON shape: {"title": string, "subtitle": string` + top + `, "slides": [` + slideJSONShape(req.Images) + `, ...]}` + "\n")
	b.WriteString(`The top-level "title" is a polished version of the topic; "subtitle" is a short phrase (4-10 words) that clarifies the scope of the presentation.`)
	if req.Images {
		b.WriteString("\n" + coverImageRule)
	}
	if req.Custom != nil {
		b.WriteString("\n\n" + numberedCheck(append(customPlanChecks(n, req.signature()), customContentChecks(false)...)))
	}
	writeRequirements(&b, req.Requirements)
	return b.String()
}

// buildDeckOutlinePrompt — katta taqdimot rejasi.
func buildDeckOutlinePrompt(req PresentationRequest) string {
	n := req.SlideCount
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	b.WriteString(languageRule(req.Language))
	fmt.Fprintf(&b, "Plan a presentation with exactly %d slides (do not write their content yet).\n\n", n)
	b.WriteString(structureRules(n, req))
	b.WriteString("\n" + layoutGuide(req.layouts(), req.Compact) + "\n\n")
	b.WriteString(`For each slide give "title", "layout" and "focus": one sentence (12-25 words) naming exactly what the slide will cover — specific sub-topics, facts or examples — so that slides do not overlap.` + "\n")
	top := ""
	if req.Images {
		top = `, "image": string`
	}
	b.WriteString(`JSON shape: {"title": string, "subtitle": string` + top + `, "slides": [{"n": number, "title": string, "layout": string, "focus": string}, ...]}` + "\n")
	// Raqamlash modellarga slaydlarni aniq sanashga yordam beradi (raqamsiz
	// rejalarda bir slayd ko'pincha tushib qoladi).
	if n >= 2 {
		fmt.Fprintf(&b, "Number the slides in \"n\" from 1 to %d without gaps: slide %d is the conclusion.\n", n, n)
	}
	b.WriteString(`The top-level "title" is a polished version of the topic; "subtitle" is a short phrase (4-10 words) that clarifies the scope of the presentation.`)
	if req.Images {
		b.WriteString("\n" + coverImageRule)
	}
	if req.Custom != nil {
		b.WriteString("\n\n" + numberedCheck(customPlanChecks(n, req.signature())))
	}
	writeRequirements(&b, req.Requirements)
	return b.String()
}

// buildSlidesBatchPrompt — rejadagi [from, to) slaydlar mazmuni.
func buildSlidesBatchPrompt(req PresentationRequest, plan *deckOutline, from, to int) string {
	images := req.Images
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	b.WriteString(languageRule(req.Language))
	b.WriteString("\nPresentation plan (all slides):\n")
	for i, s := range plan.Slides {
		fmt.Fprintf(&b, "%d. [%s] %s — %s\n", i+1, s.Layout, s.Title, s.Focus)
	}
	fmt.Fprintf(&b, "\nWrite the full content of slides %d-%d only (%d slides), exactly as planned: same order, titles and layouts. "+
		"Stay within each slide's focus and do not repeat what other slides cover.\n\n", from+1, to, to-from)
	b.WriteString(layoutGuide(req.layouts(), req.Compact) + "\n")
	writeImageRules(&b, req)
	b.WriteString("\n")
	b.WriteString(`JSON shape: {"slides": [` + slideJSONShape(images) + `, ...]}`)
	if req.Custom != nil {
		b.WriteString("\n\n" + numberedCheck(customContentChecks(true)))
	}
	writeRequirements(&b, req.Requirements)
	return b.String()
}

// ---------------------------------------------------------------------------
// JSON sxemalar (Gemini structured output)
// ---------------------------------------------------------------------------

func slideSchema(images bool, layouts []string) *genai.Schema {
	s := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"layout": {Type: genai.TypeString, Format: "enum", Enum: layouts},
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
	if images {
		addImageProperty(s)
	}
	return s
}

// addImageProperty sxemaga majburiy "image" maydonini qo'shadi (rasmli dizayn uchun).
func addImageProperty(s *genai.Schema) {
	s.Properties["image"] = &genai.Schema{Type: genai.TypeString}
	s.Required = append(s.Required, "image")
	s.PropertyOrdering = append(s.PropertyOrdering, "image")
}

func deckSchema(images bool, layouts []string) *genai.Schema {
	s := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"title":    {Type: genai.TypeString},
			"subtitle": {Type: genai.TypeString},
			"slides":   {Type: genai.TypeArray, Items: slideSchema(images, layouts)},
		},
		Required:         []string{"title", "subtitle", "slides"},
		PropertyOrdering: []string{"title", "subtitle", "slides"},
	}
	if images {
		addImageProperty(s)
	}
	return s
}

func slidesBatchSchema(images bool, layouts []string) *genai.Schema {
	return &genai.Schema{
		Type:       genai.TypeObject,
		Properties: map[string]*genai.Schema{"slides": {Type: genai.TypeArray, Items: slideSchema(images, layouts)}},
		Required:   []string{"slides"},
	}
}

func deckOutlineSchema(images bool, layouts []string) *genai.Schema {
	s := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"title":    {Type: genai.TypeString},
			"subtitle": {Type: genai.TypeString},
			"slides": {Type: genai.TypeArray, Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"n":      {Type: genai.TypeInteger},
					"title":  {Type: genai.TypeString},
					"layout": {Type: genai.TypeString, Format: "enum", Enum: layouts},
					"focus":  {Type: genai.TypeString},
				},
				Required:         []string{"n", "title", "layout", "focus"},
				PropertyOrdering: []string{"n", "title", "layout", "focus"},
			}},
		},
		Required:         []string{"title", "subtitle", "slides"},
		PropertyOrdering: []string{"title", "subtitle", "slides"},
	}
	if images {
		addImageProperty(s)
	}
	return s
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
	pres.Image = truncateRunes(cleanText(pres.Image), maxImageRunes)
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
	s.Image = truncateRunes(cleanText(s.Image), maxImageRunes)

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
// (xulosa) slaydni saqlagan holda o'rtadagilarni qisqartiradi. Xulosa
// slaydi oxirida bo'lmasa (model undan keyin yana slayd qo'ygan) — oxirga ko'chiriladi.
func (p *Presentation) fit(want int) {
	p.Slides = fitSlides(conclusionLast(p.Slides, func(s Slide) string { return s.Layout }), want)
}

// conclusionLast oxirgi slayd xulosa bo'lmasa, eng oxirgi "conclusion"
// maketli slaydni ro'yxat oxiriga ko'chiradi (xulosa yo'q bo'lsa — o'zgarishsiz).
func conclusionLast[T any](s []T, layout func(T) string) []T {
	if len(s) == 0 || layout(s[len(s)-1]) == LayoutConclusion {
		return s
	}
	for i := len(s) - 2; i >= 0; i-- {
		if layout(s[i]) == LayoutConclusion {
			return append(append(append([]T(nil), s[:i]...), s[i+1:]...), s[i])
		}
	}
	return s
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
	N      int    `json:"n"` // tartib raqami (faqat sanash uchun; ishlatilmaydi)
	Title  string `json:"title"`
	Layout string `json:"layout"`
	Focus  string `json:"focus"`
}

type deckOutline struct {
	Title    string         `json:"title"`
	Subtitle string         `json:"subtitle"`
	Image    string         `json:"image,omitempty"` // titul slayd rasmi tavsifi (Images rejimida)
	Slides   []plannedSlide `json:"slides"`
}

func parseDeckOutline(raw string) (*deckOutline, error) {
	var o deckOutline
	if err := decodeJSON(raw, &o, &o.Slides); err != nil {
		return nil, err
	}
	o.Title = truncateRunes(stripMeta(cleanText(o.Title)), maxTitleRunes)
	o.Subtitle = truncateRunes(strings.TrimRight(stripMeta(cleanText(o.Subtitle)), "."), maxSubtitleRunes)
	o.Image = truncateRunes(cleanText(o.Image), maxImageRunes)
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

func (o *deckOutline) fit(want int) {
	o.Slides = fitSlides(conclusionLast(o.Slides, func(s plannedSlide) string { return s.Layout }), want)
}
