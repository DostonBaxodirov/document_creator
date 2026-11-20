package generator

import (
	"archive/zip"
	"context"
	"fmt"
	"hash/crc32"
	"os"
	"path"
	"strings"
	"time"
)

// prepareOutputDir vaqtinchalik fayllar papkasini tayyorlaydi
// (bo'sh bo'lsa — tizimning vaqtinchalik papkasi).
func prepareOutputDir(dir string) (string, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("chiqish papkasini yaratib bo'lmadi: %w", err)
	}
	return dir, nil
}

// writeArchive fayllarni berilgan tartibda ZIP arxivga (OOXML paket) yozadi
// va vaqtinchalik fayl yo'lini qaytaradi. order da bor, lekin files da yo'q
// nomlar o'tkazib yuboriladi. Xato bo'lsa, chala fayl o'chiriladi.
func writeArchive(ctx context.Context, dir, pattern string, order []string, files map[string][]byte) (out string, err error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("vaqtinchalik fayl yaratib bo'lmadi: %w", err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()

	zw := zip.NewWriter(f)
	modified := time.Now()
	for _, name := range order {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		data, ok := files[name]
		if !ok {
			continue
		}
		if err := writeZipEntry(zw, name, data, modified); err != nil {
			return "", fmt.Errorf("zip yozuvi %s: %w", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("zip yopilmadi: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("fayl yopilmadi: %w", err)
	}
	return f.Name(), nil
}

// writeZipEntry bitta faylni arxivga yozadi.
//
// Allaqachon siqilgan formatlar (rasm, video) siqilmasdan (Store) saqlanadi.
// Muhim: Store usulida CRC va o'lchamlar oldindan header'ga yoziladi
// (CreateRaw). Oddiy CreateHeader esa "data descriptor" qo'shadi —
// LibreOffice va ba'zi Office versiyalari bunday Store yozuvli faylni
// "buzilgan" deb hisoblaydi.
func writeZipEntry(zw *zip.Writer, name string, data []byte, modified time.Time) error {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".mp4", ".m4a", ".mp3":
		h := &zip.FileHeader{
			Name:               name,
			Method:             zip.Store,
			CRC32:              crc32.ChecksumIEEE(data),
			CompressedSize64:   uint64(len(data)),
			UncompressedSize64: uint64(len(data)),
		}
		// CreateRaw Modified maydonini MS-DOS vaqtiga o'girmaydi, shuning uchun
		// sanani to'g'ridan-to'g'ri o'rnatamiz.
		h.SetModTime(modified) //nolint:staticcheck // CreateRaw uchun yagona usul
		w, err := zw.CreateRaw(h)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	default:
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
}

var xmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

// sanitizeXMLText noto'g'ri UTF-8 va XML 1.0 da taqiqlangan boshqaruv
// belgilarini olib tashlaydi. keepNewlines=false bo'lsa, qator ko'chirishlar
// va tablar bo'sh joyga aylantiriladi.
func sanitizeXMLText(s string, keepNewlines bool) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && keepNewlines:
			return r
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r < 0x20, r >= 0xD800 && r <= 0xDFFF, r == 0xFFFE, r == 0xFFFF:
			return -1
		}
		return r
	}, s)
}

// xmlEscape matnni XML ichiga xavfsiz joylash uchun tayyorlaydi: taqiqlangan
// belgilarni olib tashlaydi, qator ko'chirishlarni bo'sh joyga aylantiradi
// va maxsus belgilarni ekranlaydi.
func xmlEscape(s string) string {
	return xmlEscaper.Replace(sanitizeXMLText(s, false))
}
