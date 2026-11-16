// Package storage PostgreSQL bilan ishlash qatlami: ulanish, migratsiya
// foydalanuvchi sessiyalari (FSM holati), buyurtmalar va to'lovlar repozitoriylari.
package storage

import (
	"context"
	"fmt"
	"time"

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

// Migrate jadvallarni modelga moslab yaratadi yoki yangilaydi.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&Session{}, &User{}, &Order{}, &Payment{}); err != nil {
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
