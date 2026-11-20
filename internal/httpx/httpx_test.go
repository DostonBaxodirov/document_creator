package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestClientSingleton(t *testing.T) {
	c := Client()
	if c != Client() {
		t.Fatal("Client() har safar bitta nusxani qaytarishi kerak")
	}
	if c.Timeout != 0 {
		t.Errorf("klientda umumiy timeout bo'lmasligi kerak: %v", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport turi: %T", c.Transport)
	}
	if tr.ResponseHeaderTimeout != 0 || tr.MaxIdleConns != 100 || tr.MaxIdleConnsPerHost != 32 ||
		tr.TLSHandshakeTimeout != 10*time.Second || tr.IdleConnTimeout != 90*time.Second || !tr.ForceAttemptHTTP2 || tr.Proxy == nil {
		t.Errorf("Transport sozlamalari noto'g'ri: %+v", tr)
	}
}

func TestIsPermanentStatus(t *testing.T) {
	cases := map[int]bool{
		200: false, 301: false,
		400: true, 401: true, 403: true, 404: true, 422: true, 499: true,
		408: false, 429: false,
		500: false, 502: false, 503: false, 504: false,
	}
	for code, want := range cases {
		if got := IsPermanentStatus(code); got != want {
			t.Errorf("IsPermanentStatus(%d) = %v, kutilgan %v", code, got, want)
		}
	}
}

func TestPermanent(t *testing.T) {
	cause := errors.New("bad key")
	err := Permanent(cause)
	if !IsPermanent(err) || !errors.Is(err, cause) || err.Error() != "bad key" {
		t.Errorf("Permanent noto'g'ri: %v", err)
	}
	if wrapped := fmt.Errorf("ai: %w", err); !IsPermanent(wrapped) {
		t.Error("o'ralgan doimiy xato ham doimiy bo'lishi kerak")
	}
	if IsPermanent(cause) || IsPermanent(nil) {
		t.Error("oddiy xato doimiy emas")
	}
	if Permanent(nil) != nil {
		t.Error("Permanent(nil) nil bo'lishi kerak")
	}
}

func TestBackoffBounds(t *testing.T) {
	const base, maxDelay = 100 * time.Millisecond, 800 * time.Millisecond
	cases := []struct {
		attempt int
		d       time.Duration // jittersiz qiymat
	}{
		{0, base}, {1, base}, {2, 2 * base}, {3, 4 * base}, {4, maxDelay}, {5, maxDelay}, {100, maxDelay},
	}
	for _, tc := range cases {
		for range 200 {
			got := Backoff(tc.attempt, base, maxDelay)
			if got < tc.d || got > tc.d+tc.d/2 {
				t.Fatalf("Backoff(%d) = %v, [%v, %v] oralig'ida bo'lishi kerak", tc.attempt, got, tc.d, tc.d+tc.d/2)
			}
		}
	}
	if got := Backoff(3, 0, time.Second); got != 0 {
		t.Errorf("base 0 da kechikish 0 bo'lishi kerak: %v", got)
	}
	if got := Backoff(5, time.Second, 0); got < time.Second || got > 1500*time.Millisecond {
		t.Errorf("maxDelay < base — base ishlatilishi kerak: %v", got)
	}
}

func TestSleepCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if Sleep(ctx, time.Minute) {
		t.Error("bekor qilingan kontekstda Sleep false qaytarishi kerak")
	}
	if time.Since(started) > time.Second {
		t.Error("bekor qilingan kontekstda kutmasligi kerak")
	}
	if !Sleep(context.Background(), time.Millisecond) {
		t.Error("oddiy kutish true qaytarishi kerak")
	}
}

func TestRetryAttemptCount(t *testing.T) {
	calls := 0
	cause := errors.New("503")
	err := Retry(context.Background(), 3, time.Millisecond, func(context.Context) error {
		calls++
		return cause
	})
	if !errors.Is(err, cause) || calls != 3 {
		t.Errorf("3 ta urinish va oxirgi xato kutilgan: calls=%d, err=%v", calls, err)
	}

	calls = 0
	err = Retry(context.Background(), 3, time.Millisecond, func(context.Context) error {
		calls++
		if calls < 2 {
			return cause
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Errorf("muvaffaqiyatdan keyin to'xtashi kerak: calls=%d, err=%v", calls, err)
	}

	calls = 0
	_ = Retry(context.Background(), 0, time.Millisecond, func(context.Context) error {
		calls++
		return cause
	})
	if calls != 1 {
		t.Errorf("attempts < 1 da bir marta chaqirilishi kerak: %d", calls)
	}
}

func TestRetryStopsOnPermanent(t *testing.T) {
	calls := 0
	cause := errors.New("401")
	err := Retry(context.Background(), 5, time.Millisecond, func(context.Context) error {
		calls++
		return Permanent(cause)
	})
	if calls != 1 || !IsPermanent(err) || !errors.Is(err, cause) {
		t.Errorf("doimiy xatoda qayta urinilmasligi kerak: calls=%d, err=%v", calls, err)
	}
}

func TestRetryRespectsContext(t *testing.T) {
	// Kutish paytida bekor qilinadi — keyingi urinish bo'lmaydi.
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	started := time.Now()
	err := Retry(ctx, 5, time.Minute, func(context.Context) error {
		calls++
		cancel()
		return errors.New("503")
	})
	if calls != 1 || err == nil {
		t.Errorf("kontekst bekor bo'lgach qayta urinilmasligi kerak: calls=%d, err=%v", calls, err)
	}
	if time.Since(started) > 5*time.Second {
		t.Error("bekor qilingan kontekstda kutmasligi kerak")
	}

	// Kutish paytida muddat tugaydi — ctx.Err() qaytariladi.
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	calls = 0
	err = Retry(ctx, 5, time.Minute, func(context.Context) error {
		calls++
		return errors.New("503")
	})
	if calls != 1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("kutish paytida muddat tugasa ctx.Err() kutilgan: calls=%d, err=%v", calls, err)
	}
}
