package storage

// Tashrif buyuruvchilar statistikasi: har bir foydalanuvchining botga kirgan
// kunlari (kuniga bitta yozuv), admin hisoboti uchun hisob-kitoblar va
// foydalanuvchilar ro'yxati.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Visit — foydalanuvchining bir kundagi tashrifi (botga yozgan yoki tugma
// bosgan). Kun bot vaqt mintaqasida hisoblanadi; bir foydalanuvchi uchun
// kuniga bitta yozuv.
type Visit struct {
	Day    time.Time `gorm:"type:date;primaryKey"`
	UserID int64     `gorm:"primaryKey;autoIncrement:false"`
}

// PeriodStats — bitta davr ko'rsatkichlari.
type PeriodStats struct {
	NewUsers    int64 // botga birinchi marta kirganlar
	ActiveUsers int64 // kamida bir marta kirganlar (har biri bir marta sanaladi)
	Customers   int64 // tayyor ish olgan foydalanuvchilar
	Delivered   int64 // yetkazilgan ishlar
}

// DayStats — bitta kun ko'rsatkichlari.
type DayStats struct {
	Day         time.Time // kun boshi (repozitoriy vaqt mintaqasida)
	NewUsers    int64
	ActiveUsers int64
}

// StatsRepository tashriflarni saqlaydi va statistikani hisoblaydi.
type StatsRepository struct {
	db  *gorm.DB
	loc *time.Location
}

// NewStatsRepository yangi repozitoriy yaratadi. loc — kunlar chegarasi
// qaysi vaqt mintaqasida hisoblanishi (masalan, Toshkent).
func NewStatsRepository(db *gorm.DB, loc *time.Location) *StatsRepository {
	if loc == nil {
		loc = time.UTC
	}
	return &StatsRepository{db: db, loc: loc}
}

// date — t ning loc mintaqasidagi sanasi (date ustuni uchun UTC yarim tun).
func (r *StatsRepository) date(t time.Time) time.Time {
	y, m, d := t.In(r.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// RecordVisit foydalanuvchining at kunidagi tashrifini yozadi (shu kunda
// allaqachon yozilgan bo'lsa, hech narsa qilmaydi).
func (r *StatsRepository) RecordVisit(ctx context.Context, userID int64, at time.Time) error {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&Visit{Day: r.date(at), UserID: userID}).Error
	if err != nil {
		return fmt.Errorf("storage: tashrifni yozib bo'lmadi: %w", err)
	}
	return nil
}

// Overview barcha vaqt (all) va har bir since dan boshlab (periods[i] —
// sinces[i] uchun) ko'rsatkichlarni 3 ta so'rovda hisoblaydi: har bir jadval
// bir marta o'qiladi, davrlar FILTER bilan ajratiladi. since kun boshi
// bo'lishi kerak: faol foydalanuvchilar kunlar bo'yicha sanaladi.
func (r *StatsRepository) Overview(ctx context.Context, sinces []time.Time) (all PeriodStats, periods []PeriodStats, err error) {
	periods = make([]PeriodStats, len(sinces))
	var users, visits, delivered countQuery
	users.add("COUNT(*)", &all.NewUsers)
	visits.add("COUNT(DISTINCT user_id)", &all.ActiveUsers)
	delivered.add("COUNT(*)", &all.Delivered)
	delivered.add("COUNT(DISTINCT telegram_user_id)", &all.Customers)
	for i, since := range sinces {
		p := &periods[i]
		users.addSince("COUNT(*)", "created_at", since, &p.NewUsers)
		visits.addSince("COUNT(DISTINCT user_id)", "day", r.date(since), &p.ActiveUsers)
		delivered.addSince("COUNT(*)", "delivered_at", since, &p.Delivered)
		delivered.addSince("COUNT(DISTINCT telegram_user_id)", "delivered_at", since, &p.Customers)
	}

	db := r.db.WithContext(ctx)
	if err := users.scan(db, "sessions"); err != nil {
		return PeriodStats{}, nil, fmt.Errorf("storage: yangi foydalanuvchilarni o'qib bo'lmadi: %w", err)
	}
	if err := visits.scan(db, "visits"); err != nil {
		return PeriodStats{}, nil, fmt.Errorf("storage: faol foydalanuvchilarni o'qib bo'lmadi: %w", err)
	}
	if err := delivered.scan(db, "orders WHERE delivered_at IS NOT NULL"); err != nil {
		return PeriodStats{}, nil, fmt.Errorf("storage: yetkazilgan ishlarni o'qib bo'lmadi: %w", err)
	}
	return all, periods, nil
}

// countQuery — bitta jadval bo'yicha bir nechta sanoqni bitta so'rovda
// o'qiydi. Ustun nomlari va agregatlar faqat kod ichidagi o'zgarmaslar;
// qiymatlar (since) SQL matniga qo'shilmaydi — faqat placeholder orqali
// uzatiladi.
type countQuery struct {
	cols []string
	args []any
	dest []any
}

// add filtrsiz agregat qo'shadi (barcha vaqt).
func (q *countQuery) add(agg string, dest *int64) {
	q.cols = append(q.cols, agg)
	q.dest = append(q.dest, dest)
}

// addSince faqat col >= since bo'lgan qatorlar bo'yicha agregat qo'shadi.
func (q *countQuery) addSince(agg, col string, since time.Time, dest *int64) {
	q.cols = append(q.cols, agg+" FILTER (WHERE "+col+" >= ?)")
	q.args = append(q.args, since)
	q.dest = append(q.dest, dest)
}

// scan so'rovni bajaradi va natijalarni dest larga yozadi.
func (q *countQuery) scan(db *gorm.DB, from string) error {
	return db.Raw("SELECT "+strings.Join(q.cols, ", ")+" FROM "+from, q.args...).Row().Scan(q.dest...)
}

// DailyStats from kunidan boshlab ketma-ket days kunning ko'rsatkichlari
// (eskisidan yangisiga).
func (r *StatsRepository) DailyStats(ctx context.Context, from time.Time, days int) ([]DayStats, error) {
	if days < 1 {
		return nil, nil
	}
	db := r.db.WithContext(ctx)
	y, m, d := from.In(r.loc).Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, r.loc)

	var created []time.Time
	if err := db.Model(&Session{}).Where("created_at >= ?", start).Pluck("created_at", &created).Error; err != nil {
		return nil, fmt.Errorf("storage: yangi foydalanuvchilarni o'qib bo'lmadi: %w", err)
	}
	newByDay := make(map[string]int64)
	for _, t := range created {
		newByDay[t.In(r.loc).Format(time.DateOnly)]++
	}

	var rows []struct {
		Day time.Time
		N   int64
	}
	if err := db.Model(&Visit{}).Select("day, COUNT(*) AS n").Where("day >= ?", r.date(start)).
		Group("day").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("storage: kunlik tashriflarni o'qib bo'lmadi: %w", err)
	}
	activeByDay := make(map[string]int64, len(rows))
	for _, row := range rows {
		activeByDay[row.Day.UTC().Format(time.DateOnly)] = row.N
	}

	out := make([]DayStats, days)
	for i := range out {
		day := start.AddDate(0, 0, i)
		key := day.Format(time.DateOnly)
		out[i] = DayStats{Day: day, NewUsers: newByDay[key], ActiveUsers: activeByDay[key]}
	}
	return out, nil
}

// UserSummary — admin uchun foydalanuvchilar ro'yxatidagi bitta foydalanuvchi.
type UserSummary struct {
	UserID      int64
	Username    string
	FirstName   string // Telegram'dagi ismi (narx hisoblangan yoki to'lov qilgan bo'lsa saqlangan)
	StudentName string // titul uchun yozgan F.I.Sh.
	University  string
	Language    string
	IsAdmin     bool
	CreatedAt   time.Time  // botga birinchi kirgan vaqt
	UpdatedAt   time.Time  // sessiya oxirgi o'zgargan vaqt
	LastVisit   *time.Time // oxirgi tashrif kuni (date: UTC yarim tun)
	Delivered   int64      // yetkazilgan (tayyor) ishlar
	Payments    int64      // tasdiqlangan to'lovlar soni
	PaidSum     int64      // tasdiqlangan to'lovlar summasi, so'm
}

// LastActive foydalanuvchi oxirgi faol bo'lgan kun boshi (loc mintaqasida):
// tashriflar jadvali va sessiya o'zgarishining kechroqi.
func (u UserSummary) LastActive(loc *time.Location) time.Time {
	t := u.UpdatedAt.In(loc)
	last := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	if u.LastVisit != nil {
		// date ustuni kunni UTC yarim tun sifatida saqlaydi (RecordVisit).
		y, m, d := u.LastVisit.UTC().Date()
		if v := time.Date(y, m, d, 0, 0, 0, 0, loc); v.After(last) {
			last = v
		}
	}
	return last
}

// ListUsers since dan boshlab (nil — barcha vaqt) botga kirgan
// foydalanuvchilar: profil, oxirgi tashrif, tayyor ishlar va tasdiqlangan
// to'lovlar. Eng yangi foydalanuvchi birinchi.
func (r *StatsRepository) ListUsers(ctx context.Context, since *time.Time) ([]UserSummary, error) {
	q := `SELECT s.user_id, s.username, COALESCE(u.first_name, '') AS first_name,
		s.student_name, s.university, s.language, COALESCE(u.is_admin, FALSE) AS is_admin,
		s.created_at, s.updated_at, v.last_visit,
		COALESCE(o.delivered, 0) AS delivered, COALESCE(p.payments, 0) AS payments, COALESCE(p.paid_sum, 0) AS paid_sum
	FROM sessions s
	LEFT JOIN users u ON u.telegram_user_id = s.user_id
	LEFT JOIN (SELECT user_id, MAX(day) AS last_visit FROM visits GROUP BY user_id) v ON v.user_id = s.user_id
	LEFT JOIN (SELECT telegram_user_id, COUNT(*) AS delivered FROM orders
		WHERE delivered_at IS NOT NULL GROUP BY telegram_user_id) o ON o.telegram_user_id = s.user_id
	LEFT JOIN (SELECT telegram_user_id, COUNT(*) AS payments, SUM(amount) AS paid_sum FROM payments
		WHERE status = ? GROUP BY telegram_user_id) p ON p.telegram_user_id = s.user_id`
	args := []any{PaymentApproved}
	if since != nil {
		q += "\n\tWHERE s.created_at >= ?"
		args = append(args, *since)
	}
	q += "\n\tORDER BY s.created_at DESC, s.user_id DESC"

	var out []UserSummary
	if err := r.db.WithContext(ctx).Raw(q, args...).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("storage: foydalanuvchilar ro'yxatini o'qib bo'lmadi: %w", err)
	}
	return out, nil
}

// ReferralTotals — do'stlarni taklif qilish tizimi ko'rsatkichlari (barcha vaqt).
type ReferralTotals struct {
	Joined      int64 // taklif havolasi orqali kelgan yangi foydalanuvchilar
	Rewarded    int64 // ulardan buyurtmani to'lab, taklif qilganga mukofot keltirganlar
	BonusOrders int64 // taklif bonusi ishlatilgan (to'langan yoki yetkazilgan) buyurtmalar
}

// ReferralTotals taklif tizimi ko'rsatkichlarini bitta so'rovda hisoblaydi.
func (r *StatsRepository) ReferralTotals(ctx context.Context) (ReferralTotals, error) {
	var t ReferralTotals
	err := r.db.WithContext(ctx).Raw(`SELECT
		(SELECT COUNT(*) FROM referrals) AS joined,
		(SELECT COUNT(*) FROM referrals WHERE rewarded_at IS NOT NULL) AS rewarded,
		(SELECT COUNT(*) FROM orders WHERE discount > 0 AND status IN (?, ?)) AS bonus_orders`,
		OrderPaid, OrderDelivered).Scan(&t).Error
	if err != nil {
		return ReferralTotals{}, fmt.Errorf("storage: taklif ko'rsatkichlarini o'qib bo'lmadi: %w", err)
	}
	return t, nil
}
