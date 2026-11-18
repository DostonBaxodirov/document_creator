package bot

import (
	"strings"
	"testing"
	"time"

	"telegram-presentation-bot/internal/backup"
	"telegram-presentation-bot/internal/payment"
)

func TestNextBackup(t *testing.T) {
	tz := payment.Tashkent
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"bugun hali vaqti kelmagan", time.Date(2026, 11, 19, 1, 0, 0, 0, tz), time.Date(2026, 11, 19, 3, 0, 0, 0, tz)},
		{"vaqti o'tgan — ertaga", time.Date(2026, 11, 19, 10, 0, 0, 0, tz), time.Date(2026, 11, 20, 3, 0, 0, 0, tz)},
		{"aynan shu daqiqa — ertaga", time.Date(2026, 11, 19, 3, 0, 0, 0, tz), time.Date(2026, 11, 20, 3, 0, 0, 0, tz)},
		// UTC 18-noyabr 21:30 = Toshkent 19-noyabr 02:30.
		{"UTC da kecha", time.Date(2026, 11, 18, 21, 30, 0, 0, time.UTC), time.Date(2026, 11, 19, 3, 0, 0, 0, tz)},
		{"oy oxiri", time.Date(2026, 11, 30, 23, 0, 0, 0, tz), time.Date(2026, 12, 1, 3, 0, 0, 0, tz)},
	}
	for _, c := range cases {
		if got := nextBackup(c.now, 3, 0); !got.Equal(c.want) {
			t.Errorf("%s: %v, kutilgan %v", c.name, got, c.want)
		}
	}
	if got := nextBackup(time.Date(2026, 11, 19, 23, 50, 0, 0, tz), 23, 59); !got.Equal(time.Date(2026, 11, 19, 23, 59, 0, 0, tz)) {
		t.Errorf("23:59: %v", got)
	}
}

func TestBackupCaption(t *testing.T) {
	f := &backup.File{Name: "backup_presentation_bot_2026-11-19_0300.sql.gz", Size: 3 * 1024 * 1024 / 2}
	c := backupCaption(titleDailyBackup, f, time.Date(2026, 11, 18, 22, 0, 0, 0, time.UTC))
	for _, want := range []string{"Kunlik backup", "19.11.2026 03:00", "1.5 MB", "docs/DEPLOY.md"} {
		if !strings.Contains(c, want) {
			t.Errorf("izohda %q yo'q: %s", want, c)
		}
	}
	if n := len([]rune(c)); n > 1024 {
		t.Errorf("izoh Telegram chegarasidan uzun: %d", n)
	}
	for n, want := range map[int64]string{512: "512 B", 3500: "3.4 KB", 50 << 20: "50.0 MB"} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %s, kutilgan %s", n, got, want)
		}
	}
	if !strings.Contains(msgAdminCommands, "/backup") {
		t.Error("admin buyruqlari ro'yxatida /backup yo'q")
	}
}
