package bot

// Admin imkoniyatlari: parol orqali admin bo'lish, to'lovlar hisoboti
// (davr filtri bilan, matn yoki CSV ko'rinishida), chek rasmini ko'rish,
// tashrif buyuruvchilar statistikasi (stats.go), foydalanuvchilar ro'yxati
// (users.go) va baza nusxasi (backup.go).

import (
	"bytes"
	"context"
	"crypto/subtle"
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
	"gorm.io/gorm"

	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/tz"
)

const (
	// maxReportRunes — matnli hisobot uzunligi (Telegram chegarasi 4096 belgi).
	maxReportRunes = 3800
	// maxPendingChecks — /tekshiruv bir martada yuboradigan cheklar soni
	// (Telegram xabar yuborish chegarasiga tushmaslik uchun).
	maxPendingChecks = 10
)

// reportPeriod — hisobot davri.
type reportPeriod struct {
	ID     string
	Button string
	Title  string
	Span   time.Duration // 0 — barcha vaqt
}

var reportPeriods = []reportPeriod{
	{ID: "day", Button: "1 kun", Title: "oxirgi 1 kun", Span: 24 * time.Hour},
	{ID: "week", Button: "1 hafta", Title: "oxirgi 7 kun", Span: 7 * 24 * time.Hour},
	{ID: "month", Button: "1 oy", Title: "oxirgi 30 kun", Span: 30 * 24 * time.Hour},
	{ID: "all", Button: "Hammasi", Title: "barcha vaqt"},
}

const (
	reportText = "text"
	reportCSV  = "csv"
)

func findPeriod(id string) (reportPeriod, bool) {
	for _, p := range reportPeriods {
		if p.ID == id {
			return p, true
		}
	}
	return reportPeriod{}, false
}

// since davr boshlanish vaqti (barcha vaqt uchun nil).
func (p reportPeriod) since(now time.Time) *time.Time {
	if p.Span == 0 {
		return nil
	}
	t := now.Add(-p.Span)
	return &t
}

// ---------------------------------------------------------------------------
// Admin bo'lish
// ---------------------------------------------------------------------------

// tryAdminLogin matn admin paroliga teng bo'lsa, foydalanuvchini admin qiladi.
// Istalgan FSM holatida ishlaydi va holatni o'zgartirmaydi.
func (h *Handler) tryAdminLogin(c tele.Context, text string) (bool, error) {
	pw := h.opts.Payment.AdminPassword
	if pw == "" || h.billing == nil {
		return false, nil
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(text)), []byte(pw)) != 1 {
		return false, nil
	}

	// Parol chatda qolmasligi uchun xabar o'chiriladi.
	if err := h.bot.Delete(c.Message()); err != nil {
		h.log.Debug("failed to delete admin password message", zap.Error(err))
	}

	ctx, cancel := requestContext()
	defer cancel()
	if _, err := h.ensureUser(ctx, c); err != nil {
		return true, err
	}
	added, err := h.billing.SetAdmin(ctx, c.Sender().ID)
	if err != nil {
		return true, err
	}
	h.log.Info("admin login", append(userFields(c), zap.Bool("new_admin", added))...)
	h.setAdminCommands(c.Chat().ID)

	if added {
		return true, c.Send(msgAdminWelcome)
	}
	return true, c.Send(msgAdminAlready)
}

// setAdminCommands admin chatida buyruqlar menyusiga admin buyruqlarini qo'shadi.
func (h *Handler) setAdminCommands(chatID int64) {
	cmds := []tele.Command{
		{Text: "start", Description: "Yangi ish buyurtma qilish"},
		{Text: "cancel", Description: "Joriy jarayonni bekor qilish"},
		{Text: "help", Description: "Yordam"},
		{Text: "report", Description: "To'lovlar hisoboti"},
		{Text: "chek", Description: "Chekni ko'rish: /chek ID"},
		{Text: "tekshiruv", Description: "Tekshiruvdagi cheklarni qayta olish"},
		{Text: "statistika", Description: "Tashrif buyuruvchilar statistikasi"},
		{Text: "foydalanuvchilar", Description: "Foydalanuvchilar ro'yxati"},
		{Text: "backup", Description: "Bazaning zaxira nusxasi"},
	}
	if err := h.bot.SetCommands(cmds, tele.CommandScope{Type: tele.CommandScopeChat, ChatID: chatID}); err != nil {
		h.log.Warn("failed to set admin commands", zap.Error(err))
	}
}

// RefreshAdminCommands barcha adminlar chatida buyruqlar menyusini yangilaydi
// (yangi admin buyruqlari oldin kirgan adminlarga ham ko'rinishi uchun).
func (h *Handler) RefreshAdminCommands(ctx context.Context) {
	if h.billing == nil {
		return
	}
	admins, err := h.billing.ListAdmins(ctx)
	if err != nil {
		h.log.Warn("failed to list admins for command refresh", zap.Error(err))
		return
	}
	for _, a := range admins {
		h.setAdminCommands(a.TelegramUserID)
	}
}

// requireAdmin foydalanuvchi admin ekanini tekshiradi. Admin bo'lmasa, buyruqqa
// oddiy "noma'lum buyruq" javobi beriladi (admin buyruqlari oshkor qilinmaydi),
// inline tugmaga esa "faqat admin uchun" bildirishnomasi.
func (h *Handler) requireAdmin(c tele.Context) (bool, error) {
	deny := func(resp ...*tele.CallbackResponse) (bool, error) {
		if c.Callback() != nil {
			return false, c.Respond(resp...)
		}
		return false, c.Send(msgUnknownCommand)
	}
	if h.billing == nil {
		return deny()
	}
	ctx, cancel := requestContext()
	defer cancel()
	ok, err := h.billing.IsAdmin(ctx, c.Sender().ID)
	if err != nil {
		return false, err
	}
	if !ok {
		return deny(&tele.CallbackResponse{Text: "Bu amal faqat admin uchun."})
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// Hisobot
// ---------------------------------------------------------------------------

// onReport /report — davr tanlash tugmalari.
func (h *Handler) onReport(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	return c.Send("📊 <b>To'lovlar hisoboti</b>\n\nDavrni tanlang:", reportPeriodKeyboard())
}

// onReportPeriod davr tanlanganda — format tanlash tugmalari.
func (h *Handler) onReportPeriod(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	p, ok := findPeriod(c.Data())
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	_ = c.Respond()
	if err := c.Edit(fmt.Sprintf("📊 <b>To'lovlar hisoboti — %s</b>\n\nFormatni tanlang:", p.Title),
		reportFormatKeyboard(p.ID)); err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to edit report message", zap.Error(err))
	}
	return nil
}

// onReportFormat format tanlanganda hisobotni yuboradi.
func (h *Handler) onReportFormat(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Now()
	list, err := h.billing.ListPayments(ctx, p.since(now))
	if err != nil {
		return err
	}
	_ = c.Respond()
	h.edit(c, fmt.Sprintf("📊 <b>To'lovlar hisoboti — %s</b>", p.Title))

	if args[1] == reportText {
		return c.Send(textReport(list, p, now))
	}
	data, err := csvReport(list)
	if err != nil {
		return err
	}
	st := computeStats(list)
	return c.Send(&tele.Document{
		File:     tele.FromReader(bytes.NewReader(data)),
		FileName: fmt.Sprintf("tolovlar_%s_%s.csv", p.ID, now.In(tz.Tashkent).Format("2006-01-02_1504")),
		MIME:     "text/csv",
		Caption: fmt.Sprintf("📊 To'lovlar — %s\n✅ Tasdiqlangan: %d ta · %s\nJami yozuvlar: %d",
			p.Title, st.approved.count, formatSum(st.approved.sum), len(list)),
	})
}

func reportPeriodKeyboard() *tele.ReplyMarkup { return periodKeyboard(cbReportPeriod) }
func reportFormatKeyboard(period string) *tele.ReplyMarkup {
	return formatKeyboard(cbReportFormat, period)
}

// periodKeyboard davr tanlash tugmalari (hisobot va foydalanuvchilar ro'yxati uchun).
func periodKeyboard(unique string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var btns []tele.Btn
	for _, p := range reportPeriods {
		btns = append(btns, m.Data(p.Button, unique, p.ID))
	}
	m.Inline(m.Row(btns[:2]...), m.Row(btns[2:]...))
	return m
}

// formatKeyboard format tanlash tugmalari: matn yoki CSV.
func formatKeyboard(unique, period string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(
		m.Data("💬 Matn", unique, period, reportText),
		m.Data("📄 CSV", unique, period, reportCSV),
	))
	return m
}

// counter — to'lovlar soni va summasi.
type counter struct{ count, sum int }

func (c *counter) add(amount int) { c.count++; c.sum += amount }

type reportStats struct {
	approved, photo, plain, custom, review counter
	rejected                               int
}

func computeStats(list []storage.Payment) reportStats {
	var st reportStats
	for _, p := range list {
		switch p.Status {
		case storage.PaymentApproved:
			st.approved.add(p.Amount)
			if _, ok := storage.KindTariff(p.Kind); ok {
				st.custom.add(p.Amount)
			} else if p.Kind == storage.KindPhoto {
				st.photo.add(p.Amount)
			} else {
				st.plain.add(p.Amount)
			}
		case storage.PaymentReview:
			st.review.add(p.Amount)
		case storage.PaymentRejected:
			st.rejected++
		}
	}
	return st
}

// textReport matnli hisobot: umumiy statistika va to'lovlar ro'yxati
// (sig'maganlari qirqiladi — to'liq ro'yxat CSV da).
func textReport(list []storage.Payment, p reportPeriod, now time.Time) string {
	st := computeStats(list)
	var b strings.Builder
	fmt.Fprintf(&b, "📊 <b>To'lovlar hisoboti — %s</b>\n", p.Title)
	fmt.Fprintf(&b, "🕒 %s holatiga (Toshkent vaqti)\n\n", now.In(tz.Tashkent).Format(dateTimeLayout))
	fmt.Fprintf(&b, "✅ Tasdiqlangan: <b>%d</b> ta · <b>%s</b>\n", st.approved.count, formatSum(st.approved.sum))
	fmt.Fprintf(&b, "     📷 Rasmli: %d ta · %s\n", st.photo.count, formatSum(st.photo.sum))
	fmt.Fprintf(&b, "     📄 Rasmsiz: %d ta · %s\n", st.plain.count, formatSum(st.plain.sum))
	if st.custom.count > 0 {
		fmt.Fprintf(&b, "     ✨ Maxsus dizayn: %d ta · %s\n", st.custom.count, formatSum(st.custom.sum))
	}
	fmt.Fprintf(&b, "⏳ Tekshiruvda: %d ta · %s\n", st.review.count, formatSum(st.review.sum))
	if st.review.count > 0 {
		b.WriteString("     👉 Tasdiqlash/rad etish: /tekshiruv\n")
	}
	fmt.Fprintf(&b, "❌ Rad etilgan: %d ta\n", st.rejected)

	if len(list) == 0 {
		b.WriteString("\nBu davrda to'lovlar yo'q.")
		return b.String()
	}
	b.WriteString("\n<b>To'lovlar:</b>\n")
	for i, pay := range list {
		line := reportLine(pay)
		if utf8.RuneCountInString(b.String())+utf8.RuneCountInString(line) > maxReportRunes {
			fmt.Fprintf(&b, "… yana %d ta. To'liq ro'yxat uchun CSV ni tanlang.", len(list)-i)
			break
		}
		b.WriteString(line)
	}
	return strings.TrimRight(b.String(), "\n")
}

// reportLine: "✅ #15 · 10 000 so'm · @user · 123456 · 16.11.2026 14:32 · /chek_15".
func reportLine(p storage.Payment) string {
	icon := "❌"
	switch p.Status {
	case storage.PaymentApproved:
		icon = "✅"
	case storage.PaymentReview:
		icon = "⏳"
	}
	user := strconv.FormatInt(p.TelegramUserID, 10)
	if p.Username != "" {
		user = "@" + html.EscapeString(p.Username) + " · " + user
	}
	return fmt.Sprintf("%s #%d · %s · %s · %s · /chek_%d\n", icon, p.ID, formatSum(p.Amount), user,
		p.CreatedAt.In(tz.Tashkent).Format(dateTimeLayout), p.ID)
}

// csvHeader — CSV hisobot ustunlari.
var csvHeader = []string{
	"payment_id", "user_id", "telegram_user_id", "username", "order_id", "kind",
	"amount", "detected_amount", "status", "created_at", "check_message_id",
	"check_file_id", "transaction_id", "reason", "reviewed_by", "reviewed_at",
}

// csvReport to'lovlarni CSV ko'rinishida qaytaradi (Excel o'zbekcha harflarni
// to'g'ri ko'rsatishi uchun UTF-8 BOM bilan). Vaqtlar — Toshkent vaqtida.
func csvReport(list []storage.Payment) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	w := csv.NewWriter(&buf)
	if err := w.Write(csvHeader); err != nil {
		return nil, err
	}
	const layout = "2006-01-02 15:04:05"
	for _, p := range list {
		reviewedBy, reviewedAt := "", ""
		if p.ReviewedBy != nil {
			reviewedBy = strconv.FormatInt(*p.ReviewedBy, 10)
		}
		if p.ReviewedAt != nil {
			reviewedAt = p.ReviewedAt.In(tz.Tashkent).Format(layout)
		}
		row := []string{
			strconv.FormatUint(uint64(p.ID), 10),
			strconv.FormatUint(uint64(p.UserID), 10),
			strconv.FormatInt(p.TelegramUserID, 10),
			p.Username,
			strconv.FormatUint(uint64(p.OrderID), 10),
			p.Kind,
			strconv.Itoa(p.Amount),
			strconv.Itoa(p.DetectedAmount),
			p.Status,
			p.CreatedAt.In(tz.Tashkent).Format(layout),
			strconv.Itoa(p.CheckMessageID),
			p.CheckFileID,
			p.TransactionID,
			strings.ReplaceAll(p.Reason, "\n", "; "),
			reviewedBy,
			reviewedAt,
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

// ---------------------------------------------------------------------------
// Chekni ko'rish
// ---------------------------------------------------------------------------

// onChek /chek ID buyrug'i.
func (h *Handler) onChek(c tele.Context) error {
	return h.showCheck(c, c.Message().Payload)
}

// showCheck chek rasmini ma'lumotlari bilan yuboradi; tekshiruvdagi chek
// uchun tasdiqlash/rad etish tugmalari ham qo'shiladi.
func (h *Handler) showCheck(c tele.Context, arg string) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	if i := strings.IndexByte(arg, '@'); i >= 0 { // "/chek_12@botname"
		arg = arg[:i]
	}
	id, err := strconv.ParseUint(strings.TrimSpace(arg), 10, 64)
	if err != nil || id == 0 {
		return c.Send(msgChekUsage)
	}

	ctx, cancel := requestContext()
	defer cancel()
	p, err := h.billing.GetPayment(ctx, uint(id))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.Send(fmt.Sprintf("Chek #%d topilmadi.", id))
	}
	if err != nil {
		return err
	}
	o, err := h.billing.GetOrder(ctx, p.OrderID)
	if err != nil {
		o = nil
	}

	media := checkMedia(p, reviewCaption(p, o))
	if p.Status == storage.PaymentReview {
		return c.Send(media, reviewKeyboard(p.ID))
	}
	return c.Send(media)
}

// onPendingChecks /tekshiruv — admin qarorini kutayotgan cheklarni (eskilari
// birinchi) ✅/❌ tugmalari bilan qayta yuboradi. Admin xabarni o'tkazib
// yuborgan, o'chirib yuborgan yoki chek kelganidan keyin admin bo'lgan
// holatlar uchun.
func (h *Handler) onPendingChecks(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	list, total, err := h.billing.ListPaymentsByStatus(ctx, storage.PaymentReview, maxPendingChecks)
	if err != nil {
		return err
	}
	if total == 0 {
		return c.Send(msgNoPending)
	}
	header := fmt.Sprintf("⏳ <b>Tekshiruvda %d ta chek</b>", total)
	if total > len(list) {
		header += fmt.Sprintf("\nEng eskilaridan %d tasi yuborilmoqda. Qolganlari uchun ularni ko'rib chiqib, /tekshiruv ni qayta yuboring.", len(list))
	}
	if err := c.Send(header); err != nil {
		return err
	}
	for i := range list {
		p := &list[i]
		o, err := h.billing.GetOrder(ctx, p.OrderID)
		if err != nil {
			o = nil
		}
		if err := c.Send(checkMedia(p, reviewCaption(p, o)), reviewKeyboard(p.ID)); err != nil {
			h.log.Warn("failed to send pending check", zap.Uint("payment_id", p.ID), zap.Error(err))
		}
	}
	return nil
}
