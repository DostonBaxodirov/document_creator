package generator

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"slices"
	"strconv"
)

// SampleDeck barcha maketlarni o'z ichiga olgan namunaviy taqdimotni
// qaytaradi. AI va Telegram'siz dizaynni tekshirish (cmd/preview), mavzular
// ko'rinishi rasmlarini tayyorlash va testlar uchun ishlatiladi.
// Rasmli dizaynlarda titul va mos slaydlarga namunaviy (gradientli) rasmlar
// qo'yiladi — haqiqiy AI rasmlarsiz, internetsiz.
func SampleDeck(themeID string) Deck {
	d := sampleDeck(themeID)
	if !ThemeHasPhotos(themeID) {
		return d
	}
	t := themeByID(themeID)
	d.CoverImage = SamplePicture(1536, 1024, t.pal.primary, t.pal.accent)
	k := 0
	for i := range d.Slides {
		if SlideTakesPhoto(themeID, d.Slides[i]) {
			c1, c2 := t.seriesColor(k), t.seriesColor(k+1)
			d.Slides[i].Image = SamplePicture(1024, 1536, c1, c2)
			k++
		}
	}
	return d
}

// SamplePhotoPrompts — namunaviy taqdimot uchun rasm tavsiflari (inglizcha,
// AI yozadigan kabi): titul uchun va har bir slayd uchun (SampleDeck(themeID)
// tartibida). cmd/preview -photos bilan haqiqiy AI rasmlarni sinash uchun.
func SamplePhotoPrompts(themeID string) (cover string, slides []string) {
	for _, s := range sampleDeck(themeID).Slides {
		slides = append(slides, samplePhotoByLayout[s.Layout])
	}
	return "University students working with an AI assistant on laptops in a bright modern lecture hall, wide shot", slides
}

var samplePhotoByLayout = map[string]string{
	LayoutAgenda:     "An open notebook, a laptop and a cup of coffee on a wooden desk in a university library, top view",
	LayoutDefinition: "A glowing abstract neural network shape above a student's hands holding a tablet, close-up",
	LayoutProcess:    "Vintage room-sized computers next to a modern laptop in a museum exhibition hall",
	LayoutCards:      "A teacher helping a small group of students who use tablets in a sunny classroom",
	LayoutComparison: "Two paths in a park diverging in autumn, one sunny and one foggy, symbolic composition",
	LayoutFacts:      "Server racks with soft blue lights in a modern data center corridor",
	LayoutBullets:    "A teacher presenting a lesson plan to colleagues around a table in a staff room",
	LayoutConclusion: "A graduate in a cap and gown looking toward a modern city skyline at sunrise",
	LayoutTimeline:   "A row of historic and modern computers in a museum hall, from a vintage mainframe to a sleek laptop",
	LayoutPyramid:    "Students climbing a wide staircase of stacked books toward a bright window, symbolic composition",
	LayoutMatrix:     "A whiteboard divided into four sections with colorful sticky notes in a bright meeting room",
	LayoutCycle:      "A student studying with a tablet at a desk in soft morning light, calm atmosphere",
	LayoutHub:        "A group of students and a teacher around a round table with laptops, seen from above",
	LayoutStatement:  "A teacher talking with a student in an empty sunlit classroom, warm natural light",
}

// SamplePicture namunaviy rasm yaratadi: ikki rang orasidagi diagonal
// gradient va yumshoq doiralar (JPEG). Faqat preview va testlar uchun.
func SamplePicture(w, h int, from, to string) *Picture {
	c1, c2 := hexColor(from), hexColor(to)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy, r := float64(w)*0.68, float64(h)*0.38, float64(min(w, h))*0.32
	for y := range h {
		for x := range w {
			t := (float64(x)/float64(w) + float64(y)/float64(h)) / 2
			px := lerpColor(c1, c2, t)
			// Yorug' doira (quyosh/obyekt o'rnida) — rasmning "kompozitsiyasi" ko'rinsin.
			if d := math.Hypot(float64(x)-cx, float64(y)-cy); d < r {
				px = lerpColor(px, color.RGBA{255, 255, 255, 255}, 0.35*(1-d/r))
			}
			img.SetRGBA(x, y, px)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil
	}
	return &Picture{Data: buf.Bytes(), Ext: "jpeg", W: w, H: h}
}

func hexColor(s string) color.RGBA {
	v, _ := strconv.ParseUint(s, 16, 32)
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	l := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.RGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 255}
}

// sampleDeck namunaviy taqdimot: asosiy maketlar va dizaynning imzo maketlari.
// Imzo maketlari 2- va 4-mazmunli slaydlarga qo'yiladi — dizaynlar ko'rinishi
// rasmida (scripts/design_preview.py) aynan ular ko'rinadi. Rasmli (yon
// rasmli) dizaynlarda 4-slayd — rasmli kartochkalar.
func sampleDeck(themeID string) Deck {
	t := themeByID(themeID)
	byLayout := make(map[string]Slide)
	for _, s := range sampleSlides() {
		byLayout[s.Layout] = s
	}
	sig := t.signature
	order := []string{LayoutAgenda}
	used := map[string]bool{LayoutAgenda: true, LayoutConclusion: true}
	add := func(l string) {
		if !used[l] {
			used[l] = true
			order = append(order, l)
		}
	}
	if len(sig) > 0 {
		add(sig[0])
	}
	add(LayoutDefinition)
	if t.photos && !t.photoBanner || len(sig) < 2 {
		add(LayoutCards)
	} else {
		add(sig[1])
	}
	for _, l := range append(slices.Clone(sig), LayoutCards, LayoutProcess, LayoutComparison, LayoutFacts, LayoutBullets) {
		add(l)
	}
	order = append(order, LayoutConclusion)

	d := Deck{
		Topic:       "Sun'iy intellektning ta'limdagi o'rni",
		Subtitle:    "Imkoniyatlar, xavflar va joriy etish yo'llari",
		WorkType:    "TAQDIMOT",
		StudentName: "Aliyev Vali",
		University:  "Toshkent axborot texnologiyalari universiteti",
		TeacherName: "Karimova Dilnoza",
		Year:        "2026",
		Theme:       themeID,
	}
	for _, l := range order {
		s := byLayout[l]
		s.Items = slices.Clone(s.Items)
		d.Slides = append(d.Slides, s)
	}
	return d
}

// sampleSlides — har bir maket uchun bittadan namunaviy slayd.
func sampleSlides() []Slide {
	return []Slide{
		{
			Layout: LayoutAgenda, Title: "Reja",
			Items: []Item{
				{Heading: "Sun'iy intellekt tushunchasi", Text: "Asosiy ta'riflar va rivojlanish tarixi"},
				{Heading: "Ta'limdagi asosiy yo'nalishlar", Text: "Moslashuvchan ta'lim, baholash va tahlil"},
				{Heading: "Afzalliklar va xavflar", Text: "Imkoniyatlar hamda cheklovlarni solishtirish"},
				{Heading: "Joriy etish jihatlari", Text: "Ta'lim muassasasida tatbiq etish shartlari"},
				{Heading: "Xulosa", Text: "Asosiy fikrlar va tavsiyalar"},
			},
		},
		{
			Layout: LayoutDefinition, Title: "Sun'iy intellekt nima?",
			Lead: "Tushunchaning mohiyatini aniqlash uning ta'limdagi imkoniyatlarini to'g'ri baholashga yordam beradi.",
			Highlight: "Sun'iy intellekt — kompyuter tizimlarining odatda inson aqlini talab qiladigan vazifalarni, " +
				"jumladan o'rganish, mulohaza yuritish, nutqni tushunish va qaror qabul qilishni bajarish qobiliyatidir.",
			Items: []Item{
				{Heading: "Mashinali o'qitish", Text: "Tizim ma'lumotlardagi qonuniyatlarni aniqlab, tajriba asosida natijalarini yaxshilaydi."},
				{Heading: "Tabiiy tilni qayta ishlash", Text: "Matn va nutqni tushunish, tarjima qilish hamda yangi matn yaratish imkonini beradi."},
				{Heading: "Kompyuter ko'rishi", Text: "Tasvir va videolardagi obyektlarni aniqlash va tahlil qilishga xizmat qiladi."},
			},
		},
		{
			Layout: LayoutProcess, Title: "Rivojlanish bosqichlari",
			Lead: "Sun'iy intellekt yetmish yildan ortiq davr mobaynida nazariy g'oyadan kundalik vositaga aylandi.",
			Items: []Item{
				{Heading: "1956-yil: Dartmut", Text: "Sun'iy intellekt atamasi ilk bor ilmiy muomalaga kiritildi va yangi fan yo'nalishi shakllandi."},
				{Heading: "Ekspert tizimlar", Text: "Mantiqiy qoidalarga asoslangan dasturlar tibbiyot va muhandislikda qo'llana boshladi."},
				{Heading: "Chuqur o'rganish", Text: "Neyron tarmoqlar va katta ma'lumotlar tufayli tasvir hamda nutqni tanish keskin yaxshilandi."},
				{Heading: "Generativ modellar", Text: "Matn, tasvir va dastur kodini yarata oladigan katta til modellari keng ommalashdi."},
			},
		},
		{
			Layout: LayoutCards, Title: "Ta'limdagi asosiy yo'nalishlar",
			Lead: "Sun'iy intellekt o'quv jarayonining deyarli barcha bosqichlarida qo'llanilmoqda.",
			Items: []Item{
				{Heading: "Moslashuvchan ta'lim", Text: "Platformalar har bir o'quvchining bilim darajasi va o'zlashtirish sur'atiga mos topshiriqlarni tanlab beradi."},
				{Heading: "Avtomatik baholash", Text: "Test va yozma ishlarni tezkor tekshirish o'qituvchi vaqtini tejaydi va darhol fikr-mulohaza beradi."},
				{Heading: "Virtual yordamchilar", Text: "Chat-botlar talabalarning savollariga istalgan vaqtda javob berib, mustaqil ta'limni qo'llab-quvvatlaydi."},
				{Heading: "Ta'lim tahlili", Text: "O'zlashtirish ko'rsatkichlarini tahlil qilish orqali qiyinchilikka uchragan o'quvchilar erta aniqlanadi."},
			},
		},
		{
			Layout: LayoutComparison, Title: "Afzalliklar va xavflar",
			Lead: "Texnologiyaning foydasi bilan birga uning cheklovlarini ham hisobga olish zarur.",
			Items: []Item{
				{Heading: "Afzalliklar", Points: []string{
					"Ta'limni shaxsiylashtirish va individual yondashuv",
					"O'qituvchining tartibiy ishlarga sarflaydigan vaqti qisqaradi",
					"Ta'lim resurslaridan istalgan joyda foydalanish imkoniyati",
					"Natijalarni tezkor va xolis tahlil qilish",
				}},
				{Heading: "Xavflar", Points: []string{
					"Shaxsiy ma'lumotlar xavfsizligi bilan bog'liq muammolar",
					"Talabalarning texnologiyaga haddan tashqari tayanib qolishi",
					"Algoritmlarda noxolislik va xatoliklar ehtimoli",
					"Raqamli tengsizlikning chuqurlashishi",
				}},
			},
			Highlight: "Sun'iy intellekt o'qituvchining o'rnini bosmaydi, balki uning samaradorligini oshiruvchi vosita bo'lib xizmat qiladi.",
		},
		{
			Layout: LayoutFacts, Title: "Muhim faktlar",
			Lead: "Bir nechta asosiy ko'rsatkich sohaning ko'lami va imkoniyatlarini yaqqol ko'rsatadi.",
			Items: []Item{
				{Value: "1956", Heading: "Atama paydo bo'lgan yil", Text: "Dartmut konferensiyasida Jon Makkarti tomonidan taklif etilgan."},
				{Value: "24/7", Heading: "Doimiy qo'llab-quvvatlash", Text: "Virtual yordamchilar talabalarga kun-u tun xizmat ko'rsata oladi."},
				{Value: "3 ta", Heading: "Asosiy texnologiya", Text: "Mashinali o'qitish, tabiiy tilni qayta ishlash va kompyuter ko'rishi."},
			},
		},
		{
			Layout: LayoutBullets, Title: "Joriy etishda e'tibor beriladigan jihatlar",
			Lead: "Texnologiyani ta'lim jarayoniga kiritish puxta rejalashtirish va tayyorgarlikni talab qiladi.",
			Items: []Item{
				{Heading: "Pedagogik maqsad", Text: "Texnologiya aniq o'quv natijalariga erishishga xizmat qilishi va dars mazmuniga uyg'un bo'lishi kerak."},
				{Heading: "O'qituvchilar malakasi", Text: "Pedagoglar vositalardan to'g'ri foydalanish va natijalarni tanqidiy baholashni o'rganishlari zarur."},
				{Heading: "Ma'lumotlar himoyasi", Text: "O'quvchilar haqidagi ma'lumotlar qonunchilik talablari asosida xavfsiz saqlanishi lozim."},
				{Heading: "Akademik halollik", Text: "Topshiriqlar mustaqil fikrlashni rag'batlantiradigan va ko'chirmachilikning oldini oladigan tarzda tuziladi."},
			},
			Highlight: "Muvaffaqiyatli joriy etish texnologiyadan ko'ra ko'proq o'qituvchilar tayyorgarligi va aniq pedagogik maqsadlarga bog'liq.",
		},
		{
			Layout: LayoutConclusion, Title: "Xulosa",
			Lead: "Sun'iy intellekt ta'lim tizimini tubdan o'zgartirish salohiyatiga ega.",
			Items: []Item{
				{Heading: "Yangi imkoniyatlar", Text: "Sun'iy intellekt ta'limni shaxsiylashtirish va samaradorlikni oshirishga xizmat qiladi."},
				{Heading: "O'qituvchining roli", Text: "Pedagog bilim beruvchidan o'quv jarayonini yo'naltiruvchi murabbiyga aylanadi."},
				{Heading: "Mas'uliyatli foydalanish", Text: "Axloqiy me'yorlar va ma'lumotlar xavfsizligi doimiy nazoratda bo'lishi kerak."},
				{Heading: "Bosqichma-bosqich joriy etish", Text: "Tajriba-sinov loyihalari natijalari asosida keng qo'llashga o'tish maqsadga muvofiq."},
			},
			Highlight: "Sun'iy intellektdan oqilona foydalanish ta'lim sifatini oshirib, o'quvchilarni kelajak kasblariga tayyorlashda muhim omil bo'ladi.",
		},
		{
			Layout: LayoutTimeline, Title: "Muhim sanalar",
			Lead: "Bir necha tarixiy voqea sun'iy intellektning ilmiy g'oyadan kundalik vositaga aylanish yo'lini ko'rsatadi.",
			Items: []Item{
				{Value: "1956", Heading: "Dartmut konferensiyasi", Text: "Sun'iy intellekt atamasi ilk bor qo'llanib, yangi ilmiy yo'nalish shakllandi."},
				{Value: "1966", Heading: "ELIZA dasturi", Text: "Inson bilan matn orqali muloqot qila oladigan ilk dasturlardan biri yaratildi."},
				{Value: "1997", Heading: "Deep Blue g'alabasi", Text: "IBM kompyuteri shaxmat bo'yicha jahon chempioni Garri Kasparovni yengdi."},
				{Value: "2012", Heading: "Chuqur o'rganish", Text: "Neyron tarmoqlar tasvirlarni tanishda avvalgi usullardan keskin ustun keldi."},
				{Value: "2022", Heading: "Katta til modellari", Text: "Matn yaratuvchi modellar keng ommaga ochilib, ta'limda faol qo'llana boshladi."},
			},
		},
		{
			Layout: LayoutPyramid, Title: "Raqamli savodxonlik darajalari",
			Lead: "Sun'iy intellektdan samarali foydalanish bilimlarning bir-biriga tayanadigan darajalarini talab qiladi.",
			Items: []Item{
				{Heading: "Ijodiy qo'llash", Text: "Sun'iy intellekt yordamida yangi bilim, loyiha va yechimlar yaratish."},
				{Heading: "Tanqidiy baholash", Text: "Natijalarning to'g'riligi, xolisligi va manbalarini tekshira olish."},
				{Heading: "Amaliy foydalanish", Text: "Vositalardan o'quv topshiriqlarini bajarishda to'g'ri foydalanish."},
				{Heading: "Asosiy tushunchalar", Text: "Sun'iy intellekt qanday ishlashi va uning cheklovlarini bilish."},
			},
		},
		{
			Layout: LayoutMatrix, Title: "SWOT tahlili",
			Lead:      "Texnologiyani ta'limga joriy etishdan oldin uning ichki va tashqi omillarini baholash lozim.",
			Highlight: "SWOT",
			Items: []Item{
				{Heading: "Kuchli tomonlar", Text: "Shaxsiylashtirilgan ta'lim, tezkor fikr-mulohaza va o'qituvchi vaqtining tejalishi."},
				{Heading: "Zaif tomonlar", Text: "Texnik vositalarga bog'liqlik, xatoliklar ehtimoli va ma'lumotlar sifatiga sezgirlik."},
				{Heading: "Imkoniyatlar", Text: "Masofaviy va inklyuziv ta'limni kengaytirish, yangi o'quv kurslarini yaratish."},
				{Heading: "Tahdidlar", Text: "Akademik halollikning buzilishi, ma'lumotlar xavfsizligi va raqamli tengsizlik."},
			},
		},
		{
			Layout: LayoutCycle, Title: "Moslashuvchan ta'lim sikli",
			Lead:      "Har bir bosqich natijasi keyingisini belgilaydi va jarayon doimiy takrorlanadi.",
			Highlight: "O'quv sikli",
			Items: []Item{
				{Heading: "Tashxis", Text: "O'quvchining dastlabki bilim darajasi aniqlanadi."},
				{Heading: "Shaxsiy reja", Text: "Ehtiyojga mos topshiriqlar va sur'at tanlanadi."},
				{Heading: "O'qitish", Text: "Material interaktiv va bosqichma-bosqich o'rganiladi."},
				{Heading: "Baholash", Text: "Natijalar avtomatik tekshirilib, xatolar ko'rsatiladi."},
				{Heading: "Tahlil", Text: "Ma'lumotlar asosida keyingi qadam rejalashtiriladi."},
			},
		},
		{
			Layout: LayoutHub, Title: "Ta'lim ekotizimi",
			Lead:      "Sun'iy intellekt ta'lim jarayonining barcha ishtirokchilarini yagona tizimga bog'laydi.",
			Highlight: "Sun'iy intellekt",
			Items: []Item{
				{Heading: "O'quvchilar", Text: "Shaxsiy yordamchi va moslashuvchan topshiriqlar."},
				{Heading: "O'qituvchilar", Text: "Dars tayyorlash va baholashda yordam."},
				{Heading: "Ma'muriyat", Text: "Jadval tuzish va hujjatlarni boshqarish."},
				{Heading: "Ota-onalar", Text: "Farzand o'zlashtirishini kuzatish imkoniyati."},
				{Heading: "O'quv kontenti", Text: "Testlar, mashqlar va o'quv materiallari yaratish."},
				{Heading: "Tahlil", Text: "O'zlashtirish ko'rsatkichlarini kuzatish va prognoz."},
			},
		},
		{
			Layout: LayoutStatement, Title: "Asosiy g'oya",
			Highlight: "Sun'iy intellekt o'qituvchini almashtirmaydi — u har bir o'quvchiga e'tibor berish uchun o'qituvchiga vaqt va imkoniyat beradi.",
			Items: []Item{
				{Heading: "Vaqt", Text: "Tartibiy ishlar avtomatlashtiriladi."},
				{Heading: "E'tibor", Text: "O'qituvchi o'quvchilar bilan ko'proq ishlaydi."},
				{Heading: "Sifat", Text: "Ta'lim natijalari muntazam kuzatiladi."},
			},
		},
	}
}
