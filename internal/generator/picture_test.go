package generator

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	picEmbedRe = regexp.MustCompile(`<p:pic>.*?r:embed="(rId\d+)"`)
	imageRelRe = regexp.MustCompile(`<Relationship Id="(rId\d+)" Type="` + regexp.QuoteMeta(relTypeImage) + `" Target="\.\./media/([^"]+)"/>`)
)

// checkPictures slayddagi har bir <p:pic> uchun .rels da rasm bog'lanishi va
// ppt/media/ ichida fayl borligini tekshiradi; slayddagi rasmlar sonini qaytaradi.
func checkPictures(t *testing.T, files map[string][]byte, slide string) (pics int, media []string) {
	t.Helper()
	x := string(files[slide])
	rels := string(files[slideRelsPath(slide)])
	relTargets := map[string]string{}
	for _, m := range imageRelRe.FindAllStringSubmatch(rels, -1) {
		relTargets[m[1]] = m[2]
		if _, ok := files["ppt/media/"+m[2]]; !ok {
			t.Errorf("%s: ppt/media/%s topilmadi", slide, m[2])
		}
	}
	for _, m := range picEmbedRe.FindAllStringSubmatch(x, -1) {
		target, ok := relTargets[m[1]]
		if !ok {
			t.Errorf("%s: %s uchun rasm bog'lanishi yo'q", slide, m[1])
		}
		media = append(media, target)
		pics++
	}
	if len(relTargets) != pics {
		t.Errorf("%s: %d ta rasm bog'lanishi, %d ta <p:pic>", slide, len(relTargets), pics)
	}
	return pics, media
}

func TestPhotoThemes(t *testing.T) {
	t.Parallel()
	g := newTestPPTX(t)
	for _, info := range Themes() {
		if !info.Photos {
			continue
		}
		t.Run(info.ID, func(t *testing.T) {
			t.Parallel()
			d := SampleDeck(info.ID)
			files := buildAndRead(t, g, d)
			checkWellFormed(t, files)
			checkStructure(t, files, len(d.Slides)+extraSlides)
			if !regexp.MustCompile(`<Default Extension="jpeg" ContentType="image/jpeg"/>`).Match(files[contentTypesPath]) {
				t.Errorf("[Content_Types].xml da jpeg yozuvi yo'q")
			}

			total := len(d.Slides) + extraSlides
			_, coverMedia := checkPictures(t, files, "ppt/slides/slide1.xml")
			_, closingMedia := checkPictures(t, files, "ppt/slides/slide"+itoa(total)+".xml")
			if len(coverMedia) != 1 || len(closingMedia) != 1 || coverMedia[0] != closingMedia[0] {
				t.Errorf("titul va yakuniy slayd bitta rasm faylini ishlatishi kerak: %v %v", coverMedia, closingMedia)
			}

			withPhoto := 0
			for i, s := range d.Slides {
				n, _ := checkPictures(t, files, "ppt/slides/slide"+itoa(i+2)+".xml")
				want := 0
				if SlideTakesPhoto(info.ID, s) {
					want = 1
					withPhoto++
				}
				if n != want {
					t.Errorf("slayd %d (%s): %d ta rasm, kutilgan %d", i+2, s.Layout, n, want)
				}
			}
			if withPhoto < 3 {
				t.Errorf("namunada faqat %d ta mazmunli slaydda rasm", withPhoto)
			}
			media := 0
			for name := range files {
				if strings.HasPrefix(name, "ppt/media/") {
					media++
				}
			}
			if media != withPhoto+1 {
				t.Errorf("ppt/media da %d ta fayl, kutilgan %d", media, withPhoto+1)
			}
		})
	}
}

// TestPhotoThemeWithoutPictures — rasm chizilmagan holat (kalit yo'q, xato):
// taqdimot rasmsiz, lekin to'g'ri tuzilgan bo'lishi kerak.
func TestPhotoThemeWithoutPictures(t *testing.T) {
	g := newTestPPTX(t)
	for _, info := range Themes() {
		if !info.Photos {
			continue
		}
		d := sampleDeck(info.ID)
		d.Theme = info.ID
		files := buildAndRead(t, g, d)
		checkWellFormed(t, files)
		checkStructure(t, files, len(d.Slides)+extraSlides)
		for name, data := range files {
			if strings.HasPrefix(name, "ppt/media/") || strings.HasPrefix(name, "ppt/slides/slide") && strings.Contains(string(data), "<p:pic>") {
				t.Errorf("%s: rasmsiz taqdimotda %s bo'lmasligi kerak", info.ID, name)
			}
		}
	}
}

// TestOldThemesIgnorePictures — oddiy dizaynlarga rasm berilsa ham chizilmaydi.
func TestOldThemesIgnorePictures(t *testing.T) {
	g := newTestPPTX(t)
	d := SampleDeck(ThemeJournal)
	d.Theme = ThemeAcademic
	files := buildAndRead(t, g, d)
	for name := range files {
		if strings.HasPrefix(name, "ppt/media/") {
			t.Errorf("akademik dizaynda rasm bo'lmasligi kerak: %s", name)
		}
	}
}

func TestLimitPictures(t *testing.T) {
	a := &Picture{Data: make([]byte, 600)}
	b := &Picture{Data: make([]byte, 600)}
	slides := []Slide{{Image: a}, {Image: b}, {Image: a}}
	d := Deck{CoverImage: a, Slides: slides}
	limitPictures(&d, 1000)
	if d.CoverImage != a || d.Slides[0].Image != a || d.Slides[2].Image != a {
		t.Errorf("takrorlangan rasm hajmi bir marta hisoblanishi kerak")
	}
	if d.Slides[1].Image != nil {
		t.Errorf("chegaradan oshgan rasm olib tashlanishi kerak")
	}
	if slides[1].Image != b {
		t.Errorf("chaqiruvchining slaydlari o'zgarmasligi kerak")
	}
}

func TestCoverCrop(t *testing.T) {
	cases := []struct {
		w, h       int
		box        rect
		l, t, r, b int
	}{
		{1536, 1024, rect{0, 0, 13.333, 7.5}, 0, 7813, 0, 7813}, // 3:2 rasm 16:9 maydonda — yuqori/past qirqiladi
		{1024, 1536, rect{0, 0, 4, 4.5}, 0, 12500, 0, 12500},    // tik rasm
		{1536, 1024, rect{0, 0, 3, 4.5}, 27778, 0, 27778, 0},    // yotiq rasm tik maydonda — yonlari
		{1600, 900, rect{0, 0, 16, 9}, 0, 0, 0, 0},              // proporsiya bir xil
	}
	for _, tc := range cases {
		l, tp, r, b := coverCrop(tc.w, tc.h, tc.box)
		if abs(l-tc.l) > 2 || abs(tp-tc.t) > 2 || abs(r-tc.r) > 2 || abs(b-tc.b) > 2 {
			t.Errorf("coverCrop(%d,%d,%v) = %d %d %d %d, kutilgan %d %d %d %d", tc.w, tc.h, tc.box, l, tp, r, b, tc.l, tc.t, tc.r, tc.b)
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func TestNewPicture(t *testing.T) {
	p := SamplePicture(120, 80, "1B2A49", "C9A227")
	got, err := NewPicture(p.Data)
	if err != nil || got.Ext != "jpeg" || got.W != 120 || got.H != 80 {
		t.Fatalf("NewPicture = %+v, %v", got, err)
	}
	for name, data := range map[string][]byte{
		"bo'sh":    nil,
		"matn":     []byte("<html>xato</html>"),
		"gif":      []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"),
		"kichkina": SamplePicture(8, 8, "000000", "FFFFFF").Data,
	} {
		if _, err := NewPicture(data); err == nil {
			t.Errorf("%s: xato kutilgan edi", name)
		}
	}
}

// TestSamplePictureCache — bir xil argumentlar bilan chaqirilganda JPEG
// baytlari keshdan olinadi (umumiy), lekin har safar yangi *Picture
// qaytadi: .pptx da rasmlar ko'rsatkich bo'yicha birlashtiriladi.
func TestSamplePictureCache(t *testing.T) {
	a := SamplePicture(64, 48, "0B4F6C", "F4A259")
	b := SamplePicture(64, 48, "0B4F6C", "F4A259")
	if a == nil || b == nil {
		t.Fatal("SamplePicture nil qaytardi")
	}
	if a == b {
		t.Fatal("har chaqiruvda yangi *Picture qaytishi kerak")
	}
	if len(a.Data) == 0 || &a.Data[0] != &b.Data[0] {
		t.Error("bir xil argumentlar uchun JPEG baytlari keshdan olinishi kerak")
	}
	if a.Ext != "jpeg" || b.W != 64 || b.H != 48 {
		t.Errorf("rasm maydonlari noto'g'ri: %+v", b)
	}
	if c := SamplePicture(48, 64, "0B4F6C", "F4A259"); &c.Data[0] == &a.Data[0] || c.W != 48 || c.H != 64 {
		t.Error("boshqa o'lcham uchun alohida rasm kerak")
	}
	if got, err := NewPicture(b.Data); err != nil || got.W != 64 || got.H != 48 {
		t.Errorf("keshdagi baytlar yaroqli JPEG emas: %+v, %v", got, err)
	}
}

func TestSlideTakesPhoto(t *testing.T) {
	items := func(n int) []Item {
		out := make([]Item, n)
		for i := range out {
			out[i] = Item{Heading: "h", Text: "t"}
		}
		return out
	}
	facts := []Item{{Value: "1", Heading: "a"}, {Value: "2", Heading: "b"}}
	// Rasmli dizaynda har bir slaydga rasm qo'yiladi; wide — rasm yon panelda
	// emas, tepadagi bannerda (yotiq rasm so'raladi).
	cases := []struct {
		name       string
		theme      string
		s          Slide
		take, wide bool
	}{
		{"bullets", ThemeOcean, Slide{Layout: LayoutBullets, Items: items(4)}, true, false},
		{"bullets+highlight 3", ThemeOcean, Slide{Layout: LayoutBullets, Items: items(3), Highlight: "h"}, true, false},
		{"bullets+highlight 4", ThemeOcean, Slide{Layout: LayoutBullets, Items: items(4), Highlight: "h"}, true, true},
		{"agenda 6", ThemeJournal, Slide{Layout: LayoutAgenda, Items: items(6)}, true, false},
		{"agenda 7", ThemeJournal, Slide{Layout: LayoutAgenda, Items: items(7)}, true, true},
		{"cards 4", ThemeNature, Slide{Layout: LayoutCards, Items: items(4)}, true, false},
		{"cards 5", ThemeNature, Slide{Layout: LayoutCards, Items: items(5)}, true, true},
		{"definition", ThemeSunset, Slide{Layout: LayoutDefinition, Highlight: "d", Items: items(3)}, true, false},
		{"process", ThemeSunset, Slide{Layout: LayoutProcess, Items: items(3)}, true, true},
		{"facts", ThemeSunset, Slide{Layout: LayoutFacts, Items: facts}, true, true},
		{"comparison", ThemeSunset, Slide{Layout: LayoutComparison, Items: items(2)}, true, true},
		{"conclusion", ThemeSunset, Slide{Layout: LayoutConclusion, Items: items(3)}, true, true},
		{"timeline", ThemeJournal, Slide{Layout: LayoutTimeline, Items: items(4)}, true, true},
		{"panorama bullets", ThemePanorama, Slide{Layout: LayoutBullets, Items: items(3)}, true, true},
		{"panorama agenda", ThemePanorama, Slide{Layout: LayoutAgenda, Items: items(5)}, true, true},
		{"old theme", ThemeAcademic, Slide{Layout: LayoutBullets, Items: items(3)}, false, false},
		{"unknown theme", "mavjud-emas", Slide{Layout: LayoutBullets, Items: items(3)}, false, false},
		// normalizeSlide: ta'rifsiz "definition" -> bullets (yon panelga sig'adi)
		{"normalized", ThemeOcean, Slide{Layout: LayoutDefinition, Items: items(2)}, true, false},
	}
	for _, tc := range cases {
		if got := SlideTakesPhoto(tc.theme, tc.s); got != tc.take {
			t.Errorf("%s: SlideTakesPhoto %v, kutilgan %v", tc.name, got, tc.take)
		}
		if got := SlidePhotoWide(tc.theme, tc.s); got != tc.wide {
			t.Errorf("%s: SlidePhotoWide %v, kutilgan %v", tc.name, got, tc.wide)
		}
	}
}

func TestPhotoPrompt(t *testing.T) {
	p := PhotoPrompt(ThemeSunset, "  A lighthouse on a cliff at dusk. ")
	if !strings.HasPrefix(p, "A lighthouse on a cliff at dusk. Style: cinematic") || !strings.Contains(p, "no text") {
		t.Errorf("PhotoPrompt = %q", p)
	}
	if p := PhotoPrompt("mavjud-emas", "X"); strings.Contains(p, "Style:") || !strings.Contains(p, "no text") {
		t.Errorf("noma'lum mavzu = %q", p)
	}
}

// TestBannerPhotoSlides — keng maketli slaydda rasm tepadagi bannerda
// chiziladi: slaydda bitta rasm bor va u slayd kengligida (yon panel emas).
func TestBannerPhotoSlides(t *testing.T) {
	t.Parallel()
	g := newTestPPTX(t)
	for _, id := range []string{ThemeJournal, ThemeNature, ThemeOcean, ThemeSunset, ThemePanorama} {
		d := SampleDeck(id)
		files := buildAndRead(t, g, d)
		banners := 0
		for i, s := range d.Slides {
			if !SlidePhotoWide(id, s) {
				continue
			}
			banners++
			slide := string(files["ppt/slides/slide"+itoa(i+2)+".xml"])
			if !strings.Contains(slide, `<p:pic>`) || !strings.Contains(slide, `<a:off x="0" y="0"/><a:ext cx="`+strconv.FormatInt(emu(slideW), 10)+`"`) {
				t.Errorf("%s: slayd %d (%s) da slayd kengligidagi banner rasm yo'q", id, i+2, s.Layout)
			}
		}
		if banners == 0 {
			t.Errorf("%s: namunada banner rasmli slayd yo'q", id)
		}
	}
}

// TestFadeOverlayDirection — qoplama ikkala yo'nalishda ham to'g'ri: from > to
// bo'lganda ham boshlang'ich chet to'q, oxirgi chet och bo'ladi (oldin bu
// holatda butun maydon bir tekis from% qoplama bilan yopilib, rasm ko'rinmasdi).
func TestFadeOverlayDirection(t *testing.T) {
	shapeRe := regexp.MustCompile(`<a:off x="(\d+)" y="(\d+)"/><a:ext cx="(\d+)" cy="(\d+)"/>.*?<a:alpha val="(\d+)"/>`)
	// opacity — (px, py) nuqtadagi ustma-ust qatlamlarning umumiy to'qligi, %.
	opacity := func(xml string, px, py float64) float64 {
		clear := 1.0
		for _, m := range shapeRe.FindAllStringSubmatch(xml, -1) {
			v := make([]float64, 5)
			for i := range v {
				n, _ := strconv.ParseFloat(m[i+1], 64)
				v[i] = n
			}
			x, y, w, h := v[0]/emuPerInch, v[1]/emuPerInch, v[2]/emuPerInch, v[3]/emuPerInch
			if px >= x && px <= x+w && py >= y && py <= y+h {
				clear *= 1 - v[4]/100000
			}
		}
		return (1 - clear) * 100
	}
	r := rect{1, 1, 10, 4}
	cases := []struct {
		name       string
		from, to   int
		vertical   bool
		start, end [2]float64 // boshlang'ich va oxirgi chet yaqinidagi nuqtalar
	}{
		{"chapdan o'ngga ochiladi", 85, 20, false, [2]float64{1.05, 3}, [2]float64{10.95, 3}},
		{"chapdan o'ngga quyuqlashadi", 20, 85, false, [2]float64{1.05, 3}, [2]float64{10.95, 3}},
		{"yuqoridan pastga ochiladi", 80, 10, true, [2]float64{6, 1.02}, [2]float64{6, 4.98}},
		{"yuqoridan pastga quyuqlashadi", 10, 80, true, [2]float64{6, 1.02}, [2]float64{6, 4.98}},
	}
	for _, tc := range cases {
		c := newCanvas(paint{})
		fadeOverlay(c, r, "000000", tc.from, tc.to, tc.vertical)
		xml := c.b.String()
		s, e := opacity(xml, tc.start[0], tc.start[1]), opacity(xml, tc.end[0], tc.end[1])
		if math.Abs(s-float64(tc.from)) > 4 || math.Abs(e-float64(tc.to)) > 4 {
			t.Errorf("%s: boshida %.0f%% (kutilgan %d), oxirida %.0f%% (kutilgan %d)", tc.name, s, tc.from, e, tc.to)
		}
	}
}
