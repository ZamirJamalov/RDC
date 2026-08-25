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

Məcburi (yalnız 3 dənə) — `deploy/rdc.env` faylında (laptopda):

```bash
DB_HOST=10.0.0.5
DB_USER=rdc_user
DB_PASSWORD=********
```

Vacib optional-lar:

```bash
DB_PORT=1433                 # default: 1433
DB_NAME=RDC                  # default: RDC
SERVER_ADDR=:8000            # default: :8000
LOG_LEVEL=info               # TƏK DƏYƏR — minimum səviyyə (bax: aşağıdakı cədvəl)
```

### LOG_LEVEL — necə işləyir?

**Yalnız bir dəyər** təyin edilir — bu, **minimum səviyyədir** (threshold):
seçilən səviyyə və ondan AĞIR olanlar loglanır. Hamısını eyni anda yazmaq
mümkün deyil və lazım da deyil:

| LOG_LEVEL dəyəri | Nə loglanır | İstifadə halı |
|---|---|---|
| `debug` | DEBUG + INFO + WARN + ERROR (hər şey) | troubleshoot, asset request-ləri baxmaq |
| `info` | INFO + WARN + ERROR (**default**) | gündəlik istifadə |
| `warn` | yalnız WARN + ERROR | yalnız problemlər maraqlandıranda |
| `error` | yalnız ERROR | minimal rejim |

Nümunə: `LOG_LEVEL=info` olanda WARN və ERROR da loglanır (itmir) — DEBUG
iltisafi yalnız `debug` seçəndə görünür. Yanlış dəyər yazsanız (məs.
`LOG_LEVEL=info,debug`) — parse alınmur, **default `info`** qaytarılır.

Qeyd: HTTP asset request-ləri (css/js/png) DEBUG səviyyəsindədir (PR #292) —
onları Loki-də görmək üçün `LOG_LEVEL=debug` lazımdır.

Qeyd: `MIGRATIONS_DROP_RECREATE` artıq default **`false`**-dur (PR #294, fail-closed)
— prod-da heç nə yazmasanız da data güvəndədir. Yalnız **dev-də** DB-ni
sıfırdan qurmaq istəyəndə açıq şəkildə `MIGRATIONS_DROP_RECREATE=true` yazın
(cədvəllər DROP olunur və yenidən seed olunur — bütün data silinir).

### Bütün parametrlər — laptopda deploy/rdc.env (PR #298)

**Parollar serverdə saxlanmır.** Bütün parametrlər (DB, AZMK, MyGov, LW, OTP,
video və s.) laptopda bir faylda saxlanılır: `deploy/rdc.env`
(şablon: `deploy/env.example`; `rdc.env` .gitignore-dadır — git-ə getmir).

```bash
cp deploy/env.example deploy/rdc.env     # laptopda, bir dəfə
nano deploy/rdc.env                      # parolları doldur (DB_PASSWORD, AZMK_PASSWORD)
```

App başlanarkən `run-remote.sh` bu faylı **şifrələnmiş ssh kanalı** ilə
birbaşa prosesin env-inə ötürür:

- serverdə **heç bir fayla yazılmır** (disk təmiz qalır)
- `bash_history`-yə düşmür
- yalnız app işləyən müddətdə prosesin RAM-ında yaşayır

## 4. İşə salma — 1 əmr (PR #298)

Laptopdan:

```bash
bash deploy/run-remote.sh user@server
```

Script: köhnə prosesi dayandırır → `rdc.env`-i ssh stdin ilə proses env-inə
ötürür → app-ı `rdc` istifadəçisi ilə `nohup`-la başladır (terminal bağlansa
da yaşayır) → logları `/opt/rdc/monitoring/app.log`-a yazır (Loki zənciri).

Gündəlik əmrlər:

| Əməliyyat | Əmr |
|---|---|
| Başlat / restart | `bash deploy/run-remote.sh user@server` |
| Status | `ssh user@server 'pgrep -x rdc; tail -20 /opt/rdc/monitoring/app.log'` |
| Dayandır | `ssh user@server 'pkill -x rdc'` |
| Parametr dəyiş | laptopda `deploy/rdc.env` redaktə et → restart əmri |

Serverdə əl ilə (alternativ):

```bash
cd /opt/rdc
set -a; . /path/to/env-fayl; set +a        # env-lər (export kimi)
nohup ./rdc >> monitoring/app.log 2>&1 &
```

⚠️ Diqqət: `export KEY=parol` şəklində əl ilə yazsanız parol serverdə
`~/.bash_history`-yə düşür — ona görə laptopdan `run-remote.sh` üstünlük təşkil edir.

Startup-da migration-lar avtomatik tətbiq olunur (binary-nin içindən, diskdən
oxunmur). Yeni migration əlavə etdikdə binary yenidən build olunmalıdır.

## 5. Loglar — serverdə Loki-yə necə düşür?

App bütün logları **stdout**-a yazır (JSON formatda). Loki-yə düşməsi üçün
zəncir belədir:

```
./rdc  →  stdout  →  app.log (yönləndirmə)  →  Promtail (oxuyur)  →  Loki  →  Grafana
```
Yəni executable tək başına çatmaz — 2 şey lazımdır:

1. **stdout → app.log yönləndirməsi** (`run-remote.sh` bunu özü edir;
   alternativlər aşağıda)
2. **Monitorinq stack-i** (loki + promtail + grafana): `docker compose up -d`
   (bax: `MONITORING.md`)

### Variant A — sadə (manual run)

Monitorinq faylları (docker-compose.yml, promtail-config.yml) serverdə
hansısa qovluqda olsun (məs. `/opt/rdc/monitoring/`) və app həmin qovluqdan
işə salınsın — promtail `./app.log`-u oxuyur:

```bash
cd /opt/rdc/monitoring
docker compose up -d                    # loki + promtail + grafana qalxır

cd /opt/rdc
./rdc 2>&1 | tee /opt/rdc/monitoring/app.log   # stdout + app.log
```

`2>&1` — stderr-i (panic-lər, driver mesajları) stdout-a qoşur;
`tee` — həm ekranda göstərir, həm fayla yazır.

### Variant B — systemd (opsional)

systemd stdout-u avtomatik fayla yönləndirir — `tee` lazım deyil.
PR #298-dən sonra əsas yol `run-remote.sh`-dir; systemd yalnız avtomatik
restart istəyəndə lazımdır (placeholder-ləri əl ilə doldurun).

```ini
[Service]
ExecStart=/opt/rdc/rdc
StandardOutput=append:/opt/rdc/monitoring/app.log
StandardError=append:/opt/rdc/monitoring/app.log
```

**Quraşdırma (install.sh):** istifadəçi, qovluqlar, monitorinq stack-i (və
opsional systemd unit) bir əmrlə (PR #298-dən sonra DB parolu soruşulmur):

```bash
# serverdə, source/ qovluğunda (rdc binary orada olmalıdır):
sudo env GRAFANA_ADMIN_PASSWORD=güclüparol bash deploy/install.sh
```

Şablon: `deploy/rdc.service` (DB placeholder-lərini systemd yolu üçün əl ilə
doldurun — app-ın özü run-remote.sh ilə başladılır).

### Promtail-in faylı tapması üçün

`promtail-config.yml`-də `__path__: /var/log/app/app.log` yazılıb və bu,
docker-compose-da `./:/var/log/app:ro` mountu ilə **compose faylının
yanındakı** `app.log`-a uyğun gəlir. Əgər app.log başqa yerdədirsə (məs.
`/var/log/rdc/`), compose-da volume yolunu dəyişin:

```yaml
    volumes:
      - /var/log/rdc:/var/log/app:ro
```

### Yoxlama

```bash
curl -s "http://localhost:3100/ready"          # Loki hazırdır?
# Grafana: http://<server>:3001 → Explore → {job="go-app"}
```

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
Environment=LOG_LEVEL=info
# Loki-yə log düşməsi üçün (bax: bölmə 5):
StandardOutput=append:/opt/rdc/monitoring/app.log
StandardError=append:/opt/rdc/monitoring/app.log
Restart=on-failure
User=rdc

[Install]
WantedBy=multi-user.target
```

## Nə binary-nin içində DEYİL (xarici asılılıqlar)

- SQL Server (DB) — şəbəkə üzərindən
- AZMK / LW / MyGov / SIMA / OTP / Video extern servisləri — env ilə qurulur
  (`*_BASE_URL`, `*_API_KEY` və s.; default mock rejimləridir)
- Loki/Promtail/Grafana monitoring stack-i (bax: `MONITORING.md` və bölmə 5) —
  loglar stdout-a yazılır, `./rdc 2>&1 | tee app.log` və ya systemd redirect
  ilə fayla yönəldilir
