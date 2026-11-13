package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"

	"telegram-presentation-bot/internal/config"
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
		APIKey:  cfg.GeminiAPIKey,
		Backend: genai.BackendGeminiAPI,
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
		ResponseMIMEType: "application/json",
		ResponseSchema:   t.schema,
	})
	if err != nil {
		return "", classifyGeminiError(err)
	}

	if fb := resp.PromptFeedback; fb != nil && fb.BlockReason != "" {
		return "", permanent(fmt.Errorf("gemini so'rovni blokladi: %s", fb.BlockReason))
	}

	var reason genai.FinishReason
	if len(resp.Candidates) > 0 && resp.Candidates[0] != nil {
		reason = resp.Candidates[0].FinishReason
	}
	switch reason {
	case genai.FinishReasonSafety:
		return "", permanent(errors.New("gemini javobi xavfsizlik filtri tomonidan bloklandi"))
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

// classifyGeminiError 4xx xatolarni (429 va 408 dan tashqari) doimiy deb belgilaydi.
func classifyGeminiError(err error) error {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		e := fmt.Errorf("gemini API xatosi %d: %s", apiErr.Code, apiErr.Message)
		if isPermanentStatus(apiErr.Code) {
			return permanent(e)
		}
		return e
	}
	return fmt.Errorf("gemini: %w", err)
}

// isPermanentStatus HTTP status qayta urinish bilan tuzalmasligini bildiradi:
// 4xx (noto'g'ri kalit, model, parametr, balans yetarli emas), 429 va 408 dan tashqari.
func isPermanentStatus(code int) bool {
	return code >= 400 && code < 500 && code != http.StatusTooManyRequests && code != http.StatusRequestTimeout
}

// ---------------------------------------------------------------------------
// OpenAI-mos provayder (Chat Completions REST API, qo'shimcha SDK talab qilmaydi).
//
// Bitta implementatsiya ikki xizmat uchun ishlatiladi:
//   - OpenAI (yoki OPENAI_BASE_URL orqali boshqa mos xizmat);
//   - DeepSeek (https://api.deepseek.com) — API OpenAI formatiga to'liq mos,
//     farqi faqat qo'shimcha "thinking" parametri va max_tokens maydon nomida.
// ---------------------------------------------------------------------------

// deepSeekDefaultMaxTokens — DeepSeek fikrlashsiz rejimda standart chegara 8K;
// katta taqdimotlar (25 slayd) unga sig'masligi mumkin, shuning uchun oshiramiz.
const deepSeekDefaultMaxTokens = 16384

type compatProvider struct {
	label       string // logdagi nom: "openai" yoki "deepseek"
	http        *http.Client
	endpoint    string
	apiKey      string
	model       string
	temperature *float32
	maxTokens   int
	// maxTokensField — javob uzunligi parametri nomi: OpenAI'da
	// "max_completion_tokens" (reasoning modellari faqat shuni qabul qiladi),
	// DeepSeek'da — "max_tokens".
	maxTokensField string
	extra          map[string]any // provayderga xos qo'shimcha parametrlar
}

func newOpenAI(cfg config.AIConfig) *compatProvider {
	return &compatProvider{
		label:          "openai",
		http:           newHTTPClient(cfg.Timeout),
		endpoint:       cfg.OpenAIBaseURL + "/chat/completions",
		apiKey:         cfg.OpenAIAPIKey,
		model:          cfg.OpenAIModel,
		temperature:    cfg.Temperature,
		maxTokens:      cfg.MaxTokens,
		maxTokensField: "max_completion_tokens",
	}
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
		http:           newHTTPClient(cfg.Timeout),
		endpoint:       cfg.DeepSeekBaseURL + "/chat/completions",
		apiKey:         cfg.DeepSeekAPIKey,
		model:          cfg.DeepSeekModel,
		temperature:    cfg.Temperature, // fikrlash rejimida DeepSeek uni e'tiborsiz qoldiradi
		maxTokens:      maxTokens,
		maxTokensField: "max_tokens",
		extra:          map[string]any{"thinking": map[string]string{"type": thinking}},
	}
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

func (o *compatProvider) name() string { return o.label + "/" + o.model }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (o *compatProvider) complete(ctx context.Context, t task) (string, error) {
	payload := map[string]any{
		"model": o.model,
		"messages": []chatMessage{
			{Role: "system", Content: t.system},
			{Role: "user", Content: t.user},
		},
		// JSON rejimi: model faqat yaroqli JSON qaytaradi (promptda "JSON" so'zi bo'lishi shart).
		"response_format": map[string]string{"type": "json_object"},
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

	body, err := json.Marshal(payload)
	if err != nil {
		return "", permanent(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", permanent(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s so'rovi bajarilmadi: %w", o.label, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("%s javobini o'qib bo'lmadi: %w", o.label, err)
	}

	var parsed chatResponse
	jsonErr := json.Unmarshal(data, &parsed) // DeepSeek javob oldidan bo'sh qatorlar yuborishi mumkin — Unmarshal ularni o'tkazib yuboradi

	if resp.StatusCode != http.StatusOK {
		msg := truncateRunes(strings.TrimSpace(string(data)), 500)
		if jsonErr == nil && parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		e := fmt.Errorf("%s API xatosi %d: %s", o.label, resp.StatusCode, msg)
		if isPermanentStatus(resp.StatusCode) {
			return "", permanent(e)
		}
		return "", e
	}
	if jsonErr != nil {
		return "", fmt.Errorf("%s javobi JSON emas: %w", o.label, jsonErr)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("%s javobida choices bo'sh", o.label)
	}

	choice := parsed.Choices[0]
	if choice.Message.Refusal != "" {
		return "", permanent(fmt.Errorf("%s so'rovni rad etdi: %s", o.label, choice.Message.Refusal))
	}
	switch choice.FinishReason {
	case "length":
		// JSON yarmida qirqilgan — parse qilishga urinishning foydasi yo'q.
		return "", fmt.Errorf("%s javobi uzunlik chegarasida qirqildi (AI_MAX_TOKENS ni oshiring)", o.label)
	case "content_filter":
		return "", permanent(fmt.Errorf("%s javobi kontent filtri tomonidan bloklandi", o.label))
	case "insufficient_system_resource":
		return "", fmt.Errorf("%s serverida resurs yetishmadi (vaqtinchalik)", o.label)
	}
	// DeepSeek JSON rejimida ba'zan bo'sh (faqat probellardan iborat) javob qaytaradi —
	// bu vaqtinchalik holat, qayta urinish yordam beradi.
	if strings.TrimSpace(choice.Message.Content) == "" {
		return "", fmt.Errorf("%s bo'sh javob qaytardi (finish reason: %s)", o.label, choice.FinishReason)
	}
	return choice.Message.Content, nil
}
