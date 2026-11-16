// Package generator tayyor fayllarni yaratadi:
//   - pptx.go    — taqdimot (.pptx): paketni yig'ish;
//   - themes.go  — taqdimot dizayn mavzulari (ranglar, shriftlar, titul va yakuniy slayd);
//   - layouts.go — mazmunli slaydlar maketlari (reja, kartochkalar, jarayon, taqqoslash ...);
//   - layouts_extra.go — qo'shimcha maketlar (vaqt chizig'i, piramida, matritsa, sikl ...);
//   - themes_extra.go, themes_photo.go — tuzilishi boshqacha va rasmli dizaynlar;
//   - drawing.go — slaydlarni chizish uchun DrawingML primitivlari;
//   - docx.go    — mustaqil ish / amaliy topshiriq (.docx) noldan;
//   - ooxml.go   — ikkalasi uchun umumiy yordamchilar (ZIP yozish, XML ekranlash).
//
// PPTX — bu XML fayllardan iborat ZIP arxiv (Office Open XML). Generator
// tashqi kutubxonalarsiz, faqat archive/zip va matn/XML amallari bilan ishlaydi:
//
//  1. Asosiy paket (templates/template.pptx) ishga tushishda bir marta
//     xotiraga o'qiladi: undan slayd master, maketlar va mavzu (theme) olinadi,
//     shablondagi slaydlar esa olib tashlanadi.
//  2. Har bir slayd tanlangan dizayn mavzusida koddan chiziladi: titul,
//     AI yozgan mazmunli slaydlar (har biri o'z maketida) va yakuniy slayd.
//  3. theme1.xml ranglari va shriftlari tanlangan mavzuga moslanadi — foydalanuvchi
//     PowerPoint'da yangi element qo'shsa, u ham dizaynga mos chiqadi.
//  4. [Content_Types].xml, presentation.xml va uning .rels fayli yangi
//     slaydlar ro'yxatiga moslab qayta yoziladi.
package generator

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/lang"
)

// OOXML ichki yo'llari va turlari.
const (
	contentTypesPath = "[Content_Types].xml"
	presentationPath = "ppt/presentation.xml"
	presentationRels = "ppt/_rels/presentation.xml.rels"
	appPropsPath     = "docProps/app.xml"
	corePropsPath    = "docProps/core.xml"

	relTypeSlide       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"
	relTypeSlideLayout = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout"
	relTypeTheme       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme"
	relTypeImage       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
	contentTypeSlide   = "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"

	maxTemplateBytes = 50 << 20 // shablon hajmi chegarasi (zip-bomb'dan himoya)
	topicShortRunes  = 90       // pastki kolontitul uchun qisqartirilgan mavzu
	footerUniRunes   = 60       // pastki kolontituldagi universitet nomi
	firstSlideID     = 256      // OOXML talabi: slayd ID lari 256 dan boshlanadi
	maxMediaBytes    = 35 << 20 // rasmlar umumiy hajmi (Telegram bot fayl chegarasi — 50 MB)

	// extraSlides — AI yozmaydigan slaydlar soni: titul va yakuniy slayd.
	extraSlides = 2
)

// Item — slayddagi bitta element (punkt, kartochka, bosqich, fakt ...).
type Item struct {
	Heading string   // qisqa sarlavha
	Text    string   // izoh
	Value   string   // katta raqam yoki qiymat ("facts" maketi uchun)
	Points  []string // ichki punktlar ("comparison" maketi uchun)
}

// Slide — bitta mazmunli slayd.
type Slide struct {
	Layout    string // Layout* konstantalaridan biri; noma'lum bo'lsa — LayoutBullets
	Title     string
	Lead      string // sarlavha ostidagi kirish gapi
	Items     []Item
	Highlight string // asosiy fikr, ta'rif yoki yakuniy xulosa
	// Image — slayd yonidagi rasm (faqat rasmli dizaynlarda va mos maketlarda
	// chiziladi — SlideTakesPhoto); nil bo'lsa slayd rasmsiz.
	Image *Picture
}

// Deck — taqdimotni yaratish uchun barcha ma'lumotlar.
type Deck struct {
	Topic       string
	Subtitle    string // titul slayddagi qo'shimcha sarlavha (ixtiyoriy)
	WorkType    string // titul slaydda ko'rinadigan nom, masalan "TAQDIMOT"
	StudentName string
	University  string
	TeacherName string
	Year        string
	Theme       string // ThemeAcademic, ThemeModern ...; bo'sh bo'lsa — DefaultTheme
	// Lang — titul va yakuniy slayddagi yozuvlar tili (lang.Code); bo'sh bo'lsa — o'zbek (lotin).
	Lang string
	// CoverImage — titul va yakuniy slayd rasmi (faqat rasmli dizaynlarda);
	// nil bo'lsa, rasm o'rnida dizaynning bezakli foni chiziladi.
	CoverImage *Picture
	Slides     []Slide
}

// labels taqdimot tiliga mos qotirilgan yozuvlar ("Bajardi", "Rahmat" ...).
func (d *Deck) labels() lang.Labels { return lang.Get(d.Lang).Labels }

// PPTX — .pptx fayllarni yaratuvchi generator.
// Build bir vaqtning o'zida bir nechta goroutine'dan chaqirilishi mumkin:
// asosiy paket faqat o'qiladi.
type PPTX struct {
	base      *basePackage
	outputDir string
	log       *zap.Logger
}

// NewPPTX asosiy paketni o'qiydi, tekshiradi va generatorni qaytaradi.
// outputDir bo'sh bo'lsa, tizimning vaqtinchalik papkasi ishlatiladi.
func NewPPTX(templatePath, outputDir string, log *zap.Logger) (*PPTX, error) {
	base, err := loadBase(templatePath)
	if err != nil {
		return nil, fmt.Errorf("generator: %w", err)
	}

	outputDir, err = prepareOutputDir(outputDir)
	if err != nil {
		return nil, fmt.Errorf("generator: %w", err)
	}

	log.Info("pptx base package loaded",
		zap.String("path", templatePath),
		zap.String("layout", base.layoutTarget),
		zap.Int("themes", len(themes)),
		zap.String("output_dir", outputDir))

	return &PPTX{base: base, outputDir: outputDir, log: log.Named("pptx")}, nil
}

// ContentSlides foydalanuvchi so'ragan umumiy slaydlar sonidan nechtasi
// mazmunli (AI) slayd bo'lishini hisoblaydi: titul va yakuniy slayd ayiriladi.
func (g *PPTX) ContentSlides(total int) int {
	return max(1, total-extraSlides)
}

// Build Deck asosida yangi .pptx faylni outputDir ichida yaratadi va uning
// yo'lini qaytaradi. Faylni o'chirish (os.Remove) chaqiruvchining vazifasi.
func (g *PPTX) Build(ctx context.Context, deck Deck) (string, error) {
	if len(deck.Slides) == 0 {
		return "", errors.New("generator: slaydlar ro'yxati bo'sh")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	started := time.Now()
	t := themeByID(deck.Theme)
	limitPictures(&deck, maxMediaBytes)
	slides := renderDeck(t, &deck)

	files, newNames := g.assemble(t, &deck, slides)
	out, err := g.writeZip(ctx, files, newNames)
	if err != nil {
		return "", fmt.Errorf("generator: %w", err)
	}

	g.log.Info("pptx generated",
		zap.String("file", out),
		zap.String("theme", t.ID),
		zap.Int("slides", len(slides)),
		zap.Int("pictures", countPictures(slides)),
		zap.Duration("took", time.Since(started)))
	return out, nil
}

// slidePart — chizilgan slayd: XML va unga joylangan rasmlar (rId tartibida).
type slidePart struct {
	xml  string
	pics []*Picture
}

func countPictures(slides []slidePart) int {
	seen := make(map[*Picture]bool)
	for _, s := range slides {
		for _, p := range s.pics {
			seen[p] = true
		}
	}
	return len(seen)
}

// renderDeck barcha slaydlarni (titul, mazmunli, yakuniy) XML ko'rinishida chizadi.
func renderDeck(t *theme, d *Deck) []slidePart {
	total := len(d.Slides) + extraSlides
	out := make([]slidePart, 0, total)
	done := func(c *canvas) slidePart { return slidePart{xml: c.xml(t.transition), pics: c.pics} }

	c := newCanvas(paint{})
	t.cover(t, c, d)
	out = append(out, done(c))

	topic := truncateRunes(d.Topic, topicShortRunes)
	uni := truncateRunes(d.University, footerUniRunes)
	for i, s := range d.Slides {
		s = normalizeSlide(s)
		var photo *Picture
		if t.photos && s.Image != nil && t.photoFits(s) {
			photo = s.Image
		}
		c := newCanvas(paint{})
		body := t.chrome(t, c, slideInfo{
			title: s.Title, lead: s.Lead,
			num: i + 2, total: total,
			topic: topic, university: uni,
			photo: photo,
		})
		renderContent(t, c, body, &s)
		out = append(out, done(c))
	}

	c = newCanvas(paint{})
	t.closing(t, c, d)
	out = append(out, done(c))
	return out
}

// assemble yakuniy arxiv fayllari to'plamini tayyorlaydi.
// Qaytariladigan newNames — yangi qo'shilgan slayd fayllari nomlari (tartib bilan).
func (g *PPTX) assemble(t *theme, deck *Deck, slides []slidePart) (map[string][]byte, []string) {
	b := g.base
	files := make(map[string][]byte, len(b.files)+2*len(slides))
	for name, data := range b.files {
		files[name] = data // asosiy paket baytlari o'zgartirilmaydi, faqat almashtiriladi
	}

	const relsHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	layoutRel := `<Relationship Id="rId1" Type="` + relTypeSlideLayout + `" Target="` + b.layoutTarget + `"/>`

	var (
		overrides strings.Builder
		presRels  strings.Builder
		ids       strings.Builder
		newNames  = make([]string, 0, 2*len(slides))
		media     = make(map[*Picture]string) // rasm -> ppt/media/ ichidagi fayl nomi
		mediaExt  = make(map[string]string)   // kengaytma -> content type
	)
	for i, x := range slides {
		n := i + 1
		slidePath := fmt.Sprintf("ppt/slides/slide%d.xml", n)
		relsPath := slideRelsPath(slidePath)

		var rels strings.Builder
		rels.WriteString(relsHead + layoutRel)
		for j, p := range x.pics {
			name, ok := media[p]
			if !ok {
				name = uniqueMediaName(files, len(media)+1, p.Ext)
				media[p] = name
				mediaExt[p.Ext] = p.contentType()
				files["ppt/media/"+name] = p.Data
				newNames = append(newNames, "ppt/media/"+name)
			}
			fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="%s" Target="../media/%s"/>`, j+2, relTypeImage, name)
		}
		rels.WriteString(`</Relationships>`)

		files[slidePath] = []byte(x.xml)
		files[relsPath] = []byte(rels.String())
		newNames = append(newNames, slidePath, relsPath)

		rid := "rId" + strconv.Itoa(b.nextRelID+i)
		fmt.Fprintf(&overrides, `<Override PartName="/%s" ContentType="%s"/>`, slidePath, contentTypeSlide)
		fmt.Fprintf(&presRels, `<Relationship Id="%s" Type="%s" Target="slides/slide%d.xml"/>`, rid, relTypeSlide, n)
		fmt.Fprintf(&ids, `<p:sldId id="%d" r:id="%s"/>`, firstSlideID+i, rid)
	}

	files[contentTypesPath] = []byte(addDefaultTypes(
		strings.Replace(b.contentTypes, "</Types>", overrides.String()+"</Types>", 1), mediaExt))
	files[presentationRels] = []byte(strings.Replace(b.presRels, "</Relationships>", presRels.String()+"</Relationships>", 1))
	files[presentationPath] = []byte(strings.Replace(b.presentation, sldIDLstMarker, "<p:sldIdLst>"+ids.String()+"</p:sldIdLst>", 1))

	if th, ok := files[b.themePath]; ok {
		files[b.themePath] = []byte(applyTheme(string(th), t))
	}
	if app, ok := files[appPropsPath]; ok {
		a := slidesCountRe.ReplaceAllLiteralString(string(app), fmt.Sprintf("<Slides>%d</Slides>", len(slides)))
		a = notesCountRe.ReplaceAllLiteralString(a, "<Notes>0</Notes>")
		a = presFormatRe.ReplaceAllLiteralString(a, "<PresentationFormat>Widescreen</PresentationFormat>")
		files[appPropsPath] = []byte(a)
	}
	if core, ok := files[corePropsPath]; ok {
		now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		c := setElementText(string(core), coreTitleRe, "dc:title", deck.Topic)
		c = setElementText(c, coreCreatorRe, "dc:creator", deck.StudentName)
		c = setElementText(c, coreModifiedByRe, "cp:lastModifiedBy", deck.StudentName)
		c = setElementText(c, coreCreatedRe, "dcterms:created", now)
		c = setElementText(c, coreModifiedRe, "dcterms:modified", now)
		files[corePropsPath] = []byte(c)
	}
	return files, newNames
}

// addDefaultTypes [Content_Types].xml ga rasm kengaytmalari uchun Default
// yozuvlarini qo'shadi (shablonda bo'lmasa). An'anaga ko'ra Default yozuvlari
// Override yozuvlaridan oldin turadi.
func addDefaultTypes(ct string, exts map[string]string) string {
	names := make([]string, 0, len(exts))
	for ext := range exts {
		names = append(names, ext)
	}
	sort.Strings(names)
	var defs strings.Builder
	for _, ext := range names {
		re := regexp.MustCompile(`(?i)<Default\b[^>]*\bExtension="` + regexp.QuoteMeta(ext) + `"`)
		if !re.MatchString(ct) {
			fmt.Fprintf(&defs, `<Default Extension="%s" ContentType="%s"/>`, ext, exts[ext])
		}
	}
	if defs.Len() == 0 {
		return ct
	}
	at := strings.Index(ct, "<Override")
	if at < 0 {
		at = strings.LastIndex(ct, "</Types>")
	}
	return ct[:at] + defs.String() + ct[at:]
}

// uniqueMediaName asosiy paketdagi fayllar bilan to'qnashmaydigan media fayl nomini tanlaydi.
func uniqueMediaName(files map[string][]byte, n int, ext string) string {
	for {
		name := fmt.Sprintf("image%d.%s", n, ext)
		if _, exists := files["ppt/media/"+name]; !exists {
			return name
		}
		n += 1000
	}
}

// writeZip arxivni vaqtinchalik faylga yozadi: [Content_Types].xml an'anaviy
// ravishda birinchi, keyin asosiy paketdagi tartib, oxirida yangi slaydlar.
func (g *PPTX) writeZip(ctx context.Context, files map[string][]byte, newNames []string) (string, error) {
	order := make([]string, 0, len(files))
	order = append(order, contentTypesPath)
	for _, name := range g.base.order {
		if name != contentTypesPath {
			order = append(order, name)
		}
	}
	order = append(order, newNames...)
	return writeArchive(ctx, g.outputDir, "presentation-*.pptx", order, files)
}

// applyTheme theme1.xml dagi rang sxemasi va shriftlarni mavzuga moslaydi.
func applyTheme(x string, t *theme) string {
	p := t.pal
	dk1, lt1, dk2, lt2 := p.text, "FFFFFF", p.primary, p.bg
	if t.dark {
		// To'q mavzuda slaydlar rang xaritasini almashtiradi (bg1=dk1, tx1=lt1).
		dk1, lt1, dk2, lt2 = p.bg, p.text, p.surface, p.muted
	}
	s := p.series
	accents := []string{s[0], s[1%len(s)], s[2%len(s)], s[3%len(s)], p.accent, p.secondary}

	var b strings.Builder
	fmt.Fprintf(&b, `<a:clrScheme name="%s">`, xmlEscape(t.Name))
	for _, el := range []struct{ tag, val string }{
		{"dk1", dk1}, {"lt1", lt1}, {"dk2", dk2}, {"lt2", lt2},
		{"accent1", accents[0]}, {"accent2", accents[1]}, {"accent3", accents[2]},
		{"accent4", accents[3]}, {"accent5", accents[4]}, {"accent6", accents[5]},
		{"hlink", p.primary}, {"folHlink", p.secondary},
	} {
		fmt.Fprintf(&b, `<a:%s><a:srgbClr val="%s"/></a:%s>`, el.tag, el.val, el.tag)
	}
	b.WriteString(`</a:clrScheme>`)

	x = clrSchemeRe.ReplaceAllLiteralString(x, b.String())
	x = majorLatinRe.ReplaceAllString(x, "${1}"+xmlEscape(t.headFont)+"${2}")
	x = minorLatinRe.ReplaceAllString(x, "${1}"+xmlEscape(t.bodyFont)+"${2}")
	return x
}

// ---------------------------------------------------------------------------
// Asosiy paketni yuklash
// ---------------------------------------------------------------------------

// sldIDLstMarker — presentation.xml da slaydlar ro'yxati o'rni.
const sldIDLstMarker = "<!--SLIDES-->"

// basePackage — xotiraga o'qilgan va slaydlardan tozalangan asosiy paket.
type basePackage struct {
	files        map[string][]byte // slayd va izoh (notes) fayllaridan tashqari barcha fayllar
	order        []string          // fayllarning asl tartibi
	contentTypes string            // slayd yozuvlari olib tashlangan [Content_Types].xml
	presRels     string            // slayd bog'lanishlari olib tashlangan presentation.xml.rels
	presentation string            // ppt/presentation.xml (slaydlar o'rnida sldIDLstMarker)
	nextRelID    int               // yangi rId lar uchun boshlang'ich raqam
	layoutTarget string            // slaydlar bog'lanadigan maket: "../slideLayouts/slideLayout7.xml"
	themePath    string            // "ppt/theme/theme1.xml"
}

func loadBase(templatePath string) (*basePackage, error) {
	zr, err := zip.OpenReader(templatePath)
	if err != nil {
		return nil, fmt.Errorf("shablonni ochib bo'lmadi (%s): %w", templatePath, err)
	}
	defer zr.Close()

	files := make(map[string][]byte, len(zr.File))
	order := make([]string, 0, len(zr.File))
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		total += f.UncompressedSize64
		if total > maxTemplateBytes {
			return nil, fmt.Errorf("shablon juda katta (> %d MB)", maxTemplateBytes>>20)
		}
		data, err := readZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("shablon fayli %s: %w", f.Name, err)
		}
		files[f.Name] = data
		order = append(order, f.Name)
	}

	for _, required := range []string{contentTypesPath, presentationPath, presentationRels} {
		if _, ok := files[required]; !ok {
			return nil, fmt.Errorf("shablon yaroqsiz: %s topilmadi", required)
		}
	}

	b := &basePackage{}
	if b.layoutTarget, err = blankLayout(files); err != nil {
		return nil, err
	}

	// Shablondagi slaydlar va ularning izohlari (notes) olib tashlanadi.
	removed := make(map[string]bool)
	for name := range files {
		if strings.HasPrefix(name, "ppt/slides/") || strings.HasPrefix(name, "ppt/notesSlides/") {
			removed[name] = true
		}
	}
	b.files = make(map[string][]byte, len(files))
	for _, name := range order {
		if !removed[name] {
			b.files[name] = files[name]
			b.order = append(b.order, name)
		}
	}

	// [Content_Types].xml dan olib tashlangan qismlar yozuvlarini o'chiramiz.
	b.contentTypes = overrideRe.ReplaceAllStringFunc(string(files[contentTypesPath]), func(el string) string {
		if m := partNameRe.FindStringSubmatch(el); m != nil && removed[strings.TrimPrefix(m[1], "/")] {
			return ""
		}
		return el
	})

	// presentation.xml.rels dan slayd bog'lanishlarini o'chirib, maksimal rId ni topamiz.
	maxID := 0
	b.presRels = relationshipRe.ReplaceAllStringFunc(string(files[presentationRels]), func(el string) string {
		typ := relTypeRe.FindStringSubmatch(el)
		if typ != nil && typ[1] == relTypeSlide {
			return ""
		}
		if typ != nil && typ[1] == relTypeTheme && b.themePath == "" {
			if m := relTargetRe.FindStringSubmatch(el); m != nil {
				b.themePath = resolveTarget("ppt", m[1])
			}
		}
		if m := relIDRe.FindStringSubmatch(el); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > maxID {
				maxID = n
			}
		}
		return el
	})
	b.nextRelID = maxID + 1

	// Slaydlar ro'yxati o'rniga marker; slayd o'lchami — 16:9.
	pres := string(files[presentationPath])
	switch {
	case sldIDLstRe.MatchString(pres):
		pres = sldIDLstRe.ReplaceAllLiteralString(pres, sldIDLstMarker)
	case strings.Contains(pres, "<p:sldSz"):
		pres = strings.Replace(pres, "<p:sldSz", sldIDLstMarker+"<p:sldSz", 1)
	default:
		return nil, errors.New("shablon yaroqsiz: presentation.xml da <p:sldSz> yo'q")
	}
	b.presentation = sldSzRe.ReplaceAllLiteralString(pres, fmt.Sprintf(`<p:sldSz cx="%d" cy="%d"/>`, slideWidth, slideHeight))

	if !strings.Contains(b.contentTypes, "</Types>") || !strings.Contains(b.presRels, "</Relationships>") {
		return nil, errors.New("shablon yaroqsiz: [Content_Types].xml yoki presentation.xml.rels tuzilishi kutilganidek emas")
	}
	return b, nil
}

// blankLayout "bo'sh" (blank) slayd maketini topadi; bo'lmasa — birinchi maketni.
func blankLayout(files map[string][]byte) (string, error) {
	var layouts []string
	for name := range files {
		if layoutFileRe.MatchString(name) {
			layouts = append(layouts, name)
		}
	}
	if len(layouts) == 0 {
		return "", errors.New("shablon yaroqsiz: slayd maketlari (slideLayouts) topilmadi")
	}
	sort.Slice(layouts, func(i, j int) bool {
		return layoutNum(layouts[i]) < layoutNum(layouts[j])
	})
	chosen := layouts[0]
	for _, name := range layouts {
		if blankLayoutRe.Match(files[name]) {
			chosen = name
			break
		}
	}
	return "../slideLayouts/" + path.Base(chosen), nil
}

func layoutNum(name string) int {
	m := layoutFileRe.FindStringSubmatch(name)
	n, _ := strconv.Atoi(m[1])
	return n
}

// ---------------------------------------------------------------------------
// Yordamchi funksiyalar
// ---------------------------------------------------------------------------

var (
	sldIDLstRe     = regexp.MustCompile(`(?s)<p:sldIdLst>.*?</p:sldIdLst>|<p:sldIdLst\s*/>`)
	sldSzRe        = regexp.MustCompile(`<p:sldSz\b[^>]*/>`)
	relationshipRe = regexp.MustCompile(`<Relationship\b[^>]*>`)
	relTypeRe      = regexp.MustCompile(`\bType="([^"]+)"`)
	relIDRe        = regexp.MustCompile(`\bId="rId(\d+)"`)
	relTargetRe    = regexp.MustCompile(`\bTarget="([^"]+)"`)
	overrideRe     = regexp.MustCompile(`<Override\b[^>]*>`)
	partNameRe     = regexp.MustCompile(`\bPartName="([^"]+)"`)
	layoutFileRe   = regexp.MustCompile(`^ppt/slideLayouts/slideLayout(\d+)\.xml$`)
	blankLayoutRe  = regexp.MustCompile(`<p:sldLayout\b[^>]*\btype="blank"`)
	slidesCountRe  = regexp.MustCompile(`<Slides>\d+</Slides>`)
	notesCountRe   = regexp.MustCompile(`<Notes>\d+</Notes>`)
	presFormatRe   = regexp.MustCompile(`<PresentationFormat>[^<]*</PresentationFormat>`)
	clrSchemeRe    = regexp.MustCompile(`(?s)<a:clrScheme\b.*?</a:clrScheme>`)
	majorLatinRe   = regexp.MustCompile(`(<a:majorFont>\s*<a:latin typeface=")[^"]*(")`)
	minorLatinRe   = regexp.MustCompile(`(<a:minorFont>\s*<a:latin typeface=")[^"]*(")`)

	coreTitleRe      = elementRe("dc:title")
	coreCreatorRe    = elementRe("dc:creator")
	coreModifiedByRe = elementRe("cp:lastModifiedBy")
	coreCreatedRe    = elementRe("dcterms:created")
	coreModifiedRe   = elementRe("dcterms:modified")
)

// elementRe <tag ...>matn</tag> yoki <tag .../> elementini topuvchi ifoda.
func elementRe(tag string) *regexp.Regexp {
	q := regexp.QuoteMeta(tag)
	return regexp.MustCompile(`(?s)<` + q + `(\s[^>]*?)?(?:/>|>.*?</` + q + `>)`)
}

// setElementText element matnini almashtiradi, atributlarini saqlab qoladi.
func setElementText(doc string, re *regexp.Regexp, tag, value string) string {
	return re.ReplaceAllStringFunc(doc, func(el string) string {
		m := re.FindStringSubmatch(el)
		return "<" + tag + m[1] + ">" + xmlEscape(value) + "</" + tag + ">"
	})
}

// slideRelsPath "ppt/slides/slide3.xml" -> "ppt/slides/_rels/slide3.xml.rels".
func slideRelsPath(p string) string {
	dir, file := path.Split(p)
	return dir + "_rels/" + file + ".rels"
}

// resolveTarget .rels dagi nisbiy yoki mutlaq Target ni arxiv ichidagi yo'lga aylantiradi.
func resolveTarget(baseDir, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	return path.Clean(path.Join(baseDir, target))
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(rc, maxTemplateBytes+1)); err != nil {
		return nil, err
	}
	if buf.Len() > maxTemplateBytes {
		return nil, errors.New("fayl juda katta")
	}
	return buf.Bytes(), nil
}
