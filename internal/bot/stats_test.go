package bot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/payment"
	"telegram-presentation-bot/internal/storage"
)

// fakeStats — xotiradagi statistika ombori.
type fakeStats struct {
	visits []int64
	err    error
	sinces []*time.Time
	from   time.Time
	days   int
}

func (f *fakeStats) RecordVisit(_ context.Context, userID int64, _ time.Time) error {
	if f.err != nil {
		return f.err
	}
	f.visits = append(f.visits, userID)
	return nil
}

func (f *fakeStats) TotalUsers(context.Context) (int64, error) { return 1234, nil }

func (f *fakeStats) PeriodStats(_ context.Context, since *time.Time) (storage.PeriodStats, error) {
	f.sinces = append(f.sinces, since)
	if since == nil {
		return storage.PeriodStats{NewUsers: 1234, ActiveUsers: 1234, Customers: 310, Delivered: 845}, nil
	}
	n := int64(len(f.sinces))
	return storage.PeriodStats{NewUsers: 10 * n, ActiveUsers: 40 * n, Customers: 3 * n, Delivered: 5 * n}, nil
}

func (f *fakeStats) DailyStats(_ context.Context, from time.Time, days int) ([]storage.DayStats, error) {
	f.from, f.days = from, days
	out := make([]storage.DayStats, days)
	for i := range out {
		out[i] = storage.DayStats{Day: from.AddDate(0, 0, i), NewUsers: int64(i), ActiveUsers: int64(10 + i)}
	}
	return out, nil
}

func TestVisitTracker(t *testing.T) {
	var vt visitTracker
	if !vt.mark(1, "2026-11-18") || vt.mark(1, "2026-11-18") {
		t.Fatal("bir kunda foydalanuvchi bir marta belgilanishi kerak")
	}
	if !vt.mark(2, "2026-11-18") {
		t.Error("boshqa foydalanuvchi alohida belgilanadi")
	}
	vt.forget(1, "2026-11-18")
	if !vt.mark(1, "2026-11-18") {
		t.Error("forget dan keyin qayta belgilanishi kerak")
	}
	if !vt.mark(1, "2026-11-19") || len(vt.seen) != 1 {
		t.Errorf("yangi kunda ro'yxat tozalanishi kerak: %v", vt.seen)
	}
}

func TestRecordVisitOncePerDay(t *testing.T) {
	fs := &fakeStats{}
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{Stats: fs})
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, payment.Tashkent)

	h.recordVisit(7, now)
	h.recordVisit(7, now.Add(time.Hour))
	h.recordVisit(8, now)
	if len(fs.visits) != 2 {
		t.Fatalf("kuniga bir marta yozilishi kerak: %v", fs.visits)
	}
	h.recordVisit(7, now.Add(24*time.Hour))
	if len(fs.visits) != 3 {
		t.Errorf("ertasi kuni qayta yozilishi kerak: %v", fs.visits)
	}

	// Baza xatosida belgi olib tashlanadi — keyingi update'da qayta uriniladi.
	fs.err = errors.New("db down")
	h.recordVisit(9, now)
	fs.err = nil
	h.recordVisit(9, now)
	if fs.visits[len(fs.visits)-1] != 9 {
		t.Errorf("xatodan keyin qayta yozilishi kerak: %v", fs.visits)
	}
}

func TestCollectStatsPeriods(t *testing.T) {
	fs := &fakeStats{}
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{Stats: fs})
	// Toshkentda 19-noyabr 01:30 (UTC da hali 18-noyabr).
	now := time.Date(2026, 11, 18, 20, 30, 0, 0, time.UTC)
	rep, err := h.collectStats(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.total != 1234 || len(rep.periods) != 3 || len(rep.days) != statsDays {
		t.Fatalf("hisobot to'liq emas: %+v", rep)
	}
	today := time.Date(2026, 11, 19, 0, 0, 0, 0, payment.Tashkent)
	want := []time.Time{today, today.AddDate(0, 0, -6), today.AddDate(0, 0, -29)}
	if fs.sinces[0] != nil {
		t.Error("barcha vaqt uchun since nil bo'lishi kerak")
	}
	for i, w := range want {
		if got := fs.sinces[i+1]; got == nil || !got.Equal(w) {
			t.Errorf("%s davri boshi %v, kutilgan %v", statsPeriods[i].title, got, w)
		}
	}
	if !fs.from.Equal(today.AddDate(0, 0, -6)) || fs.days != 7 {
		t.Errorf("kunlik jadval: from=%v days=%d", fs.from, fs.days)
	}
}

func TestStatsText(t *testing.T) {
	fs := &fakeStats{}
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{Stats: fs})
	rep, err := h.collectStats(context.Background(), time.Date(2026, 11, 18, 13, 26, 0, 0, payment.Tashkent))
	if err != nil {
		t.Fatal(err)
	}
	txt := statsText(rep)
	for _, want := range []string{
		"Tashrif buyuruvchilar statistikasi", "18.11.2026 13:26",
		"Jami foydalanuvchilar: <b>1 234</b>", "Jami tayyor ishlar: <b>845</b> ta (310 kishi)",
		"            Bugun  7 kun  30 kun",
		"Yangi          20     30      40",
		"Faol           80    120     160",
		"Ish olgan       6      9      12",
		"Tayyor ish     10     15      20",
		"18.11 (Ch)      6     16", "12.11 (Pa)      0     10",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("statistikada %q yo'q:\n%s", want, txt)
		}
	}
	if strings.Index(txt, "18.11 (Ch)") > strings.Index(txt, "12.11 (Pa)") {
		t.Error("kunlar yangisidan eskisiga tartiblanishi kerak")
	}
	// Jadval qatorlari telefonda sig'ishi uchun 32 belgidan oshmasligi kerak.
	for _, block := range strings.Split(txt, "<pre>")[1:] {
		for _, line := range strings.Split(strings.SplitN(block, "</pre>", 2)[0], "\n") {
			if n := len([]rune(line)); n > 32 {
				t.Errorf("jadval qatori uzun (%d): %q", n, line)
			}
		}
	}
	if !strings.Contains(msgAdminCommands, "/statistika") {
		t.Error("admin buyruqlari ro'yxatida /statistika yo'q")
	}
}
