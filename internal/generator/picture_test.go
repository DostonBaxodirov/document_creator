package generator

import (
	"regexp"
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
	g := newTestPPTX(t)
	for _, info := range Themes() {
		if !info.Photos {
			continue
		}
		t.Run(info.ID, func(t *testing.T) {
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

func TestSlideTakesPhoto(t *testing.T) {
	items := func(n int) []Item {
		out := make([]Item, n)
		for i := range out {
			out[i] = Item{Heading: "h", Text: "t"}
		}
		return out
	}
	cases := []struct {
		name  string
		theme string
		s     Slide
		want  bool
	}{
		{"bullets", ThemeOcean, Slide{Layout: LayoutBullets, Items: items(4)}, true},
		{"bullets+highlight 3", ThemeOcean, Slide{Layout: LayoutBullets, Items: items(3), Highlight: "h"}, true},
		{"bullets+highlight 4", ThemeOcean, Slide{Layout: LayoutBullets, Items: items(4), Highlight: "h"}, false},
		{"agenda 6", ThemeJournal, Slide{Layout: LayoutAgenda, Items: items(6)}, true},
		{"agenda 7", ThemeJournal, Slide{Layout: LayoutAgenda, Items: items(7)}, false},
		{"cards 4", ThemeNature, Slide{Layout: LayoutCards, Items: items(4)}, true},
		{"cards 5", ThemeNature, Slide{Layout: LayoutCards, Items: items(5)}, false},
		{"definition", ThemeSunset, Slide{Layout: LayoutDefinition, Highlight: "d", Items: items(3)}, true},
		{"process", ThemeSunset, Slide{Layout: LayoutProcess, Items: items(3)}, false},
		{"facts", ThemeSunset, Slide{Layout: LayoutFacts, Items: []Item{{Value: "1", Heading: "a"}, {Value: "2", Heading: "b"}}}, false},
		{"comparison", ThemeSunset, Slide{Layout: LayoutComparison, Items: items(2)}, false},
		{"conclusion", ThemeSunset, Slide{Layout: LayoutConclusion, Items: items(3)}, false},
		{"old theme", ThemeAcademic, Slide{Layout: LayoutBullets, Items: items(3)}, false},
		{"unknown theme", "mavjud-emas", Slide{Layout: LayoutBullets, Items: items(3)}, false},
		// normalizeSlide: ta'rifsiz "definition" -> bullets
		{"normalized", ThemeOcean, Slide{Layout: LayoutDefinition, Items: items(2)}, true},
	}
	for _, tc := range cases {
		if got := SlideTakesPhoto(tc.theme, tc.s); got != tc.want {
			t.Errorf("%s: %v, kutilgan %v", tc.name, got, tc.want)
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

// TestPanoramaBannerPhotos — banner rasmli dizaynda rasm yotiq va deyarli
// barcha slaydlarga (reja va xulosadan tashqari) qo'yiladi.
func TestPanoramaBannerPhotos(t *testing.T) {
	if !SlidePhotoWide(ThemePanorama) || SlidePhotoWide(ThemeOcean) || SlidePhotoWide("mavjud-emas") {
		t.Errorf("SlidePhotoWide noto'g'ri")
	}
	items := []Item{{Heading: "a"}, {Heading: "b"}, {Heading: "c"}, {Heading: "d"}}
	for _, l := range []string{LayoutTimeline, LayoutMatrix, LayoutProcess, LayoutFacts} {
		s := Slide{Layout: l, Items: items}
		if l == LayoutFacts {
			s.Items = []Item{{Value: "1", Heading: "a"}, {Value: "2", Heading: "b"}}
		}
		if !SlideTakesPhoto(ThemePanorama, s) {
			t.Errorf("panorama: %s slaydga rasm qo'yilishi kerak", l)
		}
		if l != LayoutFacts && SlideTakesPhoto(ThemeOcean, s) && l != LayoutCards {
			t.Errorf("okean: keng %s maketiga yon rasm qo'yilmasligi kerak", l)
		}
	}
	for _, l := range []string{LayoutAgenda, LayoutConclusion} {
		if SlideTakesPhoto(ThemePanorama, Slide{Layout: l, Items: items}) {
			t.Errorf("panorama: %s slaydga rasm qo'yilmasligi kerak", l)
		}
	}
	if SlideTakesPhoto(ThemeStrategy, Slide{Layout: LayoutBullets, Items: items}) {
		t.Errorf("rasmsiz dizaynda rasm so'ralmasligi kerak")
	}
}
