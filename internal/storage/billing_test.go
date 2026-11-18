package storage

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Integratsion test: haqiqiy PostgreSQL talab qiladi. Ishga tushirish:
//
//	TEST_DATABASE_DSN="postgres://postgres:postgres@localhost:55432/test?sslmode=disable" go test ./internal/storage/
//
// Diqqat: test jadvallarni tozalaydi — faqat sinov bazasida ishlating.
func testDB(t *testing.T) *gorm.DB {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN berilmagan")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&Visit{}, &Payment{}, &Order{}, &User{}, &Session{}); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestBillingFlow(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewBillingRepository(db)

	// Foydalanuvchi: upsert va admin.
	u, err := r.EnsureUser(ctx, 1001, "ali", "Ali")
	if err != nil || u.ID == 0 || u.IsAdmin {
		t.Fatalf("EnsureUser: %+v %v", u, err)
	}
	u2, err := r.EnsureUser(ctx, 1001, "ali_new", "Ali")
	if err != nil || u2.ID != u.ID || u2.Username != "ali_new" {
		t.Fatalf("EnsureUser qayta: %+v %v", u2, err)
	}
	if added, err := r.SetAdmin(ctx, 1001); err != nil || !added {
		t.Fatalf("SetAdmin: %v %v", added, err)
	}
	if added, _ := r.SetAdmin(ctx, 1001); added {
		t.Error("ikkinchi SetAdmin false qaytarishi kerak")
	}
	if ok, _ := r.IsAdmin(ctx, 1001); !ok {
		t.Error("IsAdmin true bo'lishi kerak")
	}
	if admins, _ := r.ListAdmins(ctx); len(admins) != 1 {
		t.Errorf("adminlar: %d", len(admins))
	}

	// Bepul buyurtma.
	now := time.Now().UTC()
	free := &Order{UserID: u.ID, TelegramUserID: 1001, WorkType: "presentation", Kind: KindPlain,
		IsFree: true, IsPaid: true, Status: OrderPaid, PaidAt: &now}
	if err := r.CreateOrder(ctx, free); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.CountFreeUsed(ctx, u.ID, KindPlain); n != 1 {
		t.Errorf("CountFreeUsed(plain) = %d", n)
	}
	if n, _ := r.CountFreeUsed(ctx, u.ID, KindPhoto); n != 0 {
		t.Errorf("CountFreeUsed(photo) = %d", n)
	}
	// Bepul buyurtma faqat o'z toifasiga kredit bo'ladi.
	if c, _ := r.FindCredit(ctx, u.ID, KindPhoto, 10000); c != nil {
		t.Errorf("bepul rasmsiz buyurtma rasmli uchun kredit bo'lmasligi kerak: %+v", c)
	}
	if c, _ := r.FindCredit(ctx, u.ID, KindPlain, 7000); c == nil || c.ID != free.ID {
		t.Errorf("bepul buyurtma o'z toifasida kredit bo'lishi kerak: %+v", c)
	}
	if ok, _ := r.SetOrderStatus(ctx, free.ID, OrderPaid, OrderDelivered); !ok {
		t.Error("delivered ga o'tmadi")
	}
	if got, _ := r.GetOrder(ctx, free.ID); got.DeliveredAt == nil {
		t.Error("DeliveredAt yozilmadi")
	}
	if c, _ := r.FindCredit(ctx, u.ID, KindPlain, 7000); c != nil {
		t.Error("yetkazilgan buyurtma kredit bo'lmasligi kerak")
	}

	// Pullik buyurtma: shubhali chek → admin tasdiqlaydi.
	o := &Order{UserID: u.ID, TelegramUserID: 1001, WorkType: "presentation", Kind: KindPhoto, Price: 10000, Status: OrderPending}
	if err := r.CreateOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.GetOrder(ctx, o.ID); got.IsPaid || got.Status != OrderPending {
		t.Fatalf("yangi buyurtma to'lanmagan bo'lishi kerak: %+v", got)
	}
	p := &Payment{UserID: u.ID, TelegramUserID: 1001, OrderID: o.ID, Kind: KindPhoto, Amount: 10000,
		Status: PaymentReview, CheckFileID: "file-1", CheckFileUniqueID: "uniq-1", CheckMessageID: 77, TransactionID: "TX1"}
	if err := r.SubmitPayment(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.GetOrder(ctx, o.ID); got.Status != OrderReview {
		t.Errorf("buyurtma review holatida bo'lishi kerak: %s", got.Status)
	}
	if err := r.SubmitPayment(ctx, &Payment{UserID: u.ID, TelegramUserID: 1001, OrderID: o.ID, Amount: 1,
		Kind: KindPhoto, Status: PaymentReview, CheckFileID: "x"}); !errors.Is(err, ErrConflict) {
		t.Errorf("pending bo'lmagan buyurtmaga chek: ErrConflict kutilgan, olindi %v", err)
	}
	for _, q := range []struct{ tx, uniq string }{{"TX1", ""}, {"", "uniq-1"}, {"TX1", "other"}} {
		if seen, _ := r.ReceiptSeen(ctx, q.tx, q.uniq); !seen {
			t.Errorf("ReceiptSeen(%q, %q) true bo'lishi kerak", q.tx, q.uniq)
		}
	}
	if seen, _ := r.ReceiptSeen(ctx, "TX2", "uniq-2"); seen {
		t.Error("yangi chek seen bo'lmasligi kerak")
	}

	gotP, gotO, err := r.ReviewPayment(ctx, p.ID, true, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if gotP.Status != PaymentApproved || gotP.ReviewedBy == nil || *gotP.ReviewedBy != 1001 ||
		gotO.Status != OrderPaid || !gotO.IsPaid || gotO.PaidAt == nil {
		t.Errorf("tasdiqlash natijasi noto'g'ri: %+v %+v", gotP, gotO)
	}
	if _, _, err := r.ReviewPayment(ctx, p.ID, false, 1001); !errors.Is(err, ErrConflict) {
		t.Errorf("ikkinchi qaror ErrConflict berishi kerak: %v", err)
	}
	// To'langan pullik buyurtma narxi yetadigan istalgan toifaga kredit.
	if c, _ := r.FindCredit(ctx, u.ID, KindPlain, 7000); c == nil || c.ID != o.ID {
		t.Errorf("to'langan buyurtma kredit bo'lishi kerak: %+v", c)
	}

	// Rad etish: buyurtma pending ga qaytadi.
	o2 := &Order{UserID: u.ID, TelegramUserID: 1001, WorkType: "independent", Kind: KindPlain, Price: 7000, Status: OrderPending}
	_ = r.CreateOrder(ctx, o2)
	p2 := &Payment{UserID: u.ID, TelegramUserID: 1001, OrderID: o2.ID, Kind: KindPlain, Amount: 7000,
		Status: PaymentReview, CheckFileID: "file-2"}
	if err := r.SubmitPayment(ctx, p2); err != nil {
		t.Fatal(err)
	}
	if _, gotO, err := r.ReviewPayment(ctx, p2.ID, false, 1001); err != nil || gotO.Status != OrderPending || gotO.IsPaid {
		t.Errorf("rad etish: %+v %v", gotO, err)
	}

	// Avtomatik tasdiq: buyurtma darhol paid.
	p3 := &Payment{UserID: u.ID, TelegramUserID: 1001, OrderID: o2.ID, Kind: KindPlain, Amount: 7000,
		Status: PaymentApproved, CheckFileID: "file-3"}
	if err := r.SubmitPayment(ctx, p3); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.GetOrder(ctx, o2.ID); got.Status != OrderPaid || !got.IsPaid {
		t.Errorf("avtomatik tasdiqdan keyin: %+v", got)
	}
	// Buyurtma to'lovi: rad etilgan chek (p2) emas, tasdiqlangani (p3) qaytadi.
	if got, err := r.OrderPayment(ctx, o2.ID); err != nil || got == nil || got.ID != p3.ID {
		t.Errorf("OrderPayment(o2) = %+v, %v; kutilgan chek #%d", got, err, p3.ID)
	}
	if got, err := r.OrderPayment(ctx, o2.ID+1000); err != nil || got != nil {
		t.Errorf("to'lovsiz buyurtma uchun nil kutilgan: %+v, %v", got, err)
	}

	// Hisobot.
	all, err := r.ListPayments(ctx, nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("ListPayments: %d %v", len(all), err)
	}
	if all[0].ID != p3.ID {
		t.Errorf("yangilari birinchi bo'lishi kerak: %d", all[0].ID)
	}
	future := time.Now().Add(time.Hour)
	if list, _ := r.ListPayments(ctx, &future); len(list) != 0 {
		t.Errorf("kelajakdagi davrda to'lov bo'lmasligi kerak: %d", len(list))
	}

	// Tekshiruvdagi cheklar (/tekshiruv): eskilari birinchi, limit va jami soni.
	if list, total, err := r.ListPaymentsByStatus(ctx, PaymentReview, 0); err != nil || total != 0 || len(list) != 0 {
		t.Errorf("tekshiruvda chek bo'lmasligi kerak: %d %d %v", len(list), total, err)
	}
	var review []uint
	for i := 0; i < 3; i++ {
		ro := &Order{UserID: u.ID, TelegramUserID: 1001, WorkType: "presentation", Kind: KindPlain, Price: 7000, Status: OrderPending}
		if err := r.CreateOrder(ctx, ro); err != nil {
			t.Fatal(err)
		}
		rp := &Payment{UserID: u.ID, TelegramUserID: 1001, OrderID: ro.ID, Kind: KindPlain, Amount: 7000,
			Status: PaymentReview, CheckFileID: "file-r"}
		if err := r.SubmitPayment(ctx, rp); err != nil {
			t.Fatal(err)
		}
		review = append(review, rp.ID)
	}
	if list, total, err := r.ListPaymentsByStatus(ctx, PaymentReview, 2); err != nil || total != 3 || len(list) != 2 ||
		list[0].ID != review[0] || list[1].ID != review[1] {
		t.Errorf("ListPaymentsByStatus(limit 2): %+v total=%d %v", list, total, err)
	}
	if list, total, _ := r.ListPaymentsByStatus(ctx, PaymentReview, 0); total != 3 || len(list) != 3 {
		t.Errorf("ListPaymentsByStatus(limitsiz): %d total=%d", len(list), total)
	}

	// Sessiyada OrderID saqlanadi.
	sr := NewSessionRepository(db)
	s, err := sr.GetOrCreate(ctx, 1001, 1001, "ali")
	if err != nil {
		t.Fatal(err)
	}
	s.OrderID = &o2.ID
	if err := sr.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	if got, _ := sr.Get(ctx, 1001); got.OrderID == nil || *got.OrderID != o2.ID {
		t.Errorf("Session.OrderID saqlanmadi: %v", got.OrderID)
	}

	// Migratsiya mavjud jadvallarda qayta ishga tushganda xato bermasligi kerak.
	if err := Migrate(db); err != nil {
		t.Fatalf("takroriy migratsiya: %v", err)
	}
}

func TestAIKind(t *testing.T) {
	for _, tariff := range []string{"standard", "optimal", "premium"} {
		k := AIKind(tariff)
		if len(k) > 16 {
			t.Errorf("%s: toifa %q 16 belgidan uzun (orders.kind — varchar(16))", tariff, k)
		}
		if got, ok := KindTariff(k); !ok || got != tariff {
			t.Errorf("KindTariff(%q) = %q, %v", k, got, ok)
		}
	}
	for _, k := range []string{KindPhoto, KindPlain, "ai_", ""} {
		if _, ok := KindTariff(k); ok {
			t.Errorf("KindTariff(%q) AI toifa emas", k)
		}
	}
}
