package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// fakePinger — soxta baza: err qaytaradi (nil — baza ishlayapti), block
// bo'lsa ctx bekor qilinguncha javob bermaydi. Har chaqiruv called ga
// signal beradi.
type fakePinger struct {
	err    error
	block  bool
	calls  atomic.Int32
	called chan struct{}
}

func newFakePinger(err error) *fakePinger {
	return &fakePinger{err: err, called: make(chan struct{}, 64)}
}

func (f *fakePinger) PingContext(ctx context.Context) error {
	f.calls.Add(1)
	select {
	case f.called <- struct{}{}:
	default:
	}
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

// waitCalls pinger kamida n marta chaqirilishini kutadi.
func (f *fakePinger) waitCalls(t *testing.T, n int) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-f.called:
		case <-timeout:
			t.Fatalf("pinger %d marta chaqirilishi kutilgan edi, chaqirildi: %d", n, f.calls.Load())
		}
	}
}

// startMonitor monitorni qisqa interval bilan ishga tushiradi; qaytgan
// funksiya ctx ni bekor qiladi va Run qaytishini kutadi.
func startMonitor(t *testing.T, m *Monitor) (stop func()) {
	t.Helper()
	m.interval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Run(ctx)
	}()
	return func() {
		t.Helper()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run ctx bekor qilingandan keyin qaytmadi")
		}
	}
}

// staleFile bir soat oldin o'zgartirilgan fayl yaratadi.
func staleFile(t *testing.T) (path string, mtime time.Time) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "healthy")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mtime = time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path, mtime
}

func modTime(t *testing.T, path string) time.Time {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("sog'lik fayli: %v", err)
	}
	return st.ModTime()
}

// TestMonitorTouchesOnSuccess — baza javob bersa fayl yaratiladi, eskirgani
// esa yangilanadi; xato logga yozilmaydi.
func TestMonitorTouchesOnSuccess(t *testing.T) {
	t.Run("yangi fayl", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "healthy")
		core, logs := observer.New(zapcore.WarnLevel)
		p := newFakePinger(nil)
		stop := startMonitor(t, New(p, path, zap.New(core)))
		p.waitCalls(t, 2) // ikkinchi chaqiruv — birinchi tekshiruv fayl bilan yakunlangan
		stop()

		if age := time.Since(modTime(t, path)); age > time.Minute {
			t.Errorf("fayl yangi bo'lishi kerak, yoshi: %v", age)
		}
		if logs.Len() != 0 {
			t.Errorf("muvaffaqiyatli tekshiruvda Warn bo'lmasligi kerak: %v", logs.All())
		}
	})

	t.Run("eskirgan fayl", func(t *testing.T) {
		path, old := staleFile(t)
		p := newFakePinger(nil)
		stop := startMonitor(t, New(p, path, zap.NewNop()))
		p.waitCalls(t, 2)
		stop()

		if got := modTime(t, path); !got.After(old.Add(59 * time.Minute)) {
			t.Errorf("fayl vaqti yangilanishi kerak: %v (eski %v)", got, old)
		}
	})
}

// TestMonitorNoTouchOnFailure — baza javob bermasa fayl yaratilmaydi ham,
// yangilanmaydi ham; Warn ko'pi bilan daqiqada bir marta yoziladi.
func TestMonitorNoTouchOnFailure(t *testing.T) {
	t.Run("fayl yo'q", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "healthy")
		core, logs := observer.New(zapcore.WarnLevel)
		p := newFakePinger(errors.New("baza ishlamayapti"))
		stop := startMonitor(t, New(p, path, zap.New(core)))
		p.waitCalls(t, 3)
		stop()

		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("baza xatosida fayl yaratilmasligi kerak: %v", err)
		}
		if n := logs.FilterMessage("health check: database ping failed").Len(); n != 1 {
			t.Errorf("bir daqiqa ichida Warn %d marta yozildi, kutilgan 1", n)
		}
	})

	t.Run("eskirgan fayl", func(t *testing.T) {
		path, old := staleFile(t)
		p := newFakePinger(errors.New("baza ishlamayapti"))
		stop := startMonitor(t, New(p, path, zap.NewNop()))
		p.waitCalls(t, 3)
		stop()

		if got := modTime(t, path); !got.Equal(old) {
			t.Errorf("baza xatosida fayl vaqti o'zgarmasligi kerak: %v (eski %v)", got, old)
		}
	})
}

// TestMonitorStopsOnCancel — ctx bekor qilinsa Run qaytadi (baza javobini
// kutib turgan bo'lsa ham), keyin pinger chaqirilmaydi va to'xtash xato
// sifatida logga yozilmaydi.
func TestMonitorStopsOnCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthy")
	core, logs := observer.New(zapcore.WarnLevel)
	p := newFakePinger(nil)
	p.block = true
	m := New(p, path, zap.New(core))
	m.timeout = time.Hour // ping faqat ctx bekor qilinganda tugaydi
	stop := startMonitor(t, m)
	p.waitCalls(t, 1)
	stop()

	calls := p.calls.Load()
	time.Sleep(30 * time.Millisecond) // bir necha interval
	if got := p.calls.Load(); got != calls {
		t.Errorf("to'xtagandan keyin pinger chaqirilmasligi kerak: %d → %d", calls, got)
	}
	if logs.Len() != 0 {
		t.Errorf("to'xtash xato sifatida yozilmasligi kerak: %v", logs.All())
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("javobsiz tekshiruvda fayl yaratilmasligi kerak: %v", err)
	}
}

// TestMonitorWarnThrottle — xatolar daqiqada ko'pi bilan bir marta logga
// yoziladi; faylni yozib bo'lmasligi ham xato hisoblanadi.
func TestMonitorWarnThrottle(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	// Papka mavjud emas — faylni yaratib bo'lmaydi.
	path := filepath.Join(t.TempDir(), "yoq", "healthy")
	m := New(newFakePinger(nil), path, zap.New(core))
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }

	for _, step := range []time.Duration{0, 20 * time.Second, 30 * time.Second, 15 * time.Second} {
		now = now.Add(step) // 0s, 20s, 50s, 65s
		m.check(context.Background())
	}
	entries := logs.FilterMessage("health check: failed to update health file").All()
	if len(entries) != 2 {
		t.Fatalf("Warn %d marta yozildi, kutilgan 2 (0s va 65s da)", len(entries))
	}
	if got := entries[1].ContextMap()["consecutive_failures"]; got != int64(4) {
		t.Errorf("consecutive_failures = %v, kutilgan 4", got)
	}
}
