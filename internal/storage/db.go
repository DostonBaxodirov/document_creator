// Package storage PostgreSQL bilan ishlash qatlami: ulanish, migratsiya
// foydalanuvchi sessiyalari (FSM holati), buyurtmalar, to'lovlar va tashriflar
// statistikasi repozitoriylari.
package storage

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"telegram-presentation-bot/internal/config"
)

// Connect PostgreSQL ga ulanadi. Docker muhitida baza bot bilan bir vaqtda
// ishga tushishi mumkin, shuning uchun ulanish bir necha marta qayta uriniladi.
func Connect(ctx context.Context, cfg config.DBConfig, log *zap.Logger) (*gorm.DB, error) {
	// GORM loglarini zap orqali chiqaramiz: faqat ogohlantirishlar va sekin so'rovlar.
	gormLog := gormlogger.New(
		zap.NewStdLog(log.Named("gorm")),
		gormlogger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	var lastErr error
	for attempt := 1; attempt <= cfg.ConnectRetries; attempt++ {
		db, err := open(ctx, cfg, gormLog)
		if err == nil {
			log.Info("connected to PostgreSQL",
				zap.String("host", cfg.Host),
				zap.Int("port", cfg.Port),
				zap.String("db", cfg.Name))
			return db, nil
		}
		lastErr = err
		log.Warn("PostgreSQL is not ready, retrying",
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", cfg.ConnectRetries),
			zap.Error(err))

		if attempt == cfg.ConnectRetries {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("storage: ulanish bekor qilindi: %w", ctx.Err())
		case <-time.After(cfg.ConnectDelay):
		}
	}
	return nil, fmt.Errorf("storage: %d urinishdan keyin ham PostgreSQL ga ulanib bo'lmadi: %w",
		cfg.ConnectRetries, lastErr)
}

// open bitta ulanish urinishini bajaradi va ulanishlar pulini sozlaydi.
func open(ctx context.Context, cfg config.DBConfig, gormLog gormlogger.Interface) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger:  gormLog,
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// migrationsFS — versiyali SQL migratsiyalar (goose formatida). Yangi
// migratsiya: `goose -dir internal/storage/migrations -s create <nom> sql`.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate bazaga hali qo'llanmagan SQL migratsiyalarni (migrations/*.sql)
// goose orqali qo'llaydi; qo'llanganlari goose_db_version jadvalida
// saqlanadi. Bir vaqtda ishga tushgan ikki nusxa sxemani birga
// o'zgartirmasligi uchun migratsiyalar PostgreSQL advisory lock ostida
// bajariladi (ikkinchisi birinchisi tugashini kutadi).
//
// Kutish va migratsiyalar db.WithContext(ctx) bilan berilgan kontekstda
// bajariladi: kontekst bekor qilinsa, Migrate xato bilan qaytadi.
func Migrate(db *gorm.DB) error {
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("storage: migratsiya xatosi: %w", err)
	}
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("storage: migratsiya fayllarini o'qib bo'lmadi: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("storage: migratsiya qulfini yaratib bo'lmadi: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys,
		goose.WithSessionLocker(locker),
		// Faqat shu paketdagi SQL fayllar: global ro'yxatdagi Go migratsiyalar hisobga olinmaydi.
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("storage: migratsiya xatosi: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("storage: migratsiya xatosi: %w", err)
	}
	return nil
}

// Close ulanishlar pulini yopadi.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
