package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"

	"telegram-presentation-bot/internal/storage"
)

// adminBilling — faqat adminlar ro'yxatini qaytaradigan ombor.
type adminBilling struct {
	BillingStore
	admins []storage.User
}

func (a adminBilling) ListAdmins(context.Context) ([]storage.User, error) { return a.admins, nil }

type apiCall struct {
	method string
	params map[string]any
}

// fakeTelegram — Bot API so'rovlarini yozib boradigan soxta server.
// failCopyFor — shu chatga copyMessage xato qaytaradi.
func fakeTelegram(t *testing.T, failCopyFor string) (*tele.Bot, func() []apiCall) {
	var (
		mu    sync.Mutex
		calls []apiCall
		next  = 100
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string]any{}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			// Fayl yuborish (sendDocument va h.k.) multipart shaklida keladi.
			if err := r.ParseMultipartForm(32 << 20); err == nil {
				for k, v := range r.MultipartForm.Value {
					if len(v) > 0 {
						params[k] = v[0]
					}
				}
			}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&params)
		}
		method := path.Base(r.URL.Path)
		mu.Lock()
		calls = append(calls, apiCall{method, params})
		next++
		id := next
		mu.Unlock()
		if method == "copyMessage" && fmt.Sprint(params["chat_id"]) == failCopyFor {
			fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message to copy not found"}`)
			return
		}
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":%v,"type":"private"}}}`, id, params["chat_id"])
	}))
	t.Cleanup(srv.Close)
	b, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	return b, func() []apiCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]apiCall(nil), calls...)
	}
}

func TestPurchaseToAdminsCopiesFile(t *testing.T) {
	b, calls := fakeTelegram(t, "22")
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{
		Billing: adminBilling{admins: []storage.User{{TelegramUserID: 11}, {TelegramUserID: 22}}},
	})
	h.bot = b

	file := &tele.StoredMessage{MessageID: "555", ChatID: 7132518114}
	if sent := h.purchaseToAdmins("🛒 Xarid", file, zap.NewNop()); sent != 2 {
		t.Fatalf("xabar ikkala adminga yetishi kerak (fayl nusxalanmasa ham), sent=%d", sent)
	}

	got := calls()
	if len(got) != 4 {
		t.Fatalf("4 ta so'rov kutilgan (2× sendMessage + 2× copyMessage), olindi %d: %+v", len(got), got)
	}
	for i, chat := range []string{"11", "22"} {
		send, cp := got[2*i], got[2*i+1]
		if send.method != "sendMessage" || fmt.Sprint(send.params["chat_id"]) != chat || send.params["text"] != "🛒 Xarid" {
			t.Errorf("%s: avval xarid xabari yuborilishi kerak: %+v", chat, send)
		}
		if cp.method != "copyMessage" {
			t.Fatalf("%s: keyin copyMessage kutilgan: %+v", chat, cp)
		}
		for k, want := range map[string]string{
			"chat_id": chat, "from_chat_id": "7132518114", "message_id": "555",
			"reply_to_message_id": fmt.Sprint(101 + 2*i), "disable_notification": "true",
		} {
			if v := fmt.Sprint(cp.params[k]); v != want {
				t.Errorf("%s: copyMessage %s=%s, kutilgan %s", chat, k, v, want)
			}
		}
	}
}

func TestPurchaseToAdminsWithoutFile(t *testing.T) {
	b, calls := fakeTelegram(t, "")
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{
		Billing: adminBilling{admins: []storage.User{{TelegramUserID: 11}}},
	})
	h.bot = b
	if sent := h.purchaseToAdmins("🛒 Xarid", nil, zap.NewNop()); sent != 1 {
		t.Fatalf("sent=%d", sent)
	}
	if got := calls(); len(got) != 1 || got[0].method != "sendMessage" {
		t.Errorf("fayl bo'lmasa faqat xabar yuboriladi: %+v", got)
	}
}
