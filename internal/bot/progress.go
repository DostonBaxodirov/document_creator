package bot

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/generation"
)

// progressInterval — jarayon xabari ko'pi bilan shu oraliqda bir marta
// yangilanadi (Telegram API cheklovlariga tushmaslik uchun).
const progressInterval = 2 * time.Second

// progressBoard butun ish davomidagi jarayon holatini bitta xabarda
// ko'rsatadi (generation.Progress ning Telegram'dagi ko'rinishi): bir vaqtda
// bir nechta bosqich (masalan, matn yozish va rasm chizish) faol bo'lishi
// mumkin, har biri xabarda alohida qator bo'lib turadi.
//
// Xabar birinchi holatda yuboriladi, keyin tahrirlanadi, Close chaqirilganda
// (yoki ish konteksti bekor qilinganda) esa o'chiriladi. Telegram'ga faqat
// bitta goroutine murojaat qiladi — ko'pi bilan interval da bir marta.
//
// Tugagan bosqich xabardan olib tashlanadi. Faol bosqich qolmasa, xabar
// tegilmaydi (bo'sh matn yuborib bo'lmaydi) — keyingi bosqich o'sha xabarda
// ko'rsatiladi.
type progressBoard struct {
	bot      *tele.Bot
	chat     *tele.Chat
	log      *zap.Logger
	interval time.Duration

	mu    sync.Mutex
	lines []*progressLine // ro'yxatdan o'tish tartibida

	wake      chan struct{} // holat o'zgardi (bufer 1: ketma-ket signallar birlashadi)
	stop      chan struct{}
	closeOnce sync.Once
	finished  chan struct{}
}

var _ generation.Progress = (*progressBoard)(nil)

// progressLine — bitta bosqich qatori.
type progressLine struct {
	stage  generation.Stage
	value  atomic.Pointer[[2]int] // {done, total}; nil — hali holat kelmagan
	closed atomic.Bool
}

// newProgressBoard chat uchun jarayon xabarini tayyorlaydi (xabar birinchi
// holat kelganda yuboriladi). ctx bekor qilinganda xabar o'chiriladi.
func newProgressBoard(ctx context.Context, bot *tele.Bot, chat *tele.Chat, log *zap.Logger, interval time.Duration) *progressBoard {
	b := &progressBoard{
		bot:      bot,
		chat:     chat,
		log:      log,
		interval: interval,
		wake:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		finished: make(chan struct{}),
	}
	go b.run(ctx)
	return b
}

// Stage yangi bosqich qatorini qo'shadi. Qator birinchi report dan keyin
// ko'rinadi va done chaqirilganda xabardan olib tashlanadi.
//
// report AI va rasm goroutine'laridan chaqiriladi: u hech qachon bloklanmaydi
// (faqat eng so'nggi holat saqlanadi) va bir vaqtda ko'p goroutine'dan
// chaqirilishi mumkin. done bir necha marta chaqirilsa ham xavfsiz.
func (b *progressBoard) Stage(st generation.Stage) (report func(done, total int), done func()) {
	l := &progressLine{stage: st}
	b.mu.Lock()
	b.lines = append(b.lines, l)
	b.mu.Unlock()

	report = func(d, total int) {
		if l.closed.Load() {
			return
		}
		l.value.Store(&[2]int{d, total})
		b.notify()
	}
	var once sync.Once
	done = func() {
		once.Do(func() {
			l.closed.Store(true)
			b.mu.Lock()
			b.lines = slices.DeleteFunc(b.lines, func(x *progressLine) bool { return x == l })
			b.mu.Unlock()
			b.notify()
		})
	}
	return report, done
}

// Close xabarni yangilashni to'xtatadi va uni o'chiradi (o'chirilguncha
// kutadi). Bir necha marta chaqirilsa ham xavfsiz.
func (b *progressBoard) Close() {
	b.closeOnce.Do(func() { close(b.stop) })
	<-b.finished
}

// notify xabarni yangilash kerakligini bildiradi (bloklanmaydi).
func (b *progressBoard) notify() {
	select {
	case b.wake <- struct{}{}:
	default: // signal allaqachon kutmoqda
	}
}

// text barcha faol bosqichlar qatorlari (har bosqich — bitta qator).
func (b *progressBoard) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var sb strings.Builder
	for _, l := range b.lines {
		v := l.value.Load()
		if v == nil {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(progressText(v[0], v[1], l.stage))
	}
	return sb.String()
}

// stopped — Close chaqirilgan yoki ish konteksti bekor qilingan.
func (b *progressBoard) stopped(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-b.stop:
		return true
	default:
		return false
	}
}

// run xabarni yuboradi va yangilab boradi; to'xtaganda uni o'chiradi.
func (b *progressBoard) run(ctx context.Context) {
	defer close(b.finished)
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("panic in progress loop", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
		}
	}()

	var (
		msg  *tele.Message
		last string // Telegram'dagi joriy matn
	)
	defer func() {
		if msg != nil {
			if err := b.bot.Delete(msg); err != nil {
				b.log.Debug("failed to delete progress message", zap.Error(err))
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.stop:
			return
		case <-b.wake:
		}
		if b.stopped(ctx) {
			return
		}
		text := b.text()
		if text == "" || text == last {
			continue // faol bosqich yo'q yoki matn o'zgarmagan
		}

		var err error
		if msg == nil {
			msg, err = b.bot.Send(b.chat, text)
		} else {
			_, err = b.bot.Edit(msg, text)
		}
		switch {
		case err == nil, errors.Is(err, tele.ErrMessageNotModified):
			last = text
		default:
			b.log.Debug("failed to update progress message", zap.Error(err))
		}

		select {
		case <-ctx.Done():
			return
		case <-b.stop:
			return
		case <-time.After(b.interval):
		}
	}
}

// progressText bitta bosqich qatori: "✍️ Bo'limlar yozilmoqda... ▰▰▰▱▱▱▱▱▱▱ 3/8".
func progressText(done, total int, st generation.Stage) string {
	total = max(total, 1)
	done = min(max(done, 0), total)

	const width = 10
	filled := done * width / total
	bar := strings.Repeat("▰", filled) + strings.Repeat("▱", width-filled)

	stage := st.Writing
	switch {
	case done == 0:
		stage = st.First
	case done == total:
		stage = st.Last
	}
	return fmt.Sprintf("%s <b>%s</b> %s %d/%d", st.Icon, stage, bar, done, total)
}
