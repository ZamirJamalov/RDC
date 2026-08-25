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
7. binary-nin build vaxtını göstərir — köhnə binary görünsün deyə (PR #305)

⚠ DIQQƏT (PR #305): `run-remote.sh` **build ETMİR** — serverdə hazır duran
`/opt/rdc/rdc` binary-ni başladır. Kod dəyişəndə əvvəlcə yenidən build + copy:
`go build -o rdc . && sudo cp rdc /opt/rdc/rdc` (bax: bölmə 9 "Kod dəyişəndə").
Yoxlama: `strings /opt/rdc/rdc | grep -c external_call` → 0-dırsa binary köhnədir.

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

### Kənar servislərin sorğu/cavabları (PR #304)

Kənar servislərə (AZMK, LW, OTP, MyGov, SIMA, Video) gedən hər HTTP sorğusu və
cavabı Loki-yə düşür — `msg="external_call"`. Parol/token dəyərləri avtomatik
maskalanır (`***`), body-lər 4000 simvola kimi kəsilir.

Grafana Explore → Loki sorğuları:

```logql
# hamısı
{job="go-app"} | json | msg="external_call"
# yalnız AZMK (məs: application create + cavabı)
{job="go-app"} | json | service="azmk"
# yalnız xətalar
{job="go-app"} | json | msg="external_call" | level="ERROR"
```

Söndürmək: `EXTERNAL_REQRESP_LOG=false` (`deploy/rdc.env`) → restart.

```bash
curl -s "http://localhost:3100/ready"     # Loki hazırdır? (serverdə)
```

## 8. Nə binary-nin içində DEYİL (xarici asılılıqlar)

- SQL Server (DB) — şəbəkə üzərindən
- AZMK / LW / MyGov / SIMA / OTP / Video extern servisləri — env ilə qurulur
- Loki/Promtail/Grafana monitorinq stack-i (bax: `MONITORING.md`)

## 9. Lokal test — serveri simulyasiya etmək

Real serverə getməzdən əvvəl hər şeyi **lokalda, server kimi** yoxlamaq olar.
Fikir: `run-remote.sh`-in serverdə etdiyi eyni əmri lokalda `sudo` ilə icra
edirik — eyni `rdc` istifadəçisi, eyni `/opt/rdc` yolları, eyni Loki zənciri.

### Bir dəfəlik hazırlıq (~5 dəqiqə)

```bash
# 0) köhnə dev proseslərini dayandır (portlar boş olsun: 8000, 3001, 3100)
pkill -x rdc 2>/dev/null; pkill -f "go run" 2>/dev/null
cd source/
docker compose down        # dev monitorinqini söndür

# 1) build
go build -o rdc .

# 2) "server" quruluşu — install.sh lokalda da eyni işi görür
sudo bash deploy/install.sh
# → rdc istifadəçisi, /opt/rdc/rdc, /opt/rdc/monitoring/ (loki+promtail+grafana)

# 3) rdc.env-i LOKAL dəyərlərlə doldur
nano deploy/rdc.env
#   DB_HOST=localhost
#   DB_PASSWORD=<lokal SQL parolu>
#   AZMK_PASSWORD=<parol>
#   MIGRATIONS_DROP_RECREATE şərhdə qalsın! (yoxsa DB silinər)
```

### Başlatma — serverdəki eyni əmr (ssh yerinə sudo)

```bash
sudo bash -c 'pkill -x rdc 2>/dev/null; set -a; . /dev/stdin; set +a; su -m -s /bin/bash rdc -c "cd /opt/rdc && { nohup /opt/rdc/rdc >> /opt/rdc/monitoring/app.log 2>&1 < /dev/null & }"' < deploy/rdc.env
```

Qeyd: env ROOT shell-də oxunur, `su -m` onu `rdc` istifadəçisinə ötürür.
(env-i su-dan sonra oxumaq olmaz — ssh/sudo stdin-i root-a aid pipe-dır,
`rdc` onu açanda "Permission denied" alır — PR #303.)

### Yoxlama

```bash
pgrep -x rdc                                # proses yaşayır?
tail -f /opt/rdc/monitoring/app.log         # loglar canlı (Ctrl+C — çıx)
curl http://localhost:8000/                 # app cavab verir?
curl http://localhost:3100/ready            # Loki hazır?
# Grafana: http://localhost:3001 → Explore → {job="go-app"}
```

Terminalı bağlayın → app davam etməlidir (nohup testi). Sonra `pgrep -x rdc`
ilə yenidən yoxlayın.

### Kod dəyişəndə

```bash
go build -o rdc . && sudo cp rdc /opt/rdc/rdc
# sonra başlatma əmrini təkrarlayın
```

### Test bitəndə təmizləyin

```bash
sudo pkill -x rdc
cd /opt/rdc/monitoring && docker compose down   # lokal "server" monitorinqi
cd <repo>/source && docker compose up -d        # dev monitorinqini geri qaytarın
```

### Opsional: `run-remote.sh`-in özünü ssh ilə test etmək

```bash
sudo passwd root        # root parolu təyin edin (yalnız lokal test üçün)
sudo sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config
sudo systemctl enable --now ssh && sudo systemctl restart ssh

bash deploy/run-remote.sh root@localhost

# Test sonrası MUTLƏQ geri qaytarın:
#   PermitRootLogin no  +  sudo systemctl restart ssh
```

### Problemlər (real serverdə də eynidir)

| Görünən | Səbəb |
|---|---|
| `XƏTA: .../rdc.env yoxdur` | rdc.env yaradılmayıb/doldurulmayıb |
| `başlamadı` + logda DB xətası | rdc.env-də DB dəyərləri yanlış/boş |
| `başlamadı` + no such file | install.sh işlədilməyib (`/opt/rdc/rdc` yoxdur) |
| ssh bağlana bilmir | ssh servisi söndürülüb / root girişi bağlı |
| App işləyir, Grafana boşdur | monitorinq compose-u söndürülüb |
| `bash: /dev/stdin: Permission denied` | köhnə versiya — PR #303-də düzəldilib |
