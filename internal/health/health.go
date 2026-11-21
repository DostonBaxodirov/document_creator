// Package health botning sog'lig'ini HTTP serversiz bildiradi: baza javob
// bersa, belgilangan fayl (HEALTH_FILE) o'zgartirilgan vaqti yangilanadi.
// Docker HEALTHCHECK faylning yangiligini tekshiradi (Dockerfile).
package health

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	"go.uber.org/zap"
)

const (
	// Interval — tekshiruvlar orasidagi vaqt. Dockerfile dagi HEALTHCHECK
	// fayl oxirgi 1 daqiqada yangilanganini tekshiradi, shuning uchun
	// interval undan ancha kichik bo'lishi kerak.
	Interval = 15 * time.Second
	// PingTimeout — bitta baza tekshiruvi uchun maksimal vaqt.
	PingTimeout = 3 * time.Second
	// warnEvery — xatolar logga ko'pi bilan shu vaqtda bir marta yoziladi.
	warnEvery = time.Minute
)

// Pinger — bazaga ulanishni tekshiruvchi (*sql.DB shu interfeysga mos).
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Monitor bazani muntazam tekshiradi va muvaffaqiyatli bo'lsa sog'lik
// faylini yangilaydi. Bitta goroutine'da ishlatiladi (Run).
type Monitor struct {
	pinger   Pinger
	path     string
	log      *zap.Logger
	interval time.Duration
	timeout  time.Duration
	now      func() time.Time // loglarni siyraklashtirish uchun soat (testlarda almashtiriladi)

	failures int       // ketma-ket muvaffaqiyatsiz tekshiruvlar soni
	lastWarn time.Time // oxirgi Warn yozilgan vaqt
}

// New sog'lik monitorini yaratadi: har Interval da bazaga PingTimeout bilan
// so'rov yuboriladi va javob kelsa path fayli yangilanadi.
func New(p Pinger, path string, log *zap.Logger) *Monitor {
	return &Monitor{
		pinger:   p,
		path:     path,
		log:      log,
		interval: Interval,
		timeout:  PingTimeout,
		now:      time.Now,
	}
}

// Run ctx bekor qilinguncha bazani tekshirib turadi. Birinchi tekshiruv
// darhol bajariladi — konteyner ishga tushgach tezroq "healthy" bo'ladi.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		m.check(ctx)
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

// check bitta tekshiruvni bajaradi: baza javob bersa — fayl yangilanadi,
// aks holda fayl tegilmaydi (eskiradi va HEALTHCHECK xato beradi).
func (m *Monitor) check(ctx context.Context) {
	pingCtx, cancel := context.WithTimeout(ctx, m.timeout)
	err := m.pinger.PingContext(pingCtx)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return // bot to'xtamoqda — bu baza xatosi emas
		}
		m.fail("health check: database ping failed", err)
		return
	}
	if err := touch(m.path, time.Now()); err != nil {
		m.fail("health check: failed to update health file", err)
		return
	}
	m.failures = 0
}

// fail xatoni hisobga oladi va Warn darajasida, ko'pi bilan warnEvery da bir
// marta logga yozadi (baza uzoq ishlamasa, log to'lib ketmasin).
func (m *Monitor) fail(msg string, err error) {
	m.failures++
	now := m.now()
	if !m.lastWarn.IsZero() && now.Sub(m.lastWarn) < warnEvery {
		return
	}
	m.lastWarn = now
	m.log.Warn(msg,
		zap.Error(err),
		zap.Int("consecutive_failures", m.failures),
		zap.String("file", m.path))
}

// touch faylni (yo'q bo'lsa) yaratadi va o'zgartirilgan vaqtini t ga o'rnatadi.
func touch(path string, t time.Time) error {
	err := os.Chtimes(path, t, t)
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chtimes(path, t, t)
}
