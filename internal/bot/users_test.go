package bot

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"
	"time"

	"telegram-presentation-bot/internal/storage"
	"telegram-presentation-bot/internal/tz"
)

func sampleUsers() []storage.UserSummary {
	at := func(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, tz.Tashkent) }
	visit := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC) // date ustuni: UTC yarim tun
	return []storage.UserSummary{
		{UserID: 1001, Username: "ali", FirstName: "Ali <Vali>", StudentName: "Aliyev Vali", University: "TDIU",
			Language: "ru", IsAdmin: true, CreatedAt: at(17, 9), UpdatedAt: at(17, 10), LastVisit: &visit,
			Delivered: 3, Payments: 2, PaidSum: 25000},
		{UserID: 1002, StudentName: "Karimova Dilnoza", CreatedAt: at(10, 12), UpdatedAt: at(12, 8), Delivered: 1},
		{UserID: 1003, CreatedAt: at(1, 7), UpdatedAt: at(1, 7)},
	}
}

func TestUsersText(t *testing.T) {
	p, _ := findPeriod("week")
	txt := usersText(sampleUsers(), p, time.Date(2026, 11, 18, 21, 40, 0, 0, tz.Tashkent))
	for _, want := range []string{
		"Foydalanuvchilar — oxirgi 7 kun", "18.11.2026 21:40",
		"Jami: <b>3</b>", "Ish olgan: <b>2</b>", "To'lov qilgan: <b>1</b> · 25 000 so'm",
		// Ism HTML dan ekranlangan, profilga havola, admin belgisi.
		`1. 👑 <a href="tg://user?id=1001">Ali &lt;Vali&gt;</a> (@ali) · <code>1001</code> · 📅 17.11.2026 · 👁 18.11.2026 · 📦 3 · 💰 25 000 so'm`,
		// Telegram ismi yo'q — titul F.I.Sh.; tashrif yozuvi yo'q — sessiya vaqti.
		`2. <a href="tg://user?id=1002">Karimova Dilnoza</a> · <code>1002</code> · 📅 10.11.2026 · 👁 12.11.2026 · 📦 1`,
		`3. <a href="tg://user?id=1003">Foydalanuvchi</a> · <code>1003</code> · 📅 01.11.2026 · 👁 01.11.2026`,
		"👁 — oxirgi faollik",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("ro'yxatda %q yo'q:\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "1003</code> · 📅 01.11.2026 · 👁 01.11.2026 · 📦") {
		t.Error("ishi yo'q foydalanuvchida 📦 ko'rsatilmasligi kerak")
	}

	empty := usersText(nil, p, time.Now())
	if !strings.Contains(empty, "Jami: <b>0</b>") || !strings.Contains(empty, "yangi foydalanuvchi kirmagan") {
		t.Errorf("bo'sh ro'yxat matni: %s", empty)
	}

	// Ko'p foydalanuvchi — Telegram chegarasiga sig'adi, qolgani CSV ga yo'naltiriladi.
	many := make([]storage.UserSummary, 500)
	for i := range many {
		many[i] = storage.UserSummary{UserID: int64(10000 + i), Username: fmt.Sprintf("user_%d", i), FirstName: "Talaba",
			CreatedAt: time.Now(), UpdatedAt: time.Now()}
	}
	long := usersText(many, p, time.Now())
	if n := len([]rune(long)); n > 4096 {
		t.Errorf("xabar Telegram chegarasidan uzun: %d", n)
	}
	if !strings.Contains(long, "To'liq ro'yxat uchun CSV ni tanlang") || !strings.Contains(long, "Jami: <b>500</b>") {
		t.Error("qirqilgan ro'yxatda CSV ga ishora va umumiy son bo'lishi kerak")
	}
	if !strings.Contains(msgAdminCommands, "/foydalanuvchilar") {
		t.Error("admin buyruqlari ro'yxatida /foydalanuvchilar yo'q")
	}
}

func TestUsersCSV(t *testing.T) {
	data, err := usersCSV(sampleUsers())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("CSV UTF-8 BOM bilan boshlanishi kerak")
	}
	rows, err := csv.NewReader(bytes.NewReader(data[3:])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || strings.Join(rows[0], ",") != strings.Join(usersCSVHeader, ",") {
		t.Fatalf("CSV sarlavhasi yoki qatorlar soni noto'g'ri: %v", rows)
	}
	want := []string{"1001", "ali", "Ali <Vali>", "Aliyev Vali", "TDIU", "ru", "true", "2026-11-17 09:00:00", "2026-11-18", "3", "2", "25000"}
	if strings.Join(rows[1], "|") != strings.Join(want, "|") {
		t.Errorf("1-qator:\n%v\nkutilgan:\n%v", rows[1], want)
	}
	if rows[3][5] != "uz" || rows[3][6] != "false" {
		t.Errorf("til ko'rsatilmagan bo'lsa — uz: %v", rows[3])
	}
}

func TestLastActive(t *testing.T) {
	loc := tz.Tashkent
	visit := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	u := storage.UserSummary{UpdatedAt: time.Date(2026, 11, 15, 23, 0, 0, 0, time.UTC), LastVisit: &visit}
	if got := u.LastActive(loc); !got.Equal(time.Date(2026, 11, 18, 0, 0, 0, 0, loc)) {
		t.Errorf("oxirgi tashrif kuni olinishi kerak: %v", got)
	}
	// Sessiya tashrifdan keyin o'zgargan (UTC 15-noyabr 23:00 = Toshkentda 16-noyabr).
	old := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	u.LastVisit = &old
	if got := u.LastActive(loc); !got.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, loc)) {
		t.Errorf("kechroq sana olinishi kerak: %v", got)
	}
}
