package storage

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"
)

// Integratsion testlar: BillingRepository ning har bir usuli alohida
// (umumiy ssenariy — billing_test.go dagi TestBillingFlow).

// mkUser test foydalanuvchisini yaratadi.
func mkUser(t *testing.T, r *BillingRepository, telegramID int64) *User {
	t.Helper()
	u, err := r.EnsureUser(context.Background(), telegramID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// mkOrder buyurtmani berilgan holatda yaratadi (paid/delivered — to'langan).
func mkOrder(t *testing.T, r *BillingRepository, u *User, kind string, price int, free bool, status string) *Order {
	t.Helper()
	o := &Order{UserID: u.ID, TelegramUserID: u.TelegramUserID, WorkType: "presentation",
		Kind: kind, Price: price, IsFree: free, Status: status}
	if status == OrderPaid || status == OrderDelivered {
		now := time.Now().UTC()
		o.IsPaid, o.PaidAt = true, &now
	}
	if err := r.CreateOrder(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	return o
}

// newPayment buyurtma uchun saqlanmagan chek qaytaradi.
func newPayment(u *User, o *Order, status string) *Payment {
	return &Payment{UserID: u.ID, TelegramUserID: u.TelegramUserID, OrderID: o.ID, Kind: o.Kind,
		Amount: o.Price, Status: status, CheckFileID: "file", CheckMessageID: 1}
}

// insertPayment chekni (buyurtmasiz) to'g'ridan-to'g'ri yozadi.
func insertPayment(t *testing.T, db *gorm.DB, status string, created time.Time) uint {
	t.Helper()
	p := &Payment{UserID: 1, TelegramUserID: 1, OrderID: 1, Kind: KindPlain, Amount: 7000,
		Status: status, CheckFileID: "file", CreatedAt: created, UpdatedAt: created}
	if err := db.Create(p).Error; err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func paymentIDs(list []Payment) []uint {
	out := make([]uint, len(list))
	for i, p := range list {
		out[i] = p.ID
	}
	return out
}

// recent — t hozirgi vaqtdan bir daqiqadan ko'p farq qilmaydi.
func recent(t *time.Time) bool {
	return t != nil && time.Since(*t).Abs() < time.Minute
}

// ---------------------------------------------------------------------------
// Foydalanuvchilar
// ---------------------------------------------------------------------------

func TestEnsureUser(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewBillingRepository(db)

	// matchesDB — EnsureUser qaytargan qiymatlar bazadagi yozuv bilan bir xil
	// (RETURNING Go tomonidagi qiymatlarni emas, bazadagisini qaytarishi kerak).
	matchesDB := func(t *testing.T, got *User) {
		t.Helper()
		var want User
		if err := db.First(&want, "telegram_user_id = ?", got.TelegramUserID).Error; err != nil {
			t.Fatal(err)
		}
		sameTime := func(a, b *time.Time) bool {
			return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
		}
		if got.ID != want.ID || got.IsAdmin != want.IsAdmin || !sameTime(got.AdminSince, want.AdminSince) ||
			!got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Errorf("qaytgan foydalanuvchi bazadagidan farq qiladi:\n got %+v\nwant %+v", got, want)
		}
	}

	u, err := r.EnsureUser(ctx, 500, "ali", "Ali")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 || u.TelegramUserID != 500 || u.Username != "ali" || u.FirstName != "Ali" ||
		u.IsAdmin || u.AdminSince != nil {
		t.Fatalf("yangi foydalanuvchi: %+v", u)
	}
	matchesDB(t, u)
	if ok, err := r.SetAdmin(ctx, 500); err != nil || !ok {
		t.Fatalf("SetAdmin: %v %v", ok, err)
	}

	// Konflikt: username va ism yangilanadi, qolgan maydonlar saqlanadi.
	u2, err := r.EnsureUser(ctx, 500, "vali", "Vali")
	if err != nil {
		t.Fatal(err)
	}
	if u2.ID != u.ID || u2.Username != "vali" || u2.FirstName != "Vali" {
		t.Errorf("qayta EnsureUser: %+v", u2)
	}
	if !u2.IsAdmin || u2.AdminSince == nil {
		t.Errorf("admin belgisi o'chib ketdi: %+v", u2)
	}
	matchesDB(t, u2)
	if !u2.CreatedAt.Equal(u.CreatedAt) {
		t.Errorf("created_at o'zgardi: %v → %v", u.CreatedAt, u2.CreatedAt)
	}
	if u2.UpdatedAt.Before(u.UpdatedAt) {
		t.Errorf("updated_at orqaga ketdi: %v → %v", u.UpdatedAt, u2.UpdatedAt)
	}

	other, err := r.EnsureUser(ctx, 501, "", "")
	if err != nil || other.ID == u.ID {
		t.Fatalf("boshqa foydalanuvchi: %+v %v", other, err)
	}
	var n int64
	if err := db.Model(&User{}).Count(&n).Error; err != nil || n != 2 {
		t.Errorf("foydalanuvchilar soni %d (%v), kutilgan 2", n, err)
	}
}

func TestAdmins(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewBillingRepository(db)

	if ok, err := r.SetAdmin(ctx, 999); err != nil || ok {
		t.Errorf("mavjud bo'lmagan foydalanuvchi: ok=%v err=%v", ok, err)
	}
	for _, id := range []int64{10, 20, 30} {
		mkUser(t, r, id)
	}
	if ok, err := r.SetAdmin(ctx, 30); err != nil || !ok {
		t.Fatalf("SetAdmin(30): ok=%v err=%v", ok, err)
	}
	if ok, err := r.SetAdmin(ctx, 30); err != nil || ok {
		t.Errorf("takroriy SetAdmin(30): ok=%v err=%v, kutilgan false", ok, err)
	}
	if ok, err := r.SetAdmin(ctx, 10); err != nil || !ok {
		t.Fatalf("SetAdmin(10): ok=%v err=%v", ok, err)
	}

	for id, want := range map[int64]bool{10: true, 20: false, 30: true, 999: false} {
		if got, err := r.IsAdmin(ctx, id); err != nil || got != want {
			t.Errorf("IsAdmin(%d) = %v (%v), kutilgan %v", id, got, err, want)
		}
	}

	// Tartib — id bo'yicha (admin bo'lgan vaqt bo'yicha emas).
	admins, err := r.ListAdmins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, a := range admins {
		got = append(got, a.TelegramUserID)
		if !a.IsAdmin || a.AdminSince == nil {
			t.Errorf("admin: %+v", a)
		}
	}
	if !slices.Equal(got, []int64{10, 30}) {
		t.Errorf("ListAdmins: %v, kutilgan [10 30]", got)
	}
}

// ---------------------------------------------------------------------------
// Buyurtmalar
// ---------------------------------------------------------------------------

func TestCountFreeUsed(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewBillingRepository(db)
	u, other := mkUser(t, r, 1), mkUser(t, r, 2)

	for _, o := range []struct {
		u      *User
		kind   string
		free   bool
		status string
	}{
		{u, KindPlain, true, OrderPaid},
		{u, KindPlain, true, OrderDelivered},
		{u, KindPlain, true, OrderCancelled}, // bekor qilingan — hisobga olinmaydi
		{u, KindPlain, true, OrderPending},
		{u, KindPlain, true, OrderReview},
		{u, KindPlain, false, OrderPaid}, // pullik
		{u, KindPlain, false, OrderDelivered},
		{u, KindPhoto, true, OrderDelivered}, // boshqa toifa
		{other, KindPlain, true, OrderPaid},  // boshqa foydalanuvchi
	} {
		mkOrder(t, r, o.u, o.kind, 0, o.free, o.status)
	}

	for _, c := range []struct {
		u    *User
		kind string
		want int
	}{
		{u, KindPlain, 2},
		{u, KindPhoto, 1},
		{u, AIKind("premium"), 0},
		{other, KindPlain, 1},
		{other, KindPhoto, 0},
	} {
		if got, err := r.CountFreeUsed(ctx, c.u.ID, c.kind); err != nil || got != c.want {
			t.Errorf("CountFreeUsed(user %d, %s) = %d (%v), kutilgan %d", c.u.TelegramUserID, c.kind, got, err, c.want)
		}
	}
}

func TestFindCredit(t *testing.T) {
	ctx := context.Background()

	// expect FindCredit natijasini tekshiradi (want nil — kredit yo'q).
	expect := func(t *testing.T, r *BillingRepository, u *User, kind string, price int, want *Order) {
		t.Helper()
		got, err := r.FindCredit(ctx, u.ID, kind, price)
		if err != nil {
			t.Fatalf("FindCredit(%s, %d): %v", kind, price, err)
		}
		switch {
		case want == nil && got != nil:
			t.Errorf("FindCredit(%s, %d) = #%d, kutilgan nil", kind, price, got.ID)
		case want != nil && got == nil:
			t.Errorf("FindCredit(%s, %d) = nil, kutilgan #%d", kind, price, want.ID)
		case want != nil && got.ID != want.ID:
			t.Errorf("FindCredit(%s, %d) = #%d, kutilgan #%d", kind, price, got.ID, want.ID)
		}
	}

	t.Run("buyurtma yo'q", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		expect(t, r, u, KindPlain, 0, nil)
	})

	t.Run("bepul faqat o'z toifasiga", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		free := mkOrder(t, r, u, KindPlain, 0, true, OrderPaid)
		expect(t, r, u, KindPlain, 7000, free)
		expect(t, r, u, KindPhoto, 0, nil)
		expect(t, r, u, AIKind("standard"), 0, nil)
	})

	t.Run("pullik narxi yetadigan istalgan toifaga", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		paid := mkOrder(t, r, u, KindPhoto, 10000, false, OrderPaid)
		expect(t, r, u, KindPhoto, 10000, paid)
		expect(t, r, u, KindPlain, 7000, paid)
		expect(t, r, u, AIKind("premium"), 10000, paid)
		expect(t, r, u, AIKind("premium"), 10001, nil)
	})

	t.Run("faqat paid holatidagilar", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		for _, status := range []string{OrderPending, OrderReview, OrderDelivered, OrderCancelled} {
			mkOrder(t, r, u, KindPlain, 0, true, status)
			mkOrder(t, r, u, KindPhoto, 50000, false, status)
		}
		expect(t, r, u, KindPlain, 0, nil)
		expect(t, r, u, KindPhoto, 7000, nil)
	})

	t.Run("boshqa foydalanuvchiniki emas", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u, other := mkUser(t, r, 1), mkUser(t, r, 2)
		mkOrder(t, r, other, KindPlain, 0, true, OrderPaid)
		mkOrder(t, r, other, KindPhoto, 10000, false, OrderPaid)
		expect(t, r, u, KindPlain, 0, nil)
	})

	t.Run("eng eskisi birinchi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		cheap := mkOrder(t, r, u, KindPlain, 5000, false, OrderPaid) // 7000 ga yetmaydi
		photo := mkOrder(t, r, u, KindPhoto, 10000, false, OrderPaid)
		free := mkOrder(t, r, u, KindPlain, 0, true, OrderPaid)
		mkOrder(t, r, u, KindPlain, 7000, false, OrderPaid)

		expect(t, r, u, KindPlain, 7000, photo)
		expect(t, r, u, KindPlain, 5000, cheap)
		if ok, err := r.SetOrderStatus(ctx, photo.ID, OrderPaid, OrderDelivered); err != nil || !ok {
			t.Fatalf("SetOrderStatus: ok=%v err=%v", ok, err)
		}
		expect(t, r, u, KindPlain, 7000, free)
	})
}

// ---------------------------------------------------------------------------
// To'lovlar
// ---------------------------------------------------------------------------

func TestSubmitPayment(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		payStatus string
		wantOrder string
		wantPaid  bool
	}{
		{PaymentApproved, OrderPaid, true},
		{PaymentReview, OrderReview, false},
		{PaymentRejected, OrderPending, false},
	} {
		t.Run(c.payStatus, func(t *testing.T) {
			db := testDB(t)
			r := NewBillingRepository(db)
			u := mkUser(t, r, 1)
			o := mkOrder(t, r, u, KindPhoto, 10000, false, OrderPending)

			p := newPayment(u, o, c.payStatus)
			if err := r.SubmitPayment(ctx, p); err != nil {
				t.Fatal(err)
			}
			if p.ID == 0 {
				t.Fatal("chek ID si qo'yilmadi")
			}
			saved, err := r.GetPayment(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Status != c.payStatus || saved.OrderID != o.ID {
				t.Errorf("saqlangan chek: %+v", saved)
			}

			got, err := r.GetOrder(ctx, o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != c.wantOrder || got.IsPaid != c.wantPaid {
				t.Errorf("buyurtma: status=%s is_paid=%v, kutilgan %s %v", got.Status, got.IsPaid, c.wantOrder, c.wantPaid)
			}
			if c.wantPaid && !recent(got.PaidAt) {
				t.Errorf("paid_at yozilmadi: %v", got.PaidAt)
			}
			if !c.wantPaid && got.PaidAt != nil {
				t.Errorf("to'lanmagan buyurtmada paid_at: %v", got.PaidAt)
			}
		})
	}
}

func TestSubmitPaymentConflict(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewBillingRepository(db)
	u := mkUser(t, r, 1)

	for _, status := range []string{OrderReview, OrderPaid, OrderDelivered, OrderCancelled} {
		o := mkOrder(t, r, u, KindPlain, 7000, false, status)
		if err := r.SubmitPayment(ctx, newPayment(u, o, PaymentApproved)); !errors.Is(err, ErrConflict) {
			t.Errorf("%s holatidagi buyurtma: %v, kutilgan ErrConflict", status, err)
		}
		if got, _ := r.GetOrder(ctx, o.ID); got.Status != status {
			t.Errorf("buyurtma holati o'zgardi: %s → %s", status, got.Status)
		}
	}
	missing := &Order{ID: 9999, Kind: KindPlain, Price: 7000}
	if err := r.SubmitPayment(ctx, newPayment(u, missing, PaymentReview)); !errors.Is(err, ErrConflict) {
		t.Errorf("mavjud bo'lmagan buyurtma: %v, kutilgan ErrConflict", err)
	}

	var n int64
	if err := db.Model(&Payment{}).Count(&n).Error; err != nil || n != 0 {
		t.Errorf("konfliktda chek yozilmasligi kerak: %d ta (%v)", n, err)
	}
}

func TestReviewPayment(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name      string
		approve   bool
		wantPay   string
		wantOrder string
	}{
		{"tasdiqlash", true, PaymentApproved, OrderPaid},
		{"rad etish", false, PaymentRejected, OrderPending},
	} {
		t.Run(c.name, func(t *testing.T) {
			db := testDB(t)
			r := NewBillingRepository(db)
			u := mkUser(t, r, 1)
			o := mkOrder(t, r, u, KindPhoto, 10000, false, OrderPending)
			p := newPayment(u, o, PaymentReview)
			if err := r.SubmitPayment(ctx, p); err != nil {
				t.Fatal(err)
			}

			gotP, gotO, err := r.ReviewPayment(ctx, p.ID, c.approve, 777)
			if err != nil {
				t.Fatal(err)
			}
			if gotP.ID != p.ID || gotP.Status != c.wantPay || gotP.ReviewedBy == nil || *gotP.ReviewedBy != 777 ||
				!recent(gotP.ReviewedAt) {
				t.Errorf("chek: %+v", gotP)
			}
			if gotO.ID != o.ID || gotO.Status != c.wantOrder || gotO.IsPaid != c.approve ||
				(c.approve && !recent(gotO.PaidAt)) || (!c.approve && gotO.PaidAt != nil) {
				t.Errorf("buyurtma: %+v", gotO)
			}

			// Ikkinchi qaror (istalgan yo'nalishda) rad etiladi va hech narsani o'zgartirmaydi.
			for _, approve := range []bool{true, false} {
				if _, _, err := r.ReviewPayment(ctx, p.ID, approve, 888); !errors.Is(err, ErrConflict) {
					t.Errorf("ikkinchi qaror (approve=%v): %v, kutilgan ErrConflict", approve, err)
				}
			}
			saved, err := r.GetPayment(ctx, p.ID)
			if err != nil || saved.Status != c.wantPay || saved.ReviewedBy == nil || *saved.ReviewedBy != 777 {
				t.Errorf("saqlangan chek: %+v %v", saved, err)
			}
			if got, err := r.GetOrder(ctx, o.ID); err != nil || got.Status != c.wantOrder || got.IsPaid != c.approve {
				t.Errorf("saqlangan buyurtma: %+v %v", got, err)
			}
		})
	}
}

func TestReviewPaymentConflict(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewBillingRepository(db)
	u := mkUser(t, r, 1)

	// Avtomatik tasdiqlangan chek tekshiruvda emas.
	o1 := mkOrder(t, r, u, KindPlain, 7000, false, OrderPending)
	p1 := newPayment(u, o1, PaymentApproved)
	if err := r.SubmitPayment(ctx, p1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.ReviewPayment(ctx, p1.ID, false, 777); !errors.Is(err, ErrConflict) {
		t.Errorf("tasdiqlangan chek: %v, kutilgan ErrConflict", err)
	}

	if _, _, err := r.ReviewPayment(ctx, 9999, true, 777); !errors.Is(err, ErrConflict) {
		t.Errorf("mavjud bo'lmagan chek: %v, kutilgan ErrConflict", err)
	}

	// Chek tekshiruvda, lekin buyurtma boshqa holatga o'tgan: tranzaksiya
	// bekor qilinadi, chek tekshiruvda qoladi.
	o2 := mkOrder(t, r, u, KindPlain, 7000, false, OrderPending)
	p2 := newPayment(u, o2, PaymentReview)
	if err := r.SubmitPayment(ctx, p2); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.SetOrderStatus(ctx, o2.ID, OrderReview, OrderCancelled); err != nil || !ok {
		t.Fatalf("SetOrderStatus: ok=%v err=%v", ok, err)
	}
	if _, _, err := r.ReviewPayment(ctx, p2.ID, true, 777); !errors.Is(err, ErrConflict) {
		t.Errorf("bekor qilingan buyurtma: %v, kutilgan ErrConflict", err)
	}
	if got, err := r.GetPayment(ctx, p2.ID); err != nil || got.Status != PaymentReview || got.ReviewedBy != nil {
		t.Errorf("chek o'zgarmasligi kerak edi: %+v %v", got, err)
	}
	if got, err := r.GetOrder(ctx, o2.ID); err != nil || got.Status != OrderCancelled || got.IsPaid {
		t.Errorf("buyurtma o'zgarmasligi kerak edi: %+v %v", got, err)
	}
}

func TestReceiptSeen(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewBillingRepository(db)
	u := mkUser(t, r, 1)
	o := mkOrder(t, r, u, KindPlain, 7000, false, OrderPending)

	seen := newPayment(u, o, PaymentApproved)
	seen.TransactionID, seen.CheckFileUniqueID = "TX-1", "U-1"
	// Tranzaksiya raqami va file_unique_id bo'sh chek ham bor.
	blank := newPayment(u, o, PaymentRejected)
	for _, p := range []*Payment{seen, blank} {
		if err := db.Create(p).Error; err != nil {
			t.Fatal(err)
		}
	}

	for _, c := range []struct {
		tx, uniq string
		want     bool
	}{
		{"TX-1", "", true},
		{"TX-2", "", false},
		{"", "U-1", true},
		{"", "U-2", false},
		{"TX-1", "U-2", true},
		{"TX-2", "U-1", true},
		{"TX-2", "U-2", false},
		{"", "", false},
	} {
		if got, err := r.ReceiptSeen(ctx, c.tx, c.uniq); err != nil || got != c.want {
			t.Errorf("ReceiptSeen(%q, %q) = %v (%v), kutilgan %v", c.tx, c.uniq, got, err, c.want)
		}
	}
}

func TestListPaymentsByStatus(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewBillingRepository(db)

	base := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	late := insertPayment(t, db, PaymentReview, base.Add(2*time.Hour))
	first := insertPayment(t, db, PaymentReview, base)
	insertPayment(t, db, PaymentApproved, base.Add(-time.Hour))
	midA := insertPayment(t, db, PaymentReview, base.Add(time.Hour))
	midB := insertPayment(t, db, PaymentReview, base.Add(time.Hour)) // vaqt bir xil — id bo'yicha
	insertPayment(t, db, PaymentRejected, base)
	want := []uint{first, midA, midB, late}

	for _, c := range []struct {
		limit int
		want  []uint
	}{
		{0, want},
		{2, want[:2]},
		{10, want},
	} {
		list, total, err := r.ListPaymentsByStatus(ctx, PaymentReview, c.limit)
		if err != nil {
			t.Fatal(err)
		}
		if total != 4 || !slices.Equal(paymentIDs(list), c.want) {
			t.Errorf("limit %d: %v total=%d, kutilgan %v total=4", c.limit, paymentIDs(list), total, c.want)
		}
	}
	if list, total, err := r.ListPaymentsByStatus(ctx, PaymentApproved, 0); err != nil || total != 1 || len(list) != 1 {
		t.Errorf("approved: %d ta, total=%d (%v)", len(list), total, err)
	}
	if list, total, err := r.ListPaymentsByStatus(ctx, "unknown", 5); err != nil || total != 0 || len(list) != 0 {
		t.Errorf("noma'lum holat: %d ta, total=%d (%v)", len(list), total, err)
	}
}

func TestListPaymentsSince(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewBillingRepository(db)
	tashkent := time.FixedZone("UZT", 5*60*60)

	base := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	old := insertPayment(t, db, PaymentApproved, base.AddDate(0, 0, -2))
	before := insertPayment(t, db, PaymentReview, base.Add(-time.Microsecond))
	atA := insertPayment(t, db, PaymentRejected, base)
	atB := insertPayment(t, db, PaymentApproved, base) // vaqt bir xil — katta id birinchi
	after := insertPayment(t, db, PaymentReview, base.Add(time.Hour))

	for _, c := range []struct {
		name  string
		since *time.Time
		want  []uint
	}{
		{"barchasi", nil, []uint{after, atB, atA, before, old}},
		{"since UTC", ptr(base), []uint{after, atB, atA}},
		{"since Toshkent", ptr(base.In(tashkent)), []uint{after, atB, atA}},
		{"kelajak", ptr(base.Add(24 * time.Hour)), []uint{}},
	} {
		list, err := r.ListPayments(ctx, c.since)
		if err != nil {
			t.Fatal(err)
		}
		if got := paymentIDs(list); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, kutilgan %v", c.name, got, c.want)
		}
	}
}
