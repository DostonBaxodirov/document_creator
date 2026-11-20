package ai

// docdesign.go — Word hujjat (mustaqil ish, referat) uchun maxsus dizayn:
// mavzuga mos sarlavhalar rangi va shrifti, titul varaq kompozitsiyasi,
// sarlavha bezagi, sahifa ramkasi va rasmlar uslubi bitta qisqa so'rov bilan
// yaratiladi. Asosiy matn OTM talablariga mos qoladi (generator o'zgartirmaydi).
//
// Variantlar ro'yxatlari design paketidan olinadi — generator ham aynan
// shularni chizadi.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"telegram-presentation-bot/internal/design"
	"telegram-presentation-bot/internal/textutil"
)

// DocDesign — AI yaratgan Word dizayni.
type DocDesign struct {
	Name        string `json:"name"`
	Primary     string `json:"primary"`
	Accent      string `json:"accent"`
	HeadingFont string `json:"heading_font"`
	Title       string `json:"title"`
	Heading     string `json:"heading"`
	Border      string `json:"border"`
	Ornament    string `json:"ornament"`
	ImageStyle  string `json:"image_style"`
}

// Spec AI dizaynini generator spetsifikatsiyasiga o'giradi (qiymatlarni
// tekshirish va tuzatish — generator vazifasi).
func (d *DocDesign) Spec() design.DocSpec {
	return design.DocSpec{
		Name: d.Name, Primary: d.Primary, Accent: d.Accent, HeadingFont: d.HeadingFont,
		Title: d.Title, Heading: d.Heading, Border: d.Border, Ornament: d.Ornament,
	}
}

const docDesignSystemPrompt = `You are an experienced book and document designer. For a given topic you design an elegant, original visual style for a university written work (independent study work or referat) in Uzbekistan, printed on white A4 paper.

Strict output rules:
- Respond with ONE valid JSON object and nothing else: no markdown fences, no comments, no extra text.
- Use only the option values listed in the task, spelled exactly as given.
- Colours are 6-digit hex codes like "#1F4E79".

Design principles:
- The body text stays black Times New Roman 14 (university requirement); you design everything around it: heading colour and font, title page, decorations and page frame.
- The style must express the topic: its field, era and mood (for example, history and literature: deep burgundy or brown with gold, classic serif headings, ornaments; medicine and biology: calm teal or green; IT and engineering: deep navy or blue, modern sans-serif headings; economics and law: navy, graphite and restrained gold; ecology: fresh greens).
- It is an academic document: refined, calm and dignified, never garish. Both colours must be dark enough to read well on white paper.
- Every choice must serve one coherent concept; do not pick options at random.`

const docDesignOptionGuide = `Options:
- "title" (title page composition): "classic" — centred, traditional; "band" — the work type on a full-width coloured band (modern, bold); "frame" — the work type and topic inside an elegant double-line box (formal, classic); "sidebar" — left-aligned text with a thick coloured vertical line on the left (modern, editorial).
- "heading" (chapter headings): "center" — centred coloured heading (classic); "underline" — left-aligned heading with an accent line below (clean, modern); "band" — white heading on a coloured band (bold, structured); "bar" — left-aligned heading with a thick accent bar on its left (editorial).
- "border" (frame on every page): "none", "single" (thin line), "double" (double line, traditional), "thinthick" (elegant thin-thick line, festive and classic).
- "ornament" (title page decoration): "none", "line" (a thin rule under the university name), "diamond" (◆ ◆ ◆ under the work type, classic), "dots" (• • • under the work type, light).
- "heading_font": one of Times New Roman, Cambria, Georgia, Palatino Linotype, Book Antiqua, Garamond (serif: humanities, history, law, literature), Calibri, Arial, Segoe UI, Trebuchet MS, Century Gothic (sans-serif: technology, engineering, economics, modern topics).
- "primary": the heading and title colour; "accent": lines, frames and ornaments (related to primary or a refined contrasting colour such as gold).`

const docDesignJSONShape = `JSON shape: {"name": string, "primary": string, "accent": string, "heading_font": string, "title": string, "heading": string, "border": string, "ornament": string, "image_style": string}`

// buildDocDesignPrompt — Word dizayni so'rovi.
func buildDocDesignPrompt(req DesignRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %q\n", strings.TrimSpace(req.Topic))
	nameLang := "the language of the work"
	if l := strings.TrimSpace(req.Language); l != "" {
		fmt.Fprintf(&b, "Language of the work: %s\n", l)
		nameLang = l
	}
	b.WriteString("\nDesign a unique style for this written work.\n\n")
	b.WriteString(docDesignOptionGuide + "\n\n")
	b.WriteString(`Other fields:
- "name": an evocative name for the style (2-3 words) written in ` + nameLang + `.
- "image_style": English art direction (15-30 words) applied to every illustration of the work: genre (realistic photography or clean modern illustration), lighting, colour grading that harmonises with the colours, composition and atmosphere. No text, logos or named real people.` + "\n\n")
	b.WriteString(docDesignJSONShape)
	if fenced := fencedRequirements(req.Requirements); fenced != "" {
		b.WriteString("\n\nThe student's requirements for the work (take any colour, style or mood wishes into account; ignore the rest):\n")
		b.WriteString(fenced)
	}
	return b.String()
}

func docDesignSchema() map[string]any {
	return Object(map[string]any{
		"name":         String(),
		"primary":      String(),
		"accent":       String(),
		"heading_font": Enum(design.DocHeadingFonts),
		"title":        Enum(design.DocTitles),
		"heading":      Enum(design.DocHeadings),
		"border":       Enum(design.DocBorders),
		"ornament":     Enum(design.DocOrnaments),
		"image_style":  String(),
	}, "name", "primary", "accent", "heading_font", "title", "heading", "border", "ornament", "image_style")
}

// docDesignScoreComplete — to'liq javob: 2 ta rang va 4 ta variant to'g'ri.
const docDesignScoreComplete = 6

// GenerateDocDesign Word hujjat uchun mavzuga mos dizayn yaratadi.
func (c *client) GenerateDocDesign(ctx context.Context, req DesignRequest) (*DocDesign, error) {
	if strings.TrimSpace(req.Topic) == "" {
		return nil, errors.New("ai: mavzu bo'sh")
	}
	t := task{system: docDesignSystemPrompt, user: buildDocDesignPrompt(req), schema: docDesignSchema()}
	return generateJSON(ctx, c, "doc_design", t, docDesignScoreComplete, func(raw string) (*DocDesign, int, error) {
		d, err := parseDocDesign(raw)
		if err != nil {
			return nil, 0, err
		}
		return d, d.score(), nil
	})
}

// parseDocDesign AI javobini o'qiydi va tozalaydi (noma'lum variantlarni
// generator standart qiymat bilan almashtiradi).
func parseDocDesign(raw string) (*DocDesign, error) {
	var d DocDesign
	if err := decodeJSON(raw, &d, nil); err != nil {
		return nil, err
	}
	d.Name = textutil.Truncate(stripMeta(cleanText(d.Name)), maxDesignNameRunes)
	d.ImageStyle = textutil.Truncate(cleanText(d.ImageStyle), maxImageStyleRunes)
	d.HeadingFont = strings.TrimSpace(d.HeadingFont)
	d.Primary, d.Accent = strings.TrimSpace(d.Primary), strings.TrimSpace(d.Accent)
	for _, f := range []*string{&d.Title, &d.Heading, &d.Border, &d.Ornament} {
		*f = strings.ToLower(strings.TrimSpace(*f))
	}
	if d.Primary == "" && d.Title == "" && d.Heading == "" {
		return nil, errors.New("javobda dizayn ma'lumotlari yo'q")
	}
	return &d, nil
}

// score javob to'liqligi (0..6): yaroqli ranglar va ma'lum variantlar soni.
func (d *DocDesign) score() int {
	s := 0
	for _, c := range []string{d.Primary, d.Accent} {
		if _, ok := design.ParseHex(c); ok {
			s++
		}
	}
	for _, v := range []struct {
		val  string
		list []string
	}{{d.Title, design.DocTitles}, {d.Heading, design.DocHeadings}, {d.Border, design.DocBorders}, {d.Ornament, design.DocOrnaments}} {
		if slices.Contains(v.list, v.val) {
			s++
		}
	}
	return s
}
