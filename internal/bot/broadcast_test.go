package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/storage"
)

// memBroadcasts — xotiradagi e'lonlar ombori.
type memBroadcasts struct {
	mu         sync.Mutex
	list       []*storage.Broadcast
	recipients []int64 // sessiyalar (user_id = chat_id)
}

func (m *memBroadcasts) Start(_ context.Context, kind string, adminID int64, total int) (*storage.Broadcast, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.list {
		if b.Kind == kind && b.Status == storage.BroadcastRunning {
			return nil, storage.ErrConflict
		}
	}
	b := &storage.Broadcast{ID: uint(len(m.list) + 1), Kind: kind, Status: storage.BroadcastRunning, StartedBy: adminID, Total: total}
	m.list = append(m.list, b)
	c := *b
	return &c, nil
}

func (m *memBroadcasts) get(id uint) *storage.Broadcast { return m.list[id-1] }

func (m *memBroadcasts) SetMessage(_ context.Context, id uint, msgID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.get(id).MessageID = msgID
	return nil
}

func (m *memBroadcasts) SaveProgress(_ context.Context, b *storage.Broadcast) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.get(b.ID); s.Status == storage.BroadcastRunning {
		s.Sent, s.Blocked, s.Failed, s.LastUserID = b.Sent, b.Blocked, b.Failed, b.LastUserID
	}
	return nil
}

func (m *memBroadcasts) Finish(_ context.Context, b *storage.Broadcast, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.get(b.ID)
	s.Sent, s.Blocked, s.Failed, s.LastUserID, s.Status = b.Sent, b.Blocked, b.Failed, b.LastUserID, status
	b.Status = status
	return nil
}

func (m *memBroadcasts) Running(context.Context) ([]storage.Broadcast, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []storage.Broadcast
	for _, b := range m.list {
		if b.Status == storage.BroadcastRunning {
			out = append(out, *b)
		}
	}
	return out, nil
}

func (m *memBroadcasts) Last(context.Context, string) (*storage.Broadcast, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.list) == 0 {
		return nil, nil
	}
	c := *m.list[len(m.list)-1]
	return &c, nil
}

func (m *memBroadcasts) CountRecipients(context.Context) (int, error) { return len(m.recipients), nil }

func (m *memBroadcasts) Recipients(_ context.Context, after int64, limit int) ([]storage.Recipient, error) {
	ids := slices.Sorted(slices.Values(m.recipients))
	var out []storage.Recipient
	for _, id := range ids {
		if id > after && len(out) < limit {
			out = append(out, storage.Recipient{UserID: id, ChatID: id})
		}
	}
	return out, nil
}

// broadcastServer — soxta Telegram: respond har bir so'rovga javob tanlaydi
// (bo'sh satr — muvaffaqiyatli javob).
func broadcastServer(t *testing.T, respond func(method, chatID string) string) (*tele.Bot, func() []apiCall) {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []apiCall
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&params)
		method, chat := path.Base(r.URL.Path), fmt.Sprint(params["chat_id"])
		mu.Lock()
		calls = append(calls, apiCall{method: method, params: params, at: time.Now()})
		mu.Unlock()
		if body := respond(method, chat); body != "" {
			fmt.Fprint(w, body)
			return
		}
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":%s,"type":"private"}}}`, chat)
	}))
	t.Cleanup(srv.Close)
	b, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	b.Me.Username = "testbot"
	return b, func() []apiCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]apiCall(nil), calls...)
	}
}

func broadcastHandler(ctx context.Context, b *tele.Bot, store *memBroadcasts) *Handler {
	opts := refOptions(&refBilling{})
	opts.Broadcasts = store
	h := NewHandler(ctx, newMemSessions(), nil, Builders{}, zap.NewNop(), opts)
	h.bot = b
	return h
}

// sentToChats e'lon yuborilgan chatlar (takrorlar bilan).
func sentToChats(calls []apiCall, exclude string) []string {
	var out []string
	for _, c := range calls {
		if chat := fmt.Sprint(c.params["chat_id"]); c.method == "sendMessage" && chat != exclude {
			out = append(out, chat)
		}
	}
	return out
}

func TestRunBroadcast(t *testing.T) {
	var (
		mu      sync.Mutex
		flooded bool
	)
	b, calls := broadcastServer(t, func(method, chat string) string {
		if method != "sendMessage" {
			return ""
		}
		mu.Lock()
		defer mu.Unlock()
		switch chat {
		case "20": // birinchi urinishda 429 — kutib, qayta yuboriladi
			if !flooded {
				flooded = true
				return `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`
			}
		case "30":
			return `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
		case "50":
			return `{"ok":false,"error_code":400,"description":"Bad Request: something odd"}`
		}
		return ""
	})
	store := &memBroadcasts{recipients: []int64{40, 10, 30, 20, 50}}
	h := broadcastHandler(context.Background(), b, store)

	bc, err := store.Start(context.Background(), storage.BroadcastReferral, 777, 5)
	if err != nil {
		t.Fatal(err)
	}
	h.startBroadcast(bc)
	h.wg.Wait()

	got := store.get(bc.ID)
	if got.Status != storage.BroadcastDone || got.Sent != 3 || got.Blocked != 1 || got.Failed != 1 || got.LastUserID != 50 {
		t.Errorf("yakuniy holat: %+v", got)
	}
	all := calls()
	chats := sentToChats(all, "777")
	if want := []string{"10", "20", "20", "30", "40", "50"}; !slices.Equal(chats, want) {
		t.Errorf("yuborilgan chatlar = %v, kutilgan %v", chats, want)
	}
	// Har bir oluvchiga o'z havolasi.
	for _, c := range all {
		if chat := fmt.Sprint(c.params["chat_id"]); c.method == "sendMessage" && chat == "40" &&
			!strings.Contains(fmt.Sprint(c.params["text"]), "start=ref_40") {
			t.Errorf("40-oluvchiga o'z havolasi yuborilmadi: %v", c.params["text"])
		}
	}
	if report := callsTo(all, 777); len(report) != 1 || !strings.Contains(report[0], "e'loni yuborildi") {
		t.Errorf("adminga yakuniy hisobot: %v", report)
	}
	if _, ok := h.bcRuns.Load(bc.ID); ok {
		t.Error("tugagan e'lon ro'yxatdan o'chirilmadi")
	}
}

func TestStopBroadcast(t *testing.T) {
	arrived, gate := make(chan struct{}), make(chan struct{})
	b, _ := broadcastServer(t, func(method, chat string) string {
		if method == "sendMessage" && chat == "20" {
			close(arrived)
			<-gate
		}
		return ""
	})
	store := &memBroadcasts{recipients: []int64{10, 20, 30, 40}}
	h := broadcastHandler(context.Background(), b, store)
	bc, _ := store.Start(context.Background(), storage.BroadcastReferral, 777, 4)
	h.startBroadcast(bc)

	<-arrived // 20-oluvchiga yuborilmoqda
	v, ok := h.bcRuns.Load(bc.ID)
	if !ok {
		t.Fatal("e'lon ro'yxatda yo'q")
	}
	run := v.(*broadcastRun)
	run.stopped.Store(true)
	run.cancel()
	close(gate)
	h.wg.Wait()

	got := store.get(bc.ID)
	if got.Status != storage.BroadcastCancelled || got.Sent != 2 || got.LastUserID != 20 {
		t.Errorf("to'xtatilgan e'lon: %+v", got)
	}
}

func TestBroadcastShutdownAndResume(t *testing.T) {
	arrived, gate := make(chan struct{}), make(chan struct{})
	var once sync.Once
	b, calls := broadcastServer(t, func(method, chat string) string {
		if method == "sendMessage" && chat == "20" {
			once.Do(func() { close(arrived); <-gate })
		}
		return ""
	})
	store := &memBroadcasts{recipients: []int64{10, 20, 30, 40}}

	// 1-ishga tushirish: 20-oluvchiga yuborilayotganda bot to'xtatiladi
	// (main.go: b.Stop() dan keyin PauseBroadcasts).
	h := broadcastHandler(context.Background(), b, store)
	bc, _ := store.Start(context.Background(), storage.BroadcastReferral, 777, 4)
	h.startBroadcast(bc)
	<-arrived
	h.PauseBroadcasts()
	close(gate)
	h.wg.Wait()

	got := *store.get(bc.ID)
	if got.Status != storage.BroadcastRunning || got.Sent != 2 || got.LastUserID != 20 {
		t.Fatalf("to'xtatishda jarayon saqlanishi va e'lon running qolishi kerak: %+v", got)
	}

	// 2-ishga tushirish: e'lon to'xtagan joyidan davom etadi.
	h2 := broadcastHandler(context.Background(), b, store)
	h2.resumeBroadcasts(context.Background())
	h2.wg.Wait()

	got = *store.get(bc.ID)
	if got.Status != storage.BroadcastDone || got.Sent != 4 || got.LastUserID != 40 {
		t.Errorf("davom ettirilgan e'lon: %+v", got)
	}
	if chats := sentToChats(calls(), "777"); !slices.Equal(chats, []string{"10", "20", "30", "40"}) {
		t.Errorf("har bir oluvchiga bir martadan yuborilishi kerak: %v", chats)
	}
	admin := strings.Join(callsTo(calls(), 777), "\n")
	if !strings.Contains(admin, "davom ettirilmoqda (2 / 4)") || !strings.Contains(admin, "e'loni yuborildi") {
		t.Errorf("adminga xabarlar:\n%s", admin)
	}
}
