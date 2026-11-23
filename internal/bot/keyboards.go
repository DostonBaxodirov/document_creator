package bot

import (
	"fmt"
	"strconv"

	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/lang"
)

// workTypeKeyboard ish turi tugmalari; taklif tizimi yoqilgan bo'lsa —
// oxirgi qatorda "🎁 Do'stlarni taklif qilish" tugmasi.
func (h *Handler) workTypeKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	rows := make([]tele.Row, 0, len(workTypes)+1)
	for _, o := range workTypes {
		rows = append(rows, m.Row(m.Data(o.Button, cbWorkType, string(o.Type))))
	}
	if h.referralOn() {
		rows = append(rows, m.Row(m.Data(referralButton, cbReferral)))
	}
	m.Inline(rows...)
	return m
}

// languageKeyboard ish tilini tanlash tugmalari (qatorda 2 tadan).
func languageKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var btns []tele.Btn
	for _, l := range lang.All() {
		btns = append(btns, m.Data(l.Button, cbLanguage, string(l.Code)))
	}
	m.Inline(m.Split(2, btns)...)
	return m
}

// designKeyboard dizayn tanlash tugmalari (qatorda 2 tadan); custom bo'lsa —
// birinchi qatorda "✨ Maxsus dizayn" tugmasi.
func designKeyboard(custom bool) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var rows []tele.Row
	if custom {
		rows = append(rows, m.Row(m.Data(customLabel+" — mavzuga mos", cbDesign, generator.ThemeCustom)))
	}
	var btns []tele.Btn
	for i, t := range generator.Themes() {
		label := fmt.Sprintf("%d. %s %s", i+1, t.Emoji, t.Name)
		if t.Photos {
			label += " 📷" // rasmli dizayn tugmada ham ko'rinsin
		}
		btns = append(btns, m.Data(label, cbDesign, t.ID))
	}
	m.Inline(append(rows, m.Split(2, btns)...)...)
	return m
}

// docDesignKeyboard Word dizaynlari tugmalari (qatorda 2 tadan).
func docDesignKeyboard(custom bool) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var rows []tele.Row
	if custom {
		rows = append(rows, m.Row(m.Data(customLabel+" — mavzuga mos", cbDesign, generator.ThemeCustom)))
	}
	var btns []tele.Btn
	for i, t := range generator.DocThemes() {
		btns = append(btns, m.Data(fmt.Sprintf("%d. %s %s", i+1, t.Emoji, t.Name), cbDesign, t.ID))
	}
	m.Inline(append(rows, m.Split(2, btns)...)...)
	return m
}

// confirmKeyboard tasdiqlash tugmalari. Buyurtmani taklif bonusi bilan
// tasdiqlash mumkin bo'lsa (q.bonusOK), foydalanuvchi tanlaydi: bonus bilan
// yoki to'lab (bonusni boshqa buyurtmaga saqlab qo'yish uchun). q nil
// bo'lishi mumkin (to'lov o'chirilgan yoki narxni hisoblab bo'lmadi).
func confirmKeyboard(q *quote) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var rows []tele.Row
	if q.bonusOK() {
		bonus := "🎁 Bonus bilan tasdiqlash — bepul"
		if p := q.bonusPrice(); p > 0 {
			bonus = "🎁 Bonus bilan tasdiqlash — " + formatSum(p)
		}
		rows = append(rows,
			m.Row(m.Data(bonus, cbConfirmBonus)),
			m.Row(m.Data("💳 To'lab tasdiqlash — "+formatSum(q.price), cbConfirm)))
	} else {
		rows = append(rows, m.Row(m.Data("✅ Tasdiqlash", cbConfirm)))
	}
	rows = append(rows, m.Row(m.Data("🔄 Qaytadan to'ldirish", cbRestart), m.Data("❌ Bekor qilish", cbCancel)))
	m.Inline(rows...)
	return m
}

func retryKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("🔄 Qayta urinish", cbConfirm), m.Data("❌ Bekor qilish", cbCancel)))
	return m
}

func paymentKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("❌ Bekor qilish", cbCancel)))
	return m
}

func reviewKeyboard(paymentID uint) *tele.ReplyMarkup {
	id := strconv.FormatUint(uint64(paymentID), 10)
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("✅ Tasdiqlash", cbPayApprove, id), m.Data("❌ Rad etish", cbPayReject, id)))
	return m
}

// replyKeyboard javob variantlari klaviaturasi: qisqa variantlar (sonlar)
// qatorda 3 tadan, uzunlari — har biri alohida qatorda.
func replyKeyboard(options []string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{ResizeKeyboard: true, OneTimeKeyboard: true}
	perRow := 1
	short := true
	for _, o := range options {
		if len([]rune(o)) > 4 {
			short = false
			break
		}
	}
	if short {
		perRow = 3
	}

	var rows []tele.Row
	for i := 0; i < len(options); i += perRow {
		var btns []tele.Btn
		for _, o := range options[i:min(i+perRow, len(options))] {
			btns = append(btns, m.Text(o))
		}
		rows = append(rows, m.Row(btns...))
	}
	m.Reply(rows...)
	return m
}

func removeKeyboard() *tele.ReplyMarkup {
	return &tele.ReplyMarkup{RemoveKeyboard: true}
}
