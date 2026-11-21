package ai

// prompts_golden_test.go — barcha promptlar namunaviy ma'lumotlar bilan
// docs/prompts/*.txt fayllariga yoziladi (docs/PROMPTS.md ularga havola
// beradi). Oddiy ishga tushirishda koddagi prompt fayldan farq qilsa, test
// yiqiladi. Promptni o'zgartirgandan keyin fayllarni yangilash:
//
//	go test ./internal/ai -run Golden -update

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

var update = flag.Bool("update", false, "docs/prompts/*.txt fayllarini koddagi promptlar bilan qayta yozish")

// goldenDir — promptlar fayllari papkasi (paket papkasiga nisbatan).
var goldenDir = filepath.Join("..", "..", "docs", "prompts")

const goldenUpdateCmd = "go test ./internal/ai -run Golden -update"

// Namunaviy ma'lumotlar (docs/PROMPTS.md izohlari shularga tayanadi).
const (
	goldenLanguage = "Uzbek (Latin script)"

	goldenDeckTopic        = "Amir Temur davlatining tashkil topishi va boshqaruv tizimi"
	goldenDeckRequirements = "Ko'proq tarixiy sanalar bo'lsin, ranglar iliq bo'lsin"
	goldenThemeDeckTopic   = "Sun'iy intellektning ta'limdagi o'rni"

	goldenDocTopic        = "Raqamli iqtisodiyot"
	goldenDocRequirements = "O'zbekiston misolida"
	goldenPracticalTopic  = "Python dasturlash tilida saralash algoritmlari"
)

// goldenBrief — AI maxsus dizayni qisqachasi (dizayn konsepsiyasi javobidan).
var goldenBrief = &DesignBrief{
	Name:       "Temuriylar oltini",
	Mood:       "elegant, historical, warm, refined",
	ImageStyle: "warm historical travel photography, golden sunlight, earthy terracotta and sand tones",
}

// goldenCustomDeck — maxsus dizayndagi taqdimot so'rovi (bot bilan bir xil:
// barcha maketlar, rasmlar, reja va xulosa sarlavhalari tanlangan tilda).
func goldenCustomDeck(slides int, requirements string) PresentationRequest {
	return PresentationRequest{
		Topic:           goldenDeckTopic,
		SlideCount:      slides,
		Images:          true,
		Signature:       []string{LayoutTimeline, LayoutPyramid, LayoutStatement},
		Language:        goldenLanguage,
		Requirements:    requirements,
		OutlineTitle:    "Reja",
		ConclusionTitle: "Xulosa",
		Custom:          goldenBrief,
	}
}

// goldenDeckPlan — katta taqdimot rejasi (reja promptiga javob). Rasm
// tavsiflari rejada yozilgan, shuning uchun guruh promptida ular so'ralmaydi.
func goldenDeckPlan() *deckOutline {
	slides := []plannedSlide{
		{Layout: LayoutAgenda, Title: "Reja", Focus: "Taqdimotning asosiy bo'limlari"},
		{Layout: LayoutDefinition, Title: "Davlatning tashkil topishi", Focus: "1370-yilda Samarqandda davlat tashkil topishi"},
		{Layout: LayoutTimeline, Title: "Hokimiyatga kelish yo'li", Focus: "1360-1370 yillardagi asosiy voqealar"},
		{Layout: LayoutPyramid, Title: "Boshqaruv ierarxiyasi", Focus: "Oliy hukmdordan viloyat hokimlarigacha"},
		{Layout: LayoutHub, Title: "Devon va vazirlar", Focus: "Markaziy boshqaruv idoralari"},
		{Layout: LayoutProcess, Title: "Harbiy tizim", Focus: "Qo'shin tuzilishi va yurishlar tayyorgarligi"},
		{Layout: LayoutStatement, Title: "Tuzuklar", Focus: "Temur tuzuklarining boshqaruvdagi o'rni"},
		{Layout: LayoutCards, Title: "Iqtisodiyot va savdo", Focus: "Buyuk ipak yo'li va hunarmandchilik"},
		{Layout: LayoutComparison, Title: "Ichki va tashqi siyosat", Focus: "Ichki barqarorlik va tashqi yurishlar"},
		{Layout: LayoutConclusion, Title: "Xulosa", Focus: "Asosiy xulosalar"},
	}
	for i := range slides {
		slides[i].N = i + 1
		slides[i].Image = "Samarkand architecture in warm sunlight"
	}
	return &deckOutline{
		Title:    goldenDeckTopic,
		Subtitle: "Davlat qurilishi va boshqaruv tamoyillari",
		Image:    "The Registan ensemble in Samarkand at sunrise",
		Slides:   slides,
	}
}

// goldenDocPlan — mustaqil ish rejasi (reja promptiga javob).
func goldenDocPlan() *outline {
	return &outline{Chapters: []outlineChapter{
		{Title: "Raqamli iqtisodiyot tushunchasi", Points: []string{"asosiy ta'riflar", "tarkibiy qismlar", "rivojlanish bosqichlari"}},
		{Title: "O'zbekistonda raqamlashtirish", Points: []string{"davlat dasturlari", "elektron xizmatlar", "raqamli to'lovlar"}},
		{Title: "Muammolar va istiqbollar", Points: []string{"raqamli tengsizlik", "kiberxavfsizlik", "rivojlanish yo'nalishlari"}},
	}}
}

// goldenPracticalPlan — amaliy topshiriq rejasi.
func goldenPracticalPlan() *outline {
	return &outline{Chapters: []outlineChapter{
		{Title: "Saralash algoritmlarining nazariy asoslari", Points: []string{"saralash masalasi", "murakkablik baholari"}},
		{Title: "Algoritmlarni amalga oshirish", Points: []string{"pufakchali saralash", "tez saralash"}},
		{Title: "Natijalar tahlili", Points: []string{"tezlikni o'lchash", "algoritmlarni taqqoslash"}},
	}}
}

// goldenPrompts — fayl nomi (kengaytmasiz) → prompt matni.
func goldenPrompts() map[string]string {
	themeDeck := PresentationRequest{
		Topic:      goldenThemeDeckTopic,
		SlideCount: 6,
		Images:     true,
		Layouts: []string{LayoutAgenda, LayoutBullets, LayoutCards, LayoutComparison, LayoutProcess, LayoutFacts,
			LayoutDefinition, LayoutConclusion, LayoutTimeline, LayoutMatrix, LayoutHub},
		Signature:       []string{LayoutMatrix, LayoutTimeline, LayoutHub},
		Language:        goldenLanguage,
		OutlineTitle:    "Reja",
		ConclusionTitle: "Xulosa",
	}
	design := DesignRequest{Topic: goldenDeckTopic, Language: goldenLanguage, Requirements: goldenDeckRequirements}
	docDesign := DesignRequest{Topic: goldenDocTopic, Language: goldenLanguage, Requirements: goldenDocRequirements}

	independent := DocumentRequest{WorkType: WorkIndependent, Topic: goldenDocTopic, Language: goldenLanguage, Requirements: goldenDocRequirements}
	practical := DocumentRequest{WorkType: WorkPractical, Topic: goldenPracticalTopic, Language: goldenLanguage}
	referat := DocumentRequest{WorkType: WorkReferat, Topic: goldenDocTopic, Language: goldenLanguage, Figures: true}
	docPlan, practicalPlan := goldenDocPlan(), goldenPracticalPlan()
	chapter := func(ol *outline, n, words int) section {
		ch := ol.Chapters[n-1]
		return section{kind: sectionChapter, number: n, title: ch.Title, points: ch.Points, words: words}
	}

	return map[string]string{
		// Taqdimot.
		"presentation_system":      presentationSystemPrompt,
		"presentation_deck":        buildDeckPrompt(themeDeck),
		"presentation_deck_custom": buildDeckPrompt(goldenCustomDeck(5, "")),
		"presentation_outline":     buildDeckOutlinePrompt(goldenCustomDeck(10, goldenDeckRequirements)),
		"presentation_slides":      buildSlidesBatchPrompt(goldenCustomDeck(10, goldenDeckRequirements), goldenDeckPlan(), 5, 10),

		// AI maxsus dizayn.
		"design_system":     designSystemPrompt,
		"design_user":       buildDesignPrompt(design),
		"doc_design_system": docDesignSystemPrompt,
		"doc_design_user":   buildDocDesignPrompt(docDesign),

		// Mustaqil ish, referat, amaliy topshiriq.
		"document_system":            documentSystemPrompt,
		"document_outline":           buildOutlinePrompt(independent, 3),
		"document_outline_practical": buildOutlinePrompt(practical, 3),
		"document_outline_figures":   buildOutlinePrompt(referat, 3),
		"document_introduction":      buildSectionPrompt(independent, docPlan, section{kind: sectionIntro, words: 350}),
		"document_chapter":           buildSectionPrompt(independent, docPlan, chapter(docPlan, 1, 800)),
		"document_conclusion":        buildSectionPrompt(independent, docPlan, section{kind: sectionConclusion, words: 350}),
		"document_chapter_practical": buildSectionPrompt(practical, practicalPlan, chapter(practicalPlan, 2, 800)),
		"document_extend": buildExtendPrompt(independent, docPlan, chapter(docPlan, 2, 800),
			[]Paragraph{{Text: "(bo'limning hozirgi paragraflari)"}}, 260),
	}
}

// TestPromptsGolden koddagi promptlar docs/prompts/*.txt bilan bir xilligini
// tekshiradi (-update bilan — fayllarni qayta yozadi).
func TestPromptsGolden(t *testing.T) {
	for name, prompt := range goldenPrompts() {
		t.Run(name, func(t *testing.T) {
			checkGolden(t, filepath.Join(goldenDir, name+".txt"), prompt)
		})
	}
}

// checkGolden prompt matnini golden fayl bilan solishtiradi; -update bilan
// faylni qayta yozadi.
func checkGolden(t *testing.T, path, prompt string) {
	t.Helper()
	got := prompt + "\n"
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v\nfaylni yaratish: %s", err, goldenUpdateCmd)
	}
	if string(want) != got {
		t.Errorf("%s koddagi promptdan farq qiladi — %s\nprompt ataylab o'zgartirilgan bo'lsa, faylni yangilang: %s",
			path, goldenDiff(string(want), got), goldenUpdateCmd)
	}
}

// goldenDiff birinchi farq qiladigan joyni ko'rsatadi: qator va ustun raqami,
// fayldagi va koddagi matn bo'lagi.
func goldenDiff(want, got string) string {
	w, g := strings.Split(strings.TrimSuffix(want, "\n"), "\n"), strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	for i := 0; i < max(len(w), len(g)); i++ {
		wl, wok := lineAt(w, i)
		gl, gok := lineAt(g, i)
		if wok && gok && wl == gl {
			continue
		}
		col := commonPrefixRunes(wl, gl)
		return fmt.Sprintf("%d-qator, %d-ustun (faylda %d qator, kodda %d qator):\n  faylda: %s\n  kodda:  %s",
			i+1, col+1, len(w), len(g), lineSnippet(wl, wok, col), lineSnippet(gl, gok, col))
	}
	return "farq faqat fayl oxiridagi qator ko'chirishida"
}

func lineAt(lines []string, i int) (string, bool) {
	if i < len(lines) {
		return lines[i], true
	}
	return "", false
}

// commonPrefixRunes ikki satrning umumiy boshlanishi uzunligi (belgilarda).
func commonPrefixRunes(a, b string) int {
	n := 0
	for a != "" && b != "" {
		ra, sa := utf8.DecodeRuneInString(a)
		rb, sb := utf8.DecodeRuneInString(b)
		if ra != rb {
			break
		}
		a, b, n = a[sa:], b[sb:], n+1
	}
	return n
}

// lineSnippet qatorning col-belgi atrofidagi bo'lagi (uzun qatorlar qisqartiriladi).
func lineSnippet(line string, ok bool, col int) string {
	if !ok {
		return "(qator yo'q)"
	}
	const before, after = 30, 50
	r := []rune(line)
	from, to := max(0, col-before), min(len(r), col+after)
	s := fmt.Sprintf("%q", string(r[from:to]))
	if from > 0 {
		s = "…" + s
	}
	if to < len(r) {
		s += "…"
	}
	return s
}
