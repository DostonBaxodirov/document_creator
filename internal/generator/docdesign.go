package generator

// Word hujjat dizaynlari: tayyor uslublar (DocThemes) va AI maxsus dizayni
// (DocSpec). Barcha dizaynlarda asosiy matn OTM talablariga mos qoladi
// (Times New Roman 14, interval 1,5, hoshiyalar) — sahifalar soni ham shunga
// qarab hisoblanadi. Dizaynlar sarlavhalar shrifti va rangi, sarlavha
// bezagi, sahifa ramkasi, titul varaq kompozitsiyasi va bezaklari bilan farq
// qiladi.

import (
	"slices"
	"strings"

	"telegram-presentation-bot/internal/design"
)

// Tayyor Word dizaynlari identifikatorlari (sessions.design da saqlanadi —
// o'zgartirmang). Maxsus dizayn — ThemeCustom.
const (
	DocThemeClassic  = "doc_classic"
	DocThemeFrame    = "doc_frame"
	DocThemeModern   = "doc_modern"
	DocThemeAcademic = "doc_academic"
	DocThemeElegant  = "doc_elegant"
)

// DocTheme — tayyor Word dizaynining foydalanuvchiga ko'rinishi.
type DocTheme struct {
	ID          string
	Emoji       string
	Name        string
	Description string
}

// docThemes — tayyor dizaynlar (tanlash tartibida) va ularning uslubi.
var docThemes = []struct {
	DocTheme
	style docStyle
}{
	{
		DocTheme{DocThemeClassic, "📘", "Klassik", "qat'iy OTM uslubi: qora sarlavhalar, bezaksiz"},
		docStyle{primary: "000000", accent: "000000", title: docTitleClassic, heading: docHeadingCenter, border: docBorderNone, ornament: docOrnamentLine},
	},
	{
		DocTheme{DocThemeFrame, "🖼", "Ramkali", "barcha sahifalar qo'sh chiziqli ramkada"},
		docStyle{primary: "000000", accent: "000000", title: docTitleFrame, heading: docHeadingCenter, border: docBorderDouble, ornament: docOrnamentNone},
	},
	{
		DocTheme{DocThemeModern, "💠", "Zamonaviy", "to'q ko'k sarlavhalar, titulda rangli lenta"},
		docStyle{headingFont: "Cambria", primary: "1F4E79", accent: "2E75B6", title: docTitleBand, heading: docHeadingUnderline, border: docBorderNone, ornament: docOrnamentNone},
	},
	{
		DocTheme{DocThemeAcademic, "🎓", "Akademik", "bordo va oltin rang, nafis ramka va bezaklar"},
		docStyle{headingFont: "Georgia", primary: "7B1E1E", accent: "B08D57", title: docTitleClassic, heading: docHeadingCenter, border: docBorderThinThick, ornament: docOrnamentDiamond},
	},
	{
		DocTheme{DocThemeElegant, "🌿", "Nafis", "zangori-yashil ranglar, sarlavhalar yonida chiziq"},
		docStyle{headingFont: "Palatino Linotype", primary: "0F5257", accent: "2A9D8F", title: docTitleSidebar, heading: docHeadingBar, border: docBorderNone, ornament: docOrnamentDots},
	},
}

// DocThemes tayyor Word dizaynlari ro'yxati.
func DocThemes() []DocTheme {
	out := make([]DocTheme, len(docThemes))
	for i, t := range docThemes {
		out[i] = t.DocTheme
	}
	return out
}

// FindDocTheme tayyor Word dizaynini ID bo'yicha topadi.
func FindDocTheme(id string) (DocTheme, bool) {
	for _, t := range docThemes {
		if t.ID == id {
			return t.DocTheme, true
		}
	}
	return DocTheme{}, false
}

// Dizayn variantlari (AI maxsus dizayni shulardan tanlaydi) — design
// paketidagi qiymatlarning taxalluslari.
const (
	docTitleClassic = design.DocTitleClassic
	docTitleBand    = design.DocTitleBand
	docTitleFrame   = design.DocTitleFrame
	docTitleSidebar = design.DocTitleSidebar

	docHeadingCenter    = design.DocHeadingCenter
	docHeadingUnderline = design.DocHeadingUnderline
	docHeadingBand      = design.DocHeadingBand
	docHeadingBar       = design.DocHeadingBar

	docBorderNone      = design.DocBorderNone
	docBorderSingle    = design.DocBorderSingle
	docBorderDouble    = design.DocBorderDouble
	docBorderThinThick = design.DocBorderThinThick

	docOrnamentNone    = design.DocOrnamentNone
	docOrnamentLine    = design.DocOrnamentLine
	docOrnamentDiamond = design.DocOrnamentDiamond
	docOrnamentDots    = design.DocOrnamentDots
)

// DocSpec — AI yaratgan Word dizayni (design.DocSpec).
type DocSpec = design.DocSpec

// docStyle — hujjatni chizish uchun aniq uslub.
type docStyle struct {
	headingFont string // bo'sh — asosiy matn shrifti (Times New Roman)
	primary     string // sarlavhalar rangi (RRGGBB, # siz)
	accent      string // chiziqlar va bezaklar
	title       string
	heading     string
	border      string
	ornament    string
}

// docStyleFor hujjat uslubini qaytaradi: maxsus dizayn (spec bo'lsa), tayyor
// dizayn yoki (noma'lum/bo'sh ID) Klassik.
func docStyleFor(themeID string, spec *DocSpec) docStyle {
	if spec != nil {
		return docSpecStyle(spec)
	}
	for _, t := range docThemes {
		if t.ID == themeID {
			return t.style
		}
	}
	return docThemes[0].style
}

// docSpecStyle AI qiymatlarini tekshiradi: noma'lum variant — standart
// qiymat, ranglar oq qog'ozda o'qiladigan darajada to'q qilinadi.
func docSpecStyle(sp *DocSpec) docStyle {
	oneOf := func(v string, list []string, def string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		if slices.Contains(list, v) {
			return v
		}
		return def
	}
	color := func(v, def string) string {
		hex, ok := design.ParseHex(v)
		if !ok {
			hex = def
		}
		// Oq qog'ozda sarlavha va chiziqlar aniq ko'rinishi uchun (kamida 4.5:1).
		return ensureContrast(hex, []string{"FFFFFF"}, 4.5, "000000")
	}
	font := ""
	for _, f := range design.DocHeadingFonts {
		if strings.EqualFold(strings.TrimSpace(sp.HeadingFont), f) {
			font = f
		}
	}
	if font == "Times New Roman" {
		font = ""
	}
	return docStyle{
		headingFont: font,
		primary:     color(sp.Primary, "1F3864"),
		accent:      color(sp.Accent, "8C6D1F"),
		title:       oneOf(sp.Title, design.DocTitles, docTitleClassic),
		heading:     oneOf(sp.Heading, design.DocHeadings, docHeadingCenter),
		border:      oneOf(sp.Border, design.DocBorders, docBorderNone),
		ornament:    oneOf(sp.Ornament, design.DocOrnaments, docOrnamentNone),
	}
}

// IsDocDesign id Word dizayni (tayyor yoki maxsus) ekanini bildiradi.
func IsDocDesign(id string) bool {
	_, ok := FindDocTheme(id)
	return ok || id == ThemeCustom
}
