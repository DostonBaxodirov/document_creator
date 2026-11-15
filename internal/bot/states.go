package bot

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"telegram-presentation-bot/internal/ai"
	"telegram-presentation-bot/internal/storage"
)

// State — foydalanuvchining FSM (Finite State Machine) dagi holati.
// Qiymatlar bazada saqlanadi, shuning uchun ularni o'zgartirmang.
type State string

const (
	StateIdle       State = "idle"             // hech narsa kutilmayapti
	StateWorkType   State = "await_work_type"  // ish turi tanlanishi kutilmoqda (inline tugmalar)
	StateTopic      State = "await_topic"      // mavzu
	StateVolume     State = "await_volume"     // hajm: slaydlar (taqdimot) yoki sahifalar (hujjat) soni
	StateDesign     State = "await_design"     // taqdimot dizayni tanlanishi kutilmoqda (inline tugmalar)
	StateStudent    State = "await_student"    // talabaning F.I.Sh.
	StateUniversity State = "await_university" // universitet nomi
	StateTeacher    State = "await_teacher"    // tekshiruvchi (o'qituvchi) F.I.Sh.
	StateConfirm    State = "await_confirm"    // yig'ilgan ma'lumotlarni tasdiqlash
	StateGenerating State = "generating"       // AI + fayl (PPTX/DOCX) generatsiyasi ketmoqda
)

// FSM ketma-ketligi:
//
//	/start → WorkType → Topic → Volume → [Design] → Student → University → Teacher → Confirm → Generating → Idle
//
// Design qadami faqat taqdimot uchun. Matnli qadamlar (Topic..Teacher)
// quyidagi `steps` jadvalida tavsiflangan; WorkType, Design, Confirm va
// Generating holatlari inline tugmalar orqali boshqariladi.

// Inline tugmalar uchun callback identifikatorlari (telebot "unique" maydoni).
const (
	cbWorkType = "worktype"
	cbDesign   = "design"
	cbConfirm  = "confirm"
	cbRestart  = "restart"
	cbCancel   = "cancel"
)

// workTypeOption — ish turining foydalanuvchi interfeysidagi ko'rinishi.
// Taqdimot .pptx, mustaqil ish va amaliy topshiriq esa .docx bo'lib chiqadi
// (ai.WorkType.IsDocument).
type workTypeOption struct {
	Type   ai.WorkType
	Button string // tugmadagi matn
	Name   string // xulosada ko'rinadigan nom
	Title  string // titul slayd/varaqda ko'rinadigan nom
}

var workTypes = []workTypeOption{
	{Type: ai.WorkPresentation, Button: "📊 Slayd (taqdimot) — PowerPoint", Name: "Slayd (taqdimot)", Title: "TAQDIMOT"},
	{Type: ai.WorkIndependent, Button: "📚 Mustaqil ish — Word", Name: "Mustaqil ish", Title: "MUSTAQIL ISH"},
	{Type: ai.WorkPractical, Button: "🛠 Amaliy topshiriq — Word", Name: "Amaliy topshiriq", Title: "AMALIY TOPSHIRIQ"},
}

func findWorkType(t ai.WorkType) (workTypeOption, bool) {
	for _, o := range workTypes {
		if o.Type == t {
			return o, true
		}
	}
	return workTypeOption{}, false
}

// Limits — foydalanuvchi kiritadigan qiymatlar chegaralari.
type Limits struct {
	MinSlides int // taqdimot: umumiy slaydlar soni
	MaxSlides int
	MinPages  int // mustaqil ish / amaliy topshiriq: umumiy sahifalar soni
	MaxPages  int
}

// volumeSpec — ish turiga mos hajm birligi va chegaralari.
type volumeSpec struct {
	unit     string // "slayd" yoki "sahifa"
	min, max int
}

func volumeFor(l Limits, s *storage.Session) volumeSpec {
	if ai.WorkType(s.WorkType).IsDocument() {
		return volumeSpec{unit: "sahifa", min: l.MinPages, max: l.MaxPages}
	}
	return volumeSpec{unit: "slayd", min: l.MinSlides, max: l.MaxSlides}
}

// inputError — foydalanuvchiga ko'rsatiladigan validatsiya xatosi.
type inputError struct{ msg string }

func (e *inputError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &inputError{msg: fmt.Sprintf(format, args...)}
}

// step — matnli javob kutiladigan bitta FSM qadami.
type step struct {
	next State
	// route — keyingi qadam sessiyaga bog'liq bo'lsa (ixtiyoriy; next o'rniga).
	route func(s *storage.Session) State
	// prompt foydalanuvchiga beriladigan savol (HTML).
	prompt func(l Limits, s *storage.Session) string
	// suggest javob tugmalari (reply keyboard). nil yoki bo'sh bo'lsa — klaviatura olib tashlanadi.
	suggest func(l Limits, s *storage.Session) []string
	// apply javobni tekshiradi va sessiyaga yozadi; noto'g'ri bo'lsa *inputError qaytaradi.
	apply func(l Limits, s *storage.Session, input string) error
}

// steps — matnli qadamlar jadvali. Yangi qadam qo'shish uchun yangi State
// e'lon qilib, shu yerga yozuv qo'shish va oldingi qadamning `next` ini o'zgartirish kifoya.
var steps = map[State]step{
	StateTopic: {
		next: StateVolume,
		prompt: func(Limits, *storage.Session) string {
			return "📝 <b>Mavzuni kiriting.</b>\n\n<i>Masalan: Sun'iy intellektning ta'limdagi o'rni</i>"
		},
		apply: func(_ Limits, s *storage.Session, input string) error {
			v, err := validateText(input, "Mavzu", 3, maxTopicRunes)
			if err != nil {
				return err
			}
			s.Topic = v
			return nil
		},
	},
	StateVolume: {
		next: StateStudent,
		// Taqdimotda hajmdan keyin dizayn tanlanadi.
		route: func(s *storage.Session) State {
			if ai.WorkType(s.WorkType).IsDocument() {
				return StateStudent
			}
			return StateDesign
		},
		prompt: func(l Limits, s *storage.Session) string {
			v := volumeFor(l, s)
			if v.unit == "sahifa" {
				return fmt.Sprintf("📄 <b>Necha sahifa bo'lsin?</b>\n\n"+
					"%d dan %d gacha son kiriting yoki tugmalardan tanlang.\n"+
					"<i>Titul varaq, reja va adabiyotlar ro'yxati ham shu songa kiradi. "+
					"Rasmiylashtirish: Times New Roman 14, interval 1,5.</i>", v.min, v.max)
			}
			return fmt.Sprintf("🔢 <b>Nechta slayd bo'lsin?</b>\n\n"+
				"%d dan %d gacha son kiriting yoki tugmalardan tanlang.\n"+
				"<i>Titul va yakuniy slayd ham shu songa kiradi.</i>", v.min, v.max)
		},
		suggest: func(l Limits, s *storage.Session) []string {
			v := volumeFor(l, s)
			var out []string
			for _, n := range []int{5, 8, 10, 12, 15, 20} {
				if n >= v.min && n <= v.max {
					out = append(out, strconv.Itoa(n))
				}
			}
			return out
		},
		apply: func(l Limits, s *storage.Session, input string) error {
			v := volumeFor(l, s)
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil {
				return invalid("Iltimos, faqat son kiriting (masalan: 10).")
			}
			if n < v.min || n > v.max {
				return invalid("%s soni %d dan %d gacha bo'lishi kerak.", capitalize(v.unit)+"lar", v.min, v.max)
			}
			s.Volume = n
			return nil
		},
	},
	StateStudent: {
		next: StateUniversity,
		prompt: func(Limits, *storage.Session) string {
			return "👤 <b>Ism-familiyangizni kiriting.</b>\n\n<i>Masalan: Aliyev Vali</i>"
		},
		suggest: func(_ Limits, s *storage.Session) []string { return nonEmpty(s.StudentName) },
		apply: func(_ Limits, s *storage.Session, input string) error {
			v, err := validatePersonName(input, "Ism-familiya")
			if err != nil {
				return err
			}
			s.StudentName = v
			return nil
		},
	},
	StateUniversity: {
		next: StateTeacher,
		prompt: func(Limits, *storage.Session) string {
			return "🏛 <b>Universitet (o'quv yurti) nomini kiriting.</b>\n\n<i>Masalan: Toshkent axborot texnologiyalari universiteti</i>"
		},
		suggest: func(_ Limits, s *storage.Session) []string { return nonEmpty(s.University) },
		apply: func(_ Limits, s *storage.Session, input string) error {
			v, err := validateText(input, "Universitet nomi", 2, 150)
			if err != nil {
				return err
			}
			s.University = v
			return nil
		},
	},
	StateTeacher: {
		next: StateConfirm,
		prompt: func(Limits, *storage.Session) string {
			return "👨‍🏫 <b>Tekshiruvchi (o'qituvchi) F.I.Sh. ni kiriting.</b>\n\n<i>Masalan: Karimova Dilnoza</i>"
		},
		suggest: func(_ Limits, s *storage.Session) []string { return nonEmpty(s.TeacherName) },
		apply: func(_ Limits, s *storage.Session, input string) error {
			v, err := validatePersonName(input, "Tekshiruvchi F.I.Sh.")
			if err != nil {
				return err
			}
			s.TeacherName = v
			return nil
		},
	},
}

// nextState javobdan keyingi qadamni qaytaradi.
func (st step) nextState(s *storage.Session) State {
	if st.route != nil {
		return st.route(s)
	}
	return st.next
}

// resetDraft joriy buyurtma maydonlarini tozalaydi. Profil maydonlari
// (talaba, universitet, tekshiruvchi) keyingi safar taklif qilish uchun qoladi.
func resetDraft(s *storage.Session) {
	s.WorkType = ""
	s.Topic = ""
	s.Volume = 0
}

// ---------------------------------------------------------------------------
// Validatsiya
// ---------------------------------------------------------------------------

// normalizeSpaces ketma-ket bo'sh joy va qator ko'chirishlarni bitta bo'sh joyga aylantiradi.
func normalizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// maxTopicRunes — mavzu uzunligi chegarasi (bazada Session.Topic — varchar(512)).
const maxTopicRunes = 500

// validateText umumiy matn maydonini tekshiradi: uzunlik va kamida 2 ta harf.
func validateText(input, field string, minRunes, maxRunes int) (string, error) {
	v := normalizeSpaces(input)
	n := utf8.RuneCountInString(v)
	switch {
	case n < minRunes:
		return "", invalid("%s juda qisqa (kamida %d ta belgi).", field, minRunes)
	case n > maxRunes:
		return "", invalid("%s juda uzun (ko'pi bilan %d ta belgi).", field, maxRunes)
	case countLetters(v) < 2:
		return "", invalid("%s harflardan iborat bo'lishi kerak.", field)
	}
	return v, nil
}

// validatePersonName F.I.Sh. ni tekshiradi: raqam va havolalarsiz, faqat
// harflar, bo'sh joy, apostrof (O'tkir, G'ulom), nuqta (initsiallar) va chiziqcha.
func validatePersonName(input, field string) (string, error) {
	v, err := validateText(input, field, 3, 100)
	if err != nil {
		return "", err
	}
	for _, r := range v {
		switch {
		case unicode.IsLetter(r), r == ' ', r == '.', r == '-':
		case r == '\'', r == '`', r == '’', r == 'ʻ', r == 'ʼ', r == '‘':
		default:
			return "", invalid("%s faqat harflardan iborat bo'lishi kerak (raqam va maxsus belgilarsiz).", field)
		}
	}
	return v, nil
}

func countLetters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

// capitalize birinchi harfni bosh harfga aylantiradi.
func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

func nonEmpty(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}
