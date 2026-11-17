package generator

// themes_photo.go — rasmli dizaynlar: titul va yakuniy slaydda katta
// fotosurat (Deck.CoverImage), har bir mazmunli slaydda rasm (Slide.Image):
// mos maketda — yon panelda, keng maketda (jarayon, vaqt chizig'i, taqqoslash
// va h.k.) — slayd tepasida sarlavha orqasidagi bannerda. Rasm bo'lmasa
// (provayder ulanmagan yoki xato bergan), uning o'rnida dizaynning bezakli
// foni chiziladi — taqdimot baribir chiroyli.

import "math"

const fadeTransition = `<p:transition spd="med"><p:fade/></p:transition>`

// coverPhoto rasmni r maydonga joylaydi; rasm yo'q bo'lsa false qaytaradi.
func coverPhoto(c *canvas, r rect, p *Picture) bool {
	if p == nil {
		return false
	}
	c.pic(r, p, picFrame{})
	return true
}

// fullSlide — butun slayd maydoni.
var fullSlide = rect{0, 0, slideW, slideH}

// fadeOverlay rasm ustiga bir tomonga quyuqlashuvchi (yoki ochilib boruvchi)
// qoplama chizadi: shaffof emaslik from% dan to% gacha (vertical — yuqoridan
// pastga, aks holda chapdan o'ngga) o'zgaradi; from > to ham bo'lishi mumkin.
//
// Gradient ichidagi shaffoflikni ba'zi ko'ruvchilar (iPhone va macOS Quick
// Look) qo'llab-quvvatlamaydi — qoplama to'liq yopiq chiqib, rasmni berkitadi.
// Shuning uchun qoplama bir xil shaffoflikdagi qatlamlardan yig'iladi: har bir
// keyingi qatlam oldingisining ustiga tushib, umumiy to'qlikni bosqichma-bosqich
// oshiradi (qatlamlar chegarasida bo'shliq yoki chiziq paydo bo'lmaydi).
// Qatlamlar to'qlik eng yuqori bo'lgan chetda ustma-ust tushadi: from < to
// bo'lsa — oxirgi chetda, from > to bo'lsa — boshlang'ich chetda.
func fadeOverlay(c *canvas, r rect, color string, from, to int, vertical bool) {
	const steps = 28
	// Qatlamlar och chetdan to'q chetga qarab qisqaradi; from > to bo'lsa
	// yo'nalish teskari: to'q chet — boshlang'ich (chap yoki yuqori).
	reverse := from > to
	if reverse {
		from, to = to, from
	}
	prev := 0.0 // shu nuqtagacha yig'ilgan shaffof emaslik (0..1)
	for i := range steps {
		want := (float64(from) + float64(to-from)*float64(i)/float64(steps-1)) / 100
		if want <= prev {
			continue
		}
		// Ustma-ust qatlamlar: 1-(1-prev)(1-a) = want.
		a := int(math.Round((1 - (1-want)/(1-prev)) * 100))
		if a <= 0 {
			continue
		}
		prev = 1 - (1-prev)*(1-float64(a)/100)
		layer := r
		f := float64(i) / steps
		switch {
		case vertical && reverse:
			layer.H -= r.H * f
		case vertical:
			layer.Y += r.H * f
			layer.H -= r.H * f
		case reverse:
			layer.W -= r.W * f
		default:
			layer.X += r.W * f
			layer.W -= r.W * f
		}
		c.add(layer, shape{fill: solidA(color, min(a, 100))})
	}
}

// glowDot — yumshoq yorug' doira (radial gradient o'rniga, barcha
// ko'ruvchilarda bir xil chiqishi uchun bir necha shaffof doiradan).
func glowDot(c *canvas, cx, cy, r float64, color string, alpha int) {
	const layers = 7
	for i := range layers {
		k := 1 - float64(i)/layers
		c.add(rect{cx - r*k, cy - r*k, 2 * r * k, 2 * r * k}, shape{geom: "ellipse", fill: solidA(color, max(1, alpha/layers))})
	}
}

// photoPanel mazmun maydonining bir tomoniga rasm qo'yadi va matn uchun
// qolgan maydonni qaytaradi. p nil bo'lsa maydon o'zgarmaydi.
func (t *theme) photoPanel(c *canvas, body rect, p *Picture) rect {
	if p == nil {
		return body
	}
	const share, gap = 0.34, 0.35
	w := math.Round(body.W*share*100) / 100
	photo := rect{body.right() - w, body.Y, w, body.H}
	text := rect{body.X, body.Y, body.W - w - gap, body.H}
	if t.photoLeft {
		photo.X = body.X
		text.X = body.X + w + gap
	}
	c.pic(photo, p, t.photoFrame)
	return text
}

// bannerH — keng maketli slayddagi tepadagi rasm-banner balandligi.
const bannerH = 2.15

// Tepadagi banner geometriyasi: chapda — sarlavha uchun rangli maydon, o'ngda —
// ochiq rasm; rasmning chap cheti bannerPhotoFade kengligida maydon rangiga
// singib ketadi. Sarlavha bannerTextRight gacha — qoplama hali to'q joyda.
const (
	bannerPhotoX    = 6.6
	bannerPhotoFade = 2.0
	bannerTextRight = 7.45
)

// photoHeader keng maketli slaydda rasmni slayd tepasidagi bannerga qo'yadi
// (yon panel matnni siqib qo'ymasligi uchun): chapda shade rangidagi maydonda
// oq sarlavha va lead, o'ngda qoplamasiz ochiq rasm, ostida rangli chiziq
// (bar) va rasm ustida sahifa raqami. Mazmun maydoni boshlanadigan y ni
// qaytaradi.
func (t *theme) photoHeader(c *canvas, s slideInfo, shade string, bar paint) float64 {
	banner := rect{0, 0, slideW, bannerH}
	c.add(banner, shape{fill: solid(shade)})
	photo := rect{bannerPhotoX, 0, slideW - bannerPhotoX, bannerH}
	c.pic(photo, s.photo, picFrame{})
	fadeOverlay(c, rect{photo.X, 0, bannerPhotoFade, bannerH}, shade, 100, 0, false)
	c.add(rect{0, banner.bottom(), slideW, 0.07}, shape{fill: bar})
	c.add(rect{slideW - 1.4, 0.2, 1.05, 0.4}, shape{geom: "roundRect", adj: []int{50000}, fill: solidA(shade, 80), text: &textBody{
		anchor: "ctr", inset: [4]float64{0.02, 0.01, 0.02, 0.01},
		paras: []para{{align: "ctr", runs: []run{{text: pageLabel(s), size: 10.5, bold: true, color: "FFFFFF", font: t.bodyFont}}}}}})
	w := bannerTextRight - 0.75
	if s.lead != "" {
		c.text(rect{0.75, 0.2, w, 1.07}, textBody{anchor: "b", fit: true, minFit: 0.55, paras: []para{{runs: []run{t.hd(s.title, 26, "FFFFFF")}}}})
		c.text(rect{0.75, 1.3, w, 0.72}, textBody{fit: true, minFit: 0.65, paras: []para{{runs: []run{
			{text: s.lead, size: 13, italic: t.quoteItalic, color: "FFFFFF", alpha: 90, font: t.bodyFont}}}}})
	} else {
		c.text(rect{0.75, 0.25, w, 1.65}, textBody{anchor: "ctr", fit: true, minFit: 0.55, paras: []para{{runs: []run{t.hd(s.title, 28, "FFFFFF")}}}})
	}
	return banner.bottom() + 0.38
}

// bannerBody photoHeader li slayd uchun kolontitulni chizadi va mazmun
// maydonini qaytaradi.
func (t *theme) bannerBody(c *canvas, s slideInfo, y float64) rect {
	c.add(rect{0.75, 6.98, 11.83, 0.012}, shape{fill: solid(t.pal.line)})
	t.footer(c, s, 0.75, 12.58, t.pal.muted)
	return rect{0.75, y, 11.83, 6.8 - y}
}

// chip — yumaloq "yorliq" (masalan, ish turi).
func (t *theme) chip(c *canvas, x, y float64, text, fill, color string) {
	r := run{text: text, size: 12, bold: true, spc: 3, color: color, font: t.bodyFont}
	w := min(7.0, textWidth(r)+0.6)
	c.add(rect{x, y, w, 0.46}, shape{geom: "roundRect", adj: []int{50000}, fill: solid(fill), text: &textBody{
		anchor: "ctr", inset: [4]float64{0.05, 0.01, 0.05, 0.01}, paras: []para{{align: "ctr", runs: []run{r}}}}})
}

// thanksBlock — yakuniy slayd markazidagi matnlar (rahmat, chiziq, mavzu, talaba).
func (t *theme) thanksBlock(c *canvas, d *Deck, title, topic, label, name string, bar paint) {
	c.text(rect{1.0, 1.75, 11.33, 1.55}, textBody{anchor: "b", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.labels().Thanks, size: 44, bold: true, color: title, font: t.headFont}}}}})
	c.add(rect{slideW/2 - 0.5, 3.52, 1.0, 0.06}, shape{fill: bar})
	c.text(rect{1.6, 3.78, 10.13, 1.15}, textBody{fit: true, paras: []para{{align: "ctr", runs: []run{t.tx(d.Topic, 18, topic)}}}})
	if d.StudentName != "" {
		c.text(rect{1.6, 5.2, 10.13, 0.55}, textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
			{text: d.labels().Student + ": ", size: 16, bold: true, color: label, font: t.bodyFont},
			t.tx(d.StudentName, 16, name)}}}})
	}
}

// footer — pastki kolontitul: chapda mavzu, o'ngda universitet nomi.
func (t *theme) footer(c *canvas, s slideInfo, x, right float64, color string) {
	w := right - x
	c.text(rect{x, 7.02, w * 0.6, 0.32}, line1(s.topic, 10, color, t.bodyFont, "l"))
	c.text(rect{x + w*0.6 + 0.1, 7.02, w*0.4 - 0.1, 0.32}, line1(s.university, 10, color, t.bodyFont, "r"))
}

// ===========================================================================
// 5. JURNAL — oq fon, qora serif sarlavhalar, qizil aksent, katta fotosurat
// ===========================================================================

var journalTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeJournal, Name: "Jurnal", Emoji: "📰", Photos: true,
		Description: "jurnal uslubi",
	},
	pal: palette{
		bg: "FFFFFF", surface: "FFFFFF", surface2: "F5F2EC",
		primary: "141414", secondary: "3D3D3D", accent: "D7263D",
		text: "141414", muted: "5C5C5C", line: "E4E0D8", number: "D7263D",
		series:   []string{"D7263D", "141414", "D7263D", "141414"},
		onSeries: "FFFFFF", panel: solid("141414"), onPanel: "FFFFFF", shadow: "000000",
	},
	headFont: "Georgia", bodyFont: "Calibri", headBold: true, quoteItalic: true,
	cards:      cardStyle{border: true, topBar: true},
	bulletChar: "■",
	signature:  []string{LayoutStatement, LayoutTimeline},
	transition: fadeTransition,
	photos:     true,
	photoStyle: "editorial magazine photography, crisp details, natural neutral colors",
	photoFrame: picFrame{line: &stroke{color: "141414", width: 0.75}},
	cover:      journalCover,
	closing:    journalClosing,
	chrome:     journalChrome,
}

// journalPhotoFallback — rasm bo'lmaganda foto o'rnidagi to'q panel.
func journalPhotoFallback(t *theme, c *canvas, r rect, d *Deck) {
	c.add(r, shape{fill: linear(90, "2A2A2A", "141414")})
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	ring(c, cx, cy, 2.3, "FFFFFF", 1, 14, "")
	ring(c, cx, cy, 1.6, t.pal.accent, 1.5, 70, "")
	c.text(rect{r.X + 0.3, cy - 0.6, r.W - 0.6, 1.2}, line1(d.Year, 54, "FFFFFF", t.headFont, "ctr"))
}

func journalCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid("FFFFFF")
	photo := rect{0, 0, 7.0, slideH}
	hasPhoto := coverPhoto(c, photo, d.CoverImage)
	if !hasPhoto {
		journalPhotoFallback(t, c, photo, d)
	}
	c.add(rect{photo.right(), 0, 0.09, slideH}, shape{fill: solid(p.accent)})

	x, w := 7.65, 5.1
	c.text(rect{x, 0.5, w, 0.75}, textBody{anchor: "b", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 12, bold: true, caps: true, spc: 1.5, color: p.muted, font: t.bodyFont}}}}})
	c.add(rect{x + 0.05, 1.35, w - 0.1, 0.025}, shape{fill: solid(p.text)})
	c.text(rect{x, 1.48, w, 0.42}, textBody{paras: []para{{runs: []run{
		{text: d.WorkType, size: 13, bold: true, caps: true, spc: 4, color: p.accent, font: t.bodyFont}}}}})
	c.text(rect{x, 1.98, w, 3.35}, t.titleBlock(d, 36, p.text, 16, p.muted, 0, "l"))
	c.add(rect{x + 0.05, 5.55, 0.8, 0.05}, shape{fill: solid(p.accent)})
	c.text(rect{x, 5.7, w, 0.95}, textBody{fit: true, paras: t.people(d, 14, p.accent, p.text, 0, "l")})
	if hasPhoto {
		c.text(rect{x, 6.75, w, 0.4}, line1(d.Year, 12, p.muted, t.bodyFont, "r"))
	}
}

func journalClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid("FFFFFF")
	photo := rect{slideW - 5.6, 0, 5.6, slideH}
	if !coverPhoto(c, photo, d.CoverImage) {
		journalPhotoFallback(t, c, photo, d)
	}
	c.add(rect{photo.X - 0.09, 0, 0.09, slideH}, shape{fill: solid(p.accent)})

	x, w := 0.9, photo.X-1.6
	c.add(rect{x + 0.05, 1.45, w, 0.025}, shape{fill: solid(p.text)})
	c.text(rect{x, 1.6, w, 2.2}, textBody{fit: true, paras: []para{{runs: []run{
		{text: d.labels().Thanks, size: 42, bold: true, color: p.text, font: t.headFont}}}}})
	c.add(rect{x + 0.05, 3.95, 0.8, 0.05}, shape{fill: solid(p.accent)})
	c.text(rect{x, 4.1, w, 1.2}, textBody{fit: true, paras: []para{{runs: []run{t.tx(d.Topic, 18, p.muted)}}}})
	if d.StudentName != "" {
		c.text(rect{x, 5.5, w, 0.55}, textBody{anchor: "ctr", paras: []para{{runs: []run{
			{text: d.labels().Student + ": ", size: 16, bold: true, color: p.accent, font: t.bodyFont},
			t.tx(d.StudentName, 16, p.text)}}}})
	}
}

func journalChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	if s.banner && s.photo != nil {
		return t.bannerBody(c, s, t.photoHeader(c, s, "141414", solid(p.accent)))
	}
	c.text(rect{0.75, 0.08, 9.0, 0.32}, line1(s.university, 9.5, p.muted, t.bodyFont, "l"))
	c.text(rect{10.0, 0.06, 2.58, 0.34}, textBody{anchor: "ctr", paras: []para{{align: "r", runs: []run{
		{text: pageLabel(s), size: 10.5, bold: true, spc: 1, color: p.accent, font: t.bodyFont}}}}})
	c.add(rect{0.75, 0.42, 11.83, 0.025}, shape{fill: solid(p.text)})
	c.text(rect{0.7, 0.5, 11.9, 0.9}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 28, p.text)}}}})

	y := 1.6
	if s.lead != "" {
		c.add(rect{0.8, 1.5, 0.05, 0.5}, shape{fill: solid(p.accent)})
		c.text(rect{0.95, 1.42, 11.63, 0.66}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{
			{text: s.lead, size: 15, italic: true, color: p.muted, font: t.bodyFont}}}}})
		y = 2.25
	}
	c.add(rect{0.75, 6.98, 11.83, 0.012}, shape{fill: solid(p.line)})
	c.text(rect{0.75, 7.02, 11.83, 0.32}, line1(s.topic, 10, p.muted, t.bodyFont, "l"))
	return t.photoPanel(c, rect{0.75, y, 11.83, 6.8 - y}, s.photo)
}

// ===========================================================================
// 6. TABIAT — krem fon, to'q yashil va terakota, yumshoq fotosuratlar
// ===========================================================================

var natureTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeNature, Name: "Tabiat", Emoji: "🍃", Photos: true,
		Description: "tabiiy ranglar",
	},
	pal: palette{
		bg: "F6F3EA", surface: "FFFDF8", surface2: "ECE5D3",
		primary: "2F5D50", secondary: "4E7D6C", accent: "C8553D",
		text: "22332D", muted: "5F6B63", line: "DED6C3", number: "C8553D",
		series:   []string{"2F5D50", "C8553D", "5E8C70", "B7832F"},
		onSeries: "FFFFFF", panel: linear(90, "2F5D50", "234A3F"), onPanel: "FFFFFF", shadow: "2F5D50",
	},
	headFont: "Trebuchet MS", bodyFont: "Calibri", headBold: true,
	cards:      cardStyle{radius: 0.2, shadow: true},
	bulletChar: "●",
	signature:  []string{LayoutCycle, LayoutPyramid},
	transition: fadeTransition,
	photos:     true,
	photoStyle: "soft natural daylight, warm earthy tones, calm atmosphere",
	photoFrame: picFrame{radius: 0.25, shadow: &shadow{blur: 20, dist: 4, dir: 90, color: "2F5D50", alpha: 25}},
	cover:      natureCover,
	closing:    natureClosing,
	chrome:     natureChrome,
}

// natureDecor — rasm bo'lmaganda to'q yashil fon ustidagi doiralar.
func natureDecor(t *theme, c *canvas) {
	c.add(rect{8.6, -1.9, 6.4, 6.4}, shape{geom: "ellipse", fill: solidA("FFFFFF", 6)})
	c.add(rect{10.3, 3.9, 4.2, 4.2}, shape{geom: "ellipse", fill: solidA(t.pal.accent, 45)})
	c.add(rect{-1.6, 5.3, 3.6, 3.6}, shape{geom: "ellipse", fill: solidA("FFFFFF", 5)})
	ring(c, 11.3, 2.0, 1.15, "FFFFFF", 1, 28, "")
}

func natureCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = linear(90, "2F5D50", "1E3B33")
	if coverPhoto(c, fullSlide, d.CoverImage) {
		fadeOverlay(c, fullSlide, "1E3B33", 20, 92, true)
		fadeOverlay(c, rect{0, 0, slideW, 1.6}, "1E3B33", 50, 0, true) // tepadagi yozuvlar uchun
	} else {
		natureDecor(t, c)
	}
	c.text(rect{0.9, 0.5, 9.0, 0.7}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 13, bold: true, caps: true, spc: 1.5, color: "FFFFFF", alpha: 85, font: t.bodyFont}}}}})
	c.text(rect{10.4, 0.6, 2.43, 0.5}, line1(d.Year, 14, "FFFFFF", t.bodyFont, "r"))

	t.chip(c, 0.9, 2.55, d.WorkType, p.accent, "FFFFFF")
	c.text(rect{0.9, 3.15, 9.8, 2.55}, t.titleBlock(d, 40, "FFFFFF", 17, "FFFFFF", 85, "l"))
	c.add(rect{1.0, 5.9, 0.07, 0.85}, shape{geom: "roundRect", adj: []int{50000}, fill: solid(p.accent)})
	c.text(rect{1.2, 5.85, 8.0, 0.95}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, "FFFFFF", "FFFFFF", 75, "l")})
}

func natureClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = linear(90, "2F5D50", "1E3B33")
	if coverPhoto(c, fullSlide, d.CoverImage) {
		c.add(fullSlide, shape{fill: solidA("1E3B33", 80)})
	} else {
		natureDecor(t, c)
	}
	t.thanksBlock(c, d, "FFFFFF", "E9E4D6", "F2B8A8", "FFFFFF", solid(p.accent))
}

func natureChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	if s.banner && s.photo != nil {
		return t.bannerBody(c, s, t.photoHeader(c, s, "1E3B33", solid(p.accent)))
	}
	c.add(rect{-0.9, -0.9, 2.0, 2.0}, shape{geom: "ellipse", fill: solidA(p.primary, 8)})
	c.add(rect{12.3, 6.5, 1.6, 1.6}, shape{geom: "ellipse", fill: solidA(p.accent, 12)})

	c.text(rect{0.75, 0.35, 10.9, 0.95}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 28, p.primary)}}}})
	c.add(rect{0.82, 1.27, 0.75, 0.07}, shape{geom: "roundRect", adj: []int{50000}, fill: solid(p.accent)})
	c.add(rect{11.97, 0.47, 0.66, 0.66}, shape{geom: "ellipse", fill: solid(p.primary), text: &textBody{
		anchor: "ctr", inset: [4]float64{0.01, 0.01, 0.01, 0.01},
		paras: []para{{align: "ctr", runs: []run{{text: pad2(s.num), size: 15, bold: true, color: "FFFFFF", font: t.headFont}}}}}})

	y := 1.6
	if s.lead != "" {
		c.text(rect{0.75, 1.42, 11.9, 0.64}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 15, p.muted)}}}})
		y = 2.2
	}
	t.footer(c, s, 0.75, 11.9, p.muted)
	return t.photoPanel(c, rect{0.75, y, 11.9, 6.8 - y}, s.photo)
}

// ===========================================================================
// 7. OKEAN — korporativ to'q ko'k va firuza, chetgacha yoyilgan fotosurat
// ===========================================================================

var oceanTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeOcean, Name: "Okean", Emoji: "🌊", Photos: true,
		Description: "korporativ ko'k",
	},
	pal: palette{
		bg: "F3F7FB", surface: "FFFFFF", surface2: "E2EDF5",
		primary: "0B3C5D", secondary: "1CA7A8", accent: "1CA7A8",
		text: "102A43", muted: "52667A", line: "D6E1EB", number: "1CA7A8",
		series:   []string{"0B3C5D", "1CA7A8", "2F80ED", "14919B"},
		onSeries: "FFFFFF", panel: linear(0, "0B3C5D", "145374"), onPanel: "FFFFFF", shadow: "0B3C5D",
	},
	headFont: "Segoe UI", bodyFont: "Segoe UI", headBold: true,
	cards:      cardStyle{shadow: true, topBar: true},
	bulletChar: "●",
	signature:  []string{LayoutMatrix, LayoutTimeline, LayoutHub},
	transition: fadeTransition,
	photos:     true,
	photoStyle: "clean professional photography, cool blue and teal tones, bright modern setting",
	cover:      oceanCover,
	closing:    oceanClosing,
	chrome:     oceanChrome,
}

// oceanDecor — rasm bo'lmaganda o'ng tomondagi qiya chiziqlar.
func oceanDecor(t *theme, c *canvas) {
	c.add(rect{8.3, -1.2, 2.3, 9.9}, shape{fill: solidA("FFFFFF", 5), rot: 18})
	c.add(rect{10.5, -1.2, 1.2, 9.9}, shape{fill: solidA(t.pal.secondary, 35), rot: 18})
	c.add(rect{12.0, -1.2, 0.5, 9.9}, shape{fill: solidA("FFFFFF", 10), rot: 18})
}

func oceanBottomBar(t *theme, c *canvas) {
	c.add(rect{0, slideH - 0.12, slideW, 0.12}, shape{fill: linear(0, t.pal.secondary, "2F80ED")})
}

func oceanCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = linear(0, "0B3C5D", "145374")
	if coverPhoto(c, fullSlide, d.CoverImage) {
		c.add(fullSlide, shape{fill: solidA("0B3C5D", 15)})
		fadeOverlay(c, rect{0, 0, 9.5, slideH}, "0B3C5D", 85, 0, false)
		// Matn ostini qo'shimcha to'qlashtirish — keskin chegarasiz; tepada
		// universitet nomi va yil yorug' rasm ustida ham o'qilsin.
		fadeOverlay(c, rect{0, 0, 6.0, slideH}, "0B3C5D", 40, 0, false)
		fadeOverlay(c, rect{0, 0, slideW, 1.6}, "0B3C5D", 55, 0, true)
	} else {
		oceanDecor(t, c)
	}
	oceanBottomBar(t, c)

	c.text(rect{0.9, 0.5, 7.6, 0.75}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 12, bold: true, caps: true, spc: 1.5, color: "FFFFFF", alpha: 80, font: t.bodyFont}}}}})
	c.text(rect{10.4, 0.6, 2.43, 0.5}, line1(d.Year, 14, "FFFFFF", t.bodyFont, "r"))
	c.add(rect{1.0, 1.95, 0.9, 0.06}, shape{fill: solid(p.secondary)})
	c.text(rect{0.9, 2.07, 7.2, 0.45}, textBody{paras: []para{{runs: []run{
		{text: d.WorkType, size: 15, bold: true, spc: 4, color: p.secondary, font: t.bodyFont}}}}})
	c.text(rect{0.9, 2.6, 7.6, 3.0}, t.titleBlock(d, 40, "FFFFFF", 17, "FFFFFF", 80, "l"))
	c.text(rect{0.9, 5.8, 7.2, 1.0}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, p.secondary, "FFFFFF", 0, "l")})
}

func oceanClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = linear(0, "0B3C5D", "145374")
	if coverPhoto(c, fullSlide, d.CoverImage) {
		c.add(fullSlide, shape{fill: solidA("0B3C5D", 84)})
	} else {
		oceanDecor(t, c)
	}
	oceanBottomBar(t, c)
	t.thanksBlock(c, d, "FFFFFF", "D5E6F2", p.secondary, "FFFFFF", solid(p.secondary))
}

func oceanChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	if s.banner && s.photo != nil {
		return t.bannerBody(c, s, t.photoHeader(c, s, "0B3C5D", linear(0, p.secondary, "2F80ED")))
	}
	right := slideW - 0.75
	if s.photo != nil {
		// Rasm slaydning o'ng chetigacha yoyiladi.
		photo := rect{slideW - 4.3, 0, 4.3, slideH}
		right = photo.X - 0.45
		c.pic(photo, s.photo, picFrame{})
		c.add(rect{photo.X, 0, 0.07, slideH}, shape{fill: linear(90, p.secondary, "2F80ED")})
	}

	c.add(rect{0.75, 0.5, 0.08, 0.66}, shape{fill: linear(90, p.secondary, p.primary)})
	titleW := right - 0.95
	if s.photo == nil {
		titleW -= 1.3
		c.add(rect{11.55, 0.6, 1.03, 0.42}, shape{fill: solid(p.primary), text: &textBody{
			anchor: "ctr", inset: [4]float64{0.02, 0.01, 0.02, 0.01},
			paras: []para{{align: "ctr", runs: []run{{text: pageLabel(s), size: 11, bold: true, color: "FFFFFF", font: t.bodyFont}}}}}})
	}
	c.text(rect{0.95, 0.36, titleW, 0.95}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 28, p.primary)}}}})

	y := 1.5
	if s.lead != "" {
		c.text(rect{0.95, 1.32, right - 0.95, 0.64}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 15, p.muted)}}}})
		y = 2.1
	}

	c.add(rect{0.75, 6.98, right - 0.75, 0.012}, shape{fill: solid(p.line)})
	if s.photo == nil {
		t.footer(c, s, 0.75, right, p.muted)
	} else {
		c.text(rect{0.75, 7.02, right - 2.45, 0.32}, line1(s.topic, 10, p.muted, t.bodyFont, "l"))
		c.text(rect{right - 1.6, 7.02, 1.6, 0.32}, textBody{anchor: "ctr", paras: []para{{align: "r", runs: []run{
			{text: pageLabel(s), size: 10, bold: true, color: p.primary, font: t.bodyFont}}}}})
	}
	return rect{0.75, y, right - 0.75, 6.8 - y}
}

// ===========================================================================
// 8. SHOM — to'q fon, iliq shafaq ranglari, kinematik fotosuratlar
// ===========================================================================

var sunsetTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeSunset, Name: "Shom", Emoji: "🌅", Photos: true,
		Description: "iliq shafaq ranglari",
	},
	pal: palette{
		bg: "16151A", surface: "211F27", surface2: "29262F",
		primary: "FF7A45", secondary: "FFB347", accent: "FFB347",
		text: "F4EEE8", muted: "A9A09A", line: "35313C", number: "FF7A45",
		series:   []string{"FF7A45", "FFB347", "EF5D7A", "F2C14E"},
		onSeries: "16151A", panel: linear(35, "E8603C", "C2413F"), onPanel: "FFFFFF", shadow: "000000",
	},
	headFont: "Century Gothic", bodyFont: "Segoe UI", headBold: true,
	cards:      cardStyle{radius: 0.14, border: true},
	bulletChar: "●",
	dark:       true,
	neon:       true,
	signature:  []string{LayoutStatement, LayoutCycle},
	transition: fadeTransition,
	photos:     true,
	photoStyle: "cinematic photography, dramatic warm golden-hour light, deep shadows",
	photoLeft:  true,
	photoFrame: picFrame{radius: 0.18, line: &stroke{color: "FF7A45", width: 1.5, alpha: 85}},
	cover:      sunsetCover,
	closing:    sunsetClosing,
	chrome:     sunsetChrome,
}

// sunsetSun — rasm bo'lmaganda chiziladigan "botayotgan quyosh" bezagi.
func sunsetSun(t *theme, c *canvas, cx, cy, r float64) {
	p := t.pal
	glowDot(c, cx, cy, r*1.6, p.primary, 30)
	c.add(rect{cx - r, cy - r, 2 * r, 2 * r}, shape{geom: "ellipse", fill: linear(90, "FFB347", "FF7A45", "EF5D7A")})
	// Quyoshning pastki qismini kesib o'tuvchi fon rangidagi chiziqlar.
	for i := range 5 {
		h := 0.05 + float64(i)*0.035
		y := cy + r*0.1 + float64(i)*r*0.2
		c.add(rect{cx - r - 0.1, y, 2*r + 0.2, h}, shape{fill: solid(p.bg)})
	}
	c.add(rect{cx - r*1.4, cy + r + 0.05, r * 2.8, 0.02}, shape{fill: solidA(p.secondary, 60)})
}

func sunsetCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.darkMap = true
	c.bg = solid(p.bg)
	if coverPhoto(c, fullSlide, d.CoverImage) {
		c.add(fullSlide, shape{fill: solidA("0E0D11", 40)})
		fadeOverlay(c, fullSlide, p.bg, 0, 92, true)
	} else {
		sunsetSun(t, c, 10.6, 3.0, 2.0)
	}

	c.text(rect{0.9, 0.5, 8.5, 0.75}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 12, caps: true, spc: 2, color: "FFFFFF", alpha: 75, font: t.bodyFont}}}}})
	c.text(rect{10.4, 0.6, 2.43, 0.5}, line1(d.Year, 14, p.secondary, t.headFont, "r"))
	c.text(rect{0.9, 2.35, 7.2, 0.45}, textBody{paras: []para{{runs: []run{
		{text: d.WorkType, size: 14, bold: true, spc: 4, color: p.primary, font: t.bodyFont}}}}})
	c.text(rect{0.9, 2.85, 8.0, 2.75}, t.titleBlock(d, 40, "FFFFFF", 17, "E6DED7", 0, "l"))
	c.add(rect{1.0, 5.75, 2.2, 0.05}, shape{fill: linear(0, p.primary, p.secondary)})
	c.text(rect{0.9, 5.9, 7.4, 0.95}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, p.secondary, "FFFFFF", 0, "l")})
}

func sunsetClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.darkMap = true
	c.bg = solid(p.bg)
	if coverPhoto(c, fullSlide, d.CoverImage) {
		c.add(fullSlide, shape{fill: solidA("0E0D11", 74)})
	}
	glowDot(c, slideW/2, 3.2, 2.6, p.primary, 18)
	t.thanksBlock(c, d, "FFFFFF", "E6DED7", p.secondary, "FFFFFF", linear(0, p.primary, p.secondary))
}

func sunsetChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.darkMap = true
	c.bg = solid(p.bg)
	if s.banner && s.photo != nil {
		return t.bannerBody(c, s, t.photoHeader(c, s, "0E0D11", linear(0, p.primary, p.secondary)))
	}
	glowDot(c, 12.4, 0.1, 2.8, p.primary, 12)

	c.text(rect{0.75, 0.36, 10.3, 0.95}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 28, p.text)}}}})
	c.add(rect{0.8, 1.28, 1.1, 0.05}, shape{fill: linear(0, p.primary, p.secondary)})
	c.text(rect{11.15, 0.6, 1.5, 0.45}, textBody{anchor: "ctr", paras: []para{{align: "r", runs: []run{
		{text: pageLabel(s), size: 11, bold: true, spc: 1, color: p.primary, font: t.bodyFont}}}}})

	y := 1.6
	if s.lead != "" {
		c.text(rect{0.75, 1.42, 11.9, 0.64}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 14.5, p.muted)}}}})
		y = 2.2
	}
	c.add(rect{0.75, 6.98, 11.9, 0.01}, shape{fill: solid(p.line)})
	t.footer(c, s, 0.75, 12.65, p.muted)
	return t.photoPanel(c, rect{0.75, y, 11.9, 6.8 - y}, s.photo)
}

// ===========================================================================
// 16. PANORAMA — har bir slayd tepasida keng rasm-banner, sarlavha banner ustida
// ===========================================================================

var panoramaTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemePanorama, Name: "Panorama", Emoji: "🏞", Photos: true,
		Description: "tepada keng rasm-banner",
	},
	pal: palette{
		bg: "FFFFFF", surface: "F5F6F8", surface2: "ECEEF2",
		primary: "1F242C", secondary: "3A4250", accent: "F26B1D",
		text: "1F242C", muted: "5E6673", line: "E1E4E9", number: "F26B1D",
		series:   []string{"F26B1D", "1F242C", "E8954F", "4E5A6B"},
		onSeries: "FFFFFF", panel: solid("1F242C"), onPanel: "FFFFFF", shadow: "1F242C",
	},
	headFont: "Verdana", bodyFont: "Arial", headBold: true,
	cards:       cardStyle{radius: 0.05, leftBar: true},
	bulletChar:  "▸",
	compact:     true,
	signature:   []string{LayoutStatement, LayoutTimeline, LayoutMatrix},
	transition:  fadeTransition,
	photos:      true,
	photoBanner: true,
	photoStyle:  "wide cinematic panoramic photography, natural light, rich but realistic colors",
	cover:       panoramaCover,
	closing:     panoramaClosing,
	chrome:      panoramaChrome,
}

// panoramaSkyline — rasm bo'lmaganda banner o'ng tomonidagi "shahar
// silueti" bezagi (banner ichida qoladi).
func panoramaSkyline(t *theme, c *canvas, r rect) {
	hs := []float64{0.35, 0.6, 0.45, 0.85, 0.55, 0.7, 0.4, 0.9, 0.5, 0.65, 0.3}
	w := 0.36
	x := r.right() - float64(len(hs))*(w+0.08) - 0.3
	for i, h := range hs {
		a := 10 + (i%3)*5
		fill := solidA("FFFFFF", a)
		if i == 3 || i == 7 {
			fill = solidA(t.pal.accent, 70)
		}
		bh := r.H * h
		c.add(rect{x + float64(i)*(w+0.08), r.bottom() - bh, w, bh}, shape{fill: fill})
	}
}

func panoramaCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	photo := rect{0, 0, slideW, 4.55}
	if coverPhoto(c, photo, d.CoverImage) {
		fadeOverlay(c, photo, "11151B", 55, 5, true)
	} else {
		c.add(photo, shape{fill: solid(p.primary)})
		panoramaSkyline(t, c, photo)
	}
	c.add(rect{0, photo.bottom(), slideW, 0.09}, shape{fill: solid(p.accent)})
	c.text(rect{0.75, 0.45, 9.0, 0.7}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 12, bold: true, caps: true, spc: 1.5, color: "FFFFFF", font: t.bodyFont}}}}})
	c.text(rect{10.4, 0.5, 2.18, 0.6}, line1(d.Year, 16, "FFFFFF", t.headFont, "r"))

	t.chip(c, 0.75, 4.95, d.WorkType, p.accent, "FFFFFF")
	c.text(rect{0.7, 5.5, 8.6, 1.75}, t.titleBlock(d, 32, p.text, 15, p.muted, 0, "l"))
	c.add(rect{9.55, 5.0, 0.02, 2.0}, shape{fill: solid(p.line)})
	c.text(rect{9.8, 4.95, 3.0, 2.1}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 13, p.accent, p.text, 0, "l")})
}

func panoramaClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.primary)
	if coverPhoto(c, fullSlide, d.CoverImage) {
		c.add(fullSlide, shape{fill: solidA("11151B", 80)})
	} else {
		panoramaSkyline(t, c, rect{0, 4.6, slideW, 2.9})
	}
	t.thanksBlock(c, d, "FFFFFF", "D9DDE3", p.accent, "FFFFFF", solid(p.accent))
}

func panoramaChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	banner := rect{0, 0, slideW, 2.1}
	if s.photo != nil {
		c.pic(banner, s.photo, picFrame{})
		// Bir tekis yengil qoplama + chapdan o'ngga ochiluvchi qoplama: rasm
		// o'ngda yaxshi ko'rinadi, oq matn esa yorug' rasm ustida ham o'qiladi.
		c.add(banner, shape{fill: solidA("11151B", 25)})
		fadeOverlay(c, banner, "11151B", 80, 15, false)
	} else {
		c.add(banner, shape{fill: solid(p.primary)})
		panoramaSkyline(t, c, banner)
	}
	c.add(rect{0, banner.bottom(), slideW, 0.07}, shape{fill: solid(p.accent)})
	tag := rect{11.55, banner.bottom() - 0.24, 1.1, 0.48}
	c.add(tag, shape{fill: solid(p.accent), text: &textBody{anchor: "ctr", inset: [4]float64{0.02, 0.01, 0.02, 0.01},
		paras: []para{{align: "ctr", runs: []run{{text: pageLabel(s), size: 11, bold: true, color: "FFFFFF", font: t.bodyFont}}}}}})
	if s.lead != "" {
		c.text(rect{0.75, 0.25, 10.4, 1.05}, textBody{anchor: "b", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 28, "FFFFFF")}}}})
		c.text(rect{0.75, 1.3, 10.4, 0.62}, textBody{fit: true, minFit: 0.7, paras: []para{{runs: []run{
			{text: s.lead, size: 14, color: "FFFFFF", alpha: 88, font: t.bodyFont}}}}})
	} else {
		c.text(rect{0.75, 0.25, 10.4, 1.6}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 30, "FFFFFF")}}}})
	}
	t.footer(c, s, 0.75, 12.58, p.muted)
	top := banner.bottom() + 0.42
	return rect{0.75, top, 11.83, 6.85 - top}
}
