package storage

// Admin e'lonlari: barcha foydalanuvchilarga (sessiyalar jadvali bo'yicha)
// xabar yuborish jarayoni. Jarayon holati va kursori (oxirgi yuborilgan
// foydalanuvchi) bazada saqlanadi — bot qayta ishga tushsa, e'lon to'xtagan
// joyidan davom etadi.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// E'lon turlari.
const (
	BroadcastReferral = "referral" // do'stlarni taklif qilish e'loni
)

// E'lon holatlari. Qiymatlar bazada saqlanadi, shuning uchun o'zgartirmang.
const (
	BroadcastRunning   = "running"   // yuborilmoqda (yoki bot to'xtagani uchun uzilgan — davom ettiriladi)
	BroadcastDone      = "done"      // hammaga yuborildi
	BroadcastCancelled = "cancelled" // admin to'xtatdi
)

// Broadcast — bitta e'lon va uni yuborish jarayoni.
type Broadcast struct {
	ID   uint   `gorm:"primaryKey"`
	Kind string `gorm:"size:32;not null;uniqueIndex:idx_broadcasts_running,where:status = 'running'"`
	// Status — BroadcastRunning | BroadcastDone | BroadcastCancelled.
	Status    string `gorm:"size:16;not null"`
	StartedBy int64  `gorm:"not null"`           // e'lonni boshlagan adminning Telegram ID si
	MessageID int    `gorm:"not null;default:0"` // admin chatidagi jarayon xabari

	Total   int `gorm:"not null;default:0"` // boshlanganda oluvchilar soni
	Sent    int `gorm:"not null;default:0"` // yetkazildi
	Blocked int `gorm:"not null;default:0"` // botni bloklagan yoki o'chirilgan akkaunt
	Failed  int `gorm:"not null;default:0"` // boshqa xatolar
	// LastUserID — oxirgi ko'rib chiqilgan oluvchi (sessiya user_id si):
	// keyingi oluvchilar shundan katta user_id bo'yicha olinadi.
	LastUserID int64 `gorm:"not null;default:0"`

	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Processed ko'rib chiqilgan oluvchilar soni.
func (b *Broadcast) Processed() int { return b.Sent + b.Blocked + b.Failed }

// Recipient — e'lon oluvchi (bot bilan suhbati bor foydalanuvchi).
type Recipient struct {
	UserID int64
	ChatID int64
}

// BroadcastRepository e'lonlarni saqlaydi.
type BroadcastRepository struct {
	db *gorm.DB
}

// NewBroadcastRepository yangi repozitoriy yaratadi.
func NewBroadcastRepository(db *gorm.DB) *BroadcastRepository {
	return &BroadcastRepository{db: db}
}

// Start yangi e'lonni "running" holatida yaratadi. Shu turdagi e'lon
// allaqachon yuborilayotgan bo'lsa (masalan, ikki admin bir vaqtda bosgan)
// ErrConflict qaytaradi — buni bazadagi qisman unique indeks kafolatlaydi.
func (r *BroadcastRepository) Start(ctx context.Context, kind string, adminID int64, total int) (*Broadcast, error) {
	b := &Broadcast{Kind: kind, Status: BroadcastRunning, StartedBy: adminID, Total: total}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(b)
	if res.Error != nil {
		return nil, fmt.Errorf("storage: e'lonni yaratib bo'lmadi: %w", res.Error)
	}
	if res.RowsAffected != 1 {
		return nil, ErrConflict
	}
	return b, nil
}

// SetMessage admin chatidagi jarayon xabari identifikatorini saqlaydi.
func (r *BroadcastRepository) SetMessage(ctx context.Context, id uint, messageID int) error {
	if err := r.db.WithContext(ctx).Model(&Broadcast{}).Where("id = ?", id).
		Update("message_id", messageID).Error; err != nil {
		return fmt.Errorf("storage: e'lon xabarini saqlab bo'lmadi: %w", err)
	}
	return nil
}

// SaveProgress hisoblagichlar va kursorni saqlaydi (faqat "running" e'londa).
func (r *BroadcastRepository) SaveProgress(ctx context.Context, b *Broadcast) error {
	if err := r.db.WithContext(ctx).Model(&Broadcast{}).
		Where("id = ? AND status = ?", b.ID, BroadcastRunning).
		Updates(progressColumns(b)).Error; err != nil {
		return fmt.Errorf("storage: e'lon holatini saqlab bo'lmadi: %w", err)
	}
	return nil
}

// Finish e'lonni yakunlaydi: hisoblagichlar, kursor, holat (done yoki
// cancelled) va tugagan vaqt yoziladi.
func (r *BroadcastRepository) Finish(ctx context.Context, b *Broadcast, status string) error {
	now := time.Now().UTC()
	upd := progressColumns(b)
	upd["status"], upd["finished_at"] = status, now
	if err := r.db.WithContext(ctx).Model(&Broadcast{}).
		Where("id = ? AND status = ?", b.ID, BroadcastRunning).Updates(upd).Error; err != nil {
		return fmt.Errorf("storage: e'lonni yakunlab bo'lmadi: %w", err)
	}
	b.Status, b.FinishedAt = status, &now
	return nil
}

func progressColumns(b *Broadcast) map[string]any {
	return map[string]any{
		"sent": b.Sent, "blocked": b.Blocked, "failed": b.Failed, "last_user_id": b.LastUserID,
	}
}

// Running yakunlanmagan e'lonlarni qaytaradi (bot qayta ishga tushganda davom ettirish uchun).
func (r *BroadcastRepository) Running(ctx context.Context) ([]Broadcast, error) {
	var list []Broadcast
	if err := r.db.WithContext(ctx).Where("status = ?", BroadcastRunning).Order("id").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("storage: e'lonlarni o'qib bo'lmadi: %w", err)
	}
	return list, nil
}

// Last berilgan turdagi eng so'nggi e'lonni qaytaradi (bo'lmasa nil).
func (r *BroadcastRepository) Last(ctx context.Context, kind string) (*Broadcast, error) {
	var b Broadcast
	err := r.db.WithContext(ctx).Where("kind = ?", kind).Order("id DESC").Take(&b).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: oxirgi e'lonni o'qib bo'lmadi: %w", err)
	}
	return &b, nil
}

// CountRecipients e'lon oluvchilar (bot bilan suhbati bor foydalanuvchilar) soni.
func (r *BroadcastRepository) CountRecipients(ctx context.Context) (int, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&Session{}).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("storage: oluvchilarni sanab bo'lmadi: %w", err)
	}
	return int(n), nil
}

// Recipients user_id si afterUserID dan katta bo'lgan oluvchilarni user_id
// bo'yicha o'sish tartibida, ko'pi bilan limit ta qaytaradi (kursor bo'yicha
// sahifalash: jadval yuborish davomida o'zgarsa ham hech kim tushib qolmaydi
// va takrorlanmaydi).
func (r *BroadcastRepository) Recipients(ctx context.Context, afterUserID int64, limit int) ([]Recipient, error) {
	var list []Recipient
	if err := r.db.WithContext(ctx).Model(&Session{}).Select("user_id, chat_id").
		Where("user_id > ?", afterUserID).Order("user_id").Limit(limit).Scan(&list).Error; err != nil {
		return nil, fmt.Errorf("storage: oluvchilar ro'yxatini o'qib bo'lmadi: %w", err)
	}
	return list, nil
}
