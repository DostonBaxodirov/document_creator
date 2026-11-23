#!/usr/bin/env bash
# VPS da ishlaydi — GitHub Actions deploy jobi SSH orqali chaqiradi.
#
# Kirish:
#   IMAGE      — yangi image, masalan ghcr.io/owner/repo:<commit-sha>
#   GHCR_USER  — GHCR foydalanuvchisi
#   stdin      — GHCR token (docker login uchun)
#
# Ish tartibi: image yuklanadi → lokal "telegram-presentation-bot:latest" tegi
# unga ko'chiriladi → bot konteyneri qayta yaratiladi → bot ishlayotgani
# tekshiriladi. Bot ishga tushmasa, oldingi versiyaga avtomatik qaytariladi.
set -euo pipefail

: "${IMAGE:?IMAGE berilmagan}"
: "${GHCR_USER:?GHCR_USER berilmagan}"

LOCAL_TAG="telegram-presentation-bot:latest" # docker-compose.yml dagi bot image nomi
SERVICE="bot"
CONTAINER="presentation-bot"
HEALTH_WAIT="${HEALTH_WAIT:-60}" # bot shu vaqt ichida "healthy" bo'lishi kerak (soniya)

cd "$(dirname "$0")"

if [ ! -f .env ]; then
  echo "XATO: $(pwd)/.env topilmadi. Sozlamalarni GitHub environmentiga kiriting yoki .env ni qo'lda joylashtiring (docs/DEPLOY.md)." >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "XATO: docker compose o'rnatilmagan." >&2
  exit 1
fi

echo "==> GHCR ga kirish"
docker login ghcr.io -u "$GHCR_USER" --password-stdin >/dev/null
trap 'docker logout ghcr.io >/dev/null 2>&1 || true' EXIT

echo "==> Image yuklanmoqda: $IMAGE"
docker pull "$IMAGE"

PREV_ID="$(docker image inspect -f '{{.Id}}' "$LOCAL_TAG" 2>/dev/null || true)"
NEW_ID="$(docker image inspect -f '{{.Id}}' "$IMAGE")"
docker tag "$IMAGE" "$LOCAL_TAG"

# bot_is_up konteyner HEALTHCHECK natijasini (Dockerfile) ko'pi bilan
# HEALTH_WAIT soniya kutadi: "healthy" — muvaffaqiyat; "unhealthy", konteyner
# to'xtagani yoki qayta ishga tushgani (RestartCount > 0) — darhol xato.
bot_is_up() {
  echo "==> Bot sog'lig'i kutilmoqda (ko'pi bilan ${HEALTH_WAIT}s)"
  local start=$SECONDS status health restarts
  while :; do
    status="$(docker inspect -f '{{.State.Status}}' "$CONTAINER" 2>/dev/null || echo missing)"
    health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CONTAINER" 2>/dev/null || echo missing)"
    restarts="$(docker inspect -f '{{.RestartCount}}' "$CONTAINER" 2>/dev/null || echo 0)"

    if [ "$restarts" != "0" ] || [ "$health" = "unhealthy" ]; then
      break
    fi
    case "$status" in
      exited | dead | restarting | missing) break ;;
    esac
    if [ "$health" = "healthy" ]; then
      echo "    holat: $status, sog'lik: $health ($((SECONDS - start))s)"
      return 0
    fi
    if [ $((SECONDS - start)) -ge "$HEALTH_WAIT" ]; then
      echo "    ${HEALTH_WAIT}s ichida \"healthy\" bo'lmadi"
      # Image'da HEALTHCHECK bo'lmasa — eski tekshiruv: konteyner shu vaqt davomida qulamagan bo'lsin.
      if [ "$health" = "none" ] && [ "$status" = "running" ]; then
        echo "    HEALTHCHECK yo'q, konteyner ishlayapti"
        return 0
      fi
      break
    fi
    sleep 2
  done
  echo "    holat: $status, sog'lik: $health, qayta ishga tushishlar: $restarts"
  return 1
}

echo "==> Konteynerlar yangilanmoqda"
# --force-recreate faqat botga ta'sir qiladi; postgres faqat sozlamasi o'zgarsa qayta yaratiladi.
if docker compose up -d --no-build --remove-orphans --force-recreate "$SERVICE" && bot_is_up; then
  echo "==> Deploy muvaffaqiyatli: $IMAGE"
  docker compose logs --tail=20 "$SERVICE" || true

  # Eski versiyalarni tozalash: joriy va oldingisi (qaytish uchun) qoldiriladi.
  repo="${IMAGE%:*}"
  docker image ls "$repo" --no-trunc --format '{{.Repository}}:{{.Tag}} {{.ID}}' |
    while read -r ref id; do
      if [ "$id" != "$NEW_ID" ] && [ "$id" != "$PREV_ID" ]; then
        docker rmi "$ref" >/dev/null 2>&1 || true
      fi
    done || true
  docker image prune -f >/dev/null 2>&1 || true
  exit 0
fi

echo "XATO: bot ishga tushmadi. Oxirgi loglar:" >&2
docker compose logs --tail=100 "$SERVICE" >&2 || true

if [ -n "$PREV_ID" ] && [ "$PREV_ID" != "$NEW_ID" ]; then
  echo "==> Oldingi versiyaga qaytarilmoqda" >&2
  docker tag "$PREV_ID" "$LOCAL_TAG"
  docker compose up -d --no-build --force-recreate "$SERVICE"
fi
exit 1
