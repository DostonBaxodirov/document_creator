package ai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// DocumentRequest — mustaqil ish, referat yoki amaliy topshiriq generatsiyasi uchun ma'lumotlar.
type DocumentRequest struct {
	WorkType WorkType // WorkIndependent, WorkReferat yoki WorkPractical
	Topic    string
	// Words — asosiy matnning (kirish + boblar + xulosa) taxminiy hajmi, so'zlarda.
	// Sahifalar sonidan generator hisoblab beradi.
	Words int
	// OnProgress — har bir bosqich (reja, har bir bo'lim) tugaganda chaqiriladi.
	// Ixtiyoriy. Chaqiruvlar ketma-ket (bir vaqtda bittadan) bo'ladi, lekin
	// generatsiya goroutine'laridan — shuning uchun funksiya tez ishlashi kerak.
	OnProgress func(done, total int)

	// Language — kontent tili (inglizcha nom, masalan "Russian"); bo'sh bo'lsa,
	// mavzu tilida yoziladi.
	Language string
	// Requirements — foydalanuvchining qo'shimcha talablari (ixtiyoriy).
	Requirements string
	// Figures — har bir bob uchun rasm g'oyasi (izoh va rasm tavsifi) ham
	// yozilsin (maxsus dizayn: rasmlar shulardan chiziladi).
	Figures bool
	// OnOutline — reja tayyor bo'lishi bilan, bo'limlar yozilishidan oldin
	// bir marta chaqiriladi (ixtiyoriy): rasmlarni matn bilan parallel chizishni
	// boshlash uchun. figures da reja boblari soniga teng element bor:
	// figures[i] — Document.Chapters[i] bobining rasm g'oyasi (bobda g'oya
	// bo'lmasa yoki Figures false bo'lsa — nil). Chaqiruv sinxron —
	// generatsiya uni kutadi, shuning uchun funksiya tez qaytishi kerak
	// (uzoq ishni alohida goroutine'da boshlash kerak).
	OnOutline func(figures []*FigureIdea)
}

// FigureIdea — bob uchun rasm g'oyasi.
type FigureIdea struct {
	Caption string // hujjat tilidagi izoh: "Raqamli iqtisodiyotning asosiy unsurlari"
	Prompt  string // rasm modeli uchun inglizcha tavsif
}

// Paragraph — hujjatdagi bitta paragraf. Code=true bo'lsa — dastur kodi
// (qator ko'chirishlari saqlanadi, monoshirift bilan chiqariladi).
type Paragraph struct {
	Text string
	Code bool
}

// Chapter — hujjatning bitta bobi.
type Chapter struct {
	Title      string
	Paragraphs []Paragraph
	// Figure — rasm g'oyasi (faqat DocumentRequest.Figures bo'lsa; bo'lmasligi ham mumkin).
	Figure *FigureIdea
}

// Document — AI yozgan to'liq hujjat matni.
//
// Kontrakt: Chapters reja boblari tartibida va sonida bo'ladi — OnOutline
// dagi figures[i] aynan Chapters[i] ga tegishli.
type Document struct {
	Introduction []Paragraph
	Chapters     []Chapter
	Conclusion   []Paragraph
	References   []string
}

// Hujjat tuzilmasi va matn cheklovlari.
const (
	minDocumentWords   = 150
	minChapters        = 2
	maxChapters        = 6
	wordsPerChapter    = 450  // bob soni shu hajmdan kelib chiqib tanlanadi
	wordsPerParagraph  = 110  // paragraflar sonini taklif qilish uchun
	sectionMinPercent  = 55   // bo'lim shundan qisqa bo'lsa — qayta yoziladi
	sectionFillPercent = 90   // bo'lim shundan qisqa bo'lsa — davomi yozdiriladi (extendSections)
	minExtendWords     = 60   // bundan kam yetishmasa — davom ettirilmaydi
	maxParagraphRunes  = 5000 // bitta paragrafning maksimal uzunligi
	maxCodeLines       = 80
	maxPointsPerTopic  = 6
	maxReferences      = 10
	maxReferenceRunes  = 400
	maxChapterTitleLen = 150
	maxFigureCaption   = 150
	maxFigurePrompt    = 400
)

// GenerateDocument hujjatni ikki bosqichda yaratadi:
//
//  1. Reja: boblar nomlari, har bir bob yoritishi kerak bo'lgan masalalar
//     va adabiyotlar ro'yxati (bitta so'rov).
//  2. Matn: kirish, har bir bob va xulosa alohida so'rov bilan, parallel
//     (AI_PARALLEL_REQUESTS tagacha) yoziladi.
//
// Bo'lib yozish uzun hujjatlarni ham modelning javob uzunligi chegarasiga
// sig'dirish imkonini beradi va har bir bo'limning hajmini nazorat qiladi.
func (c *client) GenerateDocument(ctx context.Context, req DocumentRequest) (*Document, error) {
	if strings.TrimSpace(req.Topic) == "" {
		return nil, errors.New("ai: mavzu bo'sh")
	}
	if !req.WorkType.IsDocument() {
		return nil, fmt.Errorf("ai: hujjat uchun noto'g'ri ish turi: %q", req.WorkType)
	}
	req.Words = max(req.Words, minDocumentWords)

	b := planBudget(req.Words)
	progress := &progressReporter{fn: req.OnProgress, total: 1 + b.chapters + 2}
	progress.report()

	// 1. Reja.
	ol, err := generateJSON(ctx, c, "document_outline", task{
		system: documentSystemPrompt,
		user:   buildOutlinePrompt(req, b.chapters),
		schema: outlineSchema(req.Figures),
	}, b.chapters, func(raw string) (*outline, int, error) {
		o, err := parseOutline(raw, b.chapters)
		if err != nil {
			return nil, 0, err
		}
		return o, len(o.Chapters), nil
	})
	if err != nil {
		return nil, err
	}
	// AI kamroq bob qaytargan bo'lishi mumkin — bosqichlar sonini aniqlashtiramiz.
	progress.setTotal(1 + len(ol.Chapters) + 2)
	progress.advance()

	// Rasm g'oyalari rejada tayyor: chaqiruvchi rasmlarni bo'limlar yozilishini
	// kutmasdan chizishni boshlashi mumkin.
	if req.OnOutline != nil {
		figures := make([]*FigureIdea, len(ol.Chapters))
		for i, ch := range ol.Chapters {
			figures[i] = figureIdea(req, ch)
		}
		req.OnOutline(figures)
	}

	// 2. Bo'limlar. Boblar kamroq chiqqan bo'lsa, ularning umumiy hajmi
	// qolgan boblarga bo'linadi — hujjat baribir so'ralgan hajmda chiqadi.
	chapterWords := b.chapter * b.chapters / len(ol.Chapters)
	parts := make([]section, 0, len(ol.Chapters)+2)
	parts = append(parts, section{kind: sectionIntro, words: b.intro})
	for i, ch := range ol.Chapters {
		parts = append(parts, section{kind: sectionChapter, number: i + 1, title: ch.Title, points: ch.Points, words: chapterWords})
	}
	parts = append(parts, section{kind: sectionConclusion, words: b.conclusion})

	texts, err := c.writeSections(ctx, req, ol, parts, progress)
	if err != nil {
		return nil, err
	}
	// 3. Model ko'pincha so'ralganidan kamroq yozadi: sezilarli qisqa
	// bo'limlarning davomi yozdiriladi (sahifalar soni so'ralganiga yaqin bo'lsin).
	c.extendSections(ctx, req, ol, parts, texts, progress)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	doc := &Document{
		Introduction: texts[0],
		Conclusion:   texts[len(texts)-1],
		References:   ol.References,
		Chapters:     make([]Chapter, 0, len(ol.Chapters)),
	}
	// Boblar reja tartibida, birortasi ham tashlab ketilmaydi (OnOutline kontrakti).
	for i, ch := range ol.Chapters {
		doc.Chapters = append(doc.Chapters, Chapter{Title: ch.Title, Paragraphs: texts[i+1], Figure: figureIdea(req, ch)})
	}

	c.log.Info("AI document generated",
		zap.String("work_type", string(req.WorkType)),
		zap.Int("chapters", len(doc.Chapters)),
		zap.Int("words", doc.wordCount()),
		zap.Int("requested_words", req.Words),
		zap.Int("references", len(doc.References)))
	return doc, nil
}

// figureIdea reja bobidagi rasm g'oyasi (rasmlar so'ralmagan yoki g'oya
// bo'lmasa — nil). OnOutline va Document.Chapters uchun bir xil qoida.
func figureIdea(req DocumentRequest, ch outlineChapter) *FigureIdea {
	if !req.Figures || ch.Figure == nil || ch.Figure.Prompt == "" {
		return nil
	}
	return &FigureIdea{Caption: ch.Figure.Caption, Prompt: ch.Figure.Prompt}
}

// writeSections bo'limlarni cheklangan parallellik bilan yozadi. Birorta bo'lim
// xato bilan tugasa, qolganlari to'xtatiladi va birinchi xato qaytariladi.
func (c *client) writeSections(ctx context.Context, req DocumentRequest, ol *outline,
	parts []section, progress *progressReporter) ([][]Paragraph, error) {
	results := make([][]Paragraph, len(parts))
	// Har bir goroutine faqat o'z bo'limiga (results[i]) yozadi; panic'ni
	// forEachParallel xatoga aylantiradi.
	err := forEachParallel(ctx, len(parts), c.parallel, func(ctx context.Context, i int) error {
		part := parts[i]
		paras, err := generateJSON(ctx, c, "document_"+part.kind.String(), task{
			system: documentSystemPrompt,
			user:   buildSectionPrompt(req, ol, part),
			schema: sectionSchema(),
		}, part.words*sectionMinPercent/100, func(raw string) ([]Paragraph, int, error) {
			return parseSection(raw, part.title)
		})
		if err != nil {
			return fmt.Errorf("%s: %w", part.label(), err)
		}
		results[i] = paras
		progress.advance()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// shortSections hajmi so'ralganning sectionFillPercent foizidan kam va
// kamida minExtendWords so'z yetishmaydigan bo'limlar indekslari.
func shortSections(parts []section, texts [][]Paragraph) []int {
	var out []int
	for i, p := range parts {
		have := wordCount(texts[i])
		if have*100 < p.words*sectionFillPercent && p.words-have >= minExtendWords {
			out = append(out, i)
		}
	}
	return out
}

// extendSections qisqa chiqqan bo'limlarning davomini yozdiradi: model
// bo'limning hozirgi matnini ko'radi va yetishmayotgan hajmda yangi
// paragraflar qo'shadi. Bu qo'shimcha bosqich — xato bo'lsa, bo'lim avvalgi
// holicha qoladi va hujjat baribir tayyorlanadi.
func (c *client) extendSections(ctx context.Context, req DocumentRequest, ol *outline,
	parts []section, texts [][]Paragraph, progress *progressReporter) {
	short := shortSections(parts, texts)
	if len(short) == 0 {
		return
	}
	progress.addTotal(len(short))

	sem := make(chan struct{}, c.parallel)
	var wg sync.WaitGroup
	for _, i := range short {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer progress.advance()
			defer func() {
				if r := recover(); r != nil {
					c.log.Error("document extension panicked", zap.String("section", parts[i].label()), zap.Any("panic", r))
				}
			}()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			part, have := parts[i], wordCount(texts[i])
			missing := part.words - have
			extra, err := generateJSON(ctx, c, "document_extend_"+part.kind.String(), task{
				system: documentSystemPrompt,
				user:   buildExtendPrompt(req, ol, part, texts[i], missing),
				schema: sectionSchema(),
			}, missing*sectionMinPercent/100, func(raw string) ([]Paragraph, int, error) {
				return parseSection(raw, part.title)
			})
			if err != nil {
				c.log.Warn("document section not extended", zap.String("section", part.label()), zap.Error(err))
				return
			}
			// Har bir goroutine faqat o'z bo'limiga (texts[i]) yozadi.
			texts[i] = appendNew(texts[i], extra)
			c.log.Info("document section extended",
				zap.String("section", part.label()),
				zap.Int("words_before", have),
				zap.Int("words_after", wordCount(texts[i])),
				zap.Int("requested", part.words))
		}()
	}
	wg.Wait()
}

// appendNew bo'lim oxiriga yangi paragraflarni qo'shadi; mavjud paragrafni
// aynan takrorlaganlari tashlab yuboriladi.
func appendNew(have, extra []Paragraph) []Paragraph {
	seen := make(map[string]bool, len(have))
	for _, p := range have {
		seen[strings.ToLower(p.Text)] = true
	}
	for _, p := range extra {
		if key := strings.ToLower(p.Text); !seen[key] {
			seen[key] = true
			have = append(have, p)
		}
	}
	return have
}

// ---------------------------------------------------------------------------
// Hajm taqsimoti
// ---------------------------------------------------------------------------

// budget — umumiy hajmning bo'limlar bo'yicha taqsimoti (so'zlarda).
type budget struct {
	intro, chapter, conclusion int
	chapters                   int // boblar soni
}

// planBudget kirish (~12%), xulosa (~10%) va boblar uchun hajmni hisoblaydi.
// Boblar soni har bir bobga ~450 so'z to'g'ri keladigan qilib tanlanadi (2..6).
func planBudget(words int) budget {
	intro := max(100, words*12/100)
	conclusion := max(90, words*10/100)
	rest := max(words-intro-conclusion, minChapters*100)
	k := int(math.Round(float64(rest) / wordsPerChapter))
	k = min(max(k, minChapters), maxChapters)
	return budget{intro: intro, chapter: rest / k, conclusion: conclusion, chapters: k}
}

// progressReporter OnProgress chaqiruvlarini ketma-ket va tartibli qiladi.
type progressReporter struct {
	mu    sync.Mutex
	fn    func(done, total int)
	done  int
	total int
}

func (p *progressReporter) report() {
	if p.fn == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fn(p.done, p.total)
}

func (p *progressReporter) advance() {
	p.mu.Lock()
	p.done++
	p.mu.Unlock()
	p.report()
}

func (p *progressReporter) setTotal(total int) {
	p.mu.Lock()
	p.total = total
	p.mu.Unlock()
}

func (p *progressReporter) addTotal(n int) {
	p.mu.Lock()
	p.total += n
	p.mu.Unlock()
	p.report()
}

// ---------------------------------------------------------------------------
// Reja (outline)
// ---------------------------------------------------------------------------

type outlineChapter struct {
	Title  string         `json:"title"`
	Points []string       `json:"points"`
	Figure *outlineFigure `json:"figure,omitempty"`
}

type outlineFigure struct {
	Caption string `json:"caption"`
	Prompt  string `json:"prompt"`
}

type outline struct {
	Chapters   []outlineChapter `json:"chapters"`
	References []string         `json:"references"`
}

// outlineSchema reja sxemasi; figures — har bir bobga rasm g'oyasi ham.
func outlineSchema(figures bool) map[string]any {
	chapter := Object(map[string]any{
		"title":  String(),
		"points": stringArraySchema(),
	}, "title", "points")
	if figures {
		withProperty(chapter, "figure", Object(map[string]any{
			"caption": String(),
			"prompt":  String(),
		}, "caption", "prompt"))
	}
	return Object(map[string]any{
		"chapters":   Array(chapter),
		"references": stringArraySchema(),
	}, "chapters", "references")
}

// "[1] ", "1. " kabi adabiyot raqamlari (raqamni generator o'zi qo'yadi).
var refPrefixRe = regexp.MustCompile(`^(?:\[\d{1,2}\]|\d{1,2}[.)])\s*`)

// parseOutline rejani o'qiydi, tozalaydi va boblar sonini want gacha qisqartiradi.
func parseOutline(raw string, want int) (*outline, error) {
	var o outline
	if err := decodeJSON(raw, &o, &o.Chapters); err != nil {
		return nil, err
	}

	chapters := o.Chapters[:0]
	seen := map[string]bool{}
	for _, ch := range o.Chapters {
		ch.Title = truncateRunes(strings.TrimRight(cleanText(ch.Title), ".:;"), maxChapterTitleLen)
		key := strings.ToLower(ch.Title)
		// Kirish/Xulosa kabi bo'limlar hujjatda alohida chiqadi — boblar orasida takrorlanmasin.
		if ch.Title == "" || seen[key] || isMeta(ch.Title) || isReservedHeading(ch.Title) {
			continue
		}
		seen[key] = true

		points := make([]string, 0, len(ch.Points))
		for _, p := range ch.Points {
			if p = stripMeta(cleanText(p)); p != "" {
				points = append(points, truncateRunes(p, 200))
			}
			if len(points) == maxPointsPerTopic {
				break
			}
		}
		ch.Points = points
		if f := ch.Figure; f != nil {
			f.Caption = truncateRunes(strings.TrimRight(stripMeta(cleanText(f.Caption)), "."), maxFigureCaption)
			f.Prompt = truncateRunes(cleanText(f.Prompt), maxFigurePrompt)
			if f.Prompt == "" {
				ch.Figure = nil
			}
		}
		chapters = append(chapters, ch)
	}
	if len(chapters) == 0 {
		return nil, errors.New("rejada birorta ham bob yo'q")
	}
	o.Chapters = chapters[:min(len(chapters), want)]

	refs := make([]string, 0, len(o.References))
	seenRef := map[string]bool{}
	for _, r := range o.References {
		r = cleanText(refPrefixRe.ReplaceAllString(strings.TrimSpace(r), ""))
		// Adabiyot yozuvida izoh/eslatma bo'lsa — butun yozuv tashlab yuboriladi
		// (bibliografik yozuvda nuqtalar ko'p, uni gaplarga bo'lib bo'lmaydi).
		if r == "" || seenRef[strings.ToLower(r)] || isMeta(r) {
			continue
		}
		seenRef[strings.ToLower(r)] = true
		refs = append(refs, truncateRunes(r, maxReferenceRunes))
		if len(refs) == maxReferences {
			break
		}
	}
	o.References = refs
	return &o, nil
}

// ---------------------------------------------------------------------------
// Bo'limlar
// ---------------------------------------------------------------------------

type sectionKind int

const (
	sectionIntro sectionKind = iota
	sectionChapter
	sectionConclusion
)

func (k sectionKind) String() string {
	switch k {
	case sectionIntro:
		return "introduction"
	case sectionConclusion:
		return "conclusion"
	default:
		return "chapter"
	}
}

// section — yoziladigan bitta bo'lim.
type section struct {
	kind   sectionKind
	number int // bob raqami (1 dan)
	title  string
	points []string
	words  int // so'ralgan hajm
}

func (s section) label() string {
	if s.kind == sectionChapter {
		return fmt.Sprintf("%d-bob", s.number)
	}
	return s.kind.String()
}

func sectionSchema() map[string]any {
	return Object(map[string]any{"paragraphs": stringArraySchema()}, "paragraphs")
}

// parseSection bo'lim matnini o'qiydi; ball — so'zlar soni.
func parseSection(raw, title string) ([]Paragraph, int, error) {
	var obj struct {
		Paragraphs []string `json:"paragraphs"`
	}
	if err := decodeJSON(raw, &obj, &obj.Paragraphs); err != nil {
		return nil, 0, err
	}
	paras := normalizeParagraphs(obj.Paragraphs, title)
	if len(paras) == 0 {
		return nil, 0, errors.New("bo'limda matn yo'q")
	}
	return paras, wordCount(paras), nil
}

// normalizeParagraphs paragraflarni tozalaydi:
//   - ``` bilan boshlangan paragraf — dastur kodi (qatorlari saqlanadi);
//   - bitta satrga bir nechta paragraf yozilgan bo'lsa — bo'linadi;
//   - markdown va ro'yxat belgilari, xizmat gaplari (sanitize.go) olib tashlanadi;
//   - model bo'lim sarlavhasini ham matn boshiga yozib yuborgan bo'lsa — tashlab yuboriladi.
func normalizeParagraphs(in []string, title string) []Paragraph {
	out := make([]Paragraph, 0, len(in))
	titleKey := headingKey(title)

	for _, raw := range in {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			if code := cleanCode(trimmed); code != "" {
				out = append(out, Paragraph{Text: code, Code: true})
			}
			continue
		}
		for _, line := range strings.Split(trimmed, "\n") {
			text := stripMeta(cleanText(line))
			if text == "" {
				continue
			}
			if len(out) == 0 && titleKey != "" && headingKey(text) == titleKey {
				continue
			}
			out = append(out, Paragraph{Text: truncateRunes(text, maxParagraphRunes)})
		}
	}
	return out
}

// headingKey sarlavhalarni solishtirish uchun: kichik harflar, raqam va tinish belgilarisiz.
func headingKey(s string) string {
	s = strings.ToLower(cleanText(s))
	s = strings.TrimLeft(s, "0123456789. ")
	return strings.TrimRight(s, ".:; ")
}

// cleanCode ```lang ... ``` blokidan kodni ajratib oladi.
func cleanCode(s string) string {
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:] // birinchi qatordagi til nomi (```python) tashlab yuboriladi
	} else {
		return ""
	}
	s = strings.TrimSuffix(strings.TrimRight(s, " \n\t"), "```")

	lines := strings.Split(strings.ReplaceAll(s, "\t", "    "), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \r")
	}
	// Bosh va oxiridagi bo'sh qatorlar.
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxCodeLines {
		lines = lines[:maxCodeLines]
	}
	return truncateRunes(strings.Join(lines, "\n"), maxParagraphRunes)
}

func wordCount(paras []Paragraph) int {
	n := 0
	for _, p := range paras {
		n += len(strings.Fields(p.Text))
	}
	return n
}

func (d *Document) wordCount() int {
	n := wordCount(d.Introduction) + wordCount(d.Conclusion)
	for _, ch := range d.Chapters {
		n += wordCount(ch.Paragraphs)
	}
	return n
}

// ---------------------------------------------------------------------------
// Promptlar
// ---------------------------------------------------------------------------

const documentSystemPrompt = `You are an expert academic writer who prepares independent study works, referats (academic review essays) and practical assignments for university students in Uzbekistan.

Strict output rules:
- Respond with ONE valid JSON object and nothing else: no markdown fences around the JSON, no comments, no extra text.
- Content must be accurate, neutral and academic. Never invent statistics, quotations, URLs or bibliographic references.
- Do not use markdown (*, **, #) or emojis.
` + languageRules + "\n" + finalTextRules

func documentTypeName(w WorkType) string {
	switch w {
	case WorkPractical:
		return "practical assignment (amaliy topshiriq)"
	case WorkReferat:
		return "referat (an academic review essay that summarises and analyses the scholarly literature on the topic)"
	}
	return "independent study work (mustaqil ish)"
}

func documentStyle(w WorkType) string {
	if w == WorkPractical {
		return "applied and hands-on: methods, step-by-step procedures, worked examples, formulas and calculations " +
			"with clear explanations of every step and of the results. If the topic involves programming, you may add " +
			"short code listings as separate paragraphs that start with ``` and end with ``` (keep the line breaks)."
	}
	if w == WorkReferat {
		return "analytical review: key concepts and their definitions, the history of the question, the main scientific approaches " +
			"and viewpoints of scholars, their comparison and critical analysis, generalisations and well-grounded conclusions " +
			"(relevant to Uzbekistan where appropriate). Neutral, scholarly tone; no first-person narrative."
	}
	return "formal academic: definitions and key concepts, classifications, historical background, causes and consequences, " +
		"comparisons, current state and examples (relevant to Uzbekistan where appropriate)."
}

// buildOutlinePrompt reja tuzish topshirig'i.
func buildOutlinePrompt(req DocumentRequest, chapters int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task: plan the structure of a %s.\n", documentTypeName(req.WorkType))
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	b.WriteString(languageRule(req.Language))
	b.WriteString("\n")
	if req.Figures {
		b.WriteString(`Return JSON: {"chapters": [{"title": string, "points": [string, ...], "figure": {"caption": string, "prompt": string}}, ...], "references": [string, ...]}` + "\n\n")
	} else {
		b.WriteString(`Return JSON: {"chapters": [{"title": string, "points": [string, ...]}, ...], "references": [string, ...]}` + "\n\n")
	}
	b.WriteString("Requirements:\n")
	fmt.Fprintf(&b, "- \"chapters\": exactly %d main chapters. The introduction and the conclusion are written separately — do NOT include them.\n", chapters)
	b.WriteString("- Chapter titles are specific, 3-10 words, without numbering.\n")
	b.WriteString("- \"points\": 3-5 short phrases per chapter describing what the chapter must cover. Chapters must not overlap.\n")
	switch req.WorkType {
	case WorkPractical:
		b.WriteString("- The chapters follow the logic of a practical assignment: theoretical basis → methods and tools → step-by-step execution with worked examples → analysis of the results.\n")
	case WorkReferat:
		b.WriteString("- The chapters follow the logic of a referat: theoretical foundations and history of the question → main scientific approaches and viewpoints → current state and practice → problems and prospects.\n")
	default:
		b.WriteString("- Order the chapters logically: from fundamentals and key concepts to details, current state, problems and applications.\n")
	}
	b.WriteString("- \"references\": 5-8 entries in a standard bibliographic format (Author. Title. — City: Publisher, Year.). " +
		"Only real, widely known sources you are confident exist: laws and official documents of the Republic of Uzbekistan, " +
		"well-known textbooks and monographs, official websites (for example lex.uz). " +
		"Never invent authors, titles, years or URLs — if unsure, give fewer entries. " +
		"Each entry is a plain bibliographic record with no comments or notes.\n")
	if req.Figures {
		b.WriteString("- \"figure\": an illustration for the chapter. \"caption\" — a short figure caption (4-10 words) in the language of the work, " +
			"without the word \"figure\" and without numbering. \"prompt\" — an English description (20-40 words) for an image generator: " +
			"a realistic photograph or a clean, modern illustration that visualises the main idea of the chapter. " +
			"The image must contain no text, letters, numbers, charts with labels, logos or named real people.\n")
	}
	writeRequirements(&b, req.Requirements)
	return b.String()
}

// buildSectionPrompt bitta bo'limni yozish topshirig'i. Model butun rejani
// ko'radi — shunda bo'limlar bir-birini takrorlamaydi va mantiqan bog'lanadi.
//
// Bo'limga xos qism (qaysi bo'lim, nimani yoritadi, hajmi) promptning eng
// oxirida: bir ish bo'limlarining (kirish, boblar, xulosa) promptlari undan
// oldingi qismda belgima-belgi bir xil, shuning uchun provayderlarning prompt
// keshi umumiy prefiksni qayta ishlatadi. Shu sababli faqat boblarga tegishli
// qoida ham umumiy "Rules" ro'yxatida emas, bo'limga xos qismda.
func buildSectionPrompt(req DocumentRequest, ol *outline, s section) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are writing one part of a %s.\n", documentTypeName(req.WorkType))
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	b.WriteString(languageRule(req.Language))
	b.WriteString("\n")
	writePlan(&b, ol)

	b.WriteString(`Return JSON: {"paragraphs": [string, ...]}` + "\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Every paragraph is continuous academic prose. No headings, no chapter title, no bullet lists, no markdown.\n")
	b.WriteString("- Stay strictly within this part; do not repeat what belongs to other parts of the plan.\n")
	b.WriteString("\nStyle: ")
	b.WriteString(documentStyle(req.WorkType))
	writeRequirements(&b, req.Requirements)

	// Bo'limga xos qism — doim oxirida.
	fmt.Fprintf(&b, "\n\nWrite now: %s.\n", sectionName(s))
	b.WriteString(sectionBrief(req, s))
	if s.kind == sectionChapter {
		b.WriteString("Do not write an introduction or a conclusion to the whole work here.\n")
	}
	paragraphs := max(2, int(math.Round(float64(s.words)/wordsPerParagraph)))
	fmt.Fprintf(&b, "\nLength: about %d words in total — this is important. Split the text into %d paragraphs of 4-7 sentences each.", s.words, paragraphs)
	return b.String()
}

// writePlan butun ish rejasini yozadi (bo'limlar bir-birini takrorlamasligi uchun).
func writePlan(b *strings.Builder, ol *outline) {
	b.WriteString("Plan of the whole work:\n- Introduction\n")
	for i, ch := range ol.Chapters {
		fmt.Fprintf(b, "- Chapter %d: %s\n", i+1, ch.Title)
	}
	b.WriteString("- Conclusion\n\n")
}

// sectionName bo'limning promptdagi nomi.
func sectionName(s section) string {
	switch s.kind {
	case sectionIntro:
		return "the Introduction"
	case sectionConclusion:
		return "the Conclusion"
	}
	return fmt.Sprintf("Chapter %d %q", s.number, s.title)
}

// sectionBrief bo'lim nimani yoritishi kerakligi (bir yoki bir necha qator).
func sectionBrief(req DocumentRequest, s section) string {
	switch s.kind {
	case sectionIntro:
		if req.WorkType == WorkPractical {
			return "It must cover: the goal of the practical assignment, its tasks, the methods, tools and initial data used, and the expected result.\n"
		}
		return "It must cover: the relevance of the topic, the aim and the tasks of the work, the object and the subject of the study, and a short overview of the chapters.\n"
	case sectionConclusion:
		return "It must summarise the key results of every chapter, their theoretical and practical significance, and give recommendations. Do not introduce new facts.\n"
	}
	if len(s.points) > 0 {
		return fmt.Sprintf("It must cover: %s.\n", strings.Join(s.points, "; "))
	}
	return ""
}

// buildExtendPrompt qisqa chiqqan bo'lim davomini yozish topshirig'i: model
// bo'limning hozirgi matnini ko'radi va uni takrorlamasdan, yetishmayotgan
// hajmda yangi paragraflar bilan davom ettiradi.
func buildExtendPrompt(req DocumentRequest, ol *outline, s section, current []Paragraph, missing int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are expanding one part of a %s.\n", documentTypeName(req.WorkType))
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	b.WriteString(languageRule(req.Language))
	b.WriteString("\n")
	writePlan(&b, ol)

	fmt.Fprintf(&b, "Part to expand: %s.\n", sectionName(s))
	b.WriteString(sectionBrief(req, s))
	b.WriteString("\nCurrent text of this part:\n<<<\n")
	for i, p := range current {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if p.Code {
			b.WriteString("```\n" + p.Text + "\n```")
		} else {
			b.WriteString(p.Text)
		}
	}
	b.WriteString("\n>>>\n\n")

	paragraphs := max(1, int(math.Round(float64(missing)/wordsPerParagraph)))
	how := "deepen the analysis, add explanations, examples and the aspects of this part that are not covered yet"
	switch s.kind {
	case sectionIntro:
		how = "cover the points listed above in more detail"
	case sectionConclusion:
		how = "deepen the generalisations, the significance of the results and the recommendations, without introducing new facts"
	}
	fmt.Fprintf(&b, "This part is too short. Write about %d more words as %d new paragraph(s) of 4-7 sentences each "+
		"that continue the text naturally after its last paragraph: %s.\n", missing, paragraphs, how)
	b.WriteString(`Return JSON with the new paragraphs only: {"paragraphs": [string, ...]}` + "\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Every paragraph is continuous academic prose. No headings, no chapter title, no bullet lists, no markdown.\n")
	b.WriteString("- Do not repeat, paraphrase or summarise what is already written, and do not start the part again.\n")
	b.WriteString("- Stay strictly within this part; do not repeat what belongs to other parts of the plan.\n")
	if s.kind == sectionChapter {
		b.WriteString("- Do not write an introduction or a conclusion to the whole work here.\n")
	}
	b.WriteString("\nStyle: ")
	b.WriteString(documentStyle(req.WorkType))
	writeRequirements(&b, req.Requirements)
	return b.String()
}

// PlanChapters words so'zli hujjatdagi boblar soni (rasmlar sonini oldindan
// hisoblash uchun; GenerateDocument ham shuncha bob so'raydi).
func PlanChapters(words int) int {
	return planBudget(max(words, minDocumentWords)).chapters
}
