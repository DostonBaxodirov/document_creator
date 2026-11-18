package backup

import (
	"compress/gzip"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"telegram-presentation-bot/internal/config"
)

// fakePgDump pg_dump o'rnida ishlaydigan shell skript yaratadi.
func fakePgDump(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell skript kerak")
	}
	path := filepath.Join(t.TempDir(), "pg_dump")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

var testDB = config.DBConfig{Host: "db.local", Port: 5433, User: "bot", Password: "s3cr$t 'pw'", Name: "presentation/bot", SSLMode: "disable"}

func TestDump(t *testing.T) {
	script := fakePgDump(t, `echo "ARGS: $*"; echo "PW: $PGPASSWORD"; echo "SSL: $PGSSLMODE"`)
	d := New(testDB, script, t.TempDir(), time.Minute)
	if err := d.Check(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 3, 0, 0, 0, time.UTC)
	f, err := d.Dump(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Path)

	if f.Name != "backup_presentation_bot_2026-11-19_0300.sql.gz" {
		t.Errorf("fayl nomi: %s", f.Name)
	}
	info, _ := os.Stat(f.Path)
	if info == nil || info.Size() != f.Size {
		t.Errorf("hajm mos emas: %d", f.Size)
	}

	fh, err := os.Open(f.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	gz, err := gzip.NewReader(fh)
	if err != nil {
		t.Fatalf("gzip emas: %v", err)
	}
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		"--host=db.local", "--port=5433", "--username=bot", "--dbname=presentation/bot",
		"--no-password", "--no-owner", "--clean", "--if-exists",
		"PW: s3cr$t 'pw'", "SSL: disable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("natijada %q yo'q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.SplitN(out, "\n", 2)[0], "s3cr") {
		t.Error("parol buyruq argumentlarida bo'lmasligi kerak")
	}
}

func TestDumpErrors(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, script string
		timeout      time.Duration
		want         string
	}{
		{"xato", `echo 'pg_dump: error: connection to server failed' >&2; exit 1`, time.Minute, "connection to server failed"},
		{"bo'sh", `exit 0`, time.Minute, "hech narsa chiqarmadi"},
		{"vaqt", `exec sleep 5`, 100 * time.Millisecond, "vaqti tugadi"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := New(testDB, fakePgDump(t, c.script), dir, c.timeout)
			f, err := d.Dump(context.Background(), time.Now())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("xato %q bo'lishi kerak edi: f=%v err=%v", c.want, f, err)
			}
		})
	}
	// Xatoda vaqtinchalik fayllar qolmasligi kerak.
	if left, _ := filepath.Glob(filepath.Join(dir, "backup-*")); len(left) > 0 {
		t.Errorf("vaqtinchalik fayllar o'chirilmagan: %v", left)
	}
}

func TestCheckMissing(t *testing.T) {
	if err := New(testDB, "/nonexistent/pg_dump", "", time.Minute).Check(); err == nil {
		t.Error("pg_dump topilmasa xato bo'lishi kerak")
	}
}

// TestDumpIntegration — haqiqiy PostgreSQL va pg_dump bilan. Ishga tushirish:
//
//	TEST_DATABASE_DSN="postgres://postgres:postgres@localhost:55432/test?sslmode=disable" \
//	TEST_PG_DUMP=pg_dump go test ./internal/backup/
func TestDumpIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN berilmagan")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	pw, _ := u.User.Password()
	db := config.DBConfig{Host: u.Hostname(), Port: port, User: u.User.Username(), Password: pw,
		Name: strings.TrimPrefix(u.Path, "/"), SSLMode: u.Query().Get("sslmode")}

	d := New(db, os.Getenv("TEST_PG_DUMP"), t.TempDir(), 2*time.Minute)
	f, err := d.Dump(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("TEST_BACKUP_OUT"); out != "" {
		data, _ := os.ReadFile(f.Path)
		_ = os.WriteFile(out, data, 0o600)
	}
	fh, _ := os.Open(f.Path)
	defer fh.Close()
	gz, err := gzip.NewReader(fh)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(gz)
	for _, want := range []string{"PostgreSQL database dump complete", "DROP TABLE IF EXISTS", "CREATE TABLE"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("nusxada %q yo'q", want)
		}
	}
	t.Logf("%s: %d bayt (siqilmagan %d)", f.Name, f.Size, len(data))
}
