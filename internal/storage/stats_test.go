package storage

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"
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

	// Foydalanuvchilar: 1 — 10 kun oldin, 2 — 3 kun oldin, 3 — bugun kirgan.
	for id, created := range map[int64]time.Time{1: at(10, 12), 2: at(3, 9), 3: at(0, 1)} {
		s := Session{UserID: id, ChatID: id, State: "idle", CreatedAt: created, UpdatedAt: created}
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}

	// Jadval bo'sh — mavjud ma'lumotlardan tiklanadi (har bir sessiya: 1 kun).
	n, err := r.BackfillVisits(ctx)
	if err != nil || n != 3 {
		t.Fatalf("backfill: n=%d err=%v", n, err)
	}
	if n, err := r.BackfillVisits(ctx); err != nil || n != 0 {
		t.Fatalf("jadval to'la bo'lsa backfill ishlamasligi kerak: n=%d err=%v", n, err)
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

	if total, err := r.TotalUsers(ctx); err != nil || total != 3 {
		t.Fatalf("jami: %d %v", total, err)
	}

	cases := []struct {
		name  string
		since *time.Time
		want  PeriodStats
	}{
		{"bugun", ptr(today), PeriodStats{NewUsers: 1, ActiveUsers: 3, Customers: 1, Delivered: 1}},
		{"7 kun", ptr(today.AddDate(0, 0, -6)), PeriodStats{NewUsers: 2, ActiveUsers: 3, Customers: 1, Delivered: 1}},
		{"barcha", nil, PeriodStats{NewUsers: 3, ActiveUsers: 3, Customers: 1, Delivered: 2}},
	}
	for _, c := range cases {
		got, err := r.PeriodStats(ctx, c.since)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: %+v, kutilgan %+v", c.name, got, c.want)
		}
	}

	days, err := r.DailyStats(ctx, today.AddDate(0, 0, -3), 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []DayStats{
		{Day: today.AddDate(0, 0, -3), NewUsers: 1, ActiveUsers: 1}, // 2-foydalanuvchi kirgan kuni (backfill)
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

// tashkent — bot ishlatadigan mintaqa (payment.Tashkent bilan bir xil: UTC+5).
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

func TestPeriodStatsBoundaries(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewStatsRepository(db, tashkent)

	since := time.Date(2026, 11, 18, 0, 0, 0, 0, tashkent)
	justBefore := since.Add(-time.Microsecond)

	// Yangi foydalanuvchilar: since dan oldin (1) — davrga kirmaydi, aynan since (2) — kiradi.
	mkSession(t, db, 1, justBefore)
	mkSession(t, db, 2, since)
	mkSession(t, db, 3, since.Add(5*time.Hour))

	// Faol foydalanuvchilar: 1 — ikkala kunda, 2 — davrda ikki kun (bir marta sanaladi),
	// 4 — faqat davrdan oldin.
	for _, v := range []struct {
		user int64
		at   time.Time
	}{
		{1, justBefore}, {1, since.Add(time.Hour)},
		{2, since}, {2, since.AddDate(0, 0, 1)},
		{4, justBefore},
	} {
		if err := r.RecordVisit(ctx, v.user, v.at); err != nil {
			t.Fatal(err)
		}
	}

	// Yetkazilgan ishlar: 10 — davrda ikki ish, 11 — davrdan oldin,
	// 12 — yetkazilmagan, 13 — davrda bitta ish.
	for _, o := range []struct {
		tg        int64
		delivered *time.Time
	}{
		{10, ptr(since)}, {10, ptr(since.Add(time.Hour))},
		{11, ptr(justBefore)},
		{12, nil},
		{13, ptr(since.AddDate(0, 0, 2))},
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

	inPeriod := PeriodStats{NewUsers: 2, ActiveUsers: 2, Customers: 2, Delivered: 3}
	for _, c := range []struct {
		name  string
		since *time.Time
		want  PeriodStats
	}{
		{"since Toshkent", ptr(since), inPeriod},
		{"since UTC", ptr(since.UTC()), inPeriod},
		{"barcha vaqt", nil, PeriodStats{NewUsers: 3, ActiveUsers: 3, Customers: 3, Delivered: 4}},
	} {
		got, err := r.PeriodStats(ctx, c.since)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: %+v, kutilgan %+v", c.name, got, c.want)
		}
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
