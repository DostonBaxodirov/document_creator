package storage

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

// allModels — barcha modellar: migratsiyalar ularning AutoMigrate sxemasini
// aynan takrorlashi kerak. Dastlabki beshtasini goose'gacha Migrate
// AutoMigrate orqali yaratgan (00001_baseline), qolganlari — keyingi
// migratsiyalarda qo'shilgan.
var allModels = []any{&Session{}, &User{}, &Order{}, &Payment{}, &Visit{}, &Referral{}, &Broadcast{}}

// TestMigrateEmptySchema: bo'sh sxemada migratsiyalardan keyin har bir
// modelning jadvali, barcha ustunlari va GORM nomlari bilan indekslari mavjud.
func TestMigrateEmptySchema(t *testing.T) {
	db, _ := emptyTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migratsiya: %v", err)
	}

	m := db.Migrator()
	indexes := 0
	for _, model := range allModels {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			t.Fatalf("%T modelini o'qib bo'lmadi: %v", model, err)
		}
		table := stmt.Schema.Table
		if !m.HasTable(model) {
			t.Errorf("%s jadvali yaratilmagan", table)
			continue
		}
		for _, f := range stmt.Schema.Fields {
			if f.DBName == "" {
				continue
			}
			if !m.HasColumn(model, f.DBName) {
				t.Errorf("%s.%s ustuni yo'q", table, f.DBName)
			}
		}
		for _, idx := range stmt.Schema.ParseIndexes() {
			indexes++
			if !m.HasIndex(model, idx.Name) {
				t.Errorf("%s jadvalida %s indeksi yo'q", table, idx.Name)
			}
		}
	}
	if indexes == 0 {
		t.Fatal("modellarda birorta indeks topilmadi — tekshiruv hech narsani tekshirmadi")
	}

	if got, want := gooseVersion(t, db), latestMigrationVersion(t); got != want {
		t.Errorf("goose versiyasi = %d, kutilgan %d", got, want)
	}
}

// TestMigrateOverAutoMigrate: eski AutoMigrate yaratgan sxemada (production
// holati) migratsiyalar xatosiz o'tadi, sxemani va ma'lumotlarni o'zgartirmaydi,
// faqat versiyani yozib qo'yadi.
func TestMigrateOverAutoMigrate(t *testing.T) {
	db, _ := emptyTestDB(t)
	if err := db.AutoMigrate(allModels...); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if err := db.Create(&User{TelegramUserID: 42, Username: "eski"}).Error; err != nil {
		t.Fatalf("foydalanuvchi: %v", err)
	}
	before := schemaSnapshot(t, db)

	if err := Migrate(db); err != nil {
		t.Fatalf("migratsiya: %v", err)
	}
	if diff := diffLines(before, schemaSnapshot(t, db)); diff != "" {
		t.Errorf("migratsiya mavjud sxemani o'zgartirdi (- oldin, + keyin):\n%s", diff)
	}
	var users int64
	if err := db.Model(&User{}).Where("telegram_user_id = ?", 42).Count(&users).Error; err != nil || users != 1 {
		t.Errorf("mavjud yozuv saqlanmadi: %d ta (%v)", users, err)
	}
	if got, want := gooseVersion(t, db), latestMigrationVersion(t); got != want {
		t.Errorf("goose versiyasi = %d, kutilgan %d", got, want)
	}

	// Keyingi ishga tushirishlar ham xatosiz va hech narsa qo'llamaydi.
	if err := Migrate(db); err != nil {
		t.Fatalf("takroriy migratsiya: %v", err)
	}
	if diff := diffLines(before, schemaSnapshot(t, db)); diff != "" {
		t.Errorf("takroriy migratsiya sxemani o'zgartirdi (- oldin, + keyin):\n%s", diff)
	}
}

// TestMigrateMatchesAutoMigrate: bo'sh sxemada migratsiyalar AutoMigrate
// yaratadigan sxemani aynan takrorlaydi — ustunlar tartibi, turlari,
// default'lari, NOT NULL, kalitlar, indekslar va ketma-ketliklar (sequence).
func TestMigrateMatchesAutoMigrate(t *testing.T) {
	legacy, _ := emptyTestDB(t)
	if err := legacy.AutoMigrate(allModels...); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	migrated, _ := emptyTestDB(t)
	if err := Migrate(migrated); err != nil {
		t.Fatalf("migratsiya: %v", err)
	}
	if diff := diffLines(schemaSnapshot(t, legacy), schemaSnapshot(t, migrated)); diff != "" {
		t.Errorf("migratsiyalar sxemasi AutoMigrate'dan farq qiladi (- AutoMigrate, + goose):\n%s", diff)
	}
}

// schemaSnapshot joriy sxemaning tuzilishini (goose_db_version'dan tashqari)
// solishtirish mumkin bo'lgan qatorlar ko'rinishida qaytaradi. Sxema nomi
// qatorlardan olib tashlanadi — turli test sxemalarini solishtirish uchun.
func schemaSnapshot(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	queries := []string{
		// Ustunlar (jadvaldagi tartibi bilan).
		`SELECT format('column %s.%s #%s %s(%s) nullable=%s default=%s',
			table_name, column_name, ordinal_position, data_type,
			character_maximum_length, is_nullable, column_default)
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name <> 'goose_db_version'
		ORDER BY table_name, ordinal_position`,
		// Indekslar (birlamchi kalit indekslari ham).
		`SELECT format('index %s %s', tablename, replace(indexdef, current_schema() || '.', ''))
		FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename <> 'goose_db_version'
		ORDER BY tablename, indexname`,
		// Cheklovlar (PRIMARY KEY, UNIQUE, CHECK, FOREIGN KEY).
		`SELECT format('constraint %s %s %s', t.relname, c.conname, pg_get_constraintdef(c.oid))
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = current_schema() AND t.relname <> 'goose_db_version'
		ORDER BY t.relname, c.conname`,
		// Ketma-ketliklar va ular qaysi ustunga tegishliligi.
		`SELECT format('sequence %s %s start=%s inc=%s min=%s max=%s cache=%s cycle=%s owner=%s.%s',
			s.relname, q.data_type, q.start_value, q.increment_by, q.min_value,
			q.max_value, q.cache_size, q.cycle, t.relname, a.attname)
		FROM pg_class s
		JOIN pg_namespace n ON n.oid = s.relnamespace
		JOIN pg_sequences q ON q.schemaname = n.nspname AND q.sequencename = s.relname
		LEFT JOIN pg_depend d ON d.objid = s.oid AND d.classid = 'pg_class'::regclass
			AND d.refclassid = 'pg_class'::regclass AND d.deptype IN ('a', 'i')
		LEFT JOIN pg_class t ON t.oid = d.refobjid
		LEFT JOIN pg_attribute a ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
		WHERE s.relkind = 'S' AND n.nspname = current_schema()
			AND coalesce(t.relname, '') <> 'goose_db_version'
		ORDER BY s.relname`,
	}
	var snapshot []string
	for _, q := range queries {
		var lines []string
		if err := db.Raw(q).Scan(&lines).Error; err != nil {
			t.Fatalf("sxema tuzilishini o'qib bo'lmadi: %v\n%s", err, q)
		}
		snapshot = append(snapshot, lines...)
	}
	if len(snapshot) == 0 {
		t.Fatal("sxema bo'sh")
	}
	return snapshot
}

// diffLines a da bo'lib b da yo'q qatorlarni "-", aksincha "+" bilan
// qaytaradi; farq bo'lmasa — bo'sh satr.
func diffLines(a, b []string) string {
	var sb strings.Builder
	for _, l := range a {
		if !slices.Contains(b, l) {
			sb.WriteString("- " + l + "\n")
		}
	}
	for _, l := range b {
		if !slices.Contains(a, l) {
			sb.WriteString("+ " + l + "\n")
		}
	}
	if sb.Len() == 0 && !slices.Equal(a, b) {
		sb.WriteString("qatorlar bir xil, lekin tartibi farq qiladi\n")
	}
	return sb.String()
}

// gooseVersion bazada qo'llangan oxirgi migratsiya versiyasi.
func gooseVersion(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var v int64
	if err := db.Raw("SELECT max(version_id) FROM goose_db_version").Scan(&v).Error; err != nil {
		t.Fatalf("goose_db_version o'qib bo'lmadi: %v", err)
	}
	return v
}

// latestMigrationVersion embed qilingan migratsiyalarning eng katta versiyasi.
func latestMigrationVersion(t *testing.T) int64 {
	t.Helper()
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var latest int64
	for _, e := range entries {
		v, err := goose.NumericComponent(e.Name())
		if err != nil {
			t.Fatalf("%s: migratsiya nomi noto'g'ri: %v", e.Name(), err)
		}
		latest = max(latest, v)
	}
	if latest == 0 {
		t.Fatal("embed qilingan migratsiyalar topilmadi")
	}
	return latest
}
