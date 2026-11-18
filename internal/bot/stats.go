package bot

// Tashrif buyuruvchilar statistikasi: har bir foydalanuvchining shu kundagi
// tashrifi yoziladi (kuniga bir marta), admin esa /statistika buyrug'i bilan
// bugungi, 7 va 30 kunlik ko'rsatkichlarni va kunlar bo'yicha jadvalni ko'radi.

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/payment"
	"telegram-presentation-bot/internal/storage"
)

// StatsStore — tashriflar ombori (internal/storage.StatsRepository).
type StatsStore interface {
	RecordVisit(ctx context.Context, userID int64, at time.Time) error
	TotalUsers(ctx context.Context) (int64, error)
	PeriodStats(ctx context.Context, since *time.Time) (storage.PeriodStats, error)
	DailyStats(ctx context.Context, from time.Time, days int) ([]storage.DayStats, error)
	ListUsers(ctx context.Context, since *time.Time) ([]storage.UserSummary, error)
}

const (
	visitTimeout = 3 * time.Second // tashrifni yozish uchun baza amali vaqti
	statsDays    = 7               // kunlar bo'yicha jadvaldagi kunlar soni
)

// statsPeriods — hisobotdagi davrlar: kunlar soni (bugun bilan birga).
var statsPeriods = []struct {
	title string
	days  int
}{
	{"Bugun", 1},
	{"7 kun", 7},
	{"30 kun", 30},
}

// weekdays — hafta kunlarining qisqa nomlari (time.Weekday tartibida).
var weekdays = [...]string{"Ya", "Du", "Se", "Ch", "Pa", "Ju", "Sh"}

// ---------------------------------------------------------------------------
// Tashriflarni yozish
// ---------------------------------------------------------------------------

// visitTracker — bugun tashrifi yozilgan foydalanuvchilar: bazaga har bir
// update'da emas, foydalanuvchi uchun kuniga bir marta murojaat qilinadi.
// Kun almashganda ro'yxat tozalanadi (xotira cheklangan bo'ladi).
type visitTracker struct {
	mu   sync.Mutex
	day  string
	seen map[int64]struct{}
}

// mark foydalanuvchini day kunida belgilaydi; shu kunda birinchi marta
// bo'lsa true qaytaradi.
func (t *visitTracker) mark(userID int64, day string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.day != day || t.seen == nil {
		t.day, t.seen = day, make(map[int64]struct{})
	}
	if _, ok := t.seen[userID]; ok {
		return false
	}
	t.seen[userID] = struct{}{}
	return true
}

// forget belgini olib tashlaydi (bazaga yozib bo'lmaganda — keyingi
// update'da qayta urinish uchun).
func (t *visitTracker) forget(userID int64, day string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.day == day {
		delete(t.seen, userID)
	}
}

// trackVisits har bir update'da foydalanuvchining bugungi tashrifini yozadi.
// Baza xatosi update'ni to'xtatmaydi — faqat logga yoziladi.
func (h *Handler) trackVisits(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if h.stats != nil {
			h.recordVisit(c.Sender().ID, time.Now())
		}
		return next(c)
	}
}

func (h *Handler) recordVisit(userID int64, now time.Time) {
	day := now.In(payment.Tashkent).Format(time.DateOnly)
	if !h.visits.mark(userID, day) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), visitTimeout)
	defer cancel()
	if err := h.stats.RecordVisit(ctx, userID, now); err != nil {
		h.visits.forget(userID, day)
		h.log.Warn("failed to record visit", zap.Int64("user_id", userID), zap.Error(err))
	}
}

// ---------------------------------------------------------------------------
// /statistika
// ---------------------------------------------------------------------------

// periodRow — hisobotdagi bitta davr.
type periodRow struct {
	title string
	stats storage.PeriodStats
}

// statsReport — /statistika ma'lumotlari.
type statsReport struct {
	now     time.Time
	total   int64
	all     storage.PeriodStats // barcha vaqt
	periods []periodRow
	days    []storage.DayStats // eskisidan yangisiga
}

// onStats /statistika — tashrif buyuruvchilar statistikasi (faqat admin).
func (h *Handler) onStats(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	if h.stats == nil {
		return c.Send("Statistika hozircha mavjud emas.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rep, err := h.collectStats(ctx, time.Now())
	if err != nil {
		return err
	}
	return c.Send(statsText(rep))
}

// collectStats hisobot uchun barcha ko'rsatkichlarni bazadan oladi. Davrlar
// Toshkent vaqti bo'yicha kun boshidan hisoblanadi ("7 kun" — bugun va
// oldingi 6 kun).
func (h *Handler) collectStats(ctx context.Context, now time.Time) (statsReport, error) {
	local := now.In(payment.Tashkent)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, payment.Tashkent)
	rep := statsReport{now: now}

	var err error
	if rep.total, err = h.stats.TotalUsers(ctx); err != nil {
		return rep, err
	}
	if rep.all, err = h.stats.PeriodStats(ctx, nil); err != nil {
		return rep, err
	}
	for _, p := range statsPeriods {
		since := today.AddDate(0, 0, 1-p.days)
		st, err := h.stats.PeriodStats(ctx, &since)
		if err != nil {
			return rep, err
		}
		rep.periods = append(rep.periods, periodRow{title: p.title, stats: st})
	}
	if rep.days, err = h.stats.DailyStats(ctx, today.AddDate(0, 0, 1-statsDays), statsDays); err != nil {
		return rep, err
	}
	return rep, nil
}

// statsText hisobot matni. Jadvallar <pre> ichida — telefonda ham ustunlar
// tekis turishi uchun qatorlar 32 belgidan oshmaydi.
func statsText(r statsReport) string {
	var b strings.Builder
	b.WriteString("👥 <b>Tashrif buyuruvchilar statistikasi</b>\n")
	fmt.Fprintf(&b, "🕒 %s holatiga (Toshkent vaqti)\n\n", r.now.In(payment.Tashkent).Format("02.01.2006 15:04"))
	fmt.Fprintf(&b, "👤 Jami foydalanuvchilar: <b>%s</b>\n", formatCount(r.total))
	fmt.Fprintf(&b, "📦 Jami tayyor ishlar: <b>%s</b> ta (%s kishi)\n\n", formatCount(r.all.Delivered), formatCount(r.all.Customers))

	if len(r.periods) > 0 {
		const label, gap = 10, "  "
		rows := []struct {
			name string
			val  func(storage.PeriodStats) int64
		}{
			{"Yangi", func(s storage.PeriodStats) int64 { return s.NewUsers }},
			{"Faol", func(s storage.PeriodStats) int64 { return s.ActiveUsers }},
			{"Ish olgan", func(s storage.PeriodStats) int64 { return s.Customers }},
			{"Tayyor ish", func(s storage.PeriodStats) int64 { return s.Delivered }},
		}
		widths := make([]int, len(r.periods))
		for i, p := range r.periods {
			widths[i] = len([]rune(p.title))
			for _, row := range rows {
				widths[i] = max(widths[i], len(formatCount(row.val(p.stats))))
			}
		}
		b.WriteString("<pre>")
		b.WriteString(strings.Repeat(" ", label))
		for i, p := range r.periods {
			b.WriteString(gap + padLeft(p.title, widths[i]))
		}
		for _, row := range rows {
			b.WriteString("\n" + padRight(row.name, label))
			for i, p := range r.periods {
				b.WriteString(gap + padLeft(formatCount(row.val(p.stats)), widths[i]))
			}
		}
		b.WriteString("</pre>\n")
	}

	if len(r.days) > 0 {
		b.WriteString("\n📅 <b>Kunlar bo'yicha</b>\n<pre>")
		b.WriteString("Sana        Yangi   Faol")
		days := slices.Clone(r.days)
		slices.Reverse(days)
		for _, d := range days {
			day := d.Day.In(payment.Tashkent)
			fmt.Fprintf(&b, "\n%s (%s) %s %s", day.Format("02.01"), weekdays[day.Weekday()],
				padLeft(formatCount(d.NewUsers), 6), padLeft(formatCount(d.ActiveUsers), 6))
		}
		b.WriteString("</pre>\n")
	}

	b.WriteString("\n<i>Yangi — botga birinchi marta kirganlar. Faol — botga yozgan yoki tugma bosganlar " +
		"(davr ichida har biri bir marta sanaladi). Ish olgan — tayyor fayl olgan foydalanuvchilar.</i>")
	return b.String()
}

// formatCount sonni minglar bo'yicha ajratadi: 12345 → "12 345".
func formatCount(n int64) string {
	return groupDigits(strconv.FormatInt(n, 10), 3, true)
}

func padLeft(s string, width int) string {
	if n := len([]rune(s)); n < width {
		return strings.Repeat(" ", width-n) + s
	}
	return s
}

func padRight(s string, width int) string {
	if n := len([]rune(s)); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
