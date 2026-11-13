// Command preview har bir dizayn mavzusida namunaviy taqdimot yaratadi
// (AI va Telegram'siz). Dizaynni o'zgartirgandan keyin natijani tez ko'rish
// uchun ishlatiladi:
//
//	go run ./cmd/preview -out ./previews
//	go run ./cmd/preview -out ./previews -theme zamonaviy
//
// Har bir qatorda fayl yo'li va mavzu nomi (TAB bilan) chiqariladi.
// Slaydlarni rasmga aylantirish (LibreOffice o'rnatilgan bo'lsa):
//
//	soffice --headless --convert-to pdf --outdir ./previews ./previews/*.pptx
//
// Bot dizayn tanlashda ko'rsatadigan templates/designs.png rasmi
// scripts/design_preview.py bilan shu vosita orqali yangilanadi.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"telegram-presentation-bot/internal/generator"
)

func main() {
	tpl := flag.String("template", "templates/template.pptx", "asosiy .pptx paket")
	out := flag.String("out", "previews", "natija papkasi")
	only := flag.String("theme", "", "faqat shu mavzu (bo'sh — barchasi)")
	flag.Parse()

	if err := run(*tpl, *out, *only); err != nil {
		fmt.Fprintln(os.Stderr, "preview:", err)
		os.Exit(1)
	}
}

func run(tpl, out, only string) error {
	if err := os.MkdirAll(out, 0o750); err != nil {
		return err
	}
	g, err := generator.NewPPTX(tpl, out, zap.NewNop())
	if err != nil {
		return err
	}
	for _, t := range generator.Themes() {
		if only != "" && t.ID != only {
			continue
		}
		tmp, err := g.Build(context.Background(), generator.SampleDeck(t.ID))
		if err != nil {
			return fmt.Errorf("%s: %w", t.ID, err)
		}
		dst := filepath.Join(out, "namuna-"+t.ID+".pptx")
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
		fmt.Printf("%s\t%s\n", dst, t.Name)
	}
	return nil
}
