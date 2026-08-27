# Video record üçün HTTPS (Caddy) — secure context problemi

**Tarix:** 2026-08-27
**Status:** Tətbiq edilib — `https://172.17.1.27` (443) → `localhost:8000`

## Simptom

Video-record açılarkən brauzerdə bu xəta çıxır:

```
Kameraya giriş icazəsi verilmədi: can't access property "getUserMedia",
navigator.mediaDevices is undefined
```

## Kök səbəb

`getUserMedia` (kamera/mikrofon API) brauzerdə yalnız **secure
context**-də mövcuddur:

| URL | Secure context? | Kamera |
|---|---|---|
| `https://...` (istənilən host/IP) | ✅ | ✅ işləyir |
| `http://localhost:8000` | ✅ | ✅ işləyir |
| `http://172.17.1.27:8000` ← köhnə üsul | ❌ | ❌ `mediaDevices undefined` |

App `:8000`-də adi HTTP ilə xidmət etdiyindən brauzer
`navigator.mediaDevices`-i ümumiyyətlə təyin etmir — icazə sorğusu belə
mümkün olmur. **Kodda və serverdə problem yoxdur** — bu, brauzerin
təhlükəsizlik qaydasıdır.

## Tətbiq edilən həll — Caddy reverse proxy (server tərəfində)

### 1. Caddy quraşdırılması (bir dəfə)

```bash
sudo apt install -y debian-keyring debian-archive-keyring curl
sudo curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
sudo curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy
```

### 2. Konfiqurasiya — `/etc/caddy/Caddyfile`

```
https://172.17.1.27 {
    tls internal
    reverse_proxy localhost:8000
}
```

- `tls internal` — Caddy-nin daxili CA-sı IP üçün self-signed sertifikat
  yaradır. Public CA-lar (Let's Encrypt və s.) private IP-lər üçün
  sertifikat verə bilməz — qlobal CA/Browser Forum qaydasıdır.

### 3. Aktivləşdirmə

```bash
caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
sudo systemctl enable --now caddy
```

### 4. Brauzerdə (hər istifadəçi, bir dəfə)

`https://172.17.1.27` açılır → "sertifikata etimad edilmir" xəbərdarlığı →
**Advanced → Proceed to 172.17.1.27 (unsafe)** → kamera icazəsi soruşulur ✅

Xəbərdarlıq bir dəfə qəbul olunur, brauzer qərarı yadda saxlayır.
Şifrələmə (TLS) realdır; warning yalnız "imzalayan tanınmır" deməkdir —
imzalayan isə bizim öz serverimizdir.

## Alternativlər (gələcək rollout üçün müqayisə)

| Variant | Client tərəfdə nə lazım | URL | Warning |
|---|---|---|---|
| **Caddy + Proceed (hazırkı)** | heç nə | `https://172.17.1.27` | bir dəfə |
| CA import (root.crt → Trust Store) | root.crt köçürmək | `https://172.17.1.27` | yoxdur |
| SSH tunnel / VS Code PORTS panel | heç nə | `http://localhost:8000` | yoxdur |
| Public domain + DNS-01 (Let's Encrypt) | heç nə | `https://rdc.domain.az` | yoxdur |

Kütləvi rollout lazım olanda ən düzgün yol: daxili DNS A-record + CA-nın
paylanması, yaxud public domain + DNS-01 challenge (Caddy DNS plugin).

## Qeydlər

- Köhnə `http://172.17.1.27:8000` paralel işləməyə davam edir (kamerasız) —
  API/test axınları üçün heç nə dəyişməyib.
- Caddy logları: `sudo journalctl -u caddy -f`
- WebSocket avtomatik yönlənir (video-record status axını üçün vacib).
- CA kök sertifikatı (import variantı üçün):
  `/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt`
- UFW açıqdırsa: `sudo ufw allow 80,443/tcp`
