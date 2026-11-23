package storage

// Do'stlarni taklif qilish (referal) tizimi: kim kimni taklif qilgani,
// mukofot (bepul imkoniyat) berish, bonus bilan buyurtma yaratish va
// ishlatilmay qolgan bonusni qaytarish.
//
// Bonus balansi users.free_credits ustunida saqlanadi va faqat atomar
// UPDATE ... SET free_credits = free_credits ± 1 bilan o'zgaradi: parallel
// so'rovlarda ham balans manfiy bo'lmaydi va ikki marta yozilmaydi.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrNoCredits — foydalanuvchida bepul imkoniyat (bonus) qolmagan.
var ErrNoCredits = errors.New("storage: bepul imkoniyat qolmagan")

// Referral — bitta taklif: taklif qilgan (referrer) va taklif qilingan
// (referee) foydalanuvchilar. Bitta foydalanuvchini faqat bir kishi taklif
// qilgan bo'lishi mumkin (referee bo'yicha unique indeks).
type Referral struct {
	ID             uint `gorm:"primaryKey"`
	ReferrerUserID uint `gorm:"not null;index"`
	RefereeUserID  uint `gorm:"not null;uniqueIndex"`
	CreatedAt      time.Time
	// RewardedAt — taklif qilganga mukofot berilgan vaqt (nil — hali berilmagan:
	// taklif qilingan foydalanuvchi hali birorta buyurtmani to'lamagan).
	RewardedAt *time.Time
	// RewardOrderID — mukofotga sabab bo'lgan to'langan buyurtma.
	RewardOrderID *uint
}

// ReferralReward — berilgan mukofot (taklif qilganga xabar yuborish uchun).
type ReferralReward struct {
	ReferrerTelegramID int64
	Credits            int // taklif qilganning yangi bonus balansi
}

// AttachReferral yangi foydalanuvchini (refereeTG) uni taklif qilgan
// foydalanuvchiga (referrerTG) bog'laydi va taklif qilganni qaytaradi.
// Bog'lanmasa (o'zini o'zi taklif qilgan, taklif qilgan botda yo'q yoki bu
// foydalanuvchi allaqachon kimdir tomonidan taklif qilingan) nil qaytaradi.
//
// Taklif qilgan users jadvalida hali bo'lmasligi mumkin (havola Telegram ID
// bo'yicha tuziladi): u botda sessiyasi bo'lsa yaratiladi — mavjud yozuvning
// username va ismi o'zgartirilmaydi. Hammasi bitta tranzaksiyada bajariladi.
func (r *BillingRepository) AttachReferral(ctx context.Context, referrerTG, refereeTG int64, username, firstName string) (*User, error) {
	if referrerTG == refereeTG {
		return nil, nil
	}
	var referrer *User
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		// Taklif qilgan botda sessiyasi bo'lsagina users ga qo'shiladi.
		if err := tx.Exec(`INSERT INTO users (telegram_user_id, username, first_name, is_admin, created_at, updated_at, free_credits)
			SELECT user_id, COALESCE(username, ''), '', FALSE, ?, ?, 0 FROM sessions WHERE user_id = ?
			ON CONFLICT (telegram_user_id) DO NOTHING`, now, now, referrerTG).Error; err != nil {
			return fmt.Errorf("storage: taklif qilgan foydalanuvchini saqlab bo'lmadi: %w", err)
		}
		var ref User
		err := tx.Where("telegram_user_id = ?", referrerTG).Take(&ref).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // havoladagi ID botda yo'q
		}
		if err != nil {
			return fmt.Errorf("storage: taklif qilgan foydalanuvchini o'qib bo'lmadi: %w", err)
		}

		referee, err := ensureUser(tx, refereeTG, username, firstName)
		if err != nil {
			return err
		}
		if referee.ID == ref.ID {
			return nil
		}
		res := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "referee_user_id"}},
			DoNothing: true,
		}).Create(&Referral{ReferrerUserID: ref.ID, RefereeUserID: referee.ID})
		if res.Error != nil {
			return fmt.Errorf("storage: taklifni saqlab bo'lmadi: %w", res.Error)
		}
		if res.RowsAffected == 1 {
			referrer = &ref
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return referrer, nil
}

// RewardReferral taklif qilingan foydalanuvchi (refereeUserID) buyurtmani
// to'laganda uni taklif qilganga 1 ta bepul imkoniyat beradi. Mukofot har bir
// taklif uchun faqat bir marta beriladi: foydalanuvchi taklif qilinmagan yoki
// mukofot allaqachon berilgan bo'lsa, hech narsa qilmaydi va nil qaytaradi
// (shuning uchun qayta chaqirish xavfsiz).
func (r *BillingRepository) RewardReferral(ctx context.Context, refereeUserID, orderID uint) (*ReferralReward, error) {
	var reward *ReferralReward
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var referrers []uint
		if err := tx.Raw(`UPDATE referrals SET rewarded_at = ?, reward_order_id = ?
			WHERE referee_user_id = ? AND rewarded_at IS NULL
			RETURNING referrer_user_id`, now, orderID, refereeUserID).Scan(&referrers).Error; err != nil {
			return fmt.Errorf("storage: taklif mukofotini yozib bo'lmadi: %w", err)
		}
		if len(referrers) == 0 {
			return nil
		}
		var rows []struct {
			TelegramUserID int64
			FreeCredits    int
		}
		if err := tx.Raw(`UPDATE users SET free_credits = free_credits + 1, updated_at = ?
			WHERE id = ? RETURNING telegram_user_id, free_credits`, now, referrers[0]).Scan(&rows).Error; err != nil {
			return fmt.Errorf("storage: bonusni yozib bo'lmadi: %w", err)
		}
		if len(rows) != 1 {
			return fmt.Errorf("storage: taklif qilgan foydalanuvchi #%d topilmadi", referrers[0])
		}
		reward = &ReferralReward{ReferrerTelegramID: rows[0].TelegramUserID, Credits: rows[0].FreeCredits}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return reward, nil
}

// ReferralStats foydalanuvchi nechta do'stini taklif qilgani (invited) va
// ulardan nechtasi buyurtmani to'lab, mukofot keltirganini (rewarded) qaytaradi.
func (r *BillingRepository) ReferralStats(ctx context.Context, userID uint) (invited, rewarded int, err error) {
	var row struct{ Invited, Rewarded int }
	if err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) AS invited, COUNT(rewarded_at) AS rewarded
		FROM referrals WHERE referrer_user_id = ?`, userID).Scan(&row).Error; err != nil {
		return 0, 0, fmt.Errorf("storage: takliflar sonini o'qib bo'lmadi: %w", err)
	}
	return row.Invited, row.Rewarded, nil
}

// CreateBonusOrder buyurtmani taklif bonusi hisobidan yaratadi: bitta
// tranzaksiyada foydalanuvchining bonus balansidan 1 ta ayiriladi va buyurtma
// saqlanadi. o.Discount (bonus qoplagan summa) musbat bo'lishi shart; o.Price —
// foydalanuvchi to'laydigan qolgan qism (0 bo'lsa buyurtma darhol "paid"
// holatida yaratilishi kerak). Bonus qolmagan bo'lsa ErrNoCredits qaytaradi.
// remaining — bonus balansining qolgani.
func (r *BillingRepository) CreateBonusOrder(ctx context.Context, o *Order) (remaining int, err error) {
	if o.Discount <= 0 {
		return 0, errors.New("storage: bonusli buyurtmada chegirma (discount) musbat bo'lishi kerak")
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var left []int
		if err := tx.Raw(`UPDATE users SET free_credits = free_credits - 1, updated_at = ?
			WHERE id = ? AND free_credits > 0 RETURNING free_credits`, time.Now().UTC(), o.UserID).Scan(&left).Error; err != nil {
			return fmt.Errorf("storage: bonusni ishlatib bo'lmadi: %w", err)
		}
		if len(left) == 0 {
			return ErrNoCredits
		}
		if err := tx.Create(o).Error; err != nil {
			return fmt.Errorf("storage: buyurtma yaratib bo'lmadi: %w", err)
		}
		remaining = left[0]
		return nil
	})
	if err != nil {
		return 0, err
	}
	return remaining, nil
}

// ReleaseOrder foydalanuvchi voz kechgan buyurtmani bekor qiladi:
//   - to'lov kutilayotgan ("pending_payment") buyurtma;
//   - taklif bonusi hisobidan to'liq bepul yaratilgan, lekin yetkazilmagan
//     ("paid", price = 0, discount > 0) buyurtma.
//
// Buyurtmada bonus ishlatilgan bo'lsa (discount > 0), bonus foydalanuvchi
// balansiga qaytariladi. Haqiqiy pul to'langan va eski bepul (sinov)
// buyurtmalarga tegilmaydi — ular keyingi buyurtmada ishlatiladi (FindCredit).
// Buyurtma bekor qilingan bo'lsa released = true.
func (r *BillingRepository) ReleaseOrder(ctx context.Context, id uint) (released bool, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var rows []struct {
			UserID   uint
			Discount int
		}
		if err := tx.Raw(`UPDATE orders SET status = ?, updated_at = ?
			WHERE id = ? AND (status = ? OR (status = ? AND price = 0 AND discount > 0))
			RETURNING user_id, discount`,
			OrderCancelled, now, id, OrderPending, OrderPaid).Scan(&rows).Error; err != nil {
			return fmt.Errorf("storage: buyurtmani bekor qilib bo'lmadi: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		released = true
		if rows[0].Discount <= 0 {
			return nil
		}
		if err := tx.Exec(`UPDATE users SET free_credits = free_credits + 1, updated_at = ? WHERE id = ?`,
			now, rows[0].UserID).Error; err != nil {
			return fmt.Errorf("storage: bonusni qaytarib bo'lmadi: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return released, nil
}
