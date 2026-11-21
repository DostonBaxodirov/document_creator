package bot

// Bazaning kunlik zaxira nusxasi: har kuni belgilangan vaqtda (Toshkent)
// barcha adminlarga fayl bo'lib (ovozsiz) yuboriladi; /backup — admin
// so'rovi bilan darhol, faqat o'ziga.

import (
	"context"
	"fmt"
	"html"
	"os"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/backup"
	"telegram-presentation-bot/internal/textutil"
	"telegram-presentation-bot/internal/tz"
)

// BackupOptions — kunlik nusxa sozlamalari.
type BackupOptions struct {
	Dumper Backuper // nil — kunlik nusxa va /backup o'chirilgan
	Hour   int      // yuborish vaqti (Toshkent): soat
	Minute int      // va daqiqa
}

const (
	// maxBackupBytes — Telegram Bot API orqali yuboriladigan fayl chegarasi (50 MB).
	maxBackupBytes = 50 << 20
	// backupSendTimeout — nusxani barcha adminlarga yuborish uchun vaqt.
	backupSendTimeout = 5 * time.Minute

	titleDailyBackup  = "Kunlik backup"
	titleManualBackup = "Backup"
)

// nextBackup now dan keyingi hour:minute vaqti (Toshkent). Aynan shu daqiqada
// chaqirilsa — ertangi kun.
func nextBackup(now time.Time, hour, minute int) time.Time {
	local := now.In(tz.Tashkent)
	t := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, tz.Tashkent)
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// RunBackups har kuni belgilangan vaqtda bazaning nusxasini barcha adminlarga
// yuboradi. ctx bekor qilinguncha ishlaydi (alohida goroutine'da chaqiring).
func (h *Handler) RunBackups(ctx context.Context) {
	o := h.opts.Backup
	if o.Dumper == nil || h.billing == nil {
		return
	}
	for {
		next := nextBackup(time.Now(), o.Hour, o.Minute)
		h.log.Info("next backup scheduled", zap.Time("at", next))
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		h.backupToAdmins(ctx)
	}
}

// backupToAdmins nusxani barcha adminlarga yuboradi (admin bo'lmasa —
// nusxa olinmaydi).
func (h *Handler) backupToAdmins(ctx context.Context) {
	lctx, cancel := requestContext()
	admins, err := h.billing.ListAdmins(lctx)
	cancel()
	if err != nil {
		h.log.Error("backup: failed to list admins", zap.Error(err))
		return
	}
	if len(admins) == 0 {
		h.log.Warn("backup skipped: no admins")
		return
	}
	chats := make([]int64, len(admins))
	for i, a := range admins {
		chats[i] = a.TelegramUserID
	}
	if !h.backupMu.TryLock() {
		h.log.Warn("backup skipped: another backup is running")
		return
	}
	defer h.backupMu.Unlock()
	h.sendBackup(ctx, chats, titleDailyBackup, time.Now())
}

// onBackup /backup — nusxani darhol olib, so'ragan adminga yuboradi.
func (h *Handler) onBackup(c tele.Context) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	if h.opts.Backup.Dumper == nil {
		return c.Send(msgBackupOff)
	}
	if !h.backupMu.TryLock() {
		return c.Send(msgBackupBusy)
	}
	defer h.backupMu.Unlock()
	if err := c.Send(msgBackupWait); err != nil {
		return err
	}
	h.sendBackup(h.jobsCtx, []int64{c.Sender().ID}, titleManualBackup, time.Now())
	return nil
}

// sendBackup nusxa oladi va chats ga yuboradi. Fayl bir marta yuklanadi,
// keyingi adminlarga file_id orqali yuboriladi. Xato bo'lsa, ular xabardor
// qilinadi. Nechta chatga yetkazilganini qaytaradi.
func (h *Handler) sendBackup(ctx context.Context, chats []int64, title string, now time.Time) int {
	f, err := h.opts.Backup.Dumper.Dump(ctx, now)
	if err != nil {
		h.log.Error("backup failed", zap.Error(err))
		h.sendText(chats, fmt.Sprintf("⚠️ <b>%s olinmadi</b>\n\n<code>%s</code>",
			title, html.EscapeString(textutil.TruncateHard(err.Error(), maxNoticeErrRunes))))
		return 0
	}
	defer func() { _ = os.Remove(f.Path) }()

	if f.Size > maxBackupBytes {
		h.log.Error("backup too large for Telegram", zap.Int64("bytes", f.Size))
		h.sendText(chats, fmt.Sprintf("⚠️ <b>%s yuborilmadi</b>: fayl %s — Telegram orqali 50 MB dan katta fayl "+
			"yuborib bo'lmaydi. Nusxani serverda qo'lda oling (docs/DEPLOY.md).", title, formatBytes(f.Size)))
		return 0
	}

	caption := backupCaption(title, f, now)
	sctx, cancel := context.WithTimeout(context.Background(), backupSendTimeout)
	defer cancel()
	var fileID string
	sent := 0
	for _, id := range chats {
		if sctx.Err() != nil {
			break
		}
		file := tele.FromDisk(f.Path)
		if fileID != "" {
			file = tele.File{FileID: fileID}
		}
		doc := &tele.Document{File: file, FileName: f.Name, MIME: "application/gzip", Caption: caption}
		msg, err := h.bot.Send(&tele.Chat{ID: id}, doc, tele.Silent)
		if err != nil {
			h.log.Warn("failed to send backup", zap.Int64("chat_id", id), zap.Error(err))
			continue
		}
		sent++
		if fileID == "" && msg != nil && msg.Document != nil {
			fileID = msg.Document.FileID
		}
	}
	h.log.Info("backup sent", zap.String("file", f.Name), zap.Int64("bytes", f.Size),
		zap.Int("chats", len(chats)), zap.Int("sent", sent))
	return sent
}

// sendText matnni chats ga ovozsiz yuboradi (xatolar logga yoziladi).
func (h *Handler) sendText(chats []int64, text string) {
	for _, id := range chats {
		if _, err := h.bot.Send(&tele.Chat{ID: id}, text, tele.Silent); err != nil {
			h.log.Warn("failed to send message", zap.Int64("chat_id", id), zap.Error(err))
		}
	}
}

// backupCaption nusxa fayli izohi.
func backupCaption(title string, f *backup.File, now time.Time) string {
	return fmt.Sprintf("💾 <b>%s</b> — %s\n📦 %s\n\n"+
		"<i>Bazaning to'liq nusxasi (foydalanuvchilar, buyurtmalar, to'lovlar). "+
		"Maxfiy ma'lumot — hech kimga yubormang. Tiklash: docs/DEPLOY.md.</i>",
		title, now.In(tz.Tashkent).Format(dateTimeLayout), formatBytes(f.Size))
}
