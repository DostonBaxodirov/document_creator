package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

// refBilling — taklif tizimi oqimlari uchun xotiradagi ombor.
type refBilling struct {
	BillingStore

	mu       sync.Mutex
	credits  int
	orders   []*storage.Order
	released []uint
	attached []int64 // AttachReferral ga berilgan taklif qilganlar
	referrer *storage.User
	rewarded []uint // RewardReferral ga berilgan buyurtmalar
	reward   *storage.ReferralReward
}

func (b *refBilling) EnsureUser(_ context.Context, telegramID int64, username, _ string) (*storage.User, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return &storage.User{ID: 1, TelegramUserID: telegramID, Username: username, FreeCredits: b.credits}, nil
}

func (b *refBilling) FindCredit(context.Context, uint, string, int) (*storage.Order, error) {
	return nil, nil
}

func (b *refBilling) GetOrder(_ context.Context, id uint) (*storage.Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id == 0 || int(id) > len(b.orders) {
		return nil, errors.New("buyurtma topilmadi")
	}
	return b.orders[id-1], nil
}

func (b *refBilling) add(o *storage.Order) {
	o.ID = uint(len(b.orders) + 1)
	o.CreatedAt = time.Now()
	b.orders = append(b.orders, o)
}

func (b *refBilling) CreateOrder(_ context.Context, o *storage.Order) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.add(o)
	return nil
}

func (b *refBilling) CreateBonusOrder(_ context.Context, o *storage.Order) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.credits <= 0 {
		return 0, storage.ErrNoCredits
	}
	b.credits--
	b.add(o)
	return b.credits, nil
}

func (b *refBilling) ReleaseOrder(_ context.Context, id uint) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.released = append(b.released, id)
	o := b.orders[id-1]
	if o.Status == storage.OrderPending || (o.Status == storage.OrderPaid && o.Price == 0 && o.Discount > 0) {
		o.Status = storage.OrderCancelled
		if o.Discount > 0 {
			b.credits++
		}
		return true, nil
	}
	return false, nil
}

func (b *refBilling) SaveOrder(context.Context, *storage.Order) error { return nil }

func (b *refBilling) SetOrderStatus(_ context.Context, id uint, from, to string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if o := b.orders[id-1]; o.Status == from {
		o.Status = to
		return true, nil
	}
	return false, nil
}

func (b *refBilling) OrderPayment(context.Context, uint) (*storage.Payment, error) { return nil, nil }
func (b *refBilling) ListAdmins(context.Context) ([]storage.User, error)           { return nil, nil }

func (b *refBilling) AttachReferral(_ context.Context, referrerTG, _ int64, _, _ string) (*storage.User, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attached = append(b.attached, referrerTG)
	return b.referrer, nil
}

func (b *refBilling) RewardReferral(_ context.Context, _, orderID uint) (*storage.ReferralReward, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rewarded = append(b.rewarded, orderID)
	return b.reward, nil
}

func (b *refBilling) ReferralStats(context.Context, uint) (int, int, error) { return 3, 1, nil }

// refOptions — to'lov va taklif tizimi yoqilgan sozlamalar.
func refOptions(billing BillingStore) Options {
	return Options{
		Limits:            config.LimitsConfig{MinSlides: 5, MaxSlides: 50, MinPages: 5, MaxPages: 30},
		MaxConcurrentJobs: 1,
		JobTimeout:        5 * time.Second,
		Images:            fakeImages{},
		Billing:           billing,
		Payment:           config.PaymentConfig{Enabled: true, PriceSlidePhoto: 300, PriceSlidePlain: 250, PriceDocument: 7000, PricePage: 200},
		Referral:          config.ReferralConfig{Enabled: true, CustomMax: 15000},
		Custom:            testTariffs(),
	}
}

// refHandler soxta Telegram serveriga ulangan handler.
func refHandler(t *testing.T, sessions SessionStore, billing BillingStore) (*Handler, func() []apiCall) {
	t.Helper()
	b, calls := fakeTelegram(t, "")
	b.Me.Username = "testbot"
	h := NewHandler(context.Background(), sessions, &fakeDocAI{}, Builders{Papers: stubPapers{dir: t.TempDir()}}, zap.NewNop(), refOptions(billing))
	h.bot = b
	return h, calls
}

// callsTo berilgan chatga yuborilgan sendMessage matnlari.
func callsTo(calls []apiCall, chatID int64) []string {
	var out []string
	for _, c := range calls {
		if c.method == "sendMessage" && fmt.Sprint(c.params["chat_id"]) == fmt.Sprint(chatID) {
			out = append(out, fmt.Sprint(c.params["text"]))
		}
	}
	return out
}

func TestParseRefPayload(t *testing.T) {
	cases := map[string]struct {
		id int64
		ok bool
	}{
		"ref_123":   {123, true},
		" ref_42 ":  {42, true},
		"ref_":      {0, false},
		"ref_abc":   {0, false},
		"ref_-5":    {0, false},
		"ref_0":     {0, false},
		"123":       {0, false},
		"":          {0, false},
		"promo_123": {0, false},
	}
	for in, want := range cases {
		id, ok := parseRefPayload(in)
		if id != want.id || ok != want.ok {
			t.Errorf("parseRefPayload(%q) = %d, %v; kutilgan %d, %v", in, id, ok, want.id, want.ok)
		}
	}
}

func TestReferralLink(t *testing.T) {
	link := referralLinkFor("testbot", 7132518114)
	if link != "https://t.me/testbot?start=ref_7132518114" {
		t.Errorf("havola = %q", link)
	}
	// Telegram /start parametri: 64 belgigacha, faqat A-Z a-z 0-9 _ -.
	payload := strings.TrimPrefix(link, "https://t.me/testbot?start=")
	if len(payload) > 64 || strings.Trim(payload, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" {
		t.Errorf("parametr Telegram talabiga mos emas: %q", payload)
	}
	if id, ok := parseRefPayload(payload); !ok || id != 7132518114 {
		t.Errorf("havola parametri o'qilmadi: %d %v", id, ok)
	}
	share := shareURL(link)
	if !strings.HasPrefix(share, "https://t.me/share/url?url=https%3A%2F%2Ft.me%2Ftestbot%3Fstart%3Dref_7132518114&text=") {
		t.Errorf("ulashish havolasi = %q", share)
	}
	if (&Handler{}).referralLink(1) != "" {
		t.Error("bot ulanmagan bo'lsa havola bo'sh bo'lishi kerak")
	}
}

func TestBonusCover(t *testing.T) {
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), refOptions(&refBilling{}))
	cases := []struct {
		kind        string
		total, want int
	}{
		{storage.KindPlain, 30000, 30000},         // tayyor dizayn — to'liq
		{storage.KindPhoto, 15000, 15000},         // rasmli tayyor dizayn — to'liq
		{storage.AIKind("premium"), 34000, 15000}, // maxsus — 15 000 gacha
		{storage.AIKind("standard"), 9000, 9000},
	}
	for _, c := range cases {
		if got := h.bonusCover(c.kind, c.total); got != c.want {
			t.Errorf("bonusCover(%s, %d) = %d, kutilgan %d", c.kind, c.total, got, c.want)
		}
	}
	h.opts.Referral.CustomMax = 0
	if got := h.bonusCover(storage.AIKind("premium"), 34000); got != 0 {
		t.Errorf("CustomMax=0: bonusCover = %d, kutilgan 0", got)
	}
}

func TestQuoteBonus(t *testing.T) {
	fb := &refBilling{}
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), refOptions(fb))
	ctx := context.Background()
	plain := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeBento, Volume: 12}
	custom := &storage.Session{WorkType: string(ai.WorkPresentation), Design: generator.ThemeCustom, Tariff: config.TariffPremium, Volume: 20}

	// Bonus yo'q — oddiy narx, tugma yo'q.
	q, err := h.quote(ctx, plain, &storage.User{ID: 1})
	if err != nil || q.bonusOK() || strings.Contains(q.line(), "Bonus") {
		t.Fatalf("bonussiz: %+v %q %v", q, q.line(), err)
	}

	u := &storage.User{ID: 1, FreeCredits: 2}
	q, _ = h.quote(ctx, plain, u)
	if !q.bonusOK() || q.bonusPrice() != 0 || q.cover != 3000 {
		t.Errorf("tayyor dizayn bonus bilan to'liq bepul bo'lishi kerak: %+v", q)
	}
	if line := q.line(); !strings.Contains(line, "Bonus bilan:</b> <b>bepul</b>") || !strings.Contains(line, "2 ta") {
		t.Errorf("narx qatori: %q", line)
	}

	q, _ = h.quote(ctx, custom, u)
	if q.price != 34000 || !q.bonusOK() || q.cover != 15000 || q.bonusPrice() != 19000 {
		t.Errorf("maxsus dizayn: 15 000 chegirma, 19 000 to'lov kutilgan: %+v", q)
	}
	if line := q.line(); !strings.Contains(line, "19 000 so'm") || !strings.Contains(line, "15 000 so'm chegirma") {
		t.Errorf("narx qatori: %q", line)
	}

	h.opts.Referral.Enabled = false
	if q, _ = h.quote(ctx, plain, u); q.bonusOK() || q.credits != 0 {
		t.Errorf("taklif tizimi o'chirilgan bo'lsa bonus taklif qilinmasligi kerak: %+v", q)
	}
}

func TestQuoteReusesPaidSessionOrder(t *testing.T) {
	fb := &refBilling{}
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), refOptions(fb))
	ctx := context.Background()
	// Bonus hisobidan bepul, lekin generatsiya xato bilan tugagan buyurtma.
	_ = fb.CreateOrder(ctx, &storage.Order{UserID: 1, Price: 0, Discount: 1000, Status: storage.OrderPaid})
	s := paperSession(7, StateConfirm)
	s.OrderID = &fb.orders[0].ID

	q, err := h.quote(ctx, &s, &storage.User{ID: 1, FreeCredits: 3})
	if err != nil || q.mode != quoteCredit || q.credit.ID != 1 || q.bonusOK() {
		t.Fatalf("«Qayta urinish» sessiyadagi to'langan buyurtmani ishlatishi kerak: %+v %v", q, err)
	}
	// Boshqa foydalanuvchining yoki to'lanmagan buyurtma ishlatilmaydi.
	if q, _ := h.quote(ctx, &s, &storage.User{ID: 2}); q.mode != quotePaid {
		t.Errorf("boshqa foydalanuvchining buyurtmasi ishlatildi: %+v", q)
	}
	fb.orders[0].Status = storage.OrderCancelled
	if q, _ := h.quote(ctx, &s, &storage.User{ID: 1}); q.mode != quotePaid {
		t.Errorf("bekor qilingan buyurtma ishlatildi: %+v", q)
	}
}

func TestConfirmKeyboard(t *testing.T) {
	uniques := func(m *tele.ReplyMarkup) (out []string) {
		for _, row := range m.InlineKeyboard {
			for _, b := range row {
				out = append(out, b.Unique)
			}
		}
		return out
	}
	if got := strings.Join(uniques(confirmKeyboard(nil)), ","); got != "confirm,restart,cancel" {
		t.Errorf("bonussiz tugmalar: %s", got)
	}
	q := &quote{mode: quotePaid, price: 34000, credits: 1, cover: 15000}
	m := confirmKeyboard(q)
	if got := strings.Join(uniques(m), ","); got != "confirm_b,confirm,restart,cancel" {
		t.Errorf("bonusli tugmalar: %s", got)
	}
	if bonus, paid := m.InlineKeyboard[0][0].Text, m.InlineKeyboard[1][0].Text; !strings.Contains(bonus, "19 000") || !strings.Contains(paid, "34 000") {
		t.Errorf("tugma matnlari: %q, %q", bonus, paid)
	}
	q.cover = 34000
	if text := confirmKeyboard(q).InlineKeyboard[0][0].Text; !strings.Contains(text, "bepul") {
		t.Errorf("to'liq bepul tugma matni: %q", text)
	}
}

func TestPaymentTextBonus(t *testing.T) {
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), refOptions(&refBilling{}))
	h.opts.Payment.CardNumber = "8600123456789012"
	o := &storage.Order{ID: 9, WorkType: string(ai.WorkPresentation), Kind: storage.AIKind("premium"), Volume: 20, Price: 19000, Discount: 15000}
	text := h.paymentText(o, true)
	for _, want := range []string{"19 000 so'm", "Bonus:</b> −15 000 so'm", "to'liq narx 34 000 so'm"} {
		if !strings.Contains(text, want) {
			t.Errorf("to'lov matnida %q yo'q:\n%s", want, text)
		}
	}
	o.Discount = 0
	if strings.Contains(h.paymentText(o, true), "Bonus") {
		t.Error("bonussiz buyurtmada bonus qatori bo'lmasligi kerak")
	}
}

func TestPaymentLineBonus(t *testing.T) {
	s := sampleSession()
	free := purchase{session: s, order: &storage.Order{ID: 3, Price: 0, Discount: 3000, IsPaid: true}}
	if got := paymentLine(free); !strings.Contains(got, "taklif bonusi") || !strings.Contains(got, "3 000 so'm") {
		t.Errorf("to'liq bepul bonusli buyurtma: %q", got)
	}
	partial := purchase{session: s,
		order:   &storage.Order{ID: 4, Price: 19000, Discount: 15000, IsPaid: true},
		payment: &storage.Payment{ID: 8, Amount: 19000}}
	if got := paymentLine(partial); !strings.Contains(got, "19 000 so'm") || !strings.Contains(got, "+ 🎁 bonus 15 000 so'm") {
		t.Errorf("qisman bonusli buyurtma: %q", got)
	}
}

func TestOnStartReferral(t *testing.T) {
	const (
		userID     = 501
		referrerID = 99
	)
	start := func(h *Handler, payload string) error {
		user := &tele.User{ID: userID, FirstName: "Vali", Username: "vali"}
		chat := &tele.Chat{ID: userID, Type: tele.ChatPrivate}
		c := h.bot.NewContext(tele.Update{Message: &tele.Message{ID: 1, Sender: user, Chat: chat,
			Text: "/start " + payload, Payload: payload}})
		return h.onStart(c)
	}

	t.Run("yangi foydalanuvchi bog'lanadi va taklif qilgan xabardor qilinadi", func(t *testing.T) {
		fb := &refBilling{referrer: &storage.User{ID: 2, TelegramUserID: referrerID}}
		h, calls := refHandler(t, newMemSessions(), fb)
		if err := start(h, "ref_99"); err != nil {
			t.Fatal(err)
		}
		if len(fb.attached) != 1 || fb.attached[0] != referrerID {
			t.Fatalf("AttachReferral chaqiruvlari: %v", fb.attached)
		}
		texts := callsTo(calls(), referrerID)
		if len(texts) != 1 || !strings.Contains(texts[0], "Vali (@vali)") {
			t.Errorf("taklif qilganga xabar: %v", texts)
		}
	})

	t.Run("eski foydalanuvchi bog'lanmaydi", func(t *testing.T) {
		fb := &refBilling{referrer: &storage.User{ID: 2, TelegramUserID: referrerID}}
		h, calls := refHandler(t, newMemSessions(storage.Session{UserID: userID, ChatID: userID, State: string(StateIdle)}), fb)
		if err := start(h, "ref_99"); err != nil {
			t.Fatal(err)
		}
		if len(fb.attached) != 0 || len(callsTo(calls(), referrerID)) != 0 {
			t.Errorf("eski foydalanuvchi bog'landi: %v", fb.attached)
		}
	})

	t.Run("o'z havolasi", func(t *testing.T) {
		fb := &refBilling{}
		h, calls := refHandler(t, newMemSessions(), fb)
		if err := start(h, fmt.Sprintf("ref_%d", userID)); err != nil {
			t.Fatal(err)
		}
		if len(fb.attached) != 0 {
			t.Errorf("o'zini o'zi taklif qildi: %v", fb.attached)
		}
		if !strings.Contains(strings.Join(callsTo(calls(), userID), "\n"), "shaxsiy taklif havolangiz") {
			t.Error("o'z havolasi haqida xabar yuborilmadi")
		}
	})

	t.Run("taklif tizimi o'chirilgan", func(t *testing.T) {
		fb := &refBilling{referrer: &storage.User{ID: 2, TelegramUserID: referrerID}}
		h, _ := refHandler(t, newMemSessions(), fb)
		h.opts.Referral.Enabled = false
		if err := start(h, "ref_99"); err != nil {
			t.Fatal(err)
		}
		if len(fb.attached) != 0 {
			t.Errorf("o'chirilgan tizimda bog'landi: %v", fb.attached)
		}
	})
}

// confirmCtx «Tasdiqlash» tugmasi bosilgan kontekst.
func confirmCtx(h *Handler, userID int64, unique string) tele.Context {
	user := &tele.User{ID: userID, Username: "vali"}
	chat := &tele.Chat{ID: userID, Type: tele.ChatPrivate}
	return h.bot.NewContext(tele.Update{Callback: &tele.Callback{
		ID: "cb", Sender: user, Unique: unique,
		Message: &tele.Message{ID: 50, Chat: chat, Text: "Xulosa"},
	}})
}

func TestConfirmWithBonusFree(t *testing.T) {
	const userID = 7
	fb := &refBilling{credits: 1}
	sessions := newMemSessions(paperSession(userID, StateConfirm))
	h, calls := refHandler(t, sessions, fb)

	if err := h.onConfirmBonus(confirmCtx(h, userID, cbConfirmBonus)); err != nil {
		t.Fatal(err)
	}
	h.wg.Wait()

	if len(fb.orders) != 1 {
		t.Fatalf("buyurtmalar: %d", len(fb.orders))
	}
	o := fb.orders[0]
	// 5 sahifa × 200 so'm = 1 000 so'm — bonus to'liq qoplaydi.
	if o.Price != 0 || o.Discount != 1000 || !o.IsPaid || o.Status != storage.OrderDelivered {
		t.Errorf("bonusli buyurtma: %+v", o)
	}
	if fb.credits != 0 {
		t.Errorf("bonus ayirilmadi: %d", fb.credits)
	}
	if st := sessions.state(userID); st != string(StateIdle) {
		t.Errorf("yakuniy holat %q, kutilgan idle", st)
	}
	if n := countMethod(calls(), "sendDocument"); n != 1 {
		t.Errorf("hujjat yuborilmadi (sendDocument=%d)", n)
	}
	// To'liq bepul buyurtma uchun taklif mukofoti berilmaydi (pul to'lanmagan).
	if len(fb.rewarded) != 0 {
		t.Errorf("bepul buyurtma uchun mukofot so'raldi: %v", fb.rewarded)
	}
}

func TestConfirmWithBonusPartial(t *testing.T) {
	const userID = 7
	fb := &refBilling{credits: 1}
	s := storage.Session{UserID: userID, ChatID: userID, State: string(StateConfirm),
		WorkType: string(ai.WorkPresentation), Design: generator.ThemeCustom, Tariff: config.TariffPremium,
		Topic: "Ipak yo'li", Volume: 20, StudentName: "Aliyev Vali", University: "TDU", TeacherName: "Karimova D."}
	sessions := newMemSessions(s)
	h, calls := refHandler(t, sessions, fb)
	h.opts.Payment.CardNumber = "8600123456789012"

	if err := h.onConfirmBonus(confirmCtx(h, userID, cbConfirmBonus)); err != nil {
		t.Fatal(err)
	}
	if len(fb.orders) != 1 {
		t.Fatalf("buyurtmalar: %d", len(fb.orders))
	}
	o := fb.orders[0]
	if o.Price != 19000 || o.Discount != 15000 || o.Status != storage.OrderPending {
		t.Errorf("qisman bonusli buyurtma: %+v", o)
	}
	if st := sessions.state(userID); st != string(StatePayment) {
		t.Errorf("holat %q, kutilgan await_payment", st)
	}
	if text := strings.Join(sentTexts(calls()), "\n"); !strings.Contains(text, "Summa:</b> 19 000 so'm") {
		t.Errorf("to'lov ko'rsatmasi:\n%s", text)
	}

	// Foydalanuvchi to'lovdan voz kechdi — bonus qaytadi.
	cancel := h.bot.NewContext(tele.Update{Message: &tele.Message{ID: 2,
		Sender: &tele.User{ID: userID}, Chat: &tele.Chat{ID: userID, Type: tele.ChatPrivate}, Text: "/cancel"}})
	if got := h.cancelFlow(cancel); got != msgCancelled {
		t.Errorf("/cancel javobi: %q", got)
	}
	if fb.credits != 1 || o.Status != storage.OrderCancelled {
		t.Errorf("bonus qaytmadi: credits=%d, holat=%s", fb.credits, o.Status)
	}
}

func TestConfirmWithBonusNoCredits(t *testing.T) {
	const userID = 7
	fb := &refBilling{credits: 0}
	sessions := newMemSessions(paperSession(userID, StateConfirm))
	h, calls := refHandler(t, sessions, fb)

	// Eskirgan «Bonus bilan» tugmasi: bonus qolmagan.
	if err := h.onConfirmBonus(confirmCtx(h, userID, cbConfirmBonus)); err != nil {
		t.Fatal(err)
	}
	if len(fb.orders) != 0 {
		t.Errorf("bonussiz buyurtma yaratildi: %+v", fb.orders)
	}
	if st := sessions.state(userID); st != string(StateConfirm) {
		t.Errorf("holat %q, kutilgan await_confirm", st)
	}
	if text := strings.Join(sentTexts(calls()), "\n"); !strings.Contains(text, "Hammasi to'g'rimi?") {
		t.Errorf("xulosa qayta yuborilmadi:\n%s", text)
	}
}

func TestStartReleasesAbandonedBonusOrder(t *testing.T) {
	const userID = 7
	fb := &refBilling{}
	_ = fb.CreateOrder(context.Background(), &storage.Order{UserID: 1, Price: 0, Discount: 1000, IsPaid: true, Status: storage.OrderPaid})
	s := paperSession(userID, StateConfirm) // generatsiya xato bilan tugagan
	s.OrderID = &fb.orders[0].ID
	h, _ := refHandler(t, newMemSessions(s), fb)

	c := h.bot.NewContext(tele.Update{Message: &tele.Message{ID: 1, Sender: &tele.User{ID: userID},
		Chat: &tele.Chat{ID: userID, Type: tele.ChatPrivate}, Text: "/start"}})
	if err := h.onStart(c); err != nil {
		t.Fatal(err)
	}
	if len(fb.released) != 1 || fb.credits != 1 || fb.orders[0].Status != storage.OrderCancelled {
		t.Errorf("tashlab ketilgan bonusli buyurtma: released=%v credits=%d holat=%s", fb.released, fb.credits, fb.orders[0].Status)
	}
}

func TestRewardReferrer(t *testing.T) {
	fb := &refBilling{reward: &storage.ReferralReward{ReferrerTelegramID: 99, Credits: 4}}
	h, calls := refHandler(t, newMemSessions(), fb)

	h.rewardReferrer(&storage.Order{ID: 1, UserID: 1, Price: 0, Discount: 3000}) // bepul — mukofot yo'q
	h.rewardReferrer(&storage.Order{ID: 2, UserID: 1, Price: 5000})
	if len(fb.rewarded) != 1 || fb.rewarded[0] != 2 {
		t.Fatalf("RewardReferral chaqiruvlari: %v", fb.rewarded)
	}
	if texts := callsTo(calls(), 99); len(texts) != 1 || !strings.Contains(texts[0], "<b>4 ta</b>") {
		t.Errorf("taklif qilganga xabar: %v", texts)
	}

	h.opts.Referral.MinPayment = 6000
	h.rewardReferrer(&storage.Order{ID: 3, UserID: 1, Price: 5000})
	if len(fb.rewarded) != 1 {
		t.Errorf("eng kam summadan arzon to'lov uchun mukofot so'raldi: %v", fb.rewarded)
	}
}

func TestClassifySendErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		res  sendResult
		wait time.Duration
	}{
		{"ok", nil, sendOK, 0},
		{"flood", tele.FloodError{RetryAfter: 3}, sendRetry, 3 * time.Second},
		{"flood 0", tele.FloodError{}, sendRetry, time.Second},
		{"429 retry_after siz", tele.NewError(429, "Too Many Requests"), sendRetry, time.Second},
		{"bloklagan", fmt.Errorf("telebot: %w", tele.ErrBlockedByUser), sendBlocked, 0},
		{"o'chirilgan akkaunt", tele.ErrUserIsDeactivated, sendBlocked, 0},
		{"chat yo'q", tele.ErrChatNotFound, sendBlocked, 0},
		{"boshqa 403", errors.New("telegram: Forbidden: bot can't send messages to bots (403)"), sendBlocked, 0},
		{"tarmoq", errors.New("dial tcp: timeout"), sendFailed, 0},
	}
	for _, c := range cases {
		res, wait := classifySendErr(c.err)
		if res != c.res || wait != c.wait {
			t.Errorf("%s: %v, %v; kutilgan %v, %v", c.name, res, wait, c.res, c.wait)
		}
	}
}

func TestBroadcastTexts(t *testing.T) {
	if got := broadcastETA(0); got != "1 daqiqadan kam" {
		t.Errorf("broadcastETA(0) = %q", got)
	}
	if got := broadcastETA(2400); got != "~2 daqiqa" { // 20 xabar/s → 120 s
		t.Errorf("broadcastETA(2400) = %q", got)
	}
	b := &storage.Broadcast{Total: 10, Sent: 9, Blocked: 2, Failed: 1}
	if got := broadcastCounters(b); !strings.Contains(got, "12 / 12 (100%)") {
		t.Errorf("yangi foydalanuvchilar qo'shilganda ham foiz 100 dan oshmasligi kerak: %s", got)
	}
	b.Status = storage.BroadcastCancelled
	if got := broadcastResultText(b, time.Minute); !strings.Contains(got, "to'xtatildi") {
		t.Errorf("to'xtatilgan e'lon hisoboti: %s", got)
	}
	last := &storage.Broadcast{Sent: 1200, Status: storage.BroadcastDone, CreatedAt: time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)}
	if got := broadcastConfirmText(1234, last); !strings.Contains(got, "1 234") || !strings.Contains(got, "1 200 ta yetkazilgan") {
		t.Errorf("tasdiqlash matni: %s", got)
	}
}

// Taklif tizimi yoqilganda ham qo'llanma va taklif matnlari Telegram
// chegarasiga (4096) sig'adi.
func TestReferralTextsFit(t *testing.T) {
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), refOptions(&refBilling{}))
	g := h.guideText()
	for _, want := range []string{"Do'stlarni taklif qilish", "/taklif", "15 000 so'm gacha"} {
		if !strings.Contains(g, want) {
			t.Errorf("qo'llanmada %q yo'q", want)
		}
	}
	link := referralLinkFor("a_very_long_presentation_maker_bot", 7132518114)
	for name, text := range map[string]string{
		"guide":        g,
		"referral":     h.referralText(link, 123456, 65432, 9999),
		"announcement": h.announcementText(link),
	} {
		if n := visibleLen(text); n > 4096 {
			t.Errorf("%s: %d birlik (chegara 4096)", name, n)
		}
	}
	if !strings.Contains(strings.Join(func() (out []string) {
		for _, c := range h.userCommands() {
			out = append(out, c.Text)
		}
		return out
	}(), ","), "taklif") {
		t.Error("buyruqlar menyusida /taklif yo'q")
	}
}
