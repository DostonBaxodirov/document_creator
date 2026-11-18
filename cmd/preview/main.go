// Command preview har bir dizayn mavzusida namunaviy taqdimot yaratadi
// (AI va Telegram'siz). Dizaynni o'zgartirgandan keyin natijani tez ko'rish
// uchun ishlatiladi:
//
//	go run ./cmd/preview -out ./previews
//	go run ./cmd/preview -out ./previews -theme zamonaviy
//	go run ./cmd/preview -out ./previews -lang ru   # titul/yakuniy slayd yozuvlari ruscha
//
// Rasmli dizaynlarda sukut bo'yicha namunaviy (gradientli) rasmlar qo'yiladi.
// Haqiqiy AI rasmlar bilan sinash uchun (.env dagi OPENAI_API_KEY /
// GEMINI_API_KEY ishlatiladi, pullik — har bir rasm uchun bir necha sent):
//
//	go run ./cmd/preview -photos standard -theme okean   # arzon tarif (OpenAI)
//	go run ./cmd/preview -photos premium -theme okean    # sifatli tarif (IMAGE_PREMIUM_PROVIDER)
//
// Har bir qatorda fayl yo'li va mavzu nomi (TAB bilan) chiqariladi.
// Slaydlarni rasmga aylantirish (LibreOffice o'rnatilgan bo'lsa):
//
//	soffice --headless --convert-to pdf --outdir ./previews ./previews/*.pptx
//
// Bot dizayn tanlashda ko'rsatadigan templates/designs.png rasmi
// scripts/design_preview.py bilan shu vosita orqali yangilanadi.
//
// Word dizaynlari (mustaqil ish, referat) namunalari — .docx:
//
//	go run ./cmd/preview -out ./previews -docs
//
// templates/doc_designs.png esa scripts/doc_design_preview.py bilan yangilanadi.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/config"
	"telegram-presentation-bot/internal/generator"
	"telegram-presentation-bot/internal/images"
	"telegram-presentation-bot/internal/lang"
)

func main() {
	tpl := flag.String("template", "templates/template.pptx", "asosiy .pptx paket")
	out := flag.String("out", "previews", "natija papkasi")
	only := flag.String("theme", "", "faqat shu mavzu (bo'sh — barchasi)")
	photos := flag.String("photos", "", "haqiqiy AI rasmlar tarifi: standard yoki premium (bo'sh — namunaviy rasmlar)")
	language := flag.String("lang", "", "yozuvlar tili: uz, uz-cyrl, ru, en (bo'sh — uz)")
	docs := flag.Bool("docs", false, "Word dizaynlari namunalari (.docx): tayyor dizaynlar va maxsus dizayn namunalari")
	flag.Parse()

	if *docs {
		if err := runDocs(*out); err != nil {
			fmt.Fprintln(os.Stderr, "preview:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(*tpl, *out, *only, *photos, *language); err != nil {
		fmt.Fprintln(os.Stderr, "preview:", err)
		os.Exit(1)
	}
}

func run(tpl, out, only, photos, language string) error {
	ctx := context.Background()
	if language != "" {
		if _, ok := lang.Find(language); !ok {
			return fmt.Errorf("-lang noto'g'ri: %q (uz, uz-cyrl, ru yoki en)", language)
		}
	}
	if err := os.MkdirAll(out, 0o750); err != nil {
		return err
	}
	g, err := generator.NewPPTX(tpl, out, zap.NewNop())
	if err != nil {
		return err
	}

	var svc *images.Service
	if photos != "" {
		if !images.Tier(photos).Valid() {
			return fmt.Errorf("-photos noto'g'ri: %q (standard yoki premium)", photos)
		}
		aiCfg, imgCfg, err := config.LoadImages()
		if err != nil {
			return err
		}
		log, _ := zap.NewDevelopment()
		if svc, err = images.New(ctx, aiCfg, imgCfg, log); err != nil {
			return err
		}
		if !svc.Available() {
			return fmt.Errorf("rasm provayderi ulanmagan: .env da OPENAI_API_KEY yoki GEMINI_API_KEY bering")
		}
	}

	// AI maxsus dizayni: namunaviy spetsifikatsiyalar (-theme custom).
	if only == generator.ThemeCustom || only == "" && svc == nil {
		for i, spec := range generator.SampleCustomSpecs() {
			tmp, err := g.Build(ctx, generator.SampleCustomDeck(spec))
			if err != nil {
				return fmt.Errorf("custom %d: %w", i+1, err)
			}
			dst := filepath.Join(out, fmt.Sprintf("namuna-custom-%d.pptx", i+1))
			if err := os.Rename(tmp, dst); err != nil {
				return err
			}
			fmt.Printf("%s\t✨ %s\n", dst, spec.Name)
		}
		if only == generator.ThemeCustom {
			return nil
		}
	}

	for _, t := range generator.Themes() {
		if only != "" && t.ID != only || svc != nil && !t.Photos {
			continue
		}
		deck := generator.SampleDeck(t.ID)
		name := "namuna-" + t.ID
		if language != "" {
			deck.Lang = language
			deck.WorkType = lang.Get(language).Labels.TitlePresentation
			name += "-" + language
		}
		if svc != nil {
			placed, total := addPhotos(ctx, svc, images.Tier(photos), &deck)
			fmt.Fprintf(os.Stderr, "%s: %d/%d ta AI rasm (%s, %s)\n", t.ID, placed, total, photos, svc.Provider(images.Tier(photos)))
			name += "-" + photos
		}
		tmp, err := g.Build(ctx, deck)
		if err != nil {
			return fmt.Errorf("%s: %w", t.ID, err)
		}
		dst := filepath.Join(out, name+".pptx")
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
		fmt.Printf("%s\t%s\n", dst, t.Name)
	}
	return nil
}

// addPhotos namunaviy rasmlarni haqiqiy AI rasmlar bilan almashtiradi
// (bot bilan bir xil: titul uchun yotiq, yon panel uchun tik, tepadagi
// banner uchun yotiq rasm).
func addPhotos(ctx context.Context, svc *images.Service, tier images.Tier, d *generator.Deck) (placed, total int) {
	cover, descs := generator.SamplePhotoPrompts(d.Theme)
	reqs := []images.Request{{Prompt: generator.PhotoPrompt(d.Theme, cover), Shape: images.Landscape}}
	targets := []int{-1}
	d.CoverImage = nil
	for i := range d.Slides {
		d.Slides[i].Image = nil
		if i < len(descs) && generator.SlideTakesPhoto(d.Theme, d.Slides[i]) {
			slideShape := images.Portrait
			if generator.SlidePhotoWide(d.Theme, d.Slides[i]) {
				slideShape = images.Landscape
			}
			reqs = append(reqs, images.Request{Prompt: generator.PhotoPrompt(d.Theme, descs[i]), Shape: slideShape})
			targets = append(targets, i)
		}
	}
	for k, r := range svc.Generate(ctx, tier, reqs, nil) {
		pic, err := generator.NewPicture(r.Data)
		if err != nil {
			continue
		}
		if targets[k] < 0 {
			d.CoverImage = pic
		} else {
			d.Slides[targets[k]].Image = pic
		}
		placed++
	}
	return placed, len(reqs)
}

// runDocs har bir Word dizaynida namunaviy hujjat yaratadi: tayyor dizaynlar
// (namuna-<id>.docx) va rasmli maxsus dizayn namunalari (namuna-doc-custom-N.docx).
func runDocs(out string) error {
	if err := os.MkdirAll(out, 0o750); err != nil {
		return err
	}
	g, err := generator.NewDOCX(out, "O'ZBEKISTON RESPUBLIKASI OLIY TA'LIM, FAN VA INNOVATSIYALAR VAZIRLIGI", zap.NewNop())
	if err != nil {
		return err
	}
	save := func(p generator.Paper, name, label string) error {
		tmp, err := g.Build(context.Background(), p)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		dst := filepath.Join(out, name+".docx")
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
		fmt.Printf("%s\t%s\n", dst, label)
		return nil
	}
	for _, t := range generator.DocThemes() {
		if err := save(generator.SamplePaper(t.ID, nil, false), "namuna-"+t.ID, t.Emoji+" "+t.Name); err != nil {
			return err
		}
	}
	for i, spec := range generator.SampleDocSpecs() {
		if err := save(generator.SamplePaper("", &spec, true), fmt.Sprintf("namuna-doc-custom-%d", i+1), "✨ "+spec.Name); err != nil {
			return err
		}
	}
	return nil
}
