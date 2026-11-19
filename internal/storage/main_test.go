package storage

// Repozitoriylar uchun integratsion testlar haqiqiy PostgreSQL talab qiladi.
// Ishga tushirish:
//
//	TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable" go test ./internal/storage/...
//
// TEST_DATABASE_URL berilmasa, bazali testlar o'tkazib yuboriladi (t.Skip).
// Har bir ishga tushirishda tasodifiy nomli sxema (test_xxx) yaratiladi,
// ulanish search_path orqali shu sxemaga yo'naltiriladi va testlar oxirida
// sxema o'chiriladi — bazadagi boshqa ma'lumotlarga tegilmaydi.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// testPoolSize — pulda saqlanadigan bo'sh ulanishlar soni (warmPool ham shuncha ochadi).
const testPoolSize = 20

var (
	// sharedDB — test sxemasiga ulangan baza (nil — TEST_DATABASE_URL berilmagan).
	sharedDB *gorm.DB
	// sharedTables — test sxemasidagi jadvallar (har bir test oldidan tozalanadi).
	sharedTables []string
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

// runTests sxemani tayyorlaydi, testlarni ishga tushiradi va sxemani o'chiradi
// (os.Exit defer'larni bajarmagani uchun alohida funksiyada).
func runTests(m *testing.M) int {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		return m.Run()
	}
	db, tables, cleanup, err := setupTestSchema(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "storage: test bazasini tayyorlab bo'lmadi: %v\n", err)
		return 1
	}
	defer cleanup()
	sharedDB, sharedTables = db, tables
	return m.Run()
}

// setupTestSchema tasodifiy sxema yaratadi, unga search_path bilan ulanadi va
// migratsiyani bajaradi. cleanup ulanishni yopadi va sxemani o'chiradi.
func setupTestSchema(dsn string) (db *gorm.DB, tables []string, cleanup func(), err error) {
	gormCfg := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}

	admin, err := gorm.Open(postgres.Open(dsn), gormCfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ulanish: %w", err)
	}
	closeAdmin := func() { _ = Close(admin) }

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		closeAdmin()
		return nil, nil, nil, err
	}
	// Nom faqat hex belgilardan iborat — SQL ga to'g'ridan-to'g'ri qo'yish xavfsiz.
	schema := "test_" + hex.EncodeToString(suffix)
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		closeAdmin()
		return nil, nil, nil, fmt.Errorf("sxema yaratish: %w", err)
	}
	dropSchema := func() {
		if err := admin.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error; err != nil {
			fmt.Fprintf(os.Stderr, "storage: %s sxemasini o'chirib bo'lmadi: %v\n", schema, err)
		}
		closeAdmin()
	}

	schemaDSN, err := withSearchPath(dsn, schema)
	if err != nil {
		dropSchema()
		return nil, nil, nil, err
	}
	db, err = gorm.Open(postgres.Open(schemaDSN), gormCfg)
	if err != nil {
		dropSchema()
		return nil, nil, nil, fmt.Errorf("sxemaga ulanish: %w", err)
	}
	cleanup = func() {
		_ = Close(db)
		dropSchema()
	}
	// Poyga testlari (masalan, GetOrCreate) uchun ulanishlar pulda qoladi —
	// aks holda har bir goroutine yangi ulanish ochib, so'rovlar ketma-ket bo'lib qoladi.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxIdleConns(testPoolSize)
	}

	// Ehtiyot chorasi: search_path ishlamasa, testlar boshqa sxemadagi
	// jadvallarni tozalab yubormasligi kerak.
	var current string
	if err := db.Raw("SELECT current_schema()").Scan(&current).Error; err != nil || current != schema {
		cleanup()
		return nil, nil, nil, fmt.Errorf("search_path qo'llanmadi: current_schema=%q, kutilgan %q (%v)", current, schema, err)
	}

	if err := Migrate(db); err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	if err := db.Raw("SELECT tablename FROM pg_tables WHERE schemaname = ? ORDER BY tablename", schema).
		Scan(&tables).Error; err != nil || len(tables) == 0 {
		cleanup()
		return nil, nil, nil, fmt.Errorf("jadvallar ro'yxati: %v (%d ta)", err, len(tables))
	}
	return db, tables, cleanup, nil
}

// withSearchPath DSN ga search_path parametrini qo'shadi (URL va
// "key=value" ko'rinishidagi DSN lar qo'llab-quvvatlanadi).
func withSearchPath(dsn, schema string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("TEST_DATABASE_URL noto'g'ri: %w", err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return dsn + " search_path=" + schema, nil
}

// testDB test sxemasiga ulangan bazani qaytaradi va barcha jadvallarni
// tozalaydi (identifikatorlar ham 1 dan boshlanadi). TEST_DATABASE_URL
// berilmagan bo'lsa, test o'tkazib yuboriladi. Bazali testlar umumiy
// sxemani ishlatadi, shuning uchun ularda t.Parallel() chaqirmang.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	if sharedDB == nil {
		t.Skip("TEST_DATABASE_URL berilmagan")
	}
	if err := sharedDB.Exec("TRUNCATE " + strings.Join(sharedTables, ", ") + " RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("jadvallarni tozalab bo'lmadi: %v", err)
	}
	return sharedDB
}

// warmPool pulda testPoolSize ta ulanishni oldindan ochadi, shunda poyga
// testidagi goroutine'lar so'rovlarni haqiqatan bir vaqtda yuboradi.
func warmPool(t *testing.T, db *gorm.DB) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make([]error, testPoolSize)
	for i := range testPoolSize {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = db.Exec("SELECT pg_sleep(0.05)").Error
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatalf("ulanishlarni ochib bo'lmadi: %v", err)
	}
}
