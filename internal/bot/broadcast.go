package bot

// Admin e'loni: /taklif_elon — barcha foydalanuvchilarga do'stlarni taklif
// qilish e'loni, har biriga o'zining shaxsiy havolasi bilan.
//
// Admin avval xabarning aynan ko'rinishini va oluvchilar sonini ko'radi, so'ng
// bitta tugma bilan yuborishni boshlaydi. Yuborish alohida goroutine'da
// Telegram cheklovidan past tezlikda (broadcastRate) ketadi:
//   - 429 (flood) javobida Telegram aytgan vaqt kutiladi va qayta yuboriladi;
//   - botni bloklagan yoki akkaunti o'chirilgan foydalanuvchilar alohida sanaladi;
//   - jarayon (hisoblagichlar va kursor) bazada saqlanadi: bot qayta ishga
//     tushsa (deploy, crash), e'lon to'xtagan joyidan davom etadi;
//   - bir vaqtda faqat bitta e'lon yuboriladi (bazadagi qisman unique indeks);
//   - admin «⏹ To'xtatish» tugmasi bilan to'xtatishi mumkin.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/tz"
)

const (
	// broadcastRate — soniyasiga yuboriladigan xabarlar. Telegram ommaviy
	// xabarlarni ~30/s gacha cheklaydi; qolgani botning oddiy ishi uchun zaxira.
	broadcastRate       = 20
	broadcastBatch      = 500              // bazadan bir martada olinadigan oluvchilar
	broadcastCheckpoint = 50               // har shuncha oluvchidan keyin jarayon bazaga yoziladi
	broadcastEditEvery  = 5 * time.Second  // admin chatidagi jarayon xabarini yangilash oralig'i
	broadcastAttempts   = 3                // 429 javobida bitta oluvchiga urinishlar soni
	broadcastDBRetry    = 10 * time.Second // oluvchilarni o'qib bo'lmaganda kutish
)

// E'lon xabarlari.
const (
	msgBroadcastOff     = "📣 E'lon yuborish hozircha mavjud emas (to'lov yoki taklif tizimi o'chirilgan)."
	msgBroadcastRunning = "E'lon allaqachon yuborilmoqda."
	msgBroadcastResumed = "♻️ Bot qayta ishga tushdi — taklif e'lonini yuborish to'xtagan joyidan davom ettirilmoqda (%d / %d)."
)

// broadcastRun — yuborilayotgan e'lon (to'xtatish uchun).
type broadcastRun struct {
	cancel  context.CancelFunc
	stopped atomic.Bool // admin to'xtatdi (bot to'xtatilishidan farqlash uchun)
}

// sendResult — bitta oluvchiga yuborish natijasi.
type sendResult int

const (
	sendOK        sendResult = iota // yetkazildi
	sendBlocked                     // botni bloklagan, akkaunt o'chirilgan yoki chat topilmadi
	sendFailed                      // boshqa xato
	sendRetry                       // 429: kutib, qayta yuborish kerak
	sendCancelled                   // e'lon to'xtatildi — oluvchi hisobga olinmaydi
)

// broadcastOn e'lon yuborish mumkinligini bildiradi.
func (h *Handler) broadcastOn() bool { return h.bcast != nil && h.referralOn() }

// onBroadcast /taklif_elon — e'lonning ko'rinishi va tasdiqlash tugmasi
// (e'lon yuborilayotgan bo'lsa — uning jarayoni va to'xtatish tugmasi).
func (h *Handler) onBroadcast(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	link := h.referralLink(c.Sender().ID)
	if !h.broadcastOn() || link == "" {
		return c.Send(msgBroadcastOff)
	}
	ctx, cancel := requestContext()
	defer cancel()

	running, err := h.bcast.Running(ctx)
	if err != nil {
		return err
	}
	for i := range running {
		if b := &running[i]; b.Kind == storage.BroadcastReferral {
			return c.Send(broadcastProgressText(b), broadcastStopKeyboard(b.ID))
		}
	}
	total, err := h.bcast.CountRecipients(ctx)
	if err != nil {
		return err
	}
	last, err := h.bcast.Last(ctx, storage.BroadcastReferral)
	if err != nil {
		return err
	}
	// Xabar foydalanuvchilarga qanday borsa, adminga ham aynan shunday (uning
	// o'z havolasi bilan) ko'rsatiladi.
	if err := c.Send(h.announcementText(link), announcementKeyboard(link), tele.NoPreview); err != nil {
		return err
	}
	return c.Send(broadcastConfirmText(total, last), broadcastConfirmKeyboard(total))
}

// onBroadcastStart «✅ Hammaga yuborish» — e'lonni yuborishni boshlaydi.
func (h *Handler) onBroadcastStart(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	if !h.broadcastOn() || h.referralLink(c.Sender().ID) == "" {
		return c.Respond(&tele.CallbackResponse{Text: msgBroadcastOff, ShowAlert: true})
	}
	ctx, cancel := requestContext()
	defer cancel()

	total, err := h.bcast.CountRecipients(ctx)
	if err != nil {
		return err
	}
	b, err := h.bcast.Start(ctx, storage.BroadcastReferral, c.Sender().ID, total)
	if errors.Is(err, storage.ErrConflict) {
		return c.Respond(&tele.CallbackResponse{Text: msgBroadcastRunning, ShowAlert: true})
	}
	if err != nil {
		return err
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Yuborish boshlandi 📣"})

	// Tasdiqlash xabari jarayon xabariga aylanadi.
	if m := c.Message(); m != nil {
		b.MessageID = m.ID
		if err := h.bcast.SetMessage(ctx, b.ID, m.ID); err != nil {
			h.log.Warn("failed to save broadcast message", zap.Uint("broadcast_id", b.ID), zap.Error(err))
		}
	}
	h.editBroadcast(b, broadcastProgressText(b), broadcastStopKeyboard(b.ID))
	h.log.Info("broadcast started", zap.Uint("broadcast_id", b.ID), zap.Int64("admin_id", b.StartedBy), zap.Int("total", total))
	h.startBroadcast(b)
	return nil
}

// onBroadcastCancel «❌ Bekor qilish» — e'lon yuborilmaydi.
func (h *Handler) onBroadcastCancel(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	_ = c.Respond()
	h.edit(c, "❌ E'lon yuborilmadi.")
	return nil
}

// onBroadcastStop «⏹ To'xtatish» — yuborilayotgan e'lonni to'xtatadi.
func (h *Handler) onBroadcastStop(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	id, err := strconv.ParseUint(c.Data(), 10, 64)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	v, ok := h.bcRuns.Load(uint(id))
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Bu e'lon allaqachon yakunlangan."})
	}
	run := v.(*broadcastRun)
	run.stopped.Store(true)
	run.cancel()
	h.log.Info("broadcast stop requested", zap.Uint64("broadcast_id", id), zap.Int64("admin_id", c.Sender().ID))
	return c.Respond(&tele.CallbackResponse{Text: "To'xtatilmoqda ⏹"})
}

// PauseBroadcasts yuborilayotgan e'lonlarni darhol to'xtatadi (bot
// to'xtatilayotganda chaqiriladi): jarayon bazaga yoziladi, e'lon "running"
// holatida qoladi va keyingi ishga tushishda kursordan davom etadi. Shunda
// to'xtash uzoq e'lon tugashini kutmaydi.
func (h *Handler) PauseBroadcasts() {
	h.bcRuns.Range(func(_, v any) bool {
		v.(*broadcastRun).cancel()
		return true
	})
}

// startBroadcast e'lonni alohida goroutine'da yuboradi (bot to'xtatilganda
// jobsCtx bekor qilinadi va jarayon saqlanib, keyingi ishga tushishda davom etadi).
func (h *Handler) startBroadcast(b *storage.Broadcast) {
	ctx, cancel := context.WithCancel(h.jobsCtx)
	run := &broadcastRun{cancel: cancel}
	h.bcRuns.Store(b.ID, run)
	h.wg.Add(1)
	go h.runBroadcast(ctx, run, b)
}

// runBroadcast oluvchilarni user_id kursori bo'yicha sahifalab, har biriga
// e'lonni yuboradi.
func (h *Handler) runBroadcast(ctx context.Context, run *broadcastRun, b *storage.Broadcast) {
	defer h.wg.Done()
	defer run.cancel()
	defer h.bcRuns.Delete(b.ID)
	log := h.log.With(zap.Uint("broadcast_id", b.ID))
	defer func() {
		if r := recover(); r != nil {
			// E'lon "running" holatida qoladi va keyingi ishga tushishda davom etadi.
			log.Error("panic recovered in broadcast", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
			h.saveBroadcast(b, log)
		}
	}()

	started := time.Now()
	limiter := time.NewTicker(time.Second / broadcastRate)
	defer limiter.Stop()
	lastEdit, unsaved := time.Now(), 0

loop:
	for {
		page, err := h.broadcastPage(ctx, b.LastUserID)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Error("failed to load broadcast recipients", zap.Error(err))
			select {
			case <-ctx.Done():
				break loop
			case <-time.After(broadcastDBRetry):
			}
			continue
		}
		if len(page) == 0 {
			break
		}
		for _, rcp := range page {
			select {
			case <-ctx.Done():
				break loop
			case <-limiter.C:
			}
			switch h.deliverAnnouncement(ctx, rcp) {
			case sendCancelled:
				break loop
			case sendOK:
				b.Sent++
			case sendBlocked:
				b.Blocked++
			default:
				b.Failed++
			}
			b.LastUserID = rcp.UserID
			if unsaved++; unsaved >= broadcastCheckpoint {
				h.saveBroadcast(b, log)
				unsaved = 0
			}
			if time.Since(lastEdit) >= broadcastEditEvery {
				h.editBroadcast(b, broadcastProgressText(b), broadcastStopKeyboard(b.ID))
				lastEdit = time.Now()
			}
		}
	}

	stopped := run.stopped.Load()
	if ctx.Err() != nil && !stopped {
		// Bot to'xtatilmoqda: e'lon "running" holatida qoladi va keyingi
		// ishga tushishda kursordan davom etadi.
		h.saveBroadcast(b, log)
		log.Info("broadcast paused by shutdown", zap.Int("processed", b.Processed()), zap.Int("total", b.Total))
		return
	}
	status := storage.BroadcastDone
	if stopped {
		status = storage.BroadcastCancelled
	}
	dbCtx, cancel := requestContext()
	defer cancel()
	if err := h.bcast.Finish(dbCtx, b, status); err != nil {
		log.Error("failed to finish broadcast", zap.Error(err))
	}
	took := time.Since(started)
	log.Info("broadcast finished", zap.String("status", status), zap.Int("sent", b.Sent),
		zap.Int("blocked", b.Blocked), zap.Int("failed", b.Failed), zap.Duration("took", took))

	text := broadcastResultText(b, took)
	h.editBroadcast(b, text)
	// Yangi xabar ham yuboriladi — admin bildirishnoma oladi.
	h.notifyUser(b.StartedBy, text)
}

// broadcastPage navbatdagi oluvchilar sahifasi.
func (h *Handler) broadcastPage(ctx context.Context, afterUserID int64) ([]storage.Recipient, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return h.bcast.Recipients(ctx, afterUserID, broadcastBatch)
}

// saveBroadcast jarayonni bazaga yozadi (e'lon bekor qilingan bo'lsa ham
// ishlashi uchun alohida kontekstda).
func (h *Handler) saveBroadcast(b *storage.Broadcast, log *zap.Logger) {
	ctx, cancel := requestContext()
	defer cancel()
	if err := h.bcast.SaveProgress(ctx, b); err != nil {
		log.Warn("failed to save broadcast progress", zap.Error(err))
	}
}

// deliverAnnouncement bitta oluvchiga uning shaxsiy havolasi bilan e'lon
// yuboradi; 429 da Telegram aytgan vaqt kutiladi va qayta urinadi.
func (h *Handler) deliverAnnouncement(ctx context.Context, rcp storage.Recipient) sendResult {
	link := h.referralLink(rcp.UserID)
	text, markup := h.announcementText(link), announcementKeyboard(link)
	chat := &tele.Chat{ID: rcp.ChatID}
	for attempt := 1; ; attempt++ {
		_, err := h.bot.Send(chat, text, markup, tele.NoPreview)
		res, wait := classifySendErr(err)
		if res != sendRetry {
			if res == sendFailed {
				h.log.Debug("broadcast message failed", zap.Int64("user_id", rcp.UserID), zap.Error(err))
			}
			return res
		}
		if attempt >= broadcastAttempts {
			return sendFailed
		}
		select {
		case <-ctx.Done():
			return sendCancelled
		case <-time.After(wait):
		}
	}
}

// classifySendErr Telegram javobini tasniflaydi; sendRetry da — kutish vaqti.
func classifySendErr(err error) (sendResult, time.Duration) {
	if err == nil {
		return sendOK, 0
	}
	var flood tele.FloodError
	if errors.As(err, &flood) {
		return sendRetry, time.Duration(max(flood.RetryAfter, 1)) * time.Second
	}
	var tgErr *tele.Error
	if errors.As(err, &tgErr) && tgErr.Code == http.StatusTooManyRequests {
		return sendRetry, time.Second
	}
	for _, blocked := range []error{tele.ErrBlockedByUser, tele.ErrUserIsDeactivated,
		tele.ErrNotStartedByUser, tele.ErrChatNotFound, tele.ErrKickedFromGroup} {
		if errors.Is(err, blocked) {
			return sendBlocked, 0
		}
	}
	// telebot ro'yxatida yo'q 403 javoblari oddiy xato matni bilan qaytadi.
	if strings.Contains(err.Error(), "Forbidden") {
		return sendBlocked, 0
	}
	return sendFailed, 0
}

// editBroadcast admin chatidagi jarayon xabarini yangilaydi.
func (h *Handler) editBroadcast(b *storage.Broadcast, text string, opts ...any) {
	if b.MessageID == 0 || h.bot == nil {
		return
	}
	msg := &tele.StoredMessage{MessageID: strconv.Itoa(b.MessageID), ChatID: b.StartedBy}
	if _, err := h.bot.Edit(msg, text, opts...); err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to edit broadcast message", zap.Uint("broadcast_id", b.ID), zap.Error(err))
	}
}

// broadcastConfirmText yuborishdan oldingi tasdiqlash matni.
func broadcastConfirmText(total int, last *storage.Broadcast) string {
	var b strings.Builder
	b.WriteString("📣 <b>Taklif e'loni</b>\n\n")
	fmt.Fprintf(&b, "Yuqoridagi xabar barcha <b>%s</b> ta foydalanuvchiga yuboriladi — har biriga o'zining shaxsiy havolasi bilan.\n",
		formatCount(int64(total)))
	fmt.Fprintf(&b, "⏱ Taxminiy vaqt: %s.\n", broadcastETA(total))
	if last != nil {
		state := "yakunlangan"
		if last.Status == storage.BroadcastCancelled {
			state = "to'xtatilgan"
		}
		fmt.Fprintf(&b, "🕒 Oxirgi e'lon: %s — %s ta yetkazilgan (%s).\n",
			last.CreatedAt.In(tz.Tashkent).Format(dateTimeLayout), formatCount(int64(last.Sent)), state)
	}
	b.WriteString("\nYuborilsinmi?")
	return b.String()
}

// broadcastETA yuborishning taxminiy davomiyligi.
func broadcastETA(total int) string {
	secs := (total + broadcastRate - 1) / broadcastRate
	if secs < 60 {
		return "1 daqiqadan kam"
	}
	return fmt.Sprintf("~%d daqiqa", (secs+59)/60)
}

func broadcastConfirmKeyboard(total int) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(m.Data(fmt.Sprintf("✅ Hammaga yuborish (%s)", formatCount(int64(total))), cbBcStart)),
		m.Row(m.Data("❌ Bekor qilish", cbBcCancel)),
	)
	return m
}

func broadcastStopKeyboard(id uint) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("⏹ To'xtatish", cbBcStop, strconv.FormatUint(uint64(id), 10))))
	return m
}

// broadcastProgressText yuborish jarayoni.
func broadcastProgressText(b *storage.Broadcast) string {
	return "📣 <b>Taklif e'loni yuborilmoqda…</b>\n\n" + broadcastCounters(b) +
		"\n\n<i>Bot qayta ishga tushsa ham, e'lon to'xtagan joyidan davom etadi.</i>"
}

// broadcastResultText yakuniy hisobot.
func broadcastResultText(b *storage.Broadcast, took time.Duration) string {
	title := "✅ <b>Taklif e'loni yuborildi</b>"
	if b.Status == storage.BroadcastCancelled {
		title = "⏹ <b>Taklif e'loni to'xtatildi</b>"
	}
	return title + "\n\n" + broadcastCounters(b) + "\n⏱ Vaqt: " + formatDuration(took)
}

func broadcastCounters(b *storage.Broadcast) string {
	done := b.Processed()
	total := max(b.Total, done) // yuborish davomida yangi foydalanuvchilar qo'shilishi mumkin
	percent := 100
	if total > 0 {
		percent = done * 100 / total
	}
	return fmt.Sprintf("✅ Yetkazildi: <b>%s</b>\n🚫 Botni bloklagan: %s\n⚠️ Xato: %s\n📊 Jarayon: %s / %s (%d%%)",
		formatCount(int64(b.Sent)), formatCount(int64(b.Blocked)), formatCount(int64(b.Failed)),
		formatCount(int64(done)), formatCount(int64(total)), percent)
}

// resumeBroadcasts bot qayta ishga tushganda uzilib qolgan e'lonlarni
// to'xtagan joyidan davom ettiradi.
func (h *Handler) resumeBroadcasts(ctx context.Context) {
	if !h.broadcastOn() {
		return
	}
	list, err := h.bcast.Running(ctx)
	if err != nil {
		h.log.Error("failed to list running broadcasts", zap.Error(err))
		return
	}
	for i := range list {
		b := list[i]
		h.log.Info("resuming broadcast", zap.Uint("broadcast_id", b.ID), zap.Int("processed", b.Processed()), zap.Int("total", b.Total))
		h.notifyUser(b.StartedBy, fmt.Sprintf(msgBroadcastResumed, b.Processed(), max(b.Total, b.Processed())))
		h.startBroadcast(&b)
	}
}
