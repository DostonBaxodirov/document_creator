package ai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync/atomic"

	"go.uber.org/zap"
	"google.golang.org/genai"

	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/httpx"
	"telegram-presentation-bot/internal/openai"
)

// ---------------------------------------------------------------------------
// Google Gemini provayderi (rasmiy google.golang.org/genai SDK)
// ---------------------------------------------------------------------------

type geminiProvider struct {
	client      *genai.Client
	model       string
	temperature *float32
	maxTokens   int32 // 0 — modelning standart qiymati
}

func newGemini(ctx context.Context, cfg config.AIConfig) (*geminiProvider, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:     cfg.GeminiAPIKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: httpx.Client(),
	})
	if err != nil {
		return nil, fmt.Errorf("gemini klientini yaratib bo'lmadi: %w", err)
	}
	return &geminiProvider{
		client:      c,
		model:       cfg.GeminiModel,
		temperature: cfg.Temperature,
		maxTokens:   int32(min(cfg.MaxTokens, math.MaxInt32)),
	}, nil
}

func (g *geminiProvider) name() string { return "gemini/" + g.model }

func (g *geminiProvider) complete(ctx context.Context, t task) (string, error) {
	resp, err := g.client.Models.GenerateContent(ctx, g.model, genai.Text(t.user), &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(t.system, genai.RoleUser),
		Temperature:       g.temperature,
		MaxOutputTokens:   g.maxTokens,
		// Structured output: model javobni aynan shu sxema bo'yicha JSON qilib qaytaradi.
		ResponseMIMEType:   "application/json",
		ResponseJsonSchema: geminiResponseSchema(t.schema),
	})
	if err != nil {
		return "", classifyGeminiError(err)
	}

	if fb := resp.PromptFeedback; fb != nil && fb.BlockReason != "" {
		return "", httpx.Permanent(fmt.Errorf("gemini so'rovni blokladi: %s", fb.BlockReason))
	}

	var reason genai.FinishReason
	if len(resp.Candidates) > 0 && resp.Candidates[0] != nil {
		reason = resp.Candidates[0].FinishReason
	}
	switch reason {
	case genai.FinishReasonSafety:
		return "", httpx.Permanent(errors.New("gemini javobi xavfsizlik filtri tomonidan bloklandi"))
	case genai.FinishReasonMaxTokens:
		// JSON yarmida qirqilgan — parse qilishga urinishning foydasi yo'q.
		return "", errors.New("gemini javobi uzunlik chegarasida qirqildi (AI_MAX_TOKENS ni oshiring)")
	}

	text := resp.Text()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("gemini bo'sh javob qaytardi (finish reason: %s)", reason)
	}
	return text, nil
}

// geminiResponseSchema sxemani Gemini uchun moslaydi (nil — sxemasiz).
func geminiResponseSchema(s map[string]any) any {
	if s == nil {
		return nil
	}
	return geminiSchema(s)
}

// classifyGeminiError 4xx xatolarni (429 va 408 dan tashqari) doimiy deb belgilaydi.
func classifyGeminiError(err error) error {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		e := fmt.Errorf("gemini API xatosi %d: %s", apiErr.Code, apiErr.Message)
		if httpx.IsPermanentStatus(apiErr.Code) {
			return httpx.Permanent(e)
		}
		return e
	}
	return fmt.Errorf("gemini: %w", err)
}

// ---------------------------------------------------------------------------
// OpenAI-mos provayder (Chat Completions REST API, qo'shimcha SDK talab qilmaydi).
//
// Bitta implementatsiya ikki xizmat uchun ishlatiladi:
//   - OpenAI (yoki OPENAI_BASE_URL orqali boshqa mos xizmat);
//   - DeepSeek (https://api.deepseek.com) — API OpenAI formatiga to'liq mos,
//     farqi qo'shimcha "thinking" parametri, max_tokens maydon nomi va
//     javob formati (DeepSeek faqat json_object rejimini qo'llaydi).
// ---------------------------------------------------------------------------

// deepSeekDefaultMaxTokens — DeepSeek fikrlashsiz rejimda standart chegara 8K;
// katta taqdimotlarning bitta so'rovi unga sig'masligi mumkin, shuning uchun oshiramiz.
const deepSeekDefaultMaxTokens = 16384

type compatProvider struct {
	label       string // logdagi nom: "openai" yoki "deepseek"
	client      *openai.Client
	model       string
	temperature *float32
	maxTokens   int
	// maxTokensField — javob uzunligi parametri nomi: OpenAI'da
	// "max_completion_tokens" (reasoning modellari faqat shuni qabul qiladi),
	// DeepSeek'da — "max_tokens".
	maxTokensField string
	extra          map[string]any // provayderga xos qo'shimcha parametrlar
	// structured — Structured Outputs (response_format json_schema, strict)
	// yoqilgan. API uni qo'llamasa (400), bir marta json_object rejimiga
	// o'tiladi va jarayon oxirigacha shunday qoladi.
	structured atomic.Bool
	log        *zap.Logger
}

func newOpenAI(cfg config.AIConfig, log *zap.Logger) *compatProvider {
	if log == nil {
		log = zap.NewNop()
	}
	p := &compatProvider{
		label:          "openai",
		client:         openai.New(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, "openai"),
		model:          cfg.OpenAIModel,
		temperature:    cfg.Temperature,
		maxTokens:      cfg.MaxTokens,
		maxTokensField: "max_completion_tokens",
		log:            log,
	}
	p.structured.Store(cfg.OpenAIStructured)
	return p
}

func newDeepSeek(cfg config.AIConfig) *compatProvider {
	// DeepSeek'da fikrlash rejimi sukut bo'yicha YOQILGAN. Matn yozish uchun
	// u shart emas: o'chirilgan rejim bir necha barobar tez va arzon.
	thinking := "disabled"
	maxTokens := cfg.MaxTokens
	if cfg.DeepSeekThinking {
		thinking = "enabled" // standart chegara 64K — fikrlash uchun yetarli
	} else if maxTokens == 0 {
		maxTokens = deepSeekDefaultMaxTokens
	}
	return &compatProvider{
		label:          "deepseek",
		client:         openai.New(cfg.DeepSeekBaseURL, cfg.DeepSeekAPIKey, "deepseek"),
		model:          cfg.DeepSeekModel,
		temperature:    cfg.Temperature, // fikrlash rejimida DeepSeek uni e'tiborsiz qoldiradi
		maxTokens:      maxTokens,
		maxTokensField: "max_tokens",
		extra:          map[string]any{"thinking": map[string]string{"type": thinking}},
		log:            zap.NewNop(), // structured o'chiq — log kerak emas
	}
}

func (o *compatProvider) name() string { return o.label + "/" + o.model }

func (o *compatProvider) complete(ctx context.Context, t task) (string, error) {
	structured := t.schema != nil && o.structured.Load()
	payload, err := o.payload(t, structured)
	if err != nil {
		return "", err
	}
	raw, err := o.client.Chat(ctx, payload)
	if !structured || !isResponseFormatError(err) {
		return raw, err
	}

	// API json_schema'ni qo'llamaydi (masalan, OPENAI_BASE_URL dagi boshqa
	// xizmat yoki eski model) — json_object rejimiga o'tib, darhol qayta yuboramiz.
	if o.structured.CompareAndSwap(true, false) {
		o.log.Warn("structured outputs not supported, falling back to json_object",
			zap.String("provider", o.name()), zap.Error(err))
	}
	if payload, err = o.payload(t, false); err != nil {
		return "", err
	}
	return o.client.Chat(ctx, payload)
}

// payload Chat Completions so'rovini quradi. structured — javob formati
// json_schema (strict), aks holda json_object.
func (o *compatProvider) payload(t task, structured bool) (map[string]any, error) {
	// JSON rejimi: model faqat yaroqli JSON qaytaradi (promptda "JSON" so'zi bo'lishi shart).
	var format any = map[string]string{"type": "json_object"}
	if structured {
		schema, err := marshalSchema(t.schema)
		if err != nil {
			return nil, httpx.Permanent(fmt.Errorf("%s: javob sxemasini tayyorlab bo'lmadi: %w", o.label, err))
		}
		// Structured Outputs: javob aynan shu sxemaga mos keladi.
		format = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   schemaName(t.op),
				"strict": true,
				"schema": schema,
			},
		}
	}
	payload := map[string]any{
		"model": o.model,
		"messages": []openai.Message{
			{Role: "system", Content: t.system},
			{Role: "user", Content: t.user},
		},
		"response_format": format,
		"stream":          false,
	}
	if o.temperature != nil {
		payload["temperature"] = *o.temperature
	}
	if o.maxTokens > 0 {
		payload[o.maxTokensField] = o.maxTokens
	}
	for k, v := range o.extra {
		payload[k] = v
	}
	return payload, nil
}

// isResponseFormatError API json_schema javob formatini rad etganini bildiradi:
// 400 va xabarda response_format yoki json_schema tilga olingan.
func isResponseFormatError(err error) bool {
	var apiErr *openai.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return false
	}
	msg := strings.ToLower(apiErr.Message)
	return strings.Contains(msg, "response_format") || strings.Contains(msg, "json_schema")
}
