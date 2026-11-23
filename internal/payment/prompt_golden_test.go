package payment

// prompt_golden_test.go — chekni tekshirish promptlari docs/prompts/receipt_*.txt
// fayllariga yoziladi (docs/PROMPTS.md ularga havola beradi). Koddagi prompt
// fayldan farq qilsa, test yiqiladi. Promptni o'zgartirgandan keyin:
//
//	go test ./internal/payment -run Golden -update

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

var update = flag.Bool("update", false, "docs/prompts/receipt_*.txt fayllarini koddagi promptlar bilan qayta yozish")

const goldenUpdateCmd = "go test ./internal/payment -run Golden -update"

// TestReceiptPromptGolden tizim prompti va foydalanuvchi xabari (namunaviy
// summa va karta bilan) docs/prompts dagi fayllar bilan bir xilligini tekshiradi.
func TestReceiptPromptGolden(t *testing.T) {
	prompts := map[string]string{
		"receipt_system": systemPrompt,
		"receipt_user":   userPrompt(Expectation{Amount: 34000, CardLast4: "1234"}),
	}
	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			checkGolden(t, filepath.Join("..", "..", "docs", "prompts", name+".txt"), prompt)
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
