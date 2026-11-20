package images

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"telegram-presentation-bot/internal/openai"
)

// maxResponseBytes — rasm javobi hajmi chegarasi (base64 JSON).
const maxResponseBytes = 30 << 20

// OpenAI — OpenAI Images API (POST /images/generations) klienti:
// gpt-image-2, gpt-image-1.5, gpt-image-1-mini va boshqa gpt-image-* modellari.
type OpenAI struct {
	client  *openai.Client
	model   string
	quality string // low | medium | high | auto
	// timeout — kontekstda muddat bo'lmasa, bitta so'rov uchun muddat
	// (Service orqali chaqirilganda muddatni Service.call beradi).
	timeout time.Duration
}

// NewOpenAI yangi klient yaratadi. baseURL — masalan "https://api.openai.com/v1".
func NewOpenAI(baseURL, apiKey, model, quality string, timeout time.Duration) *OpenAI {
	return &OpenAI{
		client:  openai.New(baseURL, apiKey, "openai images"),
		model:   model,
		quality: quality,
		timeout: timeout,
	}
}

func (o *OpenAI) Name() string { return "openai/" + o.model + ":" + o.quality }

type openAIImageResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
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
	var parsed openAIImageResponse
	if err := o.client.PostJSON(ctx, "/images/generations", payload, &parsed, maxResponseBytes); err != nil {
		return nil, err
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
