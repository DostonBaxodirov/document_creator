package bot

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generation"
	"telegram-presentation-bot/internal/storage"
)

// ---------------------------------------------------------------------------
// Generatsiya vazifasi
// ---------------------------------------------------------------------------

// job — ishlayotgan generatsiya vazifasi (foydalanuvchi /cancel bilan to'xtatishi uchun).
type job struct {
	cancel context.CancelFunc
	byUser atomic.Bool
}

// startJob vazifani h.running ga yozadi va uni alohida goroutine'da
// ishga tushiradi. Foydalanuvchi qulfi ostida chaqirilishi shart: qulf
// ochilganda "generating" holatidagi har bir sessiyaning vazifasi ro'yxatda
// bo'ladi va /cancel uni albatta topadi.
func (h *Handler) startJob(s *storage.Session) {
	// Kontekst faqat bekor qilish uchun: JobTimeout navbatdan joy olingach
	// (generate ichida) qo'yiladi, navbatda kutilgan vaqt hisobga kirmaydi.
	ctx, cancel := context.WithCancel(h.jobsCtx)
	j := &job{cancel: cancel}
	h.running.Store(s.UserID, j)
	h.wg.Add(1)
	go h.runJob(ctx, j, s)
}

// runJob AI → fayl (PPTX/DOCX) → Telegram zanjirini alohida goroutine'da bajaradi.
// Vazifa (j) startJob da ro'yxatga olingan; tugaganda ro'yxatdan o'chiriladi.
func (h *Handler) runJob(ctx context.Context, j *job, s *storage.Session) {
	defer h.wg.Done()
	defer j.cancel()
	// Faqat o'z yozuvimizni o'chiramiz: shu orada foydalanuvchi yangi vazifa
	// boshlagan bo'lsa (masalan, xatodan keyin "Qayta urinish"), u saqlanib qoladi.
	defer h.running.CompareAndDelete(s.UserID, j)

	chat := &tele.Chat{ID: s.ChatID}
	log := h.log.With(
		zap.Int64("user_id", s.UserID),
		zap.String("work_type", s.WorkType),
		zap.Int("volume", s.Volume))

	// Goroutine ichidagi panic butun dasturni to'xtatmasligi va foydalanuvchi
	// "generating" holatida qotib qolmasligi uchun.
	defer func() {
		if r := recover(); r != nil {
			log.Error("panic recovered in generation job", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
			h.finishWithError(s, chat, log, fmt.Errorf("panic: %v", r), false)
		}
	}()

	started := time.Now()
	log.Info("generation started")

	out, err := h.generate(ctx, chat, s, log)
	if out != nil {
		defer removeFile(out.Path, log) // vaqtinchalik fayl har qanday holatda o'chiriladi
	}
	if err != nil {
		h.finishWithError(s, chat, log, err, j.byUser.Load())
		return
	}

	took := time.Since(started)
	log.Info("generation finished", zap.Duration("took", took))
	h.finishWithSuccess(s, chat, log, out, took)
}

// generate navbatni kutadi, AI dan matn oladi, faylni (PPTX yoki DOCX) yaratadi
// va foydalanuvchiga yuboradi. Fayl yaratilgan bo'lsa, xato bo'lganda ham
// u qaytariladi (o'chirish uchun).
func (h *Handler) generate(ctx context.Context, chat *tele.Chat, s *storage.Session, log *zap.Logger) (*output, error) {
	// 1. Navbat: bo'sh joy bo'lmasa, foydalanuvchini ogohlantirib kutamiz.
	// Kutish vaqti cheklanmaydi — faqat bekor qilish (/cancel yoki to'xtash).
	select {
	case h.slots <- struct{}{}:
	default:
		_, _ = h.bot.Send(chat, msgQueued)
		select {
		case h.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer func() { <-h.slots }()
	// Joy bo'shagan paytda vazifa bekor qilingan bo'lishi mumkin.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// JobTimeout navbatdan joy olingandan keyin hisoblanadi.
	ctx, cancel := context.WithTimeout(ctx, h.opts.JobTimeout)
	defer cancel()

	// Barcha bosqichlar (dizayn, matn, rasmlar) holati bitta xabarda
	// ko'rsatiladi; xato bo'lsa ham xabar o'chiriladi.
	progress := newProgressBoard(ctx, h.bot, chat, log, progressInterval)
	defer progress.Close()

	stopAction := h.keepUploadAction(ctx, chat)
	defer stopAction()

	// 2. AI + fayl: ish turiga qarab taqdimot yoki hujjat.
	res, err := h.gen.Build(generation.WithLogger(ctx, log), orderOf(s), progress)
	if err != nil {
		return nil, err
	}
	out := &output{Output: *res}
	if err := ctx.Err(); err != nil {
		return out, err
	}

	// 3. Telegram'ga hujjat sifatida yuborish (jarayon xabari undan oldin o'chiriladi).
	progress.Close()
	doc := &tele.Document{
		File:     tele.FromDisk(out.Path),
		FileName: out.FileName,
		MIME:     out.MIME,
		Caption:  out.Caption,
	}
	sent, err := h.bot.Send(chat, doc)
	if err != nil {
		return out, fmt.Errorf("telegram: hujjatni yuborib bo'lmadi: %w", err)
	}
	if sent != nil {
		out.delivered = &tele.StoredMessage{MessageID: strconv.Itoa(sent.ID), ChatID: chat.ID}
	}
	log.Info("document sent", append([]zap.Field{zap.String("file_name", doc.FileName)}, out.LogFields...)...)
	return out, nil
}

// output — tayyor fayl (generatsiya natijasi) va uning Telegram'dagi nusxasi.
type output struct {
	generation.Output
	// delivered — foydalanuvchiga yuborilgan fayl xabari (faqat ID lari):
	// fayl adminlarga shu xabardan nusxalanadi, qayta yuklanmaydi va saqlanmaydi.
	delivered *tele.StoredMessage
}

// orderOf sessiyadagi ma'lumotlardan generatsiya buyurtmasini tuzadi.
func orderOf(s *storage.Session) generation.Order {
	return generation.Order{
		WorkType:     ai.WorkType(s.WorkType),
		Topic:        s.Topic,
		Language:     s.Language,
		Requirements: s.Requirements,
		Design:       s.Design,
		Tariff:       s.Tariff,
		StudentName:  s.StudentName,
		University:   s.University,
		TeacherName:  s.TeacherName,
		Volume:       s.Volume,
	}
}

// finishWithSuccess sessiyani yakunlaydi, statistikani yangilaydi va xarid
// haqida adminlarga xabar (tayyor fayl bilan) beradi.
func (h *Handler) finishWithSuccess(s *storage.Session, chat *tele.Chat, log *zap.Logger, out *output, took time.Duration) {
	ctx, cancel := requestContext()
	defer cancel()

	if s.OrderID != nil && h.billing != nil {
		if _, err := h.billing.SetOrderStatus(ctx, *s.OrderID, storage.OrderPaid, storage.OrderDelivered); err != nil {
			log.Error("failed to mark order delivered", zap.Uint("order_id", *s.OrderID), zap.Error(err))
		}
	}
	// Xabar sessiya tozalanishidan oldin tayyorlanadi (buyurtma ma'lumotlari kerak).
	notice := h.purchaseNotice(ctx, s, out, took, log)

	now := time.Now().UTC()
	s.GeneratedCount++
	s.LastGeneratedAt = &now
	if err := h.resetToIdle(ctx, s); err != nil {
		log.Error("failed to save session after success", zap.Error(err))
	}
	if _, err := h.bot.Send(chat, msgDone); err != nil {
		log.Warn("failed to send done message", zap.Error(err))
	}
	if notice != "" {
		var file *tele.StoredMessage
		if out != nil {
			file = out.delivered
		}
		h.purchaseToAdmins(notice, file, log)
	}
}

// finishWithError xatoni logga yozadi va foydalanuvchiga tushunarli xabar yuboradi.
// Foydalanuvchi o'zi bekor qilgan bo'lsa — sessiya tozalanadi, aks holda
// ma'lumotlar saqlanib, "Qayta urinish" tugmasi taklif qilinadi.
func (h *Handler) finishWithError(s *storage.Session, chat *tele.Chat, log *zap.Logger, err error, byUser bool) {
	ctx, cancel := requestContext()
	defer cancel()

	if byUser {
		// To'langan buyurtma "paid" holatida qoladi va keyingi buyurtmada
		// qayta to'lovsiz ishlatiladi (FindCredit).
		log.Info("generation cancelled by user")
		if err := h.resetToIdle(ctx, s); err != nil {
			log.Error("failed to save session after cancel", zap.Error(err))
		}
		_, _ = h.bot.Send(chat, msgJobCancelled)
		return
	}

	log.Error("generation failed", zap.Error(err))
	if _, err := h.cas(ctx, s.UserID, StateGenerating, StateConfirm); err != nil {
		log.Error("failed to restore confirm state", zap.Error(err))
	}

	var msg string
	switch {
	case h.jobsCtx.Err() != nil:
		msg = "🔧 Bot texnik sabablarga ko'ra qayta ishga tushirilmoqda. Bir necha daqiqadan so'ng «Qayta urinish» tugmasini bosing."
	case errors.Is(err, context.DeadlineExceeded):
		msg = "⌛ Tayyorlash juda uzoq davom etdi. Iltimos, qayta urinib ko'ring yoki hajmni (slaydlar/sahifalar sonini) kamaytiring."
	case errors.Is(err, generation.ErrAI):
		msg = "😔 Xizmatda vaqtinchalik muammo yuz berdi. Iltimos, birozdan so'ng qayta urinib ko'ring."
	default:
		msg = "😔 Faylni tayyorlashda xatolik yuz berdi. Iltimos, qayta urinib ko'ring."
	}
	if _, err := h.bot.Send(chat, msg, retryKeyboard()); err != nil {
		log.Warn("failed to send error message", zap.Error(err))
	}
	// Buyurtma (to'langan yoki bepul) tayyorlanmay qolgani haqida adminlarga.
	// Bot to'xtatilayotganda yubormaymiz — qayta ishga tushgach tiklanadi.
	if h.jobsCtx.Err() == nil {
		if notice := h.failureNotice(ctx, s, err, log); notice != "" {
			h.toAdmins(notice)
		}
	}
}

// keepUploadAction generatsiya davomida "hujjat yuborilmoqda..." statusini
// ko'rsatib turadi (Telegram uni ~5 soniyadan keyin o'chiradi).
func (h *Handler) keepUploadAction(ctx context.Context, chat *tele.Chat) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				h.log.Error("panic in chat action loop", zap.Any("panic", r))
			}
		}()
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			_ = h.bot.Notify(chat, tele.UploadingDocument)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return cancel
}

// waitMessage generatsiya boshlanganda yuboriladigan kutish xabari (ish turiga qarab).
func waitMessage(workType string) string {
	if ai.WorkType(workType).IsDocument() {
		return msgGeneratingDoc
	}
	return msgGeneratingDeck
}

// RecoverInterrupted bot to'satdan to'xtab qolgan (crash, deploy) paytda
// "generating" holatida qolib ketgan sessiyalarni tasdiqlash holatiga
// qaytaradi va foydalanuvchilarga qayta urinish tugmasini yuboradi.
func (h *Handler) RecoverInterrupted(ctx context.Context) {
	list, err := h.listByState(ctx, StateGenerating)
	if err != nil {
		h.log.Error("failed to list interrupted jobs", zap.Error(err))
		return
	}
	recovered := 0
	for _, s := range list {
		ok, err := h.cas(ctx, s.UserID, StateGenerating, StateConfirm)
		if err != nil || !ok {
			continue
		}
		recovered++
		if _, err := h.bot.Send(&tele.Chat{ID: s.ChatID}, msgInterrupted, retryKeyboard()); err != nil {
			h.log.Warn("failed to notify user about interrupted job", zap.Int64("user_id", s.UserID), zap.Error(err))
		}
	}
	if recovered > 0 {
		h.log.Info("recovered interrupted jobs", zap.Int("count", recovered))
	}
	h.recoverReviews(ctx)
}
