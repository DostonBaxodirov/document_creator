package bot

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"telegram-presentation-bot/internal/storage"
)

// countingAdmins — adminlar ro'yxatini xotirada saqlaydigan va chaqiruvlarni
// sanaydigan soxta ombor.
type countingAdmins struct {
	BillingStore

	mu        sync.Mutex
	admins    []storage.User
	listErr   error // ListAdmins shu xatoni qaytaradi
	setErr    error // SetAdmin shu xatoni qaytaradi
	listCalls int
}

func (f *countingAdmins) ListAdmins(context.Context) ([]storage.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]storage.User(nil), f.admins...), nil
}

func (f *countingAdmins) SetAdmin(_ context.Context, id int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return false, f.setErr
	}
	for _, u := range f.admins {
		if u.TelegramUserID == id {
			return false, nil
		}
	}
	f.admins = append(f.admins, storage.User{TelegramUserID: id, IsAdmin: true})
	return true, nil
}

// addDirect adminni bot orqali emas, to'g'ridan-to'g'ri (SQL) qo'shadi.
func (f *countingAdmins) addDirect(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.admins = append(f.admins, storage.User{TelegramUserID: id, IsAdmin: true})
}

func (f *countingAdmins) set(fn func(f *countingAdmins)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *countingAdmins) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls
}

// fakeClock — qo'lda suriladigan soat.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
}

func mustIsAdmin(t *testing.T, c *adminCache, id int64) bool {
	t.Helper()
	ok, err := c.IsAdmin(context.Background(), id)
	if err != nil {
		t.Fatalf("IsAdmin(%d): %v", id, err)
	}
	return ok
}

// TestAdminCacheLoadsOnce — 100 ta (parallel) IsAdmin chaqiruvi bazaga faqat
// bitta ListAdmins so'rovini yuboradi.
func TestAdminCacheLoadsOnce(t *testing.T) {
	store := &countingAdmins{admins: []storage.User{{TelegramUserID: 11, IsAdmin: true}}}
	c := newAdminCache(store, zap.NewNop(), newClock().now)

	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := range 100 {
		wg.Go(func() {
			id := int64(11)
			if i%2 == 1 {
				id = 22
			}
			ok, err := c.IsAdmin(context.Background(), id)
			if err != nil {
				errs <- err
				return
			}
			if ok != (id == 11) {
				errs <- errors.New("noto'g'ri javob")
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := store.calls(); n != 1 {
		t.Errorf("ListAdmins %d marta chaqirildi, kutilgan 1", n)
	}

	// ListAdmins ham keshdan javob beradi va nusxa qaytaradi.
	list, err := c.ListAdmins(context.Background())
	if err != nil || len(list) != 1 || list[0].TelegramUserID != 11 {
		t.Fatalf("ListAdmins = %v, %v", list, err)
	}
	list[0].TelegramUserID = 99
	if !mustIsAdmin(t, c, 11) || store.calls() != 1 {
		t.Errorf("qaytarilgan ro'yxatni o'zgartirish keshga ta'sir qilmasligi kerak (chaqiruvlar: %d)", store.calls())
	}
}

// TestAdminCacheSetAdminInvalidates — SetAdmin dan keyin yangi admin darhol
// ko'rinadi; SetAdmin xato bersa kesh qayta yuklanmaydi.
func TestAdminCacheSetAdminInvalidates(t *testing.T) {
	store := &countingAdmins{admins: []storage.User{{TelegramUserID: 11, IsAdmin: true}}}
	c := newAdminCache(store, zap.NewNop(), newClock().now)

	if mustIsAdmin(t, c, 22) {
		t.Fatal("22 hali admin emas")
	}
	added, err := c.SetAdmin(context.Background(), 22)
	if err != nil || !added {
		t.Fatalf("SetAdmin = %v, %v", added, err)
	}
	if !mustIsAdmin(t, c, 22) {
		t.Error("SetAdmin dan keyin yangi admin darhol ko'rinishi kerak")
	}
	list, err := c.ListAdmins(context.Background())
	if err != nil || len(list) != 2 {
		t.Errorf("ListAdmins = %v, %v; kutilgan 2 ta admin", list, err)
	}
	if n := store.calls(); n != 2 {
		t.Errorf("ListAdmins %d marta chaqirildi, kutilgan 2", n)
	}

	store.set(func(f *countingAdmins) { f.setErr = errors.New("baza ishlamayapti") })
	if _, err := c.SetAdmin(context.Background(), 33); err == nil {
		t.Fatal("SetAdmin xatosi qaytarilishi kerak")
	}
	mustIsAdmin(t, c, 11)
	if n := store.calls(); n != 2 {
		t.Errorf("muvaffaqiyatsiz SetAdmin keshni yangilamasligi kerak (ListAdmins %d marta)", n)
	}
}

// TestAdminCacheTTL — SQL orqali qo'shilgan admin TTL tugagach ko'rinadi.
func TestAdminCacheTTL(t *testing.T) {
	store := &countingAdmins{admins: []storage.User{{TelegramUserID: 11, IsAdmin: true}}}
	clock := newClock()
	c := newAdminCache(store, zap.NewNop(), clock.now)

	mustIsAdmin(t, c, 11)
	store.addDirect(22)

	clock.advance(adminCacheTTL - time.Second)
	if mustIsAdmin(t, c, 22) || store.calls() != 1 {
		t.Fatalf("TTL tugamasdan ro'yxat qayta yuklanmasligi kerak (ListAdmins %d marta)", store.calls())
	}

	clock.advance(time.Second)
	if !mustIsAdmin(t, c, 22) {
		t.Error("TTL tugagach SQL orqali qo'shilgan admin ko'rinishi kerak")
	}
	if n := store.calls(); n != 2 {
		t.Errorf("ListAdmins %d marta chaqirildi, kutilgan 2", n)
	}
}

// TestAdminCacheStaleOnError — qayta yuklashda xato bo'lsa eskirgan ro'yxat
// beriladi va Warn yoziladi; hech qachon yuklanmagan bo'lsa xato qaytadi.
func TestAdminCacheStaleOnError(t *testing.T) {
	store := &countingAdmins{admins: []storage.User{{TelegramUserID: 11, IsAdmin: true}}}
	clock := newClock()
	core, logs := observer.New(zapcore.WarnLevel)
	c := newAdminCache(store, zap.New(core), clock.now)

	mustIsAdmin(t, c, 11)
	store.set(func(f *countingAdmins) { f.listErr = errors.New("baza ishlamayapti") })
	clock.advance(adminCacheTTL)

	if !mustIsAdmin(t, c, 11) {
		t.Error("yuklash xatosida eskirgan ro'yxat berilishi kerak")
	}
	list, err := c.ListAdmins(context.Background())
	if err != nil || len(list) != 1 || list[0].TelegramUserID != 11 {
		t.Errorf("ListAdmins = %v, %v; kutilgan eskirgan ro'yxat", list, err)
	}
	if n := store.calls(); n != 3 {
		t.Errorf("har bir so'rovda qayta yuklashga urinish kerak: ListAdmins %d marta, kutilgan 3", n)
	}
	if n := logs.FilterMessage("failed to reload admins, serving stale list").Len(); n != 2 {
		t.Errorf("Warn %d marta yozildi, kutilgan 2", n)
	}

	// Baza tiklangach yangi ro'yxat yuklanadi.
	store.set(func(f *countingAdmins) { f.listErr = nil })
	store.addDirect(22)
	if !mustIsAdmin(t, c, 22) {
		t.Error("baza tiklangach ro'yxat yangilanishi kerak")
	}

	// Ro'yxat hech qachon yuklanmagan bo'lsa, xato qaytariladi.
	broken := &countingAdmins{listErr: errors.New("baza ishlamayapti")}
	c = newAdminCache(broken, zap.NewNop(), clock.now)
	if _, err := c.IsAdmin(context.Background(), 11); err == nil {
		t.Error("eskirgan ro'yxat bo'lmasa IsAdmin xato qaytarishi kerak")
	}
	if _, err := c.ListAdmins(context.Background()); err == nil {
		t.Error("eskirgan ro'yxat bo'lmasa ListAdmins xato qaytarishi kerak")
	}
}

// TestNewHandlerWrapsBilling — Billing berilsa handler keshlangan ombordan
// foydalanadi, berilmasa nil qoladi.
func TestNewHandlerWrapsBilling(t *testing.T) {
	h := NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{Billing: &countingAdmins{}})
	if _, ok := h.billing.(*adminCache); !ok {
		t.Errorf("h.billing %T, kutilgan *adminCache", h.billing)
	}
	h = NewHandler(context.Background(), nil, nil, Builders{}, zap.NewNop(), Options{})
	if h.billing != nil {
		t.Errorf("Billing berilmasa h.billing nil bo'lishi kerak, olindi %T", h.billing)
	}
}
