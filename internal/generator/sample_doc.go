package generator

// Namunaviy Word hujjat: dizaynlarni AI va Telegram'siz ko'rish (cmd/preview
// -docs, scripts/doc_design_preview.py) va testlar uchun.

// SampleDocSpecs — AI maxsus Word dizayni namunalari (AI qaytaradigan kabi).
func SampleDocSpecs() []DocSpec {
	return []DocSpec{
		{Name: "Ipak yo'li", Primary: "#7A2E1D", Accent: "#C08A2E", HeadingFont: "Book Antiqua",
			Title: docTitleFrame, Heading: docHeadingCenter, Border: docBorderThinThick, Ornament: docOrnamentDiamond},
		{Name: "Raqamli davr", Primary: "#123B6D", Accent: "#1E88E5", HeadingFont: "Segoe UI",
			Title: docTitleBand, Heading: docHeadingBand, Border: docBorderNone, Ornament: docOrnamentNone},
		{Name: "Yashil iqtisodiyot", Primary: "#1B5E20", Accent: "#66BB6A", HeadingFont: "Century Gothic",
			Title: docTitleSidebar, Heading: docHeadingBar, Border: docBorderSingle, Ornament: docOrnamentDots},
	}
}

// SamplePaper — namunaviy hujjat (o'zbekcha). figures=true bo'lsa, 1- va
// 3-boblarga namunaviy rasm qo'yiladi (maxsus dizayndagi kabi).
func SamplePaper(design string, custom *DocSpec, figures bool) Paper {
	para := func(s string) Paragraph { return Paragraph{Text: s} }
	p := Paper{
		Topic:       "Raqamli iqtisodiyotning rivojlanish istiqbollari",
		WorkType:    "MUSTAQIL ISH",
		StudentName: "Aliyev Vali",
		University:  "Toshkent davlat iqtisodiyot universiteti",
		TeacherName: "Karimova Dilnoza",
		Year:        "2026",
		Design:      design,
		Custom:      custom,
		Introduction: []Paragraph{
			para("Raqamli iqtisodiyot bugungi kunda davlatlarning raqobatbardoshligini belgilovchi asosiy omillardan biriga aylandi. Axborot-kommunikatsiya texnologiyalarining jadal rivojlanishi ishlab chiqarish, savdo va xizmat ko'rsatish sohalarida tub o'zgarishlarni yuzaga keltirmoqda."),
			para("Ishning maqsadi raqamli iqtisodiyotning nazariy asoslarini o'rganish, uning O'zbekistondagi holatini tahlil qilish va rivojlanish istiqbollarini aniqlashdan iborat. Shu maqsadda raqamli texnologiyalarning iqtisodiy o'sishga ta'siri, mavjud muammolar va ularni hal etish yo'llari ko'rib chiqiladi."),
		},
		Chapters: []Section{
			{Title: "Raqamli iqtisodiyotning mohiyati va asosiy tushunchalari", Paragraphs: []Paragraph{
				para("Raqamli iqtisodiyot deganda iqtisodiy faoliyatning raqamli texnologiyalar, ma'lumotlar va onlayn platformalar asosida tashkil etilishi tushuniladi. Uning asosiy unsurlari elektron tijorat, raqamli to'lov tizimlari, bulutli hisoblash va sun'iy tarmoqlar orqali ko'rsatiladigan xizmatlardir."),
				para("Raqamli iqtisodiyotning an'anaviy iqtisodiyotdan farqi shundaki, unda ma'lumotlar asosiy resurs sifatida namoyon bo'ladi. Ma'lumotlarni yig'ish, qayta ishlash va tahlil qilish korxonalarga mijozlar ehtiyojini aniq bilish va xarajatlarni kamaytirish imkonini beradi."),
				para("Mazkur jarayonlar mehnat bozorida ham sezilarli o'zgarishlarga olib kelmoqda: yangi kasblar paydo bo'lmoqda, masofaviy ish shakllari kengaymoqda va raqamli ko'nikmalarga talab ortmoqda."),
			}},
			{Title: "O'zbekistonda raqamli iqtisodiyotning hozirgi holati", Paragraphs: []Paragraph{
				para("So'nggi yillarda O'zbekistonda raqamli iqtisodiyotni rivojlantirish davlat siyosatining ustuvor yo'nalishlaridan biri sifatida belgilandi. \"Raqamli O'zbekiston — 2030\" strategiyasi doirasida davlat xizmatlarini raqamlashtirish, internet tarmog'ini kengaytirish va IT-parklarni rivojlantirish bo'yicha keng ko'lamli ishlar amalga oshirilmoqda."),
				para("Elektron davlat xizmatlari portali orqali aholiga ko'rsatilayotgan xizmatlar soni muntazam ortib bormoqda. Bank sohasida mobil ilovalar va masofaviy xizmatlar keng ommalashdi, naqd pulsiz to'lovlar ulushi sezilarli darajada oshdi."),
			}},
			{Title: "Raqamli iqtisodiyotni rivojlantirish istiqbollari", Paragraphs: []Paragraph{
				para("Raqamli iqtisodiyotni yanada rivojlantirish uchun raqamli infratuzilmani kengaytirish, kadrlar tayyorlash tizimini takomillashtirish va kiberxavfsizlikni ta'minlash muhim ahamiyatga ega. Ayniqsa, hududlarda yuqori tezlikdagi internetdan foydalanish imkoniyatini oshirish dolzarb vazifa hisoblanadi."),
				para("Kelgusida sun'iy tarmoqlar, katta ma'lumotlar tahlili va avtomatlashtirilgan boshqaruv tizimlarining iqtisodiyotdagi ulushi ortishi kutilmoqda. Bu esa mehnat unumdorligini oshirish va yangi bozorlarga chiqish uchun keng imkoniyatlar yaratadi."),
			}},
		},
		Conclusion: []Paragraph{
			para("Olib borilgan tahlil raqamli iqtisodiyot mamlakat iqtisodiy o'sishining muhim manbai ekanini ko'rsatdi. Raqamli infratuzilmani rivojlantirish, malakali kadrlar tayyorlash va qulay huquqiy muhit yaratish ushbu sohaning barqaror rivojlanishini ta'minlaydi."),
		},
		References: []string{
			"O'zbekiston Respublikasi Prezidentining \"Raqamli O'zbekiston — 2030\" strategiyasini tasdiqlash to'g'risida\"gi Farmoni. — Toshkent, 2020.",
			"Gulyamov S.S. va boshq. Raqamli iqtisodiyot. — Toshkent: Iqtisodiyot, 2021.",
			"Tapscott D. The Digital Economy. — New York: McGraw-Hill, 2014.",
		},
	}
	if figures {
		p.Chapters[0].Figure = &Figure{Data: SamplePicture(1536, 1024, "1F4E79", "8EC9E8").Data,
			Caption: "Raqamli iqtisodiyotning asosiy unsurlari"}
		p.Chapters[2].Figure = &Figure{Data: SamplePicture(1536, 1024, "2E7D32", "F9A825").Data,
			Caption: "Raqamli texnologiyalarning rivojlanish yo'nalishlari"}
	}
	return p
}
