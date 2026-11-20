// Package payment to'lov cheklarini tekshiradi: chek rasmini OpenAI vision
// modeli o'qiydi (verifier.go), yakuniy qarorni esa qat'iy qoidalar asosida
// Go kodi chiqaradi (decide.go). Model xulosasiga to'g'ridan-to'g'ri
// ishonilmaydi — u faqat chekda ko'rinib turgan qiymatlarni qaytaradi.
package payment

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"telegram-presentation-bot/internal/httpx"
)

// Receipt — chekdan o'qilgan qiymatlar.
type Receipt struct {
	IsReceipt       bool       `json:"is_receipt"`
	Success         bool       `json:"success"`
	Amount          flexAmount `json:"amount"` // butun so'mda; 0 — o'qilmagan
	Currency        string     `json:"currency"`
	CardLast4       flexString `json:"card_last4"`
	PaidAt          string     `json:"paid_at"` // chekdagi sana-vaqt: "YYYY-MM-DD HH:MM"
	TransactionID   flexString `json:"transaction_id"`
	MatchesExpected bool       `json:"matches_expected"` // modelning o'z fikri (faqat ma'lumot uchun)
	Confidence      flexAmount `json:"confidence"`       // 0..1
	Notes           string     `json:"notes"`

	// Raw — modelning xom JSON javobi (bazada saqlash uchun).
	Raw string `json:"-"`
}

// Expectation — chekda bo'lishi kutilgan qiymatlar (promptga beriladi).
type Expectation struct {
	Amount    int    // so'mda
	CardLast4 string // qabul qiluvchi kartaning oxirgi 4 raqami
}

// Verifier — OpenAI Chat Completions API (vision) orqali chekni o'qiydi.
type Verifier struct {
	http     *http.Client
	endpoint string
	apiKey   string
	model    string
	timeout  time.Duration
}

// NewVerifier yangi klient yaratadi. baseURL — masalan "https://api.openai.com/v1".
func NewVerifier(baseURL, apiKey, model string, timeout time.Duration) *Verifier {
	return &Verifier{
		http:     httpx.Client(),
		endpoint: strings.TrimRight(baseURL, "/") + "/chat/completions",
		apiKey:   apiKey,
		model:    model,
		timeout:  timeout,
	}
}

// Name logdagi nom.
func (v *Verifier) Name() string { return "openai/" + v.model }

const systemPrompt = `You are a meticulous payment receipt reader for a Telegram bot in Uzbekistan.
You receive a screenshot or photo that a customer claims is a receipt of a bank card transfer
(Payme, Click, Uzum, Paynet, bank mobile apps, ATM/terminal checks and similar).

Report ONLY what is actually visible in the image. Never guess and never copy the expected values
from the user message: they are given only so that you can comment on mismatches.

Respond with a single JSON object with exactly these keys:
- "is_receipt" (boolean): true if the image is a payment/transfer receipt or a transaction result screen.
- "success" (boolean): true only if the transaction status is clearly successful/completed (not pending, processing, failed or cancelled).
- "amount" (integer): the transferred amount in whole Uzbek so'm (UZS), excluding commission. 0 if not visible.
- "currency" (string): currency as shown ("UZS", "so'm", "USD", ...), or "" if not visible.
- "card_last4" (string): the last 4 digits of the RECIPIENT card number as shown (for "8600 **** **** 1234" return "1234"), or "" if not visible.
- "paid_at" (string): the transaction date and time exactly as shown, converted to the format "YYYY-MM-DD HH:MM" (24-hour, local time as printed). "" if not visible.
- "transaction_id" (string): the transaction / receipt / check number if visible, otherwise "".
- "matches_expected" (boolean): your opinion whether the amount and the recipient card match the expected values.
- "confidence" (number from 0 to 1): how confident you are in the values above AND that the image is a genuine, unedited receipt.
- "notes" (string): a short note in Uzbek (Latin script) about anything suspicious: signs of editing, inconsistent fonts, mismatches, unreadable parts. "" if nothing.`

// Read chek rasmini modelga yuboradi va o'qilgan qiymatlarni qaytaradi.
// Vaqtinchalik xatolarda (tarmoq, 429, 5xx, buzilgan JSON) bir marta qayta uriniladi.
func (v *Verifier) Read(ctx context.Context, image []byte, exp Expectation) (*Receipt, error) {
	if len(image) == 0 {
		return nil, errors.New("payment: rasm bo'sh")
	}
	mime := http.DetectContentType(image)
	if !strings.HasPrefix(mime, "image/") {
		mime = "image/jpeg"
	}
	body, err := json.Marshal(map[string]any{
		"model":           v.model,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []any{
			map[string]any{"role": "system", "content": systemPrompt},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": fmt.Sprintf(
					"Expected payment: %d UZS to a card ending in %s. Read the receipt in the image and answer in JSON.",
					exp.Amount, exp.CardLast4)},
				map[string]any{"type": "image_url", "image_url": map[string]string{
					"url":    "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image),
					"detail": "high",
				}},
			}},
		},
	})
	if err != nil {
		return nil, err
	}

	var r *Receipt
	err = httpx.Retry(ctx, readAttempts, readRetryBase, func(ctx context.Context) error {
		var err error
		r, err = v.do(ctx, body)
		return err
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Read dagi qayta urinish: ko'pi bilan 2 urinish, oradagi kechikish ~2s (+ jitter).
const (
	readAttempts  = 2
	readRetryBase = 2 * time.Second
)

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
	} `json:"error"`
}

func (v *Verifier) do(ctx context.Context, body []byte) (*Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, httpx.Permanent(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.apiKey)

	resp, err := v.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai vision so'rovi bajarilmadi: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("openai javobini o'qib bo'lmadi: %w", err)
	}

	var parsed chatResponse
	jsonErr := json.Unmarshal(data, &parsed)
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 500 {
			msg = msg[:500]
		}
		if jsonErr == nil && parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		e := fmt.Errorf("openai vision API xatosi %d: %s", resp.StatusCode, msg)
		if httpx.IsPermanentStatus(resp.StatusCode) {
			return nil, httpx.Permanent(e)
		}
		return nil, e
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("openai javobi JSON emas: %w", jsonErr)
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("openai javobida choices bo'sh")
	}
	ch := parsed.Choices[0]
	if ch.Message.Refusal != "" {
		return nil, httpx.Permanent(fmt.Errorf("openai so'rovni rad etdi: %s", ch.Message.Refusal))
	}
	return parseReceipt(ch.Message.Content)
}

// parseReceipt model javobidan Receipt ni o'qiydi (```json ... ``` o'ramini ham qabul qiladi).
func parseReceipt(content string) (*Receipt, error) {
	s := strings.TrimSpace(content)
	start, end := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil, errors.New("openai javobida JSON obyekt topilmadi")
	}
	var r Receipt
	if err := json.Unmarshal([]byte(s[start:end+1]), &r); err != nil {
		return nil, fmt.Errorf("openai javobini o'qib bo'lmadi: %w", err)
	}
	r.Raw = s[start : end+1]
	return &r, nil
}

// ---------------------------------------------------------------------------
// Moslashuvchan JSON turlari: model sonni satr ko'rinishida ("10 000") yoki
// satrni son ko'rinishida (1234) qaytarishi mumkin.
// ---------------------------------------------------------------------------

// flexAmount — son yoki satr ko'rinishidagi son.
type flexAmount float64

func (f *flexAmount) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == `""` {
		*f = 0
		return nil
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		*f = flexAmount(v)
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	*f = flexAmount(parseAmountString(str))
	return nil
}

// Int butun qiymat.
func (f flexAmount) Int() int { return int(f) }

// parseAmountString "10 000", "10,000.00", "10 000,00 so'm", "0.95" kabi satrlarni o'qiydi.
// Oxirgi ajratkichdan keyin 1–2 ta raqam bo'lsa — u kasr qismi hisoblanadi.
func parseAmountString(s string) float64 {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' {
			b.WriteRune(r)
		}
	}
	clean := b.String()
	intPart, frac := clean, ""
	if i := strings.LastIndexAny(clean, ".,"); i >= 0 {
		if n := len(clean) - i - 1; n >= 1 && n <= 2 {
			intPart, frac = clean[:i], clean[i+1:]
		}
	}
	digits := strings.NewReplacer(".", "", ",", "").Replace(intPart)
	if digits == "" {
		digits = "0"
	}
	v, err := strconv.ParseFloat(digits+"."+frac+"0", 64)
	if err != nil {
		return 0
	}
	return v
}

// flexString — satr yoki son ko'rinishidagi satr.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*f = flexString(strings.TrimSpace(str))
		return nil
	}
	*f = flexString(s)
	return nil
}

func (f flexString) String() string { return string(f) }
