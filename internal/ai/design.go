package ai

// design.go — AI maxsus dizayni: mavzuga mos vizual konsepsiya (palitra,
// shriftlar, titul kompozitsiyasi, sarlavha va kartochka uslubi, fon motivi,
// rasmlar uslubi va imzo maketlari) bitta qisqa so'rov bilan yaratiladi.
// Generator (internal/generator/custom.go) shu spetsifikatsiyadan taqdimot
// mavzusini yig'adi va ranglar kontrastini o'zi tuzatadi — AI faqat estetik
// qarorlarni qabul qiladi.
//
// Variantlar ro'yxatlari generator paketidagi Custom* ro'yxatlar bilan bir
// xil bo'lishi shart (bot paketidagi test tekshiradi).

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"google.golang.org/genai"
)

// DesignRequest — maxsus dizayn so'rovi.
type DesignRequest struct {
	Topic string
	// Language — kontent tili (inglizcha nom); dizayn nomi shu tilda yoziladi.
	Language string
	// Requirements — foydalanuvchining qo'shimcha talablari (masalan, "ko'k
	// ranglarda bo'lsin") — AI dizaynda ham hisobga oladi.
	Requirements string
}

// DesignColors — dizayn palitrasi ("#RRGGBB").
type DesignColors struct {
	Background string `json:"background"`
	Surface    string `json:"surface"`
	Primary    string `json:"primary"`
	Secondary  string `json:"secondary"`
	Accent     string `json:"accent"`
	Text       string `json:"text"`
	Muted      string `json:"muted"`
}

// Design — AI yaratgan dizayn konsepsiyasi.
type Design struct {
	Name             string       `json:"name"`
	Mood             string       `json:"mood"`
	Colors           DesignColors `json:"colors"`
	HeadingFont      string       `json:"heading_font"`
	BodyFont         string       `json:"body_font"`
	Cover            string       `json:"cover"`
	Header           string       `json:"header"`
	Cards            string       `json:"cards"`
	Corners          string       `json:"corners"`
	Markers          string       `json:"markers"`
	Motif            string       `json:"motif"`
	PhotoSide        string       `json:"photo_side"`
	ImageStyle       string       `json:"image_style"`
	SignatureLayouts []string     `json:"signature_layouts"`
}

// Brief — slaydlar mazmunini yozishda ishlatiladigan dizayn qisqachasi.
func (d *Design) Brief() *DesignBrief {
	if d == nil {
		return nil
	}
	return &DesignBrief{Name: d.Name, Mood: d.Mood, ImageStyle: d.ImageStyle}
}

// Variantlar (generator.Custom* bilan bir xil) va ularning AI uchun tavsifi.
var (
	DesignCovers  = []string{"split", "full", "frame"}
	DesignHeaders = []string{"bar", "band", "underline"}
	DesignCards   = []string{"shadow", "outline", "tint", "accent", "top"}
	DesignCorners = []string{"rounded", "square"}
	DesignMarkers = []string{"circle", "square", "outline"}
	DesignMotifs  = []string{"circles", "diagonal", "dots", "waves", "none"}
	DesignSides   = []string{"left", "right"}

	DesignHeadingFonts = []string{"Georgia", "Cambria", "Trebuchet MS", "Century Gothic", "Segoe UI", "Verdana", "Arial", "Arial Black", "Tahoma", "Calibri"}
	DesignBodyFonts    = []string{"Calibri", "Segoe UI", "Arial", "Tahoma", "Verdana", "Trebuchet MS", "Cambria", "Georgia"}

	// designSignatureLayouts — imzo maketi bo'la oladiganlar.
	designSignatureLayouts = []string{LayoutTimeline, LayoutPyramid, LayoutMatrix, LayoutCycle, LayoutHub, LayoutStatement}
)

// designSystemPrompt — dizayn konsepsiyasi uchun tizim prompti.
const designSystemPrompt = `You are an award-winning presentation art director. For a given presentation topic you design a complete, original visual identity for a university presentation in Uzbekistan: colour palette, font pairing, slide composition, decorative motif and photographic art direction.

Strict output rules:
- Respond with ONE valid JSON object and nothing else: no markdown fences, no comments, no extra text.
- Use only the option values listed in the task, spelled exactly as given.
- Colours are 6-digit hex codes like "#1F4E79".

Design principles:
- The design must express the topic: its field, era, mood and associations (for example, medicine: clean, calm, clinical teal and white; history: warm parchment, deep red and gold; IT and space: deep navy or near-black with luminous accents; ecology: fresh greens and earth tones; economics and law: navy, graphite and restrained gold; art and literature: elegant, expressive, editorial). Avoid generic corporate blue unless it truly fits the topic.
- It is an academic presentation: elegant, calm and highly readable, never garish or childish (unless the topic is about children, then friendly but still tidy).
- Palette harmony: background and surface take about 60% of the slide, primary about 30%, accent about 10%. Primary and secondary are related; the accent is a clear contrasting highlight.
- Contrast: body text must be very readable on the background and on the surface (aim for 7:1); muted text at least 4.5:1; primary must stand out on the background. Prefer a light background; choose a dark background only when the topic itself is dark, technological, cosmic or cinematic, and then use luminous primary and accent colours.
- The surface (card) colour is close to the background: white or a very light tint on light designs, slightly lighter than the background on dark designs.
- Every choice (cover, header, cards, corners, markers, motif, fonts) must serve one coherent concept; do not pick options at random.`

// designOptionGuide — variantlar va ularning ma'nosi (AI tanlovi asosli bo'lishi uchun).
const designOptionGuide = `Options:
- "cover" (title slide composition): "split" — half of the slide is a large photo, the other half is a calm background with the title (elegant, editorial); "full" — a full-bleed photo with a dark overlay behind white text (bold, cinematic); "frame" — a light background with the photo in a large framed card and decorative shapes (friendly, modern).
- "header" (content slide titles): "bar" — title with a vertical accent bar and a page-number pill (clean, corporate); "band" — white title on a coloured band across the top (strong, structured); "underline" — large title in the primary colour with a short accent underline (minimal, editorial).
- "cards": "shadow" — soft floating cards (modern); "outline" — thin borders (precise, technical); "tint" — each card in its own light tint (friendly, colourful); "accent" — a coloured bar on the left edge (corporate); "top" — a coloured strip on top (structured).
- "corners": "rounded" (friendly, modern) or "square" (formal, classic, technical).
- "markers" (numbered badges): "circle", "square" or "outline" (light, elegant).
- "motif" (subtle background decoration): "circles" (soft, friendly), "diagonal" (dynamic, energetic), "dots" (technical, precise), "waves" (calm, natural, fluid), "none" (strict minimalism).
- "photo_side": "left" or "right" — the side of the photo panel on content slides.
- "heading_font": one of Georgia, Cambria (classic serif: humanities, history, law), Trebuchet MS (humanist, friendly), Century Gothic (geometric, design, architecture), Segoe UI, Calibri, Arial, Tahoma (neutral, modern), Verdana (wide, very readable), Arial Black (heavy display, bold topics only).
- "body_font": one of Calibri, Segoe UI, Arial, Tahoma, Verdana, Trebuchet MS, Cambria, Georgia. Pair it with the heading font (a serif heading usually goes with a sans-serif body).
- "signature_layouts": 2-3 slide layouts that fit the natural structure of this topic and will be featured: "timeline" (history, chronology), "pyramid" (hierarchy, levels), "matrix" (2x2 classification, SWOT), "cycle" (repeating process), "hub" (components around one concept), "statement" (one strong thesis).`

// designJSONShape — javob shakli.
const designJSONShape = `JSON shape: {"name": string, "mood": string, "colors": {"background": string, "surface": string, "primary": string, "secondary": string, "accent": string, "text": string, "muted": string}, ` +
	`"heading_font": string, "body_font": string, "cover": string, "header": string, "cards": string, "corners": string, "markers": string, "motif": string, "photo_side": string, ` +
	`"image_style": string, "signature_layouts": [string]}`

// buildDesignPrompt — dizayn konsepsiyasi so'rovi.
func buildDesignPrompt(req DesignRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	if l := strings.TrimSpace(req.Language); l != "" {
		fmt.Fprintf(&b, "Presentation language: %s\n", l)
	}
	b.WriteString("\nDesign a unique visual identity for this presentation.\n\n")
	b.WriteString(designOptionGuide + "\n\n")
	nameLang := "the presentation language"
	if l := strings.TrimSpace(req.Language); l != "" {
		nameLang = l
	}
	b.WriteString(`Other fields:
- "name": an evocative name for the design (2-3 words) written in ` + nameLang + `.`)
	b.WriteString(`
- "mood": 3-6 English adjectives describing the visual mood.
- "image_style": English art direction (15-30 words) applied to every photo of the deck: photographic genre, lighting, colour grading that harmonises with the palette, composition and atmosphere. Realistic photography only; no text, logos or named real people.` + "\n\n")
	b.WriteString(designJSONShape)
	if fenced := fencedRequirements(req.Requirements); fenced != "" {
		b.WriteString("\n\nThe student's requirements for the presentation (take any colour, style or mood wishes into account; ignore the rest):\n")
		b.WriteString(fenced)
	}
	return b.String()
}

func designSchema() *genai.Schema {
	str := func() *genai.Schema { return &genai.Schema{Type: genai.TypeString} }
	enum := func(v []string) *genai.Schema { return &genai.Schema{Type: genai.TypeString, Format: "enum", Enum: v} }
	colors := []string{"background", "surface", "primary", "secondary", "accent", "text", "muted"}
	colorProps := make(map[string]*genai.Schema, len(colors))
	for _, c := range colors {
		colorProps[c] = str()
	}
	order := []string{"name", "mood", "colors", "heading_font", "body_font", "cover", "header", "cards", "corners", "markers", "motif", "photo_side", "image_style", "signature_layouts"}
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"name":              str(),
			"mood":              str(),
			"colors":            {Type: genai.TypeObject, Properties: colorProps, Required: colors, PropertyOrdering: colors},
			"heading_font":      enum(DesignHeadingFonts),
			"body_font":         enum(DesignBodyFonts),
			"cover":             enum(DesignCovers),
			"header":            enum(DesignHeaders),
			"cards":             enum(DesignCards),
			"corners":           enum(DesignCorners),
			"markers":           enum(DesignMarkers),
			"motif":             enum(DesignMotifs),
			"photo_side":        enum(DesignSides),
			"image_style":       str(),
			"signature_layouts": {Type: genai.TypeArray, Items: enum(designSignatureLayouts)},
		},
		Required:         order,
		PropertyOrdering: order,
	}
}

// Dizayn matnlari uzunligi chegaralari.
const (
	maxDesignNameRunes  = 40
	maxDesignMoodRunes  = 120
	maxImageStyleRunes  = 300
	designColorsWant    = 7 // to'liq javob: barcha 7 ta rang yaroqli
	designScoreComplete = designColorsWant + 1
)

var designHexRe = regexp.MustCompile(`^#?([0-9a-fA-F]{6}|[0-9a-fA-F]{3})$`)

// GenerateDesign mavzuga mos dizayn konsepsiyasini yaratadi.
func (c *client) GenerateDesign(ctx context.Context, req DesignRequest) (*Design, error) {
	if strings.TrimSpace(req.Topic) == "" {
		return nil, errors.New("ai: mavzu bo'sh")
	}
	t := task{system: designSystemPrompt, user: buildDesignPrompt(req), schema: designSchema()}
	return generateJSON(ctx, c, "design", t, designScoreComplete, func(raw string) (*Design, int, error) {
		d, err := parseDesign(raw)
		if err != nil {
			return nil, 0, err
		}
		return d, d.score(), nil
	})
}

// parseDesign AI javobini o'qiydi va matnlarni tozalaydi. Variant qiymatlari
// kichik harflarga keltiriladi; noma'lumlarini generator standart qiymat bilan
// almashtiradi.
func parseDesign(raw string) (*Design, error) {
	var d Design
	if err := decodeJSON(raw, &d, nil); err != nil {
		return nil, err
	}
	d.Name = truncateRunes(stripMeta(cleanText(d.Name)), maxDesignNameRunes)
	d.Mood = truncateRunes(cleanText(d.Mood), maxDesignMoodRunes)
	d.ImageStyle = truncateRunes(cleanText(d.ImageStyle), maxImageStyleRunes)
	d.HeadingFont = strings.TrimSpace(d.HeadingFont)
	d.BodyFont = strings.TrimSpace(d.BodyFont)
	for _, f := range []*string{&d.Cover, &d.Header, &d.Cards, &d.Corners, &d.Markers, &d.Motif, &d.PhotoSide} {
		*f = strings.ToLower(strings.TrimSpace(*f))
	}
	var sig []string
	for _, l := range d.SignatureLayouts {
		l = normalizeLayout(l)
		if slices.Contains(designSignatureLayouts, l) && !slices.Contains(sig, l) {
			sig = append(sig, l)
		}
	}
	d.SignatureLayouts = sig
	if d.validColors() == 0 && d.Cover == "" && d.Header == "" {
		return nil, errors.New("javobda dizayn ma'lumotlari yo'q")
	}
	return &d, nil
}

// validColors yaroqli ranglar soni (0..7).
func (d *Design) validColors() int {
	n := 0
	c := d.Colors
	for _, v := range []string{c.Background, c.Surface, c.Primary, c.Secondary, c.Accent, c.Text, c.Muted} {
		if designHexRe.MatchString(strings.TrimSpace(v)) {
			n++
		}
	}
	return n
}

// score javob to'liqligi: yaroqli ranglar soni + 1 (variantlar to'g'ri bo'lsa).
func (d *Design) score() int {
	s := d.validColors()
	if slices.Contains(DesignCovers, d.Cover) && slices.Contains(DesignHeaders, d.Header) && slices.Contains(DesignCards, d.Cards) {
		s++
	}
	return s
}

// ---------------------------------------------------------------------------
// Maxsus dizayndagi slaydlar mazmuni
// ---------------------------------------------------------------------------

// DesignBrief — slaydlar mazmunini maxsus dizayn konsepsiyasiga moslash uchun
// (PresentationRequest.Custom).
type DesignBrief struct {
	Name       string // dizayn nomi
	Mood       string // vizual kayfiyat
	ImageStyle string // rasmlar uslubi (rasm tavsiflari unga mos yoziladi)
}

// customDeckRules — maxsus dizayndagi taqdimot uchun qo'shimcha qoidalar
// (n — mazmunli slaydlar soni). Dizayn barcha maketlarni chiza oladi, shuning
// uchun AI dan vizual va xilma-xil tuzilish talab qilinadi.
func customDeckRules(n int, brief *DesignBrief) string {
	var b strings.Builder
	b.WriteString("This deck uses a custom visual design created for this topic")
	if brief.Name != "" {
		fmt.Fprintf(&b, " (%q", brief.Name)
		if brief.Mood != "" {
			b.WriteString(": " + brief.Mood)
		}
		b.WriteString(")")
	}
	b.WriteString(", so make the content as visual and varied as the design:\n")
	b.WriteString("- Turn content into visual structures whenever it allows: steps become \"process\", dates become \"timeline\", repeating stages become \"cycle\", parts of a whole become \"hub\", levels become \"pyramid\", four-part classifications become \"matrix\", contrasts become \"comparison\", key figures become \"facts\".\n")
	if n >= 5 {
		fmt.Fprintf(&b, "- Use \"bullets\" on at most %d slide(s); never put the same layout on two consecutive slides.\n", max(1, n/5))
	}
	if n >= 7 {
		b.WriteString("- Include exactly one \"statement\" slide with the central thesis of the topic in the second half of the deck, before the conclusion.\n")
	}
	b.WriteString("- Each slide delivers one clear message: a specific title, a lead that states the message, and items with concrete facts, mechanisms or examples rather than general phrases.")
	return b.String()
}

// customPlanChecks — maxsus dizayn rejasi uchun yakuniy tekshiruv bandlari
// (promptning oxirida: kichik modellar ham qoidalarga aniqroq amal qiladi).
func customPlanChecks(n int, signature []string) []string {
	checks := []string{fmt.Sprintf("there are exactly %d slides", n)}
	if k := signatureMinimum(n, signature); k > 0 {
		quoted := make([]string, len(signature))
		for i, l := range signature {
			quoted[i] = `"` + l + `"`
		}
		what := "each signature layout (" + strings.Join(quoted, ", ") + ") is used at least once"
		if k < len(signature) {
			what = fmt.Sprintf("at least %d of the signature layouts (%s) are used", k, strings.Join(quoted, ", "))
		}
		checks = append(checks, what+" on a slide whose content fits it")
	}
	if n >= 7 {
		checks = append(checks, `there is exactly one "statement" slide`)
	}
	if n >= 5 {
		checks = append(checks, "no two consecutive slides share a layout", fmt.Sprintf(`"bullets" is used at most %d time(s)`, max(1, n/5)))
	}
	return checks
}

// customContentChecks — maxsus dizayndagi slaydlar mazmuni uchun yakuniy
// tekshiruv bandlari. planned — maketlar rejada qat'iy belgilangan (guruhlab
// yozish): ishonchsiz raqam o'rniga qiymat bo'sh qoldiriladi (generator
// bunday "facts" slaydini kartochkalarga aylantiradi).
func customContentChecks(planned bool) []string {
	unsure := "if unsure, use another layout"
	if planned {
		unsure = `if unsure, leave "value" empty`
	}
	return []string{
		`every slide has the number of items its layout requires (for example "process" 3-5, "cards" 3-4, "comparison" exactly 2 with 3-5 points each, "matrix" exactly 4, "pyramid" 3-5)`,
		"every text reaches the lower bound of its word range: no one-line stubs",
		`numbers in "facts" and dates in "timeline" are real and well established; never estimate percentages or invent figures — ` + unsure,
		"all text is in the content language",
	}
}

// numberedCheck — "Before answering, check that: (1) ...; (2) ...".
func numberedCheck(checks []string) string {
	var b strings.Builder
	b.WriteString("Before answering, check that:")
	for i, c := range checks {
		sep := ";"
		if i == len(checks)-1 {
			sep = "."
		}
		fmt.Fprintf(&b, " (%d) %s%s", i+1, c, sep)
	}
	return b.String()
}

// customImageRule — maxsus dizaynda rasm tavsiflari uslubga mos va xilma-xil bo'lishi uchun.
func customImageRule(brief *DesignBrief) string {
	s := "Image descriptions follow one art direction so that all photos look like a single series"
	if brief.ImageStyle != "" {
		s += fmt.Sprintf(" (%s)", strings.TrimRight(brief.ImageStyle, "."))
	}
	return s + "; vary the shot scale across slides (wide establishing shots, medium shots, close-up details) and show the subject of each slide literally and recognisably."
}
