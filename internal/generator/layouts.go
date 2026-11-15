package generator

// layouts.go — mazmunli slaydlar maketlari. AI har bir slayd uchun maketni
// tanlaydi; bu yerda u mavzu palitrasi va kartochka uslubida chiziladi.
// Maket uchun ma'lumot yetarli bo'lmasa (masalan, "facts" da raqamlar yo'q),
// normalizeSlide uni mosrog'iga almashtiradi. Qo'shimcha maketlar (vaqt
// chizig'i, piramida, matritsa, sikl, markaz, tezis) — layouts_extra.go da.

import (
	"math"
	"unicode/utf8"
)

// Maket nomlari (AI javobidagi "layout" maydoni qiymatlari).
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

// layoutFunc — mazmunli slayd maketini chizuvchi funksiya.
type layoutFunc func(t *theme, c *canvas, body rect, s *Slide)

// narrowBody — bundan tor mazmun maydoni (yonida rasm bor) uchun maketlar
// ixchamroq joylashadi (dyuym).
const narrowBody = 9.0

// Maketlardagi elementlar soni chegaralari.
const (
	maxAgendaItems = 8
	maxListItems   = 6
	maxFactItems   = 4
	maxValueRunes  = 14
	maxLevelItems  = 5  // piramida darajalari
	maxLabelRunes  = 40 // sikl va markaz maketlaridagi markaziy yozuv
)

// normalizeSlide maket uchun ma'lumot yetarliligini tekshiradi va kerak
// bo'lsa boshqa maketga almashtiradi.
func normalizeSlide(s Slide) Slide {
	items := s.Items[:0:0]
	for _, it := range s.Items {
		if it.Heading != "" || it.Text != "" || len(it.Points) > 0 || it.Value != "" {
			items = append(items, it)
		}
	}
	s.Items = items

	n := len(s.Items)
	switch s.Layout {
	case LayoutAgenda, LayoutBullets, LayoutCards, LayoutProcess, LayoutConclusion:
	case LayoutComparison:
		if n < 2 {
			s.Layout = LayoutBullets
		}
	case LayoutFacts:
		withValue := 0
		for _, it := range s.Items {
			if it.Value != "" && utf8.RuneCountInString(it.Value) <= maxValueRunes {
				withValue++
			}
		}
		if withValue < n || n < 2 {
			s.Layout = LayoutCards
		}
	case LayoutDefinition:
		if s.Highlight == "" {
			s.Layout = LayoutBullets
		}
	case LayoutTimeline:
		if n < 3 {
			s.Layout = LayoutProcess
		}
	case LayoutPyramid:
		if n < 3 || n > maxLevelItems {
			s.Layout = LayoutCards
		}
	case LayoutMatrix:
		if n != 4 {
			s.Layout = LayoutCards
		}
	case LayoutCycle, LayoutHub:
		if n < 3 {
			s.Layout = LayoutCards
		}
	case LayoutStatement:
		if s.Highlight == "" {
			s.Layout = LayoutBullets
		}
	default:
		s.Layout = LayoutBullets
	}

	if s.Layout == LayoutCards && n < 2 || s.Layout == LayoutProcess && n < 2 {
		s.Layout = LayoutBullets
	}
	if s.Layout == LayoutBullets && n == 0 && s.Highlight != "" {
		s.Layout = LayoutDefinition
	}
	return s
}

func renderContent(t *theme, c *canvas, body rect, s *Slide) {
	if f, ok := t.layouts[s.Layout]; ok {
		f(t, c, body, s)
		return
	}
	switch s.Layout {
	case LayoutAgenda:
		layoutAgenda(t, c, body, s)
	case LayoutCards:
		layoutCards(t, c, body, s)
	case LayoutComparison:
		layoutComparison(t, c, body, s)
	case LayoutProcess:
		layoutProcess(t, c, body, s)
	case LayoutFacts:
		layoutFacts(t, c, body, s)
	case LayoutDefinition:
		layoutDefinition(t, c, body, s)
	case LayoutConclusion:
		layoutConclusion(t, c, body, s)
	case LayoutTimeline:
		layoutTimeline(t, c, body, s)
	case LayoutPyramid:
		layoutPyramid(t, c, body, s)
	case LayoutMatrix:
		layoutMatrix(t, c, body, s)
	case LayoutCycle:
		layoutCycle(t, c, body, s)
	case LayoutHub:
		layoutHub(t, c, body, s)
	case LayoutStatement:
		layoutStatement(t, c, body, s)
	default:
		layoutBullets(t, c, body, s)
	}
}

// ---------------------------------------------------------------------------
// Yordamchilar
// ---------------------------------------------------------------------------

// placed — kanvaga qo'yilishi kutilayotgan matn bloki.
type placed struct {
	r  rect
	tb textBody
}

// Bo'sh joy ko'p bo'lsa matn kattalashtiriladigan eng katta koeffitsiyent.
const (
	growCards = 1.3
	growList  = 1.2
)

// fitTogether bloklarni bir xil masshtabda sig'diradi — bir darajadagi
// matnlar (masalan, barcha kartochkalar) bir xil o'lchamda bo'lishi uchun.
// grow > 1 bo'lsa, joy yetarli bo'lganda matn shu martagacha kattalashadi.
func fitTogether(blocks []placed, grow float64) {
	k := max(grow, 1)
	for _, b := range blocks {
		k = min(k, fitScale(b.tb.paras, b.r, b.tb.insets(), b.tb.minFit, grow))
	}
	for i := range blocks {
		scaleParas(blocks[i].tb.paras, k)
	}
}

// contentHeight bloklar ichidagi eng baland matnning balandligi (dyuym, ichki chekinishlar bilan).
func contentHeight(blocks []placed) float64 {
	h := 0.0
	for _, b := range blocks {
		ins := b.tb.insets()
		h = max(h, measureText(b.tb.paras, b.r.W-ins[0]-ins[2], 1)+ins[1]+ins[3])
	}
	return h
}

// shiftY bloklarni dy ga suradi va balandligini h ga o'zgartiradi (h <= 0 — o'zgarmaydi).
func shiftY(blocks []placed, dy, h float64) {
	for i := range blocks {
		blocks[i].r.Y += dy
		if h > 0 {
			blocks[i].r.H = h
		}
	}
}

func drawAll(c *canvas, blocks []placed) {
	for _, b := range blocks {
		c.text(b.r, b.tb)
	}
}

func capItems(items []Item, n int) []Item {
	if len(items) > n {
		return items[:n]
	}
	return items
}

// distribute qatorlar balandligini matn hajmiga mutanosib taqsimlaydi.
func distribute(need []float64, avail float64) []float64 {
	out := make([]float64, len(need))
	sum := 0.0
	for i, v := range need {
		if math.IsInf(v, 0) || math.IsNaN(v) {
			need[i] = avail // so'z sig'maydigan holat — fitTogether keyin kichraytiradi
		}
		sum += need[i]
	}
	if sum <= 0 {
		for i := range out {
			out[i] = avail / float64(len(out))
		}
		return out
	}
	if sum <= avail {
		extra := (avail - sum) / float64(len(need))
		for i, v := range need {
			out[i] = v + extra
		}
		return out
	}
	for i, v := range need {
		out[i] = v * avail / sum
	}
	return out
}

// itemParas — element sarlavhasi va izohi paragraflari.
func (t *theme) itemParas(it Item, hs, ts float64, align string, gap float64) []para {
	return t.itemParasC(it, hs, ts, align, gap, t.pal.text, t.pal.muted)
}

// itemParasC — itemParas berilgan ranglar bilan (rangli fon ustidagi matn uchun).
func (t *theme) itemParasC(it Item, hs, ts float64, align string, gap float64, headColor, textColor string) []para {
	var ps []para
	if it.Heading != "" {
		ps = append(ps, para{align: align, runs: []run{{text: it.Heading, size: hs, bold: true, color: headColor, font: t.headFont}}})
	}
	text := it.Text
	if text == "" && len(it.Points) > 0 {
		for i, p := range it.Points {
			if i > 0 {
				text += "; "
			}
			text += p
		}
	}
	if text != "" {
		p := para{align: align, lineSp: 1.05, runs: []run{t.tx(text, ts, textColor)}}
		if len(ps) > 0 {
			p.before = gap
		}
		ps = append(ps, p)
	}
	return ps
}

// marker — ro'yxat elementi belgisi (mavzuga xos shakl).
func (t *theme) marker(c *canvas, cx, cy float64, i int) {
	col := t.seriesColor(i)
	switch t.ID {
	case ThemeAcademic:
		m := 0.15
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{fill: solid(col), rot: 45})
	case ThemeMinimal:
		c.add(rect{cx - 0.13, cy - 0.016, 0.26, 0.032}, shape{fill: solid(t.pal.accent)})
	case ThemeTech:
		m := 0.2
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{geom: "diamond", fill: solid(col), glow: &glow{radius: 5, color: col, alpha: 50}})
	case ThemeJournal:
		m := 0.14
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{fill: solid(col)})
	case ThemeNature:
		m := 0.18
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{geom: "ellipse", fill: solid(col)})
	case ThemeOcean:
		m := 0.18
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{geom: "roundRect", adj: []int{25000}, fill: solid(col)})
	case ThemeSunset:
		m := 0.18
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{geom: "ellipse", fill: solid(col), glow: &glow{radius: 5, color: col, alpha: 45}})
	case ThemeStrategy:
		m := 0.16
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{fill: solid(col)})
	case ThemeRetro:
		m := 0.24
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{geom: "ellipse", fill: solid(col), line: &stroke{color: t.pal.line, width: 1.5}})
	case ThemeTypo:
		c.text(rect{cx - 0.3, cy - 0.25, 0.6, 0.5}, textBody{anchor: "ctr", inset: [4]float64{0.01, 0, 0.01, 0}, paras: []para{{align: "ctr", runs: []run{
			{text: pad2(i + 1), size: 17, bold: true, color: t.inkColor(i), font: t.headFont}}}}})
	case ThemeBento:
		m := 0.22
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{geom: "roundRect", adj: []int{30000}, fill: solid(col)})
	case ThemeBoard:
		ring(c, cx, cy, 0.11, col, 1.5, 100, "")
	case ThemeBlueprint:
		m := 0.18
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{line: &stroke{color: col, width: 1}})
		c.add(rect{cx - 0.035, cy - 0.035, 0.07, 0.07}, shape{fill: solid(col)})
	case ThemePastel:
		m := 0.2
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{geom: "ellipse", fill: solid(col)})
	case ThemePanorama:
		c.add(rect{cx - 0.08, cy - 0.11, 0.2, 0.22}, shape{geom: "triangle", rot: 90, fill: solid(col)})
	default:
		m := 0.2
		c.add(rect{cx - m/2, cy - m/2, m, m}, shape{geom: "ellipse", fill: linear(45, col, t.pal.secondary)})
	}
}

// iconTile — och tusli kvadrat ichida raqam.
func (t *theme) iconTile(c *canvas, r rect, label string, i int) {
	col := t.seriesColor(i)
	alpha := 14
	if t.dark {
		alpha = 20
	}
	s := shape{geom: "roundRect", adj: []int{22000}, fill: solidA(col, alpha)}
	txt := t.inkColor(i)
	switch {
	case t.badgePlain:
		s.fill = paint{}
	case t.cards.hard:
		s.fill, s.line = solid(t.tint(i)), &stroke{color: t.pal.line, width: max(t.cards.borderW, 1)}
	case t.cards.dash != "" || t.cards.ticks:
		s.fill, s.line = paint{}, &stroke{color: col, width: 1.25, dash: t.cards.dash}
		txt = col
	}
	s.text = &textBody{anchor: "ctr", inset: [4]float64{0.01, 0.01, 0.01, 0.01},
		paras: []para{{align: "ctr", runs: []run{{text: label, size: r.H * 72 * 0.38, bold: true, color: txt, font: t.headFont}}}}}
	c.add(r, s)
}

// quoteMark — katta qo'shtirnoq belgisi.
func quoteMark(c *canvas, r rect, size float64, color string, alpha int) {
	c.text(r, textBody{inset: [4]float64{0.02, 0, 0.02, 0}, paras: []para{{runs: []run{
		{text: "“", size: size, bold: true, color: color, alpha: alpha, font: "Georgia"}}}}})
}

// highlightPanel — asosiy fikr paneli (qo'shtirnoq bilan).
func (t *theme) highlightPanel(c *canvas, r rect, text string) {
	t.panel(c, r)
	quoteMark(c, rect{r.X + 0.28, r.Y + 0.12, 1.3, 1.1}, 80, t.pal.onPanel, 35)
	c.text(rect{r.X + 0.4, r.Y + 1.0, r.W - 0.8, r.H - 1.55}, textBody{fit: true, minFit: 0.6, paras: []para{{lineSp: 1.05, runs: []run{
		{text: text, size: 19, italic: t.quoteItalic, color: t.pal.onPanel, font: t.headFont}}}}})
	c.add(rect{r.X + 0.5, r.bottom() - 0.42, 0.75, 0.05}, shape{fill: solidA(t.pal.onPanel, 70)})
}

// noteStrip — slayd pastidagi qisqa xulosa qatori.
func (t *theme) noteStrip(c *canvas, r rect, text string) {
	s := shape{fill: solid(t.pal.surface2)}
	if t.cards.radius > 0 {
		s.geom, s.adj = "roundRect", roundAdj(r, t.cards.radius)
	}
	switch {
	case t.cards.hard || t.cards.dash != "" || t.cards.ticks:
		s.line = &stroke{color: t.pal.line, width: max(t.cards.borderW, 1), dash: t.cards.dash}
	case t.dark:
		s.line = &stroke{color: t.pal.line, width: 1}
	}
	t.hardShadow(c, r, s)
	c.add(r, s)
	quoteMark(c, rect{r.X + 0.25, r.Y + 0.02, 0.7, r.H}, 44, t.pal.accent, 100)
	c.text(rect{r.X + 0.85, r.Y + 0.04, r.W - 1.1, r.H - 0.08}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{
		{text: text, size: 15.5, italic: t.quoteItalic, color: t.pal.text, font: t.bodyFont}}}}})
}

// barOffset — kartochka yuqorisidagi rangli chiziq egallagan joy.
func (t *theme) barOffset() float64 {
	if t.cards.topBar || t.cards.tab || t.cards.rule {
		return 0.04
	}
	return 0
}

// ---------------------------------------------------------------------------
// Maketlar
//
// Umumiy tamoyil: avval matn bloklari to'liq maydon bo'yicha sig'diriladi
// (joy ko'p bo'lsa — kattalashtiriladi), so'ng kartochkalar matn hajmiga
// moslab qisqartiriladi va guruh vertikal markazlanadi. Shunda qisqa matnli
// slaydlarda ham kartochkalar bo'm-bo'sh ko'rinmaydi.
// ---------------------------------------------------------------------------

// layoutAgenda — reja: raqamlangan bo'limlar (4 tadan ko'p bo'lsa ikki ustunda).
func layoutAgenda(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxAgendaItems)
	n := len(items)
	if n == 0 {
		return
	}
	cols := 1
	if n >= 4 {
		cols = 2
	}
	rows := (n + cols - 1) / cols
	const gapY, numW = 0.2, 1.05
	rowH := min(1.55, (body.H-gapY*float64(rows-1))/float64(rows))
	groupH := rowH*float64(rows) + gapY*float64(rows-1)
	cells := rect{body.X, body.Y + (body.H-groupH)/2, body.W, groupH}.grid(cols, rows, 0.35, gapY)

	blocks := make([]placed, 0, n)
	for i, it := range items {
		r := cells[i]
		t.card(c, r, i)
		in := t.cardPad(r, 0.1, 0.08)
		c.text(rect{in.X, in.Y, numW, in.H}, textBody{anchor: "ctr", paras: []para{{align: "ctr", runs: []run{
			{text: pad2(i + 1), size: min(32, r.H*72*0.42), bold: true, color: t.inkColor(i), font: t.headFont}}}}})
		c.add(rect{in.X + numW + 0.05, r.Y + 0.22, 0.012, r.H - 0.44}, shape{fill: solid(t.pal.line)})
		blocks = append(blocks, placed{
			rect{in.X + numW + 0.15, in.Y, in.W - numW - 0.2, in.H},
			textBody{anchor: "ctr", fit: true, paras: t.itemParas(it, 19, 13.5, "l", 3)},
		})
	}
	fitTogether(blocks, growList)
	drawAll(c, blocks)
}

// layoutBullets — sarlavha va izohli punktlar kartochka-qatorlarda;
// asosiy fikr (highlight) bo'lsa — o'ng tomonda alohida panel.
func layoutBullets(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxListItems)
	area := body
	if s.Highlight != "" && len(items) > 0 && body.W < narrowBody {
		// Tor maydonda (yonida rasm bor) asosiy fikr pastki qatorga tushadi.
		const stripH, stripGap = 0.82, 0.2
		var strip rect
		area, strip = body.splitY(body.H-stripH-stripGap, stripGap)
		t.noteStrip(c, strip, s.Highlight)
	} else if s.Highlight != "" {
		var side rect
		area, side = body.splitX(body.W*0.62, 0.35)
		t.highlightPanel(c, side, s.Highlight)
	}
	n := len(items)
	if n == 0 {
		return
	}
	hs, ts := 18.0, 14.5
	if n <= 3 {
		hs, ts = 20, 16
	}
	const gap, padY, textX = 0.16, 0.12, 0.6
	textW := area.W - textX - 0.2
	avail := area.H - gap*float64(n-1)

	paras := make([][]para, n)
	for i, it := range items {
		paras[i] = t.itemParas(it, hs, ts, "l", 3)
	}
	needs := func(k float64) ([]float64, float64) {
		out, sum := make([]float64, n), 0.0
		for i, ps := range paras {
			out[i] = measureText(ps, textW-defaultInset[0]-defaultInset[2], k) + defaultInset[1] + defaultInset[3] + 2*padY
			sum += out[i]
		}
		return out, sum
	}
	// Joy ko'p bo'lsa matn biroz kattalashtiriladi.
	for g := growList; g > 1; g -= 0.05 {
		if _, sum := needs(g); sum <= avail {
			for i := range paras {
				scaleParas(paras[i], g)
			}
			break
		}
	}
	need, _ := needs(1)
	heights := distribute(need, avail)
	// Punktlar kam bo'lsa qatorlar haddan tashqari baland bo'lmasin — guruh markazlanadi.
	groupH := gap * float64(n-1)
	for i := range heights {
		heights[i] = min(heights[i], max(need[i], 1.75))
		groupH += heights[i]
	}

	blocks := make([]placed, n)
	y := area.Y + max(0, area.H-groupH)/2
	for i := range items {
		r := rect{area.X, y, area.W, heights[i]}
		t.card(c, r, i)
		t.marker(c, r.X+0.32, r.Y+r.H/2, i)
		blocks[i] = placed{rect{r.X + textX, r.Y + padY - 0.05, textW, r.H - 2*padY + 0.1}, textBody{anchor: "ctr", fit: true, paras: paras[i]}}
		y += heights[i] + gap
	}
	fitTogether(blocks, 1)
	drawAll(c, blocks)
}

// layoutCards — 2-4 ta kartochka bir qatorda yoki 5-6 tasi ikki qatorda
// (tor maydonda 4 tasi 2x2 to'rda).
func layoutCards(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxListItems)
	n := len(items)
	if n > 4 || n == 4 && body.W < narrowBody {
		layoutCardGrid(t, c, body, items)
		return
	}
	const padX, padY, d, gapB = 0.25, 0.24, 0.62, 0.14
	cols := body.cols(n, 0.3)
	textArea := func(r rect) rect {
		in := t.cardPad(r, padX, padY)
		return rect{in.X - 0.05, in.Y + d + gapB, in.W + 0.1, in.H - d - gapB}
	}

	blocks := make([]placed, n)
	for i, it := range items {
		blocks[i] = placed{textArea(cols[i]), textBody{fit: true, paras: t.itemParas(it, 17, 13.5, "l", 6)}}
	}
	fitTogether(blocks, growCards)
	cardH := min(body.H, max(body.H*0.6, contentHeight(blocks)+d+gapB+2*padY+t.barOffset()))
	dy := (body.H - cardH) / 2

	for i := range items {
		r := rect{cols[i].X, body.Y + dy, cols[i].W, cardH}
		t.card(c, r, i)
		in := t.cardPad(r, padX, padY)
		t.badge(c, rect{in.X, in.Y, d, d}, pad2(i+1), i)
		blocks[i].r = textArea(r)
	}
	drawAll(c, blocks)
}

// layoutCardGrid — 5-6 ta kartochka 3x2 yoki 4 tasi 2x2 to'rda.
func layoutCardGrid(t *theme, c *canvas, body rect, items []Item) {
	cols := 3
	if len(items) <= 4 {
		cols = 2
	}
	cells := body.grid(cols, (len(items)+cols-1)/cols, 0.3, 0.28)
	const d = 0.48
	blocks := make([]placed, 0, len(items))
	for i, it := range items {
		r := cells[i]
		t.card(c, r, i)
		in := t.cardPad(r, 0.25, 0.22)
		t.badge(c, rect{in.X, in.Y, d, d}, pad2(i+1), i)
		blocks = append(blocks, placed{
			rect{in.X - 0.05, in.Y + d + 0.1, in.W + 0.1, in.H - d - 0.1},
			textBody{fit: true, paras: t.itemParas(it, 16, 12.5, "l", 5)},
		})
	}
	grow := growList
	if cols == 2 {
		grow = growCards // 2x2 to'rda kartochkalar kattaroq — matn ham kattaroq bo'lishi mumkin
	}
	fitTogether(blocks, grow)
	drawAll(c, blocks)
}

// layoutComparison — ikki panel: rangli sarlavha va punktlar (+ pastda xulosa qatori).
func layoutComparison(t *theme, c *canvas, body rect, s *Slide) {
	items := s.Items[:2]
	const headH, stripH, stripGap = 0.78, 0.82, 0.22
	full := body.H
	if s.Highlight != "" {
		full -= stripH + stripGap
	}
	cols := rect{body.X, body.Y, body.W, full}.cols(2, 0.4)
	textArea := func(r rect) rect { return rect{r.X + 0.28, r.Y + headH + 0.2, r.W - 0.56, r.H - headH - 0.4} }

	heads := make([]placed, 2)
	bodies := make([]placed, 2)
	for i, it := range items {
		col := t.seriesColor(i)
		heads[i] = placed{rect{cols[i].X, 0, cols[i].W, headH}, textBody{anchor: "ctr", fit: true, inset: [4]float64{0.25, 0.05, 0.25, 0.05}, paras: []para{{align: "ctr", runs: []run{
			{text: it.Heading, size: 20, bold: true, color: t.pal.onSeries, font: t.headFont}}}}}}

		var ps []para
		if it.Text != "" {
			ps = append(ps, para{after: 8, runs: []run{{text: it.Text, size: 14.5, italic: true, color: t.pal.muted, font: t.bodyFont}}})
		}
		for _, pt := range it.Points {
			ps = append(ps, para{marL: 0.32, indent: -0.32, after: 9, lineSp: 1.02,
				bullet: &bullet{char: t.bulletChar, color: col, pct: 85},
				runs:   []run{t.tx(pt, 15, t.pal.text)}})
		}
		bodies[i] = placed{textArea(cols[i]), textBody{fit: true, paras: ps}}
	}
	fitTogether(heads, 1)
	fitTogether(bodies, growList)

	panelH := min(full, max(full*0.55, contentHeight(bodies)+headH+0.4))
	groupH := panelH
	if s.Highlight != "" {
		groupH += stripGap + stripH
	}
	top := body.Y + (body.H-groupH)/2
	for i := range items {
		r := rect{cols[i].X, top, cols[i].W, panelH}
		col := t.seriesColor(i)
		t.box(c, r, -1)
		hr := rect{r.X, r.Y, r.W, headH}
		hs := shape{fill: solid(col)}
		if t.neon {
			hs.glow = &glow{radius: 6, color: col, alpha: 30}
		}
		if t.cards.hard {
			hs.line = &stroke{color: t.pal.line, width: max(t.cards.borderW, 1)}
		}
		if t.cards.radius > 0 {
			// Faqat yuqori burchaklari yumaloq sarlavha: yumaloq to'rtburchak va
			// uning pastki yarmini yopuvchi oddiy to'rtburchak ("round2SameRect"
			// shaklini iPhone/Quick Look ko'rsatmaydi).
			hs.geom, hs.adj = "roundRect", roundAdj(hr, t.cards.radius)
			c.add(hr, hs)
			c.add(rect{hr.X, hr.Y + hr.H/2, hr.W, hr.H / 2}, shape{fill: solid(col)})
		} else {
			c.add(hr, hs)
		}
		heads[i].r.Y = top
		bodies[i].r = textArea(r)
	}
	if s.Highlight != "" {
		t.noteStrip(c, rect{body.X, top + panelH + stripGap, body.W, stripH}, s.Highlight)
	}
	drawAll(c, heads)
	drawAll(c, bodies)
}

// layoutProcess — raqamli doiralar chiziq bilan bog'langan bosqichlar.
func layoutProcess(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxListItems)
	if body.W < narrowBody && body.H > 4.9 {
		// Tor va baland maydonda (yon panelli dizayn) bosqichlar ustma-ust.
		layoutProcessVertical(t, c, body, items)
		return
	}
	n := len(items)
	cols := body.cols(n, 0.25)
	d := 0.8
	hs, ts := 16.0, 13.0
	if n >= 5 {
		d, hs, ts = 0.68, 15, 12
	}
	const gap, padX, padY = 0.28, 0.15, 0.2
	fullCard := body.H - d - gap

	blocks := make([]placed, n)
	for i, it := range items {
		blocks[i] = placed{t.cardPad(rect{cols[i].X, body.Y, cols[i].W, fullCard}, padX, padY),
			textBody{fit: true, paras: t.itemParas(it, hs, ts, "ctr", 6)}}
	}
	fitTogether(blocks, growCards)
	cardH := min(fullCard, max(fullCard*0.55, contentHeight(blocks)+2*padY+t.barOffset()))
	top := body.Y + (fullCard-cardH)/2

	lineY := top + d/2
	cx := func(i int) float64 { return cols[i].X + cols[i].W/2 }
	c.add(rect{cx(0), lineY - 0.02, cx(n-1) - cx(0), 0.04}, shape{fill: linear(0, t.seriesColor(0), t.seriesColor(1))})
	for i := 0; i < n-1; i++ {
		mid := (cx(i) + cx(i+1)) / 2
		c.add(rect{mid - 0.1, lineY - 0.11, 0.2, 0.22}, shape{geom: "triangle", rot: 90, fill: solid(t.seriesColor(i))})
	}
	for i := range items {
		t.badge(c, rect{cx(i) - d/2, top, d, d}, pad2(i+1), i)
		cr := rect{cols[i].X, top + d + gap, cols[i].W, cardH}
		t.card(c, cr, i)
		blocks[i].r = t.cardPad(cr, padX, padY)
	}
	drawAll(c, blocks)
}

// layoutFacts — katta raqam/qiymat, nomi va izohi bo'lgan kartochkalar.
func layoutFacts(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxFactItems)
	const stripH, stripGap, padX, padY, vh = 0.82, 0.22, 0.28, 0.22, 1.15
	full := body.H
	if s.Highlight != "" {
		full -= stripH + stripGap
	}
	cols := rect{body.X, body.Y, body.W, full}.cols(len(items), 0.3)
	textArea := func(r rect) rect {
		in := t.cardPad(r, padX, padY)
		return rect{in.X - 0.05, in.Y + vh + 0.25, in.W + 0.1, in.H - vh - 0.25}
	}

	vals := make([]placed, len(items))
	texts := make([]placed, len(items))
	for i, it := range items {
		in := t.cardPad(cols[i], padX, padY)
		vals[i] = placed{rect{in.X - 0.05, in.Y, in.W + 0.1, vh}, textBody{anchor: "b", fit: true, minFit: 0.4, paras: []para{{runs: []run{
			{text: it.Value, size: 48, bold: true, color: t.inkColor(i), font: t.headFont}}}}}}
		texts[i] = placed{textArea(cols[i]), textBody{fit: true, paras: t.itemParas(Item{Heading: it.Heading, Text: it.Text, Points: it.Points}, 16, 13, "l", 5)}}
	}
	fitTogether(vals, 1)
	fitTogether(texts, growCards)

	cardH := min(full, max(min(full*0.6, 2.9), contentHeight(texts)+vh+0.25+2*padY+t.barOffset()))
	groupH := cardH
	if s.Highlight != "" {
		groupH += stripGap + stripH
	}
	top := body.Y + (body.H-groupH)/2
	for i := range items {
		r := rect{cols[i].X, top, cols[i].W, cardH}
		t.card(c, r, i)
		in := t.cardPad(r, padX, padY)
		vals[i].r.Y = in.Y
		c.add(rect{in.X, in.Y + vh + 0.1, 0.55, 0.045}, shape{fill: solid(t.seriesColor(i))})
		texts[i].r = textArea(r)
	}
	if s.Highlight != "" {
		t.noteStrip(c, rect{body.X, top + cardH + stripGap, body.W, stripH}, s.Highlight)
	}
	drawAll(c, vals)
	drawAll(c, texts)
}

// layoutDefinition — chapda ta'rif paneli, o'ngda asosiy xususiyatlar.
func layoutDefinition(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxFactItems)
	panel := body.inset(0.8, 0.15)
	var side rect
	if len(items) > 0 {
		panel, side = body.splitX(body.W*0.5, 0.4)
	}
	t.panel(c, panel)
	quoteMark(c, rect{panel.X + 0.3, panel.Y + 0.12, 1.5, 1.3}, 100, t.pal.onPanel, 30)
	c.text(rect{panel.X + 0.45, panel.Y + 1.15, panel.W - 0.9, panel.H - 1.7}, textBody{fit: true, minFit: 0.55, paras: []para{{lineSp: 1.08, runs: []run{
		{text: s.Highlight, size: 23, italic: t.quoteItalic, color: t.pal.onPanel, font: t.headFont}}}}})
	c.add(rect{panel.X + 0.5, panel.bottom() - 0.45, 0.8, 0.05}, shape{fill: solidA(t.pal.onPanel, 70)})
	if len(items) == 0 {
		return
	}

	rows := side.rows(len(items), 0)
	blocks := make([]placed, 0, len(items))
	for i, it := range items {
		r := rows[i]
		icon := min(0.55, r.H-0.2)
		t.iconTile(c, rect{r.X, r.Y + r.H/2 - icon/2, icon, icon}, pad2(i+1), i)
		blocks = append(blocks, placed{
			rect{r.X + icon + 0.12, r.Y + 0.06, r.W - icon - 0.12, r.H - 0.12},
			textBody{anchor: "ctr", fit: true, paras: t.itemParas(it, 16.5, 13.5, "l", 3)},
		})
		if i < len(items)-1 {
			c.add(rect{r.X, r.bottom() - 0.006, r.W, 0.012}, shape{fill: solid(t.pal.line)})
		}
	}
	fitTogether(blocks, growList)
	drawAll(c, blocks)
}

// layoutConclusion — asosiy fikrlar (✓ belgili kartochkalar) va pastda yakuniy xulosa paneli.
func layoutConclusion(t *theme, c *canvas, body rect, s *Slide) {
	items := capItems(s.Items, maxListItems)
	area := body
	if s.Highlight != "" {
		var banner rect
		area, banner = body.splitY(body.H-1.2, 0.25)
		if len(items) == 0 {
			banner = rect{body.X, body.Y + (body.H-2.2)/2, body.W, 2.2}
		}
		t.panel(c, banner)
		quoteMark(c, rect{banner.X + 0.3, banner.Y + 0.05, 0.9, 0.95}, 60, t.pal.onPanel, 40)
		c.text(rect{banner.X + 1.05, banner.Y + 0.08, banner.W - 1.35, banner.H - 0.16}, textBody{anchor: "ctr", fit: true, minFit: 0.6, paras: []para{{runs: []run{
			{text: s.Highlight, size: 18, italic: t.quoteItalic, color: t.pal.onPanel, font: t.headFont}}}}})
	}
	n := len(items)
	if n == 0 {
		return
	}
	cols := 2
	if n <= 3 {
		cols = n
	}
	rows := (n + cols - 1) / cols
	cells := area.grid(cols, rows, 0.3, 0.2)
	tall := rows == 1 && area.H > 2.2

	blocks := make([]placed, 0, n)
	for i, it := range items {
		r := cells[i]
		t.card(c, r, i)
		in := t.cardPad(r, 0.22, 0.18)
		const d = 0.46
		if tall {
			t.check(c, rect{in.X, in.Y, d, d}, i)
			blocks = append(blocks, placed{rect{in.X - 0.05, in.Y + d + 0.12, in.W + 0.1, in.H - d - 0.12},
				textBody{fit: true, paras: t.itemParas(it, 16.5, 13.5, "l", 4)}})
			continue
		}
		t.check(c, rect{in.X, r.Y + r.H/2 - d/2, d, d}, i)
		blocks = append(blocks, placed{rect{in.X + d + 0.12, in.Y, in.W - d - 0.12, in.H},
			textBody{anchor: "ctr", fit: true, paras: t.itemParas(it, 16, 13, "l", 3)}})
	}
	fitTogether(blocks, growList)
	drawAll(c, blocks)
}
