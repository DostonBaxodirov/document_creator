package generator

// picture.go — slaydlarga joylanadigan rasmlar (AI chizgan fotosuratlar):
// formatni tekshirish, "cover" usulida qirqish va rasm promptini mavzu
// uslubiga moslash.

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // DecodeConfig uchun formatlarni ro'yxatdan o'tkazish
	_ "image/png"
	"math"
)

// Picture — slaydga joylanadigan rasm. Bitta Picture bir nechta slaydda
// ishlatilsa (masalan, titul va yakuniy slayd), .pptx ichida bir marta saqlanadi.
type Picture struct {
	Data []byte
	Ext  string // "jpeg" yoki "png" — ppt/media/ ichidagi fayl kengaytmasi
	W, H int    // piksellarda
}

// minPictureSide — juda kichik (buzilgan) rasmlarni rad etish chegarasi.
const minPictureSide = 32

// NewPicture rasm baytlarini tekshiradi: faqat JPEG va PNG qabul qilinadi.
func NewPicture(data []byte) (*Picture, error) {
	if len(data) == 0 {
		return nil, errors.New("rasm bo'sh")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("rasmni o'qib bo'lmadi: %w", err)
	}
	if format != "jpeg" && format != "png" {
		return nil, fmt.Errorf("rasm formati qo'llab-quvvatlanmaydi: %s", format)
	}
	if cfg.Width < minPictureSide || cfg.Height < minPictureSide {
		return nil, fmt.Errorf("rasm juda kichik: %dx%d", cfg.Width, cfg.Height)
	}
	return &Picture{Data: data, Ext: format, W: cfg.Width, H: cfg.Height}, nil
}

func (p *Picture) contentType() string { return "image/" + p.Ext }

// coverCrop rasmni box maydonini to'liq qoplaydigan qilib (proporsiyani
// saqlagan holda) markazdan qirqish uchun srcRect qiymatlarini qaytaradi:
// chap, yuqori, o'ng, past — 1/100000 ulushlarda.
func coverCrop(imgW, imgH int, box rect) (l, t, r, b int) {
	if imgW <= 0 || imgH <= 0 || box.W <= 0 || box.H <= 0 {
		return 0, 0, 0, 0
	}
	ia := float64(imgW) / float64(imgH)
	ba := box.W / box.H
	switch {
	case ia > ba*1.001: // rasm kengroq — yon tomonlari qirqiladi
		side := int(math.Round((1 - ba/ia) / 2 * 100000))
		return side, 0, side, 0
	case ia < ba/1.001: // rasm balandroq — yuqori va pasti qirqiladi
		side := int(math.Round((1 - ia/ba) / 2 * 100000))
		return 0, side, 0, side
	}
	return 0, 0, 0, 0
}

// photoSuffix — barcha rasm promptlariga qo'shiladigan talablar.
const photoSuffix = "High-quality realistic photograph, natural composition, " +
	"absolutely no text, letters, numbers, captions, signs, watermarks or logos."

// PhotoPrompt AI yozgan rasm tavsifiga dizayn uslubini va umumiy talablarni
// qo'shadi — shunda bitta taqdimotdagi rasmlar dizayn ruhiga mos chiqadi.
func PhotoPrompt(themeID, desc string) string {
	return Design{themeByID(themeID)}.PhotoPrompt(desc)
}

// photoFits slayd maketi yonida rasm uchun joy bor-yo'qligini bildiradi
// (normalizeSlide dan keyin chaqiriladi). Keng maketlar (jarayon, taqqoslash,
// faktlar, xulosa) rasmsiz qoladi — aks holda matn haddan tashqari kichrayadi.
func photoFits(s Slide) bool {
	n := len(s.Items)
	switch s.Layout {
	case LayoutBullets:
		// Asosiy fikr (highlight) tor maydonda pastki qatorga tushadi —
		// punktlar ko'p bo'lsa ular uchun balandlik yetmaydi.
		if s.Highlight != "" {
			return n <= 3
		}
		return n <= 5
	case LayoutAgenda:
		return n <= 6
	case LayoutDefinition:
		return n <= 3
	case LayoutCards:
		return n <= 4
	case LayoutStatement:
		return n <= 3
	}
	return false
}

// layoutFitsPhoto — photoFits ning faqat maketga qarab taxmini (elementlar
// soni hali noma'lum): odatdagi hajmda yon panelga sig'adigan maketlar.
func layoutFitsPhoto(layout string) bool {
	switch layout {
	case LayoutAgenda, LayoutBullets, LayoutCards, LayoutDefinition, LayoutStatement:
		return true
	}
	return false
}

// photoAsBanner slayd rasmi tepada keng banner bo'lib (sarlavha orqasida)
// chizilishini bildiradi: banner rasmli dizaynda — doim, yon panelli
// dizaynda — yon panel matnni siqib qo'yadigan keng maketlarda
// (normalizeSlide dan keyin chaqiriladi).
func (t *theme) photoAsBanner(s Slide) bool {
	return t.photoBanner || !photoFits(s)
}

// SlideTakesPhoto slayd berilgan dizaynda rasm bilan chizilishini bildiradi.
// Rasmli dizaynda har bir mazmunli slaydga rasm qo'yiladi: mos maketda — yon
// panelda, keng maketda — tepadagi bannerda (SlidePhotoWide).
func SlideTakesPhoto(themeID string, _ Slide) bool {
	t := themeByID(themeID)
	return t.ID == themeID && t.photos
}

// SlidePhotoWide slayd rasmi yotiq (tepadagi banner) bo'lishini bildiradi;
// aks holda tik rasm (slayd yonidagi panel) so'raladi.
func SlidePhotoWide(themeID string, s Slide) bool {
	return DesignFor(themeID, nil).PhotoWide(s)
}

// ThemeHasPhotos dizayn rasmli ekanini bildiradi.
func ThemeHasPhotos(themeID string) bool {
	return DesignFor(themeID, nil).Photos()
}

// limitPictures rasmlar umumiy hajmini chegaralaydi (Telegram bot yuboradigan
// fayl hajmi chegarasi 50 MB): sig'magan rasmlar olib tashlanadi.
// Slaydlar ro'yxati nusxalanadi — chaqiruvchining ma'lumotlari o'zgarmaydi.
func limitPictures(d *Deck, limit int) {
	seen := make(map[*Picture]bool)
	total := 0
	keep := func(p *Picture) *Picture {
		if p == nil || seen[p] {
			return p
		}
		if total+len(p.Data) > limit {
			return nil
		}
		seen[p] = true
		total += len(p.Data)
		return p
	}
	d.CoverImage = keep(d.CoverImage)
	slides := make([]Slide, len(d.Slides))
	copy(slides, d.Slides)
	for i := range slides {
		slides[i].Image = keep(slides[i].Image)
	}
	d.Slides = slides
}
