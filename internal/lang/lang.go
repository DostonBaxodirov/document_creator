// Package lang ish (taqdimot yoki hujjat) yoziladigan tillar va ularga mos
// qotirilgan yozuvlar: titul varaq/slayddagi "Bajardi", "Tekshirdi",
// hujjat bo'limlari sarlavhalari, yakuniy slayddagi minnatdorchilik va h.k.
//
// Bo'sh yoki noma'lum til kodi o'zbek (lotin) tili deb hisoblanadi — til
// tanlash qadami qo'shilishidan oldingi sessiya va buyurtmalar ham shu tufayli
// avvalgidek ishlaydi.
package lang

// Code — til identifikatori (bazada saqlanadi, qiymatlarni o'zgartirmang).
type Code string

const (
	UzLatn Code = "uz"      // o'zbek tili, lotin yozuvi
	UzCyrl Code = "uz-cyrl" // o'zbek tili, kirill yozuvi
	Ru     Code = "ru"      // rus tili
	En     Code = "en"      // ingliz tili
)

// Labels — fayldagi qotirilgan yozuvlar.
type Labels struct {
	Student string // titulda talaba oldidagi yozuv (ikki nuqtasiz): "Bajardi"
	Teacher string // tekshiruvchi oldidagi yozuv: "Tekshirdi"
	Year    string // "Yil"
	Thanks  string // yakuniy slayd: "E'tiboringiz uchun rahmat!"
	Topic   string // hujjat titulida mavzu oldidagi yozuv: "Mavzu"

	// Hujjat bo'limlari (reja ichida — shu ko'rinishda, sarlavhada — katta harflar bilan).
	Plan         string
	Introduction string
	Conclusion   string
	References   string

	// YearFormat — hujjat titulidagi yil (fmt formati, %s — yil): "%s-yil".
	YearFormat string

	// Ish turlarining titulda ko'rinadigan nomlari.
	TitlePresentation string
	TitleIndependent  string
	TitlePractical    string
	TitleReferat      string

	// FigureFormat — hujjatdagi rasm raqami (fmt formati, %d — raqam): "%d-rasm.".
	FigureFormat string

	// Ministry — hujjat titulining yuqori qatori (vazirlik nomi) shu tilda.
	// O'zbekcha (lotin) qiymati DOCUMENT_HEADER ning standart qiymati bilan bir
	// xil: sozlamada standart nom turgan bo'lsa, boshqa tillarda tarjimasi chiqadi.
	Ministry string
}

// Info — bitta til haqida ma'lumot.
type Info struct {
	Code   Code
	Button string // tanlash tugmasidagi matn
	Name   string // foydalanuvchiga ko'rinadigan nom (xulosada)
	Prompt string // AI promptidagi inglizcha nom
	OOXML  string // Word/PowerPoint imlo tekshiruvi tili
	Labels Labels
}

var all = []Info{
	{
		Code: UzLatn, Button: "🇺🇿 O'zbekcha (lotin)", Name: "O'zbekcha (lotin)",
		Prompt: "Uzbek (Latin script)", OOXML: "uz-Latn-UZ",
		Labels: Labels{
			Student: "Bajardi", Teacher: "Tekshirdi", Year: "Yil",
			Thanks: "E'tiboringiz uchun rahmat!", Topic: "Mavzu",
			Plan: "Reja", Introduction: "Kirish", Conclusion: "Xulosa",
			References: "Foydalanilgan adabiyotlar", YearFormat: "%s-yil",
			TitlePresentation: "TAQDIMOT", TitleIndependent: "MUSTAQIL ISH", TitlePractical: "AMALIY TOPSHIRIQ",
			TitleReferat: "REFERAT", FigureFormat: "%d-rasm.",
			Ministry: "O'ZBEKISTON RESPUBLIKASI OLIY TA'LIM, FAN VA INNOVATSIYALAR VAZIRLIGI",
		},
	},
	{
		Code: UzCyrl, Button: "🇺🇿 Ўзбекча (кирилл)", Name: "Ўзбекча (кирилл)",
		Prompt: "Uzbek (Cyrillic script)", OOXML: "uz-Cyrl-UZ",
		Labels: Labels{
			Student: "Бажарди", Teacher: "Текширди", Year: "Йил",
			Thanks: "Эътиборингиз учун раҳмат!", Topic: "Мавзу",
			Plan: "Режа", Introduction: "Кириш", Conclusion: "Хулоса",
			References: "Фойдаланилган адабиётлар", YearFormat: "%s йил",
			TitlePresentation: "ТАҚДИМОТ", TitleIndependent: "МУСТАҚИЛ ИШ", TitlePractical: "АМАЛИЙ ТОПШИРИҚ",
			TitleReferat: "РЕФЕРАТ", FigureFormat: "%d-расм.",
			Ministry: "ЎЗБЕКИСТОН РЕСПУБЛИКАСИ ОЛИЙ ТАЪЛИМ, ФАН ВА ИННОВАЦИЯЛАР ВАЗИРЛИГИ",
		},
	},
	{
		Code: Ru, Button: "🇷🇺 Русский", Name: "Русский",
		Prompt: "Russian", OOXML: "ru-RU",
		Labels: Labels{
			Student: "Выполнил(а)", Teacher: "Проверил(а)", Year: "Год",
			Thanks: "Спасибо за внимание!", Topic: "Тема",
			Plan: "План", Introduction: "Введение", Conclusion: "Заключение",
			References: "Список использованной литературы", YearFormat: "%s г.",
			TitlePresentation: "ПРЕЗЕНТАЦИЯ", TitleIndependent: "САМОСТОЯТЕЛЬНАЯ РАБОТА", TitlePractical: "ПРАКТИЧЕСКОЕ ЗАДАНИЕ",
			TitleReferat: "РЕФЕРАТ", FigureFormat: "Рис. %d.",
			Ministry: "МИНИСТЕРСТВО ВЫСШЕГО ОБРАЗОВАНИЯ, НАУКИ И ИННОВАЦИЙ РЕСПУБЛИКИ УЗБЕКИСТАН",
		},
	},
	{
		Code: En, Button: "🇬🇧 English", Name: "English",
		Prompt: "English", OOXML: "en-US",
		Labels: Labels{
			Student: "Prepared by", Teacher: "Supervisor", Year: "Year",
			Thanks: "Thank you for your attention!", Topic: "Topic",
			Plan: "Outline", Introduction: "Introduction", Conclusion: "Conclusion",
			References: "References", YearFormat: "%s",
			TitlePresentation: "PRESENTATION", TitleIndependent: "INDEPENDENT STUDY", TitlePractical: "PRACTICAL ASSIGNMENT",
			TitleReferat: "ESSAY", FigureFormat: "Figure %d.",
			Ministry: "MINISTRY OF HIGHER EDUCATION, SCIENCE AND INNOVATIONS OF THE REPUBLIC OF UZBEKISTAN",
		},
	},
}

// All barcha tillarni tanlash tugmalari tartibida qaytaradi.
func All() []Info {
	out := make([]Info, len(all))
	copy(out, all)
	return out
}

// Find til kodini qidiradi.
func Find(code string) (Info, bool) {
	for _, l := range all {
		if string(l.Code) == code {
			return l, true
		}
	}
	return Info{}, false
}

// Get til ma'lumotini qaytaradi; bo'sh yoki noma'lum kod — o'zbek (lotin).
func Get(code string) Info {
	if l, ok := Find(code); ok {
		return l
	}
	return all[0]
}
