// Package backup PostgreSQL bazasining zaxira nusxasini oladi: pg_dump
// natijasi (oddiy SQL) gzip bilan siqilib vaqtinchalik faylga yoziladi.
//
// Tiklash (docs/DEPLOY.md):
//
//	gunzip -c backup_....sql.gz | docker compose exec -T postgres psql -U postgres -d presentation_bot
//
// Nusxa --clean --if-exists bilan olinadi: tiklashda mavjud jadvallar avval
// o'chirilib, nusxadagi holat to'liq qayta yoziladi.
package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"telegram-presentation-bot/internal/config"
)

// maxStderr — xato xabariga qo'shiladigan pg_dump stderr uzunligi.
const maxStderr = 2000

// File — tayyor nusxa. Ishlatib bo'lingach chaqiruvchi faylni o'chiradi:
// os.Remove(f.Path).
type File struct {
	Path string // vaqtinchalik fayl
	Name string // foydalanuvchiga ko'rinadigan nom: backup_<db>_2026-11-19_0300.sql.gz
	Size int64  // baytlarda
}

// Dumper pg_dump orqali nusxa oladi.
type Dumper struct {
	db      config.DBConfig
	pgDump  string
	dir     string
	timeout time.Duration
}

// New yangi Dumper yaratadi. dir — vaqtinchalik fayllar papkasi (bo'sh —
// os.TempDir()), timeout — bitta nusxa uchun maksimal vaqt.
func New(db config.DBConfig, pgDump, dir string, timeout time.Duration) *Dumper {
	if pgDump == "" {
		pgDump = "pg_dump"
	}
	return &Dumper{db: db, pgDump: pgDump, dir: dir, timeout: timeout}
}

// Check pg_dump mavjudligini tekshiradi.
func (d *Dumper) Check() error {
	if _, err := exec.LookPath(d.pgDump); err != nil {
		return fmt.Errorf("backup: %s topilmadi (postgresql-client o'rnatilmagan): %w", d.pgDump, err)
	}
	return nil
}

// Dump bazaning nusxasini oladi. now — fayl nomidagi vaqt.
func (d *Dumper) Dump(ctx context.Context, now time.Time) (_ *File, err error) {
	if d.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.timeout)
		defer cancel()
	}

	f, err := os.CreateTemp(d.dir, "backup-*.sql.gz")
	if err != nil {
		return nil, fmt.Errorf("backup: vaqtinchalik fayl yaratib bo'lmadi: %w", err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()

	gz := gzip.NewWriter(f)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, d.pgDump, d.args()...)
	// Parol buyruq qatorida emas, muhit o'zgaruvchisida (jarayonlar ro'yxatida ko'rinmaydi).
	cmd.Env = append(os.Environ(),
		"PGPASSWORD="+d.db.Password,
		"PGSSLMODE="+d.db.SSLMode,
		"PGCONNECT_TIMEOUT=15",
	)
	out := &countingWriter{w: gz}
	cmd.Stdout = out
	cmd.Stderr = &stderr
	// Vaqt tugaganda jarayon o'ldiriladi; quvurlar yopilishini cheksiz kutmaslik uchun.
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("backup: pg_dump vaqti tugadi: %w", ctx.Err())
		}
		return nil, fmt.Errorf("backup: pg_dump xatosi: %w: %s", err, tail(stderr.String(), maxStderr))
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("backup: faylni siqib bo'lmadi: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("backup: faylni yozib bo'lmadi: %w", err)
	}
	if out.n == 0 {
		return nil, errors.New("backup: pg_dump hech narsa chiqarmadi")
	}
	info, err := os.Stat(f.Name())
	if err != nil {
		return nil, fmt.Errorf("backup: fayl topilmadi: %w", err)
	}
	return &File{
		Path: f.Name(),
		Name: fmt.Sprintf("backup_%s_%s.sql.gz", safeName(d.db.Name), now.Format("2006-01-02_1504")),
		Size: info.Size(),
	}, nil
}

// args pg_dump argumentlari.
func (d *Dumper) args() []string {
	return []string{
		"--host=" + d.db.Host,
		"--port=" + strconv.Itoa(d.db.Port),
		"--username=" + d.db.User,
		"--dbname=" + d.db.Name,
		"--no-password", // parol so'ralmaydi (PGPASSWORD dan olinadi)
		"--no-owner",    // boshqa foydalanuvchi nomidagi bazaga ham tiklanadi
		"--no-privileges",
		"--clean",
		"--if-exists",
	}
}

// countingWriter yozilgan baytlarni sanaydi.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// tail matnning oxirgi n baytini qaytaradi (xato sababi odatda oxirida).
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = "…" + strings.ToValidUTF8(s[len(s)-n:], "")
	}
	return s
}

// safeName fayl nomiga yaroqli qism: harf, raqam, - va _ dan boshqasi "_".
func safeName(s string) string {
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			b[i] = '_'
		}
	}
	if len(b) == 0 {
		return "db"
	}
	return string(b)
}
