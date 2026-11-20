package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeDocProvider — hujjat so'rovlariga javob beradigan soxta provayder:
// rejada chapters ta bob qaytaradi, bo'limlarni esa so'ralgan hajmning
// percent foizida yozadi; davom ettirish so'rovida so'ralgan hajmni to'liq beradi.
type fakeDocProvider struct {
	chapters int
	percent  int
	// figures — rejadagi juft indeksli (0, 2, ...) boblarga rasm g'oyasi ham qo'shilsin.
	figures bool

	mu      sync.Mutex
	prompts []string
}

var (
	lengthRe = regexp.MustCompile(`Length: about (\d+) words`)
	moreRe   = regexp.MustCompile(`Write about (\d+) more words`)
)

func (*fakeDocProvider) name() string { return "fake" }

func (f *fakeDocProvider) complete(_ context.Context, t task) (string, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, t.user)
	f.mu.Unlock()

	words := func(n int, tag string) string {
		// Har bir paragraf ~100 so'z; matnlar takrorlanmasin.
		var paras []string
		for n > 0 {
			k := min(n, 100)
			paras = append(paras, strings.TrimSpace(strings.Repeat(tag+" ", k)))
			n -= k
			tag += "x"
		}
		out, _ := json.Marshal(map[string][]string{"paragraphs": paras})
		return string(out)
	}
	switch {
	case strings.HasPrefix(t.user, "Task: plan the structure"):
		var chs []map[string]any
		for i := range f.chapters {
			ch := map[string]any{"title": fmt.Sprintf("Bob nomi %d", i+1), "points": []string{"a", "b"}}
			if f.figures && i%2 == 0 {
				ch["figure"] = map[string]string{"caption": fmt.Sprintf("Rasm izohi %d", i+1), "prompt": fmt.Sprintf("Illustration of chapter %d", i+1)}
			}
			chs = append(chs, ch)
		}
		out, _ := json.Marshal(map[string]any{"chapters": chs, "references": []string{"Muallif A. Kitob. — Toshkent, 2020."}})
		return string(out), nil
	case strings.HasPrefix(t.user, "You are expanding"):
		n, _ := strconv.Atoi(moreRe.FindStringSubmatch(t.user)[1])
		return words(n, "davom"), nil
	default:
		n, _ := strconv.Atoi(lengthRe.FindStringSubmatch(t.user)[1])
		return words(n*f.percent/100, "matn"), nil
	}
}

func (f *fakeDocProvider) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.prompts {
		if strings.HasPrefix(p, prefix) {
			n++
		}
	}
	return n
}

func testClient(p provider) *client {
	return &client{p: p, timeout: time.Minute, maxRetries: 0, parallel: 3, log: zap.NewNop()}
}

// TestDocumentReachesRequestedLength — model bo'limlarni qisqa (70%) yozsa,
// davomi yozdiriladi va hujjat so'ralgan hajmga yetadi.
func TestDocumentReachesRequestedLength(t *testing.T) {
	const words = 3000
	b := planBudget(words)
	f := &fakeDocProvider{chapters: b.chapters, percent: 70}
	var lastDone, lastTotal int
	doc, err := testClient(f).GenerateDocument(context.Background(), DocumentRequest{
		WorkType: WorkIndependent, Topic: "Raqamli iqtisodiyot", Words: words,
		OnProgress: func(done, total int) { lastDone, lastTotal = done, total },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.wordCount(); got < words*sectionFillPercent/100 {
		t.Errorf("hujjat hajmi %d so'z, kamida %d bo'lishi kerak", got, words*sectionFillPercent/100)
	}
	if n := f.count("You are expanding"); n != b.chapters+2 {
		t.Errorf("barcha qisqa bo'limlar (%d) davom ettirilishi kerak, so'rovlar: %d", b.chapters+2, n)
	}
	if lastDone != lastTotal {
		t.Errorf("jarayon oxirida done == total bo'lishi kerak: %d/%d", lastDone, lastTotal)
	}

	// Yetarli hajmda yozilgan bo'limlar davom ettirilmaydi.
	f = &fakeDocProvider{chapters: b.chapters, percent: 100}
	if _, err := testClient(f).GenerateDocument(context.Background(), DocumentRequest{WorkType: WorkIndependent, Topic: "Mavzu", Words: words}); err != nil {
		t.Fatal(err)
	}
	if n := f.count("You are expanding"); n != 0 {
		t.Errorf("to'liq bo'limlar davom ettirilmasligi kerak, so'rovlar: %d", n)
	}
}

// TestFewerChaptersGetMoreWords — model kamroq bob qaytarsa, boblar hajmi
// qayta taqsimlanadi.
func TestFewerChaptersGetMoreWords(t *testing.T) {
	const words = 3000
	b := planBudget(words)
	if b.chapters < 3 {
		t.Fatalf("test uchun kamida 3 bob kerak, reja: %+v", b)
	}
	f := &fakeDocProvider{chapters: 2, percent: 100}
	if _, err := testClient(f).GenerateDocument(context.Background(), DocumentRequest{WorkType: WorkIndependent, Topic: "Mavzu", Words: words}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("Length: about %d words", b.chapter*b.chapters/2)
	found := 0
	for _, p := range f.prompts {
		if strings.Contains(p, "Write now: Chapter") && strings.Contains(p, want) {
			found++
		}
	}
	if found != 2 {
		t.Errorf("2 ta bobning har biri %q so'rashi kerak edi (topildi %d)", want, found)
	}
}

// TestOnOutlineBeforeSections — OnOutline reja tayyor bo'lgach, bo'limlar
// yozilishidan oldin bir marta chaqiriladi: har bir reja bobiga bitta
// element, tartibi Document.Chapters bilan bir xil.
func TestOnOutlineBeforeSections(t *testing.T) {
	const words = 3000
	b := planBudget(words)
	// Model rejadagidan kam bob qaytaradi — element soni haqiqiy reja boblariga teng bo'lishi kerak.
	f := &fakeDocProvider{chapters: b.chapters - 1, percent: 100, figures: true}
	var (
		calls          int
		figures        []*FigureIdea
		sectionsBefore int
	)
	doc, err := testClient(f).GenerateDocument(context.Background(), DocumentRequest{
		WorkType: WorkReferat, Topic: "Ipak yo'li", Words: words, Figures: true,
		OnOutline: func(figs []*FigureIdea) {
			calls++
			figures = figs
			sectionsBefore = f.count("You are writing")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("OnOutline bir marta chaqirilishi kerak, chaqiruvlar: %d", calls)
	}
	if sectionsBefore != 0 {
		t.Errorf("OnOutline bo'limlar yozilishidan oldin chaqirilishi kerak, oldin yozilgan: %d", sectionsBefore)
	}
	if len(figures) != b.chapters-1 || len(figures) != len(doc.Chapters) {
		t.Fatalf("elementlar soni reja boblari soniga (%d) teng bo'lishi kerak: %d (hujjatda %d bob)",
			b.chapters-1, len(figures), len(doc.Chapters))
	}
	for i, fig := range figures {
		if (fig != nil) != (i%2 == 0) {
			t.Errorf("%d-bob: rasm g'oyasi faqat juft indeksli boblarda bo'lishi kerak: %+v", i, fig)
			continue
		}
		if fig != nil && (doc.Chapters[i].Figure == nil || *fig != *doc.Chapters[i].Figure) {
			t.Errorf("%d-bob: OnOutline g'oyasi hujjat bobinikidan farq qiladi: %+v / %+v", i, fig, doc.Chapters[i].Figure)
		}
		if fig != nil && fig.Caption != fmt.Sprintf("Rasm izohi %d", i+1) {
			t.Errorf("%d-bob: boblar tartibi saqlanmagan: %q", i, fig.Caption)
		}
	}

	// Rasmlar so'ralmagan — reja g'oya qaytarsa ham barcha elementlar nil.
	calls, figures = 0, nil
	if _, err := testClient(f).GenerateDocument(context.Background(), DocumentRequest{
		WorkType: WorkReferat, Topic: "Ipak yo'li", Words: words,
		OnOutline: func(figs []*FigureIdea) { calls++; figures = figs },
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(figures) != b.chapters-1 {
		t.Fatalf("OnOutline: %d chaqiruv, %d element", calls, len(figures))
	}
	for i, fig := range figures {
		if fig != nil {
			t.Errorf("%d-bob: rasmlar so'ralmaganda g'oya nil bo'lishi kerak: %+v", i, fig)
		}
	}
}

func TestExtendPromptAndAppend(t *testing.T) {
	ol := &outline{Chapters: []outlineChapter{{Title: "Bob", Points: []string{"tarix", "hozirgi holat"}}}}
	s := section{kind: sectionChapter, number: 1, title: "Bob", points: []string{"tarix", "hozirgi holat"}, words: 600}
	current := []Paragraph{{Text: "Birinchi paragraf."}, {Text: "print(1)", Code: true}}
	p := buildExtendPrompt(DocumentRequest{WorkType: WorkPractical, Topic: "Mavzu"}, ol, s, current, 250)
	for _, want := range []string{
		`Part to expand: Chapter 1 "Bob".`, "It must cover: tarix; hozirgi holat.", "Birinchi paragraf.",
		"```\nprint(1)\n```", "Write about 250 more words as 2 new paragraph(s)", "Do not repeat, paraphrase or summarise",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("davom ettirish promptida %q yo'q:\n%s", want, p)
		}
	}

	got := appendNew([]Paragraph{{Text: "A"}, {Text: "B"}}, []Paragraph{{Text: "b"}, {Text: "C"}})
	if len(got) != 3 || got[2].Text != "C" {
		t.Errorf("takrorlangan paragraf tashlab yuborilishi kerak: %+v", got)
	}

	parts := []section{{words: 500}, {words: 500}, {words: 100}}
	texts := [][]Paragraph{
		{{Text: strings.Repeat("a ", 300)}}, // 60% — davom ettiriladi
		{{Text: strings.Repeat("a ", 470)}}, // 94% — yetarli
		{{Text: strings.Repeat("a ", 60)}},  // 40 so'z yetishmaydi — arzimas
	}
	if got := shortSections(parts, texts); len(got) != 1 || got[0] != 0 {
		t.Errorf("faqat birinchi bo'lim qisqa: %v", got)
	}
}
