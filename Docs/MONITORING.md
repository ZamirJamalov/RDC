# PR #290 — Log Storage: Loki + Promtail + Grafana

RDC app-in logları **Loki**-də saxlanılır, **Grafana**-da axtarılır/filtrlənir.

```
go run . 2>&1 | tee app.log          ← app JSON logları stdout-a (və app.log-a)
        │
        ▼
   app.log  (source/ qovluğunda, gitignored)
        │
        ▼ (volume mount, read-only)
   Promtail  → JSON parse (level, msg, time) → Loki-yə push
        │
        ▼
   Loki  (docker volume-də saxlanılır — restart-da itmir)
        │
        ▼
   Grafana  http://localhost:3001  (Loki datasource avtomatik provision olunub)
```

App tərəfində **heç bir dəyişiklik lazım deyil** — `main.go` artıq
`slog.NewJSONHandler(os.Stdout, ...)` ilə strukturlaşdırılmış JSON loglayır.

---

## 1. Başlatma

```bash
# source/ qovluğunda:
docker compose up -d          # loki + promtail + grafana qalxır

# App-i log faylı ilə birgə işə sal:
go run . 2>&1 | tee app.log
```

> `2>&1` vacibdir: `slog` stdout-a yazır, amma panic-lər və bəzi driver
> mesajları stderr-ə düşür — hər ikisi app.log-a (və nəticədə Loki-yə) düşsün deyə.

app.log `.gitignore`-dadır — commit olunmur, promtail source/ qovluğunu
mount edib onu oxuyur (fayl yaranmamış olsa belə problem yoxdur — yarananda
avtomatik götürür).

## 2. Grafana-da baxış

1. http://localhost:3001 → login: `admin` / `admin` (dev; prod-da dəyişin)
2. **Explore** → datasource **Loki** (artıq default-dur, əl ilə əlavə etmək lazım deyil)

### Faydalı sorğular (LogQL)

```logql
# bütün app logları
{job="go-app"}

# yalnız xətalar
{job="go-app"} | json | level="ERROR"

# xəbərdarlıqlar + xətalar
{job="go-app"} | json | level=~"WARN|ERROR"

# mesaj üzrə axtarış (regex)
{job="go-app"} | json | msg=~".*application.*"

# konkret müraciət üzrə
{job="go-app"} | json | application_id="123"

# səviyyə üzrə say
sum by (level) (rate({job="go-app"}[5m]))
```

`level` promtail tərəfindən **label** kimi çıxarılır (pipeline_stages), ona
görə `{job="go-app", level="ERROR"}` kimi də filtrləyə bilərsiniz.

### HTTP request logları (PR #292)

Hər HTTP request (HTML səhifə, API, asset) `request_completed` mesajı ilə
loglanır və aşağıdakı sahələri daşıyır:

| Sahə | Məna | Nümunə |
|---|---|---|
| `type` | `page` / `api` / `asset` / `other` | `page` |
| `path` | Hansı səhifə/endpoint | `/dashboard` |
| `ip` | Client IP (XFF varsa ilk dəyər, yoxsa RemoteAddr) | `85.132.55.12` |
| `os` / `browser` / `device` | UserAgent-dən parse | `Windows` / `Chrome` / `desktop` |
| `method`, `status`, `duration_ms` | Standart request məlumatı | `GET`, `200`, `42` |
| `referer` | Hansı səhifədən keçid edilib | `http://localhost:8000/landing` |
| `request_id`, `user_agent`, `host` | Əlavə kontekst | |

Nümunə sorğular:

```logql
# yalnız HTML səhifə keçidləri (hansı səhifə, kim, nə vaxt)
{job="go-app"} | json | type="page"

# konkret IP-dən girişlər
{job="go-app"} | json | ip="85.132.55.12"

# Windows istifadəçilərinin səhifə keçidləri
{job="go-app"} | json | type="page" | os="Windows"

# OS üzrə səhifə baxış sayı (qrafik)
sum by (os) (rate({job="go-app"} | json | type="page" [5m]))

# 5xx-lər (WARN səviyyəsində loglanır)
{job="go-app"} | json | level="WARN"
```

Qeydlər:

- Asset-lər (css/js/png...) **DEBUG** səviyyəsində loglanır — default
  `LOG_LEVEL=info` ilə Loki çirklənmir; lazım olsa `LOG_LEVEL=debug` ilə görünür.
- `ip` sahəsi `X-Forwarded-For` başlığına əsaslanır və spoof ola bilər —
  yalnız müşahidə üçündür, təhlükəsizlik qərarları üçün istifadə edilməməlidir.
- Səhifə siyahısı (`/`, `/login`, `/dashboard`...) `main.go`-dakı
  `cleanURLMap` ilə sinxron saxlanmalıdır.

## 3. Log səviyyəsi

App `LOG_LEVEL` env dəstəkləyir (default: `info`):

```bash
LOG_LEVEL=debug go run . 2>&1 | tee app.log
```

## 4. Texniki qeydlər

| Mövzu | Qərar | Səbəb |
|---|---|---|
| Fayl mount | `./:/var/log/app:ro` (qovluq) | Faylı birbaşa mount etmək təhlükəlidir — app.log yoxdursa Docker qovluq yaradır. Qovluq + glob → fayl sonra yarananda promtail avtomatik görür. |
| Promtail positions | `promtail-data:/tmp` volume | Restart-da pozisiya itmasin — yoxdursa app.log başdan oxunub Loki-yə dublikat düşərdi. |
| Loki datası (PR #369) | `loki-data:/loki` volume | Image default `path_prefix`=/loki-dır; köhnə /tmp/loki mount writable layer-ə yazırdı — recreate-də silinirdi. |
| Loki sorğu limiti (PR #325) | `-query-scheduler.max-outstanding-requests-per-tenant=1000` (compose faylında) | Geniş zaman aralıqlı sorğular parçalanır, hər parça 1 outstanding request sayılır; Loki default-u (100) bizim həcmlə aşılırdı → Grafana-da `429 too many outstanding requests`. Override: `sudo env LOKI_MAX_OUTSTANDING_REQUESTS=5000 bash deploy/install.sh` (detallar: DEPLOYMENT.md bölmə 3). |
| Grafana | `grafana-data` volume + provisioning | Dashboards/datasource-lar qalıcı + Loki datasource avtomatik hazır. |
| Versiyalar | loki/promtail 2.9.4, grafana 11.1.0 | Eyni loki+promtail versiyası (uyğunluq), pinned (təkrarlanan quraşdırma). |
| `tee app.log` vs `tee -a` | hər ikisi işləyir | `tee` (truncate) — hər run təmiz fayl; `-a` — uzun müddətli toplama. Promtail truncation-u düzgün tutur. |

### Portlar

| Xidmət | Port |
|---|---|
| Grafana | http://localhost:3001 |
| Loki | http://localhost:3100 |
| Promtail (internal) | 9080 (expose olunmur) |

## 5. Dayandırma / təmizləmə

```bash
docker compose down              # dayandır (volumelar qalır)
docker compose down -v           # dayandır + Loki logları və Grafana datası silinir
```

## 6. Retention və backup (PR #369)
> Tam insident hesabatı və ətraflı bərpa runbook-u: [Docs/PR371_Loki_Incident_Recovery.md](PR371_Loki_Incident_Recovery.md)

**Loki retention — 6 ay.** Loki öz konfiqi ilə işləyir (`loki-config.yml` —
image-də `/etc/loki/rdc-config.yaml` kimi mount olunur): compactor retention
aktivdir, `limits_config.retention_period: 4320h`. Dəyişmək:

```bash
sudo env LOKI_RETENTION_PERIOD=2160h bash deploy/install.sh
```

**app.log backup.** install.sh `/etc/cron.d/rdc-log-backup` qurur — hər gün
01:00 `app.log` → `/opt/rdc/backups/app-YYYYMMDD.tar.gz` (30 gün saxlanılır).

**Bərpa (Loki datası itəndə):** xam `app.log` hər zaman bərpa mənbəyidir —

```bash
docker rm -f promtail
docker run --rm -v monitoring_promtail-data:/tmp alpine rm -f /tmp/positions.yaml
docker compose -f /opt/rdc/monitoring/docker-compose.yml --project-directory /opt/rdc/monitoring up -d
```

Promtail `app.log`-u başdan oxuyub Loki-yə göndərir (vaxtlar sətirlərdən
götürülür — Grafana-da düzgün tarixlə görünür). Qeyd: Loki 168 saatdan (7 gün)
köhnə nümunələri rədd edir — bərpanı gecikmədən edin; daha köhnə tarixçə üçün
backup arxivindən `app.log`-u bərpa edib eyni addımı təkrarlayın.

**Kök səbəb (PR #369-ya qədər):** Loki image-inin default `path_prefix`-i
`/loki`-dir; compose volume-u yanlışlıqla `/tmp/loki`-yə mount etdiyindən data
volume-a deyil, konteynerin writable layer-inə yazılırdı — hər `docker rm -f
loki` (install.sh-ın PR #307 addımı) bütün datanı silirdi. İndi volume `/loki`-yə
mount olunur və data konteyner recreate-lərində qalır.


## 7. Gələcək üçün (bu PR-da deyil)

- App konteynerləşdiriləndə promtail `docker_sd_configs`-ə keçə bilər — `tee`
  lazım olmur, container stdout birbaşa oxunur.
- S3 backend konfiqurasiyası (retention artıq var — PR #369, 6 ay).
