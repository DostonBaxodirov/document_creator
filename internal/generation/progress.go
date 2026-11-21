package generation

// Stage — jarayon xabaridagi bosqich: belgi va holatlar nomlari.
type Stage struct {
	Icon    string
	First   string // done == 0
	Writing string // 0 < done < total
	Last    string // done == total
}

// Progress — generatsiya jarayonini foydalanuvchiga ko'rsatuvchi (Telegram'da
// — butun ish davomida yangilanib turadigan bitta xabar, internal/bot). Bir
// vaqtda bir nechta bosqich (masalan, matn yozish va rasm chizish) faol
// bo'lishi mumkin.
type Progress interface {
	// Stage yangi bosqichni ro'yxatga oladi. report AI va rasm
	// goroutine'laridan chaqiriladi: u bloklanmasligi va bir vaqtda ko'p
	// goroutine'dan chaqirilishi mumkin bo'lishi shart. done bosqichni
	// yakunlaydi; bir necha marta chaqirilsa ham xavfsiz bo'lishi kerak.
	Stage(st Stage) (report func(done, total int), done func())
}

// TextStage — AI matn yozish bosqichi (writing — "Bo'limlar yozilmoqda..." kabi).
func TextStage(writing string) Stage {
	return Stage{Icon: "✍️", First: "Reja tuzilmoqda...", Writing: writing, Last: "Fayl tayyorlanmoqda..."}
}

var (
	// ImageStage — rasmli dizaynda rasmlarni chizish bosqichi.
	ImageStage = Stage{Icon: "🖼", First: "Rasmlar chizilmoqda...", Writing: "Rasmlar chizilmoqda...", Last: "Slaydlar tayyorlanmoqda..."}
	// DocImageStage — Word ish rasmlarini chizish bosqichi.
	DocImageStage = Stage{Icon: "🖼", First: "Rasmlar chizilmoqda...", Writing: "Rasmlar chizilmoqda...", Last: "Hujjat tayyorlanmoqda..."}
	// DesignStage — dizayn konsepsiyasini yaratish bosqichi.
	DesignStage = Stage{Icon: "🎨", First: "Mavzuga mos dizayn yaratilmoqda...", Writing: "Dizayn yaratilmoqda...", Last: "Dizayn tayyor"}
)
