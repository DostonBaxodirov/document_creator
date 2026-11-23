package bot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
	"gorm.io/gorm"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/storage"
)

// memSessions — xotiradagi sessiyalar ombori.
type memSessions struct {
	mu   sync.Mutex
	rows map[int64]storage.Session
}

func newMemSessions(rows ...storage.Session) *memSessions {
	m := &memSessions{rows: map[int64]storage.Session{}}
	for _, s := range rows {
		m.rows[s.UserID] = s
	}
	return m
}

func (m *memSessions) GetOrCreate(_ context.Context, userID, chatID int64, username string) (*storage.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[userID]
	if !ok {
		s = storage.Session{UserID: userID, ChatID: chatID, Username: username, State: string(StateIdle)}
		m.rows[userID] = s
	}
	return &s, nil
}

func (m *memSessions) Get(_ context.Context, userID int64) (*storage.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[userID]
	if !ok {
		// Haqiqiy ombordagi kabi: topilmasa gorm.ErrRecordNotFound o'ralgan xato.
		return nil, fmt.Errorf("sessiya topilmadi: %w", gorm.ErrRecordNotFound)
	}
	return &s, nil
}

func (m *memSessions) Save(_ context.Context, s *storage.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[s.UserID] = *s
	return nil
}

func (m *memSessions) CompareAndSwapState(_ context.Context, userID int64, from, to string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[userID]
	if !ok || s.State != from {
		return false, nil
	}
	s.State = to
	m.rows[userID] = s
	return true, nil
}

func (m *memSessions) ListByState(_ context.Context, state string) ([]storage.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []storage.Session
	for _, s := range m.rows {
		if s.State == state {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memSessions) state(userID int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rows[userID].State
}

// stubPapers — haqiqiy DOCX o'rniga kichik fayl yozadigan tez builder.
type stubPapers struct{ dir string }

func (p stubPapers) Build(_ context.Context, paper generator.Paper) (string, error) {
	f, err := os.CreateTemp(p.dir, "paper-*.docx")
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.WriteString(paper.Topic)
	return f.Name(), err
}

func (stubPapers) ContentWords(pages int) int        { return pages * 250 }
func (stubPapers) FigureWords() int                  { return 110 }
func (stubPapers) EstimatePages(generator.Paper) int { return 1 }

// mustBeEmpty — vazifa tugagach vaqtinchalik fayllar o'chirilgan bo'lishi kerak.
func (p stubPapers) mustBeEmpty(t *testing.T, name string) {
	t.Helper()
	if left, _ := filepath.Glob(filepath.Join(p.dir, "*")); len(left) != 0 {
		t.Errorf("%s: vaqtinchalik fayllar o'chirilmagan: %v", name, left)
	}
}

// slowDocAI — matnni berilgan vaqtda "yozadigan" AI.
type slowDocAI struct {
	*fakeDocAI
	delay time.Duration
}

func (a slowDocAI) GenerateDocument(ctx context.Context, req ai.DocumentRequest) (*ai.Document, error) {
	select {
	case <-time.After(a.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return a.fakeDocAI.GenerateDocument(ctx, req)
}

// gateDocAI — test gate kanalini yopmaguncha javob bermaydigan AI.
type gateDocAI struct {
	*fakeDocAI
	gate chan struct{}
}

func (a gateDocAI) GenerateDocument(ctx context.Context, req ai.DocumentRequest) (*ai.Document, error) {
	<-a.gate
	return a.fakeDocAI.GenerateDocument(ctx, req)
}

// blockingBilling — FindCredit release yopilguncha to'xtab turadigan ombor
// (onConfirm to'lovni tekshirayotgan payt). Bepul urinishlar o'chirilgani
// uchun generatsiya darhol boshlanishi uchun FindCredit oldin to'langan
// (yetkazilmagan) buyurtmani qaytaradi.
type blockingBilling struct {
	BillingStore
	entered chan struct{}
	release chan struct{}

	mu     sync.Mutex
	orders []*storage.Order
}

func (b *blockingBilling) EnsureUser(_ context.Context, telegramID int64, username, _ string) (*storage.User, error) {
	return &storage.User{ID: 1, TelegramUserID: telegramID, Username: username}, nil
}

func (b *blockingBilling) FindCredit(_ context.Context, userID uint, kind string, price int) (*storage.Order, error) {
	close(b.entered)
	<-b.release
	b.mu.Lock()
	defer b.mu.Unlock()
	o := &storage.Order{ID: uint(len(b.orders) + 1), UserID: userID, Kind: kind, Price: price,
		IsPaid: true, Status: storage.OrderPaid}
	b.orders = append(b.orders, o)
	return o, nil
}

func (b *blockingBilling) SaveOrder(context.Context, *storage.Order) error { return nil }

func (b *blockingBilling) CreateOrder(_ context.Context, o *storage.Order) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	o.ID = uint(len(b.orders) + 1)
	b.orders = append(b.orders, o)
	return nil
}

func (b *blockingBilling) GetOrder(_ context.Context, id uint) (*storage.Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id == 0 || int(id) > len(b.orders) {
		return nil, errors.New("buyurtma topilmadi")
	}
	return b.orders[id-1], nil
}

func (b *blockingBilling) SetOrderStatus(context.Context, uint, string, string) (bool, error) {
	return true, nil
}

// ReleaseOrder: oldin to'langan buyurtma bekor qilinmaydi (haqiqiy ombordagi kabi).
func (b *blockingBilling) ReleaseOrder(context.Context, uint) (bool, error) { return false, nil }

func (b *blockingBilling) OrderPayment(context.Context, uint) (*storage.Payment, error) {
	return nil, nil
}

func (b *blockingBilling) ListAdmins(context.Context) ([]storage.User, error) { return nil, nil }

// sentTexts — foydalanuvchiga yuborilgan sendMessage matnlari.
func sentTexts(calls []apiCall) []string {
	var out []string
	for _, c := range calls {
		if c.method == "sendMessage" {
			out = append(out, fmt.Sprint(c.params["text"]))
		}
	}
	return out
}

func countMethod(calls []apiCall, method string) int {
	n := 0
	for _, c := range calls {
		if c.method == method {
			n++
		}
	}
	return n
}

func paperSession(userID int64, state State) storage.Session {
	return storage.Session{
		UserID: userID, ChatID: userID, State: string(state),
		WorkType: string(ai.WorkIndependent), Topic: "Ipak yo'li tarixi", Volume: 5,
		StudentName: "Aliyev Vali", University: "TDU", TeacherName: "Karimova D.",
	}
}

// TestJobTimeoutExcludesQueueWait — JobTimeout navbatda kutilgan vaqtni
// hisobga olmaydi: navbatda 300 ms turgan vazifa 200 ms limit bilan ham
// (generatsiyaning o'zi 50 ms) muvaffaqiyatli tugaydi.
func TestJobTimeoutExcludesQueueWait(t *testing.T) {
	const userID = 7
	b, calls := fakeTelegram(t, "")
	sessions := newMemSessions(paperSession(userID, StateGenerating))
	papers := stubPapers{dir: t.TempDir()}
	h := NewHandler(context.Background(), sessions, slowDocAI{fakeDocAI: &fakeDocAI{}, delay: 50 * time.Millisecond},
		Builders{Papers: papers}, zap.NewNop(), Options{MaxConcurrentJobs: 1, JobTimeout: 200 * time.Millisecond})
	h.bot = b

	// Birinchi vazifa yagona joyni egallab turibdi.
	h.slots <- struct{}{}

	s, err := sessions.Get(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	unlock := h.lock(userID)
	h.startJob(s)
	unlock()

	// Ikkinchi vazifa navbatga tushganini kutamiz.
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(strings.Join(sentTexts(calls()), "\n"), msgQueued) {
		if time.Now().After(deadline) {
			t.Fatalf("navbat xabari yuborilmadi: %v", sentTexts(calls()))
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Birinchi vazifa ~300 ms dan keyin joyni bo'shatadi.
	time.Sleep(300*time.Millisecond - time.Since(started))
	<-h.slots

	h.wg.Wait()
	took := time.Since(started)

	got := calls()
	if n := countMethod(got, "sendDocument"); n != 1 {
		t.Fatalf("hujjat yuborilishi kerak edi (sendDocument=%d), xabarlar: %v", n, sentTexts(got))
	}
	for _, text := range sentTexts(got) {
		if strings.Contains(text, "⌛") || strings.Contains(text, "😔") {
			t.Errorf("navbatda kutish timeout'ga sabab bo'lmasligi kerak: %q", text)
		}
	}
	if st := sessions.state(userID); st != string(StateIdle) {
		t.Errorf("yakuniy holat %q, kutilgan %q", st, StateIdle)
	}
	if took < 300*time.Millisecond {
		t.Errorf("vazifa navbatda kamida 300 ms kutishi kerak edi, jami %v", took)
	}
	if _, ok := h.running.Load(int64(userID)); ok {
		t.Error("tugagan vazifa ro'yxatdan o'chirilishi kerak")
	}
	papers.mustBeEmpty(t, "muvaffaqiyatli vazifa")
}

// TestCancelDuringConfirmStopsJob — onConfirm to'lovni tekshirayotgan payt
// (FindCredit to'xtab turibdi) kelgan /cancel vazifani albatta to'xtatadi:
// yakuniy holat "idle", hujjat yuborilmaydi.
func TestCancelDuringConfirmStopsJob(t *testing.T) {
	const userID = 7
	b, calls := fakeTelegram(t, "")
	sessions := newMemSessions(paperSession(userID, StateConfirm))
	papers := stubPapers{dir: t.TempDir()}
	billing := &blockingBilling{entered: make(chan struct{}), release: make(chan struct{})}
	gen := gateDocAI{fakeDocAI: &fakeDocAI{}, gate: make(chan struct{})}
	h := NewHandler(context.Background(), sessions, gen, Builders{Papers: papers}, zap.NewNop(), Options{
		MaxConcurrentJobs: 1,
		JobTimeout:        5 * time.Second,
		Billing:           billing,
		Payment:           config.PaymentConfig{Enabled: true, PricePage: 200, PriceDocument: 7000},
	})
	h.bot = b

	user := &tele.User{ID: userID, Username: "vali"}
	chat := &tele.Chat{ID: userID, Type: tele.ChatPrivate}
	confirmCtx := b.NewContext(tele.Update{Callback: &tele.Callback{
		ID: "cb1", Sender: user, Data: cbConfirm,
		Message: &tele.Message{ID: 50, Chat: chat, Text: "Xulosa"},
	}})
	cancelCtx := b.NewContext(tele.Update{Message: &tele.Message{ID: 51, Sender: user, Chat: chat, Text: "/cancel"}})

	confirmDone := make(chan error, 1)
	go func() { confirmDone <- h.onConfirm(confirmCtx) }()
	<-billing.entered // onConfirm sessiyani "generating" ga o'tkazdi va FindCredit da turibdi

	cancelDone := make(chan string, 1)
	go func() { cancelDone <- h.cancelFlow(cancelCtx) }()

	// /cancel qulfda kutishi kerak. Qulf bo'lmasa, u shu yerda tugab qoladi —
	// bu ham xato emas: natija baribir quyida tekshiriladi.
	var cancelText string
	select {
	case cancelText = <-cancelDone:
	case <-time.After(100 * time.Millisecond):
	}
	close(billing.release)

	if err := <-confirmDone; err != nil {
		t.Fatalf("onConfirm: %v", err)
	}
	if cancelText == "" {
		cancelText = <-cancelDone
	}
	close(gen.gate) // AI javob qaytaradi (vazifa bekor qilinmagan bo'lsa — hujjat yuboriladi)
	h.wg.Wait()

	if cancelText != msgStopping {
		t.Errorf("/cancel ishlayotgan vazifani topishi kerak edi: %q", cancelText)
	}
	if st := sessions.state(userID); st != string(StateIdle) {
		t.Errorf("yakuniy holat %q, kutilgan %q", st, StateIdle)
	}
	got := calls()
	if n := countMethod(got, "sendDocument"); n != 0 {
		t.Errorf("bekor qilingan vazifa hujjat yubormasligi kerak (sendDocument=%d)", n)
	}
	if texts := sentTexts(got); !strings.Contains(strings.Join(texts, "\n"), msgJobCancelled) {
		t.Errorf("foydalanuvchiga bekor qilingani haqida xabar yuborilishi kerak: %v", texts)
	}
	if _, ok := h.running.Load(int64(userID)); ok {
		t.Error("tugagan vazifa ro'yxatdan o'chirilishi kerak")
	}
	papers.mustBeEmpty(t, "bekor qilingan vazifa")
}
