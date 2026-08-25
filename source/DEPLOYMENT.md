# RDC Deploy — sadə axın (PR #298)

App **1 əmrlə** işə salınır (laptopdan):

```bash
bash deploy/run-remote.sh user@server
```

Parollar **serverdə saxlanmır** — laptopda `deploy/rdc.env` faylındadır və
başlanğıc anında şifrələnmiş ssh kanalı ilə birbaşa prosesin env-inə ötürülür.

## Binary tam self-contained-dir (PR #293)

| Nə lazımdır? | Haradadır? |
|---|---|
| HTML səhifələr (`web/*.html`) | binary-nin içində (`go:embed`) |
| Statik asset-lər | binary-nin içində (`go:embed`) |
| SQL migration-ları | binary-nin içində (`go:embed`, PR #293) |
| Config | env dəyişənlərindən (fayl tələb olunmur) |

Yalnız **env dəyişənləri** və **SQL Server çıxışı** lazımdır.

## 1. Build (laptopda)

```bash
cd source/
go build -o rdc .
```

## 2. Serverə daşı

```bash
scp rdc user@server:/opt/rdc/
# Yalnız bu 1 fayl kifayətdir — web/, migrations/ daşımaq lazım deyil.
```

## 3. Bir dəfə: server hazırlığı (install.sh)

```bash
# serverdə, source/ qovluğunda (rdc binary orada olmalıdır):
sudo env GRAFANA_ADMIN_PASSWORD=güclüparol bash deploy/install.sh
```

Nə qurur: `rdc` istifadəçisi, `/opt/rdc/` qovluqları, monitorinq stack-i
(loki + promtail + grafana — docker). App parametrləri soruşulmur, systemd
service başladılır (yalnız unit faylı qoyulur — app run-remote.sh ilə işə salınır).

## 4. Parametrlər: laptopda `deploy/rdc.env`

```bash
cp deploy/env.example deploy/rdc.env   # bir dəfə
nano deploy/rdc.env                    # parolları doldur (DB_PASSWORD, AZMK_PASSWORD)
```

- `rdc.env` `.gitignore`-dadir — **git-ə getmir**
- Format: `KEY=value` (`export` sözü yazılmır)
- Dəyişiklikdən sonra yenidən `run-remote.sh` işlədin

### LOG_LEVEL — necə işləyir?

Yalnız bir dəyər təyin edilir — bu, **minimum səviyyədir** (threshold):

| LOG_LEVEL | Nə loglanır | İstifadə halı |
|---|---|---|
| `debug` | DEBUG + INFO + WARN + ERROR (hər şey) | troubleshoot |
| `info` | INFO + WARN + ERROR (**default**) | gündəlik istifadə |
| `warn` | yalnız WARN + ERROR | yalnız problemlər |
| `error` | yalnız ERROR | minimal rejim |

Qeyd: `MIGRATIONS_DROP_RECREATE` default **false**-dur (PR #294) — yazmasanız
data güvəndədir. Yalnız dev-də DB-ni sıfırdan qurmaq istəyəndə açıq şəkildə
`MIGRATIONS_DROP_RECREATE=true` yazın (bütün data silinir).

## 5. İşə salma: 1 əmr

```bash
bash deploy/run-remote.sh user@server
```

Script nə edir:
1. laptopdakı `rdc.env`-i yoxlayır (məcburi açarlar; `MIGRATIONS_DROP_RECREATE=true`-sa xəbərdarlıq edib təsdiq istəyir)
2. serverdə köhnə `rdc` prosesini dayandırır
3. `rdc.env`-i ssh stdin ilə **birbaşa prosesin env-inə** ötürür (serverdə fayl yaranmır, history-yə düşmür)
4. app-ı `rdc` istifadəçisi ilə `nohup`-la başladır — terminalı bağlasanız da **işləməyə davam edir**
5. logları `/opt/rdc/monitoring/app.log`-a yazır (Loki zənciri — aşağıda)
6. prosesin qalxdığını yoxlayır + son logları göstərir

Qeyd: root kimi bağlanmaq ən sadədir (`su` parol soruşmur); qeyri-root
istifadəçi üçün parolsuz sudo (`sudo -n`) tələb olunur.

## 6. Gündəlik əmrlər

| Əməliyyat | Əmr (laptopdan) |
|---|---|
| Başlat / restart | `bash deploy/run-remote.sh user@server` |
| Status | `ssh user@server 'pgrep -x rdc; tail -20 /opt/rdc/monitoring/app.log'` |
| Logları canlı izlə | `ssh user@server 'tail -f /opt/rdc/monitoring/app.log'` (çıxmaq: Ctrl+C) |
| Dayandır | `ssh user@server 'pkill -x rdc'` |
| Parametr dəyiş | `deploy/rdc.env` redaktə et → restart əmri |

### `nohup`-dan sonra prosesi necə tapmaq olar?

`nohup` prosesi terminaldan qoparıb sistemə verir — bundan sonra o **hər hansı
terminalın "içində" deyil**. Ona görə onu PID/ilə və ya **adı ilə** (`rdc`)
tapırsınız, çıxışı isə `app.log` faylına gedir (ekranı yoxdur):

```bash
ssh user@server 'pgrep -x rdc'        # PID göstərir (məs: 722)
ssh user@server 'ps aux | grep rdc'   # tam məlumat (nə vaxtdan işləyir və s.)
```

Vacib: **restart üçün heç nə axtarmaq lazım deyil** — `run-remote.sh` özü
əvvəl köhnə prosesi öldürür (`pkill -x rdc`), sonra yenisini başladır.

### Ssenari: bir günün axını

```
1. bash deploy/run-remote.sh user@server      → "rdc İŞLƏYİR ✓ PID: 722"
2. Laptopun qapağını bağlayırsınız            → app İŞLƏMƏYƏ DAVAM EDİR
3. Sabah yoxlamaq istəyirsiniz:
   ssh user@server 'pgrep -x rdc'             → 722  (tapdınız ✓)
4. Parametr dəyişmək istəyirsiniz:
   rdc.env-i redaktə edirsiniz → run-remote.sh
   → "köhnə rdc dayandırıldı" → "İŞLƏYİR ✓ PID: 801" (yeni proses)
```

Qeyd: logları brauzerdən də izləmək olar — Grafana `http://<server>:3001`
→ Explore → `{job="go-app"}` (bax: bölmə 7). Terminala ehtiyac qalmır.

## 7. Loglar → Loki

```
app (stdout, JSON) → app.log (run-remote.sh redirect) → Promtail → Loki → Grafana
```

- App bütün logları stdout-a JSON formatda yazır — redirect-i `run-remote.sh` özü edir
- Monitorinq stack-i install.sh ilə qalxır və reboot-da avtomatik qalxır (docker restart policy)
- Grafana: `http://<server>:3001` → Explore → `{job="go-app"}`

```bash
curl -s "http://localhost:3100/ready"     # Loki hazırdır? (serverdə)
```

## 8. Nə binary-nin içində DEYİL (xarici asılılıqlar)

- SQL Server (DB) — şəbəkə üzərindən
- AZMK / LW / MyGov / SIMA / OTP / Video extern servisləri — env ilə qurulur
- Loki/Promtail/Grafana monitorinq stack-i (bax: `MONITORING.md`)
