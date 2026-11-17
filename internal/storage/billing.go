package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Buyurtma (narx) toifalari.
const (
	KindPhoto = "photo" // rasmli taqdimot
	KindPlain = "plain" // rasmsiz taqdimot, mustaqil ish va amaliy topshiriq
)

// Buyurtma holatlari. Qiymatlar bazada saqlanadi, shuning uchun o'zgartirmang.
//
//	pending_payment → review → paid → delivered
//	        ↘ cancelled   ↘ pending_payment (admin rad etsa)
//
// Bepul buyurtma darhol "paid" holatida yaratiladi.
const (
	OrderPending   = "pending_payment" // to'lov (chek) kutilmoqda
	OrderReview    = "review"          // chek admin tekshiruvida
	OrderPaid      = "paid"            // to'langan (yoki bepul), fayl hali yetkazilmagan
	OrderDelivered = "delivered"       // fayl foydalanuvchiga yuborilgan
	OrderCancelled = "cancelled"       // to'lovsiz bekor qilingan
)

// To'lov (chek) holatlari.
const (
	PaymentApproved = "approved" // tasdiqlangan (AI yoki admin)
	PaymentReview   = "review"   // shubhali — admin qarorini kutmoqda
	PaymentRejected = "rejected" // rad etilgan (chek emas yoki admin rad etgan)
)

// ErrConflict — yozuv kutilgan holatda emas (masalan, to'lov allaqachon
// boshqa admin tomonidan ko'rib chiqilgan).
var ErrConflict = errors.New("storage: yozuv holati o'zgargan")

// User — bot foydalanuvchisi. ID — ichki identifikator (userId),
// TelegramUserID — Telegram'dagi identifikator.
type User struct {
	ID             uint   `gorm:"primaryKey"`
	TelegramUserID int64  `gorm:"not null;uniqueIndex"`
	Username       string `gorm:"size:64"`
	FirstName      string `gorm:"size:128"`
	IsAdmin        bool   `gorm:"not null;default:false;index"`
	AdminSince     *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Order — tasdiqlangan buyurtma nusxasi (to'lov qaysi ish uchun ekanini
// saqlash uchun). Foydalanuvchi keyin sessiyani o'zgartirsa ham buyurtma
// ma'lumotlari shu yerda qoladi.
type Order struct {
	ID             uint  `gorm:"primaryKey"`
	UserID         uint  `gorm:"not null;index:idx_orders_user_status,priority:1"`
	TelegramUserID int64 `gorm:"not null;index"`

	WorkType     string `gorm:"size:32;not null"`
	Topic        string `gorm:"size:512"`
	Volume       int    `gorm:"not null;default:0"`
	Design       string `gorm:"size:32"`
	Language     string `gorm:"size:16"`
	Requirements string `gorm:"size:1024"`
	StudentName  string `gorm:"size:128"`
	University   string `gorm:"size:256"`
	TeacherName  string `gorm:"size:128"`

	Kind   string `gorm:"size:16;not null"`
	Price  int    `gorm:"not null;default:0"` // so'mda; bepul buyurtmada 0
	IsFree bool   `gorm:"not null;default:false"`
	IsPaid bool   `gorm:"not null;default:false"`
	Status string `gorm:"size:16;not null;default:pending_payment;index:idx_orders_user_status,priority:2"`

	PaidAt      *time.Time
	DeliveredAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Payment — foydalanuvchi yuborgan bitta to'lov cheki va uni tekshirish natijasi.
type Payment struct {
	ID             uint   `gorm:"primaryKey"`
	UserID         uint   `gorm:"not null;index"`
	TelegramUserID int64  `gorm:"not null;index"`
	Username       string `gorm:"size:64"`
	OrderID        uint   `gorm:"not null;index"`
	Kind           string `gorm:"size:16;not null"`

	// Amount — to'lov summasi (buyurtma narxi), so'mda.
	Amount int `gorm:"not null"`

	// Chekdan AI o'qigan qiymatlar (o'qilmagan bo'lsa — bo'sh).
	DetectedAmount int    `gorm:"not null;default:0"`
	DetectedLast4  string `gorm:"size:8"`
	DetectedPaidAt *time.Time
	TransactionID  string `gorm:"size:128;index"`

	Status string `gorm:"size:16;not null;index"`
	Reason string `gorm:"size:1024"` // qaror sabablari (admin uchun)
	AIRaw  string `gorm:"type:text"` // AI ning xom javobi

	// Chek rasmi: Telegram file_id (qayta yuborish uchun), file_unique_id
	// (dublikatni aniqlash uchun) va foydalanuvchi chatidagi xabar ID si.
	CheckFileID       string `gorm:"size:256;not null"`
	CheckFileUniqueID string `gorm:"size:128;index"`
	CheckFileType     string `gorm:"size:16;not null;default:photo"` // photo | document
	CheckMessageID    int    `gorm:"not null"`

	ReviewedBy *int64 // tasdiqlagan/rad etgan adminning Telegram ID si
	ReviewedAt *time.Time

	CreatedAt time.Time `gorm:"index"`
	UpdatedAt time.Time
}

// BillingRepository foydalanuvchilar, buyurtmalar va to'lovlarni saqlaydi.
type BillingRepository struct {
	db *gorm.DB
}

// NewBillingRepository yangi repozitoriy yaratadi.
func NewBillingRepository(db *gorm.DB) *BillingRepository {
	return &BillingRepository{db: db}
}

// ---------------------------------------------------------------------------
// Foydalanuvchilar
// ---------------------------------------------------------------------------

// EnsureUser foydalanuvchini qaytaradi; mavjud bo'lmasa — yaratadi,
// mavjud bo'lsa — username va ismini yangilaydi.
func (r *BillingRepository) EnsureUser(ctx context.Context, telegramID int64, username, firstName string) (*User, error) {
	db := r.db.WithContext(ctx)
	u := User{TelegramUserID: telegramID, Username: username, FirstName: firstName}
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "telegram_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"username", "first_name", "updated_at"}),
	}).Create(&u).Error
	if err != nil {
		return nil, fmt.Errorf("storage: foydalanuvchini saqlab bo'lmadi: %w", err)
	}
	if err := db.First(&u, "telegram_user_id = ?", telegramID).Error; err != nil {
		return nil, fmt.Errorf("storage: foydalanuvchini o'qib bo'lmadi: %w", err)
	}
	return &u, nil
}

// SetAdmin foydalanuvchini admin qiladi. Allaqachon admin bo'lsa false qaytaradi.
func (r *BillingRepository) SetAdmin(ctx context.Context, telegramID int64) (bool, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&User{}).
		Where("telegram_user_id = ? AND is_admin = ?", telegramID, false).
		Updates(map[string]any{"is_admin": true, "admin_since": now, "updated_at": now})
	if res.Error != nil {
		return false, fmt.Errorf("storage: adminni saqlab bo'lmadi: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// IsAdmin foydalanuvchi admin ekanini bildiradi.
func (r *BillingRepository) IsAdmin(ctx context.Context, telegramID int64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&User{}).
		Where("telegram_user_id = ? AND is_admin = ?", telegramID, true).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("storage: admin holatini o'qib bo'lmadi: %w", err)
	}
	return n > 0, nil
}

// ListAdmins barcha adminlarni qaytaradi.
func (r *BillingRepository) ListAdmins(ctx context.Context) ([]User, error) {
	var list []User
	if err := r.db.WithContext(ctx).Where("is_admin = ?", true).Order("id").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("storage: adminlar ro'yxatini o'qib bo'lmadi: %w", err)
	}
	return list, nil
}

// ---------------------------------------------------------------------------
// Buyurtmalar
// ---------------------------------------------------------------------------

// CountFreeUsed foydalanuvchi berilgan toifadagi bepul urinishlardan nechtasini
// ishlatganini qaytaradi (bekor qilinganlar hisobga olinmaydi).
func (r *BillingRepository) CountFreeUsed(ctx context.Context, userID uint, kind string) (int, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&Order{}).
		Where("user_id = ? AND kind = ? AND is_free = ? AND status IN ?", userID, kind, true, []string{OrderPaid, OrderDelivered}).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("storage: bepul urinishlarni sanab bo'lmadi: %w", err)
	}
	return int(n), nil
}

// FindCredit to'langan, lekin hali yetkazilmagan buyurtmani qaytaradi
// (generatsiya xato bilan tugagan yoki foydalanuvchi uni bekor qilgan bo'lsa).
// Bepul buyurtma faqat o'z toifasiga, pullik buyurtma esa narxi yetadigan
// istalgan toifaga ishlatiladi. Topilmasa nil qaytaradi.
func (r *BillingRepository) FindCredit(ctx context.Context, userID uint, kind string, price int) (*Order, error) {
	var o Order
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND status = ?", userID, OrderPaid).
		Where(r.db.Where("is_free = ? AND kind = ?", true, kind).Or("is_free = ? AND price >= ?", false, price)).
		Order("id").First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: to'langan buyurtmani o'qib bo'lmadi: %w", err)
	}
	return &o, nil
}

// CreateOrder yangi buyurtma yaratadi.
func (r *BillingRepository) CreateOrder(ctx context.Context, o *Order) error {
	if err := r.db.WithContext(ctx).Create(o).Error; err != nil {
		return fmt.Errorf("storage: buyurtma yaratib bo'lmadi: %w", err)
	}
	return nil
}

// SaveOrder buyurtmaning barcha maydonlarini saqlaydi.
func (r *BillingRepository) SaveOrder(ctx context.Context, o *Order) error {
	if err := r.db.WithContext(ctx).Save(o).Error; err != nil {
		return fmt.Errorf("storage: buyurtmani saqlab bo'lmadi: %w", err)
	}
	return nil
}

// GetOrder buyurtmani qaytaradi.
func (r *BillingRepository) GetOrder(ctx context.Context, id uint) (*Order, error) {
	var o Order
	if err := r.db.WithContext(ctx).First(&o, id).Error; err != nil {
		return nil, fmt.Errorf("storage: buyurtmani o'qib bo'lmadi: %w", err)
	}
	return &o, nil
}

// SetOrderStatus buyurtma holatini faqat joriy holat `from` bo'lsa `to` ga
// o'zgartiradi (atomar). "delivered" holatida yetkazilgan vaqt ham yoziladi.
func (r *BillingRepository) SetOrderStatus(ctx context.Context, id uint, from, to string) (bool, error) {
	now := time.Now().UTC()
	upd := map[string]any{"status": to, "updated_at": now}
	if to == OrderDelivered {
		upd["delivered_at"] = now
	}
	res := r.db.WithContext(ctx).Model(&Order{}).Where("id = ? AND status = ?", id, from).Updates(upd)
	if res.Error != nil {
		return false, fmt.Errorf("storage: buyurtma holatini o'zgartirib bo'lmadi: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// ---------------------------------------------------------------------------
// To'lovlar
// ---------------------------------------------------------------------------

// SubmitPayment chekni saqlaydi va bitta tranzaksiyada buyurtmani
// "pending_payment" holatidan chek natijasiga mos holatga o'tkazadi:
// approved → paid, review → review, rejected → pending_payment (o'zgarmaydi).
// Buyurtma kutilgan holatda bo'lmasa ErrConflict qaytaradi.
func (r *BillingRepository) SubmitPayment(ctx context.Context, p *Payment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var (
			to  string
			now = time.Now().UTC()
			upd = map[string]any{"updated_at": now}
		)
		switch p.Status {
		case PaymentApproved:
			to = OrderPaid
			upd["is_paid"], upd["paid_at"] = true, now
		case PaymentReview:
			to = OrderReview
		default:
			to = OrderPending
		}
		upd["status"] = to

		res := tx.Model(&Order{}).Where("id = ? AND status = ?", p.OrderID, OrderPending).Updates(upd)
		if res.Error != nil {
			return fmt.Errorf("storage: buyurtma holatini o'zgartirib bo'lmadi: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.Create(p).Error; err != nil {
			return fmt.Errorf("storage: to'lovni saqlab bo'lmadi: %w", err)
		}
		return nil
	})
}

// ReviewPayment admin qarorini qo'llaydi: shubhali to'lovni tasdiqlaydi
// (buyurtma → paid) yoki rad etadi (buyurtma → pending_payment, foydalanuvchi
// boshqa chek yuborishi mumkin). To'lov allaqachon ko'rib chiqilgan bo'lsa
// ErrConflict qaytaradi.
func (r *BillingRepository) ReviewPayment(ctx context.Context, paymentID uint, approve bool, adminID int64) (*Payment, *Order, error) {
	var (
		p Payment
		o Order
	)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		payStatus, orderStatus := PaymentRejected, OrderPending
		orderUpd := map[string]any{"updated_at": now}
		if approve {
			payStatus, orderStatus = PaymentApproved, OrderPaid
			orderUpd["is_paid"], orderUpd["paid_at"] = true, now
		}
		orderUpd["status"] = orderStatus

		res := tx.Model(&Payment{}).Where("id = ? AND status = ?", paymentID, PaymentReview).
			Updates(map[string]any{"status": payStatus, "reviewed_by": adminID, "reviewed_at": now, "updated_at": now})
		if res.Error != nil {
			return fmt.Errorf("storage: to'lov holatini o'zgartirib bo'lmadi: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.First(&p, paymentID).Error; err != nil {
			return fmt.Errorf("storage: to'lovni o'qib bo'lmadi: %w", err)
		}
		res = tx.Model(&Order{}).Where("id = ? AND status = ?", p.OrderID, OrderReview).Updates(orderUpd)
		if res.Error != nil {
			return fmt.Errorf("storage: buyurtma holatini o'zgartirib bo'lmadi: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.First(&o, p.OrderID).Error; err != nil {
			return fmt.Errorf("storage: buyurtmani o'qib bo'lmadi: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &p, &o, nil
}

// GetPayment to'lovni qaytaradi.
func (r *BillingRepository) GetPayment(ctx context.Context, id uint) (*Payment, error) {
	var p Payment
	if err := r.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, fmt.Errorf("storage: to'lovni o'qib bo'lmadi: %w", err)
	}
	return &p, nil
}

// ReceiptSeen shu chek (bir xil rasm fayli yoki tranzaksiya raqami) oldin
// yuborilganini bildiradi — bitta chek bilan bir necha marta to'lamaslik uchun.
func (r *BillingRepository) ReceiptSeen(ctx context.Context, transactionID, fileUniqueID string) (bool, error) {
	if transactionID == "" && fileUniqueID == "" {
		return false, nil
	}
	q := r.db.WithContext(ctx).Model(&Payment{})
	switch {
	case transactionID != "" && fileUniqueID != "":
		q = q.Where("transaction_id = ? OR check_file_unique_id = ?", transactionID, fileUniqueID)
	case transactionID != "":
		q = q.Where("transaction_id = ?", transactionID)
	default:
		q = q.Where("check_file_unique_id = ?", fileUniqueID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return false, fmt.Errorf("storage: cheklarni tekshirib bo'lmadi: %w", err)
	}
	return n > 0, nil
}

// ListPayments to'lovlarni (yangilari birinchi) qaytaradi. since nil bo'lsa — barchasi.
func (r *BillingRepository) ListPayments(ctx context.Context, since *time.Time) ([]Payment, error) {
	q := r.db.WithContext(ctx).Order("created_at DESC, id DESC")
	if since != nil {
		q = q.Where("created_at >= ?", since.UTC())
	}
	var list []Payment
	if err := q.Find(&list).Error; err != nil {
		return nil, fmt.Errorf("storage: to'lovlar ro'yxatini o'qib bo'lmadi: %w", err)
	}
	return list, nil
}

// ListPaymentsByStatus berilgan holatdagi to'lovlarni (eskilari birinchi)
// qaytaradi. limit > 0 bo'lsa, ko'pi bilan shuncha yozuv; total — jami soni.
func (r *BillingRepository) ListPaymentsByStatus(ctx context.Context, status string, limit int) (list []Payment, total int, err error) {
	db := r.db.WithContext(ctx)
	var n int64
	if err := db.Model(&Payment{}).Where("status = ?", status).Count(&n).Error; err != nil {
		return nil, 0, fmt.Errorf("storage: to'lovlarni sanab bo'lmadi: %w", err)
	}
	q := db.Where("status = ?", status).Order("created_at, id")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&list).Error; err != nil {
		return nil, 0, fmt.Errorf("storage: to'lovlar ro'yxatini o'qib bo'lmadi: %w", err)
	}
	return list, int(n), nil
}
