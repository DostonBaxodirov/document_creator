package bot

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

func TestOrderKind(t *testing.T) {
	cases := []struct {
		s    storage.Session
		want string
	}{
		{storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeOcean}, storage.KindPhoto},
		{storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemePanorama}, storage.KindPhoto},
		{storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeModern}, storage.KindPlain},
		{storage.Session{WorkType: string(ai.WorkIndependent)}, storage.KindPlain},
		{storage.Session{WorkType: string(ai.WorkPractical), Design: generator.ThemeOcean}, storage.KindPlain},
	}
	for _, tc := range cases {
		if got := orderKind(&tc.s); got != tc.want {
			t.Errorf("%s/%s: toifa %s, kutilgan %s", tc.s.WorkType, tc.s.Design, got, tc.want)
		}
	}
}

func TestFormatting(t *testing.T) {
	sums := map[int]string{0: "0 so'm", 7000: "7 000 so'm", 10000: "10 000 so'm", 1234567: "1 234 567 so'm", 999: "999 so'm"}
	for n, want := range sums {
		if got := formatSum(n); got != want {
			t.Errorf("formatSum(%d) = %q, kutilgan %q", n, got, want)
		}
	}
	if got := formatCard("8600-1234 5678 9012"); got != "8600 1234 5678 9012" {
		t.Errorf("formatCard = %q", got)
	}
	if got := lastDigits("8600 **** **** 9012", 4); got != "9012" {
		t.Errorf("lastDigits = %q", got)
	}
	if got := truncateRunes("abcdef", 4); got != "abc…" {
		t.Errorf("truncateRunes = %q", got)
	}
}

// fakeBilling — quote() uchun kerakli metodlargina amalga oshirilgan ombor.
type fakeBilling struct {
	BillingStore
	credit *storage.Order
	used   map[string]int
}

func (f *fakeBilling) FindCredit(_ context.Context, _ uint, _ string, _ int) (*storage.Order, error) {
	return f.credit, nil
}

func (f *fakeBilling) CountFreeUsed(_ context.Context, _ uint, kind string) (int, error) {
	return f.used[kind], nil
}

// fakeImages — rasm provayderi ulangan deb hisoblanadigan servis.
type fakeImages struct{ ImageService }

func (fakeImages) Available() bool { return true }

func TestKindWithoutImageProvider(t *testing.T) {
	photo := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeOcean}
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{})
	if got := h.kindOf(photo); got != storage.KindPlain {
		t.Errorf("rasm provayderi yo'q bo'lsa rasmli dizayn rasmsiz narxda bo'lishi kerak, olindi %s", got)
	}
	h = NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{Images: fakeImages{}})
	if got := h.kindOf(photo); got != storage.KindPhoto {
		t.Errorf("rasm provayderi bor: toifa %s", got)
	}
}

func TestQuote(t *testing.T) {
	fb := &fakeBilling{used: map[string]int{}}
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{
		Images:  fakeImages{},
		Billing: fb,
		Payment: PaymentOptions{Enabled: true, PriceSlidePhoto: 300, PriceSlidePlain: 250, PriceDocument: 7000, PricePage: 200, FreePhoto: 1, FreePlain: 1},
	})
	u := &storage.User{ID: 1, TelegramUserID: 42}
	photo := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeOcean, Volume: 10}
	plain := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeBento, Volume: 12}
	doc := &storage.Session{WorkType: string(ai.WorkIndependent), Volume: 15}

	q, _ := h.quote(context.Background(), photo, u)
	if q.mode != quoteFree || q.kind != storage.KindPhoto || q.price != 3000 {
		t.Errorf("birinchi rasmli ish bepul bo'lishi kerak: %+v", q)
	}

	fb.used[storage.KindPhoto] = 1
	q, _ = h.quote(context.Background(), photo, u)
	if q.mode != quotePaid || q.price != 3000 || !strings.Contains(q.line(), "3 000 so'm") ||
		!strings.Contains(q.line(), "10 slayd × 300 so'm") {
		t.Errorf("10 ta rasmli slayd 3 000 so'm bo'lishi kerak: %+v %q", q, q.line())
	}
	photo.Volume = 50
	q, _ = h.quote(context.Background(), photo, u)
	if q.price != 15000 {
		t.Errorf("50 ta rasmli slayd 15 000 so'm bo'lishi kerak: %+v", q)
	}

	q, _ = h.quote(context.Background(), doc, u)
	if q.mode != quoteFree || q.kind != storage.KindPlain {
		t.Errorf("rasmsiz bepul urinish hali ishlatilmagan: %+v", q)
	}

	fb.used[storage.KindPlain] = 1
	q, _ = h.quote(context.Background(), plain, u)
	if q.mode != quotePaid || q.kind != storage.KindPlain || q.price != 3000 || !strings.Contains(q.line(), "12 slayd × 250 so'm") {
		t.Errorf("12 ta rasmsiz slayd 3 000 so'm bo'lishi kerak: %+v %q", q, q.line())
	}
	q, _ = h.quote(context.Background(), doc, u)
	if q.mode != quotePaid || q.price != 3000 || !strings.Contains(q.line(), "15 sahifa × 200 so'm") {
		t.Errorf("15 sahifali mustaqil ish 3 000 so'm (sahifasiga 200) bo'lishi kerak: %+v %q", q, q.line())
	}
	referat := &storage.Session{WorkType: string(ai.WorkReferat), Design: generator.DocThemeModern, Volume: 12}
	if q, _ = h.quote(context.Background(), referat, u); q.price != 2400 || q.kind != storage.KindPlain {
		t.Errorf("12 sahifali referat 2 400 so'm bo'lishi kerak: %+v", q)
	}
	practical := &storage.Session{WorkType: string(ai.WorkPractical), Volume: 15}
	if q, _ = h.quote(context.Background(), practical, u); q.price != 7000 || q.detail != "" {
		t.Errorf("amaliy topshiriq sahifalar sonidan qat'i nazar 7 000 so'm bo'lishi kerak: %+v", q)
	}

	fb.credit = &storage.Order{ID: 5, Status: storage.OrderPaid, Price: 10000}
	q, _ = h.quote(context.Background(), doc, u)
	if q.mode != quoteCredit || q.credit.ID != 5 {
		t.Errorf("to'langan buyurtma qayta ishlatilishi kerak: %+v", q)
	}
}

func TestPaymentsOn(t *testing.T) {
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{Payment: PaymentOptions{Enabled: true}})
	if h.paymentsOn() {
		t.Error("ombor (Billing) bo'lmasa to'lov o'chiq bo'lishi kerak")
	}
	if h.pricingText() != "" {
		t.Error("to'lov o'chiq bo'lsa narxlar ko'rsatilmasligi kerak")
	}
}

func samplePayments() []storage.Payment {
	at := time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC)
	admin := int64(777)
	return []storage.Payment{
		{ID: 3, UserID: 2, TelegramUserID: 222, Username: "ali", OrderID: 9, Kind: storage.KindPhoto, Amount: 10000,
			DetectedAmount: 10000, Status: storage.PaymentApproved, CreatedAt: at, CheckMessageID: 55, CheckFileID: "F3"},
		{ID: 2, UserID: 3, TelegramUserID: 333, OrderID: 8, Kind: storage.KindPlain, Amount: 7000,
			Status: storage.PaymentApproved, CreatedAt: at.Add(-time.Hour), ReviewedBy: &admin, ReviewedAt: &at},
		{ID: 1, UserID: 3, TelegramUserID: 333, OrderID: 7, Kind: storage.KindPlain, Amount: 7000, DetectedAmount: 5000,
			Status: storage.PaymentReview, Reason: "Summa kam\nKarta mos emas", CreatedAt: at.Add(-2 * time.Hour)},
		{ID: 4, UserID: 4, TelegramUserID: 444, OrderID: 10, Kind: storage.KindPlain, Amount: 7000,
			Status: storage.PaymentRejected, CreatedAt: at},
	}
}

func TestTextReport(t *testing.T) {
	p, _ := findPeriod("week")
	got := textReport(samplePayments(), p, time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"oxirgi 7 kun",
		"Tasdiqlangan: <b>2</b> ta · <b>17 000 so'm</b>",
		"Rasmli: 1 ta · 10 000 so'm",
		"Rasmsiz: 1 ta · 7 000 so'm",
		"Tekshiruvda: 1 ta · 7 000 so'm",
		"Rad etilgan: 1 ta",
		"✅ #3 · 10 000 so'm · @ali · 222 · 16.11.2026 14:30 · /chek_3",
		"⏳ #1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hisobotda %q yo'q:\n%s", want, got)
		}
	}

	if got := textReport(nil, p, time.Now()); !strings.Contains(got, "to'lovlar yo'q") {
		t.Errorf("bo'sh hisobot:\n%s", got)
	}
}

func TestTextReportTruncated(t *testing.T) {
	var list []storage.Payment
	for i := 1; i <= 200; i++ {
		list = append(list, storage.Payment{ID: uint(i), TelegramUserID: 1234567890, Username: "foydalanuvchi_nomi",
			Amount: 10000, Status: storage.PaymentApproved, Kind: storage.KindPhoto})
	}
	p, _ := findPeriod("all")
	got := textReport(list, p, time.Now())
	if n := utf8.RuneCountInString(got); n > 4096 {
		t.Errorf("hisobot Telegram chegarasidan uzun: %d belgi", n)
	}
	if !strings.Contains(got, "CSV ni tanlang") {
		t.Error("qirqilgan hisobotda CSV haqida eslatma bo'lishi kerak")
	}
}

func TestCSVReport(t *testing.T) {
	data, err := csvReport(samplePayments())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "\xEF\xBB\xBF") {
		t.Error("CSV UTF-8 BOM bilan boshlanishi kerak")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 || len(rows[0]) != len(csvHeader) {
		t.Fatalf("qatorlar: %d, ustunlar: %d", len(rows), len(rows[0]))
	}
	col := func(name string) int {
		for i, h := range csvHeader {
			if h == name {
				return i
			}
		}
		t.Fatalf("ustun yo'q: %s", name)
		return -1
	}
	r := rows[1]
	if r[col("payment_id")] != "3" || r[col("user_id")] != "2" || r[col("telegram_user_id")] != "222" ||
		r[col("amount")] != "10000" || r[col("created_at")] != "2026-11-16 14:30:00" ||
		r[col("check_message_id")] != "55" || r[col("check_file_id")] != "F3" {
		t.Errorf("birinchi qator noto'g'ri: %v", r)
	}
	if rows[2][col("reviewed_by")] != "777" {
		t.Errorf("reviewed_by noto'g'ri: %v", rows[2])
	}
	if rows[3][col("reason")] != "Summa kam; Karta mos emas" {
		t.Errorf("reason noto'g'ri: %q", rows[3][col("reason")])
	}
}

func TestReportPeriodSince(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	want := map[string]time.Duration{"day": 24 * time.Hour, "week": 7 * 24 * time.Hour, "month": 30 * 24 * time.Hour}
	for id, span := range want {
		p, ok := findPeriod(id)
		if !ok {
			t.Fatalf("davr topilmadi: %s", id)
		}
		if s := p.since(now); s == nil || !s.Equal(now.Add(-span)) {
			t.Errorf("%s: since = %v", id, s)
		}
	}
	if p, _ := findPeriod("all"); p.since(now) != nil {
		t.Error("barcha vaqt uchun since nil bo'lishi kerak")
	}
	kb := reportPeriodKeyboard()
	n := 0
	for _, row := range kb.InlineKeyboard {
		n += len(row)
	}
	if n != len(reportPeriods) {
		t.Errorf("davr tugmalari soni %d", n)
	}
}

func TestReviewCaption(t *testing.T) {
	p := samplePayments()[2]
	p.Reason = strings.Repeat("Juda uzun sabab matni ", 100)
	o := &storage.Order{ID: 7, WorkType: string(ai.WorkPresentation), Kind: storage.KindPhoto, Topic: strings.Repeat("Mavzu ", 50)}
	got := reviewCaption(&p, o)
	if n := utf8.RuneCountInString(got); n > 1024 {
		t.Errorf("izoh Telegram chegarasidan uzun: %d belgi", n)
	}
	for _, want := range []string{"Chek #1", "tekshiruvda", "ID 333", "userId 3", "Buyurtma #7", "rasmli dizayn", "7 000 so'm", "5 000 so'm", "Tasdiqlang yoki rad eting"} {
		if !strings.Contains(got, want) {
			t.Errorf("izohda %q yo'q:\n%s", want, got)
		}
	}
}

// Har qanday holatdagi chek izohi eng uzun qiymatlarda ham Telegram
// chegarasiga (1024) sig'ishi va kim qaror qilgani ko'rinishi kerak.
func TestReviewCaptionAllStatuses(t *testing.T) {
	at := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	admin := int64(777)
	o := &storage.Order{ID: 7, WorkType: string(ai.WorkPresentation), Kind: storage.KindPhoto, Volume: 50,
		Topic: strings.Repeat("Mavzu & <b> ", 60)}
	base := storage.Payment{ID: 5, UserID: 3, TelegramUserID: 333, Username: strings.Repeat("u", 64), OrderID: 7,
		Amount: 15000, DetectedAmount: 15000, DetectedLast4: "1234", DetectedPaidAt: &at, CreatedAt: at,
		TransactionID: strings.Repeat("9", 128), Reason: strings.Repeat("Juda uzun sabab & matni\n", 60)}
	cases := []struct {
		name   string
		status string
		admin  *int64
		want   string
	}{
		{"AI tasdiqlagan", storage.PaymentApproved, nil, "Avtomatik tasdiqlandi"},
		{"AI rad etgan", storage.PaymentRejected, nil, "Avtomatik rad etildi"},
		{"shubhali", storage.PaymentReview, nil, "Tasdiqlang yoki rad eting"},
		{"admin tasdiqlagan", storage.PaymentApproved, &admin, "admin 777"},
		{"admin rad etgan", storage.PaymentRejected, &admin, "admin 777"},
	}
	for _, tc := range cases {
		p := base
		p.Status, p.ReviewedBy = tc.status, tc.admin
		if tc.admin != nil {
			p.ReviewedAt = &at
		}
		got := reviewCaption(&p, o)
		if n := visibleLen(got); n > 1024 {
			t.Errorf("%s: izoh %d birlik (chegara 1024)", tc.name, n)
		}
		if !strings.Contains(got, "Sabablar") || !strings.Contains(got, "• Juda uzun sabab") {
			t.Errorf("%s: sabablar ko'rinmayapti:\n%s", tc.name, got)
		}
		for _, want := range []string{tc.want, "50 slayd", "Tranzaksiya", "****1234", "&amp;"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: izohda %q yo'q:\n%s", tc.name, want, got)
			}
		}
		if strings.Contains(got, "<b> ") {
			t.Errorf("%s: mavzudagi HTML ekranlanmagan", tc.name)
		}
	}
}
