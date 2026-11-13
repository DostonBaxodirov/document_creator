# syntax=docker/dockerfile:1

# =============================================================================
# 1-bosqich: build — statik Go binary yig'iladi
# =============================================================================
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Avval faqat modul fayllari: kod o'zgarganda bog'liqliklar keshdan olinadi.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd ./cmd
COPY internal ./internal

# CGO o'chirilgan — binary hech qanday tizim kutubxonasiga bog'liq emas.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/bot ./cmd/bot

# =============================================================================
# 2-bosqich: runtime — minimal va xavfsiz image
# =============================================================================
FROM alpine:3.22

# ca-certificates — Telegram/Gemini/OpenAI bilan HTTPS uchun,
# tzdata — to'g'ri vaqt mintaqasi uchun.
RUN apk add --no-cache ca-certificates tzdata \
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

ENTRYPOINT ["/app/bot"]
