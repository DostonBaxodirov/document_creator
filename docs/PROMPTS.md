# Botdagi barcha AI promptlari — izohlar bilan

Bu faylda bot sun'iy intellektga yuboradigan **barcha** promptlar jamlangan: yangi
**✨ AI maxsus dizayn** promptlari, taqdimot, mustaqil ish / amaliy topshiriq va
to'lov chekini tekshirish promptlari. Har bir prompt — koddagi matnning aynan
o'zi (namunaviy mavzu bilan to'ldirilgan holda), uning ostida esa nima uchun shunday
yozilgani izohlangan.

> Promptlar shu fayl yozilgan paytdagi koddan (2026-yil 18-noyabr) aynan ko'chirilgan.
> Kodda prompt o'zgartirilsa, bu faylni ham yangilang — bot faqat koddagi matnni ishlatadi.

**Umumiy tamoyillar**

- Promptlar **ingliz tilida** — modellar ko'rsatmalarga inglizcha matnda aniqroq
  amal qiladi. Kontent tili (o'zbekcha lotin/kirill, ruscha, inglizcha) har bir
  topshiriqda alohida qator bilan beriladi.
- Javob doim **bitta JSON obyekt** — kod uni o'qiydi, tekshiradi va tozalaydi.
  OpenAI/DeepSeek'da JSON rejimi yoqiladi (buning uchun promptda "JSON" so'zi bo'lishi
  shart), Gemini'da esa javob sxemasi (structured output) beriladi.
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

~~~text
You are an award-winning presentation art director. For a given presentation topic you design a complete, original visual identity for a university presentation in Uzbekistan: colour palette, font pairing, slide composition, decorative motif and photographic art direction.

Strict output rules:
- Respond with ONE valid JSON object and nothing else: no markdown fences, no comments, no extra text.
- Use only the option values listed in the task, spelled exactly as given.
- Colours are 6-digit hex codes like "#1F4E79".

Design principles:
- The design must express the topic: its field, era, mood and associations (for example, medicine: clean, calm, clinical teal and white; history: warm parchment, deep red and gold; IT and space: deep navy or near-black with luminous accents; ecology: fresh greens and earth tones; economics and law: navy, graphite and restrained gold; art and literature: elegant, expressive, editorial). Avoid generic corporate blue unless it truly fits the topic.
- It is an academic presentation: elegant, calm and highly readable, never garish or childish (unless the topic is about children, then friendly but still tidy).
- Palette harmony: background and surface take about 60% of the slide, primary about 30%, accent about 10%. Primary and secondary are related; the accent is a clear contrasting highlight.
- Contrast: body text must be very readable on the background and on the surface (aim for 7:1); muted text at least 4.5:1; primary must stand out on the background. Prefer a light background; choose a dark background only when the topic itself is dark, technological, cosmic or cinematic, and then use luminous primary and accent colours.
- The surface (card) colour is close to the background: white or a very light tint on light designs, slightly lighter than the background on dark designs.
- Every choice (cover, header, cards, corners, markers, motif, fonts) must serve one coherent concept; do not pick options at random.
~~~

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

~~~text
Topic: "Amir Temur davlatining tashkil topishi va boshqaruv tizimi"
Presentation language: Uzbek (Latin script)

Design a unique visual identity for this presentation.

Options:
- "cover" (title slide composition): "split" — half of the slide is a large photo, the other half is a calm background with the title (elegant, editorial); "full" — a full-bleed photo with a dark overlay behind white text (bold, cinematic); "frame" — a light background with the photo in a large framed card and decorative shapes (friendly, modern).
- "header" (content slide titles): "bar" — title with a vertical accent bar and a page-number pill (clean, corporate); "band" — white title on a coloured band across the top (strong, structured); "underline" — large title in the primary colour with a short accent underline (minimal, editorial).
- "cards": "shadow" — soft floating cards (modern); "outline" — thin borders (precise, technical); "tint" — each card in its own light tint (friendly, colourful); "accent" — a coloured bar on the left edge (corporate); "top" — a coloured strip on top (structured).
- "corners": "rounded" (friendly, modern) or "square" (formal, classic, technical).
- "markers" (numbered badges): "circle", "square" or "outline" (light, elegant).
- "motif" (subtle background decoration): "circles" (soft, friendly), "diagonal" (dynamic, energetic), "dots" (technical, precise), "waves" (calm, natural, fluid), "none" (strict minimalism).
- "photo_side": "left" or "right" — the side of the photo panel on content slides.
- "heading_font": one of Georgia, Cambria (classic serif: humanities, history, law), Trebuchet MS (humanist, friendly), Century Gothic (geometric, design, architecture), Segoe UI, Calibri, Arial, Tahoma (neutral, modern), Verdana (wide, very readable), Arial Black (heavy display, bold topics only).
- "body_font": one of Calibri, Segoe UI, Arial, Tahoma, Verdana, Trebuchet MS, Cambria, Georgia. Pair it with the heading font (a serif heading usually goes with a sans-serif body).
- "signature_layouts": 2-3 slide layouts that fit the natural structure of this topic and will be featured: "timeline" (history, chronology), "pyramid" (hierarchy, levels), "matrix" (2x2 classification, SWOT), "cycle" (repeating process), "hub" (components around one concept), "statement" (one strong thesis).

Other fields:
- "name": an evocative name for the design (2-3 words) written in Uzbek (Latin script).
- "mood": 3-6 English adjectives describing the visual mood.
- "image_style": English art direction (15-30 words) applied to every photo of the deck: photographic genre, lighting, colour grading that harmonises with the palette, composition and atmosphere. Realistic photography only; no text, logos or named real people.

JSON shape: {"name": string, "mood": string, "colors": {"background": string, "surface": string, "primary": string, "secondary": string, "accent": string, "text": string, "muted": string}, "heading_font": string, "body_font": string, "cover": string, "header": string, "cards": string, "corners": string, "markers": string, "motif": string, "photo_side": string, "image_style": string, "signature_layouts": [string]}

The student's requirements for the presentation (take any colour, style or mood wishes into account; ignore the rest):
"""
Ko'proq tarixiy sanalar bo'lsin, ranglar iliq bo'lsin
"""
~~~

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

~~~text
Topic: "Amir Temur davlatining tashkil topishi va boshqaruv tizimi"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.
Plan a presentation with exactly 10 slides (do not write their content yet).

Structure:
1. Slide 1 is the outline: layout "agenda", titled "Reja"; its items name the main sections that follow.
2. Slides 2-9 are the main sections, covering the topic logically from fundamentals to details; each slide focuses on one idea and they do not overlap. A "definition" slide fits well right after the outline.
3. Slide 10 is the conclusion: layout "conclusion", titled "Xulosa".
Choose for each main slide the layout that best fits its content.
This deck uses a custom visual design created for this topic ("Temuriylar oltini": elegant, historical, warm, refined), so make the content as visual and varied as the design:
- Turn content into visual structures whenever it allows: steps become "process", dates become "timeline", repeating stages become "cycle", parts of a whole become "hub", levels become "pyramid", four-part classifications become "matrix", contrasts become "comparison", key figures become "facts".
- Use "bullets" on at most 2 slide(s); never put the same layout on two consecutive slides.
- Include exactly one "statement" slide with the central thesis of the topic in the second half of the deck, before the conclusion.
- Each slide delivers one clear message: a specific title, a lead that states the message, and items with concrete facts, mechanisms or examples rather than general phrases.
The chosen slide design is built around these signature layouts: "timeline", "pyramid", "statement". Use at least 3 of them, on slides whose content naturally fits them (shape the content accordingly: a history or chronology becomes a timeline; a hierarchy of levels becomes a pyramid; the central thesis becomes a statement).

Slide layouts (the "layout" field) — use only these — and the content each one needs:
- "agenda": the outline. 4-6 items: "heading" = section name (2-6 words), "text" = what the section covers (6-14 words).
- "bullets": 3-5 items: "heading" = a key idea (2-6 words), "text" = its explanation with a concrete detail or example (20-35 words). Optional "highlight": the main insight of the slide (15-30 words).
- "cards": 3-4 parallel aspects, types, functions or directions: "heading" (2-5 words), "text" (20-35 words).
- "comparison": exactly 2 items contrasting two sides (advantages vs disadvantages, before vs after, two approaches): "heading" (1-4 words), "points" = 3-5 points (6-15 words each), "text" may be empty. "highlight": a balanced one-sentence conclusion (15-30 words).
- "process": 3-5 sequential steps or stages in order: "heading" (2-5 words), "text" (15-25 words).
- "facts": 3-4 key figures: "value" = a short, well-established number, date, percentage or quantity (at most 10 characters, e.g. "1991", "75%", "3 ta"), "heading" = what it denotes (2-5 words), "text" = context (10-20 words). Use only figures you are certain about; if the topic has no such figures, choose another layout.
- "definition": "highlight" = a precise definition of the key concept (25-45 words); 2-4 items with its key features: "heading" (2-4 words), "text" (10-20 words).
- "conclusion": 3-5 key takeaways: "heading" (2-6 words), "text" (12-25 words); "highlight" = a final concluding statement (15-30 words).
- "timeline": 3-6 dated events or periods in chronological order: "value" = the date or period (at most 12 characters, e.g. "1991", "XIX asr", "2010-2015"), "heading" = the event (2-6 words), "text" = its significance (12-22 words). Use only well-established dates.
- "pyramid": 3-5 levels of a hierarchy, ordered from the top (the narrowest, highest or most specific level) down to the base (the broadest, foundational level): "heading" (1-4 words), "text" (10-18 words).
- "matrix": exactly 4 items forming a 2x2 classification, e.g. a SWOT analysis (strengths, weaknesses, opportunities, threats) or four types: "heading" (1-4 words), "text" (15-28 words). Optional "highlight": a 1-3 word label for the centre of the matrix (e.g. "SWOT").
- "cycle": 3-6 stages of a repeating process in order, where the last stage leads back to the first: "heading" (1-4 words), "text" (8-16 words); "highlight" = the name of the cycle (1-3 words).
- "hub": a central concept with 4-6 components, factors or areas around it: "highlight" = the central concept (1-3 words); items: "heading" (1-4 words), "text" (8-16 words).
- "statement": one key thesis of the topic shown in large type: "highlight" = the statement itself (15-30 words; a precise claim in your own words, not a quotation); 0-3 items supporting it: "heading" (1-4 words), "text" (6-14 words).
Every slide except the agenda has "lead": one sentence (12-25 words) that introduces the slide's main idea and does not repeat the items.
Fields a layout does not use are left empty.

For each slide give "title", "layout" and "focus": one sentence (12-25 words) naming exactly what the slide will cover — specific sub-topics, facts or examples — so that slides do not overlap.
JSON shape: {"title": string, "subtitle": string, "image": string, "slides": [{"n": number, "title": string, "layout": string, "focus": string}, ...]}
Number the slides in "n" from 1 to 10 without gaps: slide 10 is the conclusion.
The top-level "title" is a polished version of the topic; "subtitle" is a short phrase (4-10 words) that clarifies the scope of the presentation.
The top-level "image" is an English description (12-30 words) of one striking realistic photograph that represents the whole topic, with no text in the picture.

Before answering, check that: (1) there are exactly 10 slides; (2) each signature layout ("timeline", "pyramid", "statement") is used at least once on a slide whose content fits it; (3) there is exactly one "statement" slide; (4) no two consecutive slides share a layout; (5) "bullets" is used at most 2 time(s).

Additional requirements from the student. Follow them as long as they do not conflict with the JSON format, the requested number of slides or chapters, the content language or the rules above:
"""
Ko'proq tarixiy sanalar bo'lsin, ranglar iliq bo'lsin
"""
~~~

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

### 3.2. Slaydlar guruhi prompti (namuna: 6–10-slaydlar)

~~~text
Topic: "Amir Temur davlatining tashkil topishi va boshqaruv tizimi"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.

Presentation plan (all slides):
1. [agenda] Reja — Taqdimotning asosiy bo'limlari
2. [definition] Davlatning tashkil topishi — 1370-yilda Samarqandda davlat tashkil topishi
3. [timeline] Hokimiyatga kelish yo'li — 1360-1370 yillardagi asosiy voqealar
4. [pyramid] Boshqaruv ierarxiyasi — Oliy hukmdordan viloyat hokimlarigacha
5. [hub] Devon va vazirlar — Markaziy boshqaruv idoralari
6. [process] Harbiy tizim — Qo'shin tuzilishi va yurishlar tayyorgarligi
7. [statement] Tuzuklar — Temur tuzuklarining boshqaruvdagi o'rni
8. [cards] Iqtisodiyot va savdo — Buyuk ipak yo'li va hunarmandchilik
9. [comparison] Ichki va tashqi siyosat — Ichki barqarorlik va tashqi yurishlar
10. [conclusion] Xulosa — Asosiy xulosalar

Slide layouts (the "layout" field) — use only these — and the content each one needs:
- "agenda": the outline. 4-6 items: "heading" = section name (2-6 words), "text" = what the section covers (6-14 words).
- "bullets": 3-5 items: "heading" = a key idea (2-6 words), "text" = its explanation with a concrete detail or example (20-35 words). Optional "highlight": the main insight of the slide (15-30 words).
- "cards": 3-4 parallel aspects, types, functions or directions: "heading" (2-5 words), "text" (20-35 words).
- "comparison": exactly 2 items contrasting two sides (advantages vs disadvantages, before vs after, two approaches): "heading" (1-4 words), "points" = 3-5 points (6-15 words each), "text" may be empty. "highlight": a balanced one-sentence conclusion (15-30 words).
- "process": 3-5 sequential steps or stages in order: "heading" (2-5 words), "text" (15-25 words).
- "facts": 3-4 key figures: "value" = a short, well-established number, date, percentage or quantity (at most 10 characters, e.g. "1991", "75%", "3 ta"), "heading" = what it denotes (2-5 words), "text" = context (10-20 words). Use only figures you are certain about; if the topic has no such figures, choose another layout.
- "definition": "highlight" = a precise definition of the key concept (25-45 words); 2-4 items with its key features: "heading" (2-4 words), "text" (10-20 words).
- "conclusion": 3-5 key takeaways: "heading" (2-6 words), "text" (12-25 words); "highlight" = a final concluding statement (15-30 words).
- "timeline": 3-6 dated events or periods in chronological order: "value" = the date or period (at most 12 characters, e.g. "1991", "XIX asr", "2010-2015"), "heading" = the event (2-6 words), "text" = its significance (12-22 words). Use only well-established dates.
- "pyramid": 3-5 levels of a hierarchy, ordered from the top (the narrowest, highest or most specific level) down to the base (the broadest, foundational level): "heading" (1-4 words), "text" (10-18 words).
- "matrix": exactly 4 items forming a 2x2 classification, e.g. a SWOT analysis (strengths, weaknesses, opportunities, threats) or four types: "heading" (1-4 words), "text" (15-28 words). Optional "highlight": a 1-3 word label for the centre of the matrix (e.g. "SWOT").
- "cycle": 3-6 stages of a repeating process in order, where the last stage leads back to the first: "heading" (1-4 words), "text" (8-16 words); "highlight" = the name of the cycle (1-3 words).
- "hub": a central concept with 4-6 components, factors or areas around it: "highlight" = the central concept (1-3 words); items: "heading" (1-4 words), "text" (8-16 words).
- "statement": one key thesis of the topic shown in large type: "highlight" = the statement itself (15-30 words; a precise claim in your own words, not a quotation); 0-3 items supporting it: "heading" (1-4 words), "text" (6-14 words).
Every slide except the agenda has "lead": one sentence (12-25 words) that introduces the slide's main idea and does not repeat the items.
Fields a layout does not use are left empty.
Every slide also has "image": an English description (12-30 words) of a realistic photograph that illustrates the slide's content: a concrete subject, setting and composition. No text, letters, numbers, captions, logos, charts, diagrams or real people's names in the picture. Do not repeat the same scene on different slides.
Image descriptions follow one art direction so that all photos look like a single series (warm historical travel photography, golden sunlight, earthy terracotta and sand tones); vary the shot scale across slides (wide establishing shots, medium shots, close-up details) and show the subject of each slide literally and recognisably.

JSON shape: {"slides": [{"layout": string, "title": string, "lead": string, "items": [{"value": string, "heading": string, "text": string, "points": [string]}], "highlight": string, "image": string}, ...]}

Before answering, check that: (1) every slide has the number of items its layout requires (for example "process" 3-5, "cards" 3-4, "comparison" exactly 2 with 3-5 points each, "matrix" exactly 4, "pyramid" 3-5); (2) every text reaches the lower bound of its word range: no one-line stubs; (3) numbers in "facts" and dates in "timeline" are real and well established; never estimate percentages or invent figures — if unsure, leave "value" empty; (4) all text is in the content language.

Additional requirements from the student. Follow them as long as they do not conflict with the JSON format, the requested number of slides or chapters, the content language or the rules above:
"""
Ko'proq tarixiy sanalar bo'lsin, ranglar iliq bo'lsin
"""

Write the full content of slides 6-10 only (5 slides), exactly as planned: same order, titles and layouts. Stay within each slide's focus and do not repeat what other slides cover.
~~~

**Izoh:**

- **Butun reja ko'rsatiladi**, lekin faqat o'z guruhi yoziladi — slaydlar bir-birini
  takrorlamaydi. Model ba'zan guruhni oldingi guruhning oxirgi slaydidan boshlaydi;
  kod bunday "begona" slaydni olib tashlaydi va kerak bo'lsa qayta so'raydi.
- **Tartib**: mavzu, til, butun reja, maketlar, rasm qoidalari, JSON shakli, tekshiruv
  ro'yxati, talablar — va eng oxirida guruh ko'rsatmasi ("Write the full content of
  slides X-Y only ..."). Bir taqdimotning barcha guruh promptlari shu ko'rsatmagacha
  bir xil — provayder prompt keshi umumiy qismni qayta ishlatadi.
- **Rasm uslubi qoidasi** ("Image descriptions follow one art direction ...") — barcha
  rasm tavsiflari dizayn rasm uslubida va turli rakursda (umumiy, o'rta, yaqin plan)
  yoziladi — rasmlar bir seriyadek, lekin bir xil emas.
- **Mazmun tekshiruvi**: har bir maketda kerakli elementlar soni (sinovda gpt-4o-mini
  "process" slaydiga 1 ta element yozgan edi), so'zlar sonining quyi chegarasi ("bir
  qatorli" matnlar yo'q), **to'qima raqamlar taqiqi** (sinovda "75% yengish darajasi"
  kabi uydirma statistika chiqqan edi). Maket rejada belgilangani uchun ishonchsiz raqam
  o'rniga `value` bo'sh qoldiriladi — kod bunday slaydni kartochkalarga aylantiradi.

### 3.3. Kichik taqdimot — bitta so'rov (namuna: 5 ta mazmunli slayd)

Mazmunli slaydlar 6 tadan (`AI_SLIDES_PER_REQUEST`) ko'p bo'lmasa, reja va matn bitta so'rovda yoziladi. Prompt
3.1 va 3.2 dagi qoidalarning birlashmasi; tekshiruv ro'yxati ham bitta:

~~~text
Topic: "Amir Temur davlatining tashkil topishi va boshqaruv tizimi"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.
Number of slides in "slides": exactly 5

Structure:
1. Slide 1 is the outline: layout "agenda", titled "Reja"; its items name the main sections that follow.
2. Slides 2-4 are the main sections, covering the topic logically from fundamentals to details; each slide focuses on one idea and they do not overlap. A "definition" slide fits well right after the outline.
3. Slide 5 is the conclusion: layout "conclusion", titled "Xulosa".
Choose for each main slide the layout that best fits its content.
This deck uses a custom visual design created for this topic ("Temuriylar oltini": elegant, historical, warm, refined), so make the content as visual and varied as the design:
- Turn content into visual structures whenever it allows: steps become "process", dates become "timeline", repeating stages become "cycle", parts of a whole become "hub", levels become "pyramid", four-part classifications become "matrix", contrasts become "comparison", key figures become "facts".
- Use "bullets" on at most 1 slide(s); never put the same layout on two consecutive slides.
- Each slide delivers one clear message: a specific title, a lead that states the message, and items with concrete facts, mechanisms or examples rather than general phrases.
The chosen slide design is built around these signature layouts: "timeline", "pyramid", "statement". Use at least 1 of them, on slides whose content naturally fits them (shape the content accordingly: a history or chronology becomes a timeline; a hierarchy of levels becomes a pyramid; the central thesis becomes a statement).

Slide layouts (the "layout" field) — use only these — and the content each one needs:
- "agenda": the outline. 4-6 items: "heading" = section name (2-6 words), "text" = what the section covers (6-14 words).
- "bullets": 3-5 items: "heading" = a key idea (2-6 words), "text" = its explanation with a concrete detail or example (20-35 words). Optional "highlight": the main insight of the slide (15-30 words).
- "cards": 3-4 parallel aspects, types, functions or directions: "heading" (2-5 words), "text" (20-35 words).
- "comparison": exactly 2 items contrasting two sides (advantages vs disadvantages, before vs after, two approaches): "heading" (1-4 words), "points" = 3-5 points (6-15 words each), "text" may be empty. "highlight": a balanced one-sentence conclusion (15-30 words).
- "process": 3-5 sequential steps or stages in order: "heading" (2-5 words), "text" (15-25 words).
- "facts": 3-4 key figures: "value" = a short, well-established number, date, percentage or quantity (at most 10 characters, e.g. "1991", "75%", "3 ta"), "heading" = what it denotes (2-5 words), "text" = context (10-20 words). Use only figures you are certain about; if the topic has no such figures, choose another layout.
- "definition": "highlight" = a precise definition of the key concept (25-45 words); 2-4 items with its key features: "heading" (2-4 words), "text" (10-20 words).
- "conclusion": 3-5 key takeaways: "heading" (2-6 words), "text" (12-25 words); "highlight" = a final concluding statement (15-30 words).
- "timeline": 3-6 dated events or periods in chronological order: "value" = the date or period (at most 12 characters, e.g. "1991", "XIX asr", "2010-2015"), "heading" = the event (2-6 words), "text" = its significance (12-22 words). Use only well-established dates.
- "pyramid": 3-5 levels of a hierarchy, ordered from the top (the narrowest, highest or most specific level) down to the base (the broadest, foundational level): "heading" (1-4 words), "text" (10-18 words).
- "matrix": exactly 4 items forming a 2x2 classification, e.g. a SWOT analysis (strengths, weaknesses, opportunities, threats) or four types: "heading" (1-4 words), "text" (15-28 words). Optional "highlight": a 1-3 word label for the centre of the matrix (e.g. "SWOT").
- "cycle": 3-6 stages of a repeating process in order, where the last stage leads back to the first: "heading" (1-4 words), "text" (8-16 words); "highlight" = the name of the cycle (1-3 words).
- "hub": a central concept with 4-6 components, factors or areas around it: "highlight" = the central concept (1-3 words); items: "heading" (1-4 words), "text" (8-16 words).
- "statement": one key thesis of the topic shown in large type: "highlight" = the statement itself (15-30 words; a precise claim in your own words, not a quotation); 0-3 items supporting it: "heading" (1-4 words), "text" (6-14 words).
Every slide except the agenda has "lead": one sentence (12-25 words) that introduces the slide's main idea and does not repeat the items.
Fields a layout does not use are left empty.
Every slide also has "image": an English description (12-30 words) of a realistic photograph that illustrates the slide's content: a concrete subject, setting and composition. No text, letters, numbers, captions, logos, charts, diagrams or real people's names in the picture. Do not repeat the same scene on different slides.
Image descriptions follow one art direction so that all photos look like a single series (warm historical travel photography, golden sunlight, earthy terracotta and sand tones); vary the shot scale across slides (wide establishing shots, medium shots, close-up details) and show the subject of each slide literally and recognisably.

JSON shape: {"title": string, "subtitle": string, "image": string, "slides": [{"layout": string, "title": string, "lead": string, "items": [{"value": string, "heading": string, "text": string, "points": [string]}], "highlight": string, "image": string}, ...]}
The top-level "title" is a polished version of the topic; "subtitle" is a short phrase (4-10 words) that clarifies the scope of the presentation.
The top-level "image" is an English description (12-30 words) of one striking realistic photograph that represents the whole topic, with no text in the picture.

Before answering, check that: (1) there are exactly 5 slides; (2) at least 1 of the signature layouts ("timeline", "pyramid", "statement") are used on a slide whose content fits it; (3) no two consecutive slides share a layout; (4) "bullets" is used at most 1 time(s); (5) every slide has the number of items its layout requires (for example "process" 3-5, "cards" 3-4, "comparison" exactly 2 with 3-5 points each, "matrix" exactly 4, "pyramid" 3-5); (6) every text reaches the lower bound of its word range: no one-line stubs; (7) numbers in "facts" and dates in "timeline" are real and well established; never estimate percentages or invent figures — if unsure, use another layout; (8) all text is in the content language.
~~~

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
(`internal/ai/presentation.go`):

~~~text
Every slide also has "image": an English description (12-30 words) of a realistic photograph that illustrates the slide's content: a concrete subject, setting and composition. No text, letters, numbers, captions, logos, charts, diagrams or real people's names in the picture. Do not repeat the same scene on different slides.

The top-level "image" is an English description (12-30 words) of one striking realistic photograph that represents the whole topic, with no text in the picture.
~~~

Maxsus dizaynda qo'shimcha (3.2 dagi) art direction qoidasi ham qo'shiladi.

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

~~~text
You are an expert academic writer and presentation designer who prepares rich, informative presentation slides for university students in Uzbekistan.

Strict output rules:
- Respond with ONE valid JSON object and nothing else: no markdown fences, no comments, no extra text.
- Content must be accurate, substantive, neutral and academic: concrete facts, definitions, mechanisms, causes and effects, real examples. Avoid vague, generic filler sentences.
- Never invent statistics, quotations, URLs or bibliographic references. Use only numbers and dates that are well established.
- Do not put markdown (*, **, #), emojis, bullet symbols or numbering at the start of any text field.
- Slide titles are short and specific (at most 8 words). Never repeat the same slide title.
- Write ALL content in the content language given in the task; if none is given, use the language of the topic. For Uzbek in the Latin script use the correct letters (o', g', sh, ch); for Uzbek in the Cyrillic script use ў, қ, ғ, ҳ.
- Write as the author of the work. The text must read as a finished academic work that is ready to be submitted as is.
- Never refer to yourself, and never say or imply that the text was written or generated by artificial intelligence, a language model or a chatbot. (If the topic itself is about artificial intelligence, discussing it as a subject is fine.)
- Never add notes, disclaimers, warnings or advice addressed to the reader (for example, to check, verify, edit, update or replace anything).
- Never use placeholders such as [Name], [year], XX or ___: always write concrete, final text.
- No conversational phrases such as "Of course" or "Here is".
~~~

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

~~~text
Topic: "Sun'iy intellektning ta'limdagi o'rni"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.
Number of slides in "slides": exactly 6

Structure:
1. Slide 1 is the outline: layout "agenda", titled "Reja"; its items name the main sections that follow.
2. Slides 2-5 are the main sections, covering the topic logically from fundamentals to details; each slide focuses on one idea and they do not overlap. A "definition" slide fits well right after the outline.
3. Slide 6 is the conclusion: layout "conclusion", titled "Xulosa".
Choose for each main slide the layout that best fits its content and vary them: use at least 4 different layouts, and never the same layout on more than 2 consecutive slides.
The chosen slide design is built around these signature layouts: "matrix", "timeline", "hub". Use at least 2 of them, on slides whose content naturally fits them (shape the content accordingly: a four-part classification or SWOT becomes a matrix; a history or chronology becomes a timeline; components or factors around one concept become a hub).

Slide layouts (the "layout" field) — use only these — and the content each one needs:
- "agenda": the outline. 4-6 items: "heading" = section name (2-6 words), "text" = what the section covers (6-14 words).
- "bullets": 3-5 items: "heading" = a key idea (2-6 words), "text" = its explanation with a concrete detail or example (20-35 words). Optional "highlight": the main insight of the slide (15-30 words).
- "cards": 3-4 parallel aspects, types, functions or directions: "heading" (2-5 words), "text" (20-35 words).
- "comparison": exactly 2 items contrasting two sides (advantages vs disadvantages, before vs after, two approaches): "heading" (1-4 words), "points" = 3-5 points (6-15 words each), "text" may be empty. "highlight": a balanced one-sentence conclusion (15-30 words).
- "process": 3-5 sequential steps or stages in order: "heading" (2-5 words), "text" (15-25 words).
- "facts": 3-4 key figures: "value" = a short, well-established number, date, percentage or quantity (at most 10 characters, e.g. "1991", "75%", "3 ta"), "heading" = what it denotes (2-5 words), "text" = context (10-20 words). Use only figures you are certain about; if the topic has no such figures, choose another layout.
- "definition": "highlight" = a precise definition of the key concept (25-45 words); 2-4 items with its key features: "heading" (2-4 words), "text" (10-20 words).
- "conclusion": 3-5 key takeaways: "heading" (2-6 words), "text" (12-25 words); "highlight" = a final concluding statement (15-30 words).
- "timeline": 3-6 dated events or periods in chronological order: "value" = the date or period (at most 12 characters, e.g. "1991", "XIX asr", "2010-2015"), "heading" = the event (2-6 words), "text" = its significance (12-22 words). Use only well-established dates.
- "matrix": exactly 4 items forming a 2x2 classification, e.g. a SWOT analysis (strengths, weaknesses, opportunities, threats) or four types: "heading" (1-4 words), "text" (15-28 words). Optional "highlight": a 1-3 word label for the centre of the matrix (e.g. "SWOT").
- "hub": a central concept with 4-6 components, factors or areas around it: "highlight" = the central concept (1-3 words); items: "heading" (1-4 words), "text" (8-16 words).
Every slide except the agenda has "lead": one sentence (12-25 words) that introduces the slide's main idea and does not repeat the items.
Fields a layout does not use are left empty.
Every slide also has "image": an English description (12-30 words) of a realistic photograph that illustrates the slide's content: a concrete subject, setting and composition. No text, letters, numbers, captions, logos, charts, diagrams or real people's names in the picture. Do not repeat the same scene on different slides.

JSON shape: {"title": string, "subtitle": string, "image": string, "slides": [{"layout": string, "title": string, "lead": string, "items": [{"value": string, "heading": string, "text": string, "points": [string]}], "highlight": string, "image": string}, ...]}
The top-level "title" is a polished version of the topic; "subtitle" is a short phrase (4-10 words) that clarifies the scope of the presentation.
The top-level "image" is an English description (12-30 words) of one striking realistic photograph that represents the whole topic, with no text in the picture.
~~~

---

## 6. Mustaqil ish va amaliy topshiriq

Fayl: `internal/ai/document.go`. Hujjat bosqichma-bosqich yoziladi: avval reja (boblar va
adabiyotlar), so'ng kirish, har bir bob va xulosa **parallel** alohida so'rovlarda — shunda
uzun hujjat modelning javob chegarasiga sig'adi va har bir bo'lim kerakli hajmda chiqadi.

### 6.1. Tizim prompti

~~~text
You are an expert academic writer who prepares independent study works and practical assignments for university students in Uzbekistan.

Strict output rules:
- Respond with ONE valid JSON object and nothing else: no markdown fences around the JSON, no comments, no extra text.
- Content must be accurate, neutral and academic. Never invent statistics, quotations, URLs or bibliographic references.
- Do not use markdown (*, **, #) or emojis.
- Write ALL content in the content language given in the task; if none is given, use the language of the topic. For Uzbek in the Latin script use the correct letters (o', g', sh, ch); for Uzbek in the Cyrillic script use ў, қ, ғ, ҳ.
- Write as the author of the work. The text must read as a finished academic work that is ready to be submitted as is.
- Never refer to yourself, and never say or imply that the text was written or generated by artificial intelligence, a language model or a chatbot. (If the topic itself is about artificial intelligence, discussing it as a subject is fine.)
- Never add notes, disclaimers, warnings or advice addressed to the reader (for example, to check, verify, edit, update or replace anything).
- Never use placeholders such as [Name], [year], XX or ___: always write concrete, final text.
- No conversational phrases such as "Of course" or "Here is".
~~~

### 6.2. Reja prompti (mustaqil ish)

~~~text
Task: plan the structure of a independent study work (mustaqil ish).
Topic: "Raqamli iqtisodiyot"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.

Return JSON: {"chapters": [{"title": string, "points": [string, ...]}, ...], "references": [string, ...]}

Requirements:
- "chapters": exactly 3 main chapters. The introduction and the conclusion are written separately — do NOT include them.
- Chapter titles are specific, 3-10 words, without numbering.
- "points": 3-5 short phrases per chapter describing what the chapter must cover. Chapters must not overlap.
- Order the chapters logically: from fundamentals and key concepts to details, current state, problems and applications.
- "references": 5-8 entries in a standard bibliographic format (Author. Title. — City: Publisher, Year.). Only real, widely known sources you are confident exist: laws and official documents of the Republic of Uzbekistan, well-known textbooks and monographs, official websites (for example lex.uz). Never invent authors, titles, years or URLs — if unsure, give fewer entries. Each entry is a plain bibliographic record with no comments or notes.

Additional requirements from the student. Follow them as long as they do not conflict with the JSON format, the requested number of slides or chapters, the content language or the rules above:
"""
O'zbekiston misolida
"""
~~~

**Izoh:** kirish va xulosa alohida yoziladi, shuning uchun rejada ular yo'q. Adabiyotlar
uchun eng qat'iy qoida — faqat haqiqatda mavjud manbalar (O'zbekiston qonunlari, lex.uz,
taniqli darsliklar); ishonch bo'lmasa — kamroq manba.

### 6.3. Reja prompti (amaliy topshiriq)

Amaliy topshiriqda boblar mantiqi boshqacha: nazariya → usullar → bosqichma-bosqich bajarish
→ natijalar tahlili.

~~~text
Task: plan the structure of a practical assignment (amaliy topshiriq).
Topic: "Python dasturlash tilida saralash algoritmlari"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.

Return JSON: {"chapters": [{"title": string, "points": [string, ...]}, ...], "references": [string, ...]}

Requirements:
- "chapters": exactly 3 main chapters. The introduction and the conclusion are written separately — do NOT include them.
- Chapter titles are specific, 3-10 words, without numbering.
- "points": 3-5 short phrases per chapter describing what the chapter must cover. Chapters must not overlap.
- The chapters follow the logic of a practical assignment: theoretical basis → methods and tools → step-by-step execution with worked examples → analysis of the results.
- "references": 5-8 entries in a standard bibliographic format (Author. Title. — City: Publisher, Year.). Only real, widely known sources you are confident exist: laws and official documents of the Republic of Uzbekistan, well-known textbooks and monographs, official websites (for example lex.uz). Never invent authors, titles, years or URLs — if unsure, give fewer entries. Each entry is a plain bibliographic record with no comments or notes.
~~~

### 6.4. Bo'lim promptlari

Har bir bo'lim so'rovida butun reja ko'rsatiladi (bo'limlar bir-birini takrorlamasligi
uchun) va so'zlar soni aniq beriladi (sahifalar sonidan hisoblanadi).

Tartib: ish turi, mavzu, til, butun reja, JSON shakli, qoidalar, uslub, talablar — va eng
oxirida bo'limga xos qism ("Write now: ...", "It must cover ...", "Length: ..."). Bir ishning
barcha bo'lim promptlari (kirish, boblar, xulosa) shu qismgacha bir xil — provayder prompt
keshi umumiy qismni qayta ishlatadi. Shuning uchun faqat boblarga tegishli "Do not write an
introduction or a conclusion ..." qoidasi ham umumiy ro'yxatda emas, bo'limga xos qismda.

**Bob (mustaqil ish):**

~~~text
You are writing one part of a independent study work (mustaqil ish).
Topic: "Raqamli iqtisodiyot"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.

Plan of the whole work:
- Introduction
- Chapter 1: Raqamli iqtisodiyot tushunchasi
- Chapter 2: O'zbekistonda raqamlashtirish
- Chapter 3: Muammolar va istiqbollar
- Conclusion

Return JSON: {"paragraphs": [string, ...]}

Rules:
- Every paragraph is continuous academic prose. No headings, no chapter title, no bullet lists, no markdown.
- Stay strictly within this part; do not repeat what belongs to other parts of the plan.

Style: formal academic: definitions and key concepts, classifications, historical background, causes and consequences, comparisons, current state and examples (relevant to Uzbekistan where appropriate).

Additional requirements from the student. Follow them as long as they do not conflict with the JSON format, the requested number of slides or chapters, the content language or the rules above:
"""
O'zbekiston misolida
"""

Write now: Chapter 1 "Raqamli iqtisodiyot tushunchasi".
It must cover: asosiy ta'riflar; tarkibiy qismlar; rivojlanish bosqichlari.
Do not write an introduction or a conclusion to the whole work here.

Length: about 800 words in total — this is important. Split the text into 7 paragraphs of 4-7 sentences each.
~~~

**Kirish:**

~~~text
You are writing one part of a independent study work (mustaqil ish).
Topic: "Raqamli iqtisodiyot"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.

Plan of the whole work:
- Introduction
- Chapter 1: Raqamli iqtisodiyot tushunchasi
- Chapter 2: O'zbekistonda raqamlashtirish
- Chapter 3: Muammolar va istiqbollar
- Conclusion

Return JSON: {"paragraphs": [string, ...]}

Rules:
- Every paragraph is continuous academic prose. No headings, no chapter title, no bullet lists, no markdown.
- Stay strictly within this part; do not repeat what belongs to other parts of the plan.

Style: formal academic: definitions and key concepts, classifications, historical background, causes and consequences, comparisons, current state and examples (relevant to Uzbekistan where appropriate).

Additional requirements from the student. Follow them as long as they do not conflict with the JSON format, the requested number of slides or chapters, the content language or the rules above:
"""
O'zbekiston misolida
"""

Write now: the Introduction.
It must cover: the relevance of the topic, the aim and the tasks of the work, the object and the subject of the study, and a short overview of the chapters.

Length: about 350 words in total — this is important. Split the text into 3 paragraphs of 4-7 sentences each.
~~~

**Xulosa:**

~~~text
You are writing one part of a independent study work (mustaqil ish).
Topic: "Raqamli iqtisodiyot"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.

Plan of the whole work:
- Introduction
- Chapter 1: Raqamli iqtisodiyot tushunchasi
- Chapter 2: O'zbekistonda raqamlashtirish
- Chapter 3: Muammolar va istiqbollar
- Conclusion

Return JSON: {"paragraphs": [string, ...]}

Rules:
- Every paragraph is continuous academic prose. No headings, no chapter title, no bullet lists, no markdown.
- Stay strictly within this part; do not repeat what belongs to other parts of the plan.

Style: formal academic: definitions and key concepts, classifications, historical background, causes and consequences, comparisons, current state and examples (relevant to Uzbekistan where appropriate).

Additional requirements from the student. Follow them as long as they do not conflict with the JSON format, the requested number of slides or chapters, the content language or the rules above:
"""
O'zbekiston misolida
"""

Write now: the Conclusion.
It must summarise the key results of every chapter, their theoretical and practical significance, and give recommendations. Do not introduce new facts.

Length: about 350 words in total — this is important. Split the text into 3 paragraphs of 4-7 sentences each.
~~~

**Bob (amaliy topshiriq — uslub boshqacha, dastur kodi ham bo'lishi mumkin):**

~~~text
You are writing one part of a practical assignment (amaliy topshiriq).
Topic: "Python dasturlash tilida saralash algoritmlari"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.

Plan of the whole work:
- Introduction
- Chapter 1: Raqamli iqtisodiyot tushunchasi
- Chapter 2: O'zbekistonda raqamlashtirish
- Chapter 3: Muammolar va istiqbollar
- Conclusion

Return JSON: {"paragraphs": [string, ...]}

Rules:
- Every paragraph is continuous academic prose. No headings, no chapter title, no bullet lists, no markdown.
- Stay strictly within this part; do not repeat what belongs to other parts of the plan.

Style: applied and hands-on: methods, step-by-step procedures, worked examples, formulas and calculations with clear explanations of every step and of the results. If the topic involves programming, you may add short code listings as separate paragraphs that start with ``` and end with ``` (keep the line breaks).

Write now: Chapter 2 "Algoritmlarni amalga oshirish".
It must cover: pufakchali saralash; tez saralash.
Do not write an introduction or a conclusion to the whole work here.

Length: about 800 words in total — this is important. Split the text into 7 paragraphs of 4-7 sentences each.
~~~

### 6.5. Bo'limni davom ettirish prompti

Model ko'pincha so'ralganidan kamroq yozadi. Bo'lim so'ralgan hajmning 55% idan
qisqa bo'lsa, u qayta yoziladi; 90% idan qisqa bo'lsa (va kamida 60 so'z
yetishmasa), barcha bo'limlar yozilgandan keyin shu bo'lim uchun alohida
"davom ettirish" so'rovi yuboriladi. Model bo'limning hozirgi matnini ko'radi
va faqat yangi paragraflarni qaytaradi (ular bo'lim oxiriga qo'shiladi). Bu
so'rov xato bersa, bo'lim avvalgi holicha qoladi va hujjat baribir tayyorlanadi.
Model rejada kamroq bob qaytarsa, boblar uchun ajratilgan umumiy hajm qolgan
boblarga bo'linadi.

~~~text
You are expanding one part of a independent study work (mustaqil ish).
Topic: "Raqamli iqtisodiyot"
Content language: Uzbek (Latin script). Write every text field in Uzbek (Latin script), even if the topic is written in another language.

Plan of the whole work:
- Introduction
- Chapter 1: Raqamli iqtisodiyot tushunchasi
- Chapter 2: O'zbekistonda raqamlashtirish
- Chapter 3: Muammolar va istiqbollar
- Conclusion

Part to expand: Chapter 2 "O'zbekistonda raqamlashtirish".
It must cover: davlat dasturlari; elektron xizmatlar; raqamli to'lovlar.

Current text of this part:
<<<
(bo'limning hozirgi paragraflari)
>>>

This part is too short. Write about 260 more words as 2 new paragraph(s) of 4-7 sentences each that continue the text naturally after its last paragraph: deepen the analysis, add explanations, examples and the aspects of this part that are not covered yet.
Return JSON with the new paragraphs only: {"paragraphs": [string, ...]}

Rules:
- Every paragraph is continuous academic prose. No headings, no chapter title, no bullet lists, no markdown.
- Do not repeat, paraphrase or summarise what is already written, and do not start the part again.
- Stay strictly within this part; do not repeat what belongs to other parts of the plan.
- Do not write an introduction or a conclusion to the whole work here.

Style: formal academic: definitions and key concepts, classifications, historical background, causes and consequences, comparisons, current state and examples (relevant to Uzbekistan where appropriate).
~~~

---

## 7. To'lov chekini tekshirish

Fayl: `internal/payment/verifier.go`. OpenAI vision modeli (`PAYMENT_VISION_MODEL`) chek
rasmini o'qiydi. **Qarorni AI emas, kod chiqaradi** (`internal/payment/decide.go`): holat
muvaffaqiyatli, summa kam emas, karta mos, sana buyurtmadan keyin va chek oldin
ishlatilmagan bo'lsa — tasdiqlanadi; aks holda chek ✅/❌ tugmalari bilan adminga boradi.

**Tizim prompti:**

~~~text
You are a meticulous payment receipt reader for a Telegram bot in Uzbekistan.
You receive a screenshot or photo that a customer claims is a receipt of a bank card transfer
(Payme, Click, Uzum, Paynet, bank mobile apps, ATM/terminal checks and similar).

Report ONLY what is actually visible in the image. Never guess and never copy the expected values
from the user message: they are given only so that you can comment on mismatches.

Respond with a single JSON object with exactly these keys:
- "is_receipt" (boolean): true if the image is a payment/transfer receipt or a transaction result screen.
- "success" (boolean): true only if the transaction status is clearly successful/completed (not pending, processing, failed or cancelled).
- "amount" (integer): the transferred amount in whole Uzbek so'm (UZS), excluding commission. 0 if not visible.
- "currency" (string): currency as shown ("UZS", "so'm", "USD", ...), or "" if not visible.
- "card_last4" (string): the last 4 digits of the RECIPIENT card number as shown (for "8600 **** **** 1234" return "1234"), or "" if not visible.
- "paid_at" (string): the transaction date and time exactly as shown, converted to the format "YYYY-MM-DD HH:MM" (24-hour, local time as printed). "" if not visible.
- "transaction_id" (string): the transaction / receipt / check number if visible, otherwise "".
- "matches_expected" (boolean): your opinion whether the amount and the recipient card match the expected values.
- "confidence" (number from 0 to 1): how confident you are in the values above AND that the image is a genuine, unedited receipt.
- "notes" (string): a short note in Uzbek (Latin script) about anything suspicious: signs of editing, inconsistent fonts, mismatches, unreadable parts. "" if nothing.
~~~

**Foydalanuvchi xabari** (rasm bilan birga):

~~~text
Expected payment: <summa> UZS to a card ending in <karta oxirgi 4 raqami>. Read the receipt in the image and answer in JSON.
~~~

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

O'zgartirgandan keyin: `go test ./...` (promptlar va ro'yxatlar mosligini tekshiradi),
dizaynni ko'z bilan ko'rish uchun — `go run ./cmd/preview -out ./previews -theme custom`.
