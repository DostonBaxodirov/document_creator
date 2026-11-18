package generator

// doclayout.go — titul varaq joylashuvi. Matnlar (vazirlik, universitet,
// mavzu, F.I.Sh.) necha qator egallashi taxminan hisoblanadi va qolgan bo'sh
// joy elementlar orasiga taqsimlanadi: yil doim sahifa tagida turadi, uzun
// mavzu yoki universitet nomida ham titul bitta sahifaga sig'adi (kerak
// bo'lsa mavzu shrifti kichraytiriladi).
//
// Hisob ataylab biroz "keng" olinadi (belgi kengligi va qator balandligi
// yuqoriroq): xato bo'lsa, titul pastida ozgina ortiqcha bo'sh joy qoladi,
// lekin keyingi sahifaga hech narsa o'tib ketmaydi.

import (
	"math"
	"strings"
	"unicode"
)

// Sahifa geometriyasi (twip: 1 sm ≈ 567): A4, hoshiyalar — chap 3 sm,
// o'ng 1,5 sm, yuqori va past 2 sm.
const (
	docPageW        = 11906
	docPageH        = 16838
	docMarginTop    = 1134
	docMarginBottom = 1134
	docMarginLeft   = 1701
	docMarginRight  = 850
	docTextWidth    = docPageW - docMarginLeft - docMarginRight
	docTextHeight   = docPageH - docMarginTop - docMarginBottom
)

// Titul varaq paragraflarining chekinishlari (twip).
const (
	titleSidebarIndent = 284  // yon chiziqli kompozitsiyada chapdan
	titleSignIndent    = 4820 // "Bajardi / Tekshirdi" bloki chapdan
	titleFrameIndent   = 567  // ramkali blok ikki yondan
)

// Titul varaq masofalari (twip).
const (
	titleGapBeforeUniversity = 240
	titleGapBeforeTopic      = 360
	titleGapBeforeTopicBand  = 480 // rangli lentadan keyin
	titleGapBeforeOrnament   = 200
	titleGapBeforeTeacher    = 120

	// Bo'sh joy taqsimoti: har bir oraliqning eng kichik qiymati va qolgan
	// joydan ulushi (foizda): universitet → ish turi, mavzu → "Bajardi",
	// "Tekshirdi" → yil.
	titleMinGapWork   = 900
	titleMinGapSign   = 700
	titleMinGapYear   = 500
	titleShareWork    = 40
	titleShareSign    = 32
	titleSafetyMargin = 450 // taxminiy hisob xatosi uchun zaxira

	// titleLineSpacing — titul paragraflarining qator oralig'i (w:line="276").
	titleLineSpacing = 276.0 / 240.0
)

// titleTopicSizes — mavzu shrifti o'lchamlari (yarim punktlarda: 16, 14, 13,
// 12 pt): titul sig'maguncha kattasidan kichigiga o'tiladi.
var titleTopicSizes = []int{32, 28, 26, 24}

// fontMetrics — shriftning taxminiy o'lchamlari.
type fontMetrics struct {
	line  float64 // bitta qator balandligi, em (Word'dagi kabi: ascent + descent + lineGap)
	scale float64 // belgilar kengligi Times New Roman ga nisbatan
}

// docFontMetrics — sarlavha shriftlari (DocHeadingFonts) o'lchamlari.
var docFontMetrics = map[string]fontMetrics{
	"":                  {1.15, 1.00}, // Times New Roman
	"Times New Roman":   {1.15, 1.00},
	"Cambria":           {1.18, 1.08},
	"Georgia":           {1.14, 1.12},
	"Palatino Linotype": {1.14, 1.07},
	"Book Antiqua":      {1.17, 1.05},
	"Garamond":          {1.13, 0.96},
	"Calibri":           {1.22, 0.98},
	"Arial":             {1.15, 1.10},
	"Century Gothic":    {1.23, 1.22},
	"Trebuchet MS":      {1.17, 1.08},
	"Segoe UI":          {1.33, 1.08},
}

// symbolMetrics — bezak belgilari (◆, •) odatda boshqa shriftdan olinadi.
var symbolMetrics = fontMetrics{1.35, 1.00}

// boldScale — qalin yozuv oddiydan shuncha kengroq.
const boldScale = 1.06

func metricsFor(font string) fontMetrics {
	if m, ok := docFontMetrics[font]; ok {
		return m
	}
	return fontMetrics{1.25, 1.25} // noma'lum shrift — zaxira bilan
}

// substituteMetrics — sarlavha shrifti qurilmada bo'lmasa (Mac, Android,
// LibreOffice) uning o'rniga odatda chiqadigan shrift (DejaVu Serif/Sans)
// o'lchamlari: ular Times New Roman dan ancha keng.
var substituteMetrics = fontMetrics{1.17, 1.27}

// headMetrics titul varaqdagi sarlavha shrifti o'lchamlari zaxira bilan:
// shrift o'rnatilmagan qurilmada ham hisob yetsin. Times New Roman hamma
// joyda bor (yoki o'lchamlari bir xil Liberation Serif bilan almashadi).
func headMetrics(font string) fontMetrics {
	m := metricsFor(font)
	if font == "" || font == "Times New Roman" {
		return m
	}
	return fontMetrics{line: max(m.line, substituteMetrics.line), scale: max(m.scale, substituteMetrics.scale)}
}

// runeWidthEm belgining taxminiy kengligi (em, Times New Roman).
func runeWidthEm(r rune) float64 {
	switch {
	case r == ' ':
		return 0.25
	case strings.ContainsRune("iljtfrI.,:;'’ʻʼ‘`!|()[]-", r):
		return 0.30
	case strings.ContainsRune("MWШЩЖЮЫФ", r):
		return 0.95
	case strings.ContainsRune("mwшщжюмы", r):
		return 0.75
	case unicode.IsUpper(r):
		return 0.72
	}
	return 0.50
}

// estimateLines matn width (twip) kenglikdagi paragrafda necha qator
// egallashini so'zma-so'z joylab hisoblaydi. sizePt — shrift o'lchami,
// scale — shrift kengligi koeffitsiyenti, tracking — har bir belgidan keyingi
// qo'shimcha masofa (pt).
func estimateLines(text string, sizePt float64, width int, scale, tracking float64) int {
	words := strings.Fields(text)
	if len(words) == 0 {
		return 1
	}
	avail := float64(width) / 20
	space := 0.25*sizePt*scale + tracking
	lines, cur := 1, 0.0
	for _, w := range words {
		ww := 0.0
		for _, r := range w {
			ww += runeWidthEm(r)*sizePt*scale + tracking
		}
		switch {
		case cur == 0:
			cur = ww
		case cur+space+ww <= avail:
			cur += space + ww
		default:
			lines++
			cur = ww
		}
		// Qatorga sig'maydigan uzun so'z bir necha qatorga bo'linadi.
		for cur > avail {
			lines++
			cur -= avail
		}
	}
	return lines
}

// titleTexts — titul varaqdagi matnlar (o'lchash va chizish uchun bir xil).
type titleTexts struct {
	header     string // vazirlik nomi (bo'sh bo'lishi mumkin)
	university string
	workType   string
	ornament   string // bezak qatori (bo'sh — yo'q)
	topic      string // "Mavzu: ..."
	student    string // "Bajardi: ..."
	teacher    string // "Tekshirdi: ..."
	year       string
}

// titleLayout — titul varaq oraliqlari (twip) va mavzu shrifti.
type titleLayout struct {
	gapWork   int // universitet → ish turi
	gapSign   int // mavzu → "Bajardi"
	gapYear   int // "Tekshirdi" → yil
	topicSize int // mavzu shrifti, yarim punktlarda (w:sz)
}

// titleMaxTopicLines — mavzu shundan ko'p qator egallasa, shrifti
// kichraytiriladi (juda uzun mavzu katta shriftda og'ir blok bo'lib qoladi).
const titleMaxTopicLines = 4

// planTitle titul varaq joylashuvini tanlaydi. Mavzu shrifti — mavzu
// titleMaxTopicLines qatorga sig'adigan va barcha elementlar eng kichik
// oraliqlar bilan sahifaga sig'adigan eng kattasi. Qolgan joy oraliqlarga
// ulush bo'yicha taqsimlanadi, shuning uchun yil sahifa tagiga tushadi.
func planTitle(t titleTexts, st docStyle) titleLayout {
	const minTotal = titleMinGapWork + titleMinGapSign + titleMinGapYear
	head := headMetrics(st.headingFont)
	_, block, _ := titleWidths(st)
	var lay titleLayout
	for i, size := range titleTopicSizes {
		last := i == len(titleTopicSizes)-1
		if !last && titleLines(t.topic, head, float64(size)/2, block, true, false, 0) > titleMaxTopicLines {
			continue
		}
		free := docTextHeight - titleSafetyMargin - measureTitle(t, st, size)
		extra := max(0, free-minTotal)
		work := extra * titleShareWork / 100
		sign := extra * titleShareSign / 100
		lay = titleLayout{
			gapWork:   titleMinGapWork + work,
			gapSign:   titleMinGapSign + sign,
			gapYear:   titleMinGapYear + extra - work - sign,
			topicSize: size,
		}
		if free >= minTotal {
			break
		}
	}
	return lay
}

// titleLines titul paragrafi necha qator egallashi (caps — katta harflar bilan).
func titleLines(text string, m fontMetrics, pt float64, width int, bold, caps bool, tracking float64) int {
	if caps {
		text = strings.ToUpper(text)
	}
	scale := m.scale
	if bold {
		scale *= boldScale
	}
	return estimateLines(text, pt, width, scale, tracking)
}

// titleWidths titul varaq paragraflarining matn kengligi (twip) kompozitsiyaga
// qarab: to'liq qatorlar, ish turi va mavzu bloki, "Bajardi" bloki.
func titleWidths(st docStyle) (full, block, sign int) {
	full, block, sign = docTextWidth, docTextWidth, docTextWidth-titleSignIndent
	switch st.title {
	case docTitleSidebar:
		full = docTextWidth - titleSidebarIndent
		block, sign = full, full
	case docTitleFrame:
		block = docTextWidth - 2*titleFrameIndent
	}
	return full, block, sign
}

// measureTitle titul varaq elementlari egallaydigan balandlik (twip),
// tanlanadigan uchta oraliqsiz (gapWork, gapSign, gapYear).
func measureTitle(t titleTexts, st docStyle, topicSize int) int {
	head, body := headMetrics(st.headingFont), metricsFor("")
	full, block, sign := titleWidths(st)

	para := func(text string, m fontMetrics, pt float64, width int, bold, caps bool, tracking float64) float64 {
		lines := titleLines(text, m, pt, width, bold, caps, tracking)
		return float64(lines) * pt * m.line * titleLineSpacing * 20
	}

	h := 0.0
	if t.header != "" {
		h += para(t.header, body, 14, full, true, true, 0)
	}
	h += titleGapBeforeUniversity + para(t.university, body, 14, full, true, true, 0)
	if st.ornament == docOrnamentLine && st.title != docTitleSidebar {
		h += 180 // universitet ostidagi chiziq: masofa 8 pt + qalinligi 1 pt
	}
	h += para(t.workType, head, 22, block, true, true, 1)
	switch st.title {
	case docTitleBand:
		// Lentaning yuqori va pastki chegaralari (10 pt + 0,5 pt), mavzu
		// oldidagi masofa va mavzu ostidagi chiziq (10 pt + 1,5 pt).
		h += 420 + titleGapBeforeTopicBand + 230
	case docTitleFrame:
		// Blokning yuqori va pastki qo'sh chiziqlari (14 pt + ~2,25 pt).
		h += 2*330 + titleGapBeforeTopic
	default:
		h += titleGapBeforeTopic
	}
	if t.ornament != "" {
		h += titleGapBeforeOrnament + para(t.ornament, symbolMetrics, 12, block, false, false, 0)
	}
	h += para(t.topic, head, float64(topicSize)/2, block, true, false, 0)
	h += para(t.student, body, 14, sign, true, false, 0)
	h += titleGapBeforeTeacher + para(t.teacher, body, 14, sign, true, false, 0)
	h += para(t.year, body, 14, full, true, false, 0)
	return int(math.Ceil(h))
}
