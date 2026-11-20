package ai

import (
	"regexp"
	"strings"

	"telegram-presentation-bot/internal/textutil"
)

// Tayyor hujjat (yoki taqdimot) haqiqiy, topshirishga tayyor ish ko'rinishida
// bo'lishi kerak. Modellar esa ba'zan matnga xizmat gaplarini qo'shib yuboradi:
//
//	"Eslatma: statistik ma'lumotlarni tekshirib chiqing."
//	"Ushbu matn sun'iy intellekt yordamida tayyorlandi."
//	"Albatta! Quyida siz uchun kirish qismini keltiraman."
//	"[Ismingiz] tomonidan bajarildi."
//
// Promptda bular taqiqlangan, bu yerda esa himoyaning ikkinchi qatlami:
// bunday GAPLAR (paragraf emas) matndan olib tashlanadi. Mavzuning o'zi sun'iy
// intellekt haqida bo'lishi mumkin, shuning uchun "sun'iy intellekt" so'zi
// emas, faqat matnning o'ziga yoki o'quvchiga ishora qiluvchi aniq iboralar
// qidiriladi.

// apos — o'zbek tilidagi apostrof variantlari (o', g', sun'iy ...).
const apos = "['’ʻʼ‘`]"

var metaPatterns = []*regexp.Regexp{
	// O'quvchiga qaratilgan izoh va eslatmalar.
	regexp.MustCompile(`(?i)^\s*(eslatma|izoh|diqqat|ogohlantirish|muhim\s+eslatma|примечание|важно|внимание|note|disclaimer|warning)\s*[:!]`),

	// Model o'zi haqida: "Men sun'iy intellekt sifatida...", "As an AI...".
	regexp.MustCompile(`(?i)\bmen\s+(bir\s+)?(sun` + apos + `iy\s+intellekt|til\s+modeli|ai\b|chat-?bot)`),
	regexp.MustCompile(`(?i)(til\s+modeli|sun` + apos + `iy\s+intellekt|\bai\b|chat-?bot)\s+sifatida\s+(men|menda|mening|men\s+)`),
	regexp.MustCompile(`(?i)\bas\s+an?\s+(ai|artificial\s+intelligence|language\s+model)\b|как\s+(языковая\s+модель|ии\b|искусственный\s+интеллект)`),

	// Matn qanday yaratilgani haqida: "Ushbu matn/mustaqil ish ... AI yordamida tayyorlandi".
	regexp.MustCompile(`(?i)\b(ushbu|bu|mazkur|quyidagi)\s+(matn|hujjat|maqola|javob|mustaqil\s+ish|amaliy\s+(ish|topshiriq)|taqdimot|slayd\w*)\b[^.!?]{0,80}?` +
		`(sun` + apos + `iy\s+intellekt|\bai\b|chatgpt|neyron\s*tarmoq|til\s+modeli|avtomatik)[^.!?]{0,20}?` +
		`(yordamida|tomonidan|vositasida|orqali)\s+(yozil|tayyorla|yaratil|generatsiya|ishlab\s+chiqil)`),
	regexp.MustCompile(`(?i)\b(generated\s+by|written\s+by\s+(an?\s+)?ai|сгенерирован\w*)\b`),

	// Suhbat uslubidagi kirish gaplari: "Albatta! Mana siz uchun...", yolg'iz "Albatta!".
	regexp.MustCompile(`(?i)^\s*(albatta|bajonidil|mana|quyida|sure|certainly|of\s+course|here\s+is|here's|конечно|вот)\b[!,.:]?[^.!?]{0,80}` +
		`(siz\s+uchun|sizga|keltiraman|yozib\s+beraman|yozib\s+berdim|tayyorladim|here\s+is|here's|для\s+вас)`),
	regexp.MustCompile(`(?i)^\s*(albatta|bajonidil|sure|certainly|of\s+course|конечно)\s*[!.]+\s*$`),

	// O'quvchiga "tekshiring / o'zgartiring" maslahatlari: "raqamlarni tekshiring",
	// "manbalarni tekshirib chiqing", "ma'lumotlarni aniqlashtirish tavsiya etiladi".
	// Faqat ma'lumot/manba/matn haqida — amaliy ishdagi "natijani tekshiring" kabi
	// buyruqlar tegilmaydi.
	regexp.MustCompile(`(?i)(ma` + apos + `lumot|manba|adabiyot|matn|fakt|statistik|iqtibos|raqam)\p{L}*[^.!?]{0,40}?` +
		`(tekshir|aniqlashtir|yangila|almashtir|o` + apos + `zgartir)\p{L}*` +
		`(\s+(chiq|ko` + apos + `r|ol|qo` + apos + `y)\p{L}*)?` +
		`(ing\b|ingiz\b|\s+tavsiya)`),
	regexp.MustCompile(`(?i)\b(o` + apos + `z\s+ma` + apos + `lumotlaringiz|ismingiz|familiyangiz|ehtiyojingizga|o` + apos + `qituvchingiz|please\s+(verify|check|replace)|double-check|проверьте|уточните)`),

	// To'ldirilmagan joylar: [Ism], [yil], ____ (lekin [1], [2, 3] kabi iqtiboslar emas).
	regexp.MustCompile(`\[[^\]\d\s][^\]]{0,60}\]|_{3,}`),
	regexp.MustCompile(`\b(19|20)?XX\b`),
}

// isMeta matnda xizmat iborasi borligini bildiradi.
func isMeta(s string) bool {
	for _, re := range metaPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// sentenceEndRe — gap oxiri: tinish belgisi (yopuvchi qo'shtirnoq/qavs bilan) va
// undan keyingi bo'sh joy. "2.5" yoki "t.b.e" kabi bo'sh joysiz nuqtalar
// gapni bo'lmaydi.
var sentenceEndRe = regexp.MustCompile(`[.!?…]+["»”')]*\s+`)

// stripMeta xizmat iborasi bo'lgan gaplarni olib tashlaydi; qolgan gaplar
// o'zgarishsiz qoladi. Hamma gap olib tashlansa — bo'sh satr qaytaradi.
func stripMeta(text string) string {
	var (
		sentences []string
		prev      int
		dropped   bool
	)
	for _, loc := range sentenceEndRe.FindAllStringIndex(text, -1) {
		sentences = append(sentences, text[prev:loc[1]])
		prev = loc[1]
	}
	sentences = append(sentences, text[prev:])

	var b strings.Builder
	for _, sentence := range sentences {
		if isMeta(sentence) {
			dropped = true
			continue
		}
		b.WriteString(sentence)
	}
	if !dropped {
		return text
	}
	return strings.TrimSpace(spacesRe.ReplaceAllString(b.String(), " "))
}

// reservedHeadings — rejadagi boblar orasida bo'lmasligi kerak bo'lgan
// sarlavhalar: ular hujjatda alohida, qat'iy nom bilan chiqariladi.
var reservedHeadings = map[string]bool{
	"kirish": true, "xulosa": true, "reja": true, "xulosa va takliflar": true,
	"foydalanilgan adabiyotlar": true, "foydalanilgan adabiyotlar ro" + "'" + "yxati": true, "adabiyotlar": true,
	"introduction": true, "conclusion": true, "references": true, "bibliography": true,
	"введение": true, "заключение": true, "список литературы": true, "литература": true,
	"список использованной литературы": true, "план": true, "outline": true,
	"кириш": true, "хулоса": true, "режа": true, "фойдаланилган адабиётлар": true,
}

func isReservedHeading(title string) bool {
	return reservedHeadings[headingKey(textutil.NormalizeApostrophes(title))]
}
