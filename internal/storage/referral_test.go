package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Integratsion testlar: do'stlarni taklif qilish (referal) tizimi.

// mkChat foydalanuvchining bot bilan suhbatini (sessiyasini) yaratadi.
func mkChat(t *testing.T, telegramID int64, username string) {
	t.Helper()
	if _, err := NewSessionRepository(sharedDB).GetOrCreate(context.Background(), telegramID, telegramID, username); err != nil {
		t.Fatal(err)
	}
}

// credits foydalanuvchining bazadagi bonus balansi.
func credits(t *testing.T, r *BillingRepository, telegramID int64) int {
	t.Helper()
	u, err := r.EnsureUser(context.Background(), telegramID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return u.FreeCredits
}

// setCredits foydalanuvchining bonus balansini o'rnatadi.
func setCredits(t *testing.T, r *BillingRepository, u *User, n int) {
	t.Helper()
	if err := r.db.Model(&User{}).Where("id = ?", u.ID).Update("free_credits", n).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAttachReferral(t *testing.T) {
	ctx := context.Background()

	t.Run("yangi do'st bog'lanadi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		mkChat(t, 1, "ali")
		ref, err := r.AttachReferral(ctx, 1, 2, "vali", "Vali")
		if err != nil {
			t.Fatal(err)
		}
		if ref == nil || ref.TelegramUserID != 1 {
			t.Fatalf("taklif qilgan = %+v, kutilgan Telegram ID 1", ref)
		}
		// Taklif qilgan users da sessiyadagi username bilan yaratiladi.
		if ref.Username != "ali" {
			t.Errorf("username = %q, kutilgan ali", ref.Username)
		}
		referee, _ := r.EnsureUser(ctx, 2, "vali", "Vali")
		var row Referral
		if err := r.db.Where("referee_user_id = ?", referee.ID).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.ReferrerUserID != ref.ID || row.RewardedAt != nil {
			t.Errorf("taklif yozuvi: %+v", row)
		}
	})

	t.Run("mavjud taklif qilganning ismi o'zgarmaydi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		mkChat(t, 1, "eski")
		if _, err := r.EnsureUser(ctx, 1, "ali", "Ali"); err != nil {
			t.Fatal(err)
		}
		ref, err := r.AttachReferral(ctx, 1, 2, "vali", "Vali")
		if err != nil || ref == nil {
			t.Fatalf("ref=%+v err=%v", ref, err)
		}
		if ref.Username != "ali" || ref.FirstName != "Ali" {
			t.Errorf("taklif qilgan: %+v", ref)
		}
	})

	t.Run("o'zini o'zi taklif qilib bo'lmaydi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		mkChat(t, 1, "ali")
		if ref, err := r.AttachReferral(ctx, 1, 1, "ali", "Ali"); err != nil || ref != nil {
			t.Errorf("ref=%+v err=%v, kutilgan nil", ref, err)
		}
	})

	t.Run("botda yo'q taklif qilgan", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		if ref, err := r.AttachReferral(ctx, 999, 2, "vali", "Vali"); err != nil || ref != nil {
			t.Errorf("ref=%+v err=%v, kutilgan nil", ref, err)
		}
		var n int64
		r.db.Model(&User{}).Where("telegram_user_id = ?", 999).Count(&n)
		if n != 0 {
			t.Errorf("botda yo'q foydalanuvchi users ga qo'shildi")
		}
	})

	t.Run("birinchi taklif qoladi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		mkChat(t, 1, "ali")
		mkChat(t, 3, "sardor")
		if ref, err := r.AttachReferral(ctx, 1, 2, "vali", "Vali"); err != nil || ref == nil {
			t.Fatalf("birinchi: ref=%+v err=%v", ref, err)
		}
		if ref, err := r.AttachReferral(ctx, 3, 2, "vali", "Vali"); err != nil || ref != nil {
			t.Errorf("ikkinchi: ref=%+v err=%v, kutilgan nil", ref, err)
		}
		if ref, err := r.AttachReferral(ctx, 1, 2, "vali", "Vali"); err != nil || ref != nil {
			t.Errorf("takroriy: ref=%+v err=%v, kutilgan nil", ref, err)
		}
	})
}

func TestRewardReferral(t *testing.T) {
	ctx := context.Background()
	r := NewBillingRepository(testDB(t))
	mkChat(t, 1, "ali")
	if _, err := r.AttachReferral(ctx, 1, 2, "", ""); err != nil {
		t.Fatal(err)
	}
	referee := mkUser(t, r, 2)
	order := mkOrder(t, r, referee, KindPlain, 5000, false, OrderPaid)

	reward, err := r.RewardReferral(ctx, referee.ID, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reward == nil || reward.ReferrerTelegramID != 1 || reward.Credits != 1 {
		t.Fatalf("mukofot = %+v, kutilgan {1 1}", reward)
	}
	// Qayta chaqirish (ikkinchi to'lov yoki zaxira chaqiruv) mukofot bermaydi.
	if again, err := r.RewardReferral(ctx, referee.ID, order.ID+1); err != nil || again != nil {
		t.Errorf("takroriy mukofot: %+v %v", again, err)
	}
	if got := credits(t, r, 1); got != 1 {
		t.Errorf("balans = %d, kutilgan 1", got)
	}
	var row Referral
	if err := r.db.Where("referee_user_id = ?", referee.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.RewardedAt == nil || row.RewardOrderID == nil || *row.RewardOrderID != order.ID {
		t.Errorf("taklif yozuvi: %+v", row)
	}

	// Taklif qilinmagan foydalanuvchi to'lovi hech kimga mukofot bermaydi.
	stranger := mkUser(t, r, 5)
	if reward, err := r.RewardReferral(ctx, stranger.ID, order.ID); err != nil || reward != nil {
		t.Errorf("taklifsiz: %+v %v", reward, err)
	}

	invited, rewarded, err := r.ReferralStats(ctx, mkUser(t, r, 1).ID)
	if err != nil || invited != 1 || rewarded != 1 {
		t.Errorf("ReferralStats = %d, %d, %v; kutilgan 1, 1", invited, rewarded, err)
	}
}

func bonusOrder(u *User, price, discount int) *Order {
	o := &Order{UserID: u.ID, TelegramUserID: u.TelegramUserID, WorkType: "presentation",
		Kind: KindPlain, Price: price, Discount: discount, Status: OrderPending}
	if price == 0 {
		now := time.Now().UTC()
		o.IsPaid, o.Status, o.PaidAt = true, OrderPaid, &now
	}
	return o
}

func TestCreateBonusOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("bonus ayiriladi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		setCredits(t, r, u, 2)
		o := bonusOrder(u, 0, 3000)
		left, err := r.CreateBonusOrder(ctx, o)
		if err != nil || left != 1 || o.ID == 0 {
			t.Fatalf("left=%d id=%d err=%v", left, o.ID, err)
		}
		if got := credits(t, r, 1); got != 1 {
			t.Errorf("balans = %d, kutilgan 1", got)
		}
	})

	t.Run("bonus yo'q", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		if _, err := r.CreateBonusOrder(ctx, bonusOrder(u, 0, 3000)); !errors.Is(err, ErrNoCredits) {
			t.Errorf("err = %v, kutilgan ErrNoCredits", err)
		}
		var n int64
		r.db.Model(&Order{}).Count(&n)
		if n != 0 {
			t.Errorf("bonussiz buyurtma yaratildi")
		}
	})

	t.Run("chegirmasiz rad etiladi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		setCredits(t, r, u, 1)
		if _, err := r.CreateBonusOrder(ctx, bonusOrder(u, 5000, 0)); err == nil {
			t.Error("chegirmasiz bonusli buyurtma qabul qilindi")
		}
	})

	t.Run("parallel so'rovlarda bitta bonus bir marta ishlatiladi", func(t *testing.T) {
		db := testDB(t)
		r := NewBillingRepository(db)
		u := mkUser(t, r, 1)
		setCredits(t, r, u, 1)
		warmPool(t, db)

		var (
			wg    sync.WaitGroup
			start = make(chan struct{})
			errs  = make([]error, testPoolSize)
		)
		for i := range testPoolSize {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, errs[i] = r.CreateBonusOrder(ctx, bonusOrder(u, 0, 3000))
			}()
		}
		close(start)
		wg.Wait()

		ok := 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case !errors.Is(err, ErrNoCredits):
				t.Errorf("kutilmagan xato: %v", err)
			}
		}
		if ok != 1 {
			t.Errorf("muvaffaqiyatli = %d, kutilgan 1", ok)
		}
		if got := credits(t, r, 1); got != 0 {
			t.Errorf("balans = %d, kutilgan 0", got)
		}
	})
}

func TestReleaseOrder(t *testing.T) {
	ctx := context.Background()

	// release buyurtmani bekor qilishni sinaydi va natijani tekshiradi.
	release := func(t *testing.T, r *BillingRepository, o *Order, wantReleased bool, wantStatus string) {
		t.Helper()
		released, err := r.ReleaseOrder(ctx, o.ID)
		if err != nil {
			t.Fatal(err)
		}
		if released != wantReleased {
			t.Errorf("released = %v, kutilgan %v", released, wantReleased)
		}
		got, err := r.GetOrder(ctx, o.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != wantStatus {
			t.Errorf("holat = %s, kutilgan %s", got.Status, wantStatus)
		}
	}

	t.Run("qisman to'lov kutilayotgan bonusli buyurtma — bonus qaytadi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		setCredits(t, r, u, 1)
		o := bonusOrder(u, 10000, 15000)
		if _, err := r.CreateBonusOrder(ctx, o); err != nil {
			t.Fatal(err)
		}
		release(t, r, o, true, OrderCancelled)
		if got := credits(t, r, 1); got != 1 {
			t.Errorf("balans = %d, kutilgan 1", got)
		}
		// Ikkinchi marta bekor qilish bonusni yana qo'shmaydi.
		release(t, r, o, false, OrderCancelled)
		if got := credits(t, r, 1); got != 1 {
			t.Errorf("takroriy: balans = %d, kutilgan 1", got)
		}
	})

	t.Run("to'liq bepul, yetkazilmagan bonusli buyurtma — bonus qaytadi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		setCredits(t, r, u, 1)
		o := bonusOrder(u, 0, 3000)
		if _, err := r.CreateBonusOrder(ctx, o); err != nil {
			t.Fatal(err)
		}
		release(t, r, o, true, OrderCancelled)
		if got := credits(t, r, 1); got != 1 {
			t.Errorf("balans = %d, kutilgan 1", got)
		}
	})

	t.Run("oddiy pending buyurtma bekor qilinadi, bonus qo'shilmaydi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		o := mkOrder(t, r, u, KindPlain, 5000, false, OrderPending)
		release(t, r, o, true, OrderCancelled)
		if got := credits(t, r, 1); got != 0 {
			t.Errorf("balans = %d, kutilgan 0", got)
		}
	})

	t.Run("pul to'langan va eski bepul buyurtmalarga tegilmaydi", func(t *testing.T) {
		r := NewBillingRepository(testDB(t))
		u := mkUser(t, r, 1)
		release(t, r, mkOrder(t, r, u, KindPlain, 5000, false, OrderPaid), false, OrderPaid)
		release(t, r, mkOrder(t, r, u, KindPlain, 0, true, OrderPaid), false, OrderPaid)
		release(t, r, mkOrder(t, r, u, KindPlain, 5000, false, OrderReview), false, OrderReview)
		release(t, r, mkOrder(t, r, u, KindPlain, 5000, false, OrderDelivered), false, OrderDelivered)
		// Qisman bonus bilan, lekin to'lovi tasdiqlangan buyurtma — kredit sifatida qoladi.
		partial := bonusOrder(u, 10000, 15000)
		now := time.Now().UTC()
		partial.Status, partial.IsPaid, partial.PaidAt = OrderPaid, true, &now
		if err := r.CreateOrder(ctx, partial); err != nil {
			t.Fatal(err)
		}
		release(t, r, partial, false, OrderPaid)
	})
}

func TestFindCreditBonusValue(t *testing.T) {
	ctx := context.Background()
	r := NewBillingRepository(testDB(t))
	u := mkUser(t, r, 1)
	// 10 000 so'm to'langan + 15 000 so'm bonus: to'liq qiymati 25 000.
	o := bonusOrder(u, 10000, 15000)
	now := time.Now().UTC()
	o.Status, o.IsPaid, o.PaidAt = OrderPaid, true, &now
	if err := r.CreateOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	for price, want := range map[int]bool{10000: true, 25000: true, 25001: false} {
		got, err := r.FindCredit(ctx, u.ID, AIKind("premium"), price)
		if err != nil {
			t.Fatal(err)
		}
		if (got != nil) != want {
			t.Errorf("FindCredit(%d) = %v, kutilgan topilishi %v", price, got, want)
		}
	}
}
