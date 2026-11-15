package generator

// drawing.go — slaydlarni koddan chizish uchun DrawingML primitivlari:
// shakllar (to'rtburchak, doira, strelka...), to'ldirish (rang, gradient,
// shaffoflik), soya va yorug'lik effektlari, matn bloklari va matnni maydonga
// sig'dirish (shrift o'lchamini avtomatik kichraytirish).
//
// Barcha koordinatalar dyuymlarda (slayd 13.333" x 7.5", 16:9) beriladi va
// XML yozishda EMU ga o'giriladi.

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	emuPerInch  = 914400
	slideWidth  = 12192000 // 13.333" — 16:9
	slideHeight = 6858000  // 7.5"

	slideW = 13.333 // dyuymlarda
	slideH = 7.5
)

func emu(v float64) int64 { return int64(math.Round(v * emuPerInch)) }

// rect — dyuymlardagi to'rtburchak maydon.
type rect struct{ X, Y, W, H float64 }

func (r rect) right() float64  { return r.X + r.W }
func (r rect) bottom() float64 { return r.Y + r.H }

// inset maydonni har tomondan d ga toraytiradi.
func (r rect) inset(dx, dy float64) rect {
	return rect{r.X + dx, r.Y + dy, max(0, r.W-2*dx), max(0, r.H-2*dy)}
}

// cols maydonni gap oraliq bilan n ta teng ustunga bo'ladi.
func (r rect) cols(n int, gap float64) []rect {
	n = max(n, 1)
	w := (r.W - gap*float64(n-1)) / float64(n)
	out := make([]rect, n)
	for i := range out {
		out[i] = rect{r.X + float64(i)*(w+gap), r.Y, w, r.H}
	}
	return out
}

// rows maydonni gap oraliq bilan n ta teng qatorga bo'ladi.
func (r rect) rows(n int, gap float64) []rect {
	n = max(n, 1)
	h := (r.H - gap*float64(n-1)) / float64(n)
	out := make([]rect, n)
	for i := range out {
		out[i] = rect{r.X, r.Y + float64(i)*(h+gap), r.W, h}
	}
	return out
}

// grid maydonni cols x rows katakka bo'ladi (qatorma-qator tartibda).
func (r rect) grid(cols, rows int, gapX, gapY float64) []rect {
	out := make([]rect, 0, cols*rows)
	for _, row := range r.rows(rows, gapY) {
		out = append(out, row.cols(cols, gapX)...)
	}
	return out
}

// splitX maydonni chap (w kenglikdagi) va o'ng qismga ajratadi.
func (r rect) splitX(w, gap float64) (left, right rect) {
	return rect{r.X, r.Y, w, r.H}, rect{r.X + w + gap, r.Y, max(0, r.W-w-gap), r.H}
}

// splitY maydonni yuqori (h balandlikdagi) va pastki qismga ajratadi.
func (r rect) splitY(h, gap float64) (top, rest rect) {
	return rect{r.X, r.Y, r.W, h}, rect{r.X, r.Y + h + gap, r.W, max(0, r.H-h-gap)}
}

// ---------------------------------------------------------------------------
// To'ldirish, chiziq va effektlar
// ---------------------------------------------------------------------------

// paint — shakl yoki fon to'ldirilishi. Nol qiymat — to'ldirilmagan.
type paint struct {
	color  string   // RRGGBB (solid)
	alpha  int      // shaffof emaslik foizi 1..100 (0 — 100%)
	stops  []string // gradient ranglari (kamida 2 ta bo'lsa — gradient)
	alphas []int    // gradient to'xtash nuqtalari shaffofligi (ixtiyoriy)
	angle  float64  // chiziqli gradient burchagi (gradus; 0 — chapdan o'ngga, 90 — yuqoridan pastga)
	radial bool     // markazdan tarqaluvchi gradient
}

func solid(c string) paint             { return paint{color: c} }
func solidA(c string, alpha int) paint { return paint{color: c, alpha: alpha} }
func linear(angle float64, colors ...string) paint {
	return paint{stops: colors, angle: angle}
}

// radialFade markazda color (alpha foiz), chetlarda to'liq shaffof doira.
func radialFade(color string, alpha int) paint {
	return paint{stops: []string{color, color}, alphas: []int{alpha, 0}, radial: true}
}

func (p paint) none() bool { return p.color == "" && len(p.stops) < 2 }

// withAlpha rangga shaffoflik qo'shadi.
func (p paint) withAlpha(a int) paint {
	p.alpha = a
	if len(p.stops) >= 2 {
		p.alphas = make([]int, len(p.stops))
		for i := range p.alphas {
			p.alphas[i] = a
		}
	}
	return p
}

func writeColor(b *strings.Builder, c string, alpha int) {
	if alpha > 0 && alpha < 100 {
		fmt.Fprintf(b, `<a:srgbClr val="%s"><a:alpha val="%d"/></a:srgbClr>`, c, alpha*1000)
		return
	}
	fmt.Fprintf(b, `<a:srgbClr val="%s"/>`, c)
}

func (p paint) write(b *strings.Builder) {
	switch {
	case len(p.stops) >= 2:
		b.WriteString(`<a:gradFill rotWithShape="1"><a:gsLst>`)
		last := len(p.stops) - 1
		for i, c := range p.stops {
			a := p.alpha
			if i < len(p.alphas) {
				a = p.alphas[i]
				if a == 0 {
					a = -1 // to'liq shaffof
				}
			}
			fmt.Fprintf(b, `<a:gs pos="%d">`, i*100000/last)
			if a < 0 {
				fmt.Fprintf(b, `<a:srgbClr val="%s"><a:alpha val="0"/></a:srgbClr>`, c)
			} else {
				writeColor(b, c, a)
			}
			b.WriteString(`</a:gs>`)
		}
		b.WriteString(`</a:gsLst>`)
		if p.radial {
			b.WriteString(`<a:path path="circle"><a:fillToRect l="50000" t="50000" r="50000" b="50000"/></a:path>`)
		} else {
			fmt.Fprintf(b, `<a:lin ang="%d" scaled="0"/>`, int(math.Round(p.angle*60000)))
		}
		b.WriteString(`</a:gradFill>`)
	case p.color != "":
		b.WriteString(`<a:solidFill>`)
		writeColor(b, p.color, p.alpha)
		b.WriteString(`</a:solidFill>`)
	default:
		b.WriteString(`<a:noFill/>`)
	}
}

// stroke — shakl chegarasi.
type stroke struct {
	color string
	width float64 // pt
	alpha int
	dash  string // "dash", "sysDot" ...; bo'sh — yaxlit chiziq
}

// shadow — tashqi soya.
type shadow struct {
	blur, dist float64 // pt
	dir        float64 // gradus (90 — pastga)
	color      string
	alpha      int
}

// glow — shakl atrofidagi yorug'lik.
type glow struct {
	radius float64 // pt
	color  string
	alpha  int
}

// ---------------------------------------------------------------------------
// Matn
// ---------------------------------------------------------------------------

// run — bir xil formatdagi matn bo'lagi.
type run struct {
	text   string
	size   float64 // pt
	bold   bool
	italic bool
	caps   bool
	color  string
	alpha  int
	font   string
	spc    float64 // harflar orasidagi qo'shimcha masofa, pt
}

// bullet — paragraf belgisi (marker).
type bullet struct {
	char  string
	color string
	font  string
	pct   int // marker o'lchami, matnga nisbatan foizda (0 — 100%)
}

// para — paragraf.
type para struct {
	runs   []run
	align  string  // l | ctr | r | just (bo'sh — l)
	before float64 // oldidan masofa, pt
	after  float64 // keyin masofa, pt
	lineSp float64 // qator oralig'i koeffitsiyenti (0 — 1.0)
	bullet *bullet
	marL   float64 // chap chekinish, dyuym
	indent float64 // birinchi qator chekinishi (marker uchun manfiy), dyuym
}

// textBody — shakl ichidagi matn.
type textBody struct {
	paras  []para
	anchor string     // t | ctr | b
	inset  [4]float64 // chap, yuqori, o'ng, past ichki chekinishlar (dyuym)
	fit    bool       // matnni maydonga sig'dirish (shriftni kichraytirish)
	minFit float64    // fit uchun eng kichik masshtab (0 — 0.55)
}

// defaultInset — PowerPoint'ning standart ichki chekinishlari.
var defaultInset = [4]float64{0.1, 0.05, 0.1, 0.05}

// ---------------------------------------------------------------------------
// Shakl va kanva
// ---------------------------------------------------------------------------

// shape — slayddagi bitta shakl.
type shape struct {
	geom   string // prstGeom nomi: rect, roundRect, ellipse, chevron, homePlate ...
	adj    []int  // geometriya parametrlari (adj, adj1, adj2 ...)
	fill   paint
	line   *stroke
	shadow *shadow
	glow   *glow
	rot    float64 // gradus
	flipH  bool
	text   *textBody
}

// canvas — bitta slaydning shakllar daraxti.
type canvas struct {
	b      strings.Builder
	nextID int
	bg     paint
	// clrMapOvr — slayd uchun rang xaritasi (to'q fonli mavzularda matn
	// ranglari almashtiriladi); bo'sh bo'lsa — master'niki.
	darkMap bool
	// pics — slaydga joylangan rasmlar; i-rasm slayd .rels faylida rId{i+2}
	// (rId1 — slayd maketi).
	pics []*Picture
}

func newCanvas(bg paint) *canvas { return &canvas{nextID: 2, bg: bg} }

// add shaklni kanvaga qo'shadi.
func (c *canvas) add(r rect, s shape) {
	id := c.nextID
	c.nextID++
	b := &c.b

	geom := s.geom
	if geom == "" {
		geom = "rect"
	}
	isTextBox := s.text != nil && s.fill.none() && s.line == nil && geom == "rect"
	name, nv := "Shape", ""
	if isTextBox {
		name, nv = "TextBox", ` txBox="1"`
	}
	fmt.Fprintf(b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s %d"/><p:cNvSpPr%s/><p:nvPr/></p:nvSpPr><p:spPr>`, id, name, id, nv)

	b.WriteString(`<a:xfrm`)
	if s.rot != 0 {
		fmt.Fprintf(b, ` rot="%d"`, int(math.Round(s.rot*60000)))
	}
	if s.flipH {
		b.WriteString(` flipH="1"`)
	}
	fmt.Fprintf(b, `><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`,
		emu(r.X), emu(r.Y), max(emu(r.W), 1), max(emu(r.H), 1))

	fmt.Fprintf(b, `<a:prstGeom prst="%s"><a:avLst>`, geom)
	for i, v := range s.adj {
		n := "adj"
		if len(s.adj) > 1 {
			n = fmt.Sprintf("adj%d", i+1)
		}
		fmt.Fprintf(b, `<a:gd name="%s" fmla="val %d"/>`, n, v)
	}
	b.WriteString(`</a:avLst></a:prstGeom>`)

	s.fill.write(b)

	if l := s.line; l != nil {
		fmt.Fprintf(b, `<a:ln w="%d">`, int(math.Round(l.width*12700)))
		solidA(l.color, l.alpha).write(b)
		if l.dash != "" {
			fmt.Fprintf(b, `<a:prstDash val="%s"/>`, l.dash)
		}
		b.WriteString(`</a:ln>`)
	} else {
		b.WriteString(`<a:ln><a:noFill/></a:ln>`)
	}

	if s.glow != nil || s.shadow != nil {
		b.WriteString(`<a:effectLst>`)
		if g := s.glow; g != nil {
			fmt.Fprintf(b, `<a:glow rad="%d">`, int(g.radius*12700))
			writeColor(b, g.color, g.alpha)
			b.WriteString(`</a:glow>`)
		}
		if sh := s.shadow; sh != nil {
			fmt.Fprintf(b, `<a:outerShdw blurRad="%d" dist="%d" dir="%d" algn="t" rotWithShape="0">`,
				int(sh.blur*12700), int(sh.dist*12700), int(sh.dir*60000))
			writeColor(b, sh.color, sh.alpha)
			b.WriteString(`</a:outerShdw>`)
		}
		b.WriteString(`</a:effectLst>`)
	}
	b.WriteString(`</p:spPr>`)

	if s.text != nil {
		writeTextBody(b, r, s.text)
	}
	b.WriteString(`</p:sp>`)
}

// picFrame — rasm ramkasi.
type picFrame struct {
	radius float64 // burchak radiusi, dyuym (0 — to'g'ri burchak)
	line   *stroke
	shadow *shadow
}

// picRelID rasm uchun slayd ichidagi bog'lanish identifikatorini qaytaradi
// (bir xil rasm bir slaydda qayta ishlatilsa — o'sha identifikator).
func (c *canvas) picRelID(p *Picture) string {
	for i, q := range c.pics {
		if q == p {
			return fmt.Sprintf("rId%d", i+2)
		}
	}
	c.pics = append(c.pics, p)
	return fmt.Sprintf("rId%d", len(c.pics)+1)
}

// pic rasmni r maydonga "cover" usulida (proporsiyani saqlab, ortiqchasini
// qirqib) joylaydi. p nil bo'lsa hech narsa chizilmaydi.
func (c *canvas) pic(r rect, p *Picture, f picFrame) {
	if p == nil {
		return
	}
	rid := c.picRelID(p)
	id := c.nextID
	c.nextID++
	b := &c.b

	fmt.Fprintf(b, `<p:pic><p:nvPicPr><p:cNvPr id="%d" name="Picture %d"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr>`, id, id)
	fmt.Fprintf(b, `<p:blipFill><a:blip r:embed="%s"/>`, rid)
	if l, t, rr, bb := coverCrop(p.W, p.H, r); l+t+rr+bb > 0 {
		fmt.Fprintf(b, `<a:srcRect l="%d" t="%d" r="%d" b="%d"/>`, l, t, rr, bb)
	}
	b.WriteString(`<a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr>`)
	fmt.Fprintf(b, `<a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`,
		emu(r.X), emu(r.Y), max(emu(r.W), 1), max(emu(r.H), 1))
	if f.radius > 0 {
		fmt.Fprintf(b, `<a:prstGeom prst="roundRect"><a:avLst><a:gd name="adj" fmla="val %d"/></a:avLst></a:prstGeom>`, roundAdj(r, f.radius)[0])
	} else {
		b.WriteString(`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom>`)
	}
	if l := f.line; l != nil {
		fmt.Fprintf(b, `<a:ln w="%d">`, int(math.Round(l.width*12700)))
		solidA(l.color, l.alpha).write(b)
		b.WriteString(`</a:ln>`)
	}
	if sh := f.shadow; sh != nil {
		fmt.Fprintf(b, `<a:effectLst><a:outerShdw blurRad="%d" dist="%d" dir="%d" algn="t" rotWithShape="0">`,
			int(sh.blur*12700), int(sh.dist*12700), int(sh.dir*60000))
		writeColor(b, sh.color, sh.alpha)
		b.WriteString(`</a:outerShdw></a:effectLst>`)
	}
	b.WriteString(`</p:spPr></p:pic>`)
}

// text — fonsiz matn bloki.
func (c *canvas) text(r rect, tb textBody) {
	c.add(r, shape{text: &tb})
}

// xml slaydning to'liq XML matnini qaytaradi.
func (c *canvas) xml(transition string) string {
	var b strings.Builder
	b.Grow(c.b.Len() + 1024)
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
		`xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld>`)
	if !c.bg.none() {
		b.WriteString(`<p:bg><p:bgPr>`)
		c.bg.write(&b)
		b.WriteString(`<a:effectLst/></p:bgPr></p:bg>`)
	}
	b.WriteString(`<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`)
	b.WriteString(c.b.String())
	b.WriteString(`</p:spTree></p:cSld>`)
	if c.darkMap {
		// To'q fonda foydalanuvchi qo'shgan yangi matn ham och rangda bo'lishi uchun.
		b.WriteString(`<p:clrMapOvr><a:overrideClrMapping bg1="dk1" tx1="lt1" bg2="dk2" tx2="lt2" ` +
			`accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" ` +
			`accent6="accent6" hlink="hlink" folHlink="folHlink"/></p:clrMapOvr>`)
	} else {
		b.WriteString(`<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>`)
	}
	b.WriteString(transition)
	b.WriteString(`</p:sld>`)
	return b.String()
}

func writeTextBody(b *strings.Builder, r rect, tb *textBody) {
	ins := tb.inset
	if ins == [4]float64{} {
		ins = defaultInset
	}
	if tb.fit {
		fitText(tb, r, ins)
	}
	anchor := tb.anchor
	if anchor == "" {
		anchor = "t"
	}
	fmt.Fprintf(b, `<p:txBody><a:bodyPr wrap="square" lIns="%d" tIns="%d" rIns="%d" bIns="%d" rtlCol="0" anchor="%s">`,
		emu(ins[0]), emu(ins[1]), emu(ins[2]), emu(ins[3]), anchor)
	if tb.fit {
		// Foydalanuvchi matnni tahrirlaganda PowerPoint uni o'zi sig'diradi.
		b.WriteString(`<a:normAutofit/>`)
	} else {
		b.WriteString(`<a:noAutofit/>`)
	}
	b.WriteString(`</a:bodyPr><a:lstStyle/>`)
	if len(tb.paras) == 0 {
		b.WriteString(`<a:p><a:endParaRPr lang="uz-Latn-UZ" dirty="0"/></a:p>`)
	}
	for _, p := range tb.paras {
		writePara(b, p)
	}
	b.WriteString(`</p:txBody>`)
}

func writePara(b *strings.Builder, p para) {
	b.WriteString(`<a:p><a:pPr`)
	if p.marL != 0 {
		fmt.Fprintf(b, ` marL="%d"`, emu(p.marL))
	}
	if p.indent != 0 {
		fmt.Fprintf(b, ` indent="%d"`, emu(p.indent))
	}
	if p.align != "" {
		fmt.Fprintf(b, ` algn="%s"`, p.align)
	}
	b.WriteString(`>`)
	if p.lineSp != 0 {
		fmt.Fprintf(b, `<a:lnSpc><a:spcPct val="%d"/></a:lnSpc>`, int(math.Round(p.lineSp*100000)))
	}
	if p.before != 0 {
		fmt.Fprintf(b, `<a:spcBef><a:spcPts val="%d"/></a:spcBef>`, int(math.Round(p.before*100)))
	}
	if p.after != 0 {
		fmt.Fprintf(b, `<a:spcAft><a:spcPts val="%d"/></a:spcAft>`, int(math.Round(p.after*100)))
	}
	if bu := p.bullet; bu != nil {
		if bu.color != "" {
			fmt.Fprintf(b, `<a:buClr><a:srgbClr val="%s"/></a:buClr>`, bu.color)
		}
		if bu.pct != 0 {
			fmt.Fprintf(b, `<a:buSzPct val="%d"/>`, bu.pct*1000)
		}
		font := bu.font
		if font == "" {
			font = "Arial"
		}
		fmt.Fprintf(b, `<a:buFont typeface="%s"/><a:buChar char="%s"/>`, xmlEscape(font), xmlEscape(bu.char))
	} else {
		b.WriteString(`<a:buNone/>`)
	}
	b.WriteString(`</a:pPr>`)

	var last run
	for _, r := range p.runs {
		if r.text == "" {
			continue
		}
		last = r
		b.WriteString(`<a:r>`)
		writeRunProps(b, "a:rPr", r)
		fmt.Fprintf(b, `<a:t>%s</a:t></a:r>`, xmlEscape(r.text))
	}
	if last.size > 0 {
		writeRunProps(b, "a:endParaRPr", last)
	}
	b.WriteString(`</a:p>`)
}

func writeRunProps(b *strings.Builder, tag string, r run) {
	fmt.Fprintf(b, `<%s lang="uz-Latn-UZ" sz="%d"`, tag, int(math.Round(r.size*100)))
	if r.bold {
		b.WriteString(` b="1"`)
	} else {
		b.WriteString(` b="0"`)
	}
	if r.italic {
		b.WriteString(` i="1"`)
	}
	if r.caps {
		b.WriteString(` cap="all"`)
	}
	if r.spc != 0 {
		fmt.Fprintf(b, ` spc="%d"`, int(math.Round(r.spc*100)))
	}
	b.WriteString(` dirty="0">`)
	if r.color != "" {
		solidA(r.color, r.alpha).write(b)
	}
	if r.font != "" {
		f := xmlEscape(r.font)
		fmt.Fprintf(b, `<a:latin typeface="%s"/><a:cs typeface="%s"/>`, f, f)
	}
	fmt.Fprintf(b, `</%s>`, tag)
}

// ---------------------------------------------------------------------------
// Matnni maydonga sig'dirish
// ---------------------------------------------------------------------------

// Shriftlarning o'rtacha harf kengligi (em ulushida). Qiymatlar biroz zaxira
// bilan olingan: shrift o'rnida boshqasi (masalan LibreOffice yoki telefonda)
// ishlatilganda ham matn maydondan chiqmasligi kerak.
var fontWidths = map[string]float64{
	"Calibri":               0.50,
	"Calibri Light":         0.49,
	"Cambria":               0.54,
	"Georgia":               0.56,
	"Segoe UI":              0.54,
	"Century Gothic":        0.58,
	"Arial":                 0.53,
	"Segoe UI Symbol":       0.60,
	"Trebuchet MS":          0.53,
	"Franklin Gothic":       0.50,
	"Arial Black":           0.66,
	"Verdana":               0.60,
	"Tahoma":                0.53,
	"Comic Sans MS":         0.56,
	"Courier New":           0.62,
	"Arial Rounded MT Bold": 0.57,
}

func charWidth(r run) float64 {
	w, ok := fontWidths[r.font]
	if !ok {
		w = 0.55
	}
	if r.bold {
		w *= 1.07
	}
	if r.caps {
		w *= 1.28
	}
	return w
}

// measureText matnning k masshtabdagi taxminiy balandligini (dyuym) hisoblaydi.
func measureText(paras []para, width, k float64) float64 {
	total := 0.0
	for i, p := range paras {
		avail := width - p.marL
		if avail <= 0.2 {
			avail = 0.2
		}
		// So'z ko'chishi tufayli qator oxirida bo'sh joy qoladi — tor ustunlarda ko'proq.
		wrap := 0.92
		if avail < 2.5 {
			wrap = 0.86
		}
		textW, maxSize := 0.0, 0.0
		for _, r := range p.runs {
			sz := r.size * k
			perChar := (sz*charWidth(r) + r.spc) / 72
			textW += float64(utf8.RuneCountInString(r.text)) * perChar
			maxSize = max(maxSize, sz)
			// Eng uzun so'z qatorga sig'masa, u o'rtasidan bo'linib ketadi — bunga yo'l qo'ymaymiz.
			if float64(longestWord(r.text))*perChar > avail*0.97 {
				return math.Inf(1)
			}
		}
		if maxSize == 0 {
			continue
		}
		lines := math.Max(1, math.Ceil(textW/(avail*wrap)))
		ls := p.lineSp
		if ls == 0 {
			ls = 1
		}
		total += lines * maxSize * 1.2 * ls / 72
		if i > 0 {
			total += p.before * k / 72
		}
		if i < len(paras)-1 {
			total += p.after * k / 72
		}
	}
	return total
}

// longestWord matndagi eng uzun so'z uzunligi (belgilarda).
func longestWord(s string) int {
	best := 0
	for _, w := range strings.Fields(s) {
		best = max(best, utf8.RuneCountInString(w))
	}
	return best
}

// fitScale matn r maydonga sig'adigan eng katta masshtabni tanlaydi:
// maxScale dan (bo'sh joy ko'p bo'lsa matn kattalashadi) minScale gacha.
func fitScale(paras []para, r rect, ins [4]float64, minScale, maxScale float64) float64 {
	if minScale <= 0 {
		minScale = 0.55
	}
	maxScale = max(maxScale, 1)
	w := r.W - ins[0] - ins[2]
	h := r.H - ins[1] - ins[3]
	for k := maxScale; k > minScale; k -= 0.04 {
		if measureText(paras, w, k) <= h {
			return k
		}
	}
	return minScale
}

// scaleParas shrift o'lchamlari va paragraf oraliqlarini k marta o'zgartiradi.
func scaleParas(paras []para, k float64) {
	if k == 1 {
		return
	}
	for i := range paras {
		p := &paras[i]
		p.before *= k
		p.after *= k
		for j := range p.runs {
			// Yarim punktgacha yaxlitlanadi — PowerPoint'dagi kabi.
			p.runs[j].size = math.Max(8, math.Round(p.runs[j].size*k*2)/2)
		}
	}
}

func fitText(tb *textBody, r rect, ins [4]float64) {
	scaleParas(tb.paras, fitScale(tb.paras, r, ins, tb.minFit, 1))
}

// insets matn blokining ichki chekinishlari.
func (tb *textBody) insets() [4]float64 {
	if tb.inset == [4]float64{} {
		return defaultInset
	}
	return tb.inset
}

// textWidth bir qatorli matn kengligini taxminlaydi (dyuym).
func textWidth(r run) float64 {
	return float64(utf8.RuneCountInString(r.text)) * (r.size*charWidth(r) + r.spc) / 72
}

// roundAdj roundRect burchak radiusini (dyuym) adj qiymatiga o'giradi.
func roundAdj(r rect, radius float64) []int {
	m := math.Min(r.W, r.H)
	if m <= 0 {
		return nil
	}
	return []int{int(math.Min(50000, radius/m*100000))}
}
