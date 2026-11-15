package generator

// layouts_extra.go — qo'shimcha maketlar: vaqt chizig'i, piramida, 2x2
// matritsa, sikl, markaziy tushuncha (hub) va asosiy tezis. Ular ham umumiy
// maketlar kabi mavzu uslubida (kartochka, belgi, ranglar) chiziladi va
// mazmun maydoni shakliga moslashadi: keng maydonda gorizontal, tor va
// baland maydonda (masalan, yon panelli dizaynda) vertikal joylashadi.

import (
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// compactSide — sikl va markaz maketlarida yon ustun bundan tor bo'lsa,
// barcha elementlar bitta ustunga joylanadi (dyuym).
const compactSide = 2.6

// segment (x1,y1) dan (x2,y2) gacha w qalinlikdagi chiziq chizadi. Chiziq
// aylantirilgan ingichka to'rtburchak — barcha ko'ruvchilarda bir xil chiqadi.
func segment(c *canvas, x1, y1, x2, y2, w float64, fill paint) {
	dx, dy := x2-x1, y2-y1
	l := math.Hypot(dx, dy)
	if l < 0.001 {
		return
	}
	cx, cy := (x1+x2)/2, (y1+y2)/2
	c.add(rect{cx - l/2, cy - w/2, l, w}, shape{fill: fill, rot: math.Atan2(dy, dx) * 180 / math.Pi})
}

// stackHeights qatorlar balandligini matn hajmiga qarab hisoblaydi: joy ko'p
// bo'lsa qatorlar haddan tashqari cho'zilmaydi (ko'pi bilan maxRow) va guruh
// balandligi qaytariladi — chaqiruvchi uni vertikal markazlaydi.
func stackHeights(paras [][]para, textW, padY, gap, avail, maxRow float64) ([]float64, float64) {
	n := len(paras)
	need := make([]float64, n)
	for i, ps := range paras {
		need[i] = measureText(ps, textW-defaultInset[0]-defaultInset[2], 1) + defaultInset[1] + defaultInset[3] + 2*padY
	}
	heights := distribute(slices.Clone(need), avail-gap*float64(n-1))
	group := gap * float64(n-1)
	for i := range heights {
		heights[i] = min(heights[i], max(need[i], maxRow))
		group += heights[i]
	}
	return heights, group
}

// valueParas — sana/qiymat (katta, rangli), sarlavha va izoh paragraflari.
func (t *theme) valueParas(it Item, i int, align string, vs, hs, ts float64) []para {
	var ps []para
	if it.Value != "" {
		ps = append(ps, para{align: align, after: 2, runs: []run{{text: it.Value, size: vs, bold: true, color: t.inkColor(i), font: t.headFont}}})
	}
	rest := t.itemParas(Item{Heading: it.Heading, Text: it.Text, Points: it.Points}, hs, ts, align, 3)
	if len(ps) > 0 && len(rest) > 0 {
		rest[0].before = 2
	}
	return append(ps, rest...)
}

// centerLabel sikl va markaz maketlarining markaziy yozuvi (qisqa bo'lsa).
func centerLabel(s *Slide) string {
	if h := strings.TrimSpace(s.Highlight); h != "" && utf8.RuneCountInString(h) <= maxLabelRunes {
		return h
	}
	return ""
}

// ---------------------------------------------------------------------------
// Bosqichlar (vertikal) — tor va baland maydon uchun
// ---------------------------------------------------------------------------

// layoutProcessVertical — bosqichlar ustma-ust: chapda chiziq bilan bog'langan
// raqamli doiralar, o'ngda kartochkalar.
func layoutProcessVertical(t *theme, c *canvas, body rect, items []Item) {
	n := len(items)
	const gap, padY = 0.18, 0.12
	d := min(0.62, (body.H-gap*float64(n-1))/float64(n)*0.75)
	cardX := body.X + d + 0.3
	textW := body.right() - cardX - 0.4

	paras := make([][]para, n)
	for i, it := range items {
		paras[i] = t.itemParas(it, 17, 13.5, "l", 3)
	}
	heights, group := stackHeights(paras, textW, padY, gap, body.H, 1.6)

	y := body.Y + max(0, body.H-group)/2
	centers := make([]float64, n)
	rows := make([]rect, n)
	for i := range items {
		rows[i] = rect{cardX, y, body.right() - cardX, heights[i]}
		centers[i] = y + heights[i]/2
		y += heights[i] + gap
	}
	if n > 1 {
		c.add(rect{body.X + d/2 - 0.02, centers[0], 0.04, centers[n-1] - centers[0]}, shape{fill: linear(90, t.seriesColor(0), t.seriesColor(1))})
	}
	blocks := make([]placed, n)
	for i, r := range rows {
		t.card(c, r, i)
		t.badge(c, rect{body.X, centers[i] - d/2, d, d}, pad2(i+1), i)
		in := t.cardPad(r, 0.22, padY)
		blocks[i] = placed{in, textBody{anchor: "ctr", fit: true, paras: paras[i]}}
	}
	fitTogether(blocks, 1)
	drawAll(c, blocks)
}

// ---------------------------------------------------------------------------
// Vaqt chizig'i
// ---------------------------------------------------------------------------

// layoutTimeline — sanalar bilan xronologiya. Keng maydonda gorizontal o'q va
// navbatma-navbat yuqori/pastki kartochkalar; tor maydonda vertikal o'q.
func layoutTimeline(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxListItems)
	if body.W < narrowBody || body.H > body.W*0.6 {
		timelineVertical(t, c, body, items)
		return
	}
	n := len(items)
	colW := body.W / float64(n)
	axisY := body.Y + body.H/2
	const node, stemGap, padX, padY = 0.44, 0.42, 0.2, 0.16
	cardW := min(1.6*colW-0.1, 3.6)
	halfH := body.H/2 - stemGap
	cx := func(i int) float64 { return body.X + (float64(i)+0.5)*colW }
	cardX := func(i int) float64 { return min(max(cx(i)-cardW/2, body.X), body.right()-cardW) }

	blocks := make([]placed, n)
	for i, it := range items {
		blocks[i] = placed{
			rect{cardX(i) + padX, 0, cardW - 2*padX, halfH - 2*padY - t.barOffset()},
			textBody{fit: true, paras: t.valueParas(it, i, "l", 20, 15.5, 12.5)},
		}
	}
	fitTogether(blocks, growList)
	cardH := min(halfH, max(halfH*0.6, contentHeight(blocks)+2*padY+t.barOffset()))

	c.add(rect{body.X, axisY - 0.025, body.W - 0.12, 0.05}, shape{fill: linear(0, t.seriesColor(0), t.seriesColor(1))})
	c.add(rect{body.right() - 0.2, axisY - 0.12, 0.2, 0.24}, shape{geom: "triangle", rot: 90, fill: solid(t.seriesColor(1))})
	cards := make([]rect, n)
	for i := range items {
		r := rect{cardX(i), axisY + stemGap, cardW, cardH}
		if i%2 == 0 {
			r.Y = axisY - stemGap - cardH
		}
		cards[i] = r
		top, bottom := r.bottom(), axisY
		if i%2 == 1 {
			top, bottom = axisY, r.Y
		}
		c.add(rect{cx(i) - 0.012, top, 0.024, bottom - top}, shape{fill: solid(t.seriesColor(i))})
	}
	for i, r := range cards {
		t.card(c, r, i)
		t.badge(c, rect{cx(i) - node/2, axisY - node/2, node, node}, pad2(i+1), i)
		blocks[i].r = t.cardPad(r, padX, padY)
	}
	drawAll(c, blocks)
}

// timelineVertical — vertikal o'q: chapda sanalar, o'qda raqamli doiralar,
// o'ngda kartochkalar.
func timelineVertical(t *theme, c *canvas, body rect, items []Item) {
	n := len(items)
	const gap, padY = 0.16, 0.1
	d := min(0.46, (body.H-gap*float64(n-1))/float64(n)*0.6)
	dateW := 0.0
	for _, it := range items {
		if it.Value != "" {
			dateW = min(1.7, body.W*0.22)
			break
		}
	}
	axisX := body.X + dateW + 0.2 + d/2
	if dateW == 0 {
		axisX = body.X + d/2
	}
	cardX := axisX + d/2 + 0.3
	textW := body.right() - cardX - 0.4

	paras := make([][]para, n)
	for i, it := range items {
		paras[i] = t.itemParas(Item{Heading: it.Heading, Text: it.Text, Points: it.Points}, 16.5, 13, "l", 3)
	}
	heights, group := stackHeights(paras, textW, padY, gap, body.H, 1.5)
	y := body.Y + max(0, body.H-group)/2
	rows := make([]rect, n)
	for i := range items {
		rows[i] = rect{cardX, y, body.right() - cardX, heights[i]}
		y += heights[i] + gap
	}
	first, last := rows[0].Y+rows[0].H/2, rows[n-1].Y+rows[n-1].H/2
	c.add(rect{axisX - 0.02, first, 0.04, last - first}, shape{fill: linear(90, t.seriesColor(0), t.seriesColor(1))})

	blocks := make([]placed, 0, n)
	dates := make([]placed, 0, n)
	for i, r := range rows {
		cy := r.Y + r.H/2
		t.card(c, r, i)
		t.badge(c, rect{axisX - d/2, cy - d/2, d, d}, pad2(i+1), i)
		if dateW > 0 && items[i].Value != "" {
			dates = append(dates, placed{rect{body.X, r.Y, dateW, r.H}, textBody{anchor: "ctr", fit: true, minFit: 0.5, paras: []para{{align: "r", runs: []run{
				{text: items[i].Value, size: 20, bold: true, color: t.inkColor(i), font: t.headFont}}}}}})
		}
		blocks = append(blocks, placed{t.cardPad(r, 0.22, padY), textBody{anchor: "ctr", fit: true, paras: paras[i]}})
	}
	fitTogether(dates, 1)
	fitTogether(blocks, 1)
	drawAll(c, dates)
	drawAll(c, blocks)
}

// ---------------------------------------------------------------------------
// Piramida
// ---------------------------------------------------------------------------

// layoutPyramid — ierarxiya: chapda yuqoridan pastga kengayib boruvchi
// darajalar (eng yuqori daraja — uchburchak), o'ngda har bir daraja izohi.
func layoutPyramid(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxLevelItems)
	n := len(items)
	const gap = 0.08
	base := min(body.W*0.42, body.H*1.2)
	lh := (body.H - gap*float64(n-1)) / float64(n)
	mid := body.X + base/2
	textX := body.X + base + 0.55
	textW := body.right() - textX

	blocks := make([]placed, n)
	for i, it := range items {
		y0 := body.Y + float64(i)*(lh+gap)
		w0 := base * (y0 - body.Y) / body.H
		w1 := base * (y0 + lh - body.Y) / body.H
		r := rect{mid - w1/2, y0, w1, lh}
		col := t.seriesColor(i)
		sh := shape{fill: solid(col)}
		if w0 < 0.05 {
			sh.geom = "triangle"
		} else {
			sh.geom, sh.adj = "trapezoid", []int{int((w1 - w0) / 2 / math.Min(w1, lh) * 100000)}
		}
		label := t.pal.onSeries
		switch {
		case t.cards.dash != "" || t.cards.ticks:
			sh.fill, sh.line, label = solidA(col, 16), &stroke{color: col, width: 1.5, dash: t.cards.dash}, col
		case t.cards.hard:
			sh.line = &stroke{color: t.pal.line, width: max(t.cards.borderW, 1)}
		}
		if t.neon {
			sh.glow = &glow{radius: 6, color: col, alpha: 30}
		}
		t.hardShadow(c, r, sh)
		c.add(r, sh)
		lr := r
		if sh.geom == "triangle" {
			lr = rect{r.X, r.Y + lh*0.4, r.W, lh * 0.6}
		}
		c.text(lr, textBody{anchor: "ctr", inset: [4]float64{0.01, 0.01, 0.01, 0.01}, paras: []para{{align: "ctr", runs: []run{
			{text: pad2(i + 1), size: min(22, lh*72*0.32), bold: true, color: label, font: t.headFont}}}}})

		cy := y0 + lh/2
		edge := mid + (w0+w1)/4 + 0.12
		segment(c, edge, cy, textX-0.12, cy, 0.016, solid(t.pal.line))
		c.add(rect{textX - 0.17, cy - 0.05, 0.1, 0.1}, shape{geom: "ellipse", fill: solid(col)})
		blocks[i] = placed{rect{textX, y0 - 0.04, textW, lh + 0.08}, textBody{anchor: "ctr", fit: true,
			paras: t.itemParasC(it, 17, 13.5, "l", 2, t.inkColor(i), t.pal.muted)}}
	}
	fitTogether(blocks, growList)
	drawAll(c, blocks)
}

// ---------------------------------------------------------------------------
// 2x2 matritsa
// ---------------------------------------------------------------------------

// layoutMatrix — to'rt kvadrant (masalan, SWOT): har birida katta xira bosh
// harf, rangli sarlavha va izoh; markazda (bo'lsa) qisqa yozuvli doira.
func layoutMatrix(t *theme, c *canvas, body rect, s *Slide) {
	items := s.Items[:4]
	const gap = 0.2
	cells := body.grid(2, 2, gap, gap)
	label := centerLabel(s)
	cr := 0.0
	if label != "" {
		cr = min(0.85, body.H*0.19)
	}

	blocks := make([]placed, 4)
	for i, it := range items {
		r := cells[i]
		t.card(c, r, i)
		if letter := firstLetter(it.Heading); letter != "" {
			lr := rect{r.X + 0.15, r.Y + r.H*0.3, r.W - 0.3, r.H * 0.68}
			align := "l" // tashqi burchakda — markaziy doiradan uzoqda
			if i%2 == 1 {
				align = "r"
			}
			c.text(lr, textBody{anchor: "b", inset: [4]float64{0.02, 0, 0.05, 0}, paras: []para{{align: align, runs: []run{
				{text: letter, size: min(100, r.H*72*0.55), bold: true, color: mixHex(t.seriesColor(i), t.cardBg(i), 0.86), font: t.headFont}}}}})
		}
		in := t.cardPad(r, 0.3, 0.22)
		if cr > 0 {
			in.W -= cr * 0.7
			if i%2 == 1 {
				in.X += cr * 0.7
			}
		}
		blocks[i] = placed{in, textBody{fit: true, paras: t.itemParasC(it, 18, 13.5, "l", 5, t.inkColor(i), t.pal.muted)}}
	}
	fitTogether(blocks, growCards)
	drawAll(c, blocks)

	if cr > 0 {
		cx, cy := body.X+body.W/2, body.Y+body.H/2
		s := t.panelShape(rect{})
		s.geom, s.adj, s.shadow = "ellipse", nil, nil
		s.line = &stroke{color: t.pal.bg, width: 5}
		s.text = &textBody{anchor: "ctr", fit: true, minFit: 0.5, inset: [4]float64{0.12, 0.08, 0.12, 0.08}, paras: []para{{align: "ctr", runs: []run{
			{text: label, size: 15, bold: true, color: t.pal.onPanel, font: t.headFont}}}}}
		c.add(rect{cx - cr, cy - cr, 2 * cr, 2 * cr}, s)
	}
}

// firstLetter matnning birinchi harfini (katta) qaytaradi.
func firstLetter(s string) string {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return string(unicode.ToUpper(r))
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Sikl
// ---------------------------------------------------------------------------

// sideColumns elementlarni ikki ustunga taqsimlaydi. clockwise bo'lsa —
// o'ngda yuqoridan pastga, so'ng chapda pastdan yuqoriga (aylana tartibi,
// sikl uchun); aks holda oddiy o'qish tartibida: avval chap ustun, keyin o'ng.
// Har bir element uchun qator maydoni va tomoni (+1 o'ng, -1 chap) qaytariladi.
func sideColumns(n int, left, right rect, gap float64, clockwise bool) ([]rect, []float64) {
	first := (n + 1) / 2
	second := n - first
	cells := make([]rect, n)
	sides := make([]float64, n)
	if clockwise {
		rr := right.rows(first, gap)
		lr := left.rows(max(second, 1), gap)
		for i := range n {
			if i < first {
				cells[i], sides[i] = rr[i], 1
			} else {
				cells[i], sides[i] = lr[second-1-(i-first)], -1
			}
		}
		return cells, sides
	}
	lr := left.rows(first, gap)
	rr := right.rows(max(second, 1), gap)
	for i := range n {
		if i < first {
			cells[i], sides[i] = lr[i], -1
		} else {
			cells[i], sides[i] = rr[i-first], 1
		}
	}
	return cells, sides
}

// layoutCycle — takrorlanuvchi bosqichlar: halqa ustida raqamli doiralar va
// yo'nalish o'qlari, bosqichlar izohi halqaning ikki yonida.
func layoutCycle(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxListItems)
	n := len(items)
	const nodeD, gapX = 0.56, 0.45
	R := min(body.H*0.36, 1.75)
	sideW := (body.W - 2*R - nodeD - 2*gapX) / 2
	cx, cy := body.X+body.W/2, body.Y+body.H/2

	var (
		cells []rect
		sides []float64
		nodes = make([][2]float64, n)
	)
	compact := sideW < compactSide
	if compact {
		// Tor maydon: halqa chapda, barcha bosqichlar o'ngda bitta ustunda.
		R = min(body.H*0.32, body.W*0.2)
		cx = body.X + R + nodeD/2 + 0.05
		col := rect{cx + R + nodeD/2 + gapX, body.Y, body.right() - (cx + R + nodeD/2 + gapX), body.H}
		cells = col.rows(n, 0.14)
		for i := range n {
			a := -math.Pi/2 + 2*math.Pi*float64(i)/float64(n)
			nodes[i] = [2]float64{cx + R*math.Cos(a), cy + R*math.Sin(a)}
		}
	} else {
		right := rect{cx + R + nodeD/2 + gapX, body.Y, sideW, body.H}
		left := rect{body.X, body.Y, sideW, body.H}
		cells, sides = sideColumns(n, left, right, 0.15, true)
		for i, r := range cells {
			v := min(0.9, max(-0.9, (r.Y+r.H/2-cy)/R))
			nodes[i] = [2]float64{cx + sides[i]*R*math.Sqrt(1-v*v), cy + R*v}
		}
	}

	ringColor := t.pal.line
	ring(c, cx, cy, R, ringColor, 2.5, 100, t.cards.dash)
	angle := func(i int) float64 { return math.Atan2(nodes[i][1]-cy, nodes[i][0]-cx) }
	for i := range n {
		a0, a1 := angle(i), angle((i+1)%n)
		for a1 <= a0 {
			a1 += 2 * math.Pi
		}
		m := (a0 + a1) / 2
		ax, ay := cx+R*math.Cos(m), cy+R*math.Sin(m)
		c.add(rect{ax - 0.11, ay - 0.11, 0.22, 0.22}, shape{geom: "triangle", rot: m*180/math.Pi + 180, fill: solid(t.seriesColor(i))})
	}
	if label := centerLabel(s); label != "" {
		ir := R * 0.58
		sh := t.panelShape(rect{})
		sh.geom, sh.adj, sh.shadow = "ellipse", nil, nil
		sh.text = &textBody{anchor: "ctr", fit: true, minFit: 0.5, inset: [4]float64{0.1, 0.06, 0.1, 0.06}, paras: []para{{align: "ctr", runs: []run{
			{text: label, size: 16, bold: true, color: t.pal.onPanel, font: t.headFont}}}}}
		c.add(rect{cx - ir, cy - ir, 2 * ir, 2 * ir}, sh)
	}

	blocks := make([]placed, n)
	for i, it := range items {
		r := cells[i]
		align := "l"
		if !compact && sides[i] < 0 {
			align = "r"
		}
		if !compact {
			ex := r.X
			if sides[i] < 0 {
				ex = r.right()
			}
			segment(c, nodes[i][0], nodes[i][1], ex, r.Y+r.H/2, 0.016, solid(t.pal.line))
		}
		t.badge(c, rect{nodes[i][0] - nodeD/2, nodes[i][1] - nodeD/2, nodeD, nodeD}, pad2(i+1), i)
		tr := r
		if compact {
			mini := min(0.4, r.H*0.6)
			t.badge(c, rect{r.X, r.Y + r.H/2 - mini/2, mini, mini}, pad2(i+1), i)
			tr = rect{r.X + mini + 0.1, r.Y, r.W - mini - 0.1, r.H}
		}
		blocks[i] = placed{tr, textBody{anchor: "ctr", fit: true, paras: t.itemParasC(it, 16.5, 13, align, 2, t.inkColor(i), t.pal.muted)}}
	}
	fitTogether(blocks, growList)
	drawAll(c, blocks)
}

// ---------------------------------------------------------------------------
// Markaziy tushuncha (hub)
// ---------------------------------------------------------------------------

// layoutHub — markazdagi katta doirada asosiy tushuncha, atrofida chiziqlar
// bilan bog'langan tarkibiy qismlar kartochkalari.
func layoutHub(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxListItems)
	n := len(items)
	const gapX = 0.8
	D := min(body.H*0.5, 2.5)
	sideW := (body.W - D - 2*gapX) / 2
	cx, cy := body.X+body.W/2, body.Y+body.H/2

	var cells []rect
	var sides []float64
	if sideW < compactSide {
		D = min(D, body.W*0.3)
		cx = body.X + D/2 + 0.1
		x := cx + D/2 + gapX
		cells = rect{x, body.Y, body.right() - x, body.H}.rows(n, 0.16)
		sides = make([]float64, n)
		for i := range sides {
			sides[i] = 1
		}
	} else {
		cells, sides = sideColumns(n,
			rect{body.X, body.Y, sideW, body.H},
			rect{cx + D/2 + gapX, body.Y, sideW, body.H}, 0.2, false)
	}

	// Avval chiziqlar — kartochkalar va markaz ularning uchini yopadi.
	for i, r := range cells {
		ex, ey := r.X, r.Y+r.H/2
		if sides[i] < 0 {
			ex = r.right()
		}
		a := math.Atan2(ey-cy, ex-cx)
		segment(c, cx+D/2*math.Cos(a), cy+D/2*math.Sin(a), ex, ey, 0.022, solid(t.seriesColor(i)))
		c.add(rect{ex - 0.07, ey - 0.07, 0.14, 0.14}, shape{geom: "ellipse", fill: solid(t.seriesColor(i))})
	}

	blocks := make([]placed, n)
	for i, it := range items {
		r := cells[i]
		t.card(c, r, i)
		blocks[i] = placed{t.cardPad(r, 0.24, 0.12), textBody{anchor: "ctr", fit: true,
			paras: t.itemParasC(it, 16, 12.5, "l", 2, t.inkColor(i), t.pal.muted)}}
	}
	fitTogether(blocks, growList)
	drawAll(c, blocks)

	label := centerLabel(s)
	if label == "" {
		label = s.Title
	}
	ring(c, cx, cy, D/2+0.14, t.seriesColor(0), 1.25, 45, t.cards.dash)
	sh := t.panelShape(rect{})
	sh.geom, sh.adj = "ellipse", nil
	sh.text = &textBody{anchor: "ctr", fit: true, minFit: 0.45, inset: [4]float64{0.18, 0.1, 0.18, 0.1}, paras: []para{{align: "ctr", runs: []run{
		{text: label, size: 20, bold: true, color: t.pal.onPanel, font: t.headFont}}}}}
	t.hardShadow(c, rect{cx - D/2, cy - D/2, D, D}, sh)
	c.add(rect{cx - D/2, cy - D/2, D, D}, sh)
}

// ---------------------------------------------------------------------------
// Asosiy tezis
// ---------------------------------------------------------------------------

// layoutStatement — bitta asosiy fikr katta matnda, ostida (bo'lsa) uni
// asoslovchi 1-3 ta qisqa dalil ustunlarda.
func layoutStatement(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, 3)
	st := body
	var below rect
	if len(items) > 0 {
		st, below = body.splitY(body.H*0.56, 0.3)
	}
	c.add(rect{st.X, st.Y + 0.12, 0.1, st.H - 0.24}, shape{fill: linear(90, t.pal.accent, t.seriesColor(1))})
	c.text(rect{st.X + 0.4, st.Y, st.W - 0.5, st.H}, textBody{anchor: "ctr", fit: true, minFit: 0.45, paras: []para{{lineSp: 1.02, runs: []run{
		{text: s.Highlight, size: 30, bold: t.headBold, color: t.pal.text, font: t.headFont}}}}})
	if len(items) == 0 {
		return
	}
	cols := below.cols(len(items), 0.45)
	blocks := make([]placed, len(items))
	for i, it := range items {
		r := cols[i]
		c.add(rect{r.X, r.Y, min(0.8, r.W), 0.05}, shape{fill: solid(t.seriesColor(i))})
		blocks[i] = placed{rect{r.X - 0.08, r.Y + 0.14, r.W + 0.08, r.H - 0.14}, textBody{fit: true, paras: t.itemParas(it, 16, 13, "l", 3)}}
	}
	fitTogether(blocks, growList)
	drawAll(c, blocks)
}
