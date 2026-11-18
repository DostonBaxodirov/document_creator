package generator

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// slidesText taqdimotdagi barcha slaydlar XML matni.
func slidesText(files map[string][]byte) string {
	var b strings.Builder
	for name, data := range files {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
			b.Write(data)
		}
	}
	return b.String()
}

// TestDeckLabelsFollowLanguage — titul va yakuniy slayd yozuvlari taqdimot
// tilida chiqadi; til berilmasa — avvalgidek o'zbekcha.
func TestDeckLabelsFollowLanguage(t *testing.T) {
	g := newTestPPTX(t)
	for _, info := range Themes() {
		t.Run(info.ID, func(t *testing.T) {
			d := SampleDeck(info.ID)
			d.Lang = "ru"
			d.WorkType = "ПРЕЗЕНТАЦИЯ"
			files := buildAndRead(t, g, d)
			checkWellFormed(t, files)
			text := slidesText(files)
			if !strings.Contains(text, "Выполнил(а)") && !strings.Contains(text, "ВЫПОЛНИЛ(А)") {
				t.Errorf("ruscha talaba yozuvi yo'q")
			}
			if !strings.Contains(text, "Спасибо за внимание!") {
				t.Errorf("ruscha minnatdorchilik yo'q")
			}
			for _, unwanted := range []string{"Bajardi", "BAJARDI", "Tekshirdi", "TEKSHIRDI", "tiboringiz uchun rahmat"} {
				if strings.Contains(text, unwanted) {
					t.Errorf("ruscha taqdimotda o'zbekcha yozuv qolgan: %q", unwanted)
				}
			}

			uz := slidesText(buildAndRead(t, g, SampleDeck(info.ID)))
			if !strings.Contains(uz, "tiboringiz uchun rahmat!") {
				t.Errorf("standart (o'zbekcha) minnatdorchilik yo'q")
			}
		})
	}
}

func testPaper(lang string) Paper {
	return Paper{
		Topic: "Mavzu", WorkType: "MUSTAQIL ISH", StudentName: "Aliyev Vali", University: "TATU",
		TeacherName: "Karimova Dilnoza", Year: "2026", Lang: lang,
		Introduction: []Paragraph{{Text: "Kirish matni."}},
		Chapters:     []Section{{Title: "Birinchi bob", Paragraphs: []Paragraph{{Text: "Bob matni."}}}},
		Conclusion:   []Paragraph{{Text: "Xulosa matni."}},
		References:   []string{"Manba."},
	}
}

func TestPaperLabelsFollowLanguage(t *testing.T) {
	g, err := NewDOCX(t.TempDir(), "", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	build := func(lang string) map[string][]byte {
		path, err := g.Build(context.Background(), testPaper(lang))
		if err != nil {
			t.Fatalf("Build(%q): %v", lang, err)
		}
		files := readPackage(t, path)
		checkWellFormed(t, files)
		return files
	}

	ru := build("ru")
	doc := string(ru[docDocumentPath])
	for _, want := range []string{"Тема: ", "Выполнил(а): ", "Проверил(а): ", "2026 г.", "ПЛАН", "ВВЕДЕНИЕ", "ЗАКЛЮЧЕНИЕ", "СПИСОК ИСПОЛЬЗОВАННОЙ ЛИТЕРАТУРЫ", ">Введение<"} {
		if !strings.Contains(doc, want) {
			t.Errorf("ruscha hujjatda %q yo'q", want)
		}
	}
	for _, unwanted := range []string{"Bajardi", "KIRISH", "XULOSA", "-yil"} {
		if strings.Contains(doc, unwanted) {
			t.Errorf("ruscha hujjatda o'zbekcha yozuv qolgan: %q", unwanted)
		}
	}
	if styles := string(ru[docStylesPath]); !strings.Contains(styles, `w:val="ru-RU"`) || strings.Contains(styles, "uz-Latn-UZ") {
		t.Errorf("styles.xml imlo tili ru-RU emas")
	}

	uz := build("")
	doc = string(uz[docDocumentPath])
	for _, want := range []string{"Mavzu: ", "Bajardi: ", "Tekshirdi: ", "2026-yil", "REJA", "KIRISH", "XULOSA", "FOYDALANILGAN ADABIYOTLAR"} {
		if !strings.Contains(doc, want) {
			t.Errorf("standart (o'zbekcha) hujjatda %q yo'q", want)
		}
	}
	if !strings.Contains(string(uz[docStylesPath]), `w:val="uz-Latn-UZ"`) {
		t.Errorf("standart imlo tili uz-Latn-UZ emas")
	}
}
