package generator

// SampleDeck barcha maketlarni o'z ichiga olgan namunaviy taqdimotni
// qaytaradi. AI va Telegram'siz dizaynni tekshirish (cmd/preview), mavzular
// ko'rinishi rasmlarini tayyorlash va testlar uchun ishlatiladi.
func SampleDeck(themeID string) Deck {
	return Deck{
		Topic:       "Sun'iy intellektning ta'limdagi o'rni",
		Subtitle:    "Imkoniyatlar, xavflar va joriy etish yo'llari",
		WorkType:    "TAQDIMOT",
		StudentName: "Aliyev Vali",
		University:  "Toshkent axborot texnologiyalari universiteti",
		TeacherName: "Karimova Dilnoza",
		Year:        "2026",
		Theme:       themeID,
		Slides: []Slide{
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
		},
	}
}
