package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// limitProbe bir vaqtda ishlayotgan chaqiruvlarni atomic hisoblagich bilan
// sanaydi va eng katta qiymatini (peak) eslab qoladi. Birinchi chaqiruvlar
// limit tasi yig'ilguncha kutadi: shunda test chegaradan oshmaslikni ham,
// chegaragacha haqiqatan parallel ishlashni ham tekshiradi.
type limitProbe struct {
	limit          int32
	calls          atomic.Int32
	inflight, peak atomic.Int32
	full           chan struct{}
	once           sync.Once
}

func newLimitProbe(limit int) *limitProbe {
	return &limitProbe{limit: int32(limit), full: make(chan struct{})}
}

// do bitta chaqiruvni taqlid qiladi.
func (p *limitProbe) do(ctx context.Context) error {
	p.calls.Add(1)
	n := p.inflight.Add(1)
	defer p.inflight.Add(-1)
	for {
		cur := p.peak.Load()
		if n <= cur || p.peak.CompareAndSwap(cur, n) {
			break
		}
	}
	if n >= p.limit {
		p.once.Do(func() { close(p.full) })
	}
	select {
	case <-p.full:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return fmt.Errorf("bir vaqtda %d ta chaqiruv yig'ilmadi (eng ko'pi %d)", p.limit, p.peak.Load())
	}
	// Chegarasiz pool'da chaqiruvlar albatta ustma-ust tushishi uchun.
	time.Sleep(2 * time.Millisecond)
	return nil
}

func TestForEachParallelLimit(t *testing.T) {
	const n, limit = 12, 3
	probe := newLimitProbe(limit)
	var done [n]atomic.Bool
	err := forEachParallel(context.Background(), n, limit, func(ctx context.Context, i int) error {
		if err := probe.do(ctx); err != nil {
			return err
		}
		done[i].Store(true)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if p := probe.peak.Load(); p != limit {
		t.Errorf("bir vaqtda eng ko'pi %d ta vazifa, %d bo'lishi kerak", p, limit)
	}
	for i := range done {
		if !done[i].Load() {
			t.Errorf("%d-vazifa bajarilmadi", i)
		}
	}
}

func TestForEachParallelFirstErrorCancelsRest(t *testing.T) {
	errFirst := errors.New("birinchi xato")
	var calls atomic.Int32
	err := forEachParallel(context.Background(), 20, 2, func(ctx context.Context, i int) error {
		calls.Add(1)
		if i == 0 {
			return errFirst
		}
		// Qolganlari bekor qilinishini kutadi.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return errors.New("kontekst bekor qilinmadi")
		}
	})
	if !errors.Is(err, errFirst) {
		t.Errorf("birinchi xato qaytarilishi kerak: %v", err)
	}
	if c := calls.Load(); c > 2 {
		t.Errorf("xatodan keyin yangi vazifalar boshlanmasligi kerak, chaqiruvlar: %d", c)
	}

	// Panic xatoga aylanadi va qolganlarini to'xtatadi.
	err = forEachParallel(context.Background(), 5, 2, func(ctx context.Context, i int) error {
		if i == 1 {
			panic("buzildi")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "ai: panic: buzildi") {
		t.Errorf("panic xatoga aylanishi kerak: %v", err)
	}
}

func TestForEachParallelCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	err := forEachParallel(ctx, 5, 2, func(context.Context, int) error { calls.Add(1); return nil })
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Errorf("bekor qilingan kontekstda: err=%v, chaqiruvlar=%d", err, calls.Load())
	}

	// Ish davomida bekor qilinsa — vazifalar xatosiz tugasa ham kontekst xatosi
	// qaytadi va qolgan vazifalar boshlanmaydi.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	calls.Store(0)
	err = forEachParallel(ctx, 5, 1, func(context.Context, int) error {
		calls.Add(1)
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Errorf("ish davomida bekor qilinganda: err=%v, chaqiruvlar=%d", err, calls.Load())
	}
}

// probedDocProvider bo'lim so'rovlarini limitProbe orqali o'tkazadi.
type probedDocProvider struct {
	*fakeDocProvider
	probe *limitProbe
}

func (p *probedDocProvider) complete(ctx context.Context, t task) (string, error) {
	if strings.HasPrefix(t.user, "You are writing") {
		if err := p.probe.do(ctx); err != nil {
			return "", err
		}
	}
	return p.fakeDocProvider.complete(ctx, t)
}

// TestWriteSectionsParallelLimit — bo'limlar Client.parallel tadan ortiq
// bir vaqtda yozilmaydi.
func TestWriteSectionsParallelLimit(t *testing.T) {
	const words, limit = 3000, 2
	b := planBudget(words)
	p := &probedDocProvider{fakeDocProvider: &fakeDocProvider{chapters: b.chapters, percent: 100}, probe: newLimitProbe(limit)}
	c := testClient(p)
	c.parallel = limit
	doc, err := c.GenerateDocument(context.Background(), DocumentRequest{WorkType: WorkIndependent, Topic: "Mavzu", Words: words})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.probe.peak.Load(); got != limit {
		t.Errorf("bir vaqtda eng ko'pi %d ta bo'lim yozildi, %d bo'lishi kerak", got, limit)
	}
	if want := int32(len(doc.Chapters) + 2); p.probe.calls.Load() != want {
		t.Errorf("bo'lim so'rovlari %d ta, %d bo'lishi kerak", p.probe.calls.Load(), want)
	}
}
