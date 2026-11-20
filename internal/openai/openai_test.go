package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"telegram-presentation-bot/internal/httpx"
)

// server har bir so'rovga status va tana bilan javob beradigan test serverini
// ishga tushiradi va unga ulangan klientni qaytaradi.
func server(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "key", "test")
}

// chatBody bitta variantli Chat Completions javobini yasaydi.
func chatBody(content, refusal, finish string) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"message":       map[string]string{"content": content, "refusal": refusal},
		"finish_reason": finish,
	}}})
	return string(b)
}

func TestNewTrimsSlash(t *testing.T) {
	if c := New("https://api.example.com/v1/", "k", "x"); c.baseURL != "https://api.example.com/v1" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
}

func TestPostJSONRequest(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/things" {
			t.Errorf("so'rov: %s %s", r.Method, r.URL.Path)
		}
		for h, want := range map[string]string{
			"Authorization": "Bearer secret",
			"Content-Type":  "application/json",
			"Accept":        "application/json",
		} {
			if r.Header.Get(h) != want {
				t.Errorf("%s = %q, kutilgan %q", h, r.Header.Get(h), want)
			}
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, "\n\n{\"value\":42}") // bo'sh qatorlar (DeepSeek) — o'tkazib yuboriladi
	}))
	defer srv.Close()

	var out struct {
		Value int `json:"value"`
	}
	c := New(srv.URL+"/v1/", "secret", "test")
	if err := c.PostJSON(context.Background(), "/things", map[string]any{"a": "b"}, &out, 0); err != nil {
		t.Fatal(err)
	}
	if out.Value != 42 || got["a"] != "b" {
		t.Errorf("out = %+v, payload = %v", out, got)
	}
	// out nil — javob tanasi tashlab yuboriladi.
	if err := c.PostJSON(context.Background(), "/things", nil, nil, 0); err != nil {
		t.Errorf("out nil: %v", err)
	}
}

func TestPostJSONStatusMapping(t *testing.T) {
	cases := []struct {
		status    int
		permanent bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusUnauthorized, true},
		{http.StatusPaymentRequired, true},
		{http.StatusNotFound, true},
		{http.StatusRequestTimeout, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusServiceUnavailable, false},
	}
	for _, tc := range cases {
		c := server(t, tc.status, `{"error":{"message":"xabar","type":"x"}}`)
		err := c.PostJSON(context.Background(), "/x", map[string]any{}, nil, 0)
		if err == nil {
			t.Errorf("%d: xato kutilgan", tc.status)
			continue
		}
		if want := fmt.Sprintf("test API xatosi %d: xabar", tc.status); err.Error() != want {
			t.Errorf("%d: xato matni = %q", tc.status, err)
		}
		if httpx.IsPermanent(err) != tc.permanent {
			t.Errorf("%d: permanent = %v, kutilgan %v", tc.status, httpx.IsPermanent(err), tc.permanent)
		}
	}
}

func TestPostJSONErrorMessage(t *testing.T) {
	// {"error":{"message"}} bo'lsa — xabar shundan olinadi.
	err := server(t, http.StatusUnauthorized, `{"error":{"message":"Incorrect API key"}}`).
		PostJSON(context.Background(), "/x", nil, nil, 0)
	if err == nil || err.Error() != "test API xatosi 401: Incorrect API key" {
		t.Errorf("xato = %v", err)
	}

	// JSON bo'lmagan tana — qisqartirilgan holda xatoga qo'shiladi (UTF-8 buzilmaydi).
	long := strings.Repeat("ш", 600)
	err = server(t, http.StatusBadGateway, "  "+long+"  ").PostJSON(context.Background(), "/x", nil, nil, 0)
	if err == nil {
		t.Fatal("xato kutilgan")
	}
	want := "test API xatosi 502: " + strings.Repeat("ш", maxErrorRunes) + "…"
	if err.Error() != want {
		t.Errorf("qisqartirilgan xato noto'g'ri: %d belgi", len([]rune(err.Error())))
	}

	// error.message bo'sh — tana ishlatiladi.
	err = server(t, http.StatusInternalServerError, `{"error":{"message":""}}`).
		PostJSON(context.Background(), "/x", nil, nil, 0)
	if err == nil || !strings.Contains(err.Error(), `{"error":{"message":""}}`) {
		t.Errorf("bo'sh message'da tana kutilgan: %v", err)
	}
}

func TestPostJSONBodyLimit(t *testing.T) {
	body := `{"data":"` + strings.Repeat("a", 100) + `"}`
	var out map[string]any

	// Chegaradan oshgan 200 javob — doimiy xato.
	err := server(t, http.StatusOK, body).PostJSON(context.Background(), "/x", nil, &out, 50)
	if err == nil || !httpx.IsPermanent(err) || !strings.Contains(err.Error(), "juda katta") {
		t.Errorf("katta javob doimiy xato bo'lishi kerak: %v", err)
	}

	// Aynan chegaradagi javob — qabul qilinadi.
	if err := server(t, http.StatusOK, body).PostJSON(context.Background(), "/x", nil, &out, int64(len(body))); err != nil {
		t.Errorf("chegaradagi javob qabul qilinishi kerak: %v", err)
	}

	// Chegaradan oshgan xato javobi — status bo'yicha tasniflanadi (503 — vaqtinchalik).
	err = server(t, http.StatusServiceUnavailable, strings.Repeat("x", 200)).PostJSON(context.Background(), "/x", nil, nil, 50)
	if err == nil || httpx.IsPermanent(err) || !strings.Contains(err.Error(), "API xatosi 503") {
		t.Errorf("katta 503 javob vaqtinchalik status xatosi bo'lishi kerak: %v", err)
	}
}

func TestPostJSONInvalidJSON(t *testing.T) {
	var out map[string]any
	err := server(t, http.StatusOK, "<html>").PostJSON(context.Background(), "/x", nil, &out, 0)
	if err == nil || httpx.IsPermanent(err) || !strings.Contains(err.Error(), "JSON emas") {
		t.Errorf("buzilgan JSON vaqtinchalik xato bo'lishi kerak: %v", err)
	}
}

func TestPostJSONMarshalError(t *testing.T) {
	c := New("http://127.0.0.1:1", "k", "test")
	err := c.PostJSON(context.Background(), "/x", map[string]any{"f": func() {}}, nil, 0)
	if err == nil || !httpx.IsPermanent(err) {
		t.Errorf("marshal xatosi doimiy bo'lishi kerak: %v", err)
	}
}

func TestChatSuccess(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, chatBody(`{"ok":true}`, "", "stop"))
	}))
	defer srv.Close()

	text, err := New(srv.URL+"/v1", "k", "test").Chat(context.Background(), map[string]any{
		"model":    "m",
		"messages": []Message{{Role: "user", Content: "salom"}},
	})
	if err != nil || text != `{"ok":true}` {
		t.Fatalf("Chat = %q, %v", text, err)
	}
	if got["model"] != "m" {
		t.Errorf("payload = %v", got)
	}
}

func TestChatResponseClassification(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		permanent bool
		contains  string
	}{
		{"refusal", chatBody("", "I can't help with that", "stop"), true, "rad etdi: I can't help with that"},
		{"length", chatBody(`{"a":`, "", "length"), false, "uzunlik chegarasida qirqildi (AI_MAX_TOKENS ni oshiring)"},
		{"content_filter", chatBody("", "", "content_filter"), true, "kontent filtri"},
		{"insufficient_system_resource", chatBody("", "", "insufficient_system_resource"), false, "resurs yetishmadi"},
		{"bo'sh javob", chatBody("  \n ", "", "stop"), false, "bo'sh javob"},
		{"choices yo'q", `{"choices":[]}`, false, "choices bo'sh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := server(t, http.StatusOK, tc.body).Chat(context.Background(), map[string]any{})
			if err == nil {
				t.Fatal("xato kutilgan")
			}
			if !strings.HasPrefix(err.Error(), "test ") || !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("xato matni = %q, %q bo'lishi kerak", err, tc.contains)
			}
			if httpx.IsPermanent(err) != tc.permanent {
				t.Errorf("permanent = %v, kutilgan %v", httpx.IsPermanent(err), tc.permanent)
			}
		})
	}
}

func TestChatStatusError(t *testing.T) {
	_, err := server(t, http.StatusTooManyRequests, `{"error":{"message":"Rate limit"}}`).Chat(context.Background(), map[string]any{})
	if err == nil || httpx.IsPermanent(err) || err.Error() != "test API xatosi 429: Rate limit" {
		t.Errorf("429 vaqtinchalik xato bo'lishi kerak: %v", err)
	}
}

func TestMultimodalParts(t *testing.T) {
	msg := Message{Role: "user", Content: []Part{
		TextPart("o'qing"),
		ImagePart("image/png", []byte{1, 2, 3}, "high"),
	}}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"role":"user","content":[{"type":"text","text":"o'qing"},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,AQID","detail":"high"}}]}`
	if string(b) != want {
		t.Errorf("JSON:\n%s\nkutilgan:\n%s", b, want)
	}
	if got := DataURI("image/jpeg", []byte("x")); got != "data:image/jpeg;base64,eA==" {
		t.Errorf("DataURI = %q", got)
	}
}
