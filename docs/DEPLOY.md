# VPS ga avtomatik deploy (CI/CD)

`main` branchga har bir push'da GitHub Actions ([.github/workflows/deploy.yml](../.github/workflows/deploy.yml)):

1. **Testlar** — `go vet` va `go test`. Xato bo'lsa, deploy qilinmaydi.
2. **Docker image** — GitHub serverida yig'iladi va `ghcr.io/<owner>/<repo>:<commit>` ga yuklanadi
   (VPS da kod kompilyatsiya qilinmaydi, shuning uchun kichik VPS ham yetadi).
3. **Deploy** — SSH orqali VPS ga ulanadi, yangi image'ni yuklaydi va botni qayta ishga tushiradi
   ([deploy/remote-deploy.sh](../deploy/remote-deploy.sh)). Bot 25 soniya ichida qulasa —
   **avtomatik oldingi versiyaga qaytariladi** va Actions'da xato ko'rsatiladi.

Pull request'larda faqat testlar ishlaydi.

---

## 1. VPS ni tayyorlash (bir marta)

SSH orqali VPS ga kiring (`ssh root@VPS_IP`) va:

```bash
# Docker + docker compose plagini
curl -fsSL https://get.docker.com | sh
docker compose version   # tekshirish

# Loyiha papkasi
mkdir -p /opt/document_creator
```

> Root emas, alohida foydalanuvchi ishlatmoqchi bo'lsangiz:
> `adduser deploy && usermod -aG docker deploy && chown deploy:deploy /opt/document_creator`
> va quyida `root` o'rniga `deploy` yozing.

### `.env` faylini joylashtirish

`.env` gitda yo'q (maxfiy), uni VPS ga qo'lda nusxalang — lokal kompyuterdan:

```bash
scp .env root@VPS_IP:/opt/document_creator/.env
```

Production uchun tekshiring: `APP_ENV=production`, kuchli `DB_PASSWORD`.
`DB_HOST`/`DB_PORT` ni o'zgartirish shart emas — compose ularni o'zi `postgres:5432` ga o'rnatadi.

## 2. Deploy uchun SSH kalit (bir marta)

Lokal kompyuterda GitHub uchun **alohida** kalit yarating (parolsiz):

```bash
ssh-keygen -t ed25519 -f ~/.ssh/document_creator_deploy -N "" -C "github-actions-deploy"
ssh-copy-id -i ~/.ssh/document_creator_deploy.pub root@VPS_IP

# Ishlashini tekshirish:
ssh -i ~/.ssh/document_creator_deploy root@VPS_IP "docker ps"

# Server kalitini olish (VPS_KNOWN_HOSTS uchun):
ssh-keyscan VPS_IP
```

## 3. GitHub secretlari (bir marta)

Repo → **Settings → Secrets and variables → Actions → New repository secret**:

| Secret | Qiymat |
|---|---|
| `VPS_HOST` | VPS IP manzili |
| `VPS_USER` | `root` (yoki `deploy`) |
| `VPS_SSH_KEY` | `~/.ssh/document_creator_deploy` faylining **to'liq** matni (`-----BEGIN ... END-----` bilan) |
| `VPS_KNOWN_HOSTS` | `ssh-keyscan VPS_IP` natijasi (ixtiyoriy, lekin tavsiya etiladi) |
| `VPS_PORT` | SSH porti, faqat 22 bo'lmasa |

Papka `/opt/document_creator` dan boshqa bo'lsa — **Variables** bo'limida `DEPLOY_PATH` yarating.

## 4. Ishga tushirish

`main` ga push qiling (yoki Actions → CI/CD → **Run workflow**). Jarayonni **Actions** bo'limida kuzating.
Birinchi deployda postgres ham yaratiladi.

> ⚠️ Bot VPS da ishga tushgach, uni lokal kompyuterda **to'xtating** — bitta token bilan ikki nusxa
> ishlasa, Telegram `409 Conflict` xatosini beradi.

---

## VPS dagi foydali buyruqlar

```bash
cd /opt/document_creator

docker compose ps                 # holat
docker compose logs -f bot        # loglar
docker compose restart bot        # qayta ishga tushirish

# Qo'lda oldingi versiyaga qaytish:
docker image ls | grep ghcr.io    # mavjud versiyalar (TAG = commit)
docker tag <IMAGE_ID> telegram-presentation-bot:latest
docker compose up -d --no-build --force-recreate bot
```

Eslatmalar:

- VPS da `docker compose up --build` **ishlatmang** — u yerda manba kod yo'q, image GHCR dan keladi.
- Baza ma'lumotlari `document_creator_pgdata` volumeda saqlanadi, deploylarda o'chmaydi.
- Bot long polling ishlatadi — VPS da SSH dan boshqa port ochish shart emas.
- VPS da joriy va bitta oldingi image saqlanadi, eskilari avtomatik o'chiriladi.
- Image `linux/amd64` uchun yig'iladi (Hostkey'ning oddiy VPS lari shu arxitekturada).
