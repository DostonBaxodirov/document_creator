package generator

import "strings"

// themes_extra.go — tuzilishi boshqacha dizaynlar. Ular faqat rangi bilan
// emas, slayd tuzilishi bilan ham farqlanadi: sarlavha joyi (yon panel,
// plitka, markaz, chizma shtampi), kartochka turi (qattiq soyali, fonsiz
// chiziqli, punktir, burchak belgili, och rangli kapsula) va titul slayd
// kompozitsiyasi. Har birining o'z "imzo" maketlari bor (signature) — AI
// taqdimotni shu maketlar asosida tuzadi.

// fitHeight matnni r maydonga sig'diradi (shriftni kichraytiradi) va matn
// egallagan balandlikni qaytaradi — keyingi blokni darhol ostiga qo'yish uchun.
func fitHeight(tb *textBody, r rect) float64 {
	ins := tb.insets()
	scaleParas(tb.paras, fitScale(tb.paras, r, ins, tb.minFit, 1))
	return min(r.H, measureText(tb.paras, r.W-ins[0]-ins[2], 1)+ins[1]+ins[3])
}

// tile — yumaloq burchakli rangli plitka.
func tile(c *canvas, r rect, fill string) {
	c.add(r, shape{geom: "roundRect", adj: roundAdj(r, 0.22), fill: solid(fill)})
}

// ===========================================================================
// 9. STRATEGIYA — sarlavha va kirish gapi chap yon panelda, mazmun o'ngda
// ===========================================================================

var strategyTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeStrategy, Name: "Strategiya", Emoji: "🧭",
		Description: "sarlavha yon panelda",
	},
	pal: palette{
		bg: "FAF7F5", surface: "FFFFFF", surface2: "F4EAEE",
		primary: "6D1A36", secondary: "9E3B5C", accent: "E0A458",
		text: "2A1B22", muted: "6E5A63", line: "E7DCE0", number: "6D1A36",
		series:   []string{"6D1A36", "C98532", "9E3B5C", "5C4B51"},
		onSeries: "FFFFFF", panel: solid("6D1A36"), onPanel: "FFFFFF", shadow: "6D1A36",
	},
	headFont: "Georgia", bodyFont: "Tahoma", headBold: true,
	cards:       cardStyle{radius: 0.05, shadow: true, topBar: true},
	badgeSquare: true,
	bulletChar:  "■",
	signature:   []string{LayoutMatrix, LayoutPyramid, LayoutTimeline},
	transition:  `<p:transition spd="med"><p:push dir="r"/></p:transition>`,
	cover:       strategyCover,
	closing:     strategyClosing,
	chrome:      strategyChrome,
}

// strategySquares — o'ng tomondagi kvadratchalar to'ri (bezak).
func strategySquares(t *theme, c *canvas, x, y float64) {
	for row := range 4 {
		for col := range 5 {
			fill := solidA(t.pal.primary, 10+((row+col)%3)*8)
			if (row*5+col)%7 == 3 {
				fill = solid(t.pal.accent)
			}
			c.add(rect{x + float64(col)*0.55, y + float64(row)*0.55, 0.34, 0.34}, shape{fill: fill})
		}
	}
}

func strategyCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	const cw = 7.9
	c.add(rect{0, 0, cw, slideH}, shape{fill: solid(p.primary)})
	c.add(rect{-2.0, 4.4, 4.8, 4.8}, shape{geom: "ellipse", fill: solidA("FFFFFF", 5)})
	c.add(rect{cw, 0, 0.08, slideH}, shape{fill: solid(p.accent)})

	c.text(rect{0.8, 0.55, 6.6, 0.8}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 12, bold: true, caps: true, spc: 1.5, color: "FFFFFF", alpha: 75, font: t.bodyFont}}}}})
	c.add(rect{0.85, 2.0, 0.8, 0.06}, shape{fill: solid(p.accent)})
	c.text(rect{0.8, 2.12, 6.6, 0.45}, textBody{paras: []para{{runs: []run{
		{text: d.WorkType, size: 15, bold: true, spc: 4, color: p.accent, font: t.bodyFont}}}}})
	c.text(rect{0.8, 2.65, 6.7, 2.95}, t.titleBlock(d, 38, "FFFFFF", 17, "FFFFFF", 78, "l"))
	c.text(rect{0.8, 5.85, 6.6, 1.0}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, p.accent, "FFFFFF", 0, "l")})

	c.text(rect{8.6, 1.0, 4.2, 1.3}, textBody{anchor: "b", paras: []para{{runs: []run{
		{text: d.Year, size: 60, bold: true, color: p.primary, font: t.headFont}}}}})
	c.add(rect{8.7, 2.45, 3.9, 0.012}, shape{fill: solid(p.line)})
	strategySquares(t, c, 8.7, 3.2)
}

func strategyClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	const cw = 7.9
	c.add(rect{0, 0, cw, slideH}, shape{fill: solid(p.primary)})
	c.add(rect{4.8, -2.2, 4.6, 4.6}, shape{geom: "ellipse", fill: solidA("FFFFFF", 5)})
	c.add(rect{cw, 0, 0.08, slideH}, shape{fill: solid(p.accent)})

	c.text(rect{0.8, 1.4, 6.6, 2.3}, textBody{anchor: "b", fit: true, paras: []para{{runs: []run{
		{text: d.labels().Thanks, size: 42, bold: true, color: "FFFFFF", font: t.headFont}}}}})
	c.add(rect{0.85, 3.95, 0.8, 0.06}, shape{fill: solid(p.accent)})
	c.text(rect{0.8, 4.15, 6.6, 1.3}, textBody{fit: true, paras: []para{{runs: []run{
		{text: d.Topic, size: 18, color: "FFFFFF", alpha: 80, font: t.bodyFont}}}}})
	if d.StudentName != "" {
		c.text(rect{0.8, 5.7, 6.6, 0.55}, textBody{anchor: "ctr", paras: []para{{runs: []run{
			{text: d.labels().Student + ": ", size: 16, bold: true, color: p.accent, font: t.bodyFont},
			t.tx(d.StudentName, 16, "FFFFFF")}}}})
	}
	strategySquares(t, c, 8.7, 2.3)
}

func strategyChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	const sw, x, w = 4.1, 0.45, 3.3
	c.add(rect{0, 0, sw, slideH}, shape{fill: solid(p.primary)})
	c.add(rect{sw, 0, 0.06, slideH}, shape{fill: solid(p.accent)})
	c.text(rect{x - 0.05, 0.3, w, 1.2}, textBody{paras: []para{{runs: []run{
		{text: pad2(s.num), size: 60, bold: true, color: mixHex("FFFFFF", p.primary, 0.72), font: t.headFont}}}}})
	c.add(rect{x + 0.08, 1.6, 0.7, 0.06}, shape{fill: solid(p.accent)})

	title := textBody{fit: true, minFit: 0.55, paras: []para{{lineSp: 0.98, runs: []run{t.hd(s.title, 26, "FFFFFF")}}}}
	tr := rect{x, 1.8, w, 2.7}
	tr.H = fitHeight(&title, tr)
	c.text(tr, title)
	if s.lead != "" {
		c.text(rect{x, tr.bottom() + 0.2, w, 6.6 - tr.bottom() - 0.2}, textBody{fit: true, minFit: 0.6, paras: []para{{lineSp: 1.05, runs: []run{
			{text: s.lead, size: 13.5, color: "FFFFFF", alpha: 80, font: t.bodyFont}}}}})
	}
	c.text(rect{x, 6.85, w, 0.35}, textBody{anchor: "ctr", paras: []para{{runs: []run{
		{text: pageLabel(s), size: 10, bold: true, spc: 1, color: p.accent, font: t.bodyFont}}}}})

	c.add(rect{4.6, 6.98, 8.13, 0.012}, shape{fill: solid(p.line)})
	t.footer(c, s, 4.6, 12.73, p.muted)
	return rect{4.6, 0.6, 8.13, 6.2}
}

// ===========================================================================
// 10. RETRO — neo-brutalizm: qalin qora chegaralar, qattiq soyalar, yorqin ranglar
// ===========================================================================

var retroTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeRetro, Name: "Retro", Emoji: "🟨",
		Description: "qalin chegaralar, yorqin ranglar",
	},
	pal: palette{
		bg: "FFF4DC", surface: "FFFFFF", surface2: "FFE9A8",
		primary: "111111", secondary: "FF5C8A", accent: "FFC93C",
		text: "111111", muted: "3A3A3A", line: "111111", number: "111111",
		series:   []string{"FF5C8A", "2EC4F1", "3DDC84", "FFC93C", "9B6BFF"},
		tints:    []string{"FFC2D4", "B5EBFA", "BDF2D2", "FFE59E", "D9C9FF"},
		ink:      "111111",
		onSeries: "111111", panel: solid("111111"), onPanel: "FFFFFF", shadow: "111111",
	},
	headFont: "Arial Black", bodyFont: "Arial", headBold: false,
	cards:       cardStyle{radius: 0.08, hard: true, tint: true, borderW: 2},
	badgeSquare: true,
	bulletChar:  "■",
	compact:     true,
	signature:   []string{LayoutMatrix, LayoutCycle, LayoutStatement},
	transition:  `<p:transition spd="med"><p:push dir="u"/></p:transition>`,
	cover:       retroCover,
	closing:     retroClosing,
	chrome:      retroChrome,
}

// popShape qora chegarali va qattiq soyali shakl.
func (t *theme) popShape(c *canvas, r rect, geom, fill string, rot float64) {
	s := shape{geom: geom, fill: solid(fill), rot: rot, line: &stroke{color: t.pal.line, width: 2}}
	t.hardShadow(c, r, s)
	c.add(r, s)
}

// popLabel matnli "yorliq" (qora chegara, qattiq soya); kengligi matnga moslanadi.
func (t *theme) popLabel(c *canvas, x, y float64, text run, fill string, h, maxW float64) rect {
	r := rect{x, y, min(maxW, textWidth(text)+0.6), h}
	s := shape{fill: solid(fill), line: &stroke{color: t.pal.line, width: 2}, text: &textBody{
		anchor: "ctr", fit: true, minFit: 0.55, inset: [4]float64{0.25, 0.04, 0.25, 0.04}, paras: []para{{runs: []run{text}}}}}
	t.hardShadow(c, r, s)
	c.add(r, s)
	return r
}

func retroCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	t.popShape(c, rect{9.2, 0.85, 3.1, 3.1}, "ellipse", p.series[0], 0)
	t.popShape(c, rect{10.85, 3.7, 1.9, 1.9}, "rect", p.series[1], 12)
	t.popShape(c, rect{8.55, 4.35, 1.7, 1.5}, "triangle", p.series[2], -8)
	t.popShape(c, rect{12.0, 0.45, 0.75, 0.75}, "ellipse", p.accent, 0)

	c.text(rect{0.75, 0.5, 7.9, 0.7}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 12, bold: true, caps: true, spc: 1, color: p.text, font: t.bodyFont}}}}})
	// caps: ish turi nomi bosh harflarda — yorliq kengligi shunga qarab hisoblanadi
	// (aks holda "ПРЕЗЕНТАЦИЯ" kabi uzunroq nomlar ikki qatorga bo'linadi).
	t.popLabel(c, 0.75, 1.5, run{text: d.WorkType, size: 13, bold: true, caps: true, spc: 3, color: p.text, font: t.bodyFont}, p.accent, 0.52, 7)
	c.text(rect{0.7, 2.3, 7.9, 3.0}, t.titleBlock(d, 40, p.text, 17, p.muted, 0, "l"))

	if people := t.people(d, 15, p.text, p.text, 0, "l"); len(people) > 0 {
		box := rect{0.75, 5.5, 6.3, 1.25}
		s := shape{fill: solid("FFFFFF"), line: &stroke{color: p.line, width: 2}, text: &textBody{
			anchor: "ctr", fit: true, inset: [4]float64{0.3, 0.08, 0.3, 0.08}, paras: people}}
		t.hardShadow(c, box, s)
		c.add(box, s)
	}
	year := rect{10.6, 6.4, 1.95, 0.62}
	c.add(year, shape{fill: solid(p.primary), text: &textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
		{text: d.Year, size: 18, bold: true, color: p.accent, font: t.headFont}}}}}})
}

func retroClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	t.popShape(c, rect{-0.9, -0.9, 2.6, 2.6}, "ellipse", p.series[1], 0)
	t.popShape(c, rect{11.6, 5.6, 1.6, 1.6}, "rect", p.series[0], -10)
	t.popShape(c, rect{0.7, 5.9, 1.1, 1.0}, "triangle", p.series[2], 8)

	box := rect{1.6, 1.55, 10.13, 1.75}
	s := shape{fill: solid(p.accent), line: &stroke{color: p.line, width: 2.5}, text: &textBody{anchor: "ctr", fit: true,
		inset: [4]float64{0.3, 0.1, 0.3, 0.1}, paras: []para{{align: "ctr", runs: []run{{text: d.labels().Thanks, size: 40, color: p.text, font: t.headFont}}}}}}
	t.hardShadow(c, box, s)
	c.add(box, s)
	c.text(rect{1.6, 3.75, 10.13, 1.15}, textBody{fit: true, paras: []para{{align: "ctr", runs: []run{t.tx(d.Topic, 19, p.muted)}}}})
	if d.StudentName != "" {
		c.text(rect{1.6, 5.05, 10.13, 0.55}, textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
			{text: d.labels().Student + ": ", size: 16, bold: true, color: p.text, font: t.bodyFont},
			t.tx(d.StudentName, 16, p.text)}}}})
	}
}

func retroChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	title := run{text: s.title, size: 26, color: p.text, font: t.headFont}
	h := 0.92
	if textWidth(title)+0.6 > 10.7 {
		h = 1.3 // uzun sarlavha ikki qatorda
	}
	box := t.popLabel(c, 0.6, 0.42, title, p.accent, h, 10.7)
	num := rect{11.92, 0.45, 0.82, 0.82}
	ns := shape{geom: "ellipse", fill: solid(p.secondary), line: &stroke{color: p.line, width: 2}, text: &textBody{
		anchor: "ctr", inset: [4]float64{0.01, 0.01, 0.01, 0.01}, paras: []para{{align: "ctr", runs: []run{
			{text: pad2(s.num), size: 17, color: p.text, font: t.headFont}}}}}}
	t.hardShadow(c, num, ns)
	c.add(num, ns)

	y := box.bottom() + 0.38
	if s.lead != "" {
		c.text(rect{0.55, box.bottom() + 0.2, 12.2, 0.64}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{
			t.tx(s.lead, 15, p.muted)}}}})
		y = box.bottom() + 0.98
	}
	c.add(rect{0, 7.02, slideW, 0.48}, shape{fill: solid(p.primary)})
	c.text(rect{0.6, 7.06, 8.6, 0.4}, line1(s.topic, 10, "FFFFFF", t.bodyFont, "l"))
	c.text(rect{9.3, 7.06, 3.45, 0.4}, textBody{anchor: "ctr", paras: []para{{align: "r", runs: []run{
		{text: pageLabel(s), size: 10.5, bold: true, spc: 1, color: p.accent, font: t.bodyFont}}}}})
	return rect{0.6, y, 12.05, 6.72 - y}
}

// ===========================================================================
// 11. TIPOGRAFIKA — shveytsar uslubi: katta shrift, qizil-qora, kartochkasiz
// ===========================================================================

var typoTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeTypo, Name: "Tipografika", Emoji: "🔠",
		Description: "katta shrift, qizil va qora",
	},
	pal: palette{
		bg: "FFFFFF", surface: "FFFFFF", surface2: "F3F3F3",
		primary: "111111", secondary: "111111", accent: "E3000F",
		text: "111111", muted: "555555", line: "D6D6D6", number: "E3000F",
		series:   []string{"E3000F", "111111"},
		onSeries: "FFFFFF", panel: solid("E3000F"), onPanel: "FFFFFF", shadow: "000000",
	},
	headFont: "Arial", bodyFont: "Arial", headBold: true,
	cards:      cardStyle{rule: true},
	badgePlain: true,
	bulletChar: "—",
	compact:    true,
	signature:  []string{LayoutStatement, LayoutTimeline, LayoutMatrix},
	transition: fadeTransition,
	cover:      typoCover,
	closing:    typoClosing,
	chrome:     typoChrome,
}

func typoCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid("FFFFFF")
	c.text(rect{5.0, 3.1, 8.2, 4.4}, textBody{anchor: "b", inset: [4]float64{0, 0, 0.05, 0}, paras: []para{{align: "r", runs: []run{
		{text: d.Year, size: 190, bold: true, color: "F0F0F0", font: t.headFont}}}}})
	c.add(rect{0.75, 0.55, 11.83, 0.03}, shape{fill: solid(p.text)})
	c.text(rect{0.7, 0.64, 7.6, 0.42}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 10, bold: true, caps: true, spc: 1.5, color: p.text, font: t.bodyFont}}}}})
	c.text(rect{8.4, 0.64, 4.23, 0.42}, textBody{anchor: "ctr", paras: []para{{align: "r", runs: []run{
		{text: d.WorkType, size: 10, bold: true, caps: true, spc: 3, color: p.accent, font: t.bodyFont}}}}})
	c.add(rect{0.75, 1.45, 0.62, 0.62}, shape{fill: solid(p.accent)})
	c.text(rect{0.66, 2.3, 10.2, 3.2}, t.titleBlock(d, 52, p.text, 18, p.muted, 0, "l"))

	c.add(rect{0.75, 5.85, 6.6, 0.03}, shape{fill: solid(p.text)})
	cols := []struct {
		r            rect
		label, value string
	}{
		{rect{0.7, 5.98, 3.3, 0.9}, strings.ToUpper(d.labels().Student), d.StudentName},
		{rect{4.1, 5.98, 3.3, 0.9}, strings.ToUpper(d.labels().Teacher), d.TeacherName},
	}
	for _, col := range cols {
		if col.value == "" {
			continue
		}
		c.text(col.r, textBody{fit: true, paras: []para{
			{runs: []run{{text: col.label, size: 9.5, bold: true, spc: 2, color: p.accent, font: t.bodyFont}}},
			{before: 3, runs: []run{t.tx(col.value, 15, p.text)}},
		}})
	}
}

func typoClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid("FFFFFF")
	c.add(rect{0.75, 0.55, 11.83, 0.03}, shape{fill: solid(p.text)})
	c.add(rect{0.75, 1.3, 0.62, 0.62}, shape{fill: solid(p.accent)})
	c.text(rect{0.66, 2.05, 11.6, 2.4}, textBody{fit: true, paras: []para{{lineSp: 0.95, runs: []run{
		{text: d.labels().Thanks, size: 58, bold: true, color: p.text, font: t.headFont}}}}})
	c.add(rect{0.75, 4.6, 1.6, 0.07}, shape{fill: solid(p.accent)})
	c.text(rect{0.66, 4.8, 10.5, 1.0}, textBody{fit: true, paras: []para{{runs: []run{t.tx(d.Topic, 18, p.muted)}}}})
	if d.StudentName != "" {
		c.text(rect{0.66, 5.95, 8, 0.85}, textBody{paras: []para{
			{runs: []run{{text: strings.ToUpper(d.labels().Student), size: 9.5, bold: true, spc: 2, color: p.accent, font: t.bodyFont}}},
			{before: 3, runs: []run{t.tx(d.StudentName, 15, p.text)}},
		}})
	}
}

func typoChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid("FFFFFF")
	c.text(rect{0.7, 0.26, 8.5, 0.34}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: s.university, size: 9, bold: true, caps: true, spc: 1.5, color: p.muted, font: t.bodyFont}}}}})
	c.text(rect{9.3, 0.26, 3.33, 0.34}, textBody{anchor: "ctr", paras: []para{{align: "r", runs: []run{
		{text: pageLabel(s), size: 9.5, bold: true, spc: 1, color: p.text, font: t.bodyFont}}}}})
	c.add(rect{0.75, 0.64, 11.83, 0.014}, shape{fill: solid(p.text)})

	c.text(rect{0.62, 0.72, 1.75, 1.2}, textBody{inset: [4]float64{0.02, 0, 0.02, 0}, paras: []para{{runs: []run{
		{text: pad2(s.num), size: 58, bold: true, color: p.accent, font: t.headFont}}}}})
	c.text(rect{2.4, 0.78, 10.2, 1.05}, textBody{anchor: "ctr", fit: true, minFit: 0.55, paras: []para{{lineSp: 0.95, runs: []run{t.hd(s.title, 32, p.text)}}}})
	y := 2.0
	if s.lead != "" {
		c.text(rect{2.4, 1.82, 10.2, 0.62}, textBody{fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 14, p.muted)}}}})
		y = 2.5
	}
	c.add(rect{0.75, y, 11.83, 0.05}, shape{fill: solid(p.text)})
	c.text(rect{0.7, 7.02, 11.9, 0.32}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: s.topic, size: 9, caps: true, spc: 1, color: p.muted, font: t.bodyFont}}}}})
	return rect{0.75, y + 0.32, 11.83, 6.88 - y - 0.32}
}

// ===========================================================================
// 12. BENTO — plitkalar to'ri: sarlavha ham plitkada, kartochkalar har xil o'lchamda
// ===========================================================================

var bentoTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeBento, Name: "Bento", Emoji: "🍱",
		Description: "har xil o'lchamli plitkalar",
	},
	pal: palette{
		bg: "ECEEF2", surface: "FFFFFF", surface2: "E2E6EC",
		primary: "15171C", secondary: "3B5BDB", accent: "F59F00",
		text: "15171C", muted: "4D5562", line: "D5DAE1", number: "3B5BDB",
		series:   []string{"3B5BDB", "F59F00", "12B886", "E64980", "7048E8"},
		onSeries: "FFFFFF", panel: solid("15171C"), onPanel: "FFFFFF", shadow: "15171C",
	},
	headFont: "Trebuchet MS", bodyFont: "Tahoma", headBold: true,
	cards:       cardStyle{radius: 0.22},
	badgeSquare: true,
	bulletChar:  "●",
	signature:   []string{LayoutMatrix, LayoutHub, LayoutStatement},
	layouts:     map[string]layoutFunc{LayoutCards: bentoCards},
	transition:  fadeTransition,
	cover:       bentoCover,
	closing:     bentoClosing,
	chrome:      bentoChrome,
}

func bentoCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	const g = 0.2
	main := rect{0.45, 0.45, 8.15, 6.6}
	tile(c, main, p.primary)
	t.chip(c, main.X+0.5, main.Y+0.55, d.WorkType, p.accent, p.primary)
	c.text(rect{main.X + 0.45, 1.75, main.W - 0.9, 3.5}, t.titleBlock(d, 40, "FFFFFF", 17, "FFFFFF", 75, "l"))
	c.text(rect{main.X + 0.45, 5.6, main.W - 0.9, 1.1}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, "FFFFFF", "FFFFFF", 65, "l")})

	rx := main.right() + g
	rw := slideW - 0.45 - rx
	year := rect{rx, 0.45, rw, 2.75}
	tile(c, year, p.secondary)
	c.text(year, line1(d.Year, 54, "FFFFFF", t.headFont, "ctr"))
	uni := rect{rx, year.bottom() + g, rw, 1.75}
	tile(c, uni, p.surface)
	c.text(uni.inset(0.3, 0.2), textBody{anchor: "ctr", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.University, size: 15, bold: true, color: p.text, font: t.bodyFont}}}}})
	deco := rect{rx, uni.bottom() + g, rw, main.bottom() - uni.bottom() - g}
	tile(c, deco, p.accent)
	for i, r := range []float64{0.32, 0.5, 0.7} {
		ring(c, deco.X+deco.W*0.3+float64(i)*0.85, deco.Y+deco.H/2, r, "FFFFFF", 2, 70, "")
	}
}

func bentoClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	const g = 0.2
	main := rect{0.45, 0.45, 8.15, 6.6}
	tile(c, main, p.primary)
	c.text(rect{main.X + 0.5, 1.4, main.W - 1.0, 2.3}, textBody{anchor: "b", fit: true, paras: []para{{runs: []run{
		{text: d.labels().Thanks, size: 44, bold: true, color: "FFFFFF", font: t.headFont}}}}})
	c.add(rect{main.X + 0.55, 3.95, 0.9, 0.08}, shape{geom: "roundRect", adj: []int{50000}, fill: solid(p.accent)})
	if d.StudentName != "" {
		c.text(rect{main.X + 0.5, 4.3, main.W - 1.0, 0.6}, textBody{anchor: "ctr", paras: []para{{runs: []run{
			{text: d.labels().Student + ": ", size: 16, color: "FFFFFF", alpha: 65, font: t.bodyFont},
			{text: d.StudentName, size: 16, bold: true, color: "FFFFFF", font: t.bodyFont}}}}})
	}
	rx := main.right() + g
	rw := slideW - 0.45 - rx
	topic := rect{rx, 0.45, rw, 4.2}
	tile(c, topic, p.secondary)
	c.text(topic.inset(0.35, 0.35), textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.Topic, size: 20, bold: true, color: "FFFFFF", font: t.headFont}}}}})
	year := rect{rx, topic.bottom() + g, rw, main.bottom() - topic.bottom() - g}
	tile(c, year, p.accent)
	c.text(year, line1(d.Year, 40, p.primary, t.headFont, "ctr"))
}

func bentoChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	const x, g, numW = 0.45, 0.2, 1.45
	head := rect{x, 0.4, slideW - 2*x - numW - g, 1.15}
	if s.lead != "" {
		head.H = 1.55
	}
	tile(c, head, p.primary)
	if s.lead != "" {
		c.text(rect{head.X + 0.35, head.Y + 0.12, head.W - 0.7, 0.78}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 26, "FFFFFF")}}}})
		c.text(rect{head.X + 0.35, head.Y + 0.88, head.W - 0.7, 0.58}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{
			{text: s.lead, size: 13.5, color: "FFFFFF", alpha: 75, font: t.bodyFont}}}}})
	} else {
		c.text(rect{head.X + 0.35, head.Y, head.W - 0.7, head.H}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{t.hd(s.title, 28, "FFFFFF")}}}})
	}
	num := rect{head.right() + g, head.Y, numW, head.H}
	tile(c, num, p.secondary)
	c.text(num, textBody{anchor: "ctr", inset: [4]float64{0.05, 0.05, 0.05, 0.05}, paras: []para{
		{align: "ctr", runs: []run{{text: pad2(s.num), size: 32, bold: true, color: "FFFFFF", font: t.headFont}}},
		{align: "ctr", runs: []run{{text: "/ " + pad2(s.total), size: 11, color: "FFFFFF", alpha: 75, font: t.bodyFont}}},
	}})
	t.footer(c, s, x+0.1, slideW-x-0.1, p.muted)
	top := head.bottom() + g
	return rect{x, top, slideW - 2*x, 6.92 - top}
}

// bentoCards — kartochkalar har xil o'lchamli plitkalarda: birinchisi katta
// va rangli (asosiy), qolganlari uning atrofida.
func bentoCards(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxListItems)
	n := len(items)
	const g = 0.2
	var cells []rect
	switch n {
	case 2:
		cells = body.cols(2, g)
	case 3:
		l, r := body.splitX((body.W-g)*0.5, g)
		cells = append([]rect{l}, r.rows(2, g)...)
	case 4:
		l, r := body.splitX((body.W-g)*0.38, g)
		top, bot := r.splitY((r.H-g)/2, g)
		cells = append([]rect{l, top}, bot.cols(2, g)...)
	case 5:
		top, bot := body.splitY((body.H-g)/2, g)
		a, b := top.splitX((top.W-g)*0.6, g)
		cells = append([]rect{a, b}, bot.cols(3, g)...)
	default:
		top, bot := body.splitY((body.H-g)/2, g)
		u := (body.W - 3*g) / 4
		cells = []rect{
			{top.X, top.Y, 2*u + g, top.H}, {top.X + 2*u + 2*g, top.Y, u, top.H}, {top.X + 3*u + 3*g, top.Y, u, top.H},
			{bot.X, bot.Y, u, bot.H}, {bot.X + u + g, bot.Y, u, bot.H}, {bot.X + 2*u + 2*g, bot.Y, 2*u + g, bot.H},
		}
	}

	var hero, rest []placed
	for i, it := range items {
		r := cells[i]
		in := r.inset(0.32, 0.26)
		if i == 0 {
			tile(c, r, t.seriesColor(0))
			c.text(rect{in.X - 0.05, in.Y - 0.12, 2.0, 1.0}, textBody{inset: [4]float64{0.02, 0, 0.02, 0}, paras: []para{{runs: []run{
				{text: pad2(1), size: 46, bold: true, color: mixHex("FFFFFF", t.seriesColor(0), 0.55), font: t.headFont}}}}})
			hero = append(hero, placed{rect{in.X - 0.05, in.Y + 0.85, in.W + 0.1, in.H - 0.85},
				textBody{anchor: "b", fit: true, paras: t.itemParasC(it, 22, 15, "l", 6, "FFFFFF", "EEF1FF")}})
			continue
		}
		tile(c, r, t.pal.surface)
		c.add(rect{in.X, in.Y, 0.42, 0.09}, shape{geom: "roundRect", adj: []int{50000}, fill: solid(t.seriesColor(i))})
		c.text(rect{r.right() - 1.35, r.bottom() - 0.85, 1.2, 0.75}, textBody{anchor: "b", inset: [4]float64{0.02, 0, 0.02, 0}, paras: []para{{align: "r", runs: []run{
			{text: pad2(i + 1), size: 34, bold: true, color: mixHex(t.seriesColor(i), t.pal.surface, 0.75), font: t.headFont}}}}})
		rest = append(rest, placed{rect{in.X - 0.05, in.Y + 0.2, in.W + 0.1, in.H - 0.2},
			textBody{fit: true, paras: t.itemParas(it, 16.5, 13, "l", 5)}})
	}
	fitTogether(hero, growCards)
	fitTogether(rest, growCards)
	drawAll(c, hero)
	drawAll(c, rest)
}

// ===========================================================================
// 13. DOSKA — sinf doskasi: yog'och ramka, bo'r yozuvi, punktir chegaralar
// ===========================================================================

var boardTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeBoard, Name: "Doska", Emoji: "🖍",
		Description: "sinf doskasi, bo'r yozuvi",
	},
	pal: palette{
		bg: "26453A", surface: "FFFFFF", surface2: "2F5446",
		primary: "F4F1E8", secondary: "F7D774", accent: "F7D774",
		text: "F4F1E8", muted: "C9D6CC", line: "9DB5A8", number: "F7D774",
		series:   []string{"F7D774", "9ED8F5", "F5A9A9", "B9E58C"},
		onSeries: "26453A", panel: solid("2F5446"), onPanel: "F4F1E8", shadow: "000000",
	},
	headFont: "Comic Sans MS", bodyFont: "Trebuchet MS", headBold: true,
	cards:        cardStyle{radius: 0.12, dash: "dash", borderW: 1.5, fillAlpha: 5},
	badgeOutline: true,
	bulletChar:   "•",
	dark:         true,
	signature:    []string{LayoutPyramid, LayoutCycle, LayoutHub},
	transition:   fadeTransition,
	cover:        boardCover,
	closing:      boardClosing,
	chrome:       boardChrome,
}

// boardFrame — doska foni, bo'r izlari va yog'och ramka.
func boardFrame(t *theme, c *canvas) {
	c.darkMap = true
	c.bg = solid(t.pal.bg)
	c.add(rect{1.2, 0.9, 6.2, 2.6}, shape{geom: "ellipse", fill: solidA("FFFFFF", 2)})
	c.add(rect{6.9, 4.1, 5.6, 2.8}, shape{geom: "ellipse", fill: solidA("FFFFFF", 2)})
	const f = 0.2
	wood, dark := "7A5230", "4A2E17"
	for _, r := range []rect{{0, 0, slideW, f}, {0, slideH - f, slideW, f}, {0, 0, f, slideH}, {slideW - f, 0, f, slideH}} {
		c.add(r, shape{fill: solid(wood)})
	}
	c.add(rect{f, f, slideW - 2*f, slideH - 2*f}, shape{line: &stroke{color: dark, width: 1.5}})
}

// chalkUnderline — qo'lda chizilgandek biroz qiya chiziq.
func chalkUnderline(c *canvas, cx, y, w float64, color string) {
	c.add(rect{cx - w/2, y, w, 0.05}, shape{fill: solidA(color, 85), rot: -1.2})
	c.add(rect{cx - w/2 + 0.25, y + 0.09, w * 0.7, 0.035}, shape{fill: solidA(color, 55), rot: -0.8})
}

func boardCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	boardFrame(t, c)
	c.text(rect{1.0, 0.55, 11.33, 0.6}, textBody{anchor: "ctr", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.University, size: 13, color: p.muted, font: t.bodyFont}}}}})
	c.text(rect{1.0, 1.55, 11.33, 0.55}, textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
		{text: d.WorkType, size: 18, bold: true, spc: 4, color: p.accent, font: t.headFont}}}}})
	title := t.titleBlock(d, 42, p.text, 18, p.muted, 0, "ctr")
	title.anchor = "ctr"
	c.text(rect{1.3, 2.15, 10.73, 2.7}, title)
	chalkUnderline(c, slideW/2, 4.95, 3.4, p.accent)
	c.text(rect{1.5, 5.35, 10.33, 1.0}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, p.accent, p.text, 0, "ctr")})
	ring(c, 11.85, 6.45, 0.42, p.text, 1.25, 70, "")
	c.text(rect{11.3, 6.2, 1.1, 0.5}, line1(d.Year, 13, p.text, t.headFont, "ctr"))
	// Bo'r bilan chizilgan bezaklar: yulduzcha va doiracha.
	ring(c, 1.35, 6.4, 0.3, p.series[1], 1.5, 80, "dash")
	c.add(rect{11.6, 0.6, 0.55, 0.55}, shape{geom: "star5", line: &stroke{color: p.series[2], width: 1.5}})
}

func boardClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	boardFrame(t, c)
	c.text(rect{1.0, 1.6, 11.33, 1.7}, textBody{anchor: "b", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.labels().Thanks, size: 44, bold: true, color: p.text, font: t.headFont}}}}})
	chalkUnderline(c, slideW/2, 3.5, 3.0, p.accent)
	c.text(rect{1.6, 3.85, 10.13, 1.1}, textBody{fit: true, paras: []para{{align: "ctr", runs: []run{t.tx(d.Topic, 18, p.muted)}}}})
	if d.StudentName != "" {
		c.text(rect{1.6, 5.2, 10.13, 0.55}, textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
			{text: d.labels().Student + ": ", size: 16, bold: true, color: p.accent, font: t.headFont},
			t.tx(d.StudentName, 16, p.text)}}}})
	}
	c.add(rect{1.0, 0.7, 0.6, 0.6}, shape{geom: "star5", line: &stroke{color: p.series[1], width: 1.5}})
	ring(c, 12.0, 6.3, 0.35, p.series[2], 1.5, 80, "dash")
}

func boardChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	boardFrame(t, c)
	c.text(rect{1.3, 0.4, 10.73, 0.95}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{align: "ctr", runs: []run{t.hd(s.title, 30, p.text)}}}})
	chalkUnderline(c, slideW/2, 1.38, 2.6, p.accent)
	y := 1.75
	if s.lead != "" {
		c.text(rect{1.4, 1.58, 10.53, 0.6}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{align: "ctr", runs: []run{t.tx(s.lead, 15, p.muted)}}}})
		y = 2.3
	}
	ring(c, 12.4, 6.88, 0.27, p.text, 1.25, 75, "")
	c.text(rect{12.05, 6.66, 0.7, 0.44}, line1(pad2(s.num), 12, p.text, t.headFont, "ctr"))
	c.text(rect{0.55, 6.84, 10.5, 0.34}, line1(s.topic, 10, p.muted, t.bodyFont, "l"))
	return rect{0.75, y, 11.83, 6.62 - y}
}

// ===========================================================================
// 14. CHIZMA — muhandislik chizmasi: ko'k katakli qog'oz, ramka va shtamp
// ===========================================================================

var blueprintTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemeBlueprint, Name: "Chizma", Emoji: "📐",
		Description: "muhandislik chizmasi",
	},
	pal: palette{
		bg: "0E3A73", surface: "FFFFFF", surface2: "134583",
		primary: "FFFFFF", secondary: "8FD0FF", accent: "FFD166",
		text: "FFFFFF", muted: "B7D0EE", line: "7FA3D4", number: "FFD166",
		series:   []string{"FFD166", "8FD0FF", "FFFFFF", "7FE3C4"},
		onSeries: "0E3A73", panel: solid("0A2D5C"), onPanel: "FFFFFF", shadow: "000000",
	},
	headFont: "Courier New", bodyFont: "Tahoma", headBold: true,
	cards:        cardStyle{ticks: true, borderW: 0.75, fillAlpha: 6},
	badgeSquare:  true,
	badgeOutline: true,
	bulletChar:   "▪",
	dark:         true,
	signature:    []string{LayoutHub, LayoutCycle, LayoutTimeline},
	transition:   `<p:transition spd="med"><p:wipe dir="r"/></p:transition>`,
	cover:        blueprintCover,
	closing:      blueprintClosing,
	chrome:       blueprintChrome,
}

// blueprintGrid — katakli fon va chizma ramkasi.
func blueprintGrid(t *theme, c *canvas) {
	c.darkMap = true
	c.bg = solid(t.pal.bg)
	const step = 0.25
	for i := 1; float64(i)*step < slideW; i++ {
		w, a := 0.006, 7
		if i%4 == 0 {
			w, a = 0.01, 16
		}
		c.add(rect{float64(i)*step - w/2, 0, w, slideH}, shape{fill: solidA("FFFFFF", a)})
	}
	for i := 1; float64(i)*step < slideH; i++ {
		w, a := 0.006, 7
		if i%4 == 0 {
			w, a = 0.01, 16
		}
		c.add(rect{0, float64(i)*step - w/2, slideW, w}, shape{fill: solidA("FFFFFF", a)})
	}
	c.add(rect{0.3, 0.3, slideW - 0.6, slideH - 0.6}, shape{line: &stroke{color: "FFFFFF", width: 1, alpha: 75}})
}

// dimLine — o'lcham chizig'i: uchlarida chiziqcha va strelka.
func dimLine(c *canvas, x, y, w float64, color string) {
	c.add(rect{x, y - 0.006, w, 0.012}, shape{fill: solidA(color, 90)})
	c.add(rect{x, y - 0.09, 0.012, 0.18}, shape{fill: solid(color)})
	c.add(rect{x + w - 0.012, y - 0.09, 0.012, 0.18}, shape{fill: solid(color)})
	c.add(rect{x + 0.02, y - 0.05, 0.12, 0.1}, shape{geom: "triangle", rot: -90, fill: solid(color)})
	c.add(rect{x + w - 0.14, y - 0.05, 0.12, 0.1}, shape{geom: "triangle", rot: 90, fill: solid(color)})
}

// stampTable — chizma shtampi: chapda nomlar, o'ngda qiymatlar (bo'sh qiymatli qatorlar tashlanadi).
func (t *theme) stampTable(c *canvas, r rect, labelW float64, rows [][2]string) {
	var kept [][2]string
	for _, row := range rows {
		if row[1] != "" {
			kept = append(kept, row)
		}
	}
	if len(kept) == 0 {
		return
	}
	c.add(r, shape{fill: solid(t.pal.bg), line: &stroke{color: "FFFFFF", width: 1, alpha: 75}})
	c.add(rect{r.X + labelW, r.Y, 0.01, r.H}, shape{fill: solidA("FFFFFF", 60)})
	rh := r.H / float64(len(kept))
	for i, row := range kept {
		y := r.Y + float64(i)*rh
		if i > 0 {
			c.add(rect{r.X, y, r.W, 0.01}, shape{fill: solidA("FFFFFF", 60)})
		}
		c.text(rect{r.X, y, labelW, rh}, textBody{anchor: "ctr", fit: true, inset: [4]float64{0.12, 0.02, 0.08, 0.02}, paras: []para{{runs: []run{
			{text: row[0], size: 9, bold: true, spc: 1, color: t.pal.muted, font: t.headFont}}}}})
		c.text(rect{r.X + labelW, y, r.W - labelW, rh}, textBody{anchor: "ctr", fit: true, minFit: 0.6, inset: [4]float64{0.12, 0.02, 0.1, 0.02}, paras: []para{{runs: []run{
			t.tx(row[1], 12, t.pal.text)}}}})
	}
}

func blueprintCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	blueprintGrid(t, c)
	cx, cy := 10.25, 2.85
	c.add(rect{cx - 2.55, cy - 0.005, 5.1, 0.01}, shape{fill: solidA("FFFFFF", 55)})
	c.add(rect{cx - 0.005, cy - 2.5, 0.01, 5.0}, shape{fill: solidA("FFFFFF", 55)})
	for _, r := range []float64{1.85, 1.3, 0.55} {
		ring(c, cx, cy, r, "FFFFFF", 1, 85, "")
	}
	ring(c, cx, cy, 2.2, p.accent, 1, 75, "dash")
	dimLine(c, cx-1.85, cy+2.05, 3.7, p.accent)
	c.text(rect{cx - 1.2, cy + 1.6, 2.4, 0.4}, line1("Ø "+d.Year, 12, p.accent, t.headFont, "ctr"))

	c.text(rect{0.7, 0.55, 7.4, 0.7}, textBody{anchor: "ctr", fit: true, paras: []para{{runs: []run{
		{text: d.University, size: 11, bold: true, caps: true, spc: 1, color: p.muted, font: t.bodyFont}}}}})
	c.text(rect{0.7, 1.7, 7.4, 0.45}, textBody{paras: []para{{runs: []run{
		{text: "// " + d.WorkType, size: 14, bold: true, spc: 2, color: p.accent, font: t.headFont}}}}})
	title := t.titleBlock(d, 34, "FFFFFF", 15, p.muted, 0, "l")
	tr := rect{0.65, 2.2, 7.6, 2.9}
	tr.H = fitHeight(&title, tr)
	c.text(tr, title)
	dimLine(c, 0.75, tr.bottom()+0.3, 3.2, p.accent)
	t.stampTable(c, rect{slideW - 0.3 - 5.6, slideH - 0.3 - 1.35, 5.6, 1.35}, 1.75, [][2]string{
		{strings.ToUpper(d.labels().Student), d.StudentName}, {strings.ToUpper(d.labels().Teacher), d.TeacherName}, {strings.ToUpper(d.labels().Year), d.Year},
	})
}

func blueprintClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	blueprintGrid(t, c)
	c.text(rect{1.0, 1.5, 11.33, 1.7}, textBody{anchor: "b", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.labels().Thanks, size: 40, bold: true, color: "FFFFFF", font: t.headFont}}}}})
	dimLine(c, slideW/2-2.0, 3.45, 4.0, p.accent)
	c.text(rect{1.6, 3.75, 10.13, 1.1}, textBody{fit: true, paras: []para{{align: "ctr", runs: []run{t.tx(d.Topic, 17, p.muted)}}}})
	t.stampTable(c, rect{slideW - 0.3 - 5.0, slideH - 0.3 - 0.9, 5.0, 0.9}, 1.6, [][2]string{
		{strings.ToUpper(d.labels().Student), d.StudentName}, {strings.ToUpper(d.labels().Year), d.Year},
	})
}

func blueprintChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	blueprintGrid(t, c)
	c.text(rect{0.6, 0.45, 11.6, 0.85}, textBody{anchor: "ctr", fit: true, minFit: 0.55, paras: []para{{runs: []run{
		{text: pad2(s.num) + " / ", size: 24, bold: true, color: p.accent, font: t.headFont},
		{text: s.title, size: 24, bold: true, caps: true, color: p.text, font: t.headFont}}}}})
	dimLine(c, 0.68, 1.38, 4.6, p.accent)
	y := 1.65
	if s.lead != "" {
		c.text(rect{0.6, 1.5, 11.8, 0.62}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{runs: []run{t.tx(s.lead, 14, p.muted)}}}})
		y = 2.25
	}
	tb := rect{slideW - 0.3 - 4.95, slideH - 0.3 - 0.9, 4.95, 0.9}
	c.add(tb, shape{fill: solid(p.bg), line: &stroke{color: "FFFFFF", width: 1, alpha: 75}})
	split := tb.X + 3.55
	c.add(rect{split, tb.Y, 0.01, tb.H}, shape{fill: solidA("FFFFFF", 60)})
	c.add(rect{tb.X, tb.Y + tb.H/2, split - tb.X, 0.01}, shape{fill: solidA("FFFFFF", 60)})
	c.text(rect{tb.X, tb.Y, split - tb.X, tb.H / 2}, textBody{anchor: "ctr", fit: true, minFit: 0.6, inset: [4]float64{0.1, 0.02, 0.08, 0.02}, paras: []para{{runs: []run{
		t.tx(s.topic, 10, p.text)}}}})
	c.text(rect{tb.X, tb.Y + tb.H/2, split - tb.X, tb.H / 2}, textBody{anchor: "ctr", fit: true, minFit: 0.6, inset: [4]float64{0.1, 0.02, 0.08, 0.02}, paras: []para{{runs: []run{
		t.tx(s.university, 9, p.muted)}}}})
	c.text(rect{split, tb.Y, tb.right() - split, tb.H}, textBody{anchor: "ctr", inset: [4]float64{0.05, 0.02, 0.05, 0.02}, paras: []para{
		{align: "ctr", runs: []run{{text: "VARAQ", size: 8, bold: true, spc: 1, color: p.muted, font: t.headFont}}},
		{align: "ctr", runs: []run{{text: pageLabel(s), size: 14, bold: true, color: p.accent, font: t.headFont}}},
	}})
	return rect{0.65, y, 12.03, 6.12 - y}
}

// ===========================================================================
// 15. PASTEL — yumshoq ranglar: markazlashgan sarlavha, kapsula kartochkalar
// ===========================================================================

var pastelTheme = &theme{
	ThemeInfo: ThemeInfo{
		ID: ThemePastel, Name: "Pastel", Emoji: "🍬",
		Description: "yumshoq, markazlashgan",
	},
	pal: palette{
		bg: "FFF9F4", surface: "FFFFFF", surface2: "F4ECFF",
		primary: "5B4BC4", secondary: "F27A9B", accent: "F2A541",
		text: "2F2A4A", muted: "6B6685", line: "E4DCEE", number: "5B4BC4",
		series:   []string{"5B7FD6", "E0628A", "2FA57A", "E08E2B", "8A63D2"},
		tints:    []string{"DCE7FF", "FFDDE7", "D6F3E6", "FFE8CC", "EBE0FF"},
		onSeries: "FFFFFF", panel: solid("5B4BC4"), onPanel: "FFFFFF", shadow: "5B4BC4",
	},
	headFont: "Arial Rounded MT Bold", bodyFont: "Tahoma", headBold: false,
	cards:      cardStyle{radius: 0.32, tint: true},
	bulletChar: "●",
	compact:    true,
	signature:  []string{LayoutCycle, LayoutHub, LayoutPyramid},
	transition: fadeTransition,
	cover:      pastelCover,
	closing:    pastelClosing,
	chrome:     pastelChrome,
}

// pastelBlobs — yumshoq rangli doiralar (bezak).
func pastelBlobs(t *theme, c *canvas, big bool) {
	tn := t.pal.tints
	if big {
		c.add(rect{-1.6, 3.9, 5.0, 5.0}, shape{geom: "ellipse", fill: solid(tn[2])})
		c.add(rect{9.9, -1.8, 5.2, 5.2}, shape{geom: "ellipse", fill: solid(tn[1])})
		c.add(rect{11.2, 5.4, 1.5, 1.5}, shape{geom: "ellipse", fill: solid(tn[3])})
		c.add(rect{1.3, 0.7, 0.7, 0.7}, shape{geom: "ellipse", fill: solid(tn[0])})
		return
	}
	c.add(rect{-1.3, -1.5, 3.4, 3.4}, shape{geom: "ellipse", fill: solid(tn[0])})
	c.add(rect{11.7, 5.9, 2.8, 2.8}, shape{geom: "ellipse", fill: solid(tn[1])})
	c.add(rect{12.3, -0.5, 1.3, 1.3}, shape{geom: "ellipse", fill: solid(tn[2])})
}

func pastelCover(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	pastelBlobs(t, c, true)
	c.text(rect{1.5, 0.6, 10.33, 0.6}, textBody{anchor: "ctr", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.University, size: 13, color: p.muted, font: t.bodyFont}}}}})
	chip := run{text: d.WorkType, size: 12, bold: true, spc: 3, color: "FFFFFF", font: t.bodyFont}
	cw := min(7.0, textWidth(chip)+0.7)
	t.chip(c, (slideW-cw)/2, 1.55, d.WorkType, p.primary, "FFFFFF")
	title := t.titleBlock(d, 44, p.text, 18, p.muted, 0, "ctr")
	title.anchor = "ctr"
	c.text(rect{1.4, 2.2, 10.53, 2.8}, title)
	c.text(rect{2.5, 5.25, 8.33, 0.95}, textBody{anchor: "ctr", fit: true, paras: t.people(d, 15, p.primary, p.text, 0, "ctr")})
	year := rect{slideW/2 - 0.65, 6.4, 1.3, 0.46}
	c.add(year, shape{geom: "roundRect", adj: []int{50000}, fill: solid(p.tints[4]), text: &textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
		{text: d.Year, size: 13, color: p.primary, font: t.headFont}}}}}})
}

func pastelClosing(t *theme, c *canvas, d *Deck) {
	p := t.pal
	c.bg = solid(p.bg)
	pastelBlobs(t, c, true)
	c.text(rect{1.0, 1.6, 11.33, 1.7}, textBody{anchor: "b", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: d.labels().Thanks, size: 44, color: p.text, font: t.headFont}}}}})
	topic := run{text: d.Topic, size: 16, color: p.primary, font: t.bodyFont}
	pw := min(10.5, textWidth(topic)+0.9)
	pill := rect{(slideW - pw) / 2, 3.6, pw, 0.85}
	c.add(pill, shape{geom: "roundRect", adj: []int{50000}, fill: solid(p.tints[4]), text: &textBody{
		anchor: "ctr", fit: true, inset: [4]float64{0.35, 0.05, 0.35, 0.05}, paras: []para{{align: "ctr", runs: []run{topic}}}}})
	if d.StudentName != "" {
		c.text(rect{1.0, 4.85, 11.33, 0.55}, textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
			{text: d.labels().Student + ": ", size: 16, color: p.muted, font: t.bodyFont},
			{text: d.StudentName, size: 16, bold: true, color: p.text, font: t.bodyFont}}}}})
	}
}

func pastelChrome(t *theme, c *canvas, s slideInfo) rect {
	p := t.pal
	c.bg = solid(p.bg)
	pastelBlobs(t, c, false)
	pill := rect{slideW/2 - 0.62, 0.3, 1.24, 0.38}
	c.add(pill, shape{geom: "roundRect", adj: []int{50000}, fill: solid(p.primary), text: &textBody{
		anchor: "ctr", inset: [4]float64{0.02, 0.01, 0.02, 0.01},
		paras: []para{{align: "ctr", runs: []run{{text: pageLabel(s), size: 10, bold: true, color: "FFFFFF", font: t.bodyFont}}}}}})
	c.text(rect{1.4, 0.74, 10.53, 0.9}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{align: "ctr", runs: []run{t.hd(s.title, 28, p.text)}}}})
	y := 1.8
	if s.lead != "" {
		c.text(rect{1.8, 1.62, 9.73, 0.6}, textBody{anchor: "ctr", fit: true, minFit: 0.7, paras: []para{{align: "ctr", runs: []run{t.tx(s.lead, 14.5, p.muted)}}}})
		y = 2.35
	}
	c.text(rect{2.0, 7.02, 9.33, 0.32}, textBody{anchor: "ctr", fit: true, paras: []para{{align: "ctr", runs: []run{
		{text: s.topic, size: 10, color: p.muted, font: t.bodyFont}}}}})
	return rect{0.8, y, 11.73, 6.85 - y}
}
