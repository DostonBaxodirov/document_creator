#!/usr/bin/env python3
"""Dizayn mavzulari ko'rinishi rasmini (templates/designs.png) yangilaydi.

Bot taqdimot dizaynini tanlashda shu rasmni yuboradi. Mavzular (themes.go)
o'zgartirilgandan keyin loyiha ildizidan ishga tushiring:

    python3 scripts/design_preview.py
    python3 scripts/design_preview.py --photos standard   # rasmli dizaynlar haqiqiy AI rasmlar bilan

Talablar: Go, LibreOffice (soffice), poppler-utils (pdftoppm), Pillow.
Har bir mavzu uchun titul slayd va ikkita mazmunli slayd olinadi — namunaviy
taqdimotda (generator.SampleDeck) ular dizaynning imzo maketlari.
--photos berilsa, rasmli dizaynlar .env dagi kalitlar bilan haqiqiy AI
rasmlarda chiziladi (pullik: ~20 ta rasm); aks holda namunaviy gradientlar.
"""

import argparse
import os
import subprocess
import sys
import tempfile

from PIL import Image, ImageDraw, ImageFont

OUT = os.path.join("templates", "designs.png")
CONTENT_SLIDES = (3, 5)  # dizaynning imzo maketlari (rasmli dizaynlarda — rasmli kartochkalar)
ACCENTS = {  # raqam doirasi rangi
    "akademik": "#C9A227",
    "zamonaviy": "#7C3AED",
    "minimal": "#0F766E",
    "texno": "#22D3EE",
    "jurnal": "#D7263D",
    "tabiat": "#2F5D50",
    "okean": "#1CA7A8",
    "shom": "#FF7A45",
    "strategiya": "#6D1A36",
    "retro": "#FF5C8A",
    "tipografika": "#E3000F",
    "bento": "#3B5BDB",
    "doska": "#2F6B55",
    "chizma": "#0E3A73",
    "pastel": "#5B4BC4",
    "panorama": "#F26B1D",
}
COLS = 4  # 16 ta dizayn — 4x4 to'r
BG = "#151922"
CELL_W, COVER_H, GAP, MARGIN, LABEL_H = 760, 428, 12, 32, 64


def run(*cmd, **kw):
    return subprocess.run(cmd, check=True, capture_output=True, text=True, **kw).stdout


def font(size):
    for spec in ("Lato:bold", "DejaVu Sans:bold"):
        try:
            path = run("fc-match", "-f", "%{file}", spec).strip()
            if path:
                return ImageFont.truetype(path, size)
        except (subprocess.CalledProcessError, OSError):
            continue
    return ImageFont.load_default()


def render(tmp, photos=None):
    lines = run("go", "run", "./cmd/preview", "-out", tmp).strip().splitlines()
    decks = [line.split("\t") for line in lines]
    if photos:
        # Rasmli dizaynlar haqiqiy AI rasmlar bilan qayta yaratiladi.
        real = run("go", "run", "./cmd/preview", "-out", tmp, "-photos", photos).strip().splitlines()
        by_name = {name: path for path, name in (line.split("\t") for line in real)}
        decks = [[by_name.get(name, path), name] for path, name in decks]
    run("soffice", f"-env:UserInstallation=file://{tmp}/lo", "--headless",
        "--convert-to", "pdf", "--outdir", tmp, *[d[0] for d in decks])
    result = []
    for path, name in decks:
        theme = os.path.basename(path)[len("namuna-"):-len(".pptx")]
        if photos:
            theme = theme.removesuffix("-" + photos)
        pdf = path[:-len(".pptx")] + ".pdf"
        pages = []
        for n in (1, *CONTENT_SLIDES):
            prefix = os.path.join(tmp, f"{theme}-{n}")
            run("pdftoppm", "-r", "120", "-png", "-singlefile", "-f", str(n), "-l", str(n), pdf, prefix)
            pages.append(Image.open(prefix + ".png").convert("RGB"))
        result.append((theme, name, pages))
    return result


def compose(themes):
    small_w = (CELL_W - GAP) // 2
    small_h = round(small_w * 9 / 16)
    cell_h = LABEL_H + COVER_H + GAP + small_h
    cols = COLS
    rows = (len(themes) + cols - 1) // cols
    W = MARGIN * (cols + 1) + CELL_W * cols
    H = MARGIN * (rows + 1) + cell_h * rows
    img = Image.new("RGB", (W, H), BG)
    draw = ImageDraw.Draw(img)
    f_num, f_name = font(26), font(34)

    for i, (theme, name, pages) in enumerate(themes):
        x = MARGIN + (i % cols) * (CELL_W + MARGIN)
        y = MARGIN + (i // cols) * (cell_h + MARGIN)
        r = 22
        cy = y + LABEL_H // 2 - 4
        draw.ellipse((x, cy - r, x + 2 * r, cy + r), fill=ACCENTS.get(theme, "#888888"))
        num_color = "#0B1120" if theme == "texno" else "#FFFFFF"
        draw.text((x + r, cy), str(i + 1), font=f_num, fill=num_color, anchor="mm")
        draw.text((x + 2 * r + 16, cy), name, font=f_name, fill="#FFFFFF", anchor="lm")

        y += LABEL_H
        img.paste(pages[0].resize((CELL_W, COVER_H), Image.LANCZOS), (x, y))
        y += COVER_H + GAP
        for j, page in enumerate(pages[1:3]):
            img.paste(page.resize((small_w, small_h), Image.LANCZOS), (x + j * (small_w + GAP), y))
    return img


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--photos", choices=("standard", "premium"),
                        help="rasmli dizaynlarni haqiqiy AI rasmlar bilan chizish (pullik)")
    args = parser.parse_args()
    if not os.path.exists("go.mod"):
        sys.exit("Loyiha ildizidan ishga tushiring: python3 scripts/design_preview.py")
    with tempfile.TemporaryDirectory() as tmp:
        img = compose(render(tmp, args.photos))
    img.save(OUT, optimize=True)
    print(f"{OUT}: {img.size[0]}x{img.size[1]}")


if __name__ == "__main__":
    main()
