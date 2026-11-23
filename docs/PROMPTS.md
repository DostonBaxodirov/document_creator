# Botdagi barcha AI promptlari — izohlar bilan

Bu faylda bot sun'iy intellektga yuboradigan **barcha** promptlar jamlangan: yangi
**✨ AI maxsus dizayn** promptlari, taqdimot, mustaqil ish / amaliy topshiriq va
to'lov chekini tekshirish promptlari. Har bir promptning to'liq matni (namunaviy mavzu
bilan to'ldirilgan holda) `docs/prompts/` papkasidagi alohida faylda — bu yerda unga
havola va nima uchun shunday yozilgani izohlangan.

> `docs/prompts/*.txt` fayllari koddan generatsiya qilinadi
> (`internal/ai/prompts_golden_test.go`, `internal/payment/prompt_golden_test.go`) —
> ularni qo'lda tahrirlamang. Kodda prompt o'zgarsa, `go test ./...` yiqiladi va birinchi
> farqni ko'rsatadi. Fayllarni yangilash:
>
> ```
> go test ./internal/ai ./internal/payment -run Golden -update
> ```

**Umumiy tamoyillar**

- Promptlar **ingliz tilida** — modellar ko'rsatmalarga inglizcha matnda aniqroq
  amal qiladi. Kontent tili (o'zbekcha lotin/kirill, ruscha, inglizcha) har bir
  topshiriqda alohida qator bilan beriladi.
- Javob doim **bitta JSON obyekt** — kod uni o'qiydi, tekshiradi va tozalaydi.
  OpenAI va Gemini'ga (Gemini — OpenAI-mos endpoint orqali) yagona JSON Schema
  (`internal/ai/jsonschema.go`) strict `json_schema` structured output sifatida
  beriladi (API uni qo'llamasa yoki OpenAI'da `AI_OPENAI_STRUCTURED=false` bo'lsa —
  JSON rejimi). DeepSeek'da JSON rejimi yoqiladi (buning uchun promptda "JSON" so'zi
  bo'lishi shart).
- Prompt — himoyaning **birinchi** qatlami. Ikkinchi qatlam — kod: noto'g'ri
  qiymatlar standart qiymatga almashtiriladi, ranglar kontrasti tuzatiladi, xizmat
  gaplari ("Albatta, mana...") o'chiriladi, maket uchun ma'lumot yetmasa boshqa
  maketga o'tiladi (7-bo'lim).
- Foydalanuvchining qo'shimcha talablari promptning **oxirida**, `"""` bloki ichida
  keladi va asosiy qoidalarni (JSON, slaydlar soni, til) bekor qila olmaydi. Ulardan
  keyin faqat slaydlar guruhi (3.2) va bo'lim (6.4) promptlarida guruhga/bo'limga xos
  ko'rsatma keladi.
- **Prompt keshi uchun tartib.** Bitta ishning parallel so'rovlari (slaydlar guruhlari,
  ish bo'limlari) belgima-belgi bir xil boshlanadi: mavzu, til, butun reja, qoidalar,
  JSON shakli, talablar — va faqat eng oxirida qaysi guruh/bo'lim yozilishi. Provayderlar
  bir xil prefiksni keshdan oladi (OpenAI 1024+ tokenli prefikslarni avtomatik keshlaydi,
  DeepSeek — disk keshi, Gemini — implicit caching): kirish tokenlari arzonroq va javob
  tezroq. Shu qismga o'zgaruvchan matn (guruh raqami, bo'lim nomi) qo'shmang.

## Mundarija

1. [AI maxsus dizayn: umumiy sxema va tariflar](#1-ai-maxsus-dizayn-umumiy-sxema-va-tariflar)
2. [Dizayn konsepsiyasi prompti (yangi)](#2-dizayn-konsepsiyasi-prompti-yangi)
3. [Maxsus dizayndagi slaydlar promptlari (yangi)](#3-maxsus-dizayndagi-slaydlar-promptlari-yangi)
4. [Rasm promptlari](#4-rasm-promptlari)
5. [Taqdimot: tizim prompti va tayyor dizaynlar](#5-taqdimot-tizim-prompti-va-tayyor-dizaynlar)
6. [Mustaqil ish va amaliy topshiriq](#6-mustaqil-ish-va-amaliy-topshiriq)
7. [To'lov chekini tekshirish](#7-tolov-chekini-tekshirish)
8. [Kod tomonidagi himoya va promptni o'zgartirish](#8-kod-tomonidagi-himoya-va-promptni-ozgartirish)

---

## 1. AI maxsus dizayn: umumiy sxema va tariflar

Foydalanuvchi dizayn tanlash qadamida **«✨ AI maxsus dizayn — mavzuga mos»** tugmasini
bosadi va tarifni tanlaydi. Generatsiya shu tartibda boradi:

| # | Bosqich | Kim bajaradi | Natija |
|---|---------|--------------|--------|
| 1 | Dizayn konsepsiyasi | tarif matn modeli, **2-bo'lim** prompti | palitra, shriftlar, titul/sarlavha/kartochka uslubi, motiv, rasm uslubi, imzo maketlari (JSON) |
| 2 | Dizaynni tekshirish | kod (`generator/custom.go`) | noma'lum qiymatlar → standart; ranglar kontrasti WCAG bo'yicha tuzatiladi |
| 3 | Reja (6 tadan ko'p slaydda; `AI_SLIDES_PER_REQUEST`) | tarif matn modeli, **3.1** prompti | har bir slayd: sarlavha, maket, mazmun yo'nalishi |
| 4 | Slaydlar matni (6 tadan guruhlab, parallel; `AI_SLIDES_PER_REQUEST`) | tarif matn modeli, **3.2** prompti | to'liq slaydlar + har biriga rasm tavsifi |
| 5 | Rasmlar | tarif rasm modeli, **4-bo'lim** | titul + slaydlarning bir qismiga kvadrat AI rasmlar |
| 6 | PowerPoint fayl | kod | tayyor .pptx |

Kichik taqdimotda (mazmunli slaydlar ≤ 6) 3–4-bosqichlar bitta so'rovda bajariladi (**3.3**).
Guruh hajmi (6) `.env` dagi `AI_SLIDES_PER_REQUEST` bilan o'zgartiriladi (3..10).

**Tariflar** (`.env` dagi `CUSTOM_*` sozlamalari, standart qiymatlar):

| Tarif | Narx (1 slayd) | Matn modeli | Rasm | Rasmlar soni | 20 slayd narxi | 20 slayd xarajati (taxm.) |
|-------|----------------|-------------|------|--------------|----------------|---------------------------|
| 🥉 Standart | 300 so'm | gpt-4o-mini | gpt-image-1, low | slaydlarning 40% (20 slaydda 8 ta) | 6 000 so'm | ≈ $0.09 |
| 🥈 Optimal | 500 so'm | gpt-4.1-mini | gpt-image-1, medium | 40% (8 ta) | 10 000 so'm | ≈ $0.35–0.39 |
| 🥇 Premium | 1 700 so'm | gpt-4o | gpt-image-1, high | 50% (10 ta) | 34 000 so'm | ≈ $1.72 |

- Rasmlar **kvadrat (1024×1024)** chiziladi: OpenAI narxlari (skrinshotdagi hisob ham)
  aynan shu o'lcham uchun. Yotiq/tik rasm (1536×1024) ~1.5 barobar qimmat — Premium'da
  bu marjani deyarli yo'q qilardi. Kvadrat rasm slaydga "cover" usulida qirqib joylanadi.
- Rasm soniga titul rasmi ham kiradi (u yakuniy slaydda ham ishlatiladi). Qolgan rasmlar
  slaydlarga teng oraliqda taqsimlanadi — avval rasm yon panelga sig'adigan slaydlar.
- Rasm modeli ishlamasa boshqa sifatga **o'tilmaydi** (xarajat oldindan ma'lum bo'lishi uchun);
  rasm o'rnida dizaynning bezakli paneli chiziladi.
- Bepul urinishlar hozircha butunlay o'chirilgan — AI maxsus dizayn ham, tayyor dizaynlar ham pullik.

---

## 2. Dizayn konsepsiyasi prompti (yangi)

Fayl: `internal/ai/design.go` (`designSystemPrompt`, `buildDesignPrompt`).
Bitta qisqa so'rov (2–4 soniya, ~1 500 token).

### 2.1. Tizim prompti

Matn: [`prompts/design_system.txt`](prompts/design_system.txt)

**Izoh:**

- *"award-winning presentation art director"* — modelni "matn yozuvchi" emas, "dizayner"
  rolida ishlatadi: tanlovlar estetik asoslangan bo'ladi.
- **Mavzu → assotsiatsiya misollari** (tibbiyot — toza firuza/oq, tarix — pergament, qizil,
  oltin, IT — to'q ko'k va yorqin aksentlar...). Misollarsiz modellar deyarli har doim
  "korporativ ko'k" tanlaydi; misollar mavzuga xos palitrani beradi. *"Avoid generic
  corporate blue"* — shu odatni aniq taqiqlaydi.
- **60-30-10 qoidasi** — dizaynerlarning palitra muvozanati qoidasi: fon 60%, asosiy rang
  30%, aksent 10%. Natijada ranglar "olachalpoq" chiqmaydi.
- **Kontrast talablari** (7:1, 4.5:1) — WCAG standarti. Model baribir xato qilishi mumkin,
  shuning uchun kod ham tekshiradi (8-bo'lim), lekin prompt to'g'ri yo'nalish beradi.
- **"Prefer a light background"** — akademik taqdimot ko'pincha proyektorda ko'rsatiladi,
  och fon u yerda yaxshi o'qiladi. To'q fon faqat texnologiya, kosmos kabi mavzularda.
- *"one coherent concept; do not pick options at random"* — variantlar bir-biriga mos
  bo'lishi uchun (masalan, klassik serif shrift + to'g'ri burchak + "underline" sarlavha).

### 2.2. Topshiriq prompti (namuna)

Namuna: mavzu — «Amir Temur davlatining tashkil topishi va boshqaruv tizimi», til —
o'zbekcha (lotin), qo'shimcha talab bilan.

Matn: [`prompts/design_user.txt`](prompts/design_user.txt)

**Izoh:**

- **Har bir variantning ma'nosi yozilgan** ("split — elegant, editorial", "dots — technical,
  precise"...). Faqat ro'yxat bersak, model tasodifiy tanlaydi; ma'nosi bilan — mavzuga
  mos tanlaydi. Bu ro'yxatlar generator chiza oladigan variantlar bilan aynan bir xil
  (`generator.Custom*`) — buni test tekshiradi.
- **Shriftlar faqat ro'yxatdan** — ular Windows/macOS PowerPoint'da bor va kengligi
  o'lchangan (matnni maydonga sig'dirish aniq ishlaydi). O'zbek kirill harflari
  (ў, қ, ғ, ҳ) uchun ham xavfsiz.
- **"name" — taqdimot tilida**: fayl izohida foydalanuvchiga ko'rsatiladi
  ("✨ Kvant Nurida (AI dizayn, 🥈 Optimal)").
- **"image_style" (art direction)** — barcha rasm promptlariga qo'shiladi (4-bo'lim),
  shuning uchun taqdimotdagi rasmlar bir seriyadek, palitraga mos chiqadi.
- **"signature_layouts"** — mavzuning tabiiy tuzilishiga mos 2–3 maket (tarix → vaqt
  chizig'i, ierarxiya → piramida). Ular keyingi slaydlar promptiga uzatiladi.
- **Talablar** — faqat rang/uslub istaklari olinadi ("ko'k ranglarda bo'lsin"), qolganlari
  (masalan, "ko'proq misollar") slaydlar promptida hisobga olinadi.

### 2.3. Javob namunasi (gpt-4o-mini, haqiqiy javob)

~~~json
{
  "name": "Amir Temur Meroslari",
  "mood": "elegant, historical, refined, structured, calm",
  "colors": {
    "background": "#F7F2E7", "surface": "#FFFFFF", "primary": "#B88A3D",
    "secondary": "#7A6D3A", "accent": "#E2C58D", "text": "#3E3E3E", "muted": "#7A7A7A"
  },
  "heading_font": "Georgia", "body_font": "Calibri",
  "cover": "split", "header": "band", "cards": "shadow", "corners": "rounded",
  "markers": "circle", "motif": "waves", "photo_side": "left",
  "image_style": "Realistic photography with warm tones, soft natural lighting, depicting historical architecture and landscapes, emphasizing depth and perspective.",
  "signature_layouts": ["timeline", "pyramid", "statement"]
}
~~~

Bu javobda `primary` (#B88A3D, och oltin) ustidagi oq matn yetarlicha o'qilmaydi —
kod panel va tasma rangini kontrast 4.5:1 ga yetguncha avtomatik to'qlashtiradi. Javob to'liq
bo'lmasa (7 ta rangdan biri noto'g'ri yoki asosiy variantlar yo'q) — so'rov qayta yuboriladi.

---

## 3. Maxsus dizayndagi slaydlar promptlari (yangi)

Fayllar: `internal/ai/presentation.go` (umumiy taqdimot promptlari) va
`internal/ai/design.go` (`customDeckRules`, `customImageRule`, `customPlanChecks`,
`customContentChecks`). Tizim prompti — 5.1 dagi taqdimot tizim prompti.

Maxsus dizayn tayyor dizaynlardan farqli ravishda **barcha 14 maketni** chiza oladi,
shuning uchun promptda hammasi beriladi va AI dan vizual, xilma-xil tuzilish talab qilinadi.

### 3.1. Reja prompti (namuna: 12 slaydli taqdimot → 10 ta mazmunli slayd)

Matn: [`prompts/presentation_outline.txt`](prompts/presentation_outline.txt)

**Izoh (maxsus dizaynga xos qismlar):**

- **"This deck uses a custom visual design ..."** bloki — dizayn nomi va kayfiyati
  beriladi, model matn ohangini ham shunga moslaydi.
- **"Turn content into visual structures"** — har bir mazmun turiga mos maket
  (bosqichlar → process, sanalar → timeline...). Bu oddiy "bullets" ro'yxatlar o'rniga
  sxema, vaqt chizig'i, sikl kabi vizual slaydlarni beradi — dizayn shu yerda ochiladi.
- **"bullets" chegarasi** (har 5 slaydga ko'pi bilan 1 ta) va **ketma-ket bir xil maket
  yo'q** — taqdimot bir xil ko'rinishdagi slaydlardan iborat bo'lib qolmasligi uchun.
- **Bitta "statement" slaydi, xulosadan oldin** — asosiy tezis katta harflarda; taqdimotga
  "kulminatsiya" beradi. ("before the conclusion" — sinovda model uni xulosadan keyin
  qo'ygan edi; endi kod ham xulosani doim oxirga ko'chiradi.)
- **Imzo maketlari** — dizayn konsepsiyasidan (2-bo'lim) keladi.
- **`"n"` raqamlash** — barcha dizaynlar uchun qo'shildi. Sinovda gpt-4.1-mini raqamsiz
  rejada 3 urinishdan 2 tasida 10 o'rniga 9 ta slayd qaytardi (foydalanuvchi esa har bir
  slayd uchun to'laydi). Raqamlash bilan 4/4 urinishda aniq 10 ta.
- **"Before answering, check that: ..."** — yakuniy tekshiruv ro'yxati promptning oxirida.
  Kichik modellar (gpt-4o-mini) uzun qoidalar ro'yxatining o'rtasidagi talablarni
  "unutadi"; oxiridagi raqamlangan ro'yxat ularni yana eslatadi. Sinovda shundan keyin
  imzo maketlari va "statement" slaydi ishlatila boshladi.
- **Rasm tavsiflari rejada** — rasmli dizaynda har bir slayd rasmining tavsifi rejada
  so'raladi: rasmlar slaydlar matni yozilishini kutmasdan, parallel chiziladi.
- **Rasm uslubi qoidasi** ("Image descriptions follow one art direction ...") — barcha
  rasm tavsiflari dizayn rasm uslubida va turli rakursda (umumiy, o'rta, yaqin plan)
  yoziladi — rasmlar bir seriyadek, lekin bir xil emas.

### 3.2. Slaydlar guruhi prompti (namuna: 6–10-slaydlar)

Matn: [`prompts/presentation_slides.txt`](prompts/presentation_slides.txt)

**Izoh:**

- **Butun reja ko'rsatiladi**, lekin faqat o'z guruhi yoziladi — slaydlar bir-birini
  takrorlamaydi. Model ba'zan guruhni oldingi guruhning oxirgi slaydidan boshlaydi;
  kod bunday "begona" slaydni olib tashlaydi va kerak bo'lsa qayta so'raydi.
- **Tartib**: mavzu, til, butun reja, maketlar, rasm qoidalari (reja rasm tavsiflarini
  bermagan bo'lsa), JSON shakli, tekshiruv ro'yxati, talablar — va eng oxirida guruh
  ko'rsatmasi ("Write the full content of slides X-Y only ..."). Bir taqdimotning barcha
  guruh promptlari shu ko'rsatmagacha bir xil — provayder prompt keshi umumiy qismni
  qayta ishlatadi.
- **Rasm tavsiflari** odatda rejada (3.1) yoziladi, shuning uchun namunadagi guruh
  promptida ular so'ralmaydi. Reja ularni bermagan bo'lsa, guruh prompti rasm
  qoidalarini (maxsus dizaynda — art direction qoidasi bilan) qo'shadi va har bir
  slayddan "image" maydonini so'raydi.
- **Mazmun tekshiruvi**: har bir maketda kerakli elementlar soni (sinovda gpt-4o-mini
  "process" slaydiga 1 ta element yozgan edi), so'zlar sonining quyi chegarasi ("bir
  qatorli" matnlar yo'q), **to'qima raqamlar taqiqi** (sinovda "75% yengish darajasi"
  kabi uydirma statistika chiqqan edi). Maket rejada belgilangani uchun ishonchsiz raqam
  o'rniga `value` bo'sh qoldiriladi — kod bunday slaydni kartochkalarga aylantiradi.

### 3.3. Kichik taqdimot — bitta so'rov (namuna: 5 ta mazmunli slayd)

Mazmunli slaydlar 6 tadan (`AI_SLIDES_PER_REQUEST`) ko'p bo'lmasa, reja va matn bitta so'rovda yoziladi. Prompt
3.1 va 3.2 dagi qoidalarning birlashmasi; tekshiruv ro'yxati ham bitta.

Matn: [`prompts/presentation_deck_custom.txt`](prompts/presentation_deck_custom.txt)

---

## 4. Rasm promptlari

Rasm modeliga yuboriladigan yakuniy prompt uch qismdan yig'iladi
(`generator.Design.PhotoPrompt`):

```
<AI yozgan rasm tavsifi>. Style: <dizayn rasm uslubi>. <umumiy talablar>
```

**Umumiy talablar** (`photoSuffix`, `internal/generator/picture.go`) — har bir rasmga:

~~~text
High-quality realistic photograph, natural composition, absolutely no text, letters, numbers, captions, signs, watermarks or logos.
~~~

**Namuna** (maxsus dizayn, titul rasmi):

~~~text
The Registan ensemble in Samarkand at sunrise, wide establishing shot with turquoise domes and warm light. Style: warm historical travel photography, golden sunlight, earthy terracotta and sand tones. High-quality realistic photograph, natural composition, absolutely no text, letters, numbers, captions, signs, watermarks or logos.
~~~

**Rasm tavsiflari qanday yoziladi** — slaydlar promptidagi qoidalar
(`internal/ai/presentation.go`: `imageRule`, `coverImageRule`). Matni —
[`prompts/presentation_deck.txt`](prompts/presentation_deck.txt) dagi
"Every slide also has "image" ..." va "The top-level "image" ..." qatorlari.

Maxsus dizaynda qo'shimcha (3.1 dagi) art direction qoidasi ham qo'shiladi.

**Izoh:**

- Tavsif **inglizcha** — rasm modellari uni eng aniq tushunadi.
- **Matn, harf, logotip taqiqi** ikki marta (tavsif qoidasida va umumiy talabda): rasm
  modellari matnni yaxshi chiza olmaydi va slaydda buzuq yozuvlar xunuk ko'rinadi.
- **Haqiqiy shaxslar nomi taqiqi** — xavfsizlik filtrlari rasmni rad etmasligi va noto'g'ri
  portretlar chiqmasligi uchun.
- **Rasm uslubi**: maxsus dizaynda — AI yaratgan `image_style`; tayyor rasmli dizaynlarda —
  qotirilgan uslub:

| Dizayn | Rasm uslubi |
|--------|-------------|
| 📰 Jurnal | editorial magazine photography, crisp details, natural neutral colors |
| 🍃 Tabiat | soft natural daylight, warm earthy tones, calm atmosphere |
| 🌊 Okean | clean professional photography, cool blue and teal tones, bright modern setting |
| 🌅 Shom | cinematic photography, dramatic warm golden-hour light, deep shadows |
| 🏞 Panorama | wide cinematic panoramic photography, natural light, rich but realistic colors |
| ✨ AI maxsus dizayn | AI yaratgan `image_style` (2-bo'lim) |

---

## 5. Taqdimot: tizim prompti va tayyor dizaynlar

### 5.1. Tizim prompti (barcha taqdimot so'rovlarida)

Fayl: `internal/ai/presentation.go` (`presentationSystemPrompt`). Oxiridagi til va
"tayyor ish" qoidalari `internal/ai/client.go` dagi `languageRules` va `finalTextRules`
bloklari — ular mustaqil ish promptida ham bor.

Matn: [`prompts/presentation_system.txt`](prompts/presentation_system.txt)

**Izoh:**

- **"Never invent statistics, quotations, URLs ..."** — akademik ishda eng xavfli xato
  uydirma raqam va manbalar; shuning uchun aniq taqiq.
- **Til qoidasi** o'zbek lotin (o', g') va kirill (ў, қ, ғ, ҳ) harflarini to'g'ri yozishni
  eslatadi — modellar ko'pincha o' o'rniga o yoki rus harflarini ishlatadi.
- **"Write as the author ..." bloki** — matn tayyor ish ko'rinishida bo'lishi uchun: AI
  haqida gap, "tekshirib chiqing" kabi maslahatlar, [Ism] kabi bo'sh joylar va "Albatta,
  mana..." kabi suhbat iboralari taqiqlanadi. Kod ham bunday gaplarni qidirib o'chiradi
  (`internal/ai/sanitize.go`).

### 5.2. Tayyor dizayndagi taqdimot (namuna: 📷 Okean, 6 ta mazmunli slayd)

Tayyor dizaynda AI faqat shu dizayn chiza oladigan maketlardan tanlaydi, dizaynning imzo
maketlari ishlatiladi; ixcham (katta shriftli) dizaynlarda so'zlar soni 25% kamaytiriladi.
Maxsus dizayndagi qo'shimcha qoidalar va tekshiruv ro'yxati bu yerda yo'q (reja promptida
`"n"` raqamlash esa barcha dizaynlarda bor).

Matn: [`prompts/presentation_deck.txt`](prompts/presentation_deck.txt)

---

## 6. Mustaqil ish va amaliy topshiriq

Fayl: `internal/ai/document.go`. Hujjat bosqichma-bosqich yoziladi: avval reja (boblar va
adabiyotlar), so'ng kirish, har bir bob va xulosa **parallel** alohida so'rovlarda — shunda
uzun hujjat modelning javob chegarasiga sig'adi va har bir bo'lim kerakli hajmda chiqadi.

### 6.1. Tizim prompti

Matn: [`prompts/document_system.txt`](prompts/document_system.txt)

### 6.2. Reja prompti (mustaqil ish)

Matn: [`prompts/document_outline.txt`](prompts/document_outline.txt)

**Izoh:** kirish va xulosa alohida yoziladi, shuning uchun rejada ular yo'q. Adabiyotlar
uchun eng qat'iy qoida — faqat haqiqatda mavjud manbalar (O'zbekiston qonunlari, lex.uz,
taniqli darsliklar); ishonch bo'lmasa — kamroq manba.

### 6.3. Reja prompti (amaliy topshiriq)

Amaliy topshiriqda boblar mantiqi boshqacha: nazariya → usullar → bosqichma-bosqich bajarish
→ natijalar tahlili.

Matn: [`prompts/document_outline_practical.txt`](prompts/document_outline_practical.txt)

**Referat, Word maxsus dizayni bilan** — [`prompts/document_outline_figures.txt`](prompts/document_outline_figures.txt).
Referat boblari mantig'i: nazariy asoslar va masala tarixi → asosiy ilmiy yondashuvlar →
hozirgi holat va amaliyot → muammolar va istiqbollar. Maxsus dizaynda (6.6) har bir
bobga rasm g'oyasi ham so'raladi (`figure`: ish tilidagi qisqa izoh va rasm modeli uchun
inglizcha tavsif) — rasmlar reja tayyor bo'lishi bilan, matn bilan parallel chiziladi.

### 6.4. Bo'lim promptlari

Har bir bo'lim so'rovida butun reja ko'rsatiladi (bo'limlar bir-birini takrorlamasligi
uchun) va so'zlar soni aniq beriladi (sahifalar sonidan hisoblanadi).

Tartib: ish turi, mavzu, til, butun reja, JSON shakli, qoidalar, uslub, talablar — va eng
oxirida bo'limga xos qism ("Write now: ...", "It must cover ...", "Length: ..."). Bir ishning
barcha bo'lim promptlari (kirish, boblar, xulosa) shu qismgacha bir xil — provayder prompt
keshi umumiy qismni qayta ishlatadi. Shuning uchun faqat boblarga tegishli "Do not write an
introduction or a conclusion ..." qoidasi ham umumiy ro'yxatda emas, bo'limga xos qismda.

Namunalar (mustaqil ish «Raqamli iqtisodiyot», 3 bobli reja, qo'shimcha talab bilan):

- **Kirish** — [`prompts/document_introduction.txt`](prompts/document_introduction.txt)
- **Bob** — [`prompts/document_chapter.txt`](prompts/document_chapter.txt)
- **Xulosa** — [`prompts/document_conclusion.txt`](prompts/document_conclusion.txt)
- **Bob (amaliy topshiriq — uslub boshqacha, dastur kodi ham bo'lishi mumkin)** —
  [`prompts/document_chapter_practical.txt`](prompts/document_chapter_practical.txt)

### 6.5. Bo'limni davom ettirish prompti

Model ko'pincha so'ralganidan kamroq yozadi. Bo'lim so'ralgan hajmning 55% idan
qisqa bo'lsa, u qayta yoziladi; 90% idan qisqa bo'lsa (va kamida 60 so'z
yetishmasa), barcha bo'limlar yozilgandan keyin shu bo'lim uchun alohida
"davom ettirish" so'rovi yuboriladi. Model bo'limning hozirgi matnini ko'radi
va faqat yangi paragraflarni qaytaradi (ular bo'lim oxiriga qo'shiladi). Bu
so'rov xato bersa, bo'lim avvalgi holicha qoladi va hujjat baribir tayyorlanadi.
Model rejada kamroq bob qaytarsa, boblar uchun ajratilgan umumiy hajm qolgan
boblarga bo'linadi.

Matn: [`prompts/document_extend.txt`](prompts/document_extend.txt)

### 6.6. Word maxsus dizayni prompti

Fayl: `internal/ai/docdesign.go` (`docDesignSystemPrompt`, `docDesignOptionGuide`,
`buildDocDesignPrompt`). Mustaqil ish / referat uchun maxsus dizayn tanlansa, bitta qisqa
so'rov sarlavhalar rangi va shrifti, titul varaq kompozitsiyasi, sarlavha bezagi, sahifa
ramkasi va rasmlar uslubini tanlaydi.

- Tizim prompti: [`prompts/doc_design_system.txt`](prompts/doc_design_system.txt)
- Topshiriq prompti (namuna): [`prompts/doc_design_user.txt`](prompts/doc_design_user.txt)

**Izoh:** asosiy matn OTM talabiga mos qoladi (qora Times New Roman 14) — AI faqat uning
atrofidagi bezakni tanlaydi. Ikkala rang oq qog'ozda yaxshi o'qilishi talab qilinadi.
Javob sxemasidagi variantlar generator chizadigan `design.Doc*` ro'yxatlaridan olinadi;
noma'lum qiymatlarni generator standart qiymat bilan almashtiradi.

---

## 7. To'lov chekini tekshirish

Fayl: `internal/payment/verifier.go`. OpenAI vision modeli (`PAYMENT_VISION_MODEL`) chek
rasmini o'qiydi. **Qarorni AI emas, kod chiqaradi** (`internal/payment/decide.go`): holat
muvaffaqiyatli, summa kam emas, karta mos, sana buyurtmadan keyin va chek oldin
ishlatilmagan bo'lsa — tasdiqlanadi; aks holda chek ✅/❌ tugmalari bilan adminga boradi.

- Tizim prompti: [`prompts/receipt_system.txt`](prompts/receipt_system.txt)
- Foydalanuvchi xabari (rasm bilan birga; namuna — 34 000 so'm, karta oxiri 1234):
  [`prompts/receipt_user.txt`](prompts/receipt_user.txt)

**Izoh:** *"Never guess and never copy the expected values"* — kutilgan summa modelga
faqat solishtirish uchun beriladi; aks holda model rasmda ko'rinmagan summani "ko'rgan"
deb yozib yuborishi mumkin. `notes` — admin uchun o'zbekcha izoh (tahrir izlari va h.k.).

---

## 8. Kod tomonidagi himoya va promptni o'zgartirish

Promptga qaramay model xato qilishi mumkin, shuning uchun kod har bir javobni tekshiradi:

| Himoya | Fayl | Nima qiladi |
|--------|------|-------------|
| Dizayn qiymatlari | `generator/custom.go` | noma'lum variant/shrift → standart qiymat |
| Ranglar kontrasti | `generator/custom.go` | matn ≥ 7:1, ikkinchi matn ≥ 4.5:1, oq yozuvli panel ≥ 4.5:1, fon juda och yoki juda to'q bo'ladi |
| Maket tekshiruvi | `generator/layouts.go` | ma'lumot yetmasa (masalan, "facts" da raqam yo'q) — mos maketga almashtiriladi |
| Xizmat gaplari | `ai/sanitize.go` | "Albatta, mana...", AI haqidagi gaplar, maslahatlar o'chiriladi |
| Takroriy slaydlar | `ai/presentation.go` | boshqa guruhga tegishli slayd olib tashlanadi |
| Xulosa o'rni | `ai/presentation.go` | xulosa slaydi doim oxirida |
| To'liqlik | `ai/client.go` | slaydlar/ranglar soni kam bo'lsa — qayta so'rov (eng to'liq natija saqlanadi) |
| Matn uzunligi | `ai/presentation.go` | slaydga sig'maydigan uzun matnlar qirqiladi |

**Promptni o'zgartirmoqchi bo'lsangiz:**

- Maxsus dizayn konsepsiyasi — `internal/ai/design.go` (`designSystemPrompt`,
  `designOptionGuide`, `buildDesignPrompt`).
- Maxsus dizayndagi slaydlar qoidalari — `internal/ai/design.go` (`customDeckRules`,
  `customImageRule`, `customPlanChecks`, `customContentChecks`).
- Maketlar talablari (so'zlar soni, elementlar soni) — `internal/ai/presentation.go`
  (`layoutGuides`).
- Yangi dizayn varianti qo'shilsa — uni `internal/ai/design.go` dagi ro'yxatga **va**
  `internal/generator/custom.go` ga (chizish kodi bilan) qo'shish kerak.

O'zgartirgandan keyin: `go test ./...` (promptlar va ro'yxatlar mosligini tekshiradi,
`docs/prompts` fayllari bilan farqni ko'rsatadi), so'ng
`go test ./internal/ai ./internal/payment -run Golden -update` — fayllarni yangilab,
o'zgarishni `git diff docs/prompts` da ko'rib chiqing. Dizaynni ko'z bilan ko'rish uchun —
`go run ./cmd/preview -out ./previews -theme custom`.
