package storage

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestStatsRepository(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	loc := time.FixedZone("UZT", 5*60*60)
	r := NewStatsRepository(db, loc)

	today := time.Date(2026, 11, 18, 0, 0, 0, 0, loc)
	at := func(daysAgo, hour int) time.Time {
		return today.AddDate(0, 0, -daysAgo).Add(time.Duration(hour) * time.Hour)
	}

	// Foydalanuvchilar: 1 — 10 kun oldin, 2 — 3 kun oldin, 3 — bugun kirgan
	// (kirgan kuni tashrif ham yoziladi).
	for id, created := range map[int64]time.Time{1: at(10, 12), 2: at(3, 9), 3: at(0, 1)} {
		s := Session{UserID: id, ChatID: id, State: "idle", CreatedAt: created, UpdatedAt: created}
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.RecordVisit(ctx, id, created); err != nil {
			t.Fatal(err)
		}
	}

	// Tashriflar: 1-foydalanuvchi bugun ikki marta (bitta yozuv), kecha bir marta;
	// 2-foydalanuvchi bugun. 00:30 Toshkentda — UTC bo'yicha hali kecha.
	for _, v := range []struct {
		user int64
		at   time.Time
	}{{1, at(0, 0).Add(30 * time.Minute)}, {1, at(0, 15)}, {1, at(1, 23)}, {2, at(0, 8)}} {
		if err := r.RecordVisit(ctx, v.user, v.at); err != nil {
			t.Fatal(err)
		}
	}

	// Buyurtmalar: 2-foydalanuvchiga 2 ta ish yetkazilgan (bittasi 20 kun oldin),
	// 3-foydalanuvchiniki hali yetkazilmagan.
	for _, o := range []Order{
		{UserID: 1, TelegramUserID: 2, WorkType: "presentation", Kind: KindPlain, Status: OrderDelivered, DeliveredAt: ptr(at(0, 9))},
		{UserID: 1, TelegramUserID: 2, WorkType: "presentation", Kind: KindPlain, Status: OrderDelivered, DeliveredAt: ptr(at(20, 9))},
		{UserID: 2, TelegramUserID: 3, WorkType: "presentation", Kind: KindPlain, Status: OrderPaid},
	} {
		if err := db.Create(&o).Error; err != nil {
			t.Fatal(err)
		}
	}

	overall, periods, err := r.Overview(ctx, []time.Time{today, today.AddDate(0, 0, -6)})
	if err != nil {
		t.Fatal(err)
	}
	if want := (PeriodStats{NewUsers: 3, ActiveUsers: 3, Customers: 1, Delivered: 2}); overall != want {
		t.Errorf("barcha vaqt: %+v, kutilgan %+v", overall, want)
	}
	wantPeriods := []PeriodStats{
		{NewUsers: 1, ActiveUsers: 3, Customers: 1, Delivered: 1}, // bugun
		{NewUsers: 2, ActiveUsers: 3, Customers: 1, Delivered: 1}, // 7 kun
	}
	if !slices.Equal(periods, wantPeriods) {
		t.Errorf("davrlar: %+v, kutilgan %+v", periods, wantPeriods)
	}

	days, err := r.DailyStats(ctx, today.AddDate(0, 0, -3), 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []DayStats{
		{Day: today.AddDate(0, 0, -3), NewUsers: 1, ActiveUsers: 1}, // 2-foydalanuvchi kirgan kuni
		{Day: today.AddDate(0, 0, -2)},
		{Day: today.AddDate(0, 0, -1), ActiveUsers: 1},
		{Day: today, NewUsers: 1, ActiveUsers: 3},
	}
	if len(days) != len(want) {
		t.Fatalf("kunlar soni %d", len(days))
	}
	for i, w := range want {
		g := days[i]
		if !g.Day.Equal(w.Day) || g.NewUsers != w.NewUsers || g.ActiveUsers != w.ActiveUsers {
			t.Errorf("%d-kun: %+v, kutilgan %+v", i, g, w)
		}
	}

	// Foydalanuvchilar ro'yxati: profil (users), tayyor ishlar (orders),
	// tasdiqlangan to'lovlar (payments) va oxirgi tashrif (visits).
	billing := NewBillingRepository(db)
	u2, err := billing.EnsureUser(ctx, 2, "vali", "Vali")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := billing.SetAdmin(ctx, 2); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Payment{
		{UserID: u2.ID, TelegramUserID: 2, OrderID: 1, Kind: KindPlain, Amount: 10000, Status: PaymentApproved, CheckFileID: "a", CheckMessageID: 1},
		{UserID: u2.ID, TelegramUserID: 2, OrderID: 2, Kind: KindPlain, Amount: 15000, Status: PaymentApproved, CheckFileID: "b", CheckMessageID: 2},
		{UserID: u2.ID, TelegramUserID: 2, OrderID: 2, Kind: KindPlain, Amount: 99000, Status: PaymentRejected, CheckFileID: "c", CheckMessageID: 3},
	} {
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&Session{}).Where("user_id = ?", 3).
		Updates(map[string]any{"student_name": "Karimova D.", "username": "dilnoza"}).Error; err != nil {
		t.Fatal(err)
	}

	all, err := r.ListUsers(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].UserID != 3 || all[1].UserID != 2 || all[2].UserID != 1 {
		t.Fatalf("ro'yxat eng yangisidan boshlanishi kerak: %+v", all)
	}
	vali := all[1]
	if vali.FirstName != "Vali" || !vali.IsAdmin || vali.Delivered != 2 || vali.Payments != 2 || vali.PaidSum != 25000 {
		t.Errorf("2-foydalanuvchi: %+v", vali)
	}
	if vali.LastVisit == nil || !vali.LastActive(loc).Equal(today) {
		t.Errorf("2-foydalanuvchi bugun faol bo'lgan: %+v", vali.LastVisit)
	}
	dilnoza := all[0]
	if dilnoza.StudentName != "Karimova D." || dilnoza.Username != "dilnoza" || dilnoza.FirstName != "" ||
		dilnoza.IsAdmin || dilnoza.Delivered != 0 || dilnoza.PaidSum != 0 {
		t.Errorf("3-foydalanuvchi (users jadvalida yo'q, yetkazilmagan buyurtma): %+v", dilnoza)
	}
	week, err := r.ListUsers(ctx, ptr(today.AddDate(0, 0, -6)))
	if err != nil {
		t.Fatal(err)
	}
	if len(week) != 2 || week[0].UserID != 3 || week[1].UserID != 2 {
		t.Errorf("7 kunlik ro'yxat: %+v", week)
	}
}

// tashkent — bot ishlatadigan mintaqa (tz.Tashkent bilan bir xil: UTC+5).
var tashkent = time.FixedZone("UZT", 5*60*60)

// mkSession sessiyani berilgan yaratilish vaqti bilan yozadi.
func mkSession(t *testing.T, db *gorm.DB, userID int64, created time.Time) {
	t.Helper()
	s := Session{UserID: userID, ChatID: userID, State: "idle", CreatedAt: created, UpdatedAt: created}
	if err := db.Create(&s).Error; err != nil {
		t.Fatal(err)
	}
}

func TestRecordVisitIdempotent(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewStatsRepository(db, tashkent)

	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, tashkent) }
	for _, v := range []struct {
		user int64
		at   time.Time
	}{
		{1, at(18, 0, 0)}, // kun boshi (UTC bo'yicha hali 17-noyabr 19:00)
		{1, at(18, 10, 0)},
		{1, at(18, 10, 0)}, // aynan takror
		{1, at(18, 23, 59)},
		{1, at(18, 0, 0).UTC()}, // shu payt UTC da berilgan
		{1, at(17, 23, 59)},     // oldingi kun
		{1, at(19, 0, 0)},       // keyingi kun
		{2, at(18, 12, 0)},
	} {
		if err := r.RecordVisit(ctx, v.user, v.at); err != nil {
			t.Fatalf("RecordVisit(%d, %v): %v", v.user, v.at, err)
		}
	}

	var visits []Visit
	if err := db.Order("day, user_id").Find(&visits).Error; err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range visits {
		got = append(got, fmt.Sprintf("%s/%d", v.Day.Format(time.DateOnly), v.UserID))
	}
	want := []string{"2026-11-17/1", "2026-11-18/1", "2026-11-18/2", "2026-11-19/1"}
	if !slices.Equal(got, want) {
		t.Errorf("tashriflar: %v, kutilgan %v", got, want)
	}
}

// queryCounter — bajarilgan SQL so'rovlarni sanaydigan gorm logger.
type queryCounter struct {
	gormlogger.Interface
	n atomic.Int64
}

func (q *queryCounter) Trace(context.Context, time.Time, func() (string, int64), error) { q.n.Add(1) }

// TestOverview: bir nechta davr bitta chaqiruvda, aynan since ga to'g'ri
// keladigan yozuvlar davrga kiradi, 1 mikrosoniya oldingisi — kirmaydi.
// Kutilgan qiymatlar qo'lda hisoblangan; har bir davrda to'rtala ko'rsatkich
// har xil — ustunlar adashib ketsa, test buni ushlaydi.
func TestOverview(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewStatsRepository(db, tashkent)

	d18 := time.Date(2026, 11, 18, 0, 0, 0, 0, tashkent) // UTC: 17-noyabr 19:00
	d12 := d18.AddDate(0, 0, -6)
	b18 := d18.Add(-time.Microsecond) // 17-noyabr 23:59:59.999999 Toshkent
	b12 := d12.Add(-time.Microsecond) // 11-noyabr 23:59:59.999999 Toshkent

	// Yangi foydalanuvchilar: d12 dan oldin 2 ta, [d12, d18) — 2 ta, d18 dan keyin — 4 ta.
	for id, created := range []time.Time{
		d12.AddDate(0, 0, -2), b12, // faqat barcha vaqt
		d12, b18, // + d12
		d18, d18.Add(5 * time.Hour), d18.AddDate(0, 0, 1), d18.AddDate(0, 0, 2), // + d18
	} {
		mkSession(t, db, int64(id+1), created)
	}

	// Faol foydalanuvchilar (kunlar Toshkent vaqtida):
	//   1 — 11-noyabr va 18-noyabr 00:00 (UTC da hali 17-noyabr) → hamma davrda;
	//   2 — 12, 13, 17-noyabr → barcha vaqt, d12 (bir marta sanaladi);
	//   3 — 11-noyabr → faqat barcha vaqt;
	//   4 — 17-noyabr 23:59:59.999999 → barcha vaqt, d12;
	//   5 — 10-noyabr → faqat barcha vaqt;
	//   6 — 14-noyabr → barcha vaqt, d12.
	for _, v := range []struct {
		user int64
		at   time.Time
	}{
		{1, b12}, {1, d18},
		{2, d12}, {2, b18}, {2, d12.AddDate(0, 0, 1)},
		{3, b12},
		{4, b18},
		{5, d12.AddDate(0, 0, -2)},
		{6, d12.AddDate(0, 0, 2)},
	} {
		if err := r.RecordVisit(ctx, v.user, v.at); err != nil {
			t.Fatal(err)
		}
	}

	// Yetkazilgan ishlar: 20 — d12 dan oldin va d18 dan keyin, 21 — d12 dan oldin,
	// 22 — [d12, d18) da ikki ish (aynan d12 va d18 dan 1 mks oldin),
	// 23 — aynan d18 va bir soatdan keyin, 24 — yetkazilmagan.
	for _, o := range []struct {
		tg        int64
		delivered *time.Time
	}{
		{20, ptr(b12)},
		{21, ptr(d12.AddDate(0, 0, -3))},
		{22, ptr(d12)}, {22, ptr(b18)},
		{23, ptr(d18)}, {23, ptr(d18.Add(time.Hour))},
		{20, ptr(d18.AddDate(0, 0, 2))},
		{24, nil},
	} {
		status := OrderDelivered
		if o.delivered == nil {
			status = OrderPaid
		}
		ord := Order{UserID: 1, TelegramUserID: o.tg, WorkType: "presentation", Kind: KindPlain,
			Status: status, DeliveredAt: o.delivered}
		if err := db.Create(&ord).Error; err != nil {
			t.Fatal(err)
		}
	}

	// So'rovlar sonini sanash uchun alohida logger'li sessiya.
	counter := &queryCounter{Interface: gormlogger.Default.LogMode(gormlogger.Silent)}
	counted := NewStatsRepository(db.Session(&gorm.Session{Logger: counter}), tashkent)

	// Davrlar ataylab tartibsiz: natija sinces tartibida qaytishi kerak.
	sinces := []time.Time{
		d18,
		d12,
		d18.UTC(),            // xuddi shu payt UTC da: kun Toshkent bo'yicha hisoblanadi
		d18.AddDate(0, 0, 3), // barcha yozuvlardan keyin
	}
	inD18 := PeriodStats{NewUsers: 4, ActiveUsers: 1, Customers: 2, Delivered: 3}
	wantAll := PeriodStats{NewUsers: 8, ActiveUsers: 6, Customers: 4, Delivered: 7}
	wantPeriods := []PeriodStats{
		inD18,
		{NewUsers: 6, ActiveUsers: 4, Customers: 3, Delivered: 5},
		inD18,
		{},
	}

	all, periods, err := counted.Overview(ctx, sinces)
	if err != nil {
		t.Fatal(err)
	}
	if all != wantAll {
		t.Errorf("barcha vaqt: %+v, kutilgan %+v", all, wantAll)
	}
	if len(periods) != len(wantPeriods) {
		t.Fatalf("davrlar soni %d, kutilgan %d", len(periods), len(wantPeriods))
	}
	for i, w := range wantPeriods {
		if periods[i] != w {
			t.Errorf("since=%v: %+v, kutilgan %+v", sinces[i], periods[i], w)
		}
	}
	if n := counter.n.Load(); n != 3 {
		t.Errorf("so'rovlar soni %d, kutilgan 3", n)
	}

	// Davrlarsiz: faqat barcha vaqt.
	all, periods, err = counted.Overview(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if all != wantAll || len(periods) != 0 {
		t.Errorf("davrlarsiz: %+v %+v, kutilgan %+v []", all, periods, wantAll)
	}
}

// Kunlar chegarasi Toshkent vaqtida: 18-noyabr = 17-noyabr 19:00 UTC dan
// 18-noyabr 19:00 UTC gacha.
func TestDailyStatsTashkentBoundaries(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewStatsRepository(db, tashkent)

	utc := func(day, hour, minute, sec, usec int) time.Time {
		return time.Date(2026, 11, day, hour, minute, sec, usec*1000, time.UTC)
	}
	for _, s := range []struct {
		user    int64
		created time.Time
	}{
		{1, utc(17, 18, 59, 59, 0)},      // Toshkent: 17-noyabr 23:59:59 — oynadan tashqari
		{2, utc(17, 19, 0, 0, 0)},        // 18-noyabr 00:00
		{3, utc(18, 18, 59, 59, 999999)}, // 18-noyabr 23:59:59.999999
		{4, utc(18, 19, 0, 0, 0)},        // 19-noyabr 00:00
		{5, utc(20, 12, 0, 0, 0)},        // 20-noyabr 17:00
		{6, utc(22, 12, 0, 0, 0)},        // oynadan keyin
	} {
		mkSession(t, db, s.user, s.created)
		if err := r.RecordVisit(ctx, s.user, s.created); err != nil {
			t.Fatal(err)
		}
	}
	// 1-foydalanuvchi 18-noyabr 23:30 da, 2-foydalanuvchi 19-noyabr 10:00 da yana kirgan.
	for _, v := range []struct {
		user int64
		at   time.Time
	}{{1, utc(18, 18, 30, 0, 0)}, {2, utc(19, 5, 0, 0, 0)}} {
		if err := r.RecordVisit(ctx, v.user, v.at); err != nil {
			t.Fatal(err)
		}
	}

	want := []DayStats{
		{Day: time.Date(2026, 11, 18, 0, 0, 0, 0, tashkent), NewUsers: 2, ActiveUsers: 3},
		{Day: time.Date(2026, 11, 19, 0, 0, 0, 0, tashkent), NewUsers: 1, ActiveUsers: 2},
		{Day: time.Date(2026, 11, 20, 0, 0, 0, 0, tashkent), NewUsers: 1, ActiveUsers: 1},
	}
	// from kun o'rtasi yoki UTC da berilsa ham Toshkent kuni boshidan hisoblanadi.
	for name, from := range map[string]time.Time{
		"Toshkent 15:00":                 time.Date(2026, 11, 18, 15, 0, 0, 0, tashkent),
		"UTC 17-noyabr 20:00":            utc(17, 20, 0, 0, 0),
		"Toshkent kun boshi (UTC 19:00)": utc(17, 19, 0, 0, 0),
	} {
		days, err := r.DailyStats(ctx, from, len(want))
		if err != nil {
			t.Fatal(err)
		}
		if len(days) != len(want) {
			t.Fatalf("%s: %d kun, kutilgan %d", name, len(days), len(want))
		}
		for i, w := range want {
			g := days[i]
			if _, off := g.Day.Zone(); !g.Day.Equal(w.Day) || off != 5*60*60 ||
				g.NewUsers != w.NewUsers || g.ActiveUsers != w.ActiveUsers {
				t.Errorf("%s, %d-kun: %+v, kutilgan %+v", name, i, g, w)
			}
		}
	}

	if days, err := r.DailyStats(ctx, utc(18, 0, 0, 0, 0), 0); err != nil || days != nil {
		t.Errorf("days=0: %v %v, kutilgan nil", days, err)
	}
}

func ptr[T any](v T) *T { return &v }
