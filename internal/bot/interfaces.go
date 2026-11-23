package bot

import (
	"context"
	"time"

	"telegram-presentation-bot/internal/backup"
	"telegram-presentation-bot/internal/payment"
	"telegram-presentation-bot/internal/storage"
)

// SessionStore — foydalanuvchi sessiyalari ombori.
type SessionStore interface {
	GetOrCreate(ctx context.Context, userID, chatID int64, username string) (*storage.Session, error)
	Get(ctx context.Context, userID int64) (*storage.Session, error)
	Save(ctx context.Context, s *storage.Session) error
	CompareAndSwapState(ctx context.Context, userID int64, from, to string) (bool, error)
	ListByState(ctx context.Context, state string) ([]storage.Session, error)
}

// BillingStore — foydalanuvchilar, buyurtmalar va to'lovlar ombori.
type BillingStore interface {
	EnsureUser(ctx context.Context, telegramID int64, username, firstName string) (*storage.User, error)
	SetAdmin(ctx context.Context, telegramID int64) (bool, error)
	IsAdmin(ctx context.Context, telegramID int64) (bool, error)
	ListAdmins(ctx context.Context) ([]storage.User, error)

	// Bepul urinishlar hozircha o'chirilgan — barcha ishlar pullik.
	// CountFreeUsed(ctx context.Context, userID uint, kind string) (int, error)
	FindCredit(ctx context.Context, userID uint, kind string, price int) (*storage.Order, error)
	CreateOrder(ctx context.Context, o *storage.Order) error
	SaveOrder(ctx context.Context, o *storage.Order) error
	GetOrder(ctx context.Context, id uint) (*storage.Order, error)
	SetOrderStatus(ctx context.Context, id uint, from, to string) (bool, error)

	SubmitPayment(ctx context.Context, p *storage.Payment) error
	ReviewPayment(ctx context.Context, paymentID uint, approve bool, adminID int64) (*storage.Payment, *storage.Order, error)
	GetPayment(ctx context.Context, id uint) (*storage.Payment, error)
	OrderPayment(ctx context.Context, orderID uint) (*storage.Payment, error)
	ReceiptSeen(ctx context.Context, transactionID, fileUniqueID string) (bool, error)
	ListPayments(ctx context.Context, since *time.Time) ([]storage.Payment, error)
	ListPaymentsByStatus(ctx context.Context, status string, limit int) ([]storage.Payment, int, error)
}

// ReceiptVerifier — chek rasmini o'qiydigan AI (internal/payment).
type ReceiptVerifier interface {
	Read(ctx context.Context, image []byte, exp payment.Expectation) (*payment.Receipt, error)
}

// StatsStore — tashriflar ombori (internal/storage.StatsRepository).
type StatsStore interface {
	RecordVisit(ctx context.Context, userID int64, at time.Time) error
	Overview(ctx context.Context, sinces []time.Time) (all storage.PeriodStats, periods []storage.PeriodStats, err error)
	DailyStats(ctx context.Context, from time.Time, days int) ([]storage.DayStats, error)
	ListUsers(ctx context.Context, since *time.Time) ([]storage.UserSummary, error)
}

// Backuper — bazaning zaxira nusxasini oluvchi (internal/backup.Dumper).
type Backuper interface {
	Dump(ctx context.Context, now time.Time) (*backup.File, error)
}
