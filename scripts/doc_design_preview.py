#!/usr/bin/env python3
"""Word dizaynlari ko'rinishi rasmini (templates/doc_designs.png) yangilaydi.

Bot mustaqil ish va referat dizaynini tanlashda shu rasmni yuboradi. Word
dizaynlari (internal/generator/docdesign.go) o'zgartirilgandan keyin loyiha
ildizidan ishga tushiring:

    python3 scripts/doc_design_preview.py

Talablar: Go, LibreOffice (soffice), poppler-utils (pdftoppm), Pillow.
Har bir tayyor dizayn uchun titul varaq va matn sahifasi olinadi; oxirgi
katakda — rasmli maxsus dizayn namunasi.
"""

import os
import subprocess
import sys
import tempfile

from PIL import Image, ImageDraw, ImageFont

OUT = os.path.join("templates", "doc_designs.png")
ACCENTS = {  # raqam doirasi rangi (dizayn rangi)
    "doc_classic": "#3A3F4B",
    "doc_frame": "#111111",
    "doc_modern": "#1F4E79",
    "doc_academic": "#7B1E1E",
    "doc_elegant": "#0F5257",
}
CUSTOM_SAMPLE = "namuna-doc-custom-1"  # maxsus dizayn namunasi (ramkali, rasmli)
CONTENT_PAGE = 3  # kirish va 1-bob (maxsus dizaynda — rasm bilan)
COLS = 3
BG = "#151922"
PAGE_W, GAP, MARGIN, LABEL_H = 380, 14, 34, 66
PAGE_H = round(PAGE_W * 297 / 210)


def run(*cmd, **kw):
    return subprocess.run(cmd, check=True, capture_output=True, text=True, **kw).stdout


def font(size, spec="Lato:bold"):
    for s in (spec, "DejaVu Sans:bold"):
        try:
            path = run("fc-match", "-f", "%{file}", s).strip()
            if path:
                return ImageFont.truetype(path, size)
        except (subprocess.CalledProcessError, OSError):
            continue
    return ImageFont.load_default()


def render(tmp):
    lines = run("go", "run", "./cmd/preview", "-out", tmp, "-docs").strip().splitlines()
    docs = [line.split("\t") for line in lines]
    docs = [d for d in docs if "doc-custom" not in d[0] or CUSTOM_SAMPLE in d[0]]
    run("soffice", f"-env:UserInstallation=file://{tmp}/lo", "--headless",
        "--convert-to", "pdf", "--outdir", tmp, *[d[0] for d in docs])
    result = []
    for path, label in docs:
        key = os.path.basename(path)[len("namuna-"):-len(".docx")]
        pdf = path[:-len(".docx")] + ".pdf"
        pages = []
        for n in (1, CONTENT_PAGE):
            prefix = os.path.join(tmp, f"{key}-{n}")
            run("pdftoppm", "-r", "110", "-png", "-singlefile", "-f", str(n), "-l", str(n), pdf, prefix)
            pages.append(Image.open(prefix + ".png").convert("RGB"))
        # Emoji o'rniga faqat nom (shriftlarda emoji bo'lmasligi mumkin).
        name = label.split(" ", 1)[1] if " " in label else label
        result.append((key, name, pages))
    return result


def compose(docs):
    cell_w = PAGE_W * 2 + GAP
    cell_h = LABEL_H + PAGE_H
    rows = (len(docs) + COLS - 1) // COLS
    W = MARGIN * (COLS + 1) + cell_w * COLS
    H = MARGIN * (rows + 1) + cell_h * rows
    img = Image.new("RGB", (W, H), BG)
    draw = ImageDraw.Draw(img)
    f_num, f_name, f_star = font(28), font(36), font(30, "DejaVu Sans:bold")

    number = 0
    for i, (key, name, pages) in enumerate(docs):
        x = MARGIN + (i % COLS) * (cell_w + MARGIN)
        y = MARGIN + (i // COLS) * (cell_h + MARGIN)
        r = 24
        cy = y + LABEL_H // 2 - 4
        custom = key.startswith("doc-custom")
        draw.ellipse((x, cy - r, x + 2 * r, cy + r), fill="#C9A227" if custom else ACCENTS.get(key, "#888888"))
        if custom:
            draw.text((x + r, cy), "★", font=f_star, fill="#FFFFFF", anchor="mm")
            name = "Maxsus dizayn (namuna)"
        else:
            number += 1
            draw.text((x + r, cy), str(number), font=f_num, fill="#FFFFFF", anchor="mm")
        draw.text((x + 2 * r + 16, cy), name, font=f_name, fill="#FFFFFF", anchor="lm")

        y += LABEL_H
        for j, page in enumerate(pages):
            img.paste(page.resize((PAGE_W, PAGE_H), Image.LANCZOS), (x + j * (PAGE_W + GAP), y))
    return img


def main():
    if not os.path.exists("go.mod"):
        sys.exit("Loyiha ildizidan ishga tushiring: python3 scripts/doc_design_preview.py")
    with tempfile.TemporaryDirectory() as tmp:
        img = compose(render(tmp))
    img.save(OUT, optimize=True)
    print(f"{OUT}: {img.size[0]}x{img.size[1]}")


if __name__ == "__main__":
    main()
