package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"telegram-presentation-bot/internal/httpx"
)

// maxResponseBytes — rasm javobi hajmi chegarasi (base64 JSON).
const maxResponseBytes = 30 << 20

// OpenAI — OpenAI Images API (POST /images/generations) klienti:
// gpt-image-2, gpt-image-1.5, gpt-image-1-mini va boshqa gpt-image-* modellari.
type OpenAI struct {
	http     *http.Client
	endpoint string
	apiKey   string
	model    string
	quality  string // low | medium | high | auto
	// timeout — kontekstda muddat bo'lmasa, bitta so'rov uchun muddat
	// (Service orqali chaqirilganda muddatni Service.call beradi).
	timeout time.Duration
}

// NewOpenAI yangi klient yaratadi. baseURL — masalan "https://api.openai.com/v1".
func NewOpenAI(baseURL, apiKey, model, quality string, timeout time.Duration) *OpenAI {
	return &OpenAI{
		http:     httpx.Client(),
		endpoint: strings.TrimRight(baseURL, "/") + "/images/generations",
		apiKey:   apiKey,
		model:    model,
		quality:  quality,
		timeout:  timeout,
	}
}

func (o *OpenAI) Name() string { return "openai/" + o.model + ":" + o.quality }

type openAIImageResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (o *OpenAI) Generate(ctx context.Context, prompt string, shape Shape) ([]byte, error) {
	// Umumiy HTTP klientda timeout yo'q — muddatsiz kontekst bilan so'rov
	// cheksiz osilib qolmasligi uchun.
	if _, ok := ctx.Deadline(); !ok && o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}
	size := "1536x1024"
	switch shape {
	case Portrait:
		size = "1024x1536"
	case Square:
		size = "1024x1024"
	}
	payload := map[string]any{
		"model":              o.model,
		"prompt":             prompt,
		"size":               size,
		"quality":            o.quality,
		"n":                  1,
		"output_format":      "jpeg", // PNG dan bir necha barobar kichik va tezroq
		"output_compression": 80,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, httpx.Permanent(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, httpx.Permanent(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai rasm so'rovi bajarilmadi: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("openai javobini o'qib bo'lmadi: %w", err)
	}
	if len(data) > maxResponseBytes {
		return nil, httpx.Permanent(errors.New("openai javobi juda katta"))
	}

	var parsed openAIImageResponse
	jsonErr := json.Unmarshal(data, &parsed)

	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 500 {
			msg = msg[:500]
		}
		if jsonErr == nil && parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		e := fmt.Errorf("openai images API xatosi %d: %s", resp.StatusCode, msg)
		if httpx.IsPermanentStatus(resp.StatusCode) {
			return nil, httpx.Permanent(e)
		}
		return nil, e
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("openai javobi JSON emas: %w", jsonErr)
	}
	if len(parsed.Data) == 0 || parsed.Data[0].B64JSON == "" {
		return nil, errors.New("openai javobida rasm yo'q")
	}
	img, err := base64.StdEncoding.DecodeString(parsed.Data[0].B64JSON)
	if err != nil {
		return nil, fmt.Errorf("openai rasmini dekodlab bo'lmadi: %w", err)
	}
	return img, nil
}
