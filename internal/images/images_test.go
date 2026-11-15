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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
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

func newTestService(tiers map[Tier]Generator, fallback bool) *Service {
	s := NewService(tiers, 3, time.Second, fallback, zap.NewNop())
	s.retryDelay = time.Millisecond
	return s
}

func TestServiceRoutesByTier(t *testing.T) {
	jpg := testImage(t, "jpeg")
	std := &fakeGen{name: "std", data: jpg}
	prem := &fakeGen{name: "prem", data: jpg}
	s := newTestService(map[Tier]Generator{TierStandard: std, TierPremium: prem}, true)

	res := s.Generate(context.Background(), TierPremium, []Request{{Prompt: "a"}, {Prompt: "b"}}, nil)
	if res[0].Provider != "prem" || res[1].Provider != "prem" || prem.calls.Load() != 2 || std.calls.Load() != 0 {
		t.Errorf("premium so'rovlar premium provayderga borishi kerak: %+v, std=%d", res, std.calls.Load())
	}
	if got := s.Provider(TierStandard); got != "std" {
		t.Errorf("Provider(standard) = %q", got)
	}
}

func TestServiceMissingTierUsesOther(t *testing.T) {
	std := &fakeGen{name: "std", data: testImage(t, "jpeg")}
	s := newTestService(map[Tier]Generator{TierStandard: std, TierPremium: nil}, false)
	if !s.Available() {
		t.Fatal("bitta provayder bilan ham Available bo'lishi kerak")
	}
	res := s.Generate(context.Background(), TierPremium, []Request{{Prompt: "a"}}, nil)
	if res[0].Data == nil || res[0].Provider != "std" {
		t.Errorf("premium provayder yo'q — standard ishlatilishi kerak: %+v", res[0])
	}
}

func TestServiceRetryAndFallback(t *testing.T) {
	jpg := testImage(t, "jpeg")

	// Vaqtinchalik xato — o'sha provayder bilan qayta urinish.
	std := &fakeGen{name: "std", data: jpg, errs: []error{errors.New("503")}}
	s := newTestService(map[Tier]Generator{TierStandard: std}, true)
	if r := s.Generate(context.Background(), TierStandard, []Request{{}}, nil)[0]; r.Data == nil || std.calls.Load() != 2 {
		t.Errorf("vaqtinchalik xatodan keyin qayta urinish kerak: calls=%d", std.calls.Load())
	}

	// Doimiy xato — qayta urinmasdan zaxira provayderga.
	std = &fakeGen{name: "std", data: jpg, errs: []error{permanent(errors.New("billing"))}}
	prem := &fakeGen{name: "prem", data: jpg}
	s = newTestService(map[Tier]Generator{TierStandard: std, TierPremium: prem}, true)
	r := s.Generate(context.Background(), TierStandard, []Request{{}}, nil)[0]
	if r.Provider != "prem" || std.calls.Load() != 1 {
		t.Errorf("doimiy xatoda zaxiraga o'tish kerak: %+v, std calls=%d", r, std.calls.Load())
	}

	// Zaxira o'chirilgan — rasm chizilmaydi.
	std = &fakeGen{name: "std", errs: []error{permanent(errors.New("x"))}}
	prem = &fakeGen{name: "prem", data: jpg}
	s = newTestService(map[Tier]Generator{TierStandard: std, TierPremium: prem}, false)
	if r := s.Generate(context.Background(), TierStandard, []Request{{}}, nil)[0]; r.Data != nil || prem.calls.Load() != 0 {
		t.Errorf("IMAGE_FALLBACK=false da zaxira ishlatilmasligi kerak")
	}

	// Barcha urinishlar muvaffaqiyatsiz — nil, boshqa rasmlar esa chiziladi.
	bad := &fakeGen{name: "bad", data: []byte("rasm emas")}
	s = newTestService(map[Tier]Generator{TierStandard: bad}, true)
	if r := s.Generate(context.Background(), TierStandard, []Request{{}}, nil)[0]; r.Data != nil {
		t.Errorf("yaroqsiz rasm qabul qilinmasligi kerak")
	}
}

func TestServiceParallelLimitAndProgress(t *testing.T) {
	g := &fakeGen{name: "g", data: testImage(t, "jpeg"), delay: 20 * time.Millisecond}
	s := newTestService(map[Tier]Generator{TierStandard: g}, true)
	var (
		mu    sync.Mutex
		dones []int
	)
	reqs := make([]Request, 8)
	res := s.Generate(context.Background(), TierStandard, reqs, func(done, total int) {
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

func TestServiceUnavailable(t *testing.T) {
	var s *Service
	if s.Available() {
		t.Error("nil Service Available bo'lmasligi kerak")
	}
	s = newTestService(nil, true)
	if res := s.Generate(context.Background(), TierStandard, []Request{{}}, nil); len(res) != 1 || res[0].Data != nil {
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

func TestClassifyGeminiError(t *testing.T) {
	cases := []struct {
		code      int
		msg       string
		permanent bool
	}{
		{429, "Quota exceeded for metric: generate_content_free_tier_requests, limit: 0, model: x", true},
		{429, "Resource has been exhausted (e.g. check quota).", false},
		{403, "API key not valid", true},
		{503, "overloaded", false},
	}
	for _, tc := range cases {
		if got := isPermanent(classifyGeminiError(tc.code, tc.msg)); got != tc.permanent {
			t.Errorf("%d %q: permanent = %v, kutilgan %v", tc.code, tc.msg, got, tc.permanent)
		}
	}
}
