package payment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func goodReceipt(paidAt string) *Receipt {
	return &Receipt{
		IsReceipt: true, Success: true, Amount: 10000, Currency: "UZS",
		CardLast4: "9012", PaidAt: paidAt, TransactionID: "TX-1", Confidence: 0.95,
	}
}

func TestDecide(t *testing.T) {
	created := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC) // 14:00 Toshkent
	now := created.Add(15 * time.Minute)
	base := func(r *Receipt) Input {
		return Input{Receipt: r, ExpectedAmount: 10000, CardLast4: "8600 1234 5678 9012", OrderCreatedAt: created, Now: now}
	}

	cases := []struct {
		name   string
		in     Input
		want   Verdict
		reason string // sabablardan birida bo'lishi kerak
	}{
		{"to'g'ri chek", base(goodReceipt("2026-11-16 14:10")), Approved, ""},
		{"ortiqcha summa", func() Input { r := goodReceipt("2026-11-16 14:10"); r.Amount = 12000; return base(r) }(), Approved, ""},
		{"faqat sana (bugun)", base(goodReceipt("2026-11-16")), Approved, ""},
		{"summa kam", func() Input { r := goodReceipt("2026-11-16 14:10"); r.Amount = 7000; return base(r) }(), Suspicious, "Summa kam"},
		{"summa o'qilmagan", func() Input { r := goodReceipt("2026-11-16 14:10"); r.Amount = 0; return base(r) }(), Suspicious, "Summa o'qilmadi"},
		{"boshqa karta", func() Input { r := goodReceipt("2026-11-16 14:10"); r.CardLast4 = "1111"; return base(r) }(), Suspicious, "Karta mos emas"},
		{"karta ko'rinmagan", func() Input { r := goodReceipt("2026-11-16 14:10"); r.CardLast4 = ""; return base(r) }(), Suspicious, "karta raqami ko'rinmadi"},
		{"holat muvaffaqiyatsiz", func() Input { r := goodReceipt("2026-11-16 14:10"); r.Success = false; return base(r) }(), Suspicious, "muvaffaqiyatli emas"},
		{"ishonch past", func() Input { r := goodReceipt("2026-11-16 14:10"); r.Confidence = 0.5; return base(r) }(), Suspicious, "ishonchi past"},
		{"eski chek", base(goodReceipt("2026-11-15 10:00")), Suspicious, "oldingi vaqtga"},
		{"kelajakdagi chek", base(goodReceipt("2026-11-16 18:00")), Suspicious, "kelajakda"},
		{"sana yo'q", base(goodReceipt("")), Suspicious, "sanasi o'qilmadi"},
		{"boshqa valyuta", func() Input { r := goodReceipt("2026-11-16 14:10"); r.Currency = "USD"; return base(r) }(), Suspicious, "Valyuta"},
		{"takroriy chek", func() Input { in := base(goodReceipt("2026-11-16 14:10")); in.Seen = true; return in }(), Suspicious, "oldin yuborilgan"},
		{"chek emas", func() Input { return base(&Receipt{IsReceipt: false, Confidence: 0.9}) }(), Invalid, "cheki emas"},
		{"chek emas, ishonch past", func() Input { return base(&Receipt{IsReceipt: false, Confidence: 0.3}) }(), Suspicious, "tanimadi"},
		{"AI xatosi", Input{Err: errors.New("timeout"), ExpectedAmount: 10000}, Suspicious, "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(tc.in)
			if d.Verdict != tc.want {
				t.Fatalf("qaror %s, kutilgan %s (sabablar: %v)", d.Verdict, tc.want, d.Reasons)
			}
			if tc.reason != "" && !strings.Contains(strings.Join(d.Reasons, "\n"), tc.reason) {
				t.Errorf("sabablarda %q yo'q: %v", tc.reason, d.Reasons)
			}
			if tc.want == Approved && len(d.Reasons) != 0 {
				t.Errorf("tasdiqlangan chekda sabab bo'lmasligi kerak: %v", d.Reasons)
			}
		})
	}
}

func TestDecideSetsPaidAt(t *testing.T) {
	created := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	d := Decide(Input{Receipt: goodReceipt("2026-11-16 14:10"), ExpectedAmount: 10000, CardLast4: "9012",
		OrderCreatedAt: created, Now: created.Add(time.Hour)})
	want := time.Date(2026, 11, 16, 9, 10, 0, 0, time.UTC)
	if d.PaidAt == nil || !d.PaidAt.Equal(want) {
		t.Fatalf("PaidAt = %v, kutilgan %v (Toshkent vaqti UTC+5)", d.PaidAt, want)
	}
}

func TestParseAmountString(t *testing.T) {
	cases := map[string]float64{
		"10 000":         10000,
		"10,000":         10000,
		"10 000,00 so'm": 10000,
		"10.000,50":      10000.5,
		"10,000.00 UZS":  10000,
		"0.95":           0.95,
		"":               0,
	}
	for in, want := range cases {
		if got := parseAmountString(in); got != want {
			t.Errorf("parseAmountString(%q) = %v, kutilgan %v", in, got, want)
		}
	}
}

func TestParseReceiptFlexibleTypes(t *testing.T) {
	r, err := parseReceipt("```json\n{\"is_receipt\":true,\"success\":true,\"amount\":\"10 000\",\"card_last4\":9012," +
		"\"paid_at\":\"2026-11-16 14:10\",\"transaction_id\":123456,\"confidence\":\"0.9\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if r.Amount.Int() != 10000 || r.CardLast4 != "9012" || r.TransactionID != "123456" || float64(r.Confidence) != 0.9 {
		t.Errorf("noto'g'ri o'qildi: %+v", r)
	}
	if !strings.HasPrefix(r.Raw, "{") {
		t.Errorf("Raw JSON saqlanmadi: %q", r.Raw)
	}
}

func TestVerifierRequestFormat(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("noto'g'ri so'rov: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"is_receipt\":true,\"success\":true,\"amount\":10000,` +
			`\"card_last4\":\"9012\",\"paid_at\":\"2026-11-16 14:10\",\"confidence\":0.93}"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	v := NewVerifier(srv.URL+"/v1", "key", "gpt-test", 5*time.Second)
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...)
	r, err := v.Read(context.Background(), jpeg, Expectation{Amount: 10000, CardLast4: "9012"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Amount.Int() != 10000 || r.CardLast4 != "9012" {
		t.Errorf("javob noto'g'ri o'qildi: %+v", r)
	}

	if got["model"] != "gpt-test" {
		t.Errorf("model = %v", got["model"])
	}
	if rf, _ := got["response_format"].(map[string]any); rf["type"] != "json_object" {
		t.Errorf("response_format = %v", got["response_format"])
	}
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("2 ta xabar kutilgan, olindi %d", len(msgs))
	}
	parts, _ := msgs[1].(map[string]any)["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("user xabarida matn va rasm bo'lishi kerak: %v", parts)
	}
	text, _ := parts[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "10000") || !strings.Contains(text, "9012") {
		t.Errorf("promptda kutilgan summa va karta yo'q: %q", text)
	}
	img, _ := parts[1].(map[string]any)["image_url"].(map[string]any)
	if url, _ := img["url"].(string); !strings.HasPrefix(url, "data:image/jpeg;base64,") {
		t.Errorf("rasm data URL noto'g'ri: %.40s", url)
	}
}

func TestVerifierPermanentErrorNoRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	_, err := NewVerifier(srv.URL, "x", "m", time.Second).Read(context.Background(), []byte{1, 2, 3}, Expectation{})
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("401 xatosi kutilgan edi, olindi %v", err)
	}
	if calls != 1 {
		t.Errorf("doimiy xatoda qayta urinilmasligi kerak, so'rovlar soni: %d", calls)
	}
}
