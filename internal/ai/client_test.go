package ai

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// scriptStep — soxta provayderning bitta javobi (matn yoki xato).
type scriptStep struct {
	raw string
	err error
}

// scriptedProvider — javoblarni oldindan berilgan tartibda qaytaradigan soxta provayder.
type scriptedProvider struct {
	mu    sync.Mutex
	steps []scriptStep
	calls int
}

func (*scriptedProvider) name() string { return "scripted" }

func (s *scriptedProvider) complete(context.Context, task) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	s.calls++
	if i >= len(s.steps) {
		return "", errors.New("kutilmagan so'rov")
	}
	return s.steps[i].raw, s.steps[i].err
}

// retryClient sleep chaqiruvlarini yozib boradigan (haqiqatan kutmaydigan) test klienti.
func retryClient(p provider, maxRetries int) (*client, *[]int) {
	sleeps := &[]int{}
	return &client{
		p: p, timeout: time.Minute, maxRetries: maxRetries, parallel: 1, log: zap.NewNop(),
		sleep: func(_ context.Context, retry int) error {
			*sleeps = append(*sleeps, retry)
			return nil
		},
	}, sleeps
}

// parseScore javob matnini son sifatida o'qiydi: shu son — ham natija, ham ball.
func parseScore(raw string) (int, int, error) {
	n, err := strconv.Atoi(raw)
	return n, n, err
}

const testWant = 10

// TestGenerateJSONIncompleteThenComplete — to'liq bo'lmagan natijadan keyin
// darhol (kechikishsiz) bitta qayta urinish qilinadi.
func TestGenerateJSONIncompleteThenComplete(t *testing.T) {
	p := &scriptedProvider{steps: []scriptStep{{raw: "4"}, {raw: "12"}}}
	c, sleeps := retryClient(p, 3)
	got, err := generateJSON(context.Background(), c, "test", task{}, testWant, parseScore)
	if err != nil {
		t.Fatal(err)
	}
	if got != 12 {
		t.Errorf("natija %d, kutilgan 12", got)
	}
	if p.calls != 2 {
		t.Errorf("so'rovlar soni %d, kutilgan 2", p.calls)
	}
	if len(*sleeps) != 0 {
		t.Errorf("to'liq bo'lmagan natijadan keyin kutilmasligi kerak, sleep: %v", *sleeps)
	}
}

// TestGenerateJSONIncompleteTwice — ikki marta to'liq bo'lmagan natija:
// AI_MAX_RETRIES dan qat'i nazar faqat bitta qayta urinish, eng yaxshisi qaytariladi.
func TestGenerateJSONIncompleteTwice(t *testing.T) {
	for _, maxRetries := range []int{0, 3} {
		t.Run("maxRetries="+strconv.Itoa(maxRetries), func(t *testing.T) {
			p := &scriptedProvider{steps: []scriptStep{{raw: "7"}, {raw: "3"}, {raw: "12"}}}
			c, sleeps := retryClient(p, maxRetries)
			got, err := generateJSON(context.Background(), c, "test", task{}, testWant, parseScore)
			if err != nil {
				t.Fatal(err)
			}
			if got != 7 {
				t.Errorf("eng yuqori balli natija (7) qaytarilishi kerak, olindi: %d", got)
			}
			if p.calls != 2 {
				t.Errorf("so'rovlar soni %d, kutilgan 2", p.calls)
			}
			if len(*sleeps) != 0 {
				t.Errorf("to'liq bo'lmagan natijadan keyin kutilmasligi kerak, sleep: %v", *sleeps)
			}
		})
	}
}

// TestGenerateJSONTransientErrors — vaqtinchalik xatolarda har bir qayta
// urinishdan oldin kechikish bilan maxRetries martagacha uriniladi.
func TestGenerateJSONTransientErrors(t *testing.T) {
	transient := errors.New("503 vaqtincha mavjud emas")
	p := &scriptedProvider{steps: []scriptStep{{err: transient}, {err: transient}, {raw: "12"}}}
	c, sleeps := retryClient(p, 3)
	got, err := generateJSON(context.Background(), c, "test", task{}, testWant, parseScore)
	if err != nil {
		t.Fatal(err)
	}
	if got != 12 {
		t.Errorf("natija %d, kutilgan 12", got)
	}
	if p.calls != 3 {
		t.Errorf("so'rovlar soni %d, kutilgan 3", p.calls)
	}
	if len(*sleeps) != 2 || (*sleeps)[0] != 1 || (*sleeps)[1] != 2 {
		t.Errorf("sleep ikki marta (1, 2) chaqirilishi kerak, olindi: %v", *sleeps)
	}
}

// TestGenerateJSONPermanentError — doimiy xatoda qayta urinilmaydi.
func TestGenerateJSONPermanentError(t *testing.T) {
	cause := errors.New("noto'g'ri API kalit")
	p := &scriptedProvider{steps: []scriptStep{{err: permanent(cause)}, {raw: "12"}}}
	c, sleeps := retryClient(p, 3)
	_, err := generateJSON(context.Background(), c, "test", task{}, testWant, parseScore)
	if !errors.Is(err, cause) {
		t.Fatalf("doimiy xato qaytarilishi kerak, olindi: %v", err)
	}
	if p.calls != 1 {
		t.Errorf("so'rovlar soni %d, kutilgan 1", p.calls)
	}
	if len(*sleeps) != 0 {
		t.Errorf("doimiy xatoda kutilmasligi kerak, sleep: %v", *sleeps)
	}
}
