package bot

// Foydalanuvchiga yuboriladigan matnlar.
const (
	msgWelcome = "👋 Assalomu alaykum, <b>%s</b>!\n\n" +
		"Men siz uchun tez va sifatli tayyorlab beraman:\n" +
		"📊 <b>taqdimot</b> — PowerPoint (.pptx): tayyor dizaynlarda yoki mavzungizga mos ✨ maxsus dizaynda\n" +
		"📚 <b>mustaqil ish</b>, 📝 <b>referat</b> va 🛠 <b>amaliy topshiriq</b> — Word (.docx), chiroyli dizaynlarda\n\n" +
		"Bir nechta savolga javob bering — qolganini o'zim qilaman ✨"
	msgChooseWorkType = "📌 <b>Ish turini tanlang:</b>"
	msgPressButton    = "👇 Iltimos, quyidagi tugmalardan birini tanlang."
	msgIdle           = "Yangi ish buyurtma qilish uchun /start buyrug'ini bosing."
	msgBusy           = "⏳ Ishingiz tayyorlanmoqda, biroz kuting. To'xtatish uchun — /cancel"
	msgUnknownCommand = "🤔 Noma'lum buyruq. Mavjud buyruqlar: /start, /cancel, /help"
	msgTextOnly       = "✍️ Iltimos, javobni matn ko'rinishida yuboring."
	msgInternalError  = "😔 Kutilmagan xatolik yuz berdi. Iltimos, birozdan so'ng qayta urinib ko'ring yoki /start bosing."
	msgStaleButton    = "Bu tugma eskirgan. Yangi buyurtma uchun /start bosing."
	msgCancelled      = "❌ Bekor qilindi. Yangi ish uchun /start bosing."
	msgNothingCancel  = "Bekor qiladigan jarayon yo'q. Yangi ish uchun /start bosing."
	msgStopping       = "⏹ Generatsiya to'xtatilmoqda..."
	msgQueued         = "🕒 Hozir so'rovlar ko'p — siz navbatdasiz. Ishingiz tez orada boshlanadi."
	msgGeneratingDeck = "⏳ <b>Tayyorlanmoqda...</b> Odatda 1–2 daqiqa vaqt oladi."
	msgGeneratingDoc  = "⏳ <b>Tayyorlanmoqda...</b> Hujjat bo'limma-bo'lim yoziladi, odatda 2–5 daqiqa vaqt oladi."
	msgDone           = "✅ <b>Tayyor!</b> Omad tilayman 🎓\n\nYana ish kerak bo'lsa — /start"
	msgJobCancelled   = "⏹ Generatsiya bekor qilindi. Yangi ish uchun /start bosing."
	msgInterrupted    = "⚠️ Bot qayta ishga tushirildi va ishingiz yakunlanmay qoldi.\n" +
		"Ma'lumotlaringiz saqlangan — «Qayta urinish» tugmasini bosing."
	msgDesignHint = "👇 Iltimos, quyidagi tugmalardan biri bilan dizaynni tanlang."
	msgChooseLang = "🌐 <b>Ish qaysi tilda yozilsin?</b>\n\n<i>Matn, titul varaq va sarlavhalar shu tilda bo'ladi.</i>"
)

// To'lovga oid xabarlar.
const (
	msgReviewWait      = "⏳ Chekingiz tekshirilmoqda. Natija tez orada shu yerga keladi."
	msgReceiptChecking = "🔍 Chek tekshirilmoqda, biroz kuting..."
	msgReceiptToAdmin  = "🕵️ Chekingiz admin tekshiruviga yuborildi. Tasdiqlangach, ishingiz avtomatik tayyorlanadi."
	msgReceiptInvalid  = "⚠️ Yuborilgan rasm to'lov chekiga o'xshamaydi.\n\n" +
		"Iltimos, to'lov amalga oshirilgani ko'rinib turgan chek skrinshotini yuboring yoki /cancel bilan bekor qiling."
	msgReceiptRejected = "❌ Chekingiz admin tomonidan tasdiqlanmadi.\n\n" +
		"To'g'ri chekni yuboring yoki /cancel bilan bekor qiling."
	msgReceiptError       = "😔 Chekni tekshirishda xatolik yuz berdi. Iltimos, chekni qayta yuboring."
	msgReceiptPhotoOnly   = "🖼 Iltimos, chekni rasm (skrinshot) ko'rinishida yuboring."
	msgReceiptTooLarge    = "⚠️ Fayl juda katta. Chekning skrinshotini yuboring."
	msgReceiptInterrupted = "⚠️ Bot qayta ishga tushirildi va chekingiz tekshirilmay qoldi. Iltimos, chekni qayta yuboring."
	msgPaymentApproved    = "✅ <b>To'lov tasdiqlandi!</b> Rahmat."
	msgPaymentByAdmin     = "✅ <b>To'lovingiz admin tomonidan tasdiqlandi!</b> Rahmat."
	msgCreditSaved        = "✅ To'lovingiz tasdiqlandi. Keyingi buyurtmangizda qayta to'lov so'ralmaydi — /start"
	msgPaidRetry          = "✅ To'lovingiz tasdiqlangan. Ishni tayyorlash uchun «Qayta urinish» tugmasini bosing."
)

// Maxsus dizayn (tarif tanlash) xabarlari.
const (
	msgTariffHint  = "👇 Iltimos, quyidagi tugmalardan biri bilan tarifni tanlang."
	msgNoCustomCfg = "Maxsus dizayn hozircha mavjud emas. Tayyor dizaynlardan birini tanlang."
)

// Admin xabarlari.
const (
	msgAdminCommands = "<b>Admin buyruqlari:</b>\n" +
		"/report — to'lovlar hisoboti (1 kun, 1 hafta, 1 oy yoki hammasi; matn yoki CSV)\n" +
		"/chek ID — chek rasmini ko'rish (masalan: /chek 12)\n" +
		"/tekshiruv — tekshiruvdagi barcha cheklarni ✅/❌ tugmalari bilan qayta olish\n" +
		"/statistika — tashrif buyuruvchilar soni (bugun, 7 va 30 kun, kunlar bo'yicha)\n" +
		"/foydalanuvchilar — foydalanuvchilar ro'yxati (1 kun, 1 hafta, 1 oy yoki hammasi; matn yoki CSV)\n" +
		"/taklif_elon — barcha foydalanuvchilarga do'stlarni taklif qilish e'loni (har biriga shaxsiy havola bilan)\n" +
		"/backup — bazaning zaxira nusxasini hozir olish (har kuni avtomatik ham keladi)\n\n" +
		"Har bir chek va har bir xarid haqida to'liq ma'lumot shu chatga keladi; " +
		"shubhali cheklar — ✅/❌ tugmalari bilan."
	msgAdminWelcome = "👑 <b>Siz admin sifatida saqlandingiz.</b>\n\n" + msgAdminCommands
	msgAdminAlready = "👑 Siz allaqachon adminsiz.\n\n" + msgAdminCommands
	msgChekUsage    = "Foydalanish: /chek ID (masalan: /chek 12)"
	msgNoPending    = "✅ Tekshiruvda chek yo'q."
)

// Backup xabarlari.
const (
	msgBackupOff  = "💾 Backup o'chirilgan (BACKUP_ENABLED=false yoki serverda pg_dump topilmadi)."
	msgBackupBusy = "⏳ Backup allaqachon tayyorlanmoqda, biroz kuting."
	msgBackupWait = "⏳ Backup tayyorlanmoqda…"
)

const msgNoUserStats = "Foydalanuvchilar ro'yxati hozircha mavjud emas."
