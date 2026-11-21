package bot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generation"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

func sampleSession() *storage.Session {
	id := uint(12)
	return &storage.Session{
		UserID: 682539979, Username: "talaba_01", WorkType: string(ai.WorkPresentation),
		Topic: "Sun'iy intellekt & ta'lim", Volume: 20, Design: generator.ThemeOcean, Language: "uz",
		Requirements: "Har bir slaydda misol bo'lsin", StudentName: "Ali Valiyev",
		University: "TATU", TeacherName: "B. Karimov", OrderID: &id,
	}
}

func TestPurchaseText(t *testing.T) {
	at := time.Date(2026, 11, 18, 8, 26, 0, 0, time.UTC)
	admin := int64(777)
	s := sampleSession()
	paid := &storage.Order{ID: 12, UserID: 3, Kind: storage.KindPhoto, Price: 6000, IsPaid: true}
	free := &storage.Order{ID: 12, UserID: 3, Kind: storage.KindPhoto, IsFree: true, IsPaid: true}
	out := &output{Output: generation.Output{FileName: "Sun_iy_intellekt.pptx", Summary: "20 ta slayd · 🌊 Okean · 🖼 19/19 rasm (openai/gpt-image-2:low)"}}

	cases := []struct {
		name string
		pc   purchase
		want []string
	}{
		{"AI tasdiqlagan chek", purchase{session: s, order: paid,
			payment: &storage.Payment{ID: 5, Amount: 6000, DetectedAmount: 6000, Status: storage.PaymentApproved}},
			[]string{"buyurtma #12", "<b>6 000 so'm</b> · chek #5 · avtomatik tasdiqlangan · /chek_5"}},
		{"admin tasdiqlagan, summa farqli", purchase{session: s, order: paid,
			payment: &storage.Payment{ID: 6, Amount: 6000, DetectedAmount: 7000, ReviewedBy: &admin}},
			[]string{"chek #6 · admin 777 tasdiqlagan · chekda 7 000 so'm"}},
		{"bepul (eski buyurtma)", purchase{session: s, order: free},
			[]string{"🎁 bepul sinov"}},
		{"chek topilmagan", purchase{session: s, order: paid}, []string{"<b>6 000 so'm</b> · to'langan"}},
		{"buyurtma o'qilmagan", purchase{session: s}, []string{"buyurtma #12 ma'lumotini o'qib bo'lmadi"}},
	}
	for _, tc := range cases {
		got := purchaseText(tc.pc, out, 117*time.Second, at)
		want := append([]string{
			"✅ yetkazildi", "@talaba_01", "ID <code>682539979</code>", "tg://user?id=682539979",
			"Sun&#39;iy intellekt &amp; ta&#39;lim", "Okean 📷", "<b>Slaydlar soni:</b> 20",
			"Ali Valiyev", "TATU", "B. Karimov", "Har bir slaydda misol bo&#39;lsin",
			"Sun_iy_intellekt.pptx", "19/19 rasm", "1 daq 57 s", "18.11.2026 13:26",
		}, tc.want...)
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: xabarda %q yo'q:\n%s", tc.name, w, got)
			}
		}
	}

	// To'lov o'chirilgan: buyurtmasiz ish.
	s.OrderID = nil
	if got := purchaseText(purchase{session: s}, nil, 5*time.Second, at); !strings.Contains(got, "to'lov tizimi o'chirilgan") ||
		!strings.Contains(got, "Yangi ish") || !strings.Contains(got, "5 s") {
		t.Errorf("buyurtmasiz ish xabari:\n%s", got)
	}
}

// Eng uzun maydonlar bilan ham xabar Telegram chegarasiga (4096) sig'adi.
func TestPurchaseTextFitsTelegramLimit(t *testing.T) {
	s := sampleSession()
	s.Username = strings.Repeat("u", 64)
	s.Topic = strings.Repeat("Я", maxTopicRunes)
	s.Requirements = strings.Repeat("Ж", maxRequirementsRunes)
	s.StudentName, s.University, s.TeacherName = strings.Repeat("A", 128), strings.Repeat("B", 256), strings.Repeat("C", 128)
	pc := purchase{session: s, order: &storage.Order{ID: 12, Kind: storage.KindPhoto, Price: 15000},
		payment: &storage.Payment{ID: 99999, Amount: 15000, DetectedAmount: 150000}}
	out := &output{Output: generation.Output{FileName: strings.Repeat("f", 120) + ".pptx", Summary: "50 ta slayd · 🌊 Okean · 🖼 49/49 rasm (openai/gpt-image-2:medium)"}}
	got := purchaseText(pc, out, time.Hour, time.Now())
	if n := visibleLen(got); n > 4096 {
		t.Errorf("xabar %d birlik (chegara 4096)", n)
	}
	if !strings.Contains(got, s.Requirements) {
		t.Error("talablar to'liq ko'rsatilishi kerak")
	}
}

func TestFailureText(t *testing.T) {
	s := sampleSession()
	pc := purchase{session: s, order: &storage.Order{ID: 12, Kind: storage.KindPhoto, Price: 6000},
		payment: &storage.Payment{ID: 5, Amount: 6000}}
	got := failureText(pc, errors.New(`ai: <timeout> & "retry"`))
	for _, w := range []string{
		"Buyurtma #12 tayyorlanmadi", "@talaba_01", "rasmli dizayn · 20 slayd", "Sun&#39;iy intellekt &amp; ta&#39;lim",
		"chek #5", "&lt;timeout&gt; &amp; &#34;retry&#34;", "Qayta urinish",
	} {
		if !strings.Contains(got, w) {
			t.Errorf("xabarda %q yo'q:\n%s", w, got)
		}
	}
	long := failureText(pc, errors.New(strings.Repeat("x", 5000)))
	if n := visibleLen(long); n > 4096 {
		t.Errorf("uzun xato bilan xabar %d belgi", n)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0: "0 s", 45 * time.Second: "45 s", 60 * time.Second: "1 daq 0 s",
		117*time.Second + 400*time.Millisecond: "1 daq 57 s", 15 * time.Minute: "15 daq 0 s",
	}
	for d, want := range cases {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, kutilgan %q", d, got, want)
		}
	}
}

// Ombor ulanmagan bo'lsa, adminlarga xabar yuborilmaydi va panic bo'lmaydi.
func TestNotifyWithoutBilling(t *testing.T) {
	h := &Handler{}
	if n := h.toAdmins("salom"); n != 0 {
		t.Errorf("toAdmins = %d, kutilgan 0", n)
	}
	if got := h.purchaseNotice(context.Background(), sampleSession(), nil, time.Second, nil); got != "" {
		t.Errorf("purchaseNotice = %q", got)
	}
	if got := h.failureNotice(context.Background(), sampleSession(), errors.New("x"), nil); got != "" {
		t.Errorf("failureNotice = %q", got)
	}
}

func TestVisibleLen(t *testing.T) {
	cases := map[string]int{
		"":                         0,
		"abc":                      3,
		"<b>abc</b>":               3,
		"a &amp; b":                5,
		"&lt;b&gt;":                3,
		"🧾 <code>x</code>":         4, // emoji — 2 ta UTF-16 birlik
		"<a href=\"tg://x\">p</a>": 1,
	}
	for in, want := range cases {
		if got := visibleLen(in); got != want {
			t.Errorf("visibleLen(%q) = %d, kutilgan %d", in, got, want)
		}
	}
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"abcdef", 10, "abcdef"}, {"abcdef", 6, "abcdef"}, {"abcdef", 4, "abc…"},
		{"ab🧾cd", 4, "ab…"}, {"abc", 1, "…"},
	} {
		if got := truncateUnits(tc.in, tc.n); got != tc.want || utf16Len(got) > tc.n {
			t.Errorf("truncateUnits(%q, %d) = %q, kutilgan %q", tc.in, tc.n, got, tc.want)
		}
	}
}
