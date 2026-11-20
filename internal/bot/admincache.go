package bot

// Adminlar ro'yxatini xotirada keshlash: har bir admin buyrug'i, tugmasi va
// adminlarga yuboriladigan xabar uchun bazaga so'rov yuborilmaydi.

import (
	"context"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/storage"
)

// adminCacheTTL — keshlangan ro'yxatning amal qilish muddati. SQL orqali
// to'g'ridan-to'g'ri qo'shilgan adminlar shu vaqt ichida ko'rinadi.
const adminCacheTTL = 5 * time.Minute

// adminCache — BillingStore ustidagi dekorator: IsAdmin va ListAdmins
// javoblarini keshdan beradi, SetAdmin muvaffaqiyatli bo'lsa keshni eskirgan
// deb belgilaydi. Qolgan metodlar to'g'ridan-to'g'ri omborga uzatiladi.
type adminCache struct {
	BillingStore
	log *zap.Logger
	now func() time.Time

	mu      sync.RWMutex
	loaded  bool               // ro'yxat kamida bir marta yuklangan (eskirgan bo'lsa ham)
	expires time.Time          // shu vaqtdan keyin ro'yxat qayta yuklanadi
	list    []storage.User     // adminlar (ombordagi tartibda)
	set     map[int64]struct{} // TelegramUserID bo'yicha
}

func newAdminCache(store BillingStore, log *zap.Logger, now func() time.Time) *adminCache {
	return &adminCache{BillingStore: store, log: log, now: now}
}

// IsAdmin foydalanuvchi admin ekanini keshlangan ro'yxatdan aniqlaydi.
func (c *adminCache) IsAdmin(ctx context.Context, telegramID int64) (bool, error) {
	set, _, err := c.admins(ctx)
	if err != nil {
		return false, err
	}
	_, ok := set[telegramID]
	return ok, nil
}

// ListAdmins keshlangan ro'yxat nusxasini qaytaradi (chaqiruvchi uni
// o'zgartirsa ham kesh buzilmaydi).
func (c *adminCache) ListAdmins(ctx context.Context) ([]storage.User, error) {
	_, list, err := c.admins(ctx)
	if err != nil {
		return nil, err
	}
	return slices.Clone(list), nil
}

// SetAdmin foydalanuvchini admin qiladi; muvaffaqiyatli bo'lsa keyingi
// so'rovda ro'yxat qayta yuklanadi (yangi admin darhol ko'rinadi).
func (c *adminCache) SetAdmin(ctx context.Context, telegramID int64) (bool, error) {
	added, err := c.BillingStore.SetAdmin(ctx, telegramID)
	if err != nil {
		return added, err
	}
	c.mu.Lock()
	c.expires = time.Time{} // eskirgan ro'yxat yuklash xatosida zaxira sifatida qoladi
	c.mu.Unlock()
	return added, nil
}

// admins amaldagi ro'yxatni qaytaradi; muddati o'tgan bo'lsa ombordan qayta
// yuklaydi. Yuklashda xato bo'lsa va eskirgan ro'yxat bo'lsa, o'shani beradi.
// Qaytarilgan map va slice faqat o'qish uchun.
func (c *adminCache) admins(ctx context.Context) (map[int64]struct{}, []storage.User, error) {
	c.mu.RLock()
	if c.loaded && c.now().Before(c.expires) {
		set, list := c.set, c.list
		c.mu.RUnlock()
		return set, list, nil
	}
	c.mu.RUnlock()

	// Yuklash yozish qulfi ostida: bir vaqtda kelgan so'rovlar bitta yuklashni
	// kutadi, SetAdmin esa yuklash tugagach keshni eskirgan deb belgilaydi.
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded && c.now().Before(c.expires) {
		return c.set, c.list, nil
	}
	list, err := c.BillingStore.ListAdmins(ctx)
	if err != nil {
		if c.loaded {
			c.log.Warn("failed to reload admins, serving stale list", zap.Int("admins", len(c.list)), zap.Error(err))
			return c.set, c.list, nil
		}
		return nil, nil, err
	}
	set := make(map[int64]struct{}, len(list))
	for _, u := range list {
		set[u.TelegramUserID] = struct{}{}
	}
	c.list, c.set, c.loaded = list, set, true
	c.expires = c.now().Add(adminCacheTTL)
	return set, list, nil
}
