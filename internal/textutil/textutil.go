// Package textutil bir nechta paketda kerak bo'ladigan kichik matn
// yordamchilari: qisqartirish, raqamlarni ajratib olish va apostroflarni
// bitta ko'rinishga keltirish.
package textutil

import (
	"strings"
	"unicode/utf8"
)

// Truncate matnni so'z chegarasida qirqadi va "…" qo'shadi. Matn limit ta
// belgidan uzun bo'lsa, limit ta belgigacha kesiladi; oxirgi bo'sh joy kesilgan
// qismning ikkinchi yarmida bo'lsa, shu joygacha qisqartiriladi, oxiridagi
// " ,.;:-" belgilari olib tashlanadi.
func Truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	cut := string([]rune(s)[:limit])
	if i := strings.LastIndexByte(cut, ' '); i > len(cut)/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-") + "…"
}

// TruncateHard matnni so'z chegarasiga qaramay n ta belgigacha qisqartiradi:
// uzun matndan dastlabki n-1 ta belgi va "…" qoladi.
func TruncateHard(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// Digits satrdan faqat ASCII raqamlarni (0-9) qoldiradi.
func Digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LastDigits satrdagi raqamlarning oxirgi n tasini qaytaradi.
func LastDigits(s string, n int) string {
	d := Digits(s)
	if len(d) > n {
		d = d[len(d)-n:]
	}
	return d
}

// apostrophes — o'zbek tilidagi apostrof variantlari (o', g', sun'iy ...).
var apostrophes = strings.NewReplacer("`", "'", "’", "'", "‘", "'", "ʻ", "'", "ʼ", "'")

// NormalizeApostrophes apostrof variantlarini (` ’ ‘ ʻ ʼ) oddiy "'" ga keltiradi.
func NormalizeApostrophes(s string) string {
	return apostrophes.Replace(s)
}
