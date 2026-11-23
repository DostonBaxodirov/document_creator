package bot

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/textutil"
)

// ---------------------------------------------------------------------------
// Formatlash
// ---------------------------------------------------------------------------

// dateTimeLayout — xabarlardagi sana-vaqt ko'rinishi.
const dateTimeLayout = "02.01.2006 15:04"

// formatSum summani "10 000 so'm" ko'rinishida qaytaradi.
func formatSum(n int) string {
	return groupDigits(strconv.Itoa(n), 3, true) + " so'm"
}

// formatCard karta raqamini 4 tadan guruhlaydi: "8600 1234 5678 9012".
func formatCard(card string) string {
	return groupDigits(textutil.LastDigits(card, 32), 4, false)
}

// groupDigits raqamlarni bo'sh joy bilan guruhlaydi: fromRight — o'ngdan (summalar uchun).
func groupDigits(d string, size int, fromRight bool) string {
	neg := strings.HasPrefix(d, "-")
	d = strings.TrimPrefix(d, "-")
	var parts []string
	if fromRight {
		for len(d) > size {
			parts = append([]string{d[len(d)-size:]}, parts...)
			d = d[:len(d)-size]
		}
		parts = append([]string{d}, parts...)
	} else {
		for len(d) > size {
			parts = append(parts, d[:size])
			d = d[size:]
		}
		parts = append(parts, d)
	}
	out := strings.Join(parts, " ")
	if neg {
		out = "-" + out
	}
	return out
}

// formatCount sonni minglar bo'yicha ajratadi: 12345 → "12 345".
func formatCount(n int64) string {
	return groupDigits(strconv.FormatInt(n, 10), 3, true)
}

func padLeft(s string, width int) string {
	if n := len([]rune(s)); n < width {
		return strings.Repeat(" ", width-n) + s
	}
	return s
}

func padRight(s string, width int) string {
	if n := len([]rune(s)); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// formatDuration: "45 s" yoki "1 daq 57 s".
func formatDuration(d time.Duration) string {
	sec := int(d.Round(time.Second) / time.Second)
	if sec < 60 {
		return fmt.Sprintf("%d s", sec)
	}
	return fmt.Sprintf("%d daq %d s", sec/60, sec%60)
}

// formatBytes: 512 B, 3.4 KB, 12.5 MB.
func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// removeFile vaqtinchalik faylni o'chiradi.
func removeFile(path string, log *zap.Logger) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn("failed to remove temp file", zap.String("path", path), zap.Error(err))
	}
}
