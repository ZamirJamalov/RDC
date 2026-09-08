# RDC Runbook — servis nasazlığı zamanı nə etməli

> PR #426 · Kim üçün: on-call (Zamir / admin) · Son yeniləmə: 2026-09-08
> Əlaqəli sənədlər: `Docs/DEPLOYMENT.md` (tam deploy izahı), `Docs/MONITORING.md`,
> `Docs/PR371_Loki_Incident_Recovery.md` (Loki bərpası)

**Qısa məlumat:**

- Server: `172.17.1.27` (Ubuntu 24.04, ssh root) · router public IP: `185.161.225.102`
- App: `/opt/rdc/rdc` (istifadəçi `rdc`) · `localhost:8000`-də dinləyir
- Qapı: Caddy `:443` — daxili `https://172.17.1.27` (tam giriş), public `alpul.az`
  (yalnız müştəri səhifələri — dashboard/API 403)
- Loglar: `/opt/rdc/monitoring/app.log` → Loki → Grafana `http://172.17.1.27:3001`

## 0. Sürətli triyaj — simptom → bölmə

| Simptom | Gedilən bölmə |
|---|---|
| Sayt açılmır / bağlanır | §1 (app), §5 (Caddy/cert) |
| 502 Bad Gateway | App yıxılıb → §1 |
| App işləyir, AZMK/SMS/video xətaları | §4 |
| `alpul.az` açılmır, IP ilə işləyir | §5 (DNS/cert) |
| DB xətaları logda / app başlamır | §6 |
| Grafana-da keçmiş loglar yoxdur | §7 |
| Deploy-dan sonra nəsə səhv getdi | §2.4 (rollback) |

## 1. Dayandırma / başlatma / status

| Əməliyyat | Əmr |
|---|---|
| Status | `ssh root@172.17.1.27 'pgrep -x rdc; tail -20 /opt/rdc/monitoring/app.log'` |
| Dayandır | `ssh root@172.17.1.27 'pkill -x rdc'` |
| Başlat / restart | laptopdan: `bash deploy/run-remote.sh root@172.17.1.27` |
| Canlı log | `ssh root@172.17.1.27 'tail -f /opt/rdc/monitoring/app.log'` |

⚠ **Restart yalnız laptopdan olur** — `rdc.env` (DB/AZMK parolları) laptopda durur,
`run-remote.sh` onu ssh kanalı ilə birbaşa proses env-inə ötürür (serverdə fayl yaranmır).

**Dayandırma nə edir:** `pkill` SIGTERM göndərir → graceful shutdown (maks. 30 saniyə —
`main_helpers.go: shutdownTimeout`): gedə-gedə sorğular tamamlanır, DB transaksiyaları
təhlükəsizdir, yeni sorğular rədd olunur. Data pozuntusu riski yoxdur.

**App başlamırsa:** `tail -50 /opt/rdc/monitoring/app.log` — ən çox səbəblər:
DB qoşulma xətası (`rdc.env`-də SQL Server parametrləri), port məşğuldur
(`ss -tlnp | grep 8000`).

## 2. Deploy (yeni PR merge olunduqdan sonra)

### 2.1 Kiçik dəyişiklik (yalnız Go kodu)

```bash
# laptopda (source/ qovluğunda):
GOOS=linux GOARCH=amd64 go build -o rdc .
scp rdc root@172.17.1.27:/root/rdc-new

# serverdə binary yenilənir (işləyən prosesə təsir etmir — Linux köhnə inode-u saxlayır):
ssh root@172.17.1.27 'cp /opt/rdc/rdc /opt/rdc/rdc.bak-$(date +%Y%m%d) && mv /root/rdc-new /opt/rdc/rdc && chown rdc:rdc /opt/rdc/rdc'

# restart:
bash deploy/run-remote.sh root@172.17.1.27
```

⚠ `run-remote.sh` build ETMİR — serverdə duran `/opt/rdc/rdc` binary-ni başladır.
Kod dəyişəndə mütləq əvvəlcə build + copy (yuxarıdakı axın).

### 2.2 Tam quraşdırma (bundle)

```bash
# laptopda:
bash deploy/make-setup-prod.sh                 # → dist/rdc-prod-<commit>.tgz
scp dist/rdc-prod-*.tgz root@172.17.1.27:/root/

# serverdə:
ssh root@172.17.1.27
mkdir -p rdc-setup && tar xzf /root/rdc-prod-*.tgz -C rdc-setup && cd rdc-setup
sudo env GRAFANA_ADMIN_PASSWORD=güclüparol bash setup-prod.sh
rm /root/rdc-prod-*.tgz        # bundle-da PLAINTEXT parollar var — silin!
```

⚠ `rdc.env`-də `MIGRATIONS_DROP_RECREATE=true`-sa bundle QADAĞANDIR (app başlayanda
DB silinər!). Generator bunu avtomatik bloklayır.

### 2.3 Caddyfile yeniləmək

`setup-prod.sh` mövcud Caddyfile-i üzərinə YAZMIR — yalnız 3 halda yazır:
fayl yoxdursa / apt default-u dur / `INSTALL_CADDYFILE_OVERWRITE=1` veriləndə.

Manual yol (PR #425 tətbiqi kimi): backup → yaz → `caddy validate` →
`systemctl reload caddy`. Köhnə backup-lar: `/etc/caddy/Caddyfile.bak-*`.

### 2.4 Rollback (köhnə binaryyə qayıtmaq)

```bash
ssh root@172.17.1.27 'mv /opt/rdc/rdc.bak-<tarix> /opt/rdc/rdc && chown rdc:rdc /opt/rdc/rdc'
bash deploy/run-remote.sh root@172.17.1.27
```

### 2.5 Deploy sonrası yoxlama

```bash
ssh root@172.17.1.27 'pgrep -x rdc && tail -5 /opt/rdc/monitoring/app.log'
curl -skI https://172.17.1.27/landing.html     # HTTP 200 + security header-lər
```

+ brauzerdə dashboard bir də açılıb yoxlanılır.

## 3. Loglar / diaqnoz

- Canlı: `tail -f /opt/rdc/monitoring/app.log`
- Grafana `http://172.17.1.27:3001` → Explore → LogQL:

```logql
{job="go-app"} | json | level="ERROR"                        # son xətalar
{job="go-app"} | json | msg="external_call" | level="ERROR"  # xarici çağırış xətaları
{job="go-app"} | json | service="azmk"                       # AZMK trafiki
{job="go-app"} | json | request_id="<id>"                    # konkret sorğunun tam izi
```

- Loki hazırdır mı (serverdə): `curl -s http://localhost:3100/ready`
- Xarici sorğu/cavablar `msg="external_call"` ilə düşür, parollar `***` maskalanır (PR #304)

## 4. Xarici servis nasazlığı

Servislər: **AZMK/LW** (`web.azmk.az:7077`), **SMS** (`gw.soft-line.az`),
**Video** (`rec.azmk.az:8699`), MyGov, SIMA.

- **Retry avtomatikdir** (PR #420): müvəqqəti xətalarda (5xx/timeout) idempotent
  əməliyyatlar 3 cəhd + backoff ilə təkrarlanır
- **Sağlamlıq paneli** (PR #421/#422): dashboard → admin → hər xarici servisin son
  uğur/uğursuzluq vaxtı + gecikmə (API: `GET /api/admin/service-health`)

| Servis down | Təsir | Nə etməli |
|---|---|---|
| AZMK | approve uğursuz olur, texniki rollback icra olunur (müştəriyə imtina SMS-i GETMİR — PR #421 fix) | AZMK-nın öz statusunu yoxlayın; bərpa olanda müraciəti yenidən approve edin |
| SMS | SMS çatmır, əsas axın davam edir | `{job="go-app"} \| json \| service="otp" \| level="ERROR"` — provider balansı logda `balance=` sahəsində görünür |
| Video | video-request uğursuz qayıdır | `rec.azmk.az:8699` reachable?; sonra təkrar sifariş |
| DB | app başlamaya bilər / sorğular xəta verir | §6 |

## 5. Sertifikat / Caddy (alpul.az)

**Simptom:** `alpul.az` açılmır, `https://172.17.1.27` işləyir.

1. DNS: `nslookup alpul.az 8.8.8.8` → `185.161.225.102` olmalıdır (yoxsa — admin)
2. Router: TCP 80+443 forward → `172.17.1.27` (admin)
3. Sertifikat: `journalctl -u caddy -f` → "certificate obtained" gözlənilir (avtomatik ACME)
4. Caddy statusu: `systemctl status caddy`
5. Konfiq dəyişəndə: `caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile && systemctl reload caddy`
6. Backup-lar: `/etc/caddy/Caddyfile.bak-*` — geri qaytarma: köçür + reload

Qeyd: daxili qapı (`tls internal`) öz-özünə imzalı sertifikat istifadə edir —
brauzerdə xəbərdarlıq normaldır.

## 6. DB (SQL Server)

- App DB-yə qoşulmazsa başlamır — logda SQL connection xətaları görünür
- Parametrlər: laptopda `deploy/rdc.env` (serverdə yazılmır) → düzəliş + restart
- ⚠ **Avtomatik DB backup HAZIRDA YOXDUR** — IT ilə gündəlik full + transaction log
  backup razılaşdırın (produksiyada ən yüksək prioritetli boşluq)
- Log itkisi/bərpası (Loki): `Docs/PR371_Loki_Incident_Recovery.md`

## 7. Loki / log itkisi

Simptom: Grafana-da keçmiş günlər görünmür. → `Docs/PR371_Loki_Incident_Recovery.md`
(volume mount səhvi, 168h qaydası, app.log-dan backfill proseduru).

Xam log həmişə `/opt/rdc/monitoring/app.log`-da durur + gündəlik tar.gz backup
`/opt/rdc/backups/` (30 gün saxlanılır).

## 8. Əlaqələr — DOLDURULMALI

| Rol | Kim | Əlaqə |
|---|---|---|
| Server / router / DNS admin | `[doldurun]` | `[doldurun]` |
| SQL Server (IT) | `[doldurun]` | `[doldurun]` |
| AZMK inteqrasiya dəstəyi | `[doldurun]` | `[doldurun]` |
| SMS provider (soft-line) | `[doldurun]` | `[doldurun]` |

## 9. Bilinən qeydlər (bug deyil)

- Lokal testlərdə ~21 `TestProcessApplication_*` xətası təmiz main-də də var —
  regressiya deyil, nəzərə alınmamalıdır
- Login rate-limit: 10 cəhd/dəq/IP (`LOGIN_RATE_LIMIT_PER_MIN`) — 429 görmək =
  brute-force cəhdi və ya konfiq nəticəsidir
- LW partner telefonlarının ötürülməsi söndürülüb (`LwPartnerPhonesEnabled=false`) —
  sifarişlə aktivləşəcək (GitHub issue açılıb)
