package generator

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// DOCX generatori mustaqil ish va amaliy topshiriq uchun Word (.docx) hujjatni
// shablonsiz, to'g'ridan-to'g'ri WordprocessingML (XML) dan yig'adi.
//
// Rasmiylashtirish O'zbekiston OTMlarida keng qo'llaniladigan talablarga mos:
//   - A4, hoshiyalar: chap 3 sm, o'ng 1,5 sm, yuqori va past 2 sm;
//   - Times New Roman 14 pt, qatorlar oralig'i 1,5, xatboshi 1,25 sm, ikki tomonga tekislash;
//   - sahifa raqami pastda markazda, titul varaqda raqam ko'rsatilmaydi;
//   - reja, kirish va adabiyotlar ro'yxati yangi sahifadan boshlanadi; boblar va
//     xulosa matn ketidan davom etadi (sahifalar soni oldindan aytilganiga yaqin
//     chiqishi uchun — har bir bobni yangi sahifadan boshlash chala sahifalar beradi).
//
// Hujjat tuzilmasi: titul varaq → REJA → KIRISH → 1..N boblar → XULOSA →
// FOYDALANILGAN ADABIYOTLAR.

// Paragraph — hujjatdagi bitta paragraf. Code=true bo'lsa — dastur kodi.
type Paragraph struct {
	Text string
	Code bool
}

// Section — hujjatning bitta bobi.
type Section struct {
	Title      string
	Paragraphs []Paragraph
}

// Paper — .docx hujjatni yaratish uchun barcha ma'lumotlar.
type Paper struct {
	Topic        string
	WorkType     string // titul varaqda ko'rinadigan nom, masalan "MUSTAQIL ISH"
	StudentName  string
	University   string
	TeacherName  string
	Year         string
	Introduction []Paragraph
	Chapters     []Section
	Conclusion   []Paragraph
	References   []string
}

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

// OOXML ichki yo'llari.
const (
	docRelsPath       = "_rels/.rels"
	docDocumentPath   = "word/document.xml"
	docDocumentRels   = "word/_rels/document.xml.rels"
	docStylesPath     = "word/styles.xml"
	docSettingsPath   = "word/settings.xml"
	docFooterPath     = "word/footer1.xml" // sahifa raqami
	docFooterTitle    = "word/footer2.xml" // titul varaq uchun bo'sh kolontitul
	docCorePropsPath  = corePropsPath
	docAppPropsPath   = appPropsPath
	docContentTypes   = contentTypesPath
	docFileNamePrefix = "document-*.docx"
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
	files := map[string][]byte{
		docContentTypes:  []byte(docxContentTypes),
		docRelsPath:      []byte(docxRootRels),
		docDocumentRels:  []byte(docxDocumentRels),
		docStylesPath:    []byte(docxStyles),
		docSettingsPath:  []byte(docxSettings),
		docFooterPath:    []byte(docxPageNumberFooter),
		docFooterTitle:   []byte(docxEmptyFooter),
		docAppPropsPath:  []byte(docxAppProps),
		docCorePropsPath: []byte(docxCoreProps(p.Topic, p.StudentName, time.Now().UTC())),
		docDocumentPath:  []byte(g.renderDocument(p)),
	}
	order := []string{
		docContentTypes, docRelsPath, docCorePropsPath, docAppPropsPath,
		docDocumentPath, docDocumentRels, docStylesPath, docSettingsPath,
		docFooterPath, docFooterTitle,
	}

	out, err := writeArchive(ctx, g.outputDir, docFileNamePrefix, order, files)
	if err != nil {
		return "", fmt.Errorf("generator: %w", err)
	}
	g.log.Info("docx generated",
		zap.String("file", out),
		zap.Int("chapters", len(p.Chapters)),
		zap.Int("references", len(p.References)),
		zap.Duration("took", time.Since(started)))
	return out, nil
}

// ---------------------------------------------------------------------------
// document.xml
// ---------------------------------------------------------------------------

// Bo'lim sarlavhalari (titul varaqdagi yozuvlar kabi — o'zbek tilida).
const (
	headingPlan         = "REJA"
	headingIntroduction = "KIRISH"
	headingConclusion   = "XULOSA"
	headingReferences   = "FOYDALANILGAN ADABIYOTLAR"
)

// renderDocument hujjat tanasini (titul, reja, matn, adabiyotlar) yig'adi.
func (g *DOCX) renderDocument(p Paper) string {
	var w docBuilder
	w.raw(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`)

	g.renderTitlePage(&w, p)

	// Reja.
	chapterTitles := make([]string, len(p.Chapters))
	for i, ch := range p.Chapters {
		chapterTitles[i] = fmt.Sprintf("%d. %s", i+1, strings.TrimSpace(ch.Title))
	}
	w.heading(headingPlan, true)
	w.para("PlanItem", "", textRun("Kirish", ""))
	for _, t := range chapterTitles {
		w.para("PlanItem", `<w:ind w:left="284" w:firstLine="0"/>`, textRun(t, ""))
	}
	w.para("PlanItem", "", textRun("Xulosa", ""))
	if len(p.References) > 0 {
		w.para("PlanItem", "", textRun("Foydalanilgan adabiyotlar", ""))
	}

	// Asosiy matn.
	w.heading(headingIntroduction, true)
	w.body(p.Introduction)
	for i, ch := range p.Chapters {
		w.heading(chapterTitles[i], false)
		w.body(ch.Paragraphs)
	}
	w.heading(headingConclusion, false)
	w.body(p.Conclusion)

	// Adabiyotlar: raqam + tab, osilgan xatboshi (hanging indent) bilan.
	if len(p.References) > 0 {
		w.heading(headingReferences, true)
		for i, ref := range p.References {
			w.para("Reference", "", textRun(strconv.Itoa(i+1)+".", "")+`<w:r><w:tab/></w:r>`+textRun(ref, ""))
		}
	}

	w.raw(docxSectionProperties + `</w:body></w:document>`)
	return w.String()
}

// Titul varaq elementlari orasidagi vertikal masofalar (twip: 1 sm ≈ 567).
const (
	titleGapAfterUniversity = 3400 // universitet → ish turi
	titleGapBeforeTopic     = 360
	titleGapBeforeSign      = 2600 // mavzu → "Bajardi"
	titleGapBeforeYear      = 2600 // "Tekshirdi" → yil
)

// renderTitlePage titul varaqni chizadi. Uzun universitet nomi yoki mavzu
// bo'lsa ham hammasi bitta sahifaga sig'adigan qilib masofalar tanlangan.
func (g *DOCX) renderTitlePage(w *docBuilder, p Paper) {
	const (
		bold      = `<w:rPr><w:b/><w:bCs/></w:rPr>`
		boldCaps  = `<w:rPr><w:b/><w:bCs/><w:caps/></w:rPr>`
		workType  = `<w:rPr><w:b/><w:bCs/><w:caps/><w:sz w:val="44"/><w:szCs w:val="44"/></w:rPr>`
		topicBold = `<w:rPr><w:b/><w:bCs/><w:sz w:val="32"/><w:szCs w:val="32"/></w:rPr>`
	)
	if g.header != "" {
		w.para("TitleCenter", "", textRun(g.header, boldCaps))
	}
	w.para("TitleCenter", spacingBefore(240), textRun(p.University, boldCaps))
	w.para("TitleCenter", spacingBefore(titleGapAfterUniversity), textRun(p.WorkType, workType))
	w.para("TitleCenter", spacingBefore(titleGapBeforeTopic), textRun("Mavzu: ", topicBold)+textRun(p.Topic, topicBold))
	w.para("TitleSign", spacingBefore(titleGapBeforeSign), textRun("Bajardi: ", bold)+textRun(p.StudentName, ""))
	w.para("TitleSign", spacingBefore(120), textRun("Tekshirdi: ", bold)+textRun(p.TeacherName, ""))
	w.para("TitleCenter", spacingBefore(titleGapBeforeYear), textRun(p.Year+"-yil", bold))
}

// docBuilder — document.xml ni bo'lakma-bo'lak yig'uvchi yordamchi.
type docBuilder struct {
	strings.Builder
}

func (w *docBuilder) raw(s string) { w.WriteString(s) }

// para paragraf yozadi. extraPPr — pStyle dan keyingi qo'shimcha xususiyatlar;
// OOXML sxemasidagi tartibga rioya qilinishi kerak (spacing → ind → jc).
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

// docxSectionProperties — sahifa o'lchami (A4), hoshiyalar va kolontitullar.
// w:titlePg — birinchi (titul) sahifa uchun alohida, bo'sh kolontitul.
// Raqamlash titul varaqdan boshlanadi, shuning uchun reja 2-sahifa bo'ladi.
const docxSectionProperties = `<w:sectPr>` +
	`<w:footerReference w:type="default" r:id="rId3"/>` +
	`<w:footerReference w:type="first" r:id="rId4"/>` +
	`<w:pgSz w:w="11906" w:h="16838"/>` +
	`<w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1701" w:header="709" w:footer="567" w:gutter="0"/>` +
	`<w:pgNumType w:start="1"/>` +
	`<w:titlePg/>` +
	`</w:sectPr>`

// ---------------------------------------------------------------------------
// Paketning qolgan qismlari (o'zgarmas XML)
// ---------------------------------------------------------------------------

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const docxContentTypes = xmlHeader +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
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

const docxDocumentRels = xmlHeader +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>` +
	`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>` +
	`<Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer2.xml"/>` +
	`</Relationships>`

const docxSettings = xmlHeader +
	`<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:zoom w:percent="100"/>` +
	`<w:defaultTabStop w:val="709"/>` +
	`<w:characterSpacingControl w:val="doNotCompress"/>` +
	`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat>` +
	`</w:settings>`

// docxStyles — hujjat uslublari. Elementlar tartibi OOXML sxemasiga mos
// (Word tartib buzilgan faylni "shikastlangan" deb ochmasligi mumkin).
const docxStyles = xmlHeader +
	`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:docDefaults>` +
	`<w:rPrDefault><w:rPr>` +
	`<w:rFonts w:ascii="Times New Roman" w:eastAsia="Times New Roman" w:hAnsi="Times New Roman" w:cs="Times New Roman"/>` +
	`<w:sz w:val="28"/><w:szCs w:val="28"/>` +
	`<w:lang w:val="uz-Latn-UZ" w:eastAsia="en-US" w:bidi="ar-SA"/>` +
	`</w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="0" w:line="360" w:lineRule="auto"/></w:pPr></w:pPrDefault>` +
	`</w:docDefaults>` +

	// Asosiy matn: xatboshi 1,25 sm, ikki tomonga tekislash, 1,5 interval.
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal">` +
	`<w:name w:val="Normal"/><w:qFormat/>` +
	`<w:pPr><w:widowControl/><w:ind w:firstLine="709"/><w:jc w:val="both"/></w:pPr>` +
	`</w:style>` +

	// Bo'lim sarlavhasi: markazda, qalin, keyingi paragrafdan ajralmaydi.
	`<w:style w:type="paragraph" w:styleId="Heading1">` +
	`<w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="9"/><w:qFormat/>` +
	`<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="480" w:after="240"/>` +
	`<w:ind w:firstLine="0"/><w:jc w:val="center"/><w:outlineLvl w:val="0"/></w:pPr>` +
	`<w:rPr><w:b/><w:bCs/></w:rPr>` +
	`</w:style>` +

	// Titul varaq: markazlashtirilgan matn.
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="TitleCenter">` +
	`<w:name w:val="Title Center"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:spacing w:line="276" w:lineRule="auto"/><w:ind w:firstLine="0"/><w:jc w:val="center"/></w:pPr>` +
	`</w:style>` +

	// Titul varaq: "Bajardi / Tekshirdi" bloki (o'ng tomonda).
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="TitleSign">` +
	`<w:name w:val="Title Signature"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:spacing w:line="276" w:lineRule="auto"/><w:ind w:left="4820" w:firstLine="0"/><w:jc w:val="left"/></w:pPr>` +
	`</w:style>` +

	// Reja bandlari.
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="PlanItem">` +
	`<w:name w:val="Plan Item"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:ind w:left="0" w:firstLine="0"/><w:jc w:val="left"/></w:pPr>` +
	`</w:style>` +

	// Adabiyotlar: osilgan xatboshi.
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Reference">` +
	`<w:name w:val="Reference"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:ind w:left="567" w:hanging="567"/></w:pPr>` +
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

// docxPageNumberFooter — markazda sahifa raqami (PAGE maydoni).
const docxPageNumberFooter = xmlHeader +
	`<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
	`<w:p><w:pPr><w:pStyle w:val="Footer"/></w:pPr>` +
	`<w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
	`<w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r>` +
	`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
	`<w:r><w:t>2</w:t></w:r>` +
	`<w:r><w:fldChar w:fldCharType="end"/></w:r>` +
	`</w:p></w:ftr>`

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
