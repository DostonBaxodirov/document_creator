package images

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// Gemini — Gemini rasm modeli (Nano Banana oilasi) klienti.
type Gemini struct {
	client *genai.Client
	model  string
	size   string // "1K", "2K", "4K"; bo'sh — modelning standart qiymati
}

// NewGemini yangi klient yaratadi.
func NewGemini(ctx context.Context, apiKey, model, size string) (*Gemini, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("gemini klientini yaratib bo'lmadi: %w", err)
	}
	return &Gemini{client: c, model: model, size: size}, nil
}

func (g *Gemini) Name() string { return "gemini/" + g.model }

func (g *Gemini) Generate(ctx context.Context, prompt string, shape Shape) ([]byte, error) {
	ratio := "3:2"
	switch shape {
	case Portrait:
		ratio = "2:3"
	case Square:
		ratio = "1:1"
	}
	resp, err := g.client.Models.GenerateContent(ctx, g.model, genai.Text(prompt), &genai.GenerateContentConfig{
		ResponseModalities: []string{"IMAGE"},
		ImageConfig:        &genai.ImageConfig{AspectRatio: ratio, ImageSize: g.size},
	})
	if err != nil {
		var apiErr genai.APIError
		if errors.As(err, &apiErr) {
			return nil, classifyGeminiError(apiErr.Code, apiErr.Message)
		}
		return nil, fmt.Errorf("gemini: %w", err)
	}
	if fb := resp.PromptFeedback; fb != nil && fb.BlockReason != "" {
		return nil, permanent(fmt.Errorf("gemini so'rovni blokladi: %s", fb.BlockReason))
	}

	var reason genai.FinishReason
	for _, cand := range resp.Candidates {
		if cand == nil {
			continue
		}
		reason = cand.FinishReason
		if cand.Content == nil {
			continue
		}
		for _, p := range cand.Content.Parts {
			if p != nil && p.InlineData != nil && len(p.InlineData.Data) > 0 &&
				strings.HasPrefix(p.InlineData.MIMEType, "image/") {
				return p.InlineData.Data, nil
			}
		}
	}
	e := fmt.Errorf("gemini rasm qaytarmadi (finish reason: %s)", reason)
	if reason == genai.FinishReasonSafety || reason == genai.FinishReasonProhibitedContent || reason == genai.FinishReasonImageSafety {
		return nil, permanent(e)
	}
	return nil, e
}

// classifyGeminiError API xatosini doimiy yoki vaqtinchalik deb belgilaydi.
// 429 odatda vaqtinchalik (daqiqalik limit), lekin "limit: 0" — model bu
// tarifda umuman yopiq (masalan, bepul tarifda rasm modellari): qayta urinish
// foyda bermaydi, billing yoqish kerak.
func classifyGeminiError(code int, msg string) error {
	e := fmt.Errorf("gemini API xatosi %d: %s", code, msg)
	if isPermanentStatus(code) || code == 429 && strings.Contains(msg, "limit: 0") {
		return permanent(e)
	}
	return e
}
