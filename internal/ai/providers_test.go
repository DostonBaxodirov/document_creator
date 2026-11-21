package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/design"
	"telegram-presentation-bot/internal/httpx"
)

// chatServer — OpenAI-mos soxta server: so'rov tanalarini (manzili va
// Authorization sarlavhasi bilan) yozib boradi va respond qaytargan
// status/tanani yuboradi.
type chatServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []map[string]any
	raws   []string
	paths  []string
	auths  []string
}

const chatOK = `{"choices":[{"message":{"content":"{\"paragraphs\":[\"Matn\"]}"},"finish_reason":"stop"}]}`

func newChatServer(t *testing.T, respond func(body map[string]any) (int, string)) *chatServer {
	t.Helper()
	s := &chatServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Errorf("so'rov JSON emas: %v", err)
		}
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.raws = append(s.raws, string(data))
		s.paths = append(s.paths, r.URL.Path)
		s.auths = append(s.auths, r.Header.Get("Authorization"))
		s.mu.Unlock()
		code, resp := respond(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *chatServer) requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.bodies...)
}

// formatType so'rovdagi response_format.type.
func formatType(body map[string]any) string {
	f, _ := body["response_format"].(map[string]any)
	typ, _ := f["type"].(string)
	return typ
}

func testOpenAIConfig(url string, structured bool) config.AIConfig {
	return config.AIConfig{OpenAIBaseURL: url, OpenAIAPIKey: "k", OpenAIModel: "gpt-test", OpenAIStructured: structured}
}

func sectionTask() task {
	return task{system: "Respond with JSON.", user: "Write JSON.", schema: sectionSchema(), op: "document_chapter"}
}

// TestOpenAIStructuredPayload — OpenAI so'rovida strict json_schema yuboriladi:
// nomi — op, sxema esa tartibi saqlangan holda.
func TestOpenAIStructuredPayload(t *testing.T) {
	srv := newChatServer(t, func(map[string]any) (int, string) { return http.StatusOK, chatOK })
	p := newOpenAI(testOpenAIConfig(srv.URL, true), nil)
	c := &Client{p: p, timeout: time.Minute, parallel: 1, log: zap.NewNop()}

	// generateJSON op ni task'ga yozadi — json_schema nomi shundan olinadi.
	_, _ = c.GenerateDocDesign(context.Background(), DesignRequest{Topic: "Tarix"})

	reqs := srv.requests()
	if len(reqs) == 0 {
		t.Fatal("so'rov yuborilmadi")
	}
	format, _ := reqs[0]["response_format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("response_format = %v", format)
	}
	js, _ := format["json_schema"].(map[string]any)
	if js["strict"] != true || js["name"] != "doc_design" {
		t.Errorf("json_schema strict/name noto'g'ri: strict=%v name=%v", js["strict"], js["name"])
	}
	schema, _ := js["schema"].(map[string]any)
	if schema["type"] != "object" || schema["additionalProperties"] != false || len(schemaProps(schema)) != 9 {
		t.Errorf("sxema noto'g'ri: %v", schema)
	}
	if raw := srv.raws[0]; !strings.Contains(raw, `"properties":{"name":{"type":"string"},"primary":{"type":"string"},"accent":`) {
		t.Errorf("sxemadagi xossalar tartibi saqlanmadi:\n%s", raw)
	}
	if !strings.Contains(reqs[0]["messages"].([]any)[0].(map[string]any)["content"].(string), "JSON") {
		t.Error(`json_object rejimi uchun promptda "JSON" so'zi qolishi kerak`)
	}
}

// TestOpenAIStructuredFallback — API json_schema'ni rad etsa (400,
// response_format haqida), provayder json_object ga o'tadi, so'rovni darhol
// qayta yuboradi va keyingi so'rovlar ham json_object bilan ketadi.
func TestOpenAIStructuredFallback(t *testing.T) {
	srv := newChatServer(t, func(body map[string]any) (int, string) {
		if formatType(body) == "json_schema" {
			return http.StatusBadRequest, `{"error":{"message":"Invalid parameter: 'response_format' of type 'json_schema' is not supported with this model."}}`
		}
		return http.StatusOK, chatOK
	})
	core, logs := observer.New(zapcore.WarnLevel)
	p := newOpenAI(testOpenAIConfig(srv.URL, true), zap.New(core))

	for i := range 2 {
		raw, err := p.complete(context.Background(), sectionTask())
		if err != nil || !strings.Contains(raw, "paragraphs") {
			t.Fatalf("%d-chaqiruv: %q, %v", i+1, raw, err)
		}
	}

	var types []string
	for _, b := range srv.requests() {
		types = append(types, formatType(b))
	}
	if got := strings.Join(types, ","); got != "json_schema,json_object,json_object" {
		t.Errorf("so'rovlar formati = %s", got)
	}
	if p.structured.Load() {
		t.Error("fallback'dan keyin structured o'chishi kerak")
	}
	if n := logs.FilterMessage("structured outputs not supported, falling back to json_object").Len(); n != 1 || logs.Len() != 1 {
		t.Errorf("bitta Warn log kutilgan edi: %d (%d jami)", n, logs.Len())
	}
}

// TestOpenAIBadRequestNoFallback — response_format'ga aloqasi yo'q 400 xato
// fallback'ni ishga tushirmaydi va doimiy xato sifatida qaytadi.
func TestOpenAIBadRequestNoFallback(t *testing.T) {
	srv := newChatServer(t, func(map[string]any) (int, string) {
		return http.StatusBadRequest, `{"error":{"message":"The model 'gpt-test' does not exist"}}`
	})
	p := newOpenAI(testOpenAIConfig(srv.URL, true), nil)
	_, err := p.complete(context.Background(), sectionTask())
	if err == nil || !httpx.IsPermanent(err) {
		t.Fatalf("doimiy xato kutilgan edi: %v", err)
	}
	if n := len(srv.requests()); n != 1 || !p.structured.Load() {
		t.Errorf("qayta yuborilmasligi va structured yoqiq qolishi kerak: %d so'rov, structured=%v", n, p.structured.Load())
	}
}

// TestJSONObjectProviders — AI_OPENAI_STRUCTURED=false bo'lsa va DeepSeek'da
// javob formati json_object.
func TestJSONObjectProviders(t *testing.T) {
	srv := newChatServer(t, func(map[string]any) (int, string) { return http.StatusOK, chatOK })
	providers := map[string]*compatProvider{
		"openai (structured=false)": newOpenAI(testOpenAIConfig(srv.URL, false), nil),
		"deepseek": newDeepSeek(config.AIConfig{
			DeepSeekBaseURL: srv.URL, DeepSeekAPIKey: "k", DeepSeekModel: "deepseek-test",
			OpenAIStructured: true, // DeepSeek'ga ta'sir qilmaydi
		}),
	}
	for name, p := range providers {
		before := len(srv.requests())
		if _, err := p.complete(context.Background(), sectionTask()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		reqs := srv.requests()
		if len(reqs) != before+1 || formatType(reqs[before]) != "json_object" {
			t.Errorf("%s: json_object kutilgan edi: %v", name, reqs[len(reqs)-1]["response_format"])
		}
	}
}

// TestGeminiCompatPayload — Gemini OpenAI-mos endpoint orqali ishlaydi:
// so'rov /chat/completions ga Bearer kalit bilan ketadi, javob formati strict
// json_schema (AI_OPENAI_STRUCTURED ga bog'liq emas), uzunlik maydoni max_tokens.
func TestGeminiCompatPayload(t *testing.T) {
	srv := newChatServer(t, func(map[string]any) (int, string) { return http.StatusOK, chatOK })

	temp := float32(0.5)
	p := newGemini(config.AIConfig{
		GeminiBaseURL: srv.URL + "/v1beta/openai", GeminiAPIKey: "gk", GeminiModel: "gemini-test",
		Temperature: &temp, MaxTokens: 1234,
		OpenAIStructured: false, // Gemini'ga ta'sir qilmaydi
	}, nil)
	if p.name() != "gemini/gemini-test" {
		t.Errorf("name = %q", p.name())
	}
	raw, err := p.complete(context.Background(), task{system: "s", user: "u", schema: deckOutlineSchema(true, design.AllLayouts), op: "deck_outline"})
	if err != nil || !strings.Contains(raw, "paragraphs") {
		t.Fatalf("javob: %q, %v", raw, err)
	}

	srv.mu.Lock()
	path, auth := srv.paths[0], srv.auths[0]
	srv.mu.Unlock()
	if path != "/v1beta/openai/chat/completions" || auth != "Bearer gk" {
		t.Errorf("so'rov manzili/kaliti noto'g'ri: path=%q auth=%q", path, auth)
	}
	reqs := srv.requests()
	if len(reqs) != 1 {
		t.Fatalf("1 ta so'rov kutilgan edi: %d", len(reqs))
	}
	body := reqs[0]
	if body["model"] != "gemini-test" || body["max_tokens"] != float64(1234) || body["temperature"] != float64(0.5) {
		t.Errorf("model/max_tokens/temperature noto'g'ri: %v %v %v", body["model"], body["max_tokens"], body["temperature"])
	}
	if _, ok := body["max_completion_tokens"]; ok {
		t.Error("max_completion_tokens yuborilmasligi kerak")
	}
	format, _ := body["response_format"].(map[string]any)
	js, _ := format["json_schema"].(map[string]any)
	if format["type"] != "json_schema" || js["strict"] != true || js["name"] != "deck_outline" {
		t.Fatalf("response_format = %v", format)
	}
	if !strings.Contains(srv.raws[0], `"properties":{"title":{"type":"string"},"subtitle":{"type":"string"},"slides":`) {
		t.Errorf("sxemadagi xossalar tartibi saqlanmadi:\n%s", srv.raws[0])
	}
}

// TestGeminiStructuredFallback — Gemini endpoint'i json_schema'ni rad etsa,
// provayder json_object ga o'tib, so'rovni darhol qayta yuboradi.
func TestGeminiStructuredFallback(t *testing.T) {
	srv := newChatServer(t, func(body map[string]any) (int, string) {
		if formatType(body) == "json_schema" {
			return http.StatusBadRequest, `{"error":{"message":"Invalid JSON payload received. Unknown name \"json_schema\" at 'response_format'"}}`
		}
		return http.StatusOK, chatOK
	})
	p := newGemini(config.AIConfig{GeminiBaseURL: srv.URL, GeminiAPIKey: "gk", GeminiModel: "gemini-test"}, nil)
	if _, err := p.complete(context.Background(), sectionTask()); err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, b := range srv.requests() {
		types = append(types, formatType(b))
	}
	if got := strings.Join(types, ","); got != "json_schema,json_object" || p.structured.Load() {
		t.Errorf("so'rovlar formati = %s, structured=%v", got, p.structured.Load())
	}
}
