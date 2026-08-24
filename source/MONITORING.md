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
| Loki datası | `loki-data:/tmp/loki` volume | Restart-da loglar itmesin. |
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

## 6. Gələcək üçün (bu PR-da deyil)

- App konteynerləşdiriləndə promtail `docker_sd_configs`-ə keçə bilər — `tee`
  lazım olmur, container stdout birbaşa oxunur.
- Prod-da Loki retention (`retention_period`) və S3 backend konfiqurasiyası.
