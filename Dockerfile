# syntax=docker/dockerfile:1

# =============================================================================
# 1-bosqich: build — statik Go binary yig'iladi
# =============================================================================
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Avval faqat modul fayllari: kod o'zgarganda bog'liqliklar keshdan olinadi.
# Modullar BuildKit kesh mount'ida saqlanadi (go.sum o'zgarsa ham qayta
# yuklanmaydi); yuklanganlari go.sum bo'yicha tekshiriladi, shu sabab
# alohida `go mod verify` kerak emas.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd ./cmd
COPY internal ./internal

# CGO o'chirilgan — binary hech qanday tizim kutubxonasiga bog'liq emas.
# Go build keshi (/root/.cache/go-build) buildlar orasida saqlanadi: kod
# o'zgarganda faqat o'zgargan paketlar qayta kompilyatsiya qilinadi.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/bot ./cmd/bot

# =============================================================================
# 2-bosqich: runtime — minimal va xavfsiz image
# =============================================================================
FROM alpine:3.22

# ca-certificates — Telegram/Gemini/OpenAI bilan HTTPS uchun,
# tzdata — to'g'ri vaqt mintaqasi uchun,
# postgresql17-client — kunlik backup uchun pg_dump (versiyasi docker-compose.yml
# dagi postgres versiyasidan past bo'lmasligi kerak).
RUN apk add --no-cache ca-certificates tzdata postgresql17-client \
    && addgroup -S app \
    && adduser -S -G app -H -h /app app

WORKDIR /app

COPY --from=builder /out/bot /app/bot
COPY templates /app/templates

ENV APP_ENV=production \
    TZ=Asia/Tashkent \
    TEMPLATE_PATH=/app/templates/template.pptx \
    OUTPUT_DIR=/tmp/presentations

# Bot root bo'lmagan foydalanuvchi nomidan ishlaydi.
USER app

# Sog'lik tekshiruvi HTTP serversiz: bot baza javob bersa har 15 soniyada
# /tmp/healthy faylini yangilaydi (HEALTH_FILE); fayl oxirgi 1 daqiqada
# yangilanmagan bo'lsa — konteyner "unhealthy". busybox find -mmin ni qo'llaydi,
# /tmp esa app foydalanuvchisi uchun yoziladigan.
HEALTHCHECK --interval=20s --timeout=3s --start-period=30s --retries=3 \
    CMD find /tmp/healthy -mmin -1 | grep -q .

ENTRYPOINT ["/app/bot"]
