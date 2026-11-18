package generator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // rasm o'lchamini aniqlash uchun
	_ "image/png"
	"math"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/lang"
)

// DOCX generatori mustaqil ish, referat va amaliy topshiriq uchun Word (.docx)
// hujjatni shablonsiz, to'g'ridan-to'g'ri WordprocessingML (XML) dan yig'adi.
//
// Rasmiylashtirish O'zbekiston OTMlarida keng qo'llaniladigan talablarga mos:
//   - A4, hoshiyalar: chap 3 sm, o'ng 1,5 sm, yuqori va past 2 sm;
//   - Times New Roman 14 pt, qatorlar oralig'i 1,5, xatboshi 1,25 sm, ikki tomonga tekislash;
//   - sahifa raqami pastda markazda, titul varaqda raqam ko'rsatilmaydi;
//   - titul varaq doim bitta sahifada, yil sahifa tagida: oraliqlar va mavzu
//     shrifti matnlar uzunligiga qarab hisoblanadi (doclayout.go);
//   - reja, kirish va adabiyotlar ro'yxati yangi sahifadan boshlanadi; boblar va
//     xulosa matn ketidan davom etadi (sahifalar soni oldindan aytilganiga yaqin
//     chiqishi uchun — har bir bobni yangi sahifadan boshlash chala sahifalar beradi).
//
// Dizayn (docdesign.go) asosiy matnga tegmaydi: sarlavhalar shrifti, rangi va
// bezagi, sahifa ramkasi va titul varaq kompozitsiyasini belgilaydi. Boblarga
// rasm (Section.Figure) qo'yilishi mumkin — raqamlangan izohi bilan.
//
// Hujjat tuzilmasi: titul varaq → REJA → KIRISH → 1..N boblar → XULOSA →
// FOYDALANILGAN ADABIYOTLAR.

// Paragraph — hujjatdagi bitta paragraf. Code=true bo'lsa — dastur kodi.
type Paragraph struct {
	Text string
	Code bool
}

// Figure — bob ichidagi rasm va uning izohi.
type Figure struct {
	Data    []byte // JPEG yoki PNG
	Caption string // "1-rasm." dan keyingi matn
}

// Section — hujjatning bitta bobi.
type Section struct {
	Title      string
	Paragraphs []Paragraph
	// Figure — bobdagi rasm (ixtiyoriy): birinchi paragrafdan keyin qo'yiladi.
	Figure *Figure
}

// Paper — .docx hujjatni yaratish uchun barcha ma'lumotlar.
type Paper struct {
	Topic       string
	WorkType    string // titul varaqda ko'rinadigan nom, masalan "MUSTAQIL ISH"
	StudentName string
	University  string
	TeacherName string
	Year        string
	// Lang — titul yozuvlari va bo'lim sarlavhalari tili (lang.Code); bo'sh bo'lsa — o'zbek (lotin).
	Lang         string
	Introduction []Paragraph
	Chapters     []Section
	Conclusion   []Paragraph
	References   []string

	// Design — tayyor Word dizayni (DocTheme*; bo'sh yoki noma'lum — Klassik).
	Design string
	// Custom — AI maxsus dizayni (berilsa, Design dan ustun).
	Custom *DocSpec
}

// labels hujjat tiliga mos qotirilgan yozuvlar ("Bajardi", "KIRISH" ...).
func (p *Paper) labels() lang.Labels { return lang.Get(p.Lang).Labels }

// Sahifa hajmini baholash uchun ko'rsatkichlar (LibreOffice'da o'lchab moslangan).
const (
	// docWordsPerPage — TNR 14 pt, 1,5 interval, berilgan hoshiyalarda to'liq
	// sahifaga sig'adigan o'zbekcha so'zlar soni.
	docWordsPerPage = 245
	// docServicePages — matndan tashqari sahifalar: titul, reja, adabiyotlar.
	docServicePages = 3
	// docSlackWords — sarlavhalar va oxirgi chala sahifa uchun zaxira (so'zlarda).
	docSlackWords = 250
	// docMinWords — eng kichik hujjat uchun ham yetarli matn bo'lishi uchun.
	docMinWords = 200
	// docFigureWords — bitta rasm (izohi bilan) egallaydigan joy, so'zlarda (~0,45 sahifa).
	docFigureWords = 110
)

// DOCX — .docx hujjatlarni yaratuvchi generator. Holatsiz, shuning uchun
// Build bir vaqtning o'zida bir nechta goroutine'dan chaqirilishi mumkin.
type DOCX struct {
	outputDir string
	header    string // titul varaqning yuqori qatori (vazirlik nomi); bo'sh bo'lsa — chiqarilmaydi
	log       *zap.Logger
}

// NewDOCX generatorni yaratadi. outputDir bo'sh bo'lsa, tizimning vaqtinchalik
// papkasi ishlatiladi.
func NewDOCX(outputDir, header string, log *zap.Logger) (*DOCX, error) {
	dir, err := prepareOutputDir(outputDir)
	if err != nil {
		return nil, fmt.Errorf("generator: %w", err)
	}
	log.Info("docx generator ready", zap.String("output_dir", dir), zap.Bool("title_header", header != ""))
	return &DOCX{outputDir: dir, header: strings.TrimSpace(header), log: log.Named("docx")}, nil
}

// ContentWords foydalanuvchi so'ragan umumiy sahifalar sonidan asosiy matn
// (kirish + boblar + xulosa) uchun kerakli taxminiy so'zlar sonini hisoblaydi.
func (g *DOCX) ContentWords(pages int) int {
	return max(docMinWords, (pages-docServicePages)*docWordsPerPage-docSlackWords)
}

// FigureWords bitta rasm matn o'rnida egallaydigan joy (so'zlarda): rasmli
// hujjatda matn hajmi shuncha kamaytiriladi.
func (g *DOCX) FigureWords() int { return docFigureWords }

// EstimatePages tayyor hujjatning taxminiy sahifalar soni (ContentWords ning
// teskarisi): matn hajmi, rasmlar va xizmat sahifalari bo'yicha.
func (g *DOCX) EstimatePages(p Paper) int {
	words, figures := 0, 0
	count := func(ps []Paragraph) {
		for _, x := range ps {
			words += len(strings.Fields(x.Text))
		}
	}
	count(p.Introduction)
	count(p.Conclusion)
	for _, ch := range p.Chapters {
		count(ch.Paragraphs)
		if ch.Figure != nil && len(ch.Figure.Data) > 0 {
			figures++
		}
	}
	content := float64(words+figures*docFigureWords+docSlackWords) / docWordsPerPage
	return docServicePages + int(math.Round(content))
}

// OOXML ichki yo'llari.
const (
	docRelsPath       = "_rels/.rels"
	docDocumentPath   = "word/document.xml"
	docDocumentRels   = "word/_rels/document.xml.rels"
	docStylesPath     = "word/styles.xml"
	docSettingsPath   = "word/settings.xml"
	docFontTablePath  = "word/fontTable.xml"
	docFooterPath     = "word/footer1.xml" // sahifa raqami
	docFooterTitle    = "word/footer2.xml" // titul varaq uchun bo'sh kolontitul
	docCorePropsPath  = corePropsPath
	docAppPropsPath   = appPropsPath
	docContentTypes   = contentTypesPath
	docFileNamePrefix = "document-*.docx"
)

// docImage — hujjatga joylanadigan rasm.
type docImage struct {
	path    string // paket ichidagi yo'li: word/media/image1.jpeg
	relID   string
	id      int   // docPr id (1 dan)
	cx, cy  int64 // o'lchami, EMU
	data    []byte
	caption string
}

// Rasm o'lchamlari chegarasi (EMU: 1 sm = 360000).
const (
	docFigureMaxW = 14 * 360000
	docFigureMaxH = 9 * 360000
)

// Build Paper asosida yangi .docx faylni outputDir ichida yaratadi va uning
// yo'lini qaytaradi. Faylni o'chirish (os.Remove) chaqiruvchining vazifasi.
func (g *DOCX) Build(ctx context.Context, p Paper) (string, error) {
	if len(p.Chapters) == 0 {
		return "", errors.New("generator: hujjatda birorta ham bob yo'q")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	started := time.Now()
	st := docStyleFor(p.Design, p.Custom)
	figures := g.collectFigures(p)

	files := map[string][]byte{
		docContentTypes:  []byte(docxContentTypes),
		docRelsPath:      []byte(docxRootRels),
		docDocumentRels:  []byte(docxDocumentRels(figures)),
		docStylesPath:    []byte(docxStyles(st, lang.Get(p.Lang).OOXML)),
		docSettingsPath:  []byte(docxSettings),
		docFontTablePath: []byte(docxFontTable(st)),
		docFooterPath:    []byte(docxPageNumberFooter(st)),
		docFooterTitle:   []byte(docxEmptyFooter),
		docAppPropsPath:  []byte(docxAppProps),
		docCorePropsPath: []byte(docxCoreProps(p.Topic, p.StudentName, time.Now().UTC())),
		docDocumentPath:  []byte(g.renderDocument(p, st, figures)),
	}
	order := []string{
		docContentTypes, docRelsPath, docCorePropsPath, docAppPropsPath,
		docDocumentPath, docDocumentRels, docStylesPath, docSettingsPath,
		docFontTablePath, docFooterPath, docFooterTitle,
	}
	placed := 0
	for _, f := range figures {
		if f == nil {
			continue
		}
		files[f.path] = f.data
		order = append(order, f.path)
		placed++
	}

	out, err := writeArchive(ctx, g.outputDir, docFileNamePrefix, order, files)
	if err != nil {
		return "", fmt.Errorf("generator: %w", err)
	}
	g.log.Info("docx generated",
		zap.String("file", out),
		zap.Int("chapters", len(p.Chapters)),
		zap.Int("figures", placed),
		zap.Int("references", len(p.References)),
		zap.Duration("took", time.Since(started)))
	return out, nil
}

// collectFigures boblardagi rasmlarni tayyorlaydi (bob indeksi bo'yicha; rasm
// yo'q yoki o'qib bo'lmagan bob — nil).
func (g *DOCX) collectFigures(p Paper) []*docImage {
	out := make([]*docImage, len(p.Chapters))
	n := 0
	for i, ch := range p.Chapters {
		if ch.Figure == nil || len(ch.Figure.Data) == 0 {
			continue
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(ch.Figure.Data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
			g.log.Warn("figure skipped: unreadable image", zap.Int("chapter", i+1), zap.Error(err))
			continue
		}
		ext := "jpeg"
		if format == "png" {
			ext = "png"
		}
		cx := int64(docFigureMaxW)
		cy := cx * int64(cfg.Height) / int64(cfg.Width)
		if cy > docFigureMaxH {
			cy = docFigureMaxH
			cx = cy * int64(cfg.Width) / int64(cfg.Height)
		}
		n++
		out[i] = &docImage{
			path:    fmt.Sprintf("word/media/image%d.%s", n, ext),
			relID:   fmt.Sprintf("rIdImg%d", n),
			id:      n,
			cx:      cx,
			cy:      cy,
			data:    ch.Figure.Data,
			caption: strings.TrimSpace(ch.Figure.Caption),
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// document.xml
// ---------------------------------------------------------------------------

// renderDocument hujjat tanasini (titul, reja, matn, adabiyotlar) yig'adi.
func (g *DOCX) renderDocument(p Paper, st docStyle, figures []*docImage) string {
	var w docBuilder
	w.raw(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
		`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
		`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>`)

	g.renderTitlePage(&w, p, st)
	l := p.labels()

	// Reja.
	chapterTitles := make([]string, len(p.Chapters))
	for i, ch := range p.Chapters {
		chapterTitles[i] = fmt.Sprintf("%d. %s", i+1, strings.TrimSpace(ch.Title))
	}
	// Bo'lim sarlavhalari hujjat tilida: rejada — oddiy, matnda — katta harflar bilan.
	w.heading(strings.ToUpper(l.Plan), true)
	w.para("PlanItem", "", textRun(l.Introduction, ""))
	for i, ch := range p.Chapters {
		// Raqam + tab, osilgan xatboshi: uzun nomning keyingi qatorlari
		// birinchi qatordagi nom bilan bir chiziqda tekislanadi.
		w.para("PlanItem", `<w:ind w:left="709" w:hanging="425"/>`,
			textRun(strconv.Itoa(i+1)+".", "")+`<w:r><w:tab/></w:r>`+textRun(strings.TrimSpace(ch.Title), ""))
	}
	w.para("PlanItem", "", textRun(l.Conclusion, ""))
	if len(p.References) > 0 {
		w.para("PlanItem", "", textRun(l.References, ""))
	}

	// Asosiy matn.
	w.heading(strings.ToUpper(l.Introduction), true)
	w.body(p.Introduction)
	figNo := 0
	for i, ch := range p.Chapters {
		w.heading(chapterTitles[i], false)
		fig := figures[i]
		if fig == nil {
			w.body(ch.Paragraphs)
			continue
		}
		// Rasm birinchi paragrafdan keyin (bob bitta paragrafdan iborat bo'lsa — oxirida).
		at := min(1, len(ch.Paragraphs))
		w.body(ch.Paragraphs[:at])
		figNo++
		w.figure(fig, fmt.Sprintf(l.FigureFormat, figNo))
		w.body(ch.Paragraphs[at:])
	}
	w.heading(strings.ToUpper(l.Conclusion), false)
	w.body(p.Conclusion)

	// Adabiyotlar: raqam + tab, osilgan xatboshi (hanging indent) bilan.
	if len(p.References) > 0 {
		w.heading(strings.ToUpper(l.References), true)
		for i, ref := range p.References {
			w.para("Reference", "", textRun(strconv.Itoa(i+1)+".", "")+`<w:r><w:tab/></w:r>`+textRun(ref, ""))
		}
	}

	w.raw(docxSectionProperties(st) + `</w:body></w:document>`)
	return w.String()
}

// renderTitlePage titul varaqni dizayn kompozitsiyasiga qarab chizadi.
// Oraliqlar va mavzu shrifti matnlar uzunligiga qarab tanlanadi (doclayout.go):
// titul doim bitta sahifaga sig'adi, yil esa sahifa tagida turadi.
func (g *DOCX) renderTitlePage(w *docBuilder, p Paper, st docStyle) {
	l := p.labels()
	t := titleTexts{
		header:     g.titleHeader(p),
		university: p.University,
		workType:   p.WorkType,
		ornament:   ornamentText(st.ornament),
		topic:      l.Topic + ": " + p.Topic,
		student:    l.Student + ": ",
		teacher:    l.Teacher + ": ",
		year:       fmt.Sprintf(l.YearFormat, p.Year),
	}
	// O'lchash uchun to'liq qatorlar; chizishda yozuv qalin, ism oddiy.
	measured := t
	measured.student += p.StudentName
	measured.teacher += p.TeacherName
	lay := planTitle(measured, st)

	color := func(c string) string {
		if c == "" || c == "000000" {
			return ""
		}
		return `<w:color w:val="` + c + `"/>`
	}
	headFont := ""
	if st.headingFont != "" {
		headFont = `<w:rFonts w:ascii="` + st.headingFont + `" w:hAnsi="` + st.headingFont + `" w:cs="` + st.headingFont + `"/>`
	}
	topicSize := strconv.Itoa(lay.topicSize)
	var (
		bold      = `<w:rPr><w:b/><w:bCs/></w:rPr>`
		boldCaps  = `<w:rPr><w:b/><w:bCs/><w:caps/></w:rPr>`
		workColor = color(st.primary)
		workShd   = ""
		topicRPr  = `<w:rPr>` + headFont + `<w:b/><w:bCs/>` + color(st.primary) +
			`<w:sz w:val="` + topicSize + `"/><w:szCs w:val="` + topicSize + `"/></w:rPr>`
	)
	if st.title == docTitleBand {
		// Oq matn rangli lenta ustida. Lenta paragraf fonida, lekin matnning
		// o'ziga ham shu fon beriladi: paragraf fonini chizmaydigan
		// ko'ruvchilarda (iPhone/Mac Quick Look, Telegram ichida) oq matn oq
		// qog'ozda yo'qolib qolmasin.
		workColor = `<w:color w:val="FFFFFF"/>`
		workShd = shading(st.primary)
	}
	workRPr := `<w:rPr>` + headFont + `<w:b/><w:bCs/><w:caps/>` + workColor +
		`<w:spacing w:val="20"/><w:sz w:val="44"/><w:szCs w:val="44"/>` + workShd + `</w:rPr>`

	// Kompozitsiyaga qarab paragraf uslubi va qo'shimcha xususiyatlar.
	center, sign := "TitleCenter", "TitleSign"
	sidebar := ""
	if st.title == docTitleSidebar {
		// Chapga tekislangan; barcha paragraflarda bir xil chap chiziq — Word
		// ularni bitta uzluksiz chiziqqa birlashtiradi.
		center, sign = "TitleLeft", "TitleLeft"
		sidebar = `<w:pBdr><w:left w:val="single" w:sz="36" w:space="14" w:color="` + st.accent + `"/></w:pBdr>`
	}
	pPr := func(extra string) string { return extra }
	if sidebar != "" {
		// pBdr sxema bo'yicha spacing dan oldin keladi.
		pPr = func(extra string) string { return sidebar + extra }
	}

	if t.header != "" {
		w.para(center, pPr(""), textRun(t.header, boldCaps))
	}
	uniPPr := spacingBefore(titleGapBeforeUniversity)
	if st.ornament == docOrnamentLine && sidebar == "" {
		// Universitet ostida ingichka chiziq.
		uniPPr = `<w:pBdr><w:bottom w:val="single" w:sz="8" w:space="8" w:color="` + st.accent + `"/></w:pBdr>` + uniPPr
	}
	w.para(center, pPr(uniPPr), textRun(t.university, boldCaps))

	// Ish turi va mavzu.
	workPPr := spacingBefore(lay.gapWork)
	topicPPr := spacingBefore(titleGapBeforeTopic)
	ornPPr := spacingBefore(titleGapBeforeOrnament)
	switch st.title {
	case docTitleBand:
		// Rangli lenta: fon rangidagi chegaralar lentaga ichki bo'shliq beradi.
		band := `<w:pBdr>` +
			`<w:top w:val="single" w:sz="4" w:space="10" w:color="` + st.primary + `"/>` +
			`<w:bottom w:val="single" w:sz="4" w:space="10" w:color="` + st.primary + `"/>` +
			`</w:pBdr>` + shading(st.primary)
		workPPr = band + spacingBefore(lay.gapWork)
		topicPPr = `<w:pBdr><w:bottom w:val="single" w:sz="12" w:space="10" w:color="` + st.accent + `"/></w:pBdr>` +
			spacingBefore(titleGapBeforeTopicBand)
	case docTitleFrame:
		// Ish turi va mavzu bitta chiziqli blok ichida (bir xil chegaralar birlashadi).
		box := `<w:pBdr>` +
			`<w:top w:val="double" w:sz="6" w:space="14" w:color="` + st.accent + `"/>` +
			`<w:left w:val="double" w:sz="6" w:space="14" w:color="` + st.accent + `"/>` +
			`<w:bottom w:val="double" w:sz="6" w:space="14" w:color="` + st.accent + `"/>` +
			`<w:right w:val="double" w:sz="6" w:space="14" w:color="` + st.accent + `"/>` +
			`</w:pBdr>`
		ind := `<w:ind w:left="` + strconv.Itoa(titleFrameIndent) + `" w:right="` + strconv.Itoa(titleFrameIndent) + `" w:firstLine="0"/>`
		workPPr = box + spacingBefore(lay.gapWork) + ind
		topicPPr = box + spacingBefore(titleGapBeforeTopic) + ind
		ornPPr = box + spacingBefore(titleGapBeforeOrnament) + ind // bezak ham blok ichida (blok bo'linmasin)
	}
	w.para(center, pPr(workPPr), textRun(t.workType, workRPr))
	if t.ornament != "" {
		w.para(center, pPr(ornPPr), textRun(t.ornament, `<w:rPr>`+color(st.accent)+`<w:sz w:val="24"/></w:rPr>`))
	}
	w.para(center, pPr(topicPPr), textRun(t.topic, topicRPr))

	w.para(sign, pPr(spacingBefore(lay.gapSign)), textRun(t.student, bold)+textRun(p.StudentName, ""))
	w.para(sign, pPr(spacingBefore(titleGapBeforeTeacher)), textRun(t.teacher, bold)+textRun(p.TeacherName, ""))
	w.para(center, pPr(spacingBefore(lay.gapYear)), textRun(t.year, bold))
}

// ornamentText titul varaqdagi bezak qatori (bo'sh — bezaksiz yoki chiziq).
func ornamentText(ornament string) string {
	switch ornament {
	case docOrnamentDiamond:
		return "◆   ◆   ◆"
	case docOrnamentDots:
		return "•   •   •   •   •"
	}
	return ""
}

// shading — paragraf yoki matn foni (w:shd).
func shading(fill string) string {
	return `<w:shd w:val="clear" w:color="auto" w:fill="` + fill + `"/>`
}

// apostrophes — apostrof variantlari (solishtirish uchun bittasiga keltiriladi).
var apostrophes = strings.NewReplacer("’", "'", "ʻ", "'", "ʼ", "'", "‘", "'", "`", "'")

// titleHeader titul varaqning yuqori qatori (vazirlik nomi) hujjat tilida.
// Sozlamada standart nom (o'zbekcha) turgan bo'lsa, hujjat tilidagi tarjimasi
// olinadi; boshqa, qo'lda yozilgan nom o'zgarishsiz chiqadi.
func (g *DOCX) titleHeader(p Paper) string {
	if g.header == "" {
		return ""
	}
	norm := func(s string) string { return strings.Join(strings.Fields(apostrophes.Replace(s)), " ") }
	if def := lang.Get(string(lang.UzLatn)).Labels.Ministry; strings.EqualFold(norm(g.header), norm(def)) {
		return p.labels().Ministry
	}
	return g.header
}

// docBuilder — document.xml ni bo'lakma-bo'lak yig'uvchi yordamchi.
type docBuilder struct {
	strings.Builder
}

func (w *docBuilder) raw(s string) { w.WriteString(s) }

// para paragraf yozadi. extraPPr — pStyle dan keyingi qo'shimcha xususiyatlar;
// OOXML sxemasidagi tartibga rioya qilinishi kerak (pBdr → shd → spacing → ind → jc).
func (w *docBuilder) para(style, extraPPr, runs string) {
	w.WriteString(`<w:p><w:pPr><w:pStyle w:val="`)
	w.WriteString(style)
	w.WriteString(`"/>`)
	w.WriteString(extraPPr)
	w.WriteString(`</w:pPr>`)
	w.WriteString(runs)
	w.WriteString(`</w:p>`)
}

// heading bo'lim sarlavhasi (Heading 1 — Word'ning navigatsiya panelida va
// avtomatik mundarijada ko'rinadi). newPage=true bo'lsa — yangi sahifadan.
func (w *docBuilder) heading(text string, newPage bool) {
	extra := ""
	if newPage {
		// Sahifa boshida yuqori masofa kerak emas.
		extra = `<w:pageBreakBefore/><w:spacing w:before="0"/>`
	}
	w.para("Heading1", extra, textRun(text, ""))
}

// body bo'lim paragraflarini yozadi.
func (w *docBuilder) body(paras []Paragraph) {
	for _, p := range paras {
		if p.Code {
			w.para("Code", "", codeRuns(p.Text))
			continue
		}
		w.para("Normal", "", textRun(p.Text, ""))
	}
}

// figure rasmni (markazda) va uning izohini ("1-rasm. ...") yozadi.
func (w *docBuilder) figure(f *docImage, label string) {
	cx, cy := strconv.FormatInt(f.cx, 10), strconv.FormatInt(f.cy, 10)
	id := strconv.Itoa(f.id)
	w.para("Figure", "", `<w:r><w:drawing>`+
		`<wp:inline distT="0" distB="0" distL="0" distR="0">`+
		`<wp:extent cx="`+cx+`" cy="`+cy+`"/>`+
		`<wp:effectExtent l="0" t="0" r="0" b="0"/>`+
		`<wp:docPr id="`+id+`" name="Rasm `+id+`"/>`+
		`<wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr>`+
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic><pic:nvPicPr><pic:cNvPr id="`+id+`" name="image`+id+`"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="`+f.relID+`"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="`+cx+`" cy="`+cy+`"/></a:xfrm>`+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic>`+
		`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`)
	runs := textRun(label, `<w:rPr><w:b/><w:bCs/></w:rPr>`)
	if f.caption != "" {
		runs += textRun(" "+f.caption, "")
	}
	w.para("FigureCaption", "", runs)
}

// textRun bitta matn bo'lagini (w:r) yaratadi; rPr — formatlash (yoki bo'sh).
func textRun(text, rPr string) string {
	return `<w:r>` + rPr + `<w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r>`
}

// codeRuns dastur kodini qatorlari va bo'sh joylari saqlangan holda yozadi.
func codeRuns(code string) string {
	lines := strings.Split(sanitizeXMLText(code, true), "\n")
	var b strings.Builder
	b.WriteString(`<w:r>`)
	for i, line := range lines {
		if i > 0 {
			b.WriteString(`<w:br/>`)
		}
		b.WriteString(`<w:t xml:space="preserve">`)
		b.WriteString(xmlEscaper.Replace(line))
		b.WriteString(`</w:t>`)
	}
	b.WriteString(`</w:r>`)
	return b.String()
}

func spacingBefore(twips int) string {
	return `<w:spacing w:before="` + strconv.Itoa(twips) + `"/>`
}

// docxSectionProperties — sahifa o'lchami (A4), hoshiyalar, ramka va
// kolontitullar. w:titlePg — birinchi (titul) sahifa uchun alohida, bo'sh
// kolontitul. Raqamlash titul varaqdan boshlanadi, shuning uchun reja
// 2-sahifa bo'ladi.
func docxSectionProperties(st docStyle) string {
	itoa := strconv.Itoa
	return `<w:sectPr>` +
		`<w:footerReference w:type="default" r:id="rId3"/>` +
		`<w:footerReference w:type="first" r:id="rId4"/>` +
		`<w:pgSz w:w="` + itoa(docPageW) + `" w:h="` + itoa(docPageH) + `"/>` +
		`<w:pgMar w:top="` + itoa(docMarginTop) + `" w:right="` + itoa(docMarginRight) + `" w:bottom="` + itoa(docMarginBottom) +
		`" w:left="` + itoa(docMarginLeft) + `" w:header="709" w:footer="567" w:gutter="0"/>` +
		pageBorders(st) +
		`<w:pgNumType w:start="1"/>` +
		`<w:titlePg/>` +
		`</w:sectPr>`
}

// pageBorders sahifa ramkasi: sahifa chetidan 20 pt ichkarida (sahifa
// raqami ramka ichida qoladi), barcha sahifalarda.
func pageBorders(st docStyle) string {
	var tl, br, sz string
	switch st.border {
	case docBorderSingle:
		tl, br, sz = "single", "single", "12"
	case docBorderDouble:
		tl, br, sz = "double", "double", "6"
	case docBorderThinThick:
		// Tashqarida ingichka, ichkarida qalin chiziq — har tomonda nosimmetrik.
		tl, br, sz = "thinThickSmallGap", "thickThinSmallGap", "18"
	default:
		return ""
	}
	side := func(name, val string) string {
		return `<w:` + name + ` w:val="` + val + `" w:sz="` + sz + `" w:space="20" w:color="` + st.accent + `"/>`
	}
	return `<w:pgBorders w:offsetFrom="page">` +
		side("top", tl) + side("left", tl) + side("bottom", br) + side("right", br) +
		`</w:pgBorders>`
}

// ---------------------------------------------------------------------------
// Paketning qolgan qismlari
// ---------------------------------------------------------------------------

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const docxContentTypes = xmlHeader +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Default Extension="jpeg" ContentType="image/jpeg"/>` +
	`<Default Extension="png" ContentType="image/png"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
	`<Override PartName="/word/fontTable.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.fontTable+xml"/>` +
	`<Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>` +
	`<Override PartName="/word/footer2.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>` +
	`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
	`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` +
	`</Types>`

const docxRootRels = xmlHeader +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>` +
	`</Relationships>`

// docxDocumentRels document.xml bog'lanishlari: uslublar, sozlamalar,
// kolontitullar va rasmlar.
func docxDocumentRels(figures []*docImage) string {
	var b strings.Builder
	b.WriteString(xmlHeader +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>` +
		`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>` +
		`<Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer2.xml"/>` +
		`<Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable" Target="fontTable.xml"/>`)
	for _, f := range figures {
		if f == nil {
			continue
		}
		b.WriteString(`<Relationship Id="` + f.relID + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="` +
			strings.TrimPrefix(f.path, "word/") + `"/>`)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

// docFontInfo — shriftlar jadvali uchun ma'lumot: oilasi, PANOSE kodi va
// boshqa tizimlardagi o'xshash shrift. Shrift o'rnatilmagan qurilmada (Mac,
// telefon) dastur shular bo'yicha mos o'rinbosarni tanlaydi: serifli
// sarlavha serifli, serifsiz — serifsiz shrift bilan almashadi.
type docFontInfo struct{ family, panose, alt string }

var docFonts = map[string]docFontInfo{
	"Times New Roman":   {"roman", "02020603050405020304", ""},
	"Courier New":       {"modern", "02070309020205020404", ""},
	"Cambria":           {"roman", "02040503050406030204", ""},
	"Georgia":           {"roman", "02040502050405020303", ""},
	"Palatino Linotype": {"roman", "02040502050505030304", "Palatino"},
	"Book Antiqua":      {"roman", "02040602050305030304", "Palatino"},
	"Garamond":          {"roman", "02020404030301010803", ""},
	"Calibri":           {"swiss", "020F0502020204030204", ""},
	"Arial":             {"swiss", "020B0604020202020204", ""},
	"Century Gothic":    {"swiss", "020B0502020202020204", "Avenir Next"},
	"Trebuchet MS":      {"swiss", "020B0603020202020204", ""},
	"Segoe UI":          {"swiss", "020B0502040204020203", "Helvetica Neue"},
}

// docxFontTable — hujjatdagi shriftlar jadvali (word/fontTable.xml).
func docxFontTable(st docStyle) string {
	names := []string{"Times New Roman", "Courier New"}
	if st.headingFont != "" && st.headingFont != "Times New Roman" {
		names = append(names, st.headingFont)
	}
	var b strings.Builder
	b.WriteString(xmlHeader + `<w:fonts xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	for _, name := range names {
		f, ok := docFonts[name]
		if !ok {
			f.family = "auto"
		}
		pitch := "variable"
		if f.family == "modern" {
			pitch = "fixed"
		}
		// Elementlar tartibi sxema bo'yicha: altName → panose1 → family → pitch.
		b.WriteString(`<w:font w:name="` + xmlEscape(name) + `">`)
		if f.alt != "" {
			b.WriteString(`<w:altName w:val="` + f.alt + `"/>`)
		}
		if f.panose != "" {
			b.WriteString(`<w:panose1 w:val="` + f.panose + `"/>`)
		}
		b.WriteString(`<w:family w:val="` + f.family + `"/><w:pitch w:val="` + pitch + `"/></w:font>`)
	}
	b.WriteString(`</w:fonts>`)
	return b.String()
}

const docxSettings = xmlHeader +
	`<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:zoom w:percent="100"/>` +
	`<w:defaultTabStop w:val="709"/>` +
	`<w:characterSpacingControl w:val="doNotCompress"/>` +
	`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat>` +
	`</w:settings>`

// docxStyles — hujjat uslublari (dizaynga qarab sarlavhalar). Elementlar
// tartibi OOXML sxemasiga mos (Word tartib buzilgan faylni "shikastlangan"
// deb ochmasligi mumkin). ooxmlLang — imlo tekshiruvi tili.
func docxStyles(st docStyle, ooxmlLang string) string {
	headFont := ""
	if st.headingFont != "" {
		headFont = `<w:rFonts w:ascii="` + st.headingFont + `" w:hAnsi="` + st.headingFont + `" w:cs="` + st.headingFont + `"/>`
	}
	headColor := st.primary
	var headPPr string
	switch st.heading {
	case docHeadingUnderline:
		headPPr = `<w:pBdr><w:bottom w:val="single" w:sz="12" w:space="4" w:color="` + st.accent + `"/></w:pBdr>` +
			`<w:spacing w:before="480" w:after="240"/><w:ind w:firstLine="0"/><w:jc w:val="left"/>`
	case docHeadingBand:
		headColor = "FFFFFF"
		headPPr = `<w:pBdr>` +
			`<w:top w:val="single" w:sz="4" w:space="3" w:color="` + st.primary + `"/>` +
			`<w:left w:val="single" w:sz="4" w:space="6" w:color="` + st.primary + `"/>` +
			`<w:bottom w:val="single" w:sz="4" w:space="3" w:color="` + st.primary + `"/>` +
			`<w:right w:val="single" w:sz="4" w:space="6" w:color="` + st.primary + `"/>` +
			`</w:pBdr><w:shd w:val="clear" w:color="auto" w:fill="` + st.primary + `"/>` +
			`<w:spacing w:before="480" w:after="240"/><w:ind w:left="113" w:right="113" w:firstLine="0"/><w:jc w:val="left"/>`
	case docHeadingBar:
		headPPr = `<w:pBdr><w:left w:val="single" w:sz="36" w:space="8" w:color="` + st.accent + `"/></w:pBdr>` +
			`<w:spacing w:before="480" w:after="240"/><w:ind w:left="170" w:firstLine="0"/><w:jc w:val="left"/>`
	default:
		headPPr = `<w:spacing w:before="480" w:after="240"/><w:ind w:firstLine="0"/><w:jc w:val="center"/>`
	}
	headRPr := headFont + `<w:b/><w:bCs/>`
	if headColor != "" && headColor != "000000" {
		headRPr += `<w:color w:val="` + headColor + `"/>`
	}
	if st.heading == docHeadingBand {
		// Matnning o'ziga ham lenta foni: paragraf fonini chizmaydigan
		// ko'ruvchilarda (Quick Look) oq sarlavha yo'qolib qolmasin.
		headRPr += shading(st.primary)
	}

	return xmlHeader +
		`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:docDefaults>` +
		`<w:rPrDefault><w:rPr>` +
		`<w:rFonts w:ascii="Times New Roman" w:eastAsia="Times New Roman" w:hAnsi="Times New Roman" w:cs="Times New Roman"/>` +
		`<w:sz w:val="28"/><w:szCs w:val="28"/>` +
		`<w:lang w:val="` + ooxmlLang + `" w:eastAsia="en-US" w:bidi="ar-SA"/>` +
		`</w:rPr></w:rPrDefault>` +
		`<w:pPrDefault><w:pPr><w:spacing w:after="0" w:line="360" w:lineRule="auto"/></w:pPr></w:pPrDefault>` +
		`</w:docDefaults>` +

		// Asosiy matn: xatboshi 1,25 sm, ikki tomonga tekislash, 1,5 interval.
		`<w:style w:type="paragraph" w:default="1" w:styleId="Normal">` +
		`<w:name w:val="Normal"/><w:qFormat/>` +
		`<w:pPr><w:widowControl/><w:ind w:firstLine="709"/><w:jc w:val="both"/></w:pPr>` +
		`</w:style>` +

		// Bo'lim sarlavhasi: dizayn uslubida, keyingi paragrafdan ajralmaydi.
		`<w:style w:type="paragraph" w:styleId="Heading1">` +
		`<w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="9"/><w:qFormat/>` +
		`<w:pPr><w:keepNext/><w:keepLines/>` + headPPr + `<w:outlineLvl w:val="0"/></w:pPr>` +
		`<w:rPr>` + headRPr + `</w:rPr>` +
		`</w:style>` +

		// Titul varaq: markazlashtirilgan matn.
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="TitleCenter">` +
		`<w:name w:val="Title Center"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:spacing w:line="276" w:lineRule="auto"/><w:ind w:firstLine="0"/><w:jc w:val="center"/></w:pPr>` +
		`</w:style>` +

		// Titul varaq: chapga tekislangan (yon chiziqli kompozitsiya).
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="TitleLeft">` +
		`<w:name w:val="Title Left"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:spacing w:line="276" w:lineRule="auto"/><w:ind w:left="` + strconv.Itoa(titleSidebarIndent) + `" w:firstLine="0"/><w:jc w:val="left"/></w:pPr>` +
		`</w:style>` +

		// Titul varaq: "Bajardi / Tekshirdi" bloki (o'ng tomonda).
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="TitleSign">` +
		`<w:name w:val="Title Signature"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:spacing w:line="276" w:lineRule="auto"/><w:ind w:left="` + strconv.Itoa(titleSignIndent) + `" w:firstLine="0"/><w:jc w:val="left"/></w:pPr>` +
		`</w:style>` +

		// Reja bandlari.
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="PlanItem">` +
		`<w:name w:val="Plan Item"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:ind w:left="0" w:firstLine="0"/><w:jc w:val="left"/></w:pPr>` +
		`</w:style>` +

		// Rasm: markazda, izohidan ajralmaydi.
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Figure">` +
		`<w:name w:val="Figure"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:keepNext/><w:spacing w:before="240" w:after="60" w:line="240" w:lineRule="auto"/><w:ind w:firstLine="0"/><w:jc w:val="center"/></w:pPr>` +
		`</w:style>` +

		// Rasm izohi: "1-rasm. ..." markazda, 12 pt.
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="FigureCaption">` +
		`<w:name w:val="Figure Caption"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:spacing w:after="240" w:line="240" w:lineRule="auto"/><w:ind w:firstLine="0"/><w:jc w:val="center"/></w:pPr>` +
		`<w:rPr><w:i/><w:iCs/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr>` +
		`</w:style>` +

		// Adabiyotlar: osilgan xatboshi, chapga tekislangan (ikki tomonga
		// tekislashda uzun havolali qatorlarda so'zlar orasi cho'zilib ketadi).
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Reference">` +
		`<w:name w:val="Reference"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:ind w:left="567" w:hanging="567"/><w:jc w:val="left"/></w:pPr>` +
		`</w:style>` +

		// Dastur kodi: monoshirift, kulrang fon, bitta interval.
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Code">` +
		`<w:name w:val="Code"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:keepLines/><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>` +
		`<w:spacing w:before="120" w:after="120" w:line="240" w:lineRule="auto"/>` +
		`<w:ind w:left="284" w:right="284" w:firstLine="0"/><w:jc w:val="left"/></w:pPr>` +
		`<w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:cs="Courier New"/><w:noProof/><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr>` +
		`</w:style>` +

		// Kolontitul (sahifa raqami).
		`<w:style w:type="paragraph" w:styleId="Footer">` +
		`<w:name w:val="footer"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:spacing w:line="240" w:lineRule="auto"/><w:ind w:firstLine="0"/><w:jc w:val="center"/></w:pPr>` +
		`<w:rPr><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr>` +
		`</w:style>` +
		`</w:styles>`
}

// docxPageNumberFooter — markazda sahifa raqami (PAGE maydoni), dizayn rangida.
func docxPageNumberFooter(st docStyle) string {
	rPr := ""
	if st.primary != "" && st.primary != "000000" {
		rPr = `<w:rPr><w:color w:val="` + st.primary + `"/></w:rPr>`
	}
	run := func(inner string) string { return `<w:r>` + rPr + inner + `</w:r>` }
	return xmlHeader +
		`<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<w:p><w:pPr><w:pStyle w:val="Footer"/></w:pPr>` +
		run(`<w:fldChar w:fldCharType="begin"/>`) +
		run(`<w:instrText xml:space="preserve"> PAGE </w:instrText>`) +
		run(`<w:fldChar w:fldCharType="separate"/>`) +
		run(`<w:t>2</w:t>`) +
		run(`<w:fldChar w:fldCharType="end"/>`) +
		`</w:p></w:ftr>`
}

const docxEmptyFooter = xmlHeader +
	`<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
	`<w:p><w:pPr><w:pStyle w:val="Footer"/></w:pPr></w:p></w:ftr>`

// docxAppProps — kengaytirilgan xususiyatlar. Hujjat qanday yaratilgani haqida
// hech qanday ma'lumot yozilmaydi (muallif — talaba, core.xml da).
const docxAppProps = xmlHeader +
	`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" ` +
	`xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">` +
	`<DocSecurity>0</DocSecurity>` +
	`</Properties>`

// docxCoreProps hujjat metama'lumotlari (sarlavha, muallif, sana).
func docxCoreProps(title, author string, now time.Time) string {
	ts := now.Format("2006-01-02T15:04:05Z")
	return xmlHeader +
		`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" ` +
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" ` +
		`xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + xmlEscape(title) + `</dc:title>` +
		`<dc:creator>` + xmlEscape(author) + `</dc:creator>` +
		`<cp:lastModifiedBy>` + xmlEscape(author) + `</cp:lastModifiedBy>` +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + ts + `</dcterms:created>` +
		`<dcterms:modified xsi:type="dcterms:W3CDTF">` + ts + `</dcterms:modified>` +
		`</cp:coreProperties>`
}
