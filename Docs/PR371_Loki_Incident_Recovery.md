# PR #371 — Loki log itkisi: insident hesabatı və bərpa təlimatı

> Tarix: 2026-09-03 · Əlaqəli PR-lər: #368, #369, #370, #371
> Status: həll olunub. Bu sənəd həm hesabat, həm də gələcək bərpalarda runbook-dur.

## 1. Insident xülasəsi

**Simptom:** `install.sh` + `run-remote.sh` təkrarlarından sonra Loki-də keçmiş
günlərin logları görünmürdü.

**Nə tapıldı:**
- `monitoring_loki-data` volume-u 4.0K — heç vaxt dolmamışdı (`du -sh` sübutu)
- Loki datası konteynerin writable layer-ində qalırdı, hər install-da silinirdi
- Xam `app.log` (3.4MB, Aug 27 06:50-dən) sağ qaldı → tarixçə ondan bərpa olundu

**Nəticə:** Aug 27 08:29-dan bugünə qədər tam bərpa olundu. Yeganə qalıq itki:
Aug 27 06:50–08:29 (~1.5 saat, 168h qaydasına görə əbədi).

## 2. Kök səbəblər

### 2.1 Volume yanlış yola mount olunmuşdu (ƏSAS SƏBƏB)

Loki-nin rəsmi Docker image-i (2.9.4) datasını `/loki`-yə yazır
(mənbə: `cmd/loki/loki-docker-config.yaml` → image-də `/etc/loki/local-config.yaml`,
`path_prefix: /loki`). Bizim compose isə volume-u `/tmp/loki`-yə mount edirdi.

Nəticə: data volume-a deyil, konteynerin writable layer-inə yazılırdı.
`install.sh`-dəki `docker rm -f loki` (PR #307) hər install-da writable layer-i
konteynerlə birlikdə silirdi → Loki hər dəfə boş başlayırdı.

### 2.2 Konfiqdə uyğun olmayan field (crash-loop)

PR #369-da `loki-config.yml`-ə `retention_delay: 0s` əlavə olunmuşdu — bu field
Loki 2.9.4-ün `compactor.Config`-ində YOXDUR (Loki 3.x-ə aiddir). Loki strict
YAML parse ilə başlanğıcda çıxıb crash-loop edirdi.
Diaqnoz: `docker logs loki` → `field retention_delay not found`.

### 2.3 "Data var, amma görünmür" (index gecikməsi — bug deyil)

Backfill-dən sonra ~1 saat sorğular boş qayıdırdı. Səbəb: chunk-lərin flush-u
və boltdb-shipper index cədvəllərinin upload dövrü hələ tamamlanmamışdı.
Loki loglarında `uploading table index_20692...20699` göründükdən sonra
sorğular işlədi. Qeyd: `index/stats` endpointi də index-dən oxuyur — təzə
data üçün `query_range`-dən geri qalır.

### 2.4 Yan itki: 168h qaydası

Loki ingestdə 7 gündən köhnə sətirləri rədd edir (`timestamp too old`).
Backfill gecikdiyindən Aug 27 səhərinin 06:50–08:29 hissəsi qəbul edilmədi.

## 3. Həllər

| PR | Nə etdi |
|---|---|
| #368 | `docker-compose.yml`-ə `name: monitoring` — volume adları qovluqdan asılılıqdan qurtardı |
| #369 | `loki-data:/loki` mount düzəlişi; öz `loki-config.yml` (6 ay retention = 4320h); app.log gündəlik backup cron-u; install.sh + make-setup-prod sinxronu |
| #370 | `retention_delay` sətri silindi (2.9.4 uyğunluğu) |
| #371 | BU SƏNƏD — hesabat + runbook |

## 4. Hazırkı arxitektura — nə harada saxlanılır

| Komponent | Yer | Install təkrarında |
|---|---|---|
| Loki datası (chunks + index + WAL) | `monitoring_loki-data` volume (`/loki`) | QALIR |
| Promtail positions ("harada qaldım") | `monitoring_promtail-data` volume | QALIR |
| Xam tarixçə | `/opt/rdc/monitoring/app.log` | QALIR (install yalnız yoxdursa yaradır) |
| Gündəlik arxivlər | `/opt/rdc/backups/app-*.tar.gz` (30 gün) | QALIR |
| Loki konfiqi (6 ay retention) | `loki-config.yml` → `/etc/loki/rdc-config.yaml` | Yenidən kopyalanır |

## 5. Bərpa runbook-ları

### 5.1 Loki datası itibsə (volume silinibsə) — ən sıx ssenari

```bash
docker rm -f promtail
docker run --rm -v monitoring_promtail-data:/tmp alpine rm -f /tmp/positions.yaml
docker compose -f /opt/rdc/monitoring/docker-compose.yml --project-directory /opt/rdc/monitoring up -d
```

Promtail `app.log`-u başdan oxuyub Loki-yə göndərir (vaxtlar sətirlərdən götürülür).
Əhəmiyyətli: sorğular ~1 saatə qədər boş qaya bilər (chunk flush + index ship
dövrü — bölmə 2.3). Gözləyin, gəririn. 7 gündən köhnə sətirlər rədd olunacaq.

### 5.2 app.log silinibsə / zədələnibsə

```bash
sudo systemctl stop rdc
sudo tar xzf /opt/rdc/backups/app-YYYYMMDD.tar.gz -C /opt/rdc/monitoring/
sudo chown rdc:rdc /opt/rdc/monitoring/app.log
sudo systemctl start rdc
# Loki da boşdursa: bölmə 5.1-i də tətbiq et
```

Hər arxiv O gecəyə qədər TAM tarixçədir (incremental deyil).

### 5.3 Loki konteyneri restart-loop edirsə

1. `docker logs loki --tail 30` → dəqiq xəta mesajına bax
2. Konfiq xətasıdırsa (`field X not found`): `loki-config.yml`-də həmin sahəni sil —
   Loki versiya dəyişəndə sahələr dəyişə bilər (strict YAML parse)
3. İcazə xətasıdırsa: `docker run --rm -v monitoring_loki-data:/d alpine chown -R 10001:10001 /d`

### 5.4 Backup sistemi

- Cron: `/etc/cron.d/rdc-log-backup`, hər gecə 01:00, root
- Hədəf: `/opt/rdc/backups/app-YYYYMMDD.tar.gz`, 30 gündən köhnələr avtomatik silinir
- Manual test: `sudo bash -c 'tar czf /opt/rdc/backups/app-test.tar.gz -C /opt/rdc/monitoring app.log'`

### 5.5 7 gündən köhnə dataya baxmaq

Loki-yə qaytarmaq mümkün deyil (168h qaydası). Arxivdən axtarış:
`zcat /opt/rdc/backups/app-*.tar.gz | tar -xzO app.log 2>/dev/null | grep 'axtarılan'`
və ya arxivi açıb `grep` ilə.

## 6. Diaqnostika cheat-sheet

| Simptom | Komanda | Nə yoxlayırıq |
|---|---|---|
| Loki boş görünür | `docker exec loki du -sh /loki` | Data volume-a yazılırmı (4.0K = problem) |
| Sorğu boş, WAL dolu | `curl .../query_range` vs `index/stats` | Index ship gecikməsi (1 saata qədər normal) |
| Konteyner restarting | `docker logs loki --tail 30` | Konfiq/icazə xətasının dəqiq mesajı |
| Push xətaları | `docker logs promtail --tail 15` | 400 `timestamp too old` = 168h qaydası |
| Volume nə vaxt yaradılıb | `docker volume inspect monitoring_loki-data --format '{{.CreatedAt}}'` | Volume silinib-yenidən yaranıbmı |
| Restart sayı | `docker inspect loki --format '{{.RestartCount}} {{.State.Status}}'` | Crash-loop varmı |

## 7. Qorunma qaydaları

1. `docker volume prune` və `docker compose down -v` — QADAĞA (Loki/Grafana/positions volume-larını silir)
2. Backup-lar eyni diskdədir — ikinci nüsxə üçün offsite sync (gələcək iş)
3. Loki image versiyası dəyişəndə `loki-config.yml` sahələrini yeni versiya ilə yoxla (strict parse)
4. `app.log` rotasiyası yoxdur — limitsiz böyüyür (gələcək iş: logrotate)
