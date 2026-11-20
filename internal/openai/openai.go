// Package openai OpenAI-mos REST API'lar (OpenAI, DeepSeek va OPENAI_BASE_URL
// dagi boshqa mos xizmatlar) uchun yagona HTTP klient: Chat Completions (matn
// va vision) hamda Images API. So'rov yuborish, javob hajmini cheklash, HTTP
// status va xato matnini tasniflash shu yerda; payload'ni chaqiruvchi paket
// (ai, payment, images) o'zi quradi.
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"telegram-presentation-bot/internal/httpx"
)

const (
	// DefaultMaxBody — PostJSON ga maxBody <= 0 berilganda javob hajmi chegarasi.
	DefaultMaxBody = 10 << 20
	// chatMaxBody — Chat javobi hajmi chegarasi.
	chatMaxBody = 10 << 20
	// maxErrorRunes — xato matniga qo'shiladigan javob tanasining uzunligi.
	maxErrorRunes = 500
)

// Client — bitta OpenAI-mos API (baseURL + kalit) uchun klient. Bir vaqtda
// bir nechta goroutine'dan ishlatish xavfsiz.
type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	label   string // xato matnlaridagi nom: "openai", "deepseek", "openai vision" ...
}

// New yangi klient yaratadi. baseURL — masalan "https://api.openai.com/v1"
// (oxiridagi "/" olib tashlanadi); label — xato matnlarida ishlatiladigan nom.
func New(baseURL, apiKey, label string) *Client {
	return &Client{
		http:    httpx.Client(),
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		label:   label,
	}
}

// errorResponse — OpenAI-mos API xato javobi: {"error":{"message":"..."}}.
type errorResponse struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// PostJSON payload'ni JSON qilib baseURL+path ga POST qiladi va 200 javobni
// out ga o'qiydi (out nil bo'lsa — javob tanasi tashlab yuboriladi). Javob
// tanasi maxBody baytdan oshmasligi kerak (maxBody <= 0 — DefaultMaxBody).
//
// 200 dan boshqa statusda "{label} API xatosi {kod}: {xabar}" xatosi
// qaytariladi; xabar {"error":{"message"}} dan, bo'lmasa qisqartirilgan javob
// tanasidan olinadi. Qayta urinish foyda bermaydigan statuslarda
// (httpx.IsPermanentStatus) xato httpx.Permanent bilan o'raladi.
func (c *Client) PostJSON(ctx context.Context, path string, payload, out any, maxBody int64) error {
	if maxBody <= 0 {
		maxBody = DefaultMaxBody
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return httpx.Permanent(fmt.Errorf("%s so'rovini tayyorlab bo'lmadi: %w", c.label, err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return httpx.Permanent(fmt.Errorf("%s so'rovini tayyorlab bo'lmadi: %w", c.label, err))
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s so'rovi bajarilmadi: %w", c.label, err)
	}
	defer resp.Body.Close()

	// Chegaradan bitta bayt ko'p o'qiladi — javob chegaradan oshganini bilish uchun.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("%s javobini o'qib bo'lmadi: %w", c.label, err)
	}
	if resp.StatusCode != http.StatusOK {
		return c.statusError(resp.StatusCode, data)
	}
	if int64(len(data)) > maxBody {
		return httpx.Permanent(fmt.Errorf("%s javobi juda katta (> %d bayt)", c.label, maxBody))
	}
	if out == nil {
		return nil
	}
	// DeepSeek javob oldidan bo'sh qatorlar yuborishi mumkin — Unmarshal ularni o'tkazib yuboradi.
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s javobi JSON emas: %w", c.label, err)
	}
	return nil
}

// APIError — API'ning 200 dan boshqa javobi. Chaqiruvchi status va xabarni
// errors.As bilan tekshirishi mumkin (masalan, qo'llab-quvvatlanmagan
// parametrni aniqlash uchun).
type APIError struct {
	Label      string // klient nomi: "openai", "deepseek" ...
	StatusCode int
	Message    string // {"error":{"message"}} yoki qisqartirilgan javob tanasi
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s API xatosi %d: %s", e.Label, e.StatusCode, e.Message)
}

// statusError 200 dan boshqa javob uchun xato yasaydi.
func (c *Client) statusError(code int, data []byte) error {
	msg := truncate(strings.TrimSpace(string(data)), maxErrorRunes)
	var parsed errorResponse
	if json.Unmarshal(data, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	e := &APIError{Label: c.label, StatusCode: code, Message: msg}
	if httpx.IsPermanentStatus(code) {
		return httpx.Permanent(e)
	}
	return e
}

// chatResponse — Chat Completions javobining kerakli qismi.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// Chat payload'ni POST /chat/completions ga yuboradi va birinchi javob
// variantining matnini qaytaradi. Payload'ni (model, messages, response_format
// va h.k.) chaqiruvchi quradi.
//
// Doimiy xatolar (qayta urinish foyda bermaydi): model so'rovni rad etgani
// (refusal) va kontent filtri (finish_reason "content_filter"). Vaqtinchalik
// xatolar: javob uzunlik chegarasida qirqilgani ("length"), serverda resurs
// yetishmagani ("insufficient_system_resource") va bo'sh javob.
func (c *Client) Chat(ctx context.Context, payload map[string]any) (string, error) {
	var parsed chatResponse
	if err := c.PostJSON(ctx, "/chat/completions", payload, &parsed, chatMaxBody); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("%s javobida choices bo'sh", c.label)
	}

	choice := parsed.Choices[0]
	if choice.Message.Refusal != "" {
		return "", httpx.Permanent(fmt.Errorf("%s so'rovni rad etdi: %s", c.label, choice.Message.Refusal))
	}
	switch choice.FinishReason {
	case "length":
		// JSON yarmida qirqilgan — parse qilishga urinishning foydasi yo'q.
		return "", fmt.Errorf("%s javobi uzunlik chegarasida qirqildi (AI_MAX_TOKENS ni oshiring)", c.label)
	case "content_filter":
		return "", httpx.Permanent(fmt.Errorf("%s javobi kontent filtri tomonidan bloklandi", c.label))
	case "insufficient_system_resource":
		return "", fmt.Errorf("%s serverida resurs yetishmadi (vaqtinchalik)", c.label)
	}
	// DeepSeek JSON rejimida ba'zan bo'sh (faqat probellardan iborat) javob qaytaradi —
	// bu vaqtinchalik holat, qayta urinish yordam beradi.
	if strings.TrimSpace(choice.Message.Content) == "" {
		return "", fmt.Errorf("%s bo'sh javob qaytardi (finish reason: %s)", c.label, choice.FinishReason)
	}
	return choice.Message.Content, nil
}

// ---------------------------------------------------------------------------
// Chat xabarlari (matnli va multimodal)
// ---------------------------------------------------------------------------

// Message — Chat Completions xabari. Content — satr (oddiy matn) yoki []Part
// (multimodal: matn va rasmlar).
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// Part — multimodal xabar qismi: matn ("text") yoki rasm ("image_url").
type Part struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL — rasm manzili (yoki data URI) va tahlil aniqligi.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "low" | "high" | "auto"
}

// TextPart matnli xabar qismini yaratadi.
func TextPart(text string) Part { return Part{Type: "text", Text: text} }

// ImagePart rasmni base64 data URI ko'rinishida beradigan xabar qismini
// yaratadi. detail — "low", "high" yoki "auto" ("" — modelning standarti).
func ImagePart(mime string, image []byte, detail string) Part {
	return Part{Type: "image_url", ImageURL: &ImageURL{URL: DataURI(mime, image), Detail: detail}}
}

// DataURI "data:{mime};base64,..." satrini qaytaradi.
func DataURI(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// truncate satrni ko'pi bilan limit ta belgigacha qisqartiradi (UTF-8 belgilari
// buzilmaydi) va qisqartirilgan bo'lsa "…" qo'shadi.
func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}
