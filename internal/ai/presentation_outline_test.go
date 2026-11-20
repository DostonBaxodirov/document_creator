package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"google.golang.org/genai"
)

// TestOutlineImagePrompt — rasmli dizaynda rejadagi har bir slayd uchun rasm
// tavsifi so'raladi (prompt va sxemada), oddiy dizaynda — yo'q. Reja rasm
// tavsiflari bilan kelsa, guruh prompti va sxemasida rasm maydoni bo'lmaydi.
func TestOutlineImagePrompt(t *testing.T) {
	plain := PresentationRequest{Topic: "Mavzu", SlideCount: 10}
	photo := PresentationRequest{Topic: "Mavzu", SlideCount: 10, Images: true}
	const slideImage = `"focus": string, "image": string}`

	p := buildDeckOutlinePrompt(photo)
	if !strings.Contains(p, slideImage) || !strings.Contains(p, imageRule) {
		t.Errorf("rasmli reja promptida slayd rasm tavsifi talabi yo'q:\n%s", p)
	}
	if !strings.Contains(p, `"subtitle": string, "image": string`) || !strings.Contains(p, coverImageRule) {
		t.Errorf("rasmli reja promptida titul rasmi yo'q:\n%s", p)
	}
	if p := buildDeckOutlinePrompt(plain); strings.Contains(p, `"image"`) || strings.Contains(p, "realistic photograph") {
		t.Errorf("oddiy reja promptida rasm talabi bo'lmasligi kerak:\n%s", p)
	}
	custom := photo
	custom.Custom = &DesignBrief{Name: "Okean", ImageStyle: "soft watercolor"}
	if p := buildDeckOutlinePrompt(custom); !strings.Contains(p, customImageRule(custom.Custom)) || !strings.Contains(p, slideImage) {
		t.Errorf("maxsus dizayn reja promptida rasm uslubi talabi yo'q:\n%s", p)
	}

	items := func(s *genai.Schema) *genai.Schema { return s.Properties["slides"].Items }
	s := deckOutlineSchema(true, slideLayouts)
	if items(s).Properties["image"] == nil || !slices.Contains(items(s).Required, "image") || s.Properties["image"] == nil {
		t.Errorf("rasmli reja sxemasida slayd va titul rasmi bo'lishi kerak: %+v", items(s))
	}
	s = deckOutlineSchema(false, slideLayouts)
	if items(s).Properties["image"] != nil || s.Properties["image"] != nil {
		t.Errorf("oddiy reja sxemasida rasm maydoni bo'lmasligi kerak")
	}

	// Reja rasm tavsiflari bilan — guruhda ular qayta so'ralmaydi.
	plan := &deckOutline{Slides: []plannedSlide{{Title: "A", Layout: "cards", Focus: "f", Image: "A photo of a lab"}}}
	if b := buildSlidesBatchPrompt(photo, plan, 0, 1); strings.Contains(b, `"image"`) || strings.Contains(b, "realistic photograph") {
		t.Errorf("reja rasm tavsiflari bilan kelganda guruh promptida rasm maydoni bo'lmasligi kerak:\n%s", b)
	}
	// Rejada tavsif yo'q (masalan, model ularni yozmagan) — guruhda so'raladi.
	plan.Slides[0].Image = ""
	if b := buildSlidesBatchPrompt(photo, plan, 0, 1); !strings.Contains(b, `"highlight": string, "image": string`) {
		t.Errorf("rejada rasm tavsifi bo'lmasa guruh promptida rasm maydoni bo'lishi kerak:\n%s", b)
	}

	// Reja javobidagi rasm tavsifi tozalanadi va qirqiladi.
	long := strings.Repeat("word ", 100)
	o, err := parseDeckOutline(`{"title": "T", "subtitle": "S", "image": "cover",
	  "slides": [{"n": 1, "title": "A", "layout": "cards", "focus": "f", "image": "  A **robot**   in a lab  "},
	             {"n": 2, "title": "B", "layout": "bullets", "focus": "g", "image": "` + long + `"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if o.Slides[0].Image != "A robot in a lab" {
		t.Errorf("reja rasm tavsifi tozalanmadi: %q", o.Slides[0].Image)
	}
	if n := len([]rune(o.Slides[1].Image)); n > maxImageRunes+1 {
		t.Errorf("reja rasm tavsifi qirqilmadi: %d", n)
	}
	if op := o.outlinePlan(); op.CoverImage != "cover" || len(op.Slides) != 2 || op.Slides[0] != (PlannedSlide{Title: "A", Layout: "cards", Image: "A robot in a lab"}) {
		t.Errorf("OutlinePlan: %+v", op)
	}
}

// TestAssignPlanIndex — guruh slaydlari reja slaydlariga sarlavha bo'yicha,
// mos kelmasa — guruhdagi o'rni bo'yicha moslanadi; har bir reja slaydi
// ko'pi bilan bitta slaydga beriladi.
func TestAssignPlanIndex(t *testing.T) {
	plan := &deckOutline{}
	for _, title := range []string{"Reja", "Tarix", "Turlari", "Afzalliklar", "Kamchiliklar", "Xulosa"} {
		plan.Slides = append(plan.Slides, plannedSlide{Title: title})
	}
	cases := []struct {
		name     string
		from, to int
		titles   []string
		want     []int
	}{
		{"tartib bo'yicha", 0, 3, []string{"Reja", "Tarix", "Turlari"}, []int{0, 1, 2}},
		{"aralash tartib", 3, 6, []string{"Xulosa", "afzalliklar", "Kamchiliklar"}, []int{5, 3, 4}},
		{"bitta kam", 0, 3, []string{"Reja", "Turlari"}, []int{0, 2}},
		{"bitta kam, aralash", 3, 6, []string{"Xulosa", "Afzalliklar"}, []int{5, 3}},
		{"sarlavha o'zgargan", 0, 3, []string{"Boshqa nom", "Tarix", "Turlari"}, []int{0, 1, 2}},
		// O'rni (0) sarlavhasi mos slaydga berilgan — birinchi bo'sh reja slaydi olinadi.
		{"o'rni band", 0, 3, []string{"Yangi nom", "Reja"}, []int{1, 0}},
	}
	for _, c := range cases {
		slides := make([]Slide, len(c.titles))
		for i, title := range c.titles {
			slides[i] = Slide{Title: title, PlanIndex: -1}
		}
		assignPlanIndex(slides, plan, c.from, c.to)
		var got []int
		for _, s := range slides {
			got = append(got, s.PlanIndex)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: PlanIndex %v, kutilgan %v", c.name, got, c.want)
		}
	}
}

// fakeDeckProvider — taqdimot so'rovlariga javob beradigan soxta provayder:
// rejada n ta slayd (rasm tavsiflari bilan), guruhlarda esa slaydlarni
// teskari tartibda qaytaradi; dropFrom > 0 bo'lsa, shu raqamdan (1 dan)
// boshlanadigan guruh bitta slaydni (o'rtadagisini) tashlab ketadi.
type fakeDeckProvider struct {
	dropFrom int

	mu    sync.Mutex
	tasks []task
}

var batchRangeRe = regexp.MustCompile(`full content of slides (\d+)-(\d+) only`)

func (*fakeDeckProvider) name() string { return "fake" }

func (f *fakeDeckProvider) complete(_ context.Context, t task) (string, error) {
	f.mu.Lock()
	f.tasks = append(f.tasks, t)
	f.mu.Unlock()

	slide := func(i int, image string) map[string]any {
		s := map[string]any{"layout": "bullets", "title": fmt.Sprintf("Slayd %d", i), "lead": "Kirish.",
			"items": []map[string]string{{"heading": "h", "text": "t"}}}
		if image != "" {
			s["image"] = image
		}
		return s
	}
	var out any
	switch {
	case strings.Contains(t.user, "Plan a presentation with exactly"):
		n, _ := strconv.Atoi(regexp.MustCompile(`exactly (\d+) slides`).FindStringSubmatch(t.user)[1])
		var slides []map[string]any
		for i := 1; i <= n; i++ {
			slides = append(slides, map[string]any{"n": i, "title": fmt.Sprintf("Slayd %d", i), "layout": "bullets",
				"focus": "f", "image": fmt.Sprintf("Photo of slide %d", i)})
		}
		out = map[string]any{"title": "T", "subtitle": "S", "image": "Cover photo", "slides": slides}
	case batchRangeRe.MatchString(t.user):
		m := batchRangeRe.FindStringSubmatch(t.user)
		from, _ := strconv.Atoi(m[1])
		to, _ := strconv.Atoi(m[2])
		var slides []map[string]any
		for i := to; i >= from; i-- {
			if from == f.dropFrom && i == (from+to)/2 {
				continue
			}
			slides = append(slides, slide(i, ""))
		}
		out = map[string]any{"slides": slides}
	default: // bitta so'rovli taqdimot
		n, _ := strconv.Atoi(regexp.MustCompile(`"slides": exactly (\d+)`).FindStringSubmatch(t.user)[1])
		var slides []map[string]any
		for i := 1; i <= n; i++ {
			slides = append(slides, slide(i, fmt.Sprintf("Photo %d", i)))
		}
		out = map[string]any{"title": "T", "subtitle": "S", "image": "Cover", "slides": slides}
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

// TestBatchedDeckOnOutline — katta taqdimotda OnOutline reja tayyor bo'lgach,
// slaydlar yozilishidan oldin bir marta chaqiriladi; guruhlarda rasm
// tavsifi so'ralmaydi, slaydlar esa reja slaydlariga PlanIndex orqali
// moslanadi (guruh aralash tartibda va bitta slayd kam qaytarganda ham).
func TestBatchedDeckOnOutline(t *testing.T) {
	f := &fakeDeckProvider{dropFrom: 6} // 2-guruh (6-10) bitta slaydni tashlab ketadi
	c := testClient(f)
	var (
		calls        int
		batchesFirst int
		plan         OutlinePlan
	)
	pres, err := c.GeneratePresentation(context.Background(), PresentationRequest{
		Topic: "Mavzu", SlideCount: 10, Images: true,
		OnOutline: func(p OutlinePlan) {
			calls++
			plan = p
			f.mu.Lock()
			batchesFirst = len(f.tasks) - 1 // reja so'rovidan keyingilari
			f.mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || batchesFirst != 0 {
		t.Fatalf("OnOutline slaydlar yozilishidan oldin bir marta chaqirilishi kerak: %d chaqiruv, oldin %d guruh", calls, batchesFirst)
	}
	if plan.CoverImage != "Cover photo" || len(plan.Slides) != 10 {
		t.Fatalf("OutlinePlan: %+v", plan)
	}
	for i, s := range plan.Slides {
		if s.Title != fmt.Sprintf("Slayd %d", i+1) || s.Image != fmt.Sprintf("Photo of slide %d", i+1) || s.Layout != LayoutBullets {
			t.Errorf("reja slaydi %d: %+v", i, s)
		}
	}

	f.mu.Lock()
	for _, tk := range f.tasks[1:] {
		if strings.Contains(tk.user, `"image"`) || tk.schema.Properties["slides"].Items.Properties["image"] != nil {
			t.Errorf("guruh so'rovida rasm maydoni bo'lmasligi kerak:\n%s", tk.user)
		}
	}
	f.mu.Unlock()

	// 1-guruh: 5..1 (teskari), 2-guruh: 10, 9, 7, 6 (8 tashlab ketilgan).
	if len(pres.Slides) != 9 {
		t.Fatalf("9 ta slayd kutilgan edi, bor: %d", len(pres.Slides))
	}
	seen := map[int]bool{}
	for _, s := range pres.Slides {
		n, _ := strconv.Atoi(strings.TrimPrefix(s.Title, "Slayd "))
		if s.PlanIndex != n-1 {
			t.Errorf("%q: PlanIndex %d, kutilgan %d", s.Title, s.PlanIndex, n-1)
		}
		if seen[s.PlanIndex] {
			t.Errorf("PlanIndex %d takrorlandi", s.PlanIndex)
		}
		seen[s.PlanIndex] = true
		if want := fmt.Sprintf("Photo of slide %d", n); s.Image != want {
			t.Errorf("%q: rasm tavsifi rejadan olinishi kerak: %q", s.Title, s.Image)
		}
	}
	if seen[7] {
		t.Error("tashlab ketilgan 8-slaydning reja indeksi ishlatilmasligi kerak")
	}

	// Oddiy dizayn ham reja bilan yoziladi — OnOutline baribir chaqiriladi.
	calls = 0
	if _, err := testClient(&fakeDeckProvider{}).GeneratePresentation(context.Background(), PresentationRequest{
		Topic: "Mavzu", SlideCount: 8, OnOutline: func(p OutlinePlan) {
			calls++
			if len(p.Slides) != 8 {
				t.Errorf("OutlinePlan: %+v", p)
			}
		},
	}); err != nil || calls != 1 {
		t.Errorf("oddiy dizaynda OnOutline: %d chaqiruv, xato %v", calls, err)
	}
}

// TestSingleDeckPlanIndex — kichik taqdimot (bitta so'rov, rejasiz): OnOutline
// chaqirilmaydi, PlanIndex — slayd tartib raqami, rasm tavsiflari slaydlarda.
func TestSingleDeckPlanIndex(t *testing.T) {
	f := &fakeDeckProvider{}
	called := false
	pres, err := testClient(f).GeneratePresentation(context.Background(), PresentationRequest{
		Topic: "Mavzu", SlideCount: 4, Images: true, OnOutline: func(OutlinePlan) { called = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("kichik taqdimotda OnOutline chaqirilmasligi kerak")
	}
	if len(pres.Slides) != 4 {
		t.Fatalf("4 ta slayd kutilgan edi: %d", len(pres.Slides))
	}
	for i, s := range pres.Slides {
		if s.PlanIndex != i || s.Image == "" {
			t.Errorf("slayd %d: PlanIndex %d, rasm %q", i, s.PlanIndex, s.Image)
		}
	}
	if f.tasks[0].schema.Properties["slides"].Items.Properties["image"] == nil {
		t.Error("bitta so'rovli taqdimot sxemasida rasm maydoni qolishi kerak")
	}
}
