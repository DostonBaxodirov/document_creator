// Package design — dizayn lug'atining yagona manbai: slayd maketlari nomlari,
// AI maxsus dizayni variantlari (taqdimot va Word hujjat), ular asosida
// yig'iladigan spetsifikatsiyalar (CustomSpec, DocSpec) va ranglarni o'qish.
//
// ai paketi shu qiymatlarni promptlar, JSON sxemalari va javobni o'qishda,
// generator paketi esa chizishda ishlatadi — shuning uchun ular hech qachon
// bir-biridan farq qilmaydi. Qiymatlar promptlarda ko'rinadi va bazada
// saqlanadi — o'zgartirmang.
//
// Paket boshqa ichki paketlarni import qilmaydi.
package design

import (
	"regexp"
	"slices"
	"strings"
)

// Slayd maketlari nomlari (AI javobidagi "layout" maydoni qiymatlari).
const (
	LayoutAgenda     = "agenda"     // reja: raqamlangan bo'limlar
	LayoutBullets    = "bullets"    // sarlavha+izohli punktlar (+ asosiy fikr paneli)
	LayoutCards      = "cards"      // 2-6 ta kartochka
	LayoutComparison = "comparison" // ikki tomonni taqqoslash
	LayoutProcess    = "process"    // bosqichlar / jarayon / xronologiya
	LayoutFacts      = "facts"      // katta raqamlar va faktlar
	LayoutDefinition = "definition" // tushuncha ta'rifi + xususiyatlari
	LayoutConclusion = "conclusion" // xulosa: asosiy fikrlar + yakuniy xulosa

	LayoutTimeline  = "timeline"  // sanalar bilan xronologiya (vaqt chizig'i)
	LayoutPyramid   = "pyramid"   // ierarxiya darajalari (piramida)
	LayoutMatrix    = "matrix"    // 2x2 tasnif (masalan, SWOT)
	LayoutCycle     = "cycle"     // takrorlanuvchi sikl bosqichlari
	LayoutHub       = "hub"       // markaziy tushuncha va uning tarkibiy qismlari
	LayoutStatement = "statement" // bitta asosiy tezis (katta matn) + dalillar
)

// Maketlar ro'yxatlari.
var (
	// CoreLayouts — barcha dizaynlarda ishlatiladigan asosiy maketlar.
	CoreLayouts = []string{
		LayoutAgenda, LayoutBullets, LayoutCards, LayoutComparison,
		LayoutProcess, LayoutFacts, LayoutDefinition, LayoutConclusion,
	}
	// SignatureLayouts — dizaynning imzo maketi bo'la oladigan maketlar.
	SignatureLayouts = []string{LayoutTimeline, LayoutPyramid, LayoutMatrix, LayoutCycle, LayoutHub, LayoutStatement}
	// AllLayouts — barcha maketlar (generator ularning hammasini chiza oladi):
	// avval asosiylari, so'ng imzo maketlari.
	AllLayouts = slices.Concat(CoreLayouts, SignatureLayouts)
)

// ---------------------------------------------------------------------------
// Taqdimot uchun AI maxsus dizayni
// ---------------------------------------------------------------------------

// Maxsus dizayn variantlari.
const (
	CoverSplit = "split" // titul: yarmi rasm, yarmi toza fon va matn
	CoverFull  = "full"  // titul: butun slayd rasm, matn tomonida quyuq qoplama
	CoverFrame = "frame" // titul: ochiq fon, rasm katta ramkada, bezakli motiv

	HeaderBar       = "bar"       // sarlavha chapida vertikal rangli chiziq
	HeaderBand      = "band"      // sarlavha slayd tepasidagi rangli tasmada
	HeaderUnderline = "underline" // katta sarlavha, ostida qisqa aksent chiziq

	CardsShadow  = "shadow"  // yumshoq soyali kartochkalar
	CardsOutline = "outline" // ingichka chegarali kartochkalar
	CardsTint    = "tint"    // har bir kartochka o'z och rangida
	CardsAccent  = "accent"  // chap tomonda rangli chiziq
	CardsTop     = "top"     // yuqorida rangli chiziq

	CornersRounded = "rounded"
	CornersSquare  = "square"

	MarkerCircle  = "circle"  // raqamlar to'liq rangli doirada
	MarkerSquare  = "square"  // raqamlar yumaloq burchakli kvadratda
	MarkerOutline = "outline" // raqamlar faqat chegarali doirada

	MotifCircles  = "circles"  // katta shaffof doiralar
	MotifDiagonal = "diagonal" // qiya chiziqlar
	MotifDots     = "dots"     // nuqtalar to'ri
	MotifWaves    = "waves"    // to'lqinsimon qatlamlar
	MotifNone     = "none"     // bezaksiz

	PhotoLeft  = "left"
	PhotoRight = "right"
)

// Maxsus dizayn variantlari ro'yxatlari (AI prompti, JSON sxemasi va
// generator tekshiruvi shular bilan ishlaydi).
var (
	Covers  = []string{CoverSplit, CoverFull, CoverFrame}
	Headers = []string{HeaderBar, HeaderBand, HeaderUnderline}
	Cards   = []string{CardsShadow, CardsOutline, CardsTint, CardsAccent, CardsTop}
	Corners = []string{CornersRounded, CornersSquare}
	Markers = []string{MarkerCircle, MarkerSquare, MarkerOutline}
	Motifs  = []string{MotifCircles, MotifDiagonal, MotifDots, MotifWaves, MotifNone}
	Sides   = []string{PhotoLeft, PhotoRight}

	// Shriftlar — Windows/macOS PowerPoint'da bor va kengligi o'lchangan
	// (generator: fontWidths) — matnni maydonga sig'dirish aniq ishlaydi.
	HeadingFonts = []string{"Georgia", "Cambria", "Trebuchet MS", "Century Gothic", "Segoe UI", "Verdana", "Arial", "Arial Black", "Tahoma", "Calibri"}
	BodyFonts    = []string{"Calibri", "Segoe UI", "Arial", "Tahoma", "Verdana", "Trebuchet MS", "Cambria", "Georgia"}
)

// CustomSpec — AI tanlagan dizayn parametrlari. Ranglar "#RRGGBB" (yoki
// "RRGGBB", "#RGB") ko'rinishida; noto'g'ri yoki bo'sh qiymatlar standart
// qiymat bilan almashtiriladi.
type CustomSpec struct {
	Name string // dizayn nomi (masalan, "Deep Ocean") — izoh va PowerPoint rang sxemasi nomi

	Background string // mazmunli slayd foni
	Surface    string // kartochka foni
	Primary    string // asosiy rang (sarlavha elementlari, belgilar, panel)
	Secondary  string // ikkinchi rang
	Accent     string // aksent (chiziqlar, raqamlar, urg'u)
	Text       string // asosiy matn
	Muted      string // ikkinchi darajali matn

	HeadingFont string
	BodyFont    string

	Cover     string // CoverSplit | CoverFull | CoverFrame
	Header    string // HeaderBar | HeaderBand | HeaderUnderline
	Cards     string // Cards*
	Corners   string // CornersRounded | CornersSquare
	Markers   string // Marker*
	Motif     string // Motif*
	PhotoSide string // PhotoLeft | PhotoRight — mazmunli slayddagi rasm tomoni

	// PhotoStyle — barcha rasm promptlariga qo'shiladigan uslub (inglizcha):
	// yorug'lik, ranglar, kompozitsiya. Taqdimotdagi rasmlar bir uslubda chiqadi.
	PhotoStyle string
	// Signature — dizaynning imzo maketlari (AI mazmun yozishda ishlatadi).
	Signature []string
}

// ---------------------------------------------------------------------------
// Word hujjat uchun AI maxsus dizayni
// ---------------------------------------------------------------------------

// Word dizayn variantlari.
const (
	DocTitleClassic = "classic" // markazlashtirilgan, an'anaviy
	DocTitleBand    = "band"    // ish turi rangli lenta ustida
	DocTitleFrame   = "frame"   // ish turi va mavzu chiziqli blok ichida
	DocTitleSidebar = "sidebar" // chapga tekislangan, chap tomonda rangli chiziq

	DocHeadingCenter    = "center"    // markazda, rangli
	DocHeadingUnderline = "underline" // chapda, ostida rangli chiziq
	DocHeadingBand      = "band"      // rangli lenta ustida oq matn
	DocHeadingBar       = "bar"       // chapda, yonida qalin rangli chiziq

	DocBorderNone      = "none"
	DocBorderSingle    = "single"
	DocBorderDouble    = "double"
	DocBorderThinThick = "thinthick"

	DocOrnamentNone    = "none"
	DocOrnamentLine    = "line"    // titulda ingichka ajratuvchi chiziqlar
	DocOrnamentDiamond = "diamond" // ◆ ◆ ◆
	DocOrnamentDots    = "dots"    // • • •
)

// Word dizayn variantlari ro'yxatlari.
var (
	DocTitles    = []string{DocTitleClassic, DocTitleBand, DocTitleFrame, DocTitleSidebar}
	DocHeadings  = []string{DocHeadingCenter, DocHeadingUnderline, DocHeadingBand, DocHeadingBar}
	DocBorders   = []string{DocBorderNone, DocBorderSingle, DocBorderDouble, DocBorderThinThick}
	DocOrnaments = []string{DocOrnamentNone, DocOrnamentLine, DocOrnamentDiamond, DocOrnamentDots}
	// DocHeadingFonts — sarlavhalar shriftlari (Windows/Office'da mavjud).
	DocHeadingFonts = []string{"Times New Roman", "Cambria", "Georgia", "Palatino Linotype", "Book Antiqua",
		"Garamond", "Calibri", "Arial", "Century Gothic", "Trebuchet MS", "Segoe UI"}
)

// DocSpec — AI yaratgan Word dizayni (qiymatlar generator tomonidan
// tekshiriladi va tuzatiladi).
type DocSpec struct {
	Name        string
	Primary     string // sarlavhalar rangi ("#RRGGBB")
	Accent      string // chiziqlar va bezaklar rangi
	HeadingFont string
	Title       string // DocTitles
	Heading     string // DocHeadings
	Border      string // DocBorders
	Ornament    string // DocOrnaments
}

// ---------------------------------------------------------------------------
// Ranglar
// ---------------------------------------------------------------------------

var hexRe = regexp.MustCompile(`^#?([0-9a-fA-F]{6}|[0-9a-fA-F]{3})$`)

// ParseHex "#1A2B3C", "1a2b3c" yoki "#abc" ni "1A2B3C" ko'rinishiga keltiradi
// (atrofidagi bo'shliqlar e'tiborsiz qoldiriladi). Noto'g'ri qiymat uchun
// ok = false.
func ParseHex(s string) (string, bool) {
	m := hexRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", false
	}
	h := strings.ToUpper(m[1])
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	return h, true
}
