# AZMK "connection refused" — split-DNS problemi və həlli

**Tarix:** 2026-08-27
**Status:** Həll tətbiq edilib (server-side, `/etc/hosts`)

## Simptom

App loglarında (app.log / Loki / Grafana) AZMK KYC çağırışı retry-larla
uğursuz olur:

```json
{"time":"...","level":"WARN","msg":"AZMK request failed — retrying","attempt":2,"max_retries":3,"backoff_ms":2000,"error":"Post \"https://web.azmk.az:7077/LW_AKP/services/OnlineLendingService/kyc\": dial tcp 172.17.1.22:7077: connect: connection refused"}
```

App fail-soft davranır (3 retry → KYC nəticəsiz qalır, amma müraciət
donmur), ona görə xətta asan görünmür — loglarda axtarın:
`msg="external_call"` / `AZMK request failed`.

## Kök səbəb — split-DNS

`web.azmk.az` adı iki şəbəkədə **iki fərqli IP-yə** həll olunur:

| Haradan baxılır | DNS cavabı | Nəticə |
|---|---|---|
| Korporativ şəbəkə (server) | `172.17.1.22` (daxili IP) | ❌ 7077-də servis dinləmir → connection refused |
| Public şəbəkə | `185.161.225.102` | ✅ servis cavab verir |

### Diaqnostika addımları (serverdə)

```bash
nslookup web.azmk.az            # korporativ DNS → 172.17.1.22
ping -c 3 172.17.1.22           # cavab verir → host sağlamdır
nc -zv 172.17.1.22 7077         # Connection refused → portda servis YOXDUR

# Public endpoint-i DNS-i əl ilə göstərərək sına:
curl -kv --resolve web.azmk.az:7077:185.161.225.102 \
  https://web.azmk.az:7077/LW_AKP/services/OnlineLendingService/kyc
# HTTP 401 → bağlantı OK-dur (curl credential göndərmir, 401 gözləniləndir)
```

**Şərhlər:**
- `connection refused` = TCP paketi çatıb, aktiv RST qayıdıb (timeout deyil).
- Ping işləyir + port refused = maşın sağlamdır, amma servis dayanıb
  və ya firewall reject edir.
- curl ilə 401/5xx almaq **yaxşı xəbərdir** — hər hansı HTTP cavabı
  bağlantının işlədiyini göstərir.

## Tətbiq edilən həll

Serverdə (`ubuntu2404`, `172.17.1.27`) `/etc/hosts`-a sətir əlavə edildi:

```bash
echo "185.161.225.102 web.azmk.az" | sudo tee -a /etc/hosts
```

Sonra app restart:

```bash
cd /alpul/RDC/source && sudo bash deploy/run-remote.sh local
```

**Niyə işləyir:** Go resolver-i DNS-dən əvvəl `/etc/hosts`-a baxır →
app həmişə public endpoint-ə (`185.161.225.102:7077`) bağlanır.

## Risklər və qeydlər

- Sətir **server-lokaldır**: server yenidən qurulanda təkrar edilmləlidir
  (bu sənədə istinad edin). Yeni server əlavə olunarsa orada da tələb olunur.
- AZMK daxili endpoint-i (`172.17.1.22:7077`) gələcəkdə bərpa olunarsa və
  daxili trafik tələb olunarsa — sətri silin, DNS öz işini görəcək.
- `nslookup` köhnə IP göstərməyə davam edəcək — o birbaşa DNS-ə soruşur,
  `/etc/hosts`-u oxumur. Doğru yoxlama: `getent hosts web.azmk.az`.
- AZMK credential-ları (`AZMK_PASSWORD` və s.) `deploy/rdc.env`-də qalır —
  bu həll yalnız ünvan yönləndirməsidir, autentifikasiyaya toxunmur.
