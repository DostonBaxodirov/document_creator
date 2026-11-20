package images

import "telegram-presentation-bot/internal/httpx"

// permanent va isPermanent — mavjud testlar uchun qisqa nomlar (paket kodi
// httpx dagi funksiyalarni bevosita chaqiradi).
var (
	permanent   = httpx.Permanent
	isPermanent = httpx.IsPermanent
)
