package generator

// themes.go — taqdimot dizayn mavzulari. Har bir mavzu o'z ranglar
// palitrasi, shriftlari, kartochka va belgi uslubi, titul va yakuniy slaydi
// hamda mazmunli slaydlar "ramkasi"ga (fon, sarlavha joyi, kolontitul) ega.
// Mazmunli slaydlarning maketlari (layouts.go, layouts_extra.go) umumiy,
// lekin mavzu uslubi (kartochka turi, belgilar, mazmun maydoni shakli)
// orqali har bir dizaynda boshqacha ko'rinadi; ayrim dizaynlar o'z maketiga
// ham ega (theme.layouts). Har bir dizaynning "imzo" maketlari (signature)
// AI promptiga uzatiladi — taqdimot tuzilishi dizaynga mos chiqadi.
// Rasmli dizaynlar — themes_photo.go, tuzilishi boshqacha dizaynlar —
// themes_extra.go da.

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// ThemeInfo — mavzuning foydalanuvchiga ko'rsatiladigan ma'lumotlari.
type ThemeInfo struct {
	ID          string // bazada saqlanadi — o'zgartirmang
	Name        string
	Emoji       string
	Description string
	Photos      bool // rasmli dizayn: titul va mos slaydlarda AI chizgan rasmlar
}

// palette — mavzu ranglari (RRGGBB).
type palette struct {
	bg        string   // mazmunli slayd foni
	surface   string   // kartochka foni
	surface2  string   // ikkinchi darajali panel foni
	primary   string   // asosiy rang
	secondary string   // ikkinchi rang
	accent    string   // aksent (urg'u) rangi
	text      string   // asosiy matn
	muted     string   // ikkinchi darajali matn
	line      string   // chiziqlar, ajratgichlar
	number    string   // katta raqamlar rangi
	series    []string // ketma-ket elementlar (kartochkalar, qadamlar) ranglari
	onSeries  string   // series rangidagi shakl ustidagi matn
	panel     paint    // urg'u paneli (ta'rif, xulosa) foni
	onPanel   string   // panel ustidagi matn
	shadow    string   // soya rangi
	// tints — "tint" uslubidagi kartochkalar foni (elementga xos och ranglar);
	// bo'sh bo'lsa series rangidan hisoblanadi.
	tints []string
	// ink — katta raqamlar va qiymatlar matni rangi; bo'sh bo'lsa series
	// rangi ishlatiladi (series och bo'lgan dizaynlarda o'qilishi uchun).
	ink string
}

// cardStyle — kartochkalar ko'rinishi.
type cardStyle struct {
	radius  float64 // burchak radiusi, dyuym (0 — to'g'ri burchak)
	shadow  bool
	border  bool
	leftBar bool // chap tomonda rangli chiziq
	topBar  bool // yuqorida to'liq rangli chiziq
	tab     bool // yuqorida qisqa yorug' chiziq

	hard      bool    // qalin chegara va siljigan qattiq soya (neo-brutalizm)
	tint      bool    // fon — elementga xos och rang (palette.tints)
	rule      bool    // fon yo'q, faqat yuqorida rangli chiziq (tipografik uslub)
	ticks     bool    // burchaklarda "+" belgilari (chizma uslubi)
	dash      string  // chegara chizig'i turi ("dash" — bo'r bilan chizilgandek)
	borderW   float64 // chegara qalinligi, pt (0 — 1 pt)
	fillAlpha int     // fon shaffof emasligi, % (0 — to'liq)
}

type theme struct {
	ThemeInfo
	pal          palette
	headFont     string
	bodyFont     string
	headBold     bool
	quoteItalic  bool // ta'rif va xulosa matni kursivda
	badgeOutline bool // raqamli doiralar faqat chegara bilan
	cards        cardStyle
	bulletChar   string
	dark         bool
	neon         bool // to'q fonda belgilar va sarlavhalar atrofida yorug'lik (glow)
	badgeSquare  bool // raqamli belgilar kvadrat shaklida
	badgePlain   bool // raqamlar shaklsiz — fon rangidagi maydonda katta matn
	transition   string

	// Dizayn tuzilishi (AI promptiga uzatiladi — ThemeStructure).
	signature []string // dizaynga xos maketlar: AI ularni albatta ishlatadi
	compact   bool     // matn uchun joy kam (katta shrift, banner) — AI qisqaroq yozadi
	// layouts — mavzuga xos maketlar (umumiy maket o'rniga chiziladi).
	layouts map[string]layoutFunc

	// Rasmli dizaynlar (themes_photo.go).
	photos     bool     // titul, yakuniy va mos mazmunli slaydlarda rasm bor
	photoStyle string   // AI rasm promptiga qo'shiladigan uslub (inglizcha)
	photoLeft  bool     // mazmunli slaydda rasm chap tomonda (aks holda o'ngda)
	photoFrame picFrame // mazmunli slayddagi rasm ramkasi
	// photoBanner — rasm mazmunli slayd tepasida keng banner (yotiq rasm);
	// banner mazmun kengligini kamaytirmaydi, shuning uchun deyarli barcha
	// maketlarga rasm qo'yiladi.
	photoBanner bool

	cover   func(t *theme, c *canvas, d *Deck)
	closing func(t *theme, c *canvas, d *Deck)
	// chrome mazmunli slayd fonini, sarlavhasini va kolontitulini chizadi
	// hamda asosiy mazmun uchun bo'sh maydonni qaytaradi.
	chrome func(t *theme, c *canvas, s slideInfo) rect
}

// slideInfo — mazmunli slayd ramkasi uchun ma'lumotlar.
type slideInfo struct {
	title, lead string
	num, total  int
	topic       string // qisqartirilgan mavzu
	university  string // qisqartirilgan universitet nomi
	// photo — slayd rasmi (rasmli dizaynlarda); chrome uni chizadi va matn
	// uchun qolgan maydonni qaytaradi.
	photo *Picture
	// banner — rasm yon panelda emas, slayd tepasida sarlavha orqasida keng
	// banner bo'lib chiziladi (keng maketlar uchun, photoHeader).
	banner bool
}

// Mavzu identifikatorlari.
const (
	ThemeAcademic = "akademik"
	ThemeModern   = "zamonaviy"
	ThemeMinimal  = "minimal"
	ThemeTech     = "texno"

	// Rasmli dizaynlar.
	ThemeJournal = "jurnal"
	ThemeNature  = "tabiat"
	ThemeOcean   = "okean"
	ThemeSunset  = "shom"

	// Tuzilishi boshqacha dizaynlar (themes_extra.go).
	ThemeStrategy  = "strategiya"
	ThemeRetro     = "retro"
	ThemeTypo      = "tipografika"
	ThemeBento     = "bento"
	ThemeBoard     = "doska"
	ThemeBlueprint = "chizma"
	ThemePastel    = "pastel"
	ThemePanorama  = "panorama" // rasmli
)

// DefaultTheme — foydalanuvchi tanlamagan bo'lsa ishlatiladigan mavzu.
const DefaultTheme = ThemeAcademic

var themes = []*theme{
	academicTheme, modernTheme, minimalTheme, techTheme,
	journalTheme, natureTheme, oceanTheme, sunsetTheme,
	strategyTheme, retroTheme, typoTheme, bentoTheme,
	boardTheme, blueprintTheme, pastelTheme, panoramaTheme,
}

// Structure — dizayn tuzilishi: AI promptini tanlangan dizaynga moslash uchun.
type Structure struct {
	Layouts   []string // dizayn chizadigan maketlar (AI faqat shulardan tanlaydi)
	Signature []string // dizaynga xos maketlar — AI ularni ishlatishi kerak
	Compact   bool     // matn uchun joy kam — AI qisqaroq yozadi
}

// coreLayouts — barcha dizaynlarda ishlatiladigan asosiy maketlar.
var coreLayouts = []string{
	LayoutAgenda, LayoutBullets, LayoutCards, LayoutComparison,
	LayoutProcess, LayoutFacts, LayoutDefinition, LayoutConclusion,
}

// ThemeStructure dizayn tuzilishini qaytaradi: asosiy maketlar va dizaynning
// imzo maketlari. Noma'lum ID uchun — standart dizayn tuzilishi.
func ThemeStructure(id string) Structure {
	t := themeByID(id)
	layouts := append([]string(nil), coreLayouts...)
	for _, l := range t.signature {
		if !slices.Contains(layouts, l) {
			layouts = append(layouts, l)
		}
	}
	return Structure{Layouts: layouts, Signature: slices.Clone(t.signature), Compact: t.compact}
}

// Themes barcha mavzular ro'yxatini (tanlash tugmalari uchun) qaytaradi.
func Themes() []ThemeInfo {
	out := make([]ThemeInfo, len(themes))
	for i, t := range themes {
		out[i] = t.ThemeInfo
	}
	return out
}

// FindTheme ID bo'yicha mavzu ma'lumotlarini qaytaradi.
func FindTheme(id string) (ThemeInfo, bool) {
	for _, t := range themes {
		if t.ID == id {
			return t.ThemeInfo, true
		}
	}
	return ThemeInfo{}, false
}

func themeByID(id string) *theme {
	for _, t := range themes {
		if t.ID == id {
			return t
		}
	}
	return themes[0]
}

// ---------------------------------------------------------------------------
// Umumiy yordamchilar
// ---------------------------------------------------------------------------

// hd — sarlavha shriftidagi matn.
func (t *theme) hd(text string, size float64, color string) run {
	return run{text: text, size: size, bold: t.headBold, color: color, font: t.headFont}
}

// tx — asosiy shriftdagi matn.
func (t *theme) tx(text string, size float64, color string) run {
	return run{text: text, size: size, color: color, font: t.bodyFont}
}

func (t *theme) seriesColor(i int) string {
	if i < 0 {
		return t.pal.primary
	}
	return t.pal.series[i%len(t.pal.series)]
}

// tint i-element uchun och fon rangi ("tint" uslubidagi kartochkalar).
func (t *theme) tint(i int) string {
	if len(t.pal.tints) > 0 {
		return t.pal.tints[max(i, 0)%len(t.pal.tints)]
	}
	return mixHex(t.seriesColor(i), t.pal.surface, 0.85)
}

// cardBg kartochka fonining haqiqiy rangi (xira matnni oldindan aralashtirish
// uchun: iPhone va Quick Look matn shaffofligini ko'rsatmaydi).
func (t *theme) cardBg(i int) string {
	cs := t.cards
	switch {
	case cs.rule:
		return t.pal.bg
	case cs.tint && i >= 0:
		return t.tint(i)
	case cs.fillAlpha > 0:
		return mixHex(t.pal.bg, t.pal.surface, float64(cs.fillAlpha)/100)
	}
	return t.pal.surface
}

// inkColor i-element katta raqami yoki qiymati rangi.
func (t *theme) inkColor(i int) string {
	if t.pal.ink != "" {
		return t.pal.ink
	}
	return t.seriesColor(i)
}

// boxShape kartochka (yoki panel) asosiy shakli; i < 0 — rangsiz (oddiy fon).
func (t *theme) boxShape(r rect, i int) shape {
	cs := t.cards
	s := shape{fill: solid(t.pal.surface)}
	switch {
	case cs.rule:
		s.fill = paint{}
	case cs.tint && i >= 0:
		s.fill = solid(t.tint(i))
	case cs.fillAlpha > 0:
		s.fill = solidA(t.pal.surface, cs.fillAlpha)
	}
	if cs.radius > 0 {
		s.geom, s.adj = "roundRect", roundAdj(r, cs.radius)
	}
	if cs.shadow {
		s.shadow = &shadow{blur: 18, dist: 3, dir: 90, color: t.pal.shadow, alpha: 13}
	}
	if cs.border || cs.hard || cs.dash != "" || cs.ticks {
		w := cs.borderW
		if w == 0 {
			w = 1
		}
		s.line = &stroke{color: t.pal.line, width: w, dash: cs.dash}
	}
	return s
}

// hardShadow neo-brutalizm uslubidagi siljigan qattiq soya (shakldan oldin chiziladi).
func (t *theme) hardShadow(c *canvas, r rect, s shape) {
	if !t.cards.hard {
		return
	}
	const off = 0.08
	c.add(rect{r.X + off, r.Y + off, r.W, r.H}, shape{geom: s.geom, adj: s.adj, rot: s.rot, fill: solid(t.pal.shadow)})
}

// cornerTicks kartochka burchaklaridagi "+" belgilari (chizma uslubi).
func (t *theme) cornerTicks(c *canvas, r rect) {
	if !t.cards.ticks {
		return
	}
	const arm, th = 0.09, 0.012
	for _, p := range [][2]float64{{r.X, r.Y}, {r.right(), r.Y}, {r.X, r.bottom()}, {r.right(), r.bottom()}} {
		c.add(rect{p[0] - arm, p[1] - th/2, 2 * arm, th}, shape{fill: solid(t.pal.accent)})
		c.add(rect{p[0] - th/2, p[1] - arm, th, 2 * arm}, shape{fill: solid(t.pal.accent)})
	}
}

// box kartochka fonini (soya va burchak belgilari bilan) chizadi, rangli chiziqlarsiz.
func (t *theme) box(c *canvas, r rect, i int) {
	s := t.boxShape(r, i)
	t.hardShadow(c, r, s)
	c.add(r, s)
	t.cornerTicks(c, r)
}

// card mavzu uslubidagi kartochkani chizadi; i — ketma-ketlikdagi o'rni (rang uchun).
func (t *theme) card(c *canvas, r rect, i int) {
	t.box(c, r, i)
	col := t.seriesColor(i)
	switch {
	case t.cards.rule:
		c.add(rect{r.X, r.Y, r.W, 0.045}, shape{fill: solid(col)})
	case t.cards.leftBar:
		c.add(rect{r.X, r.Y, 0.06, r.H}, shape{fill: solid(col)})
	case t.cards.topBar:
		c.add(rect{r.X, r.Y, r.W, 0.045}, shape{fill: solid(col)})
	case t.cards.tab:
		c.add(rect{r.X + 0.22, r.Y - 0.01, min(0.9, r.W/3), 0.045}, shape{fill: solid(col)})
	}
}

// cardPad — kartochka ichidagi matn maydoni.
func (t *theme) cardPad(r rect, padX, padY float64) rect {
	in := r.inset(padX, padY)
	if t.cards.leftBar {
		in.X += 0.05
		in.W -= 0.05
	}
	if t.cards.topBar || t.cards.tab || t.cards.rule {
		in.Y += 0.04
		in.H -= 0.04
	}
	if t.cards.rule {
		// Fonsiz kartochkada matn chap chetga tekislanadi.
		in.X -= padX * 0.6
		in.W += padX * 0.6
	}
	return in
}

// panelShape — urg'u paneli (ta'rif, asosiy fikr, xulosa).
func (t *theme) panelShape(r rect) shape {
	s := shape{fill: t.pal.panel}
	if t.cards.radius > 0 {
		s.geom, s.adj = "roundRect", roundAdj(r, t.cards.radius)
	}
	if t.cards.shadow {
		s.shadow = &shadow{blur: 20, dist: 4, dir: 90, color: t.pal.shadow, alpha: 22}
	}
	switch {
	case t.cards.hard:
		s.line = &stroke{color: t.pal.line, width: max(t.cards.borderW, 1)}
	case t.cards.dash != "":
		s.line = &stroke{color: t.pal.onPanel, width: 1.25, alpha: 55, dash: t.cards.dash}
	case t.cards.ticks:
		s.line = &stroke{color: t.pal.onPanel, width: 0.75, alpha: 70}
	case t.dark:
		s.line = &stroke{color: t.pal.primary, width: 1, alpha: 45}
	}
	return s
}

// panel urg'u panelini (soya va burchak belgilari bilan) chizadi.
func (t *theme) panel(c *canvas, r rect) {
	s := t.panelShape(r)
	t.hardShadow(c, r, s)
	c.add(r, s)
	t.cornerTicks(c, r)
}

// badgeShape raqamli (yoki belgili) belgi shakli va uning matn rangi.
func (t *theme) badgeShape(i int) (shape, string) {
	col := t.seriesColor(i)
	s := shape{geom: "ellipse", fill: solid(col)}
	txt := t.pal.onSeries
	if t.badgeSquare {
		s.geom, s.adj = "roundRect", []int{12000}
	}
	switch {
	case t.badgePlain:
		s.geom, s.adj, s.fill, txt = "rect", nil, solid(t.pal.bg), t.inkColor(i)
	case t.badgeOutline:
		s.fill = solid(t.pal.bg)
		s.line = &stroke{color: col, width: 1.5, dash: t.cards.dash}
		txt = col
	case t.cards.hard:
		s.line = &stroke{color: t.pal.line, width: max(t.cards.borderW, 1)}
	}
	if t.neon {
		s.glow = &glow{radius: 7, color: col, alpha: 35}
	}
	return s, txt
}

// badge raqamli (yoki belgili) doirani chizadi.
func (t *theme) badge(c *canvas, r rect, label string, i int) {
	s, color := t.badgeShape(i)
	size := r.H * 72 * 0.4
	if t.badgePlain {
		size = r.H * 72 * 0.62
	}
	txt := run{text: label, size: size, bold: true, color: color, font: t.headFont}
	align := "ctr"
	if t.badgePlain {
		align = "l"
	}
	s.text = &textBody{anchor: "ctr", inset: [4]float64{0.01, 0.01, 0.01, 0.01}, paras: []para{{align: align, runs: []run{txt}}}}
	c.add(r, s)
}

// check — "bajarildi" belgili doira (xulosa slaydi uchun).
func (t *theme) check(c *canvas, r rect, i int) {
	s, color := t.badgeShape(i)
	align := "ctr"
	if t.badgePlain {
		align = "l"
	}
	txt := run{text: "✓", size: r.H * 72 * 0.5, bold: true, color: color, font: "Segoe UI Symbol"}
	s.text = &textBody{anchor: "ctr", inset: [4]float64{0.01, 0.01, 0.01, 0.01}, paras: []para{{align: align, runs: []run{txt}}}}
	c.add(r, s)
}

// mixHex ikki rangni aralashtiradi: k=0 — a, k=1 — b.
func mixHex(a, b string, k float64) string {
	ca, cb := hexColor(a), hexColor(b)
	m := func(x, y uint8) int { return int(math.Round(float64(x) + (float64(y)-float64(x))*k)) }
	return fmt.Sprintf("%02X%02X%02X", m(ca.R, cb.R), m(ca.G, cb.G), m(ca.B, cb.B))
}

// titleBlock — titul slayddagi mavzu va qo'shimcha sarlavha.
func (t *theme) titleBlock(d *Deck, size float64, color string, subSize float64, subColor string, subAlpha int, align string) textBody {
	paras := []para{{align: align, lineSp: 0.95, runs: []run{{text: d.Topic, size: size, bold: true, color: color, font: t.headFont}}}}
	if !t.headBold {
		paras[0].runs[0].bold = false
	}
	if d.Subtitle != "" {
		paras = append(paras, para{align: align, before: 14, runs: []run{{text: d.Subtitle, size: subSize, color: subColor, alpha: subAlpha, font: t.bodyFont}}})
	}
	// Mavzu 500 belgigacha bo'lishi mumkin — uzun mavzu sig'ishi uchun shrift
	// asl o'lchamining 35% igacha kichraya oladi.
	return textBody{fit: true, minFit: 0.35, paras: paras}
}

// people — "Bajardi" / "Tekshirdi" qatorlari.
func (t *theme) people(d *Deck, size float64, labelColor, valueColor string, labelAlpha int, align string) []para {
	var out []para
	add := func(label, value string) {
		if value == "" {
			return
		}
		out = append(out, para{align: align, after: 4, runs: []run{
			{text: label + " ", size: size, bold: true, color: labelColor, alpha: labelAlpha, font: t.bodyFont},
			{text: value, size: size, color: valueColor, font: t.bodyFont},
		}})
	}
	add(d.labels().Student+":", d.StudentName)
	add(d.labels().Teacher+":", d.TeacherName)
	return out
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func pageLabel(s slideInfo) string { return fmt.Sprintf("%s / %s", pad2(s.num), pad2(s.total)) }

func line1(text string, size float64, color, font, align string) textBody {
	return textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{align: align, runs: []run{{text: text, size: size, color: color, font: font}}}}}
}

// ring — chegara bilan chizilgan doira (markazi va radiusi bo'yicha).
func ring(c *canvas, cx, cy, r float64, color string, width float64, alpha int, dash string) {
	c.add(rect{cx - r, cy - r, 2 * r, 2 * r}, shape{geom: "ellipse", line: &stroke{color: color, width: width, alpha: alpha, dash: dash}})
}

// ===========================================================================
// 1. AKADEMIK — to'q ko'k va oltin, klassik serif sarlavhalar
// ===========================================================================

var academicTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeAcademic, Name: "Akademik", Emoji: "🏛",
		Description: "klassik, ko'k va oltin",
	},
	pal: palette{
		bg: "F7F5F0", surface: "FFFFFF", surface2: "EEF1F6",
		primary: "1B2A49", secondary: "2E4A7D", accent: "C9A227",
		text: "1F2937", muted: "5B6475", line: "DDD8CC", number: "B08A1E",
		series:   []string{"1B2A49", "B08A1E", "2E4A7D", "8A6A12"},
		onSeries: "FFFFFF", panel: linear(90, "1B2A49", "24375F"), onPanel: "FFFFFF", shadow: "1B2A49",
	},
	headFont: "Cambria", bodyFont: "Calibri", headBold: true, quoteItalic: true,
	cards:      cardStyle{shadow: true, leftBar: true},
	bulletChar: "■",
	signature:  []string{LayoutTimeline, LayoutPyramid, LayoutStatement},
	transition: `<p:transition spd="med"><p:fade/></p:transition>`,
	cover:      academicCover,
	closing:    academicClosing,
	chrome:     academicChrome,
}

func academicCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = linear(90, "1B2A49", "121C33")
	c.add(rect{8.3, -2.5, 7.4, 7.4}, shape{geom: "ellipse", fill: solidA("FFFFFF", 5)})
	ring(c, 12.1, 5.9, 2.4, p.accent, 1.25, 55, "")
	ring(c, 12.1, 5.9, 1.75, "FFFFFF", 0.75, 18, "")
	c.add(rect{11.6, 3.3, 0.3, 0.3}, shape{geom: "ellipse", fill: solid(p.accent)})
	c.add(rect{0, 0, 0.22, slideH}, shape{fill: solid(p.accent)})

	c.text(rect{0.9, 0.55, 10.2, 0.9}, textBody{fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 14, bold: true, caps: true, spc: 1.5, color: "C9D6E8", font: t.bodyFont}}}}})
	c.add(rect{1.0, 2.0, 0.9, 0.06}, shape{fill: solid(p.accent)})
	c.text(rect{0.9, 2.12, 8, 0.45}, textBody{paras: []para{{runs: []run{
		{text: d.WorkType, size: 16, bold: true, spc: 4, color: p.accent, font: t.bodyFont}}}}})
	c.text(rect{0.9, 2.65, 8.6, 3.05}, t.titleBlock(d, 40, "FFFFFF", 18, "C9D6E8", 0, "l"))

	c.add(rect{1.0, 5.95, 0.05, 0.8}, shape{fill: solid(p.accent)})
	c.text(rect{1.15, 5.88, 7.6, 0.95}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, p.accent, "FFFFFF", 0, "l")})
	c.text(rect{10.4, 6.7, 2.4, 0.45}, line1(d.Year, 14, "C9D6E8", t.bodyFont, "r"))
}

func academicClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = linear(90, "1B2A49", "121C33")
	c.add(rect{-2.8, 3.0, 6.6, 6.6}, shape{geom: "ellipse", fill: solidA("FFFFFF", 5)})
	ring(c, 0.9, 0.6, 2.0, p.accent, 1.25, 50, "")
	c.add(rect{slideW - 0.22, 0, 0.22, slideH}, shape{fill: solid(p.accent)})

	c.text(rect{1.2, 1.7, 10.9, 1.6}, textBody{anchor: "b", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.labels().Thanks, size: 46, bold: true, color: "FFFFFF", font: t.headFont}}}}})
	c.add(rect{slideW/2 - 0.45, 3.5, 0.9, 0.06}, shape{fill: solid(p.accent)})
	c.text(rect{1.6, 3.75, 10.1, 1.2}, textBody{fit: true, paras: []para{{align: "ctr", runs: []run{t.tx(d.Topic, 20, "C9D6E8")}}}})
	if d.StudentName != "" {
		c.text(rect{1.6, 5.35, 10.1, 0.55}, textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
			{text: d.labels().Student + ": ", size: 16, bold: true, color: p.accent, font: t.bodyFont},
			t.tx(d.StudentName, 16, "FFFFFF")}}}})
	}
}

func academicChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	c.add(rect{0, 0, 0.18, slideH}, shape{fill: solid(p.primary)})
	c.add(rect{0.18, 0, 0.05, slideH}, shape{fill: solid(p.accent)})
	c.add(rect{12.03, 0.45, 0.62, 0.62}, shape{fill: solid(p.primary), text: &textBody{
		anchor: "ctr", inset: [4]float64{0.01, 0.01, 0.01, 0.01},
		paras: []para{{align: "ctr", runs: []run{{text: pad2(s.num), size: 16, bold: true, color: "FFFFFF", font: t.headFont}}}}}})

	c.text(rect{0.75, 0.3, 11.1, 0.9}, textBody{anchor: "b", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 30, p.primary)}}}})
	c.add(rect{0.85, 1.25, 0.9, 0.05}, shape{fill: solid(p.accent)})
	y := 1.6
	if s.lead != "" {
		c.text(rect{0.75, 1.36, 11.9, 0.64}, textBody{fit: true, minFit: 0.7, paras: []para{{runs: []run{
			{text: s.lead, size: 15, italic: true, color: p.muted, font: t.bodyFont}}}}})
		y = 2.12
	}

	c.add(rect{0.75, 6.98, 11.9, 0.012}, shape{fill: solid(p.line)})
	c.text(rect{0.75, 7.02, 7.0, 0.32}, line1(s.topic, 10, p.muted, t.bodyFont, "l"))
	c.text(rect{7.8, 7.02, 4.85, 0.32}, line1(s.university, 10, p.muted, t.bodyFont, "r"))
	return rect{0.75, y, 11.9, 6.8 - y}
}

// ===========================================================================
// 2. ZAMONAVIY — binafsha gradient, yumaloq kartochkalar, yorqin aksent
// ===========================================================================

var modernTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeModern, Name: "Zamonaviy", Emoji: "💜",
		Description: "binafsha gradient",
	},
	pal: palette{
		bg: "F4F5FB", surface: "FFFFFF", surface2: "EEF0FF",
		primary: "4F46E5", secondary: "7C3AED", accent: "F97362",
		text: "1E1B4B", muted: "5F6283", line: "E3E5F2", number: "4F46E5",
		series:   []string{"4F46E5", "7C3AED", "F97362", "0EA5E9"},
		onSeries: "FFFFFF", panel: linear(35, "4F46E5", "7C3AED"), onPanel: "FFFFFF", shadow: "4F46E5",
	},
	headFont: "Segoe UI", bodyFont: "Segoe UI", headBold: true,
	cards:      cardStyle{radius: 0.16, shadow: true},
	bulletChar: "●",
	signature:  []string{LayoutHub, LayoutMatrix, LayoutStatement},
	transition: `<p:transition spd="med"><p:push dir="u"/></p:transition>`,
	cover:      modernCover,
	closing:    modernClosing,
	chrome:     modernChrome,
}

func modernBackdrop(c *canvas) {
	c.bg = linear(35, "4338CA", "6D28D9", "9333EA")
}

func modernCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	modernBackdrop(c)
	c.add(rect{8.0, -2.1, 6.9, 6.9}, shape{geom: "ellipse", fill: solidA("FFFFFF", 10)})
	c.add(rect{10.4, 3.9, 4.6, 4.6}, shape{geom: "ellipse", fill: solidA("FFFFFF", 7)})
	c.add(rect{7.25, 5.55, 1.55, 1.55}, shape{geom: "ellipse", fill: linear(45, "FB923C", p.accent)})
	ring(c, 12.15, 1.15, 0.55, "FFFFFF", 1.5, 45, "")

	// "Shisha" kartochka: yil va universitet.
	glass := rect{8.85, 1.55, 3.7, 4.35}
	c.add(glass, shape{geom: "roundRect", adj: roundAdj(glass, 0.28), fill: solidA("FFFFFF", 13), line: &stroke{color: "FFFFFF", width: 1, alpha: 35}})
	c.text(glass.inset(0.3, 0.3), textBody{anchor: "ctr", fit: true, paras: []para{
		{align: "ctr", runs: []run{{text: d.Year, size: 46, bold: true, color: "FFFFFF", font: t.headFont}}},
		{align: "ctr", before: 10, runs: []run{{text: d.University, size: 14, color: "FFFFFF", alpha: 88, font: t.bodyFont}}},
	}})

	chip := run{text: d.WorkType, size: 12, bold: true, spc: 3, color: "FFFFFF", font: t.bodyFont}
	cw := min(7.0, textWidth(chip)+0.6)
	chipR := rect{0.9, 1.45, cw, 0.46}
	c.add(chipR, shape{geom: "roundRect", adj: []int{50000}, fill: solidA("FFFFFF", 18), text: &textBody{
		anchor: "ctr", inset: [4]float64{0.05, 0.01, 0.05, 0.01}, paras: []para{{align: "ctr", runs: []run{chip}}}}})

	c.text(rect{0.9, 2.15, 7.6, 3.3}, t.titleBlock(d, 40, "FFFFFF", 17, "FFFFFF", 80, "l"))
	c.text(rect{0.9, 5.75, 6.2, 1.0}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, "FFFFFF", "FFFFFF", 70, "l")})
}

func modernClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	modernBackdrop(c)
	c.add(rect{-1.6, -2.2, 6.2, 6.2}, shape{geom: "ellipse", fill: solidA("FFFFFF", 10)})
	c.add(rect{9.8, 4.1, 5.0, 5.0}, shape{geom: "ellipse", fill: solidA("FFFFFF", 7)})
	c.add(rect{11.1, 0.9, 1.0, 1.0}, shape{geom: "ellipse", fill: linear(45, "FB923C", p.accent)})
	ring(c, 1.8, 6.2, 0.6, "FFFFFF", 1.5, 45, "")

	c.text(rect{1.0, 1.75, 11.33, 1.55}, textBody{anchor: "b", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.labels().Thanks, size: 46, bold: true, color: "FFFFFF", font: t.headFont}}}}})
	topic := run{text: d.Topic, size: 16, color: "FFFFFF", font: t.bodyFont}
	pw := min(10.5, textWidth(topic)+0.9)
	pill := rect{(slideW - pw) / 2, 3.6, pw, 0.85}
	c.add(pill, shape{geom: "roundRect", adj: []int{50000}, fill: solidA("FFFFFF", 16), text: &textBody{
		anchor: "ctr", fit: true, inset: [4]float64{0.35, 0.05, 0.35, 0.05}, paras: []para{{align: "ctr", runs: []run{topic}}}}})
	if d.StudentName != "" {
		c.text(rect{1.0, 4.85, 11.33, 0.55}, textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
			{text: d.labels().Student + ": ", size: 16, color: "FFFFFF", alpha: 75, font: t.bodyFont},
			{text: d.StudentName, size: 16, bold: true, color: "FFFFFF", font: t.bodyFont}}}}})
	}
}

func modernChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	c.add(rect{11.0, 5.2, 3.6, 3.6}, shape{geom: "ellipse", fill: linear(45, p.primary, p.secondary).withAlpha(8)})
	c.add(rect{-1.0, -1.15, 2.4, 2.4}, shape{geom: "ellipse", fill: solidA(p.accent, 10)})
	c.add(rect{0, 0, slideW, 0.09}, shape{fill: linear(0, p.primary, p.secondary, p.accent)})

	c.add(rect{0.5, 0.52, 0.09, 0.58}, shape{geom: "roundRect", adj: []int{50000}, fill: linear(90, p.primary, p.secondary)})
	c.text(rect{0.68, 0.33, 10.5, 0.95}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 28, p.text)}}}})
	pill := rect{11.42, 0.58, 1.23, 0.44}
	c.add(pill, shape{geom: "roundRect", adj: []int{50000}, fill: linear(0, p.primary, p.secondary), text: &textBody{
		anchor: "ctr", inset: [4]float64{0.02, 0.01, 0.02, 0.01},
		paras: []para{{align: "ctr", runs: []run{{text: pageLabel(s), size: 11, bold: true, color: "FFFFFF", font: t.bodyFont}}}}}})

	y := 1.45
	if s.lead != "" {
		c.text(rect{0.68, 1.24, 11.97, 0.64}, textBody{fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 15, p.muted)}}}})
		y = 2.0
	}
	c.text(rect{0.68, 7.02, 7.2, 0.32}, line1(s.topic, 10, p.muted, t.bodyFont, "l"))
	c.text(rect{7.95, 7.02, 4.7, 0.32}, line1(s.university, 10, p.muted, t.bodyFont, "r"))
	return rect{0.75, y, 11.9, 6.82 - y}
}

// ===========================================================================
// 3. MINIMAL — oq fon, ko'k-yashil aksent, nafis serif sarlavhalar
// ===========================================================================

var minimalTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeMinimal, Name: "Minimal", Emoji: "🌿",
		Description: "oq fon, nafis va toza",
	},
	pal: palette{
		bg: "FFFFFF", surface: "F5F7F6", surface2: "E7F2EF",
		primary: "1F2937", secondary: "374151", accent: "0F766E",
		text: "1F2937", muted: "6B7280", line: "E5E7EB", number: "0F766E",
		series:   []string{"0F766E", "1F2937", "0F766E", "1F2937"},
		onSeries: "FFFFFF", panel: solid("0F766E"), onPanel: "FFFFFF", shadow: "000000",
	},
	headFont: "Georgia", bodyFont: "Calibri", headBold: false, quoteItalic: true, badgeOutline: true,
	cards:      cardStyle{topBar: true},
	bulletChar: "–",
	signature:  []string{LayoutStatement, LayoutTimeline},
	transition: `<p:transition spd="med"><p:fade/></p:transition>`,
	cover:      minimalCover,
	closing:    minimalClosing,
	chrome:     minimalChrome,
}

// minimalRings panel ichidagi oq halqalar (oq fonga chiqqan qismi ko'rinmaydi).
func minimalRings(c *canvas, cx, cy float64) {
	for i, r := range []float64{0.9, 1.5, 2.1, 2.7, 3.3} {
		ring(c, cx, cy, r, "FFFFFF", 1, 32-i*5, "")
	}
}

func minimalCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid("FFFFFF")
	panel := rect{8.75, 0, slideW - 8.75, slideH}
	c.add(panel, shape{fill: solid(p.accent)})
	minimalRings(c, panel.X+panel.W/2, 3.75)
	c.text(rect{panel.X, 3.2, panel.W, 1.1}, line1(d.Year, 48, "FFFFFF", t.headFont, "ctr"))

	c.text(rect{0.9, 0.6, 7.4, 0.9}, textBody{fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 12, caps: true, spc: 2, color: p.muted, font: t.bodyFont}}}}})
	c.add(rect{1.0, 1.95, 0.6, 0.04}, shape{fill: solid(p.accent)})
	c.text(rect{0.9, 2.08, 7.4, 0.42}, textBody{paras: []para{{runs: []run{
		{text: d.WorkType, size: 13, bold: true, caps: true, spc: 5, color: p.accent, font: t.bodyFont}}}}})
	c.text(rect{0.9, 2.6, 7.5, 3.0}, t.titleBlock(d, 38, p.text, 17, p.muted, 0, "l"))

	c.add(rect{1.0, 5.75, 7.3, 0.012}, shape{fill: solid(p.line)})
	cols := []struct {
		r            rect
		label, value string
	}{
		{rect{0.9, 5.88, 3.6, 0.95}, strings.ToUpper(d.labels().Student), d.StudentName},
		{rect{4.6, 5.88, 3.8, 0.95}, strings.ToUpper(d.labels().Teacher), d.TeacherName},
	}
	for _, col := range cols {
		if col.value == "" {
			continue
		}
		c.text(col.r, textBody{fit: true, paras: []para{
			{runs: []run{{text: col.label, size: 10, spc: 2, color: p.muted, font: t.bodyFont}}},
			{before: 3, runs: []run{t.tx(col.value, 15, p.text)}},
		}})
	}
}

func minimalClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid("FFFFFF")
	panel := rect{0, 0, 4.58, slideH}
	c.add(panel, shape{fill: solid(p.accent)})
	minimalRings(c, panel.W/2, 3.75)

	c.text(rect{5.4, 1.6, 7.3, 2.2}, textBody{anchor: "b", fit: true, paras: []para{{runs: []run{
		{text: d.labels().Thanks, size: 42, color: p.text, font: t.headFont}}}}})
	c.add(rect{5.5, 3.98, 0.6, 0.04}, shape{fill: solid(p.accent)})
	c.text(rect{5.4, 4.12, 7.3, 1.2}, textBody{fit: true, paras: []para{{runs: []run{t.tx(d.Topic, 18, p.muted)}}}})
	if d.StudentName != "" {
		c.text(rect{5.4, 5.5, 7.3, 0.85}, textBody{paras: []para{
			{runs: []run{{text: strings.ToUpper(d.labels().Student), size: 10, spc: 2, color: p.muted, font: t.bodyFont}}},
			{before: 3, runs: []run{t.tx(d.StudentName, 15, p.text)}},
		}})
	}
}

func minimalChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	c.text(rect{9.9, 0.05, 2.85, 1.45}, textBody{paras: []para{{align: "r", runs: []run{
		{text: pad2(s.num), size: 72, color: "EDF1EF", font: t.headFont}}}}})
	c.add(rect{0.9, 0.6, 0.45, 0.035}, shape{fill: solid(p.accent)})
	c.text(rect{0.8, 0.66, 10.2, 0.8}, textBody{anchor: "b", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 30, p.text)}}}})
	if s.lead != "" {
		c.text(rect{0.8, 1.44, 11.4, 0.6}, textBody{fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 15, p.muted)}}}})
	}
	c.add(rect{0.9, 2.1, 11.73, 0.012}, shape{fill: solid(p.line)})
	c.add(rect{0.9, 2.09, 1.2, 0.03}, shape{fill: solid(p.accent)})

	c.text(rect{0.8, 7.0, 8.8, 0.32}, line1(s.topic, 10, p.muted, t.bodyFont, "l"))
	c.text(rect{9.8, 7.0, 2.93, 0.32}, line1(pageLabel(s), 10, p.muted, t.bodyFont, "r"))
	return rect{0.9, 2.32, 11.73, 4.5}
}

// ===========================================================================
// 4. TEXNO — to'q fon, neon moviy va yashil, texnologik uslub
// ===========================================================================

var techTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeTech, Name: "Texno", Emoji: "🌌",
		Description: "to'q fon, neon ranglar",
	},
	pal: palette{
		bg: "0B1120", surface: "111A2E", surface2: "152039",
		primary: "22D3EE", secondary: "A3E635", accent: "A3E635",
		text: "E2E8F0", muted: "94A3B8", line: "22304F", number: "22D3EE",
		series:   []string{"22D3EE", "A3E635", "F472B6", "FBBF24"},
		onSeries: "0B1120", panel: linear(35, "0E7490", "1E3A8A"), onPanel: "FFFFFF", shadow: "000000",
	},
	headFont: "Century Gothic", bodyFont: "Segoe UI", headBold: true,
	cards:      cardStyle{radius: 0.07, border: true, tab: true},
	bulletChar: "►",
	dark:       true,
	neon:       true,
	signature:  []string{LayoutCycle, LayoutHub, LayoutTimeline},
	transition: `<p:transition spd="med"><p:wipe dir="r"/></p:transition>`,
	cover:      techCover,
	closing:    techClosing,
	chrome:     techChrome,
}

func techBackdrop(t *theme, c *canvas) {
	p := t.pal
	c.darkMap = true
	c.bg = linear(45, "0B1120", "111B36")
	for x := 0.0; x < slideW; x += slideW / 16 {
		c.add(rect{x, 0, 0.008, slideH}, shape{fill: solidA(p.primary, 6)})
	}
	for y := slideH / 9; y < slideH; y += slideH / 9 {
		c.add(rect{0, y, slideW, 0.008}, shape{fill: solidA(p.primary, 6)})
	}
	// Burchak qavslari (HUD uslubi).
	const arm, th = 0.45, 0.03
	for _, cr := range [][2]float64{{0.5, 0.45}, {slideW - 0.5, 0.45}, {0.5, slideH - 0.45}, {slideW - 0.5, slideH - 0.45}} {
		x, y := cr[0], cr[1]
		hx, vy := x, y
		if x > slideW/2 {
			hx = x - arm
		}
		if y > slideH/2 {
			vy = y - arm
		}
		c.add(rect{hx, y - th/2, arm, th}, shape{fill: solidA(p.primary, 80)})
		c.add(rect{x - th/2, vy, th, arm}, shape{fill: solidA(p.primary, 80)})
	}
}

func techCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	techBackdrop(t, c)
	c.add(rect{8.3, 0.75, 4.9, 4.9}, shape{geom: "ellipse", fill: radialFade(p.primary, 32)})
	ring(c, 10.75, 3.2, 1.7, p.primary, 1.5, 70, "")
	ring(c, 10.75, 3.2, 2.05, p.secondary, 0.75, 45, "dash")
	c.add(rect{12.18, 2.05, 0.16, 0.16}, shape{geom: "ellipse", fill: solid(p.secondary), glow: &glow{radius: 6, color: p.secondary, alpha: 60}})
	c.text(rect{9.05, 2.7, 3.4, 1.0}, line1(d.Year, 40, "FFFFFF", t.headFont, "ctr"))

	c.text(rect{0.9, 0.75, 7.6, 0.8}, textBody{fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 12, caps: true, spc: 2, color: p.muted, font: t.bodyFont}}}}})
	c.text(rect{0.9, 2.0, 7.0, 0.42}, textBody{paras: []para{{runs: []run{
		{text: "// " + d.WorkType, size: 14, bold: true, spc: 3, color: p.primary, font: t.bodyFont}}}}})
	c.text(rect{0.9, 2.5, 7.4, 3.05}, t.titleBlock(d, 38, "FFFFFF", 17, p.muted, 0, "l"))
	c.add(rect{1.0, 5.62, 2.2, 0.05}, shape{fill: linear(0, p.primary, p.secondary)})
	c.text(rect{0.9, 5.8, 7.4, 1.0}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, p.primary, "FFFFFF", 0, "l")})
}

func techClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	techBackdrop(t, c)
	c.add(rect{slideW/2 - 2.6, 0.55, 5.2, 5.2}, shape{geom: "ellipse", fill: radialFade(p.primary, 24)})
	c.text(rect{1.0, 1.75, 11.33, 1.6}, textBody{anchor: "b", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.labels().Thanks, size: 42, bold: true, color: "FFFFFF", font: t.headFont}}}}})
	c.add(rect{slideW/2 - 1.1, 3.55, 2.2, 0.05}, shape{fill: linear(0, p.primary, p.secondary)})
	c.text(rect{1.6, 3.8, 10.1, 1.1}, textBody{fit: true, paras: []para{{align: "ctr", runs: []run{t.tx(d.Topic, 18, p.muted)}}}})
	if d.StudentName != "" {
		c.text(rect{1.6, 5.2, 10.1, 0.55}, textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
			{text: d.labels().Student + ": ", size: 16, bold: true, color: p.primary, font: t.bodyFont},
			t.tx(d.StudentName, 16, "FFFFFF")}}}})
	}
}

func techChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.darkMap = true
	c.bg = linear(90, "0B1120", "0E1629")
	for row := range 3 {
		for col := range 8 {
			c.add(rect{11.2 + float64(col)*0.2, 0.5 + float64(row)*0.2, 0.04, 0.04}, shape{geom: "ellipse", fill: solidA(p.primary, 40)})
		}
	}
	c.add(rect{0.75, 0.5, 0.06, 0.66}, shape{fill: linear(90, p.primary, p.secondary)})
	c.text(rect{0.92, 0.36, 10.1, 0.94}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 28, p.text)}}}})
	if s.lead != "" {
		c.text(rect{0.92, 1.3, 11.73, 0.62}, textBody{fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 14.5, p.muted)}}}})
	}
	c.add(rect{0.75, 2.0, 11.9, 0.01}, shape{fill: solid(p.line)})

	c.text(rect{0.75, 7.0, 9.4, 0.32}, line1(s.topic, 10, p.muted, t.bodyFont, "l"))
	c.text(rect{10.4, 7.0, 2.25, 0.32}, textBody{anchor: "ctr", paras: []para{{align: "r", runs: []run{
		{text: pageLabel(s), size: 11, bold: true, spc: 1, color: p.primary, font: t.bodyFont}}}}})
	return rect{0.75, 2.22, 11.9, 4.58}
}
