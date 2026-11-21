package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/config"
)

func testImage(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), 120, 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeGen — sinov generatori: navbatdagi javoblarni qaytaradi.
type fakeGen struct {
	name     string
	mu       sync.Mutex
	errs     []error // har bir chaqiruvda navbatdagi xato (tugasa — muvaffaqiyat)
	data     []byte
	calls    atomic.Int32
	inflight atomic.Int32
	peak     atomic.Int32
	delay    time.Duration
}

func (f *fakeGen) Name() string { return f.name }

func (f *fakeGen) Generate(ctx context.Context, prompt string, shape Shape) ([]byte, error) {
	f.calls.Add(1)
	n := f.inflight.Add(1)
	defer f.inflight.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	return f.data, nil
}

func newTestService(g Generator) *Service {
	s := NewService(g, 3, time.Second, zap.NewNop())
	s.retryDelay = time.Millisecond
	return s
}

func TestServiceRetry(t *testing.T) {
	jpg := testImage(t, "jpeg")

	// Muvaffaqiyat — natijada provayder nomi (admin xabari va loglar uchun).
	g := &fakeGen{name: "openai/gpt-image-2:low", data: jpg}
	s := newTestService(g)
	if r := s.Generate(context.Background(), []Request{{Prompt: "a"}}, nil)[0]; r.Data == nil || r.Provider != "openai/gpt-image-2:low" || g.calls.Load() != 1 {
		t.Errorf("rasm chizilishi va provayder nomi qaytishi kerak: %+v, calls=%d", r, g.calls.Load())
	}

	// Vaqtinchalik xato — o'sha provayder bilan bir marta qayta urinish.
	g = &fakeGen{name: "g", data: jpg, errs: []error{errors.New("503")}}
	s = newTestService(g)
	if r := s.Generate(context.Background(), []Request{{}}, nil)[0]; r.Data == nil || g.calls.Load() != 2 {
		t.Errorf("vaqtinchalik xatodan keyin qayta urinish kerak: calls=%d", g.calls.Load())
	}

	// Ikki marta vaqtinchalik xato — faqat bitta qayta urinish, rasm chizilmaydi.
	g = &fakeGen{name: "g", data: jpg, errs: []error{errors.New("503"), errors.New("503"), errors.New("503")}}
	s = newTestService(g)
	if r := s.Generate(context.Background(), []Request{{}}, nil)[0]; r.Data != nil || g.calls.Load() != 2 {
		t.Errorf("faqat bitta qayta urinish bo'lishi kerak: %+v, calls=%d", r, g.calls.Load())
	}

	// Doimiy xato — qayta urinmasdan, rasm chizilmaydi.
	g = &fakeGen{name: "g", data: jpg, errs: []error{permanent(errors.New("billing"))}}
	s = newTestService(g)
	if r := s.Generate(context.Background(), []Request{{}}, nil)[0]; r.Data != nil || g.calls.Load() != 1 {
		t.Errorf("doimiy xatoda qayta urinmaslik kerak: %+v, calls=%d", r, g.calls.Load())
	}

	// Yaroqsiz rasm (doimiy xato) — nil.
	bad := &fakeGen{name: "bad", data: []byte("rasm emas")}
	s = newTestService(bad)
	if r := s.Generate(context.Background(), []Request{{}}, nil)[0]; r.Data != nil || bad.calls.Load() != 1 {
		t.Errorf("yaroqsiz rasm qabul qilinmasligi kerak: calls=%d", bad.calls.Load())
	}
}

// TestServicePerRequestTimeout — har bir urinish alohida muddat bilan
// cheklanadi (umumiy HTTP klientda timeout yo'q).
func TestServicePerRequestTimeout(t *testing.T) {
	var deadlines []time.Duration
	var mu sync.Mutex
	g := genFunc(func(ctx context.Context) ([]byte, error) {
		d, ok := ctx.Deadline()
		if !ok {
			t.Error("so'rov kontekstida muddat yo'q")
			return nil, permanent(errors.New("no deadline"))
		}
		mu.Lock()
		deadlines = append(deadlines, time.Until(d))
		mu.Unlock()
		return nil, permanent(errors.New("x"))
	})
	NewService(g, 1, 300*time.Millisecond, zap.NewNop()).Generate(context.Background(), []Request{{}}, nil)
	NewService(g, 1, 0, zap.NewNop()).Generate(context.Background(), []Request{{}}, nil)
	if len(deadlines) != 2 || deadlines[0] > 300*time.Millisecond || deadlines[1] <= 300*time.Millisecond || deadlines[1] > defaultTimeout {
		t.Errorf("muddatlar: %v (kutilgan ≤300ms va ≤%s)", deadlines, defaultTimeout)
	}
}

// genFunc — funksiyadan Generator.
type genFunc func(ctx context.Context) ([]byte, error)

func (genFunc) Name() string { return "func" }

func (f genFunc) Generate(ctx context.Context, _ string, _ Shape) ([]byte, error) { return f(ctx) }

// TestNew — New faqat OpenAI provayderini ulaydi: kalitsiz — ulanmaydi,
// kalit bilan — OPENAI_IMAGE_MODEL va OPENAI_IMAGE_QUALITY bilan chizadi.
func TestNew(t *testing.T) {
	cfg := config.ImagesConfig{OpenAIModel: "gpt-image-2", OpenAIQuality: "low", Timeout: time.Second, Parallel: 2}

	s, err := New(config.AIConfig{}, cfg, zap.NewNop())
	if err != nil || s.Available() {
		t.Fatalf("OPENAI_API_KEY siz provayder ulanmasligi kerak: available=%v, err=%v", s.Available(), err)
	}
	if res := s.Generate(context.Background(), []Request{{}}, nil); len(res) != 1 || res[0].Data != nil {
		t.Errorf("provaydersiz natija bo'sh bo'lishi kerak: %+v", res)
	}

	jpg := testImage(t, "jpeg")
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(jpg)}}})
	}))
	defer srv.Close()

	s, err = New(config.AIConfig{OpenAIAPIKey: "key", OpenAIBaseURL: srv.URL}, cfg, zap.NewNop())
	if err != nil || !s.Available() {
		t.Fatalf("kalit bilan provayder ulanishi kerak: available=%v, err=%v", s.Available(), err)
	}
	r := s.Generate(context.Background(), []Request{{Prompt: "a cat", Shape: Landscape}}, nil)[0]
	if !bytes.Equal(r.Data, jpg) || r.Provider != "openai/gpt-image-2:low" {
		t.Errorf("natija: provider=%q, %d bayt", r.Provider, len(r.Data))
	}
	if got["model"] != "gpt-image-2" || got["quality"] != "low" || got["size"] != "1536x1024" {
		t.Errorf("so'rov: %v", got)
	}
}

func TestServiceParallelLimitAndProgress(t *testing.T) {
	g := &fakeGen{name: "g", data: testImage(t, "jpeg"), delay: 20 * time.Millisecond}
	s := newTestService(g)
	var (
		mu    sync.Mutex
		dones []int
	)
	reqs := make([]Request, 8)
	res := s.Generate(context.Background(), reqs, func(done, total int) {
		mu.Lock()
		defer mu.Unlock()
		if total != 8 {
			t.Errorf("total = %d", total)
		}
		dones = append(dones, done)
	})
	for i, r := range res {
		if r.Data == nil {
			t.Errorf("rasm %d chizilmadi", i)
		}
	}
	if p := g.peak.Load(); p > 3 {
		t.Errorf("bir vaqtda %d ta so'rov (chegara 3)", p)
	}
	if len(dones) != 8 || dones[7] != 8 {
		t.Errorf("progress = %v", dones)
	}
}

// probeGen — bir vaqtda ishlayotgan chaqiruvlarni atomic hisoblagich bilan
// sanaydigan generator. Birinchi chaqiruvlar limit tasi yig'ilguncha kutadi:
// shunda test chegaradan oshmaslikni ham, chegaragacha haqiqatan parallel
// ishlashni ham tekshiradi.
type probeGen struct {
	data           []byte
	limit          int32
	calls          atomic.Int32
	inflight, peak atomic.Int32
	full           chan struct{}
	once           sync.Once
}

func (*probeGen) Name() string { return "probe" }

func (g *probeGen) Generate(ctx context.Context, prompt string, shape Shape) ([]byte, error) {
	g.calls.Add(1)
	n := g.inflight.Add(1)
	defer g.inflight.Add(-1)
	for {
		p := g.peak.Load()
		if n <= p || g.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if n >= g.limit {
		g.once.Do(func() { close(g.full) })
	}
	select {
	case <-g.full:
	case <-ctx.Done():
		return nil, permanent(ctx.Err())
	}
	// Chegarasiz pool'da chaqiruvlar albatta ustma-ust tushishi uchun.
	time.Sleep(2 * time.Millisecond)
	return g.data, nil
}

func TestServiceGenerateConcurrencyLimit(t *testing.T) {
	const n, limit = 10, 2
	g := &probeGen{data: testImage(t, "jpeg"), limit: limit, full: make(chan struct{})}
	s := NewService(g, limit, 2*time.Second, zap.NewNop())
	res := s.Generate(context.Background(), make([]Request, n), nil)
	for i, r := range res {
		if r.Data == nil {
			t.Errorf("rasm %d chizilmadi", i)
		}
	}
	if p := g.peak.Load(); p != limit {
		t.Errorf("bir vaqtda eng ko'pi %d ta so'rov, %d bo'lishi kerak", p, limit)
	}
	if c := g.calls.Load(); c != n {
		t.Errorf("chaqiruvlar %d ta, %d bo'lishi kerak", c, n)
	}
}

func TestServiceGenerateStopsOnCancel(t *testing.T) {
	jpg := testImage(t, "jpeg")

	// Kontekst oldindan bekor qilingan — birorta ham rasm boshlanmaydi.
	g := &fakeGen{name: "g", data: jpg}
	s := newTestService(g)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var dones atomic.Int32
	res := s.Generate(ctx, make([]Request, 4), func(int, int) { dones.Add(1) })
	if g.calls.Load() != 0 || dones.Load() != 0 || len(res) != 4 || res[0].Data != nil {
		t.Errorf("bekor qilingan kontekstda: calls=%d, onDone=%d, natijalar=%d", g.calls.Load(), dones.Load(), len(res))
	}

	// Ish davomida bekor qilinsa — navbatdagi rasmlar boshlanmaydi.
	g = &fakeGen{name: "g", data: jpg}
	s = NewService(g, 1, time.Second, zap.NewNop())
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	res = s.Generate(ctx, make([]Request, 4), func(int, int) { cancel() })
	drawn := make([]bool, len(res))
	for i, r := range res {
		drawn[i] = r.Data != nil
	}
	if g.calls.Load() != 1 || !slices.Equal(drawn, []bool{true, false, false, false}) {
		t.Errorf("ish davomida bekor qilinganda faqat birinchi rasm chizilishi kerak: calls=%d, chizilgan=%v", g.calls.Load(), drawn)
	}
}

func TestServiceUnavailable(t *testing.T) {
	var s *Service
	if s.Available() {
		t.Error("nil Service Available bo'lmasligi kerak")
	}
	s = newTestService(nil)
	if res := s.Generate(context.Background(), []Request{{}}, nil); len(res) != 1 || res[0].Data != nil {
		t.Errorf("provaydersiz natija bo'sh bo'lishi kerak: %+v", res)
	}
}

func TestToJPEG(t *testing.T) {
	jpg := testImage(t, "jpeg")
	if out, err := toJPEG(jpg); err != nil || !bytes.Equal(out, jpg) {
		t.Errorf("JPEG o'zgarishsiz qaytishi kerak: %v", err)
	}
	out, err := toJPEG(testImage(t, "png"))
	if err != nil {
		t.Fatal(err)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(out)); err != nil || format != "jpeg" {
		t.Errorf("PNG JPEG ga o'girilmadi: %s %v", format, err)
	}
	if _, err := toJPEG([]byte("<html>")); err == nil {
		t.Error("rasm bo'lmagan ma'lumot xato berishi kerak")
	}
}

func TestOpenAIGenerate(t *testing.T) {
	jpg := testImage(t, "jpeg")
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("so'rov: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(jpg)}}})
	}))
	defer srv.Close()

	o := NewOpenAI(srv.URL+"/v1/", "key", "gpt-image-2", "low", time.Second)
	data, err := o.Generate(context.Background(), "a cat", Portrait)
	if err != nil || !bytes.Equal(data, jpg) {
		t.Fatalf("Generate: %v", err)
	}
	for k, want := range map[string]any{"model": "gpt-image-2", "quality": "low", "size": "1024x1536", "output_format": "jpeg", "prompt": "a cat"} {
		if got[k] != want {
			t.Errorf("%s = %v, kutilgan %v", k, got[k], want)
		}
	}
	if !strings.HasPrefix(o.Name(), "openai/gpt-image-2") {
		t.Errorf("Name = %q", o.Name())
	}

	// Kvadrat rasm (AI maxsus dizayn) — 1024x1024.
	if _, err := o.Generate(context.Background(), "a dog", Square); err != nil {
		t.Fatalf("Generate(Square): %v", err)
	}
	if got["size"] != "1024x1024" {
		t.Errorf("Square size = %v, kutilgan 1024x1024", got["size"])
	}
}

func TestOpenAIErrors(t *testing.T) {
	cases := []struct {
		status    int
		permanent bool
	}{
		{http.StatusBadRequest, true},   // kontent filtri, noto'g'ri parametr
		{http.StatusUnauthorized, true}, // noto'g'ri kalit
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":{"message":"xabar","code":"x"}}`))
		}))
		_, err := NewOpenAI(srv.URL, "k", "m", "low", time.Second).Generate(context.Background(), "p", Landscape)
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "xabar") {
			t.Errorf("%d: xato = %v", tc.status, err)
			continue
		}
		if isPermanent(err) != tc.permanent {
			t.Errorf("%d: permanent = %v, kutilgan %v", tc.status, isPermanent(err), tc.permanent)
		}
	}
}
