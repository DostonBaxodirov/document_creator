package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
	"gorm.io/gorm"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/generation"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/lang"
	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/textutil"
)

// ---------------------------------------------------------------------------
// Buyruqlar
// ---------------------------------------------------------------------------

// onStart yangi buyurtmani boshlaydi. "/start ref_<ID>" — do'stining taklif
// havolasi orqali kirish (referral.go).
func (h *Handler) onStart(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	// Taklif faqat botga birinchi marta kirgan foydalanuvchiga hisoblanadi,
	// shuning uchun sessiya yaratilishidan oldin tekshiriladi.
	referrerTG, viaReferral := parseRefPayload(c.Message().Payload)
	viaReferral = viaReferral && h.referralOn()
	isNew := false
	if viaReferral {
		_, err := h.sessions.Get(ctx, c.Sender().ID)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			isNew = true
		case err != nil:
			return err
		}
	}

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	if viaReferral {
		h.handleReferralStart(ctx, c, referrerTG, isNew)
	}
	switch State(s.State) {
	case StateGenerating:
		return c.Send(msgBusy)
	case StateReview:
		return h.replyInReview(ctx, c, s)
	case StatePayment, StateConfirm:
		// Tashlab ketilgan buyurtma: to'lov kutilayotgan yoki bonus hisobidan
		// bepul, lekin tayyorlanmay qolgan bo'lsa — bekor qilinadi (bonus qaytadi).
		h.releaseOrder(ctx, s)
	}
	if err := h.beginFlow(ctx, s); err != nil {
		return err
	}

	name := c.Sender().FirstName
	if name == "" {
		name = "do'stim"
	}
	// Birinchi /start da — to'liq qo'llanma (foydalanish tartibi va tariflar),
	// keyingilarida — qisqa narxlar ro'yxati.
	first := s.GuideSentAt == nil
	welcome := fmt.Sprintf(msgWelcome, html.EscapeString(name))
	if !first {
		welcome += h.pricingText()
	}
	if err := c.Send(welcome, removeKeyboard()); err != nil {
		return err
	}
	if first {
		h.sendGuideOnce(ctx, c, s)
	}
	return c.Send(msgChooseWorkType, h.workTypeKeyboard())
}

// onHelp to'liq qo'llanmani (foydalanish tartibi va tariflar) yuboradi.
func (h *Handler) onHelp(c tele.Context) error {
	return c.Send(h.guideText())
}

// onCancel /cancel buyrug'i.
func (h *Handler) onCancel(c tele.Context) error {
	return c.Send(h.cancelFlow(c), removeKeyboard())
}

// onCancelButton "❌ Bekor qilish" inline tugmasi.
func (h *Handler) onCancelButton(c tele.Context) error {
	_ = c.Respond()
	h.removeButtons(c)
	return c.Send(h.cancelFlow(c), removeKeyboard())
}

// cancelFlow joriy jarayonni bekor qiladi va foydalanuvchiga javob matnini qaytaradi.
func (h *Handler) cancelFlow(c tele.Context) string {
	userID := c.Sender().ID
	defer h.lock(userID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		h.log.Error("cancel: failed to load session", zap.Int64("user_id", userID), zap.Error(err))
		return msgInternalError
	}

	switch State(s.State) {
	case StateGenerating:
		// Generatsiya o'z goroutine'ida ishlaydi: uni kontekst orqali to'xtatamiz,
		// holatni esa runJob o'zi "idle" ga o'tkazadi.
		if v, ok := h.running.Load(userID); ok {
			j := v.(*job)
			j.byUser.Store(true)
			j.cancel()
			return msgStopping
		}
		// Vazifa topilmadi (masalan, jarayon to'satdan to'xtagan) — holatni tiklaymiz.
	case StateIdle, "":
		return msgNothingCancel
	case StateReview:
		// Pul allaqachon o'tkazilgan — chek tekshiruvi tugashini kutamiz.
		return msgReviewWait
	case StatePayment, StateConfirm:
		h.releaseOrder(ctx, s)
	}

	if err := h.resetToIdle(ctx, s); err != nil {
		h.log.Error("cancel: failed to save session", zap.Int64("user_id", userID), zap.Error(err))
		return msgInternalError
	}
	return msgCancelled
}

// ---------------------------------------------------------------------------
// Matnli xabarlar (FSM router)
// ---------------------------------------------------------------------------

// onText foydalanuvchi holatiga qarab matnni tegishli qadamga yo'naltiradi.
func (h *Handler) onText(c tele.Context) error {
	text := c.Text()
	// Admin paroli istalgan holatda qabul qilinadi.
	if ok, err := h.tryAdminLogin(c, text); ok {
		return err
	}
	if strings.HasPrefix(text, "/chek_") { // hisobotdagi bosiladigan havola: /chek_12
		return h.showCheck(c, strings.TrimPrefix(text, "/chek_"))
	}
	if strings.HasPrefix(text, "/") {
		return c.Send(msgUnknownCommand)
	}

	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}

	state := State(s.State)
	switch state {
	case StateIdle, "":
		return c.Send(msgIdle, removeKeyboard())
	case StateWorkType:
		return c.Send(msgPressButton, h.workTypeKeyboard())
	case StateLanguage:
		return c.Send(msgPressButton, languageKeyboard())
	case StateDesign:
		return c.Send(msgDesignHint, h.designKeyboardFor(s))
	case StateTariff:
		return c.Send(msgTariffHint, h.tariffKeyboard(s))
	case StateConfirm:
		return h.sendSummary(c, s)
	case StatePayment:
		return h.sendPaymentInfo(ctx, c, s)
	case StateReview:
		return h.replyInReview(ctx, c, s)
	case StateGenerating:
		return c.Send(msgBusy)
	}

	st, ok := steps[state]
	if !ok {
		// Bazada noma'lum holat (masalan, eski versiyadan qolgan) — xavfsiz tiklash.
		h.log.Warn("unknown session state, resetting", zap.Int64("user_id", s.UserID), zap.String("state", s.State))
		if err := h.resetToIdle(ctx, s); err != nil {
			return err
		}
		return c.Send(msgIdle, removeKeyboard())
	}

	if err := st.apply(h.opts.Limits, s, text); err != nil {
		var ie *inputError
		if errors.As(err, &ie) {
			return c.Send("⚠️ " + html.EscapeString(ie.msg))
		}
		return err
	}

	next := st.nextState(s)
	s.State = string(next)
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}

	switch next {
	case StateConfirm:
		if err := c.Send("👌 Ma'lumotlar qabul qilindi.", removeKeyboard()); err != nil {
			return err
		}
		return h.sendSummary(c, s)
	case StateDesign:
		unit := "Slaydlar soni"
		if isDoc(s) {
			unit = "Sahifalar soni"
		}
		if err := c.Send(fmt.Sprintf("👌 %s: <b>%d</b>", unit, s.Volume), removeKeyboard()); err != nil {
			return err
		}
		return h.askDesign(c, s)
	}
	return h.ask(c, s, next)
}

// onMedia rasm, stiker va boshqa media xabarlarga javob beradi.
func (h *Handler) onMedia(c tele.Context) error {
	return c.Send(msgTextOnly)
}

// ---------------------------------------------------------------------------
// Inline tugmalar
// ---------------------------------------------------------------------------

// onWorkType ish turi tanlanganda chaqiriladi.
func (h *Handler) onWorkType(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	if State(s.State) != StateWorkType {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}

	opt, ok := findWorkType(ai.WorkType(c.Data()))
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}

	s.WorkType = string(opt.Type)
	s.State = string(StateLanguage)
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}

	_ = c.Respond(&tele.CallbackResponse{Text: opt.Name})
	h.edit(c, fmt.Sprintf("✅ Ish turi: <b>%s</b>", html.EscapeString(opt.Name)))
	return c.Send(msgChooseLang, languageKeyboard())
}

// onLanguage ish tili tanlanganda chaqiriladi.
func (h *Handler) onLanguage(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	if State(s.State) != StateLanguage {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	l, ok := lang.Find(c.Data())
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}

	s.Language = string(l.Code)
	s.State = string(StateTopic)
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}

	_ = c.Respond(&tele.CallbackResponse{Text: l.Name})
	h.edit(c, fmt.Sprintf("🌐 Til: <b>%s</b>", html.EscapeString(l.Name)))
	return h.ask(c, s, StateTopic)
}

// onDesign taqdimot yoki Word ish dizayni tanlanganda chaqiriladi.
func (h *Handler) onDesign(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	if State(s.State) != StateDesign {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	d, ok := resolveDesign(s, c.Data())
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: msgStaleButton})
	}
	custom := d.id == generator.ThemeCustom
	if custom && !h.customOn() {
		return c.Respond(&tele.CallbackResponse{Text: msgNoCustomCfg, ShowAlert: true})
	}
	s.Design, s.State = d.id, string(d.next)
	if custom {
		s.Tariff = ""
	}
	if err := h.sessions.Save(ctx, s); err != nil {
		return err
	}
	_ = c.Respond(&tele.CallbackResponse{Text: d.name})
	h.editPrompt(c, "🎨 Dizayn: <b>"+d.label+"</b>")
	if custom {
		return h.askTariff(c, s)
	}
	return h.ask(c, s, d.next)
}

// onRestart "Qaytadan to'ldirish" tugmasi — buyurtmani boshidan boshlaydi.
func (h *Handler) onRestart(c tele.Context) error {
	defer h.lock(c.Sender().ID)()
	ctx, cancel := requestContext()
	defer cancel()

	s, err := h.loadSession(ctx, c)
	if err != nil {
		return err
	}
	switch State(s.State) {
	case StateGenerating:
		return c.Respond(&tele.CallbackResponse{Text: "Ishingiz allaqachon tayyorlanmoqda."})
	case StateReview:
		return c.Respond(&tele.CallbackResponse{Text: "Chekingiz tekshirilmoqda."})
	case StatePayment, StateConfirm:
		h.releaseOrder(ctx, s)
	}
	if err := h.beginFlow(ctx, s); err != nil {
		return err
	}

	_ = c.Respond()
	h.removeButtons(c)
	return c.Send(msgChooseWorkType, h.workTypeKeyboard())
}

// onConfirm ma'lumotlar tasdiqlanganda (yoki "Qayta urinish" bosilganda) generatsiyani boshlaydi.
func (h *Handler) onConfirm(c tele.Context) error { return h.confirm(c, false) }

// onConfirmBonus «🎁 Bonus bilan tasdiqlash» — buyurtma taklif bonusi hisobidan.
func (h *Handler) onConfirmBonus(c tele.Context) error { return h.confirm(c, true) }

// confirm buyurtmani tasdiqlaydi: to'lovni hal qiladi va generatsiyani boshlaydi.
//
// Butun o'tish (CAS, o'qish, to'lov, saqlash) va vazifani ro'yxatga olish
// foydalanuvchi qulfi ostida bajariladi: aks holda shu oraliqda kelgan
// /cancel "generating" holatini ko'radi-yu, vazifani topa olmaydi.
func (h *Handler) confirm(c tele.Context, useBonus bool) error {
	userID := c.Sender().ID
	defer h.lock(userID)()
	ctx, cancel := requestContext()
	defer cancel()

	// Atomar o'tish: tugma ikki marta bosilsa ham generatsiya faqat bir marta boshlanadi.
	ok, err := h.cas(ctx, userID, StateConfirm, StateGenerating)
	if err != nil {
		return err
	}
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Bu so'rov allaqachon qabul qilingan yoki eskirgan."})
	}

	s, err := h.sessions.Get(ctx, userID)
	if err != nil {
		_, _ = h.cas(ctx, userID, StateGenerating, StateConfirm)
		return err
	}

	// To'lov: oldin to'langan buyurtma, taklif bonusi yoki to'lov so'rash.
	note := ""
	if h.paymentsOn() {
		proceed, n, err := h.authorize(ctx, c, s, useBonus)
		if errors.Is(err, storage.ErrNoCredits) {
			// Eskirgan tugma yoki bonus boshqa buyurtmaga ishlatilgan: xulosa
			// yangi narx va tugmalar bilan qayta yuboriladi.
			if _, err := h.cas(ctx, userID, StateGenerating, StateConfirm); err != nil {
				return err
			}
			_ = c.Respond(&tele.CallbackResponse{Text: msgNoCredits, ShowAlert: true})
			h.removeButtons(c)
			return h.sendSummary(c, s)
		}
		if err != nil {
			_, _ = h.cas(ctx, userID, StateGenerating, StateConfirm)
			return err
		}
		if !proceed {
			return nil // to'lov ko'rsatmasi yuborildi, chek kutilmoqda
		}
		note = "\n" + n
	}

	_ = c.Respond(&tele.CallbackResponse{Text: "Qabul qilindi ✅"})
	h.edit(c, summaryText(s)+note+"\n\n"+waitMessage(s.WorkType))

	h.startJob(s)
	return nil
}

// isDoc buyurtma Word ish (.docx) ekanini bildiradi.
func isDoc(s *storage.Session) bool { return ai.WorkType(s.WorkType).IsDocument() }

// beginFlow buyurtmani noldan boshlaydi (profil ma'lumotlari saqlanadi).
func (h *Handler) beginFlow(ctx context.Context, s *storage.Session) error {
	resetDraft(s)
	s.State = string(StateWorkType)
	return h.sessions.Save(ctx, s)
}

// resetToIdle joriy buyurtmani tozalaydi va sessiyani "idle" holatida saqlaydi.
func (h *Handler) resetToIdle(ctx context.Context, s *storage.Session) error {
	resetDraft(s)
	s.State = string(StateIdle)
	return h.sessions.Save(ctx, s)
}

// ask berilgan qadam savolini (kerak bo'lsa javob tugmalari bilan) yuboradi.
func (h *Handler) ask(c tele.Context, s *storage.Session, state State) error {
	st, ok := steps[state]
	if !ok {
		return fmt.Errorf("qadam topilmadi: %s", state)
	}
	markup := removeKeyboard()
	if st.suggest != nil {
		if options := st.suggest(h.opts.Limits, s); len(options) > 0 {
			markup = replyKeyboard(options)
		}
	}
	return c.Send(st.prompt(h.opts.Limits, s), markup)
}

// sendSummary yig'ilgan ma'lumotlarni tasdiqlash tugmalari bilan yuboradi
// (taklif bonusi bo'lsa — «🎁 Bonus bilan» va «💳 To'lab» tugmalari).
func (h *Handler) sendSummary(c tele.Context, s *storage.Session) error {
	text := summaryText(s)
	q := h.summaryQuote(c, s)
	if q != nil {
		text += "\n" + q.line()
	}
	return c.Send(text+"\n\nHammasi to'g'rimi?", confirmKeyboard(q))
}

// edit callback xabarini tahrirlaydi (inline tugmalar ham olib tashlanadi).
// "message is not modified" kabi zararsiz xatolar e'tiborsiz qoldiriladi.
func (h *Handler) edit(c tele.Context, text string) {
	if err := c.Edit(text); err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to edit message", zap.Error(err))
	}
}

// editPrompt callback xabarini tahrirlaydi: rasmli xabarda — izohini,
// oddiy xabarda — matnini (inline tugmalar ham olib tashlanadi).
func (h *Handler) editPrompt(c tele.Context, text string) {
	var err error
	if m := c.Message(); m != nil && m.Photo != nil {
		err = c.EditCaption(text)
	} else {
		err = c.Edit(text)
	}
	if err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to edit prompt message", zap.Error(err))
	}
}

// removeButtons callback xabaridagi inline tugmalarni olib tashlaydi.
func (h *Handler) removeButtons(c tele.Context) {
	if c.Message() == nil {
		return
	}
	if _, err := h.bot.EditReplyMarkup(c.Message(), nil); err != nil && !errors.Is(err, tele.ErrMessageNotModified) {
		h.log.Debug("failed to remove buttons", zap.Error(err))
	}
}

// summaryText buyurtma ma'lumotlarini HTML ko'rinishida qaytaradi.
func summaryText(s *storage.Session) string {
	return "📋 <b>Ma'lumotlarni tekshiring</b>\n\n" + orderDetails(s, maxSummaryRequirements)
}

// orderDetails buyurtma tafsilotlari (ish turi, til, mavzu, hajm, fayl,
// talaba ma'lumotlari va talablar) — foydalanuvchi xulosasi va admin xabari
// uchun. Talablar maxRequirements belgigacha qisqartiriladi.
func orderDetails(s *storage.Session, maxRequirements int) string {
	name := s.WorkType
	if opt, ok := findWorkType(ai.WorkType(s.WorkType)); ok {
		name = opt.Name
	}
	volume, format := "🔢 <b>Slaydlar soni:</b>", "PowerPoint (.pptx)"
	if isDoc(s) {
		volume, format = "📄 <b>Sahifalar soni:</b>", "Word (.docx)"
	}
	e := html.EscapeString
	if isCustom(s) {
		format += "\n🎨 <b>Dizayn:</b> " + customLabel + " · " + generation.TariffName(s.Tariff)
	} else if ai.WorkType(s.WorkType).HasDesigns() {
		if t, ok := generator.FindDocTheme(s.Design); ok {
			format += "\n🎨 <b>Dizayn:</b> " + t.Emoji + " " + e(t.Name)
		}
	} else if !isDoc(s) {
		if t, ok := generator.FindTheme(s.Design); ok {
			format += "\n🎨 <b>Dizayn:</b> " + t.Emoji + " " + e(t.Name)
			if t.Photos {
				format += " 📷 (rasmlar bilan)"
			}
		}
	}
	text := fmt.Sprintf("📌 <b>Ish turi:</b> %s\n"+
		"🌐 <b>Til:</b> %s\n"+
		"📝 <b>Mavzu:</b> %s\n"+
		"%s %d\n"+
		"📎 <b>Fayl:</b> %s\n"+
		"👤 <b>Talaba:</b> %s\n"+
		"🏛 <b>Universitet:</b> %s\n"+
		"👨‍🏫 <b>Tekshiruvchi:</b> %s",
		e(name), e(lang.Get(s.Language).Name), e(s.Topic), volume, s.Volume, format,
		e(s.StudentName), e(s.University), e(s.TeacherName))
	if s.Requirements != "" {
		text += "\n✍️ <b>Qo'shimcha talablar:</b> " + e(textutil.TruncateHard(s.Requirements, maxRequirements))
	}
	return text
}

// maxSummaryRequirements — xulosada ko'rsatiladigan talablar uzunligi
// (to'liq matn AI ga baribir yuboriladi).
const maxSummaryRequirements = 300
