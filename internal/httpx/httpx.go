// Package httpx tashqi API'lar (OpenAI, DeepSeek, Gemini) bilan ishlash uchun
// umumiy vositalar: bitta ulanishlar hovuzli HTTP klient, doimiy (qayta
// urinish foyda bermaydigan) xato turi va eksponensial kechikishli qayta urinish.
//
// Provayderga xos tasnif (masalan, Gemini'ning "limit: 0" javobi yoki finish
// reason) o'z paketida qoladi va Permanent ustiga quriladi.
package httpx

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"
)

// Client barcha tashqi API so'rovlari uchun umumiy HTTP klientni qaytaradi
// (jarayon bo'yicha yagona nusxa — bitta Transport, bitta ulanishlar hovuzi:
// TLS ulanishlar provayderlar va paketlar o'rtasida qayta ishlatiladi).
//
// Klientda umumiy timeout ham, ResponseHeaderTimeout ham yo'q: har bir so'rov
// chaqiruvchi kontekstining muddati (deadline) bilan cheklanadi. Bu yerda
// faqat ulanish bosqichlari cheklanadi.
func Client() *http.Client { return sharedClient() }

var sharedClient = sync.OnceValue(func() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}}
})

// permanentError — qayta urinish foyda bermaydigan xato (noto'g'ri API kalit,
// noto'g'ri model nomi, balans yetarli emas, kontent bloklangan va h.k.).
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent xatoni doimiy deb belgilaydi: Retry va paketlarning o'z qayta
// urinish mantiqi bunday xatoda to'xtaydi. nil uchun nil qaytaradi.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent xato (yoki u o'ragan xatolardan biri) doimiy ekanini bildiradi.
func IsPermanent(err error) bool {
	var pe *permanentError
	return errors.As(err, &pe)
}

// IsPermanentStatus HTTP status qayta urinish bilan tuzalmasligini bildiradi:
// 4xx (noto'g'ri kalit, model, parametr, balans yetarli emas), 408 va 429 dan tashqari.
func IsPermanentStatus(code int) bool {
	return code >= 400 && code < 500 && code != http.StatusRequestTimeout && code != http.StatusTooManyRequests
}

// Backoff attempt-chi (1 dan boshlab) qayta urinishdan oldingi kechikishni
// hisoblaydi: d = base·2^(attempt−1), ko'pi bilan maxDelay; unga [0, d/2]
// oralig'ida tasodifiy jitter qo'shiladi. Demak natija [d, 1.5·d] oralig'ida.
// attempt < 1 — 1 deb olinadi; maxDelay < base — base deb olinadi; base <= 0 — 0.
func Backoff(attempt int, base, maxDelay time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	maxDelay = max(maxDelay, base)
	d := base
	for i := 1; i < attempt && d < maxDelay; i++ {
		if d > maxDelay/2 {
			d = maxDelay
			break
		}
		d *= 2
	}
	return d + rand.N(d/2+1)
}

// Sleep d davomida kutadi; kontekst bekor bo'lsa (yoki muddati tugasa) darhol
// false qaytaradi.
func Sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// retryMaxFactor — Retry dagi kechikish o'sishi chegarasi (base ning necha barobari).
const retryMaxFactor = 16

// Retry fn ni ko'pi bilan attempts marta chaqiradi (attempts < 1 — bir marta).
// Urinishlar orasida Backoff(i, base, 16·base) kutiladi. Muvaffaqiyatda,
// doimiy xatoda (IsPermanent) yoki kontekst bekor bo'lganda to'xtaydi va
// oxirgi xatoni qaytaradi; kutish paytida kontekst bekor bo'lsa — ctx.Err().
func Retry(ctx context.Context, attempts int, base time.Duration, fn func(context.Context) error) error {
	var err error
	for i := range max(1, attempts) {
		if i > 0 && !Sleep(ctx, Backoff(i, base, base*retryMaxFactor)) {
			return ctx.Err()
		}
		if err = fn(ctx); err == nil || IsPermanent(err) || ctx.Err() != nil {
			return err
		}
	}
	return err
}
