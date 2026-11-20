package generator

// custom.go — AI yaratgan maxsus dizayn ("custom"). Tayyor dizaynlardan
// farqli ravishda ranglar, shriftlar va uslub elementlari koddan emas, AI
// mavzuga qarab tanlagan spetsifikatsiyadan (CustomSpec) olinadi: palitra,
// shriftlar juftligi, titul kompozitsiyasi, sarlavha uslubi, kartochka va
// belgi turi, fon motivi hamda rasmlar uslubi (art direction).
//
// AI xato qilsa ham taqdimot buzilmasligi uchun generator:
//   - noma'lum variant va shriftlarni xavfsiz standart qiymatlarga almashtiradi;
//   - ranglar kontrastini WCAG mezonlari bo'yicha avtomatik tuzatadi (matn
//     fonda doim o'qiladi, raqamli belgilar ustidagi yozuv ko'rinadi);
//   - qolgan ranglarni (chiziqlar, panel, ketma-ket elementlar, soya)
//     asosiy palitradan hisoblaydi.
//
// Mazmunli slaydlar maketlari umumiy (layouts.go, layouts_extra.go) — ular
// maxsus dizayn palitrasi va uslubida chiziladi. Rasmlar slaydlarning bir
// qismiga qo'yiladi (Design.PhotoSlides) — qolganlari rasmsiz, lekin bezakli.

import (
	"math"
	"slices"
	"strings"

	"telegram-presentation-bot/internal/design"
)

// ThemeCustom — AI maxsus dizayni identifikatori (Session.Design da saqlanadi).
const ThemeCustom = "custom"

// Maxsus dizayn variantlari — design paketidagi qiymatlarning taxalluslari.
const (
	CoverSplit = design.CoverSplit
	CoverFull  = design.CoverFull
	CoverFrame = design.CoverFrame

	HeaderBar       = design.HeaderBar
	HeaderBand      = design.HeaderBand
	HeaderUnderline = design.HeaderUnderline

	CardsShadow  = design.CardsShadow
	CardsOutline = design.CardsOutline
	CardsTint    = design.CardsTint
	CardsAccent  = design.CardsAccent
	CardsTop     = design.CardsTop

	CornersRounded = design.CornersRounded
	CornersSquare  = design.CornersSquare

	MarkerCircle  = design.MarkerCircle
	MarkerSquare  = design.MarkerSquare
	MarkerOutline = design.MarkerOutline

	MotifCircles  = design.MotifCircles
	MotifDiagonal = design.MotifDiagonal
	MotifDots     = design.MotifDots
	MotifWaves    = design.MotifWaves
	MotifNone     = design.MotifNone

	PhotoLeft  = design.PhotoLeft
	PhotoRight = design.PhotoRight
)

// CustomSpec — AI tanlagan dizayn parametrlari (design.CustomSpec).
type CustomSpec = design.CustomSpec

// normalizeCustomSpec spetsifikatsiya nusxasini noma'lum variantlarni
// standart qiymatlarga almashtirgan holda qaytaradi (nil — barcha qiymatlar
// standart).
func normalizeCustomSpec(sp *CustomSpec) CustomSpec {
	var out CustomSpec
	if sp != nil {
		out = *sp
	}
	oneOf := func(v string, allowed []string, def string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		if slices.Contains(allowed, v) {
			return v
		}
		return def
	}
	font := func(v string, allowed []string, def string) string {
		for _, f := range allowed {
			if strings.EqualFold(strings.TrimSpace(v), f) {
				return f
			}
		}
		return def
	}
	out.Name = strings.TrimSpace(out.Name)
	if out.Name == "" {
		out.Name = "Custom Design"
	}
	out.HeadingFont = font(out.HeadingFont, design.HeadingFonts, "Segoe UI")
	out.BodyFont = font(out.BodyFont, design.BodyFonts, "Calibri")
	out.Cover = oneOf(out.Cover, design.Covers, CoverSplit)
	out.Header = oneOf(out.Header, design.Headers, HeaderBar)
	out.Cards = oneOf(out.Cards, design.Cards, CardsShadow)
	out.Corners = oneOf(out.Corners, design.Corners, CornersRounded)
	out.Markers = oneOf(out.Markers, design.Markers, MarkerCircle)
	out.Motif = oneOf(out.Motif, design.Motifs, MotifCircles)
	out.PhotoSide = oneOf(out.PhotoSide, design.Sides, PhotoRight)
	out.PhotoStyle = strings.TrimSpace(out.PhotoStyle)

	var sig []string
	for _, l := range out.Signature {
		l = strings.ToLower(strings.TrimSpace(l))
		if slices.Contains(design.SignatureLayouts, l) && !slices.Contains(sig, l) {
			sig = append(sig, l)
		}
		if len(sig) == 3 {
			break
		}
	}
	out.Signature = sig
	return out
}

// ---------------------------------------------------------------------------
// Ranglar va kontrast
// ---------------------------------------------------------------------------

// relLum — WCAG nisbiy yorqinligi (0 — qora, 1 — oq).
func relLum(hex string) float64 {
	c := hexColor(hex)
	ch := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

// contrast — ikki rang orasidagi WCAG kontrast nisbati (1..21).
func contrast(a, b string) float64 {
	la, lb := relLum(a), relLum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// ensureContrast c rangini toward (odatda qora yoki oq) tomon bosqichma-bosqich
// aralashtirib, barcha bgs ranglari bilan kontrasti kamida min bo'lishiga
// erishadi. Rang allaqachon mos bo'lsa — o'zgarmaydi (AI tanlovi saqlanadi).
func ensureContrast(c string, bgs []string, min float64, toward string) string {
	for k := 0.0; k <= 1.0001; k += 0.04 {
		x := mixHex(c, toward, k)
		ok := true
		for _, bg := range bgs {
			if contrast(x, bg) < min {
				ok = false
				break
			}
		}
		if ok {
			return x
		}
	}
	return toward
}

// darkBg — fon to'q (matn och rangda) ekanini bildiradi.
func darkBg(bg string) bool { return relLum(bg) < 0.2 }

// customColors — tuzatilgan va hisoblangan ranglar (palette dan tashqari
// maxsus dizayn chizishda kerak bo'ladiganlari).
type customColors struct {
	panel string // urg'u paneli va tasma rangi: ustidagi oq matn o'qiladi (≥ 4.5)
	shade string // rasm ustidagi qoplama rangi (oq matn uchun to'q)
}

// customPalette AI ranglaridan to'liq palitrani yig'adi va kontrastni tuzatadi.
func customPalette(sp CustomSpec) (palette, customColors, bool) {
	pick := func(v, def string) string {
		if h, ok := design.ParseHex(v); ok {
			return h
		}
		return def
	}
	bg := pick(sp.Background, "F6F7FB")
	dark := darkBg(bg)
	// Slayd foni yo juda och, yo juda to'q bo'lishi kerak: o'rtacha tonda
	// (masalan, kulrang) hech bir matn rangi yetarli kontrast bermaydi.
	if dark {
		bg = ensureContrast(bg, []string{"FFFFFF"}, 12, "000000")
	} else {
		bg = ensureContrast(bg, []string{"000000"}, 15, "FFFFFF")
	}
	ink := "000000" // matnni kuchaytirish yo'nalishi
	defText, defSurface := "1B2333", mixHex(bg, "FFFFFF", 0.75)
	if dark {
		ink, defText, defSurface = "FFFFFF", "F1F4F8", mixHex(bg, "FFFFFF", 0.07)
	}

	surface := pick(sp.Surface, defSurface)
	if dark {
		// Kartochka foni to'q fondan biroz yorug'roq bo'lsin, lekin och emas.
		if relLum(surface) > 0.25 || contrast(surface, bg) > 2.5 {
			surface = defSurface
		}
	} else if relLum(surface) < 0.72 {
		surface = defSurface
	}

	text := ensureContrast(pick(sp.Text, defText), []string{bg, surface}, 7, ink)
	muted := ensureContrast(pick(sp.Muted, mixHex(text, bg, 0.42)), []string{bg, surface}, 4.6, ink)
	primary := ensureContrast(pick(sp.Primary, "2B4C7E"), []string{bg}, 3, ink)
	secondary := ensureContrast(pick(sp.Secondary, mixHex(primary, bg, 0.3)), []string{bg}, 3, ink)
	accent := ensureContrast(pick(sp.Accent, "E07A2F"), []string{bg, surface}, 2.6, ink)

	// Ketma-ket elementlar (kartochka chiziqlari, raqamli belgilar): och
	// mavzuda belgilar ustida oq yozuv, to'q mavzuda — fon rangidagi yozuv.
	series := []string{primary, accent, secondary, mixHex(primary, accent, 0.5)}
	onSeries := "FFFFFF"
	for i, s := range series {
		if dark {
			series[i] = ensureContrast(s, []string{bg, surface}, 4.5, "FFFFFF")
		} else {
			series[i] = ensureContrast(s, []string{"FFFFFF", surface}, 3.2, "000000")
		}
	}
	if dark {
		onSeries = bg
	}

	panel := ensureContrast(primary, []string{"FFFFFF"}, 4.6, "000000")
	shade := ensureContrast(mixHex(panel, "000000", 0.35), []string{"FFFFFF"}, 9, "000000")
	shadowCol := mixHex(primary, "000000", 0.55)
	line := mixHex(bg, text, 0.13)
	if dark {
		shadowCol, line = "000000", mixHex(bg, text, 0.18)
	}
	pal := palette{
		bg: bg, surface: surface, surface2: mixHex(bg, primary, 0.08),
		primary: primary, secondary: secondary, accent: accent,
		text: text, muted: muted, line: line,
		number: ensureContrast(accent, []string{bg, surface}, 3, ink),
		series: series, onSeries: onSeries,
		panel: linear(35, panel, mixHex(panel, "000000", 0.22)), onPanel: "FFFFFF",
		shadow: shadowCol,
	}
	return pal, customColors{panel: panel, shade: shade}, dark
}

// ---------------------------------------------------------------------------
// Mavzuni yig'ish
// ---------------------------------------------------------------------------

// customTheme spetsifikatsiyadan dizayn mavzusini yaratadi (nil — standart
// qiymatlar bilan).
func customTheme(spec *CustomSpec) *theme {
	sp := normalizeCustomSpec(spec)
	pal, colors, dark := customPalette(sp)

	radius := 0.16
	if sp.Corners == CornersSquare {
		radius = 0
	}
	cards := cardStyle{radius: radius}
	switch sp.Cards {
	case CardsShadow:
		cards.shadow = true
	case CardsOutline:
		cards.border = true
	case CardsTint:
		cards.tint = true
	case CardsAccent:
		cards.shadow, cards.leftBar = true, true
	case CardsTop:
		cards.shadow, cards.topBar = true, true
	}
	if dark && cards.shadow {
		// To'q fonda soya ko'rinmaydi — o'rniga ingichka chegara.
		cards.shadow, cards.border = false, true
	}

	frame := picFrame{radius: min(radius*1.4, 0.25)}
	if dark {
		frame.line = &stroke{color: pal.primary, width: 1.25, alpha: 80}
	} else {
		frame.shadow = &shadow{blur: 18, dist: 4, dir: 90, color: pal.shadow, alpha: 22}
	}

	bullet := "●"
	if sp.Markers == MarkerSquare || sp.Corners == CornersSquare {
		bullet = "■"
	}
	return &theme{
		ThemeInfo: ThemeInfo{
			ID: ThemeCustom, Name: sp.Name, Emoji: "✨", Photos: true,
			Description: "Maxsus dizayn",
		},
		pal:          pal,
		headFont:     sp.HeadingFont,
		bodyFont:     sp.BodyFont,
		headBold:     true,
		badgeOutline: sp.Markers == MarkerOutline,
		badgeSquare:  sp.Markers == MarkerSquare,
		cards:        cards,
		bulletChar:   bullet,
		dark:         dark,
		signature:    sp.Signature,
		transition:   fadeTransition,
		photos:       true,
		photoStyle:   sp.PhotoStyle,
		photoLeft:    sp.PhotoSide == PhotoLeft,
		photoFrame:   frame,
		custom:       &customLook{spec: sp, colors: colors},
		cover:        customCover,
		closing:      customClosing,
		chrome:       customChrome,
	}
}

// customLook — maxsus dizayn chizish uchun qo'shimcha ma'lumotlar (theme.custom).
type customLook struct {
	spec   CustomSpec
	colors customColors
}

// deckTheme taqdimot uchun mavzuni tanlaydi: maxsus dizayn yoki ID bo'yicha.
func deckTheme(d *Deck) *theme {
	if d.Custom != nil || d.Theme == ThemeCustom {
		return customTheme(d.Custom)
	}
	return themeByID(d.Theme)
}

// ---------------------------------------------------------------------------
// Design — tayyor yoki maxsus dizayn uchun umumiy interfeys
// ---------------------------------------------------------------------------

// Design — taqdimot dizayni: tayyor mavzu yoki AI maxsus dizayni. Bot rasm
// promptlari, rasm shakli va AI uchun tuzilishni shu orqali oladi.
type Design struct{ t *theme }

// DesignFor dizaynni qaytaradi: custom berilgan bo'lsa (yoki themeID —
// ThemeCustom) — maxsus dizayn, aks holda ID bo'yicha tayyor mavzu
// (noma'lum ID — standart, rasmsiz dizayn).
func DesignFor(themeID string, custom *CustomSpec) Design {
	if custom != nil || themeID == ThemeCustom {
		return Design{customTheme(custom)}
	}
	t := themeByID(themeID)
	if t.ID != themeID {
		return Design{themeByID(DefaultTheme)}
	}
	return Design{t}
}

// Photos dizayn rasmli ekanini bildiradi.
func (d Design) Photos() bool { return d.t.photos }

// Custom — AI maxsus dizayni ekanini bildiradi.
func (d Design) Custom() bool { return d.t.custom != nil }

// Name dizayn nomi.
func (d Design) Name() string { return d.t.Name }

// Structure dizayn tuzilishi (AI promptini moslash uchun).
func (d Design) Structure() Structure {
	layouts := append([]string(nil), design.CoreLayouts...)
	for _, l := range d.t.signature {
		if !slices.Contains(layouts, l) {
			layouts = append(layouts, l)
		}
	}
	if d.Custom() {
		// Maxsus dizayn barcha maketlarni chiza oladi — AI mazmunga eng
		// mosini tanlaydi, imzo maketlari esa ustuvor.
		for _, l := range design.SignatureLayouts {
			if !slices.Contains(layouts, l) {
				layouts = append(layouts, l)
			}
		}
	}
	return Structure{Layouts: layouts, Signature: slices.Clone(d.t.signature), Compact: d.t.compact}
}

// PhotoPrompt AI yozgan rasm tavsifiga dizayn uslubini va umumiy talablarni qo'shadi.
func (d Design) PhotoPrompt(desc string) string {
	desc = strings.TrimRight(strings.TrimSpace(desc), ".")
	var b strings.Builder
	b.WriteString(desc)
	b.WriteString(". ")
	if d.t.photoStyle != "" {
		b.WriteString("Style: " + strings.TrimRight(d.t.photoStyle, ".") + ". ")
	}
	b.WriteString(photoSuffix)
	return b.String()
}

// PhotoWide slayd rasmi yotiq (tepadagi banner) bo'lishini bildiradi;
// aks holda tik rasm (yon panel).
func (d Design) PhotoWide(s Slide) bool {
	return d.t.photos && d.t.photoAsBanner(normalizeSlide(s))
}

// PhotoWideLayout — PhotoWide ning faqat maketga qarab taxmini (slayd matni
// hali yozilmagan, masalan reja bosqichida): banner rasmli dizaynda — doim
// yotiq, aks holda yon panelga sig'adigan maketlarda tik, qolganlarida yotiq.
// Rasmsiz dizaynda — false.
func (d Design) PhotoWideLayout(layout string) bool {
	return d.t.photos && (d.t.photoBanner || !layoutFitsPhoto(layout))
}

// PhotoSlides n ta rasm qo'yiladigan slaydlar indekslarini (o'sish tartibida)
// tanlaydi. Avval rasm yon panelga sig'adigan slaydlar teng oraliqda
// olinadi, yetmasa — qolganlari (tepadagi banner bilan). Rasmsiz dizaynda — nil.
func (d Design) PhotoSlides(slides []Slide, n int) []int {
	fits := make([]bool, len(slides))
	for i, s := range slides {
		fits[i] = photoFits(normalizeSlide(s))
	}
	return d.pickPhotoSlides(fits, n)
}

// PhotoSlidesByLayout — PhotoSlides ning faqat maketlarga qarab tanlovi
// (slaydlar matni hali yozilmagan, masalan reja bosqichida): xuddi shunday
// yon panelga sig'adigan maketlar ustuvor va teng oraliqda.
func (d Design) PhotoSlidesByLayout(layouts []string, n int) []int {
	fits := make([]bool, len(layouts))
	for i, l := range layouts {
		fits[i] = layoutFitsPhoto(l)
	}
	return d.pickPhotoSlides(fits, n)
}

// pickPhotoSlides n ta rasmli slaydni tanlaydi: fits[i] — i-slayd yonida
// rasm uchun joy bor (ustuvor), qolganlari faqat ular yetmasa olinadi.
func (d Design) pickPhotoSlides(fits []bool, n int) []int {
	if !d.t.photos || n <= 0 || len(fits) == 0 {
		return nil
	}
	var side, wide []int
	for i, ok := range fits {
		if ok {
			side = append(side, i)
		} else {
			wide = append(wide, i)
		}
	}
	picked := spread(side, n)
	if left := n - len(picked); left > 0 {
		picked = append(picked, spread(wide, left)...)
	}
	slices.Sort(picked)
	return picked
}

// spread ro'yxatdan k ta elementni teng oraliqda tanlaydi (k ≥ len — barchasi).
func spread(list []int, k int) []int {
	if k >= len(list) {
		return slices.Clone(list)
	}
	out := make([]int, 0, k)
	step := float64(len(list)) / float64(k)
	for i := range k {
		out = append(out, list[int(float64(i)*step+step/2)])
	}
	return out
}

// ---------------------------------------------------------------------------
// Bezak (motiv)
// ---------------------------------------------------------------------------

// customMotif fon motivini chizadi. bold — titul va yakuniy slayd (yirikroq,
// yorqinroq); aks holda mazmunli slayd burchaklarida juda yengil bezak.
func (t *theme) customMotif(c *canvas, area rect, color string, bold bool) {
	a := 6 // mazmunli slaydda shaffof emaslik, %
	if bold {
		a = 14
	}
	r, b := area.right(), area.bottom()
	switch t.custom.spec.Motif {
	case MotifCircles:
		if bold {
			c.add(rect{r - 4.6, area.Y - 2.0, 6.2, 6.2}, shape{geom: "ellipse", fill: solidA(color, a)})
			c.add(rect{area.X - 1.4, b - 2.2, 3.4, 3.4}, shape{geom: "ellipse", fill: solidA(color, a-4)})
			ring(c, r-1.2, b-1.3, 0.75, color, 1.25, a*3, "")
		} else {
			c.add(rect{r - 1.3, area.Y - 1.3, 2.4, 2.4}, shape{geom: "ellipse", fill: solidA(color, a)})
			c.add(rect{area.X - 0.9, b - 0.9, 1.7, 1.7}, shape{geom: "ellipse", fill: solidA(color, a+2)})
		}
	case MotifDiagonal:
		if bold {
			c.add(rect{r - 4.2, area.Y - 1.2, 1.9, area.H + 2.4}, shape{fill: solidA(color, a), rot: 20})
			c.add(rect{r - 1.9, area.Y - 1.2, 0.75, area.H + 2.4}, shape{fill: solidA(color, a+8), rot: 20})
			c.add(rect{r - 0.7, area.Y - 1.2, 0.3, area.H + 2.4}, shape{fill: solidA(color, a+4), rot: 20})
		} else {
			c.add(rect{r - 1.25, area.Y - 0.6, 0.5, 2.0}, shape{fill: solidA(color, a+4), rot: 30})
			c.add(rect{r - 0.55, area.Y - 0.6, 0.2, 2.0}, shape{fill: solidA(color, a+8), rot: 30})
		}
	case MotifDots:
		cols, rows, step, d := 4, 3, 0.26, 0.07
		x0, y0 := r-0.45-float64(cols-1)*step, area.Y+0.12
		if bold {
			// Titulda — pastki o'ng burchakda (tepada universitet nomi, o'rtada mavzu).
			cols, rows, step, d = 7, 5, 0.34, 0.09
			x0, y0 = r-0.6-float64(cols-1)*step, b-0.55-float64(rows-1)*step
		}
		for i := range cols {
			for j := range rows {
				c.add(rect{x0 + float64(i)*step, y0 + float64(j)*step, d, d}, shape{geom: "ellipse", fill: solidA(color, a*3)})
			}
		}
	case MotifWaves:
		if bold {
			for i, k := range []float64{0, 0.55, 1.1} {
				c.add(rect{area.X - 2.5 + k*2, b - 1.6 + k*0.5, area.W*0.75 + 2.5, 3.2}, shape{geom: "ellipse", fill: solidA(color, a-2*i)})
			}
		} else {
			c.add(rect{r - 2.6, area.Y - 1.5, 3.4, 2.2}, shape{geom: "ellipse", fill: solidA(color, a)})
			c.add(rect{r - 1.7, area.Y - 1.3, 2.6, 1.7}, shape{geom: "ellipse", fill: solidA(color, a+2)})
		}
	}
}

// customFallback — rasm bo'lmaganda (provayder xatosi) rasm o'rnidagi bezakli panel.
func (t *theme) customFallback(c *canvas, r rect, d *Deck) {
	p := t.pal
	panel := t.custom.colors.panel
	c.add(r, shape{fill: linear(45, panel, mixHex(panel, p.secondary, 0.45))})
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	rad := min(r.W, r.H) * 0.3
	glowDot(c, cx, cy, rad*1.5, p.accent, 22)
	ring(c, cx, cy, rad, "FFFFFF", 1.25, 30, "")
	ring(c, cx, cy, rad*0.68, p.accent, 2, 75, "")
	c.text(rect{r.X + 0.3, cy - 0.55, r.W - 0.6, 1.1}, line1(d.Year, 44, "FFFFFF", t.headFont, "ctr"))
}

// ---------------------------------------------------------------------------
// Titul slayd
// ---------------------------------------------------------------------------

func customCover(t *theme, c *canvas, d *Deck) {
	c.darkMap = t.dark
	switch t.custom.spec.Cover {
	case CoverFull:
		customCoverFull(t, c, d)
	case CoverFrame:
		customCoverFrame(t, c, d)
	default:
		customCoverSplit(t, c, d)
	}
}

// coverText — titul slayd matnlari (universitet, ish turi, mavzu, talaba) x..x+w oralig'ida.
func (t *theme) coverText(c *canvas, d *Deck, x, w float64, onPhoto bool) {
	p := t.pal
	title, sub, label, value, uni := p.text, p.muted, t.seriesColor(0), p.text, p.muted
	chipFill, chipText := t.seriesColor(0), p.onSeries
	// Rasm ustida shaffof matn o'rniga oldindan aralashtirilgan rang: iPhone,
	// Quick Look va LibreOffice matn shaffofligini to'g'ri ko'rsatmaydi.
	if onPhoto {
		soft := mixHex("FFFFFF", t.custom.colors.shade, 0.15)
		title, sub, label, value, uni = "FFFFFF", soft, "FFFFFF", "FFFFFF", soft
		chipFill, chipText = p.accent, "FFFFFF"
		if contrast(p.accent, "FFFFFF") < 3 {
			chipText = "1A1A1A"
		}
	}
	c.text(rect{x, 0.5, w, 0.7}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 12, bold: true, caps: true, spc: 1.5, color: uni, font: t.bodyFont}}}}})
	t.chip(c, x, 2.0, d.WorkType, chipFill, chipText)
	c.text(rect{x, 2.6, w, 2.95}, t.titleBlock(d, 38, title, 17, sub, 0, "l"))
	c.add(rect{x + 0.05, 5.72, 0.9, 0.06}, shape{fill: solid(p.accent)})
	c.text(rect{x, 5.88, w, 0.95}, textBody{anchor: "t", fit: true, paras: t.people(d, 14.5, label, value, 0, "l")})
	c.text(rect{x, 6.85, w, 0.4}, line1(d.Year, 12, uni, t.bodyFont, "l"))
}

func customCoverSplit(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	const pw = 6.2
	photo := rect{slideW - pw, 0, pw, slideH}
	bar := rect{photo.X - 0.08, 0, 0.08, slideH}
	text := rect{0, 0, slideW - pw, slideH}
	if t.photoLeft {
		photo.X, bar.X, text.X = 0, pw, pw
	}
	t.customMotif(c, text, p.primary, true)
	if !coverPhoto(c, photo, d.CoverImage) {
		t.customFallback(c, photo, d)
	}
	c.add(bar, shape{fill: solid(p.accent)})
	t.coverText(c, d, text.X+0.8, text.W-1.45, false)
}

func customCoverFull(t *theme, c *canvas, d *Deck) {
	p := t.pal
	cc := t.custom.colors
	c.bg = linear(35, cc.shade, cc.panel)
	if coverPhoto(c, fullSlide, d.CoverImage) {
		// Matn tomoni (chap) yorug' rasm ustida ham oq yozuv o'qiladigan
		// darajada to'q; o'ng tomonda rasm ochiq ko'rinadi.
		c.add(fullSlide, shape{fill: solidA(cc.shade, 20)})
		fadeOverlay(c, rect{0, 0, 10.2, slideH}, cc.shade, 92, 0, false)
		fadeOverlay(c, rect{0, 0, 7.4, slideH}, cc.shade, 65, 0, false)
		fadeOverlay(c, rect{0, 0, slideW, 1.5}, cc.shade, 60, 0, true)
	} else {
		t.customMotif(c, fullSlide, "FFFFFF", true)
	}
	c.add(rect{0, slideH - 0.12, slideW, 0.12}, shape{fill: linear(0, p.accent, p.secondary)})
	t.coverText(c, d, 0.9, 6.6, true)
}

func customCoverFrame(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	t.customMotif(c, fullSlide, p.primary, true)
	frame := rect{7.6, 0.85, 4.9, 5.8}
	textX := 0.85
	if t.photoLeft {
		frame.X, textX = 0.85, 6.55
	}
	rad := t.cards.radius * 2.2
	back := rect{frame.X + 0.28, frame.Y + 0.28, frame.W, frame.H}
	backShape := shape{fill: solidA(p.accent, 85)}
	if rad > 0 {
		backShape.geom, backShape.adj = "roundRect", roundAdj(back, rad)
	}
	c.add(back, backShape)
	if d.CoverImage != nil {
		c.pic(frame, d.CoverImage, picFrame{radius: rad, shadow: &shadow{blur: 24, dist: 5, dir: 90, color: p.shadow, alpha: 28}})
	} else {
		t.customFallback(c, frame, d)
	}
	t.coverText(c, d, textX, 5.95, false)
}

// ---------------------------------------------------------------------------
// Yakuniy slayd
// ---------------------------------------------------------------------------

func customClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	cc := t.custom.colors
	c.darkMap = t.dark
	switch t.custom.spec.Cover {
	case CoverFull:
		c.bg = linear(35, cc.shade, cc.panel)
		if coverPhoto(c, fullSlide, d.CoverImage) {
			c.add(fullSlide, shape{fill: solidA(cc.shade, 82)})
		} else {
			t.customMotif(c, fullSlide, "FFFFFF", true)
		}
		c.add(rect{0, slideH - 0.12, slideW, 0.12}, shape{fill: linear(0, p.accent, p.secondary)})
		t.thanksBlock(c, d, "FFFFFF", "E8ECF2", "FFFFFF", "FFFFFF", solid(p.accent))
	case CoverFrame:
		c.bg = solid(p.bg)
		t.customMotif(c, fullSlide, p.primary, true)
		frame := rect{0.85, 1.1, 4.4, 5.3}
		textX := 5.95
		if t.photoLeft {
			frame.X, textX = slideW-0.85-4.4, 0.85
		}
		rad := t.cards.radius * 2.2
		if d.CoverImage != nil {
			c.pic(frame, d.CoverImage, picFrame{radius: rad, shadow: &shadow{blur: 24, dist: 5, dir: 90, color: p.shadow, alpha: 28}})
		} else {
			t.customFallback(c, frame, d)
		}
		t.closingText(c, d, textX, 6.5)
	default: // split — rasm titulga nisbatan qarama-qarshi tomonda
		c.bg = solid(p.bg)
		const pw = 5.4
		photo := rect{0, 0, pw, slideH}
		bar := rect{pw, 0, 0.08, slideH}
		text := rect{pw, 0, slideW - pw, slideH}
		if t.photoLeft {
			photo.X, bar.X, text.X = slideW-pw, slideW-pw-0.08, 0
		}
		t.customMotif(c, text, p.primary, true)
		if !coverPhoto(c, photo, d.CoverImage) {
			t.customFallback(c, photo, d)
		}
		c.add(bar, shape{fill: solid(p.accent)})
		t.closingText(c, d, text.X+0.85, text.W-1.6)
	}
}

// closingText — chapga tekislangan "Rahmat" bloki (split va frame variantlari).
func (t *theme) closingText(c *canvas, d *Deck, x, w float64) {
	p := t.pal
	c.add(rect{x + 0.05, 1.75, 0.9, 0.06}, shape{fill: solid(p.accent)})
	c.text(rect{x, 1.95, w, 1.8}, textBody{anchor: "b", fit: true, paras: []para{{runs: []run{
		{text: d.labels().Thanks, size: 44, bold: true, color: p.text, font: t.headFont}}}}})
	c.text(rect{x, 3.95, w, 1.25}, textBody{fit: true, paras: []para{{runs: []run{t.tx(d.Topic, 18, p.muted)}}}})
	if d.StudentName != "" {
		c.text(rect{x, 5.45, w, 0.55}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
			{text: d.labels().Student + ": ", size: 16, bold: true, color: t.seriesColor(0), font: t.bodyFont},
			t.tx(d.StudentName, 16, p.text)}}}})
	}
}

// ---------------------------------------------------------------------------
// Mazmunli slayd ramkasi
// ---------------------------------------------------------------------------

func customChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	cc := t.custom.colors
	c.darkMap = t.dark
	c.bg = solid(p.bg)
	if s.banner && s.photo != nil {
		return t.bannerBody(c, s, t.photoHeader(c, s, cc.shade, solid(p.accent)))
	}
	area := rect{0, 0, slideW, slideH}

	var y float64
	switch t.custom.spec.Header {
	case HeaderBand:
		band := rect{0, 0, slideW, 1.3}
		c.add(band, shape{fill: linear(0, cc.panel, mixHex(cc.panel, p.secondary, 0.35))})
		if t.custom.spec.Motif != MotifDots { // nuqtalar sahifa raqamiga tegmasin
			t.customMotif(c, band, "FFFFFF", false)
		}
		c.add(rect{0, band.bottom(), slideW, 0.06}, shape{fill: solid(p.accent)})
		c.text(rect{0.75, 0.17, 10.4, 0.96}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 27, "FFFFFF")}}}})
		c.text(rect{11.3, 0.4, 1.35, 0.5}, textBody{anchor: "ctr", paras: []para{{align: "r", runs: []run{
			{text: pageLabel(s), size: 11, bold: true, color: mixHex("FFFFFF", cc.panel, 0.15), font: t.bodyFont}}}}})
		y = 1.6
		if s.lead != "" {
			c.text(rect{0.75, 1.5, 11.85, 0.64}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 15, p.muted)}}}})
			y = 2.25
		}
	case HeaderUnderline:
		t.customMotif(c, area, p.primary, false)
		c.text(rect{0.75, 0.32, 10.5, 0.98}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 30, p.primary)}}}})
		c.add(rect{0.8, 1.3, 1.1, 0.07}, shape{geom: "roundRect", adj: []int{50000}, fill: solid(p.accent)})
		c.text(rect{11.25, 0.42, 1.4, 0.6}, textBody{anchor: "ctr", paras: []para{{align: "r", runs: []run{
			{text: pad2(s.num), size: 22, bold: true, color: p.number, font: t.headFont},
			{text: " / " + pad2(s.total), size: 11, color: p.muted, font: t.bodyFont}}}}})
		y = 1.6
		if s.lead != "" {
			c.text(rect{0.75, 1.45, 11.85, 0.64}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 15, p.muted)}}}})
			y = 2.2
		}
	default: // HeaderBar
		t.customMotif(c, area, p.primary, false)
		c.add(rect{0, 0, slideW, 0.07}, shape{fill: linear(0, p.primary, p.accent)})
		barShape := shape{fill: linear(90, p.primary, p.accent)}
		if t.cards.radius > 0 {
			barShape.geom, barShape.adj = "roundRect", []int{50000}
		}
		c.add(rect{0.6, 0.5, 0.09, 0.62}, barShape)
		c.text(rect{0.8, 0.33, 10.4, 0.95}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 28, p.text)}}}})
		pill := rect{11.42, 0.58, 1.23, 0.44}
		pillShape := shape{fill: solid(t.seriesColor(0)), text: &textBody{
			anchor: "ctr", inset: [4]float64{0.02, 0.01, 0.02, 0.01},
			paras: []para{{align: "ctr", runs: []run{{text: pageLabel(s), size: 11, bold: true, color: p.onSeries, font: t.bodyFont}}}}}}
		if t.cards.radius > 0 {
			pillShape.geom, pillShape.adj = "roundRect", []int{50000}
		}
		c.add(pill, pillShape)
		y = 1.5
		if s.lead != "" {
			c.text(rect{0.8, 1.27, 11.85, 0.64}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 15, p.muted)}}}})
			y = 2.08
		}
	}

	c.add(rect{0.75, 6.98, 11.83, 0.012}, shape{fill: solid(p.line)})
	t.footer(c, s, 0.75, 12.58, p.muted)
	return t.photoPanel(c, rect{0.75, y, 11.83, 6.8 - y}, s.photo)
}
