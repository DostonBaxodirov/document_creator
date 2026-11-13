package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Session — foydalanuvchining joriy FSM holati va to'ldirilayotgan buyurtma ma'lumotlari.
//
// Talaba F.I.Sh., universitet va tekshiruvchi kabi "profil" maydonlari
// keyingi buyurtmalarda tayyor variant sifatida taklif qilish uchun saqlanib qoladi.
type Session struct {
	UserID   int64  `gorm:"primaryKey;autoIncrement:false"`
	ChatID   int64  `gorm:"not null"`
	Username string `gorm:"size:64"`

	// State — FSM holati (qiymatlar bot paketidagi State konstantalarida).
	State string `gorm:"size:32;not null;default:idle;index"`

	// Joriy buyurtma (har bir yangi /start da tozalanadi).
	WorkType string `gorm:"size:32"`
	Topic    string `gorm:"size:512"`
	// Volume — hajm: taqdimot uchun slaydlar soni, mustaqil ish va amaliy
	// topshiriq uchun sahifalar soni.
	Volume int `gorm:"not null;default:0"`
	// Design — taqdimot dizayn mavzusi (generator.Theme* qiymatlari).
	Design string `gorm:"size:32"`

	// Profil ma'lumotlari (keyingi buyurtmalar uchun eslab qolinadi).
	StudentName string `gorm:"size:128"`
	University  string `gorm:"size:256"`
	TeacherName string `gorm:"size:128"`

	// Statistika.
	GeneratedCount  int `gorm:"not null;default:0"`
	LastGeneratedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionRepository sessiyalarni PostgreSQL da saqlaydi.
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository yangi repozitoriy yaratadi.
func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// GetOrCreate foydalanuvchi sessiyasini qaytaradi; mavjud bo'lmasa — yaratadi.
// Bir foydalanuvchidan bir vaqtda kelgan ikki xabar poyga (race) holatini
// keltirib chiqarmasligi uchun INSERT ... ON CONFLICT DO NOTHING ishlatiladi.
func (r *SessionRepository) GetOrCreate(ctx context.Context, userID, chatID int64, username string) (*Session, error) {
	db := r.db.WithContext(ctx)

	var s Session
	err := db.First(&s, "user_id = ?", userID).Error
	if err == nil {
		return &s, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("storage: sessiyani o'qib bo'lmadi: %w", err)
	}

	s = Session{UserID: userID, ChatID: chatID, Username: username}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&s).Error; err != nil {
		return nil, fmt.Errorf("storage: sessiya yaratib bo'lmadi: %w", err)
	}
	// Default qiymatlar (state='idle' va h.k.) bazadan to'liq o'qib olinadi.
	if err := db.First(&s, "user_id = ?", userID).Error; err != nil {
		return nil, fmt.Errorf("storage: yangi sessiyani o'qib bo'lmadi: %w", err)
	}
	return &s, nil
}

// Get mavjud sessiyani qaytaradi. Topilmasa gorm.ErrRecordNotFound o'ralgan xato qaytadi.
func (r *SessionRepository) Get(ctx context.Context, userID int64) (*Session, error) {
	var s Session
	if err := r.db.WithContext(ctx).First(&s, "user_id = ?", userID).Error; err != nil {
		return nil, fmt.Errorf("storage: sessiyani o'qib bo'lmadi: %w", err)
	}
	return &s, nil
}

// Save sessiyaning barcha maydonlarini (nol qiymatlar bilan birga) saqlaydi.
func (r *SessionRepository) Save(ctx context.Context, s *Session) error {
	if err := r.db.WithContext(ctx).Save(s).Error; err != nil {
		return fmt.Errorf("storage: sessiyani saqlab bo'lmadi: %w", err)
	}
	return nil
}

// CompareAndSwapState holatni faqat joriy holat `from` ga teng bo'lsa `to` ga
// o'zgartiradi (atomar). Masalan, "Tasdiqlash" tugmasi ikki marta bosilganda
// generatsiya ikki marta ishga tushmasligini kafolatlaydi.
func (r *SessionRepository) CompareAndSwapState(ctx context.Context, userID int64, from, to string) (bool, error) {
	res := r.db.WithContext(ctx).
		Model(&Session{}).
		Where("user_id = ? AND state = ?", userID, from).
		Updates(map[string]any{"state": to, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return false, fmt.Errorf("storage: holatni o'zgartirib bo'lmadi: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// ListByState berilgan holatdagi barcha sessiyalarni qaytaradi
// (masalan, qayta ishga tushganda yakunlanmay qolgan generatsiyalarni topish uchun).
func (r *SessionRepository) ListByState(ctx context.Context, state string) ([]Session, error) {
	var list []Session
	if err := r.db.WithContext(ctx).Where("state = ?", state).Find(&list).Error; err != nil {
		return nil, fmt.Errorf("storage: sessiyalar ro'yxatini o'qib bo'lmadi: %w", err)
	}
	return list, nil
}
