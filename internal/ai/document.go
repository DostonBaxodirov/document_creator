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
	"google.golang.org/genai"
)

// DocumentRequest — mustaqil ish yoki amaliy topshiriq generatsiyasi uchun ma'lumotlar.
type DocumentRequest struct {
	WorkType WorkType // WorkIndependent yoki WorkPractical
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
}

// Document — AI yozgan to'liq hujjat matni.
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
	sectionMinPercent  = 55   // bo'lim so'ralgan hajmning kamida shuncha foizi bo'lishi kerak
	maxParagraphRunes  = 5000 // bitta paragrafning maksimal uzunligi
	maxCodeLines       = 80
	maxPointsPerTopic  = 6
	maxReferences      = 10
	maxReferenceRunes  = 400
	maxChapterTitleLen = 150
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
		schema: outlineSchema(),
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

	// 2. Bo'limlar.
	parts := make([]section, 0, len(ol.Chapters)+2)
	parts = append(parts, section{kind: sectionIntro, words: b.intro})
	for i, ch := range ol.Chapters {
		parts = append(parts, section{kind: sectionChapter, number: i + 1, title: ch.Title, points: ch.Points, words: b.chapter})
	}
	parts = append(parts, section{kind: sectionConclusion, words: b.conclusion})

	texts, err := c.writeSections(ctx, req, ol, parts, progress)
	if err != nil {
		return nil, err
	}

	doc := &Document{
		Introduction: texts[0],
		Conclusion:   texts[len(texts)-1],
		References:   ol.References,
		Chapters:     make([]Chapter, 0, len(ol.Chapters)),
	}
	for i, ch := range ol.Chapters {
		doc.Chapters = append(doc.Chapters, Chapter{Title: ch.Title, Paragraphs: texts[i+1]})
	}

	c.log.Info("AI document generated",
		zap.String("work_type", string(req.WorkType)),
		zap.Int("chapters", len(doc.Chapters)),
		zap.Int("words", doc.wordCount()),
		zap.Int("requested_words", req.Words),
		zap.Int("references", len(doc.References)))
	return doc, nil
}

// writeSections bo'limlarni cheklangan parallellik bilan yozadi. Birorta bo'lim
// xato bilan tugasa, qolganlari to'xtatiladi va birinchi xato qaytariladi.
func (c *client) writeSections(ctx context.Context, req DocumentRequest, ol *outline,
	parts []section, progress *progressReporter) ([][]Paragraph, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		results  = make([][]Paragraph, len(parts))
		sem      = make(chan struct{}, c.parallel)
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

	for i, part := range parts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Goroutine ichidagi panic tashqi recover'ga yetib bormaydi —
			// shu yerda ushlab, oddiy xatoga aylantiramiz.
			defer func() {
				if r := recover(); r != nil {
					fail(fmt.Errorf("ai: %s bo'limida panic: %v", part.label(), r))
				}
			}()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			paras, err := generateJSON(ctx, c, "document_"+part.kind.String(), task{
				system: documentSystemPrompt,
				user:   buildSectionPrompt(req, ol, part),
				schema: sectionSchema(),
			}, part.words*sectionMinPercent/100, func(raw string) ([]Paragraph, int, error) {
				return parseSection(raw, part.title)
			})
			if err != nil {
				fail(fmt.Errorf("%s: %w", part.label(), err))
				return
			}
			results[i] = paras
			progress.advance()
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
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

// ---------------------------------------------------------------------------
// Reja (outline)
// ---------------------------------------------------------------------------

type outlineChapter struct {
	Title  string   `json:"title"`
	Points []string `json:"points"`
}

type outline struct {
	Chapters   []outlineChapter `json:"chapters"`
	References []string         `json:"references"`
}

func outlineSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"chapters": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"title":  {Type: genai.TypeString},
						"points": stringArraySchema(),
					},
					Required:         []string{"title", "points"},
					PropertyOrdering: []string{"title", "points"},
				},
			},
			"references": stringArraySchema(),
		},
		Required:         []string{"chapters", "references"},
		PropertyOrdering: []string{"chapters", "references"},
	}
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

func sectionSchema() *genai.Schema {
	return &genai.Schema{
		Type:             genai.TypeObject,
		Properties:       map[string]*genai.Schema{"paragraphs": stringArraySchema()},
		Required:         []string{"paragraphs"},
		PropertyOrdering: []string{"paragraphs"},
	}
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

const documentSystemPrompt = `You are an expert academic writer who prepares independent study works and practical assignments for university students in Uzbekistan.

Strict output rules:
- Respond with ONE valid JSON object and nothing else: no markdown fences around the JSON, no comments, no extra text.
- Content must be accurate, neutral and academic. Never invent statistics, quotations, URLs or bibliographic references.
- Do not use markdown (*, **, #) or emojis.
` + languageRules + "\n" + finalTextRules

func documentTypeName(w WorkType) string {
	if w == WorkPractical {
		return "practical assignment (amaliy topshiriq)"
	}
	return "independent study work (mustaqil ish)"
}

func documentStyle(w WorkType) string {
	if w == WorkPractical {
		return "applied and hands-on: methods, step-by-step procedures, worked examples, formulas and calculations " +
			"with clear explanations of every step and of the results. If the topic involves programming, you may add " +
			"short code listings as separate paragraphs that start with ``` and end with ``` (keep the line breaks)."
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
	b.WriteString(`Return JSON: {"chapters": [{"title": string, "points": [string, ...]}, ...], "references": [string, ...]}` + "\n\n")
	b.WriteString("Requirements:\n")
	fmt.Fprintf(&b, "- \"chapters\": exactly %d main chapters. The introduction and the conclusion are written separately — do NOT include them.\n", chapters)
	b.WriteString("- Chapter titles are specific, 3-10 words, without numbering.\n")
	b.WriteString("- \"points\": 3-5 short phrases per chapter describing what the chapter must cover. Chapters must not overlap.\n")
	if req.WorkType == WorkPractical {
		b.WriteString("- The chapters follow the logic of a practical assignment: theoretical basis → methods and tools → step-by-step execution with worked examples → analysis of the results.\n")
	} else {
		b.WriteString("- Order the chapters logically: from fundamentals and key concepts to details, current state, problems and applications.\n")
	}
	b.WriteString("- \"references\": 5-8 entries in a standard bibliographic format (Author. Title. — City: Publisher, Year.). " +
		"Only real, widely known sources you are confident exist: laws and official documents of the Republic of Uzbekistan, " +
		"well-known textbooks and monographs, official websites (for example lex.uz). " +
		"Never invent authors, titles, years or URLs — if unsure, give fewer entries. " +
		"Each entry is a plain bibliographic record with no comments or notes.\n")
	writeRequirements(&b, req.Requirements)
	return b.String()
}

// buildSectionPrompt bitta bo'limni yozish topshirig'i. Model butun rejani
// ko'radi — shunda bo'limlar bir-birini takrorlamaydi va mantiqan bog'lanadi.
func buildSectionPrompt(req DocumentRequest, ol *outline, s section) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are writing one part of a %s.\n", documentTypeName(req.WorkType))
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	b.WriteString(languageRule(req.Language))
	b.WriteString("\n")

	b.WriteString("Plan of the whole work:\n- Introduction\n")
	for i, ch := range ol.Chapters {
		fmt.Fprintf(&b, "- Chapter %d: %s\n", i+1, ch.Title)
	}
	b.WriteString("- Conclusion\n\n")

	switch s.kind {
	case sectionIntro:
		b.WriteString("Write now: the Introduction.\n")
		if req.WorkType == WorkPractical {
			b.WriteString("It must cover: the goal of the practical assignment, its tasks, the methods, tools and initial data used, and the expected result.\n")
		} else {
			b.WriteString("It must cover: the relevance of the topic, the aim and the tasks of the work, the object and the subject of the study, and a short overview of the chapters.\n")
		}
	case sectionConclusion:
		b.WriteString("Write now: the Conclusion.\n")
		b.WriteString("It must summarise the key results of every chapter, their theoretical and practical significance, and give recommendations. Do not introduce new facts.\n")
	default:
		fmt.Fprintf(&b, "Write now: Chapter %d %q.\n", s.number, s.title)
		if len(s.points) > 0 {
			fmt.Fprintf(&b, "It must cover: %s.\n", strings.Join(s.points, "; "))
		}
	}

	paragraphs := max(2, int(math.Round(float64(s.words)/wordsPerParagraph)))
	fmt.Fprintf(&b, "\nLength: about %d words in total — this is important. Split the text into %d paragraphs of 4-7 sentences each.\n", s.words, paragraphs)
	b.WriteString(`Return JSON: {"paragraphs": [string, ...]}` + "\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Every paragraph is continuous academic prose. No headings, no chapter title, no bullet lists, no markdown.\n")
	b.WriteString("- Stay strictly within this part; do not repeat what belongs to other parts of the plan.\n")
	if s.kind == sectionChapter {
		b.WriteString("- Do not write an introduction or a conclusion to the whole work here.\n")
	}
	b.WriteString("\nStyle: ")
	b.WriteString(documentStyle(req.WorkType))
	writeRequirements(&b, req.Requirements)
	return b.String()
}
