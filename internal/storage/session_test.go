package storage

import (
	"context"
	"slices"
	"sync"
	"testing"
)

// Bir foydalanuvchidan bir vaqtda kelgan ko'p so'rov bitta sessiya yaratishi kerak.
func TestSessionGetOrCreateConcurrent(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewSessionRepository(db)

	// Poyga har safar ham yuzaga kelmaydi — ehtimolni oshirish uchun bir necha
	// foydalanuvchida takrorlanadi.
	const (
		workers = testPoolSize
		rounds  = 5
	)
	for round := range rounds {
		userID := int64(100 + round)
		warmPool(t, db)

		var (
			wg    sync.WaitGroup
			start = make(chan struct{})
			errs  = make([]error, workers)
			got   = make([]*Session, workers)
		)
		for i := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				got[i], errs[i] = r.GetOrCreate(ctx, userID, userID*10, "ali")
			}()
		}
		close(start)
		wg.Wait()

		for i := range workers {
			if errs[i] != nil {
				t.Fatalf("foydalanuvchi %d, %d-goroutine: %v", userID, i, errs[i])
			}
			if s := got[i]; s.UserID != userID || s.ChatID != userID*10 || s.Username != "ali" || s.State != "idle" {
				t.Errorf("foydalanuvchi %d, %d-goroutine: kutilmagan sessiya %+v", userID, i, s)
			}
		}
		var n int64
		if err := db.Model(&Session{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("foydalanuvchi %d: sessiyalar soni %d, kutilgan 1", userID, n)
		}
	}

	// Mavjud sessiya qayta yaratilmaydi va ma'lumotlari o'zgarmaydi.
	s, err := r.GetOrCreate(ctx, 100, 9999, "boshqa")
	if err != nil || s.ChatID != 1000 || s.Username != "ali" {
		t.Errorf("mavjud sessiya: %+v %v", s, err)
	}
}

func TestSessionCompareAndSwapState(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewSessionRepository(db)

	before, err := r.GetOrCreate(ctx, 1, 1, "ali")
	if err != nil {
		t.Fatal(err)
	}

	// Muvaffaqiyatli: joriy holat from ga teng.
	ok, err := r.CompareAndSwapState(ctx, 1, "await_confirm", "generating")
	if err != nil || ok {
		t.Fatalf("idle holatida await_confirm → generating: ok=%v err=%v, kutilgan false", ok, err)
	}
	ok, err = r.CompareAndSwapState(ctx, 1, "idle", "await_confirm")
	if err != nil || !ok {
		t.Fatalf("idle → await_confirm: ok=%v err=%v", ok, err)
	}
	s, err := r.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if s.State != "await_confirm" {
		t.Errorf("holat %q, kutilgan await_confirm", s.State)
	}
	if s.UpdatedAt.Before(before.UpdatedAt) {
		t.Errorf("updated_at orqaga ketdi: %v < %v", s.UpdatedAt, before.UpdatedAt)
	}

	// "Tasdiqlash" ikki marta bosilganda faqat birinchisi holatni o'zgartiradi.
	if ok, err := r.CompareAndSwapState(ctx, 1, "await_confirm", "generating"); err != nil || !ok {
		t.Fatalf("birinchi bosish: ok=%v err=%v", ok, err)
	}
	if ok, err := r.CompareAndSwapState(ctx, 1, "await_confirm", "generating"); err != nil || ok {
		t.Errorf("ikkinchi bosish: ok=%v err=%v, kutilgan false", ok, err)
	}
	if s, _ := r.Get(ctx, 1); s.State != "generating" {
		t.Errorf("holat %q, kutilgan generating", s.State)
	}

	// Mavjud bo'lmagan foydalanuvchi: xatosiz false.
	if ok, err := r.CompareAndSwapState(ctx, 999, "idle", "generating"); err != nil || ok {
		t.Errorf("mavjud bo'lmagan sessiya: ok=%v err=%v", ok, err)
	}
}

func TestSessionListByState(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := NewSessionRepository(db)

	for _, id := range []int64{1, 2, 3, 4} {
		if _, err := r.GetOrCreate(ctx, id, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{2, 4} {
		if ok, err := r.CompareAndSwapState(ctx, id, "idle", "generating"); err != nil || !ok {
			t.Fatalf("%d: ok=%v err=%v", id, ok, err)
		}
	}

	ids := func(state string) []int64 {
		t.Helper()
		list, err := r.ListByState(ctx, state)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]int64, 0, len(list))
		for _, s := range list {
			if s.State != state {
				t.Errorf("%d-sessiya holati %q, kutilgan %q", s.UserID, s.State, state)
			}
			out = append(out, s.UserID)
		}
		slices.Sort(out)
		return out
	}
	if got := ids("generating"); !slices.Equal(got, []int64{2, 4}) {
		t.Errorf("generating: %v", got)
	}
	if got := ids("idle"); !slices.Equal(got, []int64{1, 3}) {
		t.Errorf("idle: %v", got)
	}
	if got := ids("payment_review"); len(got) != 0 {
		t.Errorf("payment_review: %v, kutilgan bo'sh", got)
	}
}
