package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
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
}

// NewOpenAI yangi klient yaratadi. baseURL — masalan "https://api.openai.com/v1".
func NewOpenAI(baseURL, apiKey, model, quality string, timeout time.Duration) *OpenAI {
	return &OpenAI{
		http:     newHTTPClient(timeout),
		endpoint: strings.TrimRight(baseURL, "/") + "/images/generations",
		apiKey:   apiKey,
		model:    model,
		quality:  quality,
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
		return nil, permanent(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, permanent(err)
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
		return nil, permanent(errors.New("openai javobi juda katta"))
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
		if isPermanentStatus(resp.StatusCode) {
			return nil, permanent(e)
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

// newHTTPClient umumiy timeout kontekst orqali boshqariladi; bu yerda faqat
// ulanish bosqichlari cheklanadi.
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
	}}
}
