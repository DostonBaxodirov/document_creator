package bot

import (
	"fmt"
	"html"
	"strings"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

// designChoice — tanlangan dizayn: ID, nomi (tugma javobi uchun), xabardagi
// ko'rinishi (HTML) va keyingi holat.
type designChoice struct {
	id, name, label string
	next            State
}

// resolveDesign dizayn tugmasi qiymatini ish turiga mos dizaynga aylantiradi:
// maxsus dizayn — tarif tanlashga, tayyor dizaynlar — talaba ma'lumotlariga o'tadi.
func resolveDesign(s *storage.Session, data string) (designChoice, bool) {
	switch {
	case data == generator.ThemeCustom:
		return designChoice{id: data, name: customLabel, label: customLabel, next: StateTariff}, true
	case isDoc(s):
		if t, ok := generator.FindDocTheme(data); ok {
			return designChoice{id: t.ID, name: t.Name, label: t.Emoji + " " + html.EscapeString(t.Name), next: StateStudent}, true
		}
	default:
		if t, ok := generator.FindTheme(data); ok {
			return designChoice{id: t.ID, name: t.Name, label: t.Emoji + " " + html.EscapeString(t.Name), next: StateStudent}, true
		}
	}
	return designChoice{}, false
}

// designKeyboardFor ish turiga mos dizayn tugmalari.
func (h *Handler) designKeyboardFor(s *storage.Session) *tele.ReplyMarkup {
	if isDoc(s) {
		return docDesignKeyboard(h.customOn())
	}
	return designKeyboard(h.customOn())
}

// askDesign dizayn tanlash xabarini yuboradi: dizaynlar ko'rinishi rasmi
// (bo'lsa) va har bir dizayn uchun tugma. Word ishlar uchun — Word dizaynlari.
func (h *Handler) askDesign(c tele.Context, s *storage.Session) error {
	text, path, cache := designPrompt(h.customOn()), h.opts.DesignPreview, &h.previewID
	if isDoc(s) {
		text, path, cache = docDesignPrompt(h.customOn()), h.opts.DocDesignPreview, &h.docPreviewID
	}
	keyboard := h.designKeyboardFor(s)
	h.previewMu.Lock()
	cached := *cache
	h.previewMu.Unlock()

	var file tele.File
	switch {
	case cached != "":
		file = tele.File{FileID: cached}
	case path != "" && fileExists(path):
		file = tele.FromDisk(path)
	default:
		return c.Send(text, keyboard)
	}

	// Tarmoq so'rovi qulfsiz bajariladi: birinchi yuklashlar bir vaqtga to'g'ri
	// kelsa, rasm ikki marta yuklanishi mumkin — bu zararsiz.
	msg, err := h.bot.Send(c.Recipient(), &tele.Photo{File: file, Caption: text}, keyboard)
	h.previewMu.Lock()
	defer h.previewMu.Unlock()
	if err != nil {
		// Rasmni yuborib bo'lmasa (masalan, file_id eskirgan) — rasmsiz davom etamiz.
		h.log.Warn("failed to send design preview", zap.Error(err))
		if *cache == cached {
			*cache = ""
		}
		return c.Send(text, keyboard)
	}
	if msg != nil && msg.Photo != nil && *cache == "" {
		*cache = msg.Photo.FileID
	}
	return nil
}

// docDesignPrompt Word dizaynini tanlash xabari (rasm izohi — 1024 belgigacha).
func docDesignPrompt(custom bool) string {
	var b strings.Builder
	b.WriteString("🎨 <b>Hujjat dizaynini tanlang:</b>\n\n")
	for i, t := range generator.DocThemes() {
		fmt.Fprintf(&b, "%d. %s <b>%s</b> — %s\n", i+1, t.Emoji, html.EscapeString(t.Name), html.EscapeString(t.Description))
	}
	b.WriteString("\n<i>Barcha dizaynlarda asosiy matn OTM talablariga mos: Times New Roman 14, interval 1,5.</i>")
	if custom {
		b.WriteString("\n✨ <b>Maxsus dizayn</b> — mavzungizga mos noyob uslub va boblarda rasmlar (3 ta tarif).")
	}
	return b.String()
}

// designPrompt dizayn tanlash xabari matni (custom — AI maxsus dizayn yoqilgan).
// Rasm izohi bo'lib yuboriladi — Telegram chegarasi 1024 belgi.
func designPrompt(custom bool) string {
	var b strings.Builder
	b.WriteString("🎨 <b>Taqdimot dizaynini tanlang:</b>\n\n")
	for i, t := range generator.Themes() {
		photo := ""
		if t.Photos {
			photo = " 📷"
		}
		fmt.Fprintf(&b, "%d. %s <b>%s</b>%s — %s\n", i+1, t.Emoji, html.EscapeString(t.Name), photo, html.EscapeString(t.Description))
	}
	b.WriteString("\n📷 — mavzuga mos rasmlar bilan.\n")
	if custom {
		b.WriteString("✨ <b>Maxsus dizayn</b> — mavzungizga mos noyob dizayn noldan yaratiladi (3 ta tarif).")
	} else {
		b.WriteString("<i>Har bir dizaynning o'z tuzilishi bor: vaqt chizig'i, piramida, SWOT, sikl, sxema va boshqa maketlar.</i>")
	}
	return b.String()
}
