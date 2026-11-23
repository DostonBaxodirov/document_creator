package generation

// labels.go — fayl izohi va nomida ko'rinadigan nomlar: ish turlari, maxsus
// dizayn tariflari va fayl nomi. Bot xabarlari ham shu nomlarni ishlatadi
// (yagona manba).

import (
	"fmt"
	"regexp"
	"strings"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/lang"
)

// workTypeNames — ish turlarining foydalanuvchiga ko'rinadigan nomlari.
var workTypeNames = map[ai.WorkType]string{
	ai.WorkPresentation: "Slayd (taqdimot)",
	ai.WorkIndependent:  "Mustaqil ish",
	ai.WorkReferat:      "Referat",
	ai.WorkPractical:    "Amaliy topshiriq",
}

// WorkTypeName ish turining foydalanuvchiga ko'rinadigan nomi: "Slayd
// (taqdimot)", "Referat" ... (noma'lum tur — o'zi).
func WorkTypeName(w ai.WorkType) string {
	if n, ok := workTypeNames[w]; ok {
		return n
	}
	return string(w)
}

// workTypeTitle ish turining titul slayd/varaqda ko'rinadigan nomi (ish tilida):
// "TAQDIMOT", "САМОСТОЯТЕЛЬНАЯ РАБОТА" ...
func workTypeTitle(w ai.WorkType, l lang.Info) string {
	switch w {
	case ai.WorkIndependent:
		return l.Labels.TitleIndependent
	case ai.WorkPractical:
		return l.Labels.TitlePractical
	case ai.WorkReferat:
		return l.Labels.TitleReferat
	default:
		return l.Labels.TitlePresentation
	}
}

// tariffLabels — maxsus dizayn tariflarining belgisi va nomi.
var tariffLabels = map[string]struct{ emoji, name string }{
	config.TariffStandard: {"🥉", "Standart"},
	config.TariffOptimal:  {"🥈", "Optimal"},
	config.TariffPremium:  {"🥇", "Premium"},
}

// TariffLabel tarif belgisi va nomi: "🥇", "Premium" (noma'lum tarif — "" va o'zi).
func TariffLabel(t string) (emoji, name string) {
	if l, ok := tariffLabels[t]; ok {
		return l.emoji, l.name
	}
	return "", t
}

// TariffName — "🥇 Premium" (noma'lum tarif — o'zi).
func TariffName(t string) string {
	if l, ok := tariffLabels[t]; ok {
		return l.emoji + " " + l.name
	}
	return t
}

// customCaptionName fayl izohidagi maxsus dizayn nomi (taqdimot va Word ish):
// "✨ Chuqur okean (maxsus dizayn, 🥇 Premium)"; nomsiz — "✨ Maxsus dizayn (🥇 Premium)".
func customCaptionName(name, tariff string) string {
	if name != "" {
		return fmt.Sprintf("✨ %s (maxsus dizayn, %s)", name, TariffName(tariff))
	}
	return fmt.Sprintf("✨ Maxsus dizayn (%s)", TariffName(tariff))
}

var unsafeFileChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

// documentFileName mavzudan xavfsiz fayl nomi yasaydi: "Suniy_intellekt....pptx".
// ext — kengaytma (".pptx" yoki ".docx").
func documentFileName(topic, ext string) string {
	name := unsafeFileChars.ReplaceAllString(topic, " ")
	name = strings.Join(strings.Fields(name), "_")
	if r := []rune(name); len(r) > 60 {
		name = strings.TrimRight(string(r[:60]), "_.-")
	}
	if name == "" {
		name = "hujjat"
		if ext == ".pptx" {
			name = "taqdimot"
		}
	}
	return name + ext
}
