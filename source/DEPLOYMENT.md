# PR #293 — Deploy: self-contained executable

App binary-si **tam self-contained**-dir — executable faylı tək başına başqa
serverə daşımaq kifayətdir:

| Nə lazımdır? | Haradadır? |
|---|---|
| HTML səhifələr (`web/*.html`) | binary-nin içində (`//go:embed web`) |
| Statik asset-lər (`web/assets/*`, `auth.js`) | binary-nin içində (`//go:embed web`) |
| SQL migration-lar (`migrations/*.sql`) | binary-nin içində (`//go:embed migrations`, **PR #293**) |
| Config | env dəyişənlərindən (fayl tələb olunmur) |

Yalnız **env dəyişənləri** və **SQL Server çıxışı** lazımdır.

## 1. Build

```bash
cd source/
go build -o rdc .
```

## 2. Serverə daşı

```bash
scp rdc user@server:/opt/rdc/
# və ya hər hansı üsulla — YALNIZ bu bir fayl kifayətdir.
# web/, migrations/ qovluqlarını daşımaq LAZIM DEYİL.
```

## 3. Env dəyişənləri

Məcburi (yalnız 3 dənə):

```bash
export DB_HOST=10.0.0.5
export DB_USER=rdc_user
export DB_PASSWORD=********
```

Vacib optional-lar:

```bash
export DB_PORT=1433                 # default: 1433
export DB_NAME=RDC                  # default: RDC
export SERVER_ADDR=":8000"          # default: :8000
export MIGRATIONS_DROP_RECREATE=false   # PROD-DA MÜTLƏQ false! (default true — dev üçün)
export LOG_LEVEL=info               # info | debug | warn | error
```

Qeyd: `MIGRATIONS_DROP_RECREATE` default `true`-dur (dev rejimi — cədvəllər
sıfırdan qurulur). Production-da mütləq `false` edin, əks halda restart-da
bütün data silinir.

## 4. İşə salma

```bash
./rdc
```

Startup-da migration-lar avtomatik tətbiq olunur (binary-nin içindən, diskdən
oxunmur). Yeni migration əlavə etdikdə binary yenidən build olunmalıdır.

## systemd nümunəsi (opsional)

```ini
[Unit]
Description=RDC Loan Application Service
After=network.target

[Service]
ExecStart=/opt/rdc/rdc
Environment=DB_HOST=10.0.0.5
Environment=DB_USER=rdc_user
Environment=DB_PASSWORD=********
Environment=MIGRATIONS_DROP_RECREATE=false
Restart=on-failure
User=rdc

[Install]
WantedBy=multi-user.target
```

## Nə binary-nin içində DEYİL (xarici asılılıqlar)

- SQL Server (DB) — şəbəkə üzərindən
- AZMK / LW / MyGov / SIMA / OTP / Video extern servisləri — env ilə qurulur
  (`*_BASE_URL`, `*_API_KEY` və s.; default mock rejimləridir)
- Loki/Promtail/Grafana monitoring stack-i (bax: `MONITORING.md`) —
  loglar stdout-a yazılır, `./rdc 2>&1 | tee app.log` ilə fayla yönəldilir
