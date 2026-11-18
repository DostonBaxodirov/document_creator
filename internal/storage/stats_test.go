package storage

import (
	"context"
	"testing"
	"time"
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
}

func ptr[T any](v T) *T { return &v }
