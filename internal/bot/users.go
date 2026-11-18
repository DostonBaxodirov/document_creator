package bot

// Foydalanuvchilar ro'yxati (faqat admin): /foydalanuvchilar — davr (botga
// kirgan sanasi bo'yicha) va format tanlanadi. Matnda — umumiy ko'rsatkichlar
// va eng yangi foydalanuvchilar (Telegram xabariga sig'gancha), CSV da —
// to'liq ro'yxat barcha ustunlar bilan.

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/payment"
	"telegram-presentation-bot/internal/storage"
)

const msgNoUserStats = "Foydalanuvchilar ro'yxati hozircha mavjud emas."

// onUsers /foydalanuvchilar — davr tanlash tugmalari.
func (h *Handler) onUsers(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	if h.stats == nil {
		return c.Send(msgNoUserStats)
	}
	return c.Send("👥 <b>Foydalanuvchilar ro'yxati</b>\n\nQaysi davrda botga kirganlar?", periodKeyboard(cbUsersPeriod))
}

// onUsersPeriod davr tanlanganda — format tanlash tugmalari.
func (h *Handler) onUsersPeriod(c tele.Context) error {
	if ok, err := h.requireAdminCallback(c); !ok {
		return err
	}
	p, ok := findPeriod(c.Data())
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	_ = c.Respond()
	if err := c.Edit(fmt.Sprintf("👥 <b>Foydalanuvchilar — %s</b>\n\nFormatni tanlang:", p.Title),
		formatKeyboard(cbUsersFormat, p.ID)); err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to edit users message", zap.Error(err))
	}
	return nil
}

// onUsersFormat format tanlanganda ro'yxatni yuboradi.
func (h *Handler) onUsersFormat(c tele.Context) error {
	if ok, err := h.requireAdminCallback(c); !ok {
		return err
	}
	args := c.Args()
	if len(args) != 2 {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	p, ok := findPeriod(args[0])
	if !ok || (args[1] != reportText && args[1] != reportCSV) {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	if h.stats == nil {
		return c.Respond(&tele.CallbackResponse{Text: msgNoUserStats})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Now()
	list, err := h.stats.ListUsers(ctx, p.since(now))
	if err != nil {
		return err
	}
	_ = c.Respond()
	h.edit(c, fmt.Sprintf("👥 <b>Foydalanuvchilar — %s</b>", p.Title))
	h.log.Info("admin users list", append(userFields(c),
		zap.String("period", p.ID), zap.String("format", args[1]), zap.Int("users", len(list)))...)

	if args[1] == reportText {
		return c.Send(usersText(list, p, now), tele.NoPreview)
	}
	data, err := usersCSV(list)
	if err != nil {
		return err
	}
	st := computeUserStats(list)
	return c.Send(&tele.Document{
		File:     tele.FromReader(bytes.NewReader(data)),
		FileName: fmt.Sprintf("foydalanuvchilar_%s_%s.csv", p.ID, now.In(payment.Tashkent).Format("2006-01-02_1504")),
		MIME:     "text/csv",
		Caption: fmt.Sprintf("👥 Foydalanuvchilar — %s\nJami: %s · ish olgan: %s · to'lov qilgan: %s (%s)",
			p.Title, formatCount(st.total), formatCount(st.customers), formatCount(st.payers), formatSum(int(st.paidSum))),
	})
}

// userStats — ro'yxat bo'yicha umumiy ko'rsatkichlar.
type userStats struct {
	total, customers, payers, paidSum int64
}

func computeUserStats(list []storage.UserSummary) userStats {
	st := userStats{total: int64(len(list))}
	for _, u := range list {
		if u.Delivered > 0 {
			st.customers++
		}
		if u.Payments > 0 {
			st.payers++
			st.paidSum += u.PaidSum
		}
	}
	return st
}

// usersText matnli ro'yxat: umumiy ko'rsatkichlar va foydalanuvchilar (eng
// yangisi birinchi; sig'maganlari qirqiladi — to'liq ro'yxat CSV da).
func usersText(list []storage.UserSummary, p reportPeriod, now time.Time) string {
	st := computeUserStats(list)
	var b strings.Builder
	fmt.Fprintf(&b, "👥 <b>Foydalanuvchilar — %s</b>\n", p.Title)
	fmt.Fprintf(&b, "🕒 %s holatiga (Toshkent vaqti)\n\n", now.In(payment.Tashkent).Format("02.01.2006 15:04"))
	fmt.Fprintf(&b, "👤 Jami: <b>%s</b>\n", formatCount(st.total))
	fmt.Fprintf(&b, "📦 Ish olgan: <b>%s</b>\n", formatCount(st.customers))
	fmt.Fprintf(&b, "💰 To'lov qilgan: <b>%s</b> · %s\n", formatCount(st.payers), formatSum(int(st.paidSum)))

	if len(list) == 0 {
		b.WriteString("\nBu davrda botga yangi foydalanuvchi kirmagan.")
		return b.String()
	}
	const legend = "\n<i>📅 — botga kirgan sana, 👁 — oxirgi faollik, 📦 — tayyor ishlar, 💰 — tasdiqlangan to'lovlar.</i>"
	b.WriteString("\n")
	for i, u := range list {
		line := userLine(i+1, u)
		if utf8.RuneCountInString(b.String())+utf8.RuneCountInString(line)+utf8.RuneCountInString(legend) > maxReportRunes {
			fmt.Fprintf(&b, "… yana %s ta. To'liq ro'yxat uchun CSV ni tanlang.\n", formatCount(int64(len(list)-i)))
			break
		}
		b.WriteString(line)
	}
	b.WriteString(legend)
	return b.String()
}

// userLine: "3. 👑 Ali (@ali) · 123456 · 📅 18.11.2026 · 👁 18.11.2026 · 📦 2 · 💰 25 000 so'm".
// Ism — profilga havola (admin foydalanuvchiga bir bosishda yoza oladi).
func userLine(n int, u storage.UserSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d. ", n)
	if u.IsAdmin {
		b.WriteString("👑 ")
	}
	fmt.Fprintf(&b, `<a href="tg://user?id=%d">%s</a>`, u.UserID, html.EscapeString(userDisplayName(u)))
	if u.Username != "" {
		b.WriteString(" (@" + html.EscapeString(u.Username) + ")")
	}
	fmt.Fprintf(&b, " · <code>%d</code> · 📅 %s · 👁 %s", u.UserID,
		u.CreatedAt.In(payment.Tashkent).Format("02.01.2006"), u.LastActive(payment.Tashkent).Format("02.01.2006"))
	if u.Delivered > 0 {
		fmt.Fprintf(&b, " · 📦 %d", u.Delivered)
	}
	if u.PaidSum > 0 {
		b.WriteString(" · 💰 " + formatSum(int(u.PaidSum)))
	}
	b.WriteString("\n")
	return b.String()
}

// userDisplayName foydalanuvchi ismi: Telegram'dagi ismi, bo'lmasa titulga
// yozgan F.I.Sh., bo'lmasa umumiy nom.
func userDisplayName(u storage.UserSummary) string {
	for _, s := range []string{u.FirstName, u.StudentName} {
		if s = strings.TrimSpace(s); s != "" {
			return truncateRunes(s, 40)
		}
	}
	return "Foydalanuvchi"
}

// usersCSVHeader — foydalanuvchilar CSV ustunlari.
var usersCSVHeader = []string{
	"telegram_user_id", "username", "first_name", "student_name", "university", "language", "is_admin",
	"registered_at", "last_active", "delivered_works", "approved_payments", "paid_sum",
}

// usersCSV foydalanuvchilar ro'yxatini CSV ko'rinishida qaytaradi (Excel
// o'zbekcha harflarni to'g'ri ko'rsatishi uchun UTF-8 BOM bilan). Vaqtlar —
// Toshkent vaqtida.
func usersCSV(list []storage.UserSummary) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	w := csv.NewWriter(&buf)
	if err := w.Write(usersCSVHeader); err != nil {
		return nil, err
	}
	for _, u := range list {
		language := u.Language
		if language == "" {
			language = "uz"
		}
		row := []string{
			strconv.FormatInt(u.UserID, 10),
			u.Username,
			u.FirstName,
			u.StudentName,
			u.University,
			language,
			strconv.FormatBool(u.IsAdmin),
			u.CreatedAt.In(payment.Tashkent).Format("2006-01-02 15:04:05"),
			u.LastActive(payment.Tashkent).Format(time.DateOnly),
			strconv.FormatInt(u.Delivered, 10),
			strconv.FormatInt(u.Payments, 10),
			strconv.FormatInt(u.PaidSum, 10),
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
