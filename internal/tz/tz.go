// Package tz — bot ishlatadigan vaqt mintaqasi.
package tz

import "time"

// Tashkent — O'zbekiston vaqti (UTC+5, yozgi vaqt yo'q). Cheklardagi vaqt shu
// mintaqada yoziladi. FixedZone tizimda tzdata bo'lishini talab qilmaydi.
var Tashkent = time.FixedZone("UZT", 5*60*60)
