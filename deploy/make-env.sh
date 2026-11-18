#!/usr/bin/env bash
# GitHub environment'dagi variable va secretlardan docker compose uchun .env yaratadi
# (GitHub Actions deploy jobi chaqiradi).
#
# Kirish:  VARS_JSON, SECRETS_JSON — ${{ toJSON(vars) }} va ${{ toJSON(secrets) }}
#          $1 — natija fayli
# Chiqish: stdout — .env ga yozilgan kalitlar ro'yxati (qiymatlar chiqarilmaydi)
# Kod:     0 — yozildi; 3 — TELEGRAM_BOT_TOKEN yo'q, fayl yozilmadi; 1 — xato
#
# Deployga oid kalitlar (VPS_*, DEPLOY_PATH, github_token) .env ga yozilmaydi.
# Bir xil nomli variable va secret bo'lsa, secret ustun.
# Qiymatlar qo'shtirnoqda, \ " $ va yangi qator escape qilinadi — compose ularni
# o'zgarishsiz o'qiydi (apostrof, bo'sh joy, $ belgisi ham saqlanadi).
set -euo pipefail

out="${1:?natija fayli berilmagan}"
umask 077

cfg="$(jq -n '(env.VARS_JSON // "{}" | fromjson) + (env.SECRETS_JSON // "{}" | fromjson)
  | with_entries(select(.key | test("^(VPS_|DEPLOY_PATH$|GITHUB_TOKEN$)"; "i") | not))')"

if ! jq -e 'has("TELEGRAM_BOT_TOKEN")' <<<"$cfg" >/dev/null; then
  exit 3
fi

# Oxiri \ bilan tugagan qiymatni compose to'g'ri o'qiy olmaydi.
bad="$(jq -r 'to_entries[]
  | select((.key | test("^[A-Za-z_][A-Za-z0-9_]*$") | not) or (.value | endswith("\\")))
  | .key' <<<"$cfg")"
if [ -n "$bad" ]; then
  echo "::error::Bu sozlamalar nomi noto'g'ri yoki qiymati \\ bilan tugaydi: $(echo "$bad" | paste -sd, -)" >&2
  exit 1
fi

jq -r 'to_entries | sort_by(.key)[]
  | .value |= (gsub("\\\\"; "\\\\") | gsub("\""; "\\\"") | gsub("\\$"; "$$")
               | gsub("\n"; "\\n") | gsub("\r"; "\\r"))
  | "\(.key)=\"\(.value)\""' <<<"$cfg" > "$out"

jq -r 'keys | join(", ")' <<<"$cfg"
