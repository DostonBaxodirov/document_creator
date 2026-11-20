package textutil

import (
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in    string
		limit int
		want  string
	}{
		{"", 5, ""},
		{"salom", 10, "salom"},
		{"salom", 5, "salom"}, // aynan limit — o'zgarishsiz
		{"abcdefghij", 4, "abcd…"},
		// Oxirgi bo'sh joy ikkinchi yarmida — so'z chegarasigacha qaytiladi.
		{"Birinchi, ikkinchi; uchinchi", 24, "Birinchi, ikkinchi…"},
		// Bo'sh joy aynan o'rtada (9 > 18/2 emas) — qaytilmaydi.
		{"Birinchi, ikkinchi; uchinchi", 18, "Birinchi, ikkinchi…"},
		// Oxiridagi tinish belgilari olib tashlanadi.
		{"Birinchi, ikkinchi; uchinchi", 19, "Birinchi, ikkinchi…"},
		// Bo'sh joy birinchi yarmida — so'z o'rtasidan kesiladi.
		{"ab cdefghijkl", 10, "ab cdefghi…"},
		{"...........", 3, "…"},
		{"Sun'iy intellekt va ta'lim", 20, "Sun'iy intellekt va…"},
		// O'zbek kirill yozuvi (har bir harf 2 bayt).
		{"Ўзбекистон Республикаси Президенти", 15, "Ўзбекистон…"},
		{"Ўзбекистон Республикаси", 23, "Ўзбекистон Республикаси"},
		{"Мустақиллик", 5, "Муста…"},
		// Emoji (har biri 4 bayt).
		{"🎉🎉🎉🎉🎉", 3, "🎉🎉🎉…"},
		{"Salom 👋🌍 dunyo", 8, "Salom 👋🌍…"},
		{"Taqdimot tayyor 🎉🎉", 17, "Taqdimot tayyor…"},
		// Bo'sh joysiz uzun satr.
		{"https://example.com/juda/uzun/manzil", 12, "https://exam…"},
		{"abc", 0, "…"},
	}
	for _, tc := range cases {
		got := Truncate(tc.in, tc.limit)
		if got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, kutilgan %q", tc.in, tc.limit, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("Truncate(%q, %d) yaroqsiz UTF-8 qaytardi: %q", tc.in, tc.limit, got)
		}
		if n := utf8.RuneCountInString(got); n > tc.limit+1 && got != tc.in {
			t.Errorf("Truncate(%q, %d): %d ta belgi, ko'pi bilan %d kutilgan", tc.in, tc.limit, n, tc.limit+1)
		}
	}
}

func TestTruncateHard(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"", 3, ""},
		{"abcd", 4, "abcd"},
		{"abcdef", 4, "abc…"},
		{"ab", 1, "…"},
		// So'z chegarasi va bo'sh joylar hisobga olinmaydi.
		{"Salom dunyo", 7, "Salom …"},
		// O'zbek kirill yozuvi.
		{"Ўзбекистон", 5, "Ўзбе…"},
		{"Ўзбекистон Республикаси", 12, "Ўзбекистон …"},
		{"йўқ", 3, "йўқ"},
		// Emoji.
		{"👋🌍🎉🚀", 3, "👋🌍…"},
		{"👋🌍🎉", 3, "👋🌍🎉"},
		// Bo'sh joysiz uzun satr.
		{"abcdefghijklmnop", 10, "abcdefghi…"},
	}
	for _, tc := range cases {
		got := TruncateHard(tc.in, tc.n)
		if got != tc.want {
			t.Errorf("TruncateHard(%q, %d) = %q, kutilgan %q", tc.in, tc.n, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("TruncateHard(%q, %d) yaroqsiz UTF-8 qaytardi: %q", tc.in, tc.n, got)
		}
		if n := utf8.RuneCountInString(got); n > tc.n {
			t.Errorf("TruncateHard(%q, %d): %d ta belgi, ko'pi bilan %d kutilgan", tc.in, tc.n, n, tc.n)
		}
	}
}

func TestDigits(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"abc":                 "",
		"8600-1234 5678 9012": "8600123456789012",
		"+998 (90) 123-45-67": "998901234567",
		"8600 **** **** 9012": "86009012",
		"١٢٣":                 "", // faqat ASCII raqamlar
		"５":                   "",
	}
	for in, want := range cases {
		if got := Digits(in); got != want {
			t.Errorf("Digits(%q) = %q, kutilgan %q", in, got, want)
		}
	}
}

func TestLastDigits(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"8600 **** **** 9012", 4, "9012"},
		{"****12", 4, "12"},
		{"", 4, ""},
		{"karta: 8600 1234 5678 9012", 32, "8600123456789012"},
		{"1234", 0, ""},
	}
	for _, tc := range cases {
		if got := LastDigits(tc.in, tc.n); got != tc.want {
			t.Errorf("LastDigits(%q, %d) = %q, kutilgan %q", tc.in, tc.n, got, tc.want)
		}
	}
}

func TestNormalizeApostrophes(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"o'zbek":      "o'zbek",
		"o`zbek":      "o'zbek",
		"O’zbekiston": "O'zbekiston",
		"‘Sun‘iy":     "'Sun'iy",
		"oʻgʻil":      "o'g'il",
		"yoʼq":        "yo'q",
		"йўқ":         "йўқ",
		// Qo'shtirnoq va boshqa belgilar o'zgarmaydi.
		`«ta"lim»′`: `«ta"lim»′`,
	}
	for in, want := range cases {
		if got := NormalizeApostrophes(in); got != want {
			t.Errorf("NormalizeApostrophes(%q) = %q, kutilgan %q", in, got, want)
		}
	}
}
