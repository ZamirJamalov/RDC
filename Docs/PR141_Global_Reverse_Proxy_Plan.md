# PR #141 — Global Reverse Proxy Planı

## Məqsəd

`landing.html` və `apply.html` gələcəkdə **qlobal ön qapı** (global front door) olacaq — internetdən birbaşa daxil olan müştəri müraciətləri reverse proxy üzərindən daxil olacaq. `detail.html` və `index.html` isə **daxili ekspert paneli** kimi qalacaq və yalnız korporativ şəbəkədən (VPN/IP-allowlist) çatdırılacaq.

Bu plan, həmin arxitektura keçid üçün lazım olan **bütün texniki dəyişiklikləri** vahid sənəddə cəmləşdirir.

---

## 1. Hazırkı Vəziyyət (As-Is)

### 1.1 Tək server, tək port, auth yoxdur

```
┌──────────────────────────────────────────────────────────────┐
│ Internet (müştərilər + ekspertlər eyni şəbəkədə)            │
└────────────────────────┬─────────────────────────────────────┘
                         │  HTTP :8000
                         ▼
┌──────────────────────────────────────────────────────────────┐
│ Go server (main.go)                                          │
│                                                              │
│  /api/*        → JSON router (auth YOXDUR)                   │
│  /api/expert/* → ekspert əməliyyatları (auth YOXDUR)         │
│  /api/admin/*  → admin əməliyyatları (auth YOXDUR)           │
│  /api/mock/*   → dev-only mock endpoint-lər (auth YOXDUR)    │
│  *.html        → embed.FS file server                        │
│                                                              │
│  Middleware: RequestID → Recovery → Logger                   │
│  TLS:        yoxdur                                          │
│  Timeouts:   yoxdur                                          │
│  CORS:       yoxdur                                          │
│  Rate limit: yalnız OTP send (1/min/phone)                   │
└──────────────────────────────────────────────────────────────┘
```

### 1.2 Endpoint qrupları və hədəf auditoriya

| Qrup | Endpoint prefiks | Hədəf auditoriya | Hazırda auth? |
|------|------------------|------------------|---------------|
| **Public (müştəri)** | `POST /api/applications/init`, `/init/verify`, `/customer-confirm`, `GET /api/applications/offer`, `POST /api/otp/send`, `/verify`, `GET /api/discount-codes/validate` | İnternetdən gələn müştərilər | ❌ yoxdur (lakin JSON məzmun standartdır) |
| **Internal ekspert** | `GET /api/expert/queue`, `/expert/{id}`, `PUT /expert/{id}/approve`, `/reject`, `PUT /api/applications/{id}/timer`, `/contacts`, MyGov request/verify | Ekspertlər (operatorlar) | ❌ yoxdur — **TƏHLÜKƏLİ** |
| **Admin** | `GET/PUT /api/admin/feature-flags*` | Adminlər | ❌ yoxdur — **TƏHLÜKƏLİ** |
| **Router (LW-yə məxsus)** | `GET /api/router/*`, `GET /api/lw/blacklist`, `POST /api/lw/loans/approve` | Daxili credit engine istifadə edir | ❌ yoxdur — ekspozur tələb olunmur |
| **Callback** | `POST /api/rdc/callback/sima-result`, `/lw-loan-status` | LW və SIMA xarici servis çağırır | ❌ yoxdur — **LW IP-lərindən gəlməlidir** |
| **Mock** | `POST /api/mock/lw/setup`, `GET /api/mock/lw/query` | Yalnız dev | ❌ yoxdur — **prod-da block olunmalıdır** |

### 1.3 Statik fayllar

| URL | Fayl | Hədəf |
|-----|------|-------|
| `/` | `index.html` (default) | **EKSPERT** queue — qlobal qapıda yanlış! |
| `/landing.html` | `landing.html` | Müştəri (marketinq) |
| `/apply.html` | `apply.html` | Müştəri (müraciət forması) |
| `/detail.html` | `detail.html` | Ekspert dashboard |
| `/index.html` | `index.html` | Ekspert queue |

**Problem:** `/` hazırda ekspert queue-sine açılır. Qlobal qapıda `/` müştəri landing-ə açılmalıdır.

### 1.4 Konfiqurasiya boşluqları

| Boşluq | Risk | Həll yolu |
|--------|------|-----------|
| `MIGRATIONS_DROP_RECREATE=true` (default) | Prod restart zamanı bütün data silinir | `.env`-də mütləq `false` |
| `MYGOV_REDIRECT_URI=https://webhook.site/...` | Test URL prod-a sızır | Prod URL olmalıdır |
| `MYGOV_WEB_URL=https://...netlify.app/` | Test URL | Prod URL |
| `AZMK InsecureSkipVerify: true` | Self-signed cert qəbul edilir | CA pin və ya risk qəbul |
| `DB DSN encrypt=disable` | SQL trafiki şifrəsiz | `encrypt=true` + CA |
| `SetMaxOpenConns` yoxdur | Yüksək yük-də DB exhausting | Tətbiq səviyyəsində tune |
| Server timeout yoxdur | Slowloris DoS | `ReadHeaderTimeout: 10s` və s. |
| `X-Forwarded-For` parse olunmur | Log-lar proxy IP göstərir | App-səviyyəsində parse |
| `healthz` endpoint yoxdur | K8s/CDN health-check mümkün deyil | Əlavə edilməlidir |

---

## 2. Hədəf Arxitektura (To-Be)

### 2.1 Yüksək səviyyə diaqram

```
                          ┌─────────────────────┐
                          │  Internet istifadəçiləri │
                          │  (müştərilər)        │
                          └──────────┬──────────┘
                                     │  HTTPS (443)
                                     ▼
                ┌────────────────────────────────────────┐
                │  Public Reverse Proxy (NGINX/Cloudflare)│
                │                                        │
                │  • TLS termination (Let's Encrypt/ACM) │
                │  • www.alpul.az → alpul.az redirect    │
                │  • /  → /landing.html rewrite          │
                │  • Rate limit (token bucket)           │
                │  • Security headers (HSTS, CSP, ...)   │
                │  • WAF (Web Application Firewall)      │
                │  • Bot/DDoS protection                 │
                └──────────┬─────────────────────────────┘
                           │
                           │ Yalnız public route-lar:
                           │   • /landing.html
                           │   • /apply.html
                           │   • /api/applications/init*
                           │   • /api/applications/offer
                           │   • /api/applications/{id}/customer-confirm
                           │   • /api/otp/send, /verify
                           │   • /api/discount-codes/validate
                           │   • /api/applications/{id}/contacts (write-only, OTP-protected)
                           │   • /api/applications/{id}/timer (write-only)
                           │
                           ▼
                ┌────────────────────────────────────────┐
                │  Public App Instance (Go :8000)        │
                │                                        │
                │  • DB: SQL Server                       │
                │  • AZMK, AKB (via LW), MyGov, SIMA, OTP│
                │  • Mock modellər: *_USE_MOCK=false     │
                │  • MIGRATIONS_DROP_RECREATE=false      │
                └────────────────────────────────────────┘


  ┌──────────────────────┐                  ┌─────────────────────┐
  │  Ekspertlər (VPN/ilə)│                  │  LW / SIMA (external)│
  └──────────┬───────────┘                  └──────────┬──────────┘
             │  HTTPS (443)                            │ HTTPS (callback)
             ▼                                         ▼
  ┌─────────────────────────────────────┐  ┌────────────────────────────────┐
  │  Internal Reverse Proxy (NGINX)     │  │  Callback Proxy (NGINX, az IP)  │
  │                                     │  │                                │
  │  • mTLS və ya Basic Auth            │  │  • IP allowlist: yalnız LW/SIMA │
  │  • /expert/* path                   │  │  • HMAC signature verification │
  │  • CSRF token                       │  │  • Rate limit (stricter)        │
  │  • Session cookie (secure, httpOnly)│  │  • Replay attack prevention    │
  └──────────┬──────────────────────────┘  └──────────────┬─────────────────┘
             │                                            │
             │ /api/expert/*                              │ /api/rdc/callback/*
             │ /api/admin/*                               │
             │ /api/mygov/*-request, *-verify             │
             │ /api/applications/{id}/contacts (read)     │
             │ /detail.html, /index.html                  │
             │                                            │
             ▼                                            ▼
  ┌────────────────────────────────────────────────────────────────────┐
  │  Internal App Instance (Go :8001)  ←→ DB: SQL Server               │
  │                                                                    │
  │  Eyni Go binary, lakin fərqli env:                                 │
  │  • SERVER_ADDR=:8001                                               │
  │  • Ekspert və callback route-ları aktiv                            │
  │  • Mock route-ları DISABLED                                        │
  └────────────────────────────────────────────────────────────────────┘
```

### 2.2 2 instans, 1 binary strategiyası

Strategiya: **eyni Go binary** iki dəfə işə salınır, lakin **fərqli env** və **fərqli port** ilə:

| Instans | Port | Açık route-lar | Bağlı route-lar |
|---------|------|----------------|-----------------|
| **public-app** | `:8000` | `*.html` (landing/apply), `POST /api/applications/init*`, `/customer-confirm`, `GET /api/applications/offer`, `/api/otp/*`, `/api/discount-codes/validate`, `/api/applications/{id}/contacts` (PUT yalnız), `/api/applications/{id}/timer` (PUT yalnız), `/healthz` | `/api/expert/*`, `/api/admin/*`, `/api/mock/*`, `/api/router/*`, `/api/lw/*`, `/api/rdc/callback/*`, `detail.html`, `index.html` |
| **internal-app** | `:8001` | Bütün route-lar (development və daxili istifadə üçün) | — |

**Niyə 2 instans?**
- Public instans sıradan çıxsa (məs. DDoS), ekspert panel işləməyə davam edir.
- Public instans yalnız JSON API qaytarır — `detail.html`/`index.html` faylları fiziki olaraq yalnız internal instansda mövcuddur (nginx-də `location /detail.html { deny all; }`).
- Fərqli log faylları, fərqli metrikalar, fərqli scaling policy.

**Alternativ:** Tək instans + nginx-də path-based filtering. Daha sadə, lakin riski yüksək (səhv konfiqurasiya public ekspert panelini açıq saxlaya bilər).

---

## 3. Route Map (Nə hara gedir)

### 3.1 Public Reverse Proxy — route cədvəli

| URL path (gələn) | Action | Upstream |
|-------------------|--------|----------|
| `/` | `rewrite ^ /landing.html` | public-app:8000 |
| `/landing.html` | pass | public-app:8000 |
| `/apply.html` | pass | public-app:8000 |
| `/api/applications/init` (POST) | pass | public-app:8000 |
| `/api/applications/init/verify` (POST) | pass | public-app:8000 |
| `/api/applications/offer` (GET) | pass | public-app:8000 |
| `/api/applications/{id}/customer-confirm` (POST) | pass | public-app:8000 |
| `/api/applications/{id}/contacts` (PUT) | pass | public-app:8000 |
| `/api/applications/{id}/timer` (PUT) | pass | public-app:8000 |
| `/api/applications/{id}` (GET) | pass | public-app:8000 |
| `/api/applications/{id}/status` (GET) | pass | public-app:8000 |
| `/api/otp/send` (POST) | rate limit 1/min/IP+phone | public-app:8000 |
| `/api/otp/verify` (POST) | rate limit 5/min/IP | public-app:8000 |
| `/api/discount-codes/validate` (GET) | rate limit 10/min/IP | public-app:8000 |
| `/healthz` (GET) | pass | public-app:8000 |
| `/api/expert/*` | **deny 403** | — |
| `/api/admin/*` | **deny 403** | — |
| `/api/mock/*` | **deny 403** | — |
| `/api/router/*` | **deny 403** | — |
| `/api/lw/*` | **deny 403** | — |
| `/api/rdc/callback/*` | **deny 403** (proxy səviyyəsində) | — |
| `/api/mygov/*-request`, `*-verify` | **deny 403** | — |
| `/detail.html`, `/index.html` | **deny 403** | — |
| `/*.css`, `/*.js`, `/*.png`, `/*.svg` | pass (CDN və ya self-host) | CDN və ya public-app |

### 3.2 Internal Reverse Proxy — route cədvəli

| URL path | Action | Upstream |
|----------|--------|----------|
| `/` | `rewrite ^ /index.html` | internal-app:8001 |
| `/index.html` | pass | internal-app:8001 |
| `/detail.html` | pass | internal-app:8001 |
| `/api/expert/*` | pass (mTLS/auth) | internal-app:8001 |
| `/api/admin/*` | pass (mTLS/auth) | internal-app:8001 |
| `/api/applications/*` | pass | internal-app:8001 |
| `/api/mygov/*` | pass | internal-app:8001 |
| `/api/router/*` | pass | internal-app:8001 |
| `/api/lw/*` | pass | internal-app:8001 |
| `/api/otp/*` | pass | internal-app:8001 |
| `/api/discount-codes/*` | pass | internal-app:8001 |
| `/healthz` | pass | internal-app:8001 |

### 3.3 Callback Proxy — yalnız LW/SIMA callback-ləri

| URL path | Action | Upstream |
|----------|--------|----------|
| `/api/rdc/callback/sima-result` (POST) | IP allowlist + HMAC verify | internal-app:8001 |
| `/api/rdc/callback/lw-loan-status` (POST) | IP allowlist + HMAC verify | internal-app:8001 |
| Everything else | **deny 403** | — |

---

## 4. App-Səviyyəsi Dəyişikliklər (Go kodunda)

### 4.1 Yeni: `ROUTE_MODE` env var

App-səviyyəsində hansı route qruplarının aktiv olduğunu idarə etmək üçün yeni env var:

```go
// config/config.go
type Config struct {
    // ...
    RouteMode string // "all" | "public" | "internal"
}
```

`.env.example`:
```
# PR #141: route mode
# all      — bütün route-lar aktiv (dev/default)
# public   — yalnız müştəri route-ları aktiv (public-app instansı)
# internal — bütün route-lar aktiv, lakin mock route-ları deaktiv (internal-app)
ROUTE_MODE=all
```

`router.go`-da qruplaşdırma:
```go
// internal/handler/router.go
func New(deps Deps) http.Handler {
    mux := http.NewServeMux()
    mode := deps.Config.RouteMode

    // HƏMİŞƏ aktiv (hər iki modda)
    registerCustomerRoutes(mux, deps)
    registerHealthRoutes(mux, deps)

    if mode == "all" || mode == "internal" {
        registerExpertRoutes(mux, deps)
        registerAdminRoutes(mux, deps)
        registerMyGovRoutes(mux, deps)
        registerRouterRoutes(mux, deps)
        registerCallbackRoutes(mux, deps)
    }

    if mode == "all" {
        // mock route-ları yalnız dev modda
        registerMockRoutes(mux, deps)
    }
    // public modda: expert/admin/router/callback/mock route-ları Qeydiyyatdan keçmir
    // nginx 404 qaytarır (403 yox), çünki Go mux bu path-ları tanımır
}
```

### 4.2 Yeni: `/healthz` endpoint

```go
// internal/handler/health_handler.go
func (h *HealthHandler) Healthz(w http.ResponseWriter, r *http.Request) {
    if err := h.db.PingContext(r.Context()); err != nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        json.NewEncoder(w).Encode(map[string]string{
            "status": "unhealthy",
            "error":  "db ping failed",
        })
        return
    }
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{
        "status":     "ok",
        "version":    h.version,
        "route_mode": h.routeMode,
    })
}
```

Route: `GET /healthz` — heç bir middleware-dən keçmir (RequestID belə).

### 4.3 Server timeout-ları

```go
// main.go
srv := &http.Server{
    Addr:              cfg.ServerAddr,
    Handler:           httpHandler,
    ReadHeaderTimeout: 10 * time.Second,
    ReadTimeout:       30 * time.Second,
    WriteTimeout:      30 * time.Second,
    IdleTimeout:       120 * time.Second,
    MaxHeaderBytes:    1 << 20, // 1 MB
}
```

### 4.4 `X-Forwarded-For` parse

```go
// internal/middleware/logger.go
func realIP(r *http.Request) string {
    // Trusted proxy-lərin IP-si config-dən gəlməlidir
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        // İlk IP = əsl müştəri
        if idx := strings.Index(xff, ","); idx >= 0 {
            return strings.TrimSpace(xff[:idx])
        }
        return strings.TrimSpace(xff)
    }
    if xri := r.Header.Get("X-Real-IP"); xri != "" {
        return xri
    }
    return r.RemoteAddr
}
```

**Vacib:** Trusted proxy IP-ləri `.env`-dən gəlməlidir (`TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12`). Əks halda istənilən müştəri saxta `X-Forwarded-For` göndərə bilər.

### 4.5 DB pool tune

```go
// main.go
db, err := sql.Open("mssql", cfg.DSN())
db.SetMaxOpenConns(25)      // prod default
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
db.SetConnMaxIdleTime(2 * time.Minute)
```

`.env.example`:
```
DB_MAX_OPEN_CONNS=25
DB_MAX_IDLE_CONNS=5
DB_CONN_MAX_LIFETIME_S=300
DB_CONN_MAX_IDLE_S=120
```

### 4.6 DB TLS

```go
// config/config.go DSN()
func (c *Config) DSN() string {
    encrypt := c.DBEncrypt // "true" və ya "disable"
    trustCert := c.DBTrustServerCertificate // "true" və ya "false"
    return fmt.Sprintf("server=%s;port=%s;user id=%s;password=%s;database=%s;encrypt=%s;trustServerCertificate=%s",
        c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, encrypt, trustCert)
}
```

`.env.example`:
```
DB_ENCRYPT=true
DB_TRUST_SERVER_CERTIFICATE=false
```

### 4.7 Migrations path fix (mütləq)

```go
// main.go
migrationsDir := cfg.MigrationsDir // default: "migrations"
if err := migration.Run(db, migrationsDir); err != nil {
    slog.Error("migration failed", "error", err)
    os.Exit(1)
}
```

`.env.example`:
```
MIGRATIONS_DIR=/app/migrations
MIGRATIONS_DROP_RECREATE=false  # PROD-DA MÜTLƏQ false!
```

---

## 5. Statik Faylların İdarə Edilməsi

### 5.1 `/` kök route-un dəyişdirilməsi

**Strategiya A (App-səviyyəsində, tövsiyə edilir):**

```go
// main.go
httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.HasPrefix(r.URL.Path, "/api/") {
        router.ServeHTTP(w, r)
        return
    }
    // PR #141: kök route landing.html-a yönləndir
    if r.URL.Path == "/" {
        http.ServeFileFS(w, r, webFS, "web/landing.html")
        return
    }
    fileServer.ServeHTTP(w, r)
})
```

**Strategiya B (Proxy-səviyyəsində):** nginx-də `location = / { rewrite ^ /landing.html last; }`.

Tövsiyə: **hər ikisi** — app-səviyyəsində default landing.html, proxy-səviyyəsində isə əlavə rewrite.

### 5.2 Self-host CDN resursları (opsional, lakin tövsiyə edilir)

`landing.html` və `apply.html` hal-hazırda bu CDN-ləri çağırır:
- `https://cdn.tailwindcss.com` (Tailwind JIT)
- `https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js`
- `https://fonts.googleapis.com` (Inter)
- `https://cdn.soft10.io/widget.js` (yalnız landing-də)

**Risklər:**
- CDN xarici ölkədədirsə, bəzi istifadəçilər üçün gecikmə.
- CDN-nin sıradan çıxması bütün UI-nu pozur.
- Məxfilik/GDPR: üçüncü tərəfə istifadəçi IP-ləri sızır.

**Həll (opsional PR):**
1. Tailwind-ın production build-i (`tailwindcss-cli` ilə minify CSS) → `/static/css/tailwind.min.css`
2. Alpine.js download → `/static/js/alpine.min.js`
3. Inter font download → `/static/fonts/`
4. HTML-də CDN link-lərini self-host path-ləri ilə əvəz et

Bu, ayrı PR ola bilər (PR #142+).

### 5.3 Soft10 widget

`landing.html:722`-də hardcoded `data-customer-id="lc_53a767d43ff6e7d09543006b49651e2c"`.

**Həll:** HTML-i templating et (server-side render və ya JS əvəzetmə):
```html
<script src="https://cdn.soft10.io/widget.js"
        data-customer-id="{{.Soft10CustomerID}}"></script>
```

---

## 6. Təhlükəsizlik (Security)

### 6.1 Security headers (nginx-də)

```nginx
# hSTS — 1 il, preload, subdomain-lər də daxil
add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;

# Clickjaking qorunması
add_header X-Frame-Options "DENY" always;

# MIME sniffing qorunması
add_header X-Content-Type-Options "nosniff" always;

# Referrer policy
add_header Referrer-Policy "strict-origin-when-cross-origin" always;

# CSP — diqqətlə konfiqurasiya edilməlidir
add_header Content-Security-Policy "default-src 'self'; \
    script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://cdn.jsdelivr.net https://cdn.soft10.io; \
    style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; \
    font-src 'self' https://fonts.gstatic.com; \
    img-src 'self' data: https:; \
    connect-src 'self'; \
    frame-ancestors 'none'; \
    base-uri 'self'; \
    form-action 'self'" always;
```

### 6.2 Rate limiting (nginx-də)

```nginx
# /etc/nginx/conf.d/rate_limits.conf

# OTP send — 1 req/dəq/phone (app-də də var, lakin proxy səviyyəsində də qoruma)
limit_req_zone $binary_remote_addr zone=otp_send:10m rate=1r/m;

# OTP verify — 5 req/dəq
limit_req_zone $binary_remote_addr zone=otp_verify:10m rate=5r/m;

# Application init — 10 req/dəq/IP (anti-spam)
limit_req_zone $binary_remote_addr zone=app_init:10m rate=10r/m;

# Discount code validate — 10 req/dəq
limit_req_zone $binary_remote_addr zone=discount:10m rate=10r/m;

# Generic API — 60 req/dəq
limit_req_zone $binary_remote_addr zone=api_generic:10m rate=60r/m;
```

```nginx
location = /api/otp/send {
    limit_req zone=otp_send burst=1 nodelay;
    proxy_pass http://public-app:8000;
}
location = /api/applications/init {
    limit_req zone=app_init burst=3 nodelay;
    proxy_pass http://public-app:8000;
}
location /api/ {
    limit_req zone=api_generic burst=20 nodelay;
    proxy_pass http://public-app:8000;
}
```

### 6.3 Ekspert panel auth (gələcək PR-lar)

Hazırda app-də auth yoxdur. Bu plan-da **2 mərhələ** tövsiyə edilir:

**Mərhələ 1 (proxy səviyyəsində, dərhal):**
- Internal proxy-də Basic Auth və ya IP allowlist
- `auth_basic /etc/nginx/.htpasswd;`
- VPN tələb olunur

**Mərhələ 2 (app səviyyəsində, gələcək PR):**
- JWT və ya session cookie auth
- `internal/middleware/auth.go`
- Login endpoint (`POST /api/auth/login`)
- Token refresh
- Role-based access (ekspert vs admin)

Bu, PR #141 scope-xaricidir, lakin plan-a daxil edilməlidir.

### 6.4 Callback HMAC imzası

LW və SIMA callback-ləri (`/api/rdc/callback/*`) üçün HMAC imzası:

```go
// internal/middleware/callback_auth.go
func CallbackAuth(secret string, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        sig := r.Header.Get("X-Webhook-Signature")
        body, _ := io.ReadAll(r.Body)
        r.Body = io.NopCloser(bytes.NewReader(body))

        mac := hmac.New(sha256.New, []byte(secret))
        mac.Write(body)
        expected := hex.EncodeToString(mac.Sum(nil))

        if !hmac.Equal([]byte(sig), []byte(expected)) {
            http.Error(w, "invalid signature", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### 6.5 IP allowlist (callback proxy-də)

```nginx
location /api/rdc/callback/ {
    # Yalnız LW və SIMA-nın rəsmi IP-ləri
    allow 185.78.1.0/24;   # nümunə: LW IP range
    allow 94.20.20.0/24;   # nümunə: SIMA IP range
    deny  all;

    # Əlavə HMAC verify (app-səviyyəsində)
    proxy_pass http://internal-app:8001;
}
```

---

## 7. Konfiqurasiya Dəyişiklikləri (`.env`)

### 7.1 Yeni env var-lar

```bash
# ============= PR #141: Reverse Proxy =============

# Route mode: all | public | internal
ROUTE_MODE=all

# Trusted proxy CIDR-ləri (X-Forwarded-For parse üçün)
TRUSTED_PROXIES=127.0.0.1,10.0.0.0/8,172.16.0.0/12

# DB TLS
DB_ENCRYPT=true
DB_TRUST_SERVER_CERTIFICATE=false

# DB pool
DB_MAX_OPEN_CONNS=25
DB_MAX_IDLE_CONNS=5
DB_CONN_MAX_LIFETIME_S=300
DB_CONN_MAX_IDLE_S=120

# Migrations
MIGRATIONS_DIR=/app/migrations
MIGRATIONS_DROP_RECREATE=false   # PROD-DA MÜTLƏQ false

# Callback auth
CALLBACK_HMAC_SECRET=<random-64-char-hex>

# Soft10 widget (landing-də)
SOFT10_CUSTOMER_ID=lc_53a767d43ff6e7d09543006b49651e2c
```

### 7.2 Mövcud env var-ların prod dəyərləri

```bash
# Mock-ları deaktiv et
LW_USE_MOCK=false
LW_USE_STUB=false
OTP_USE_MOCK=false
SIMA_USE_MOCK=false
MYGOV_USE_MOCK=false
AZMK_USE_MOCK=false

# External servis URL-ləri (prod) — PR #378: bu servis-lər LW
# tərəfindən təqdim edilir, alpul.az subdomain-i DEYİL (öz
# domain-ləri ilə gəlir). Real URL-lər LW-dən alındıqda doldurulur.
LW_BASE_URL=<LW-nin verdiyi prod URL>
OTP_BASE_URL=<LW-nin verdiyi OTP/SMS URL>
SIMA_BASE_URL=<LW-nin verdiyi SIMA URL>
MYGOV_BASE_URL=<LW-nin verdiyi MyGov URL>
AZMK_BASE_URL=https://web.azmk.az:7077/LW_AKP/services/OnlineLendingService

# MyGov prod URL-ləri
MYGOV_REDIRECT_URI=https://alpul.az/api/mygov/permission-link
MYGOV_WEB_URL=https://alpul.az

# AZMK Basic Auth
AZMK_USERNAME=<prod-username>
AZMK_PASSWORD=<prod-password>
```

---

## 8. Deployment Artifaktları (Yeni)

### 8.1 Dockerfile

```dockerfile
# ============= Build stage =============
FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY source/go.mod source/go.sum ./
RUN go mod download

COPY source/ .
# Migrations fayllarını binary-yə embed et
# (Əgər go:embed migrations əlavə olunarsa — tövsiyə edilir)

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /rdc .

# ============= Runtime stage =============
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /rdc /app/rdc
COPY source/migrations /app/migrations

USER nonroot:nonroot
EXPOSE 8000

ENTRYPOINT ["/app/rdc"]
```

### 8.2 docker-compose.yml (dev)

```yaml
version: "3.9"

services:
  rdc-public:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "8000:8000"
    env_file: .env
    environment:
      ROUTE_MODE: public
      SERVER_ADDR: :8000
    depends_on:
      - db

  rdc-internal:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "8001:8001"
    env_file: .env
    environment:
      ROUTE_MODE: internal
      SERVER_ADDR: :8001
    depends_on:
      - db

  db:
    image: mcr.microsoft.com/mssql/server:2022-latest
    environment:
      ACCEPT_EULA: "Y"
      MSSQL_SA_PASSWORD: "YourStrong!Passw0rd"
      MSSQL_PID: "Developer"
    ports:
      - "1433:1433"
    volumes:
      - mssql-data:/var/opt/mssql

  nginx-public:
    image: nginx:alpine
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - ./nginx/public.conf:/etc/nginx/conf.d/default.conf
      - ./nginx/certs:/etc/nginx/certs
    depends_on:
      - rdc-public

  nginx-internal:
    image: nginx:alpine
    ports:
      - "8443:443"
    volumes:
      - ./nginx/internal.conf:/etc/nginx/conf.d/default.conf
      - ./nginx/certs:/etc/nginx/certs
    depends_on:
      - rdc-internal

volumes:
  mssql-data:
```

### 8.3 NGINX config nümunəsi (public)

```nginx
# /etc/nginx/conf.d/public.conf

upstream rdc_public {
    server rdc-public:8000;
    keepalive 32;
}

# Rate limit zones
limit_req_zone $binary_remote_addr zone=otp_send:10m       rate=1r/m;
limit_req_zone $binary_remote_addr zone=app_init:10m       rate=10r/m;
limit_req_zone $binary_remote_addr zone=discount:10m       rate=10r/m;
limit_req_zone $binary_remote_addr zone=api_generic:10m    rate=60r/m;

# HTTP → HTTPS redirect
server {
    listen 80;
    server_name alpul.az www.alpul.az;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name alpul.az;

    ssl_certificate     /etc/nginx/certs/alpul.crt;
    ssl_certificate_key /etc/nginx/certs/alpul.key;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;

    # Security headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;
    add_header X-Frame-Options "DENY" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://cdn.jsdelivr.net https://cdn.soft10.io; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data: https:; connect-src 'self'; frame-ancestors 'none'" always;

    # / → landing.html
    location = / {
        proxy_pass http://rdc_public/landing.html;
    }

    # Public statik fayllar
    location ~ ^/(landing|apply)\.html$ {
        proxy_pass http://rdc_public;
    }

    # Public API — OTP send (strict rate limit)
    location = /api/otp/send {
        limit_req zone=otp_send burst=1 nodelay;
        proxy_pass http://rdc_public;
    }

    # Public API — application init (rate limit)
    location = /api/applications/init {
        limit_req zone=app_init burst=3 nodelay;
        proxy_pass http://rdc_public;
    }

    # Public API — discount code validate
    location = /api/discount-codes/validate {
        limit_req zone=discount burst=5 nodelay;
        proxy_pass http://rdc_public;
    }

    # Public API — generic
    location /api/ {
        limit_req zone=api_generic burst=20 nodelay;
        proxy_pass http://rdc_public;
    }

    # Healthcheck (no rate limit)
    location = /healthz {
        proxy_pass http://rdc_public;
    }

    # EXPLICIT DENY — internal route-lar
    location ~ ^/(detail|index)\.html$ {
        return 403;
    }
    location ~ ^/api/(expert|admin|mock|router|lw|rdc/callback)/ {
        return 403;
    }
    location ~ ^/api/mygov/.*-(request|verify)$ {
        return 403;
    }

    # Proxy headers
    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Request-ID      $request_id;

    proxy_connect_timeout 5s;
    proxy_send_timeout    30s;
    proxy_read_timeout    30s;
}
```

---

## 9. İcra Planı (Phased Roadmap)

Bu plan-ı **5 ayrı PR-a** bölmək tövsiyə edilir. PR #141 yalnız **Phase 1** (bu sənəd) və **Phase 2** (infrastruktur dəyişiklikləri) əhatə edir.

### Phase 1 — Plan sənədi (PR #141, bu PR)
- ✅ Bu sənəd (`Docs/PR141_Global_Reverse_Proxy_Plan.md`)
- ✅ `.env.example` yenilənir (yeni env var-lar əlavə olunur, lakin default dəyərlərlə — prod-a təsiri yoxdur)

### Phase 2 — App-səviyyəsi hazırlıq (PR #142)
- `ROUTE_MODE` env var və router qruplaşdırması
- `/healthz` endpoint
- Server timeout-ları (`ReadHeaderTimeout`, və s.)
- `X-Forwarded-For` parse (trusted proxy config ilə)
- DB pool tune (`SetMaxOpenConns` və s.)
- DB TLS konfiqurasiyası (`DB_ENCRYPT`)
- Migrations dir konfiqurasiyası (`MIGRATIONS_DIR`)
- `/` kök route → landing.html

### Phase 3 — Deployment artifaktları (PR #143)
- `Dockerfile` (multi-stage build)
- `docker-compose.yml` (dev)
- `Makefile` (build/run/test targets)
- `.dockerignore`
- NGINX config nümunələri (`nginx/public.conf`, `nginx/internal.conf`)

### Phase 4 — Təhlükəsizlik (PR #144)
- Security headers middleware (app-səviyyəsində, opsional)
- Callback HMAC verification middleware
- Rate limit middleware (app-səviyyəsində, nginx-i əvəz edə bilər)
- Mock route-ları prod build-də exclude

### Phase 5 — CI/CD (PR #145)
- `.github/workflows/ci.yml` (lint, test, build)
- `.github/workflows/deploy.yml` (deploy to staging/prod)
- Container registry push
- Automated migrations run

### Phase 6 — Self-host CDN (PR #146+, opsional)
- Tailwind production build
- Alpine.js self-host
- Inter font self-host
- Soft10 widget parametrization

### Phase 7 — App-level auth (PR #147+, gələcək)
- JWT auth middleware
- Login endpoint
- Session/refresh token
- Role-based access control

---

## 10. Risklər və Azaldıcı Tədbirlər

| Risk | Ehtimal | Təsir | Azaldıcı tədbir |
|------|---------|-------|-----------------|
| Səhv nginx konfiq ekspert paneli açıq saxlayır | orta | **yüksək** (data leak) | 2 instans + app-səviyyəsində `ROUTE_MODE=public` |
| `MIGRATIONS_DROP_RECREATE=true` prod-da qalır | aşağı | **çox yüksək** (data loss) | `.env.example`-də `false`, Dockerfile-da məcburi yoxlama |
| AZMK `InsecureSkipVerify` aktiv qalır | yüksək | orta (MITM risk) | AZMK CA-sını trust store-a əlavə et |
| CDN xarici ölkədədir, gecikmə | yüksək | aşağı (UI gec açılır) | Self-host (Phase 6) |
| Callback IP-ləri dəyişir | orta | orta (callback-lər düşür) | HMAC + IP allowlist hər ikisi |
| DB pool tükənir (yüksək yük) | orta | yüksək (500 errors) | `SetMaxOpenConns=25`, monitoring |
| X-Forwarded-For spoofing | orta | aşağı (log poisoning) | Trusted proxy CIDR yoxlaması |
| CORS cross-origin istifadə lazımdır | aşağı | orta | nginx-də `add_header Access-Control-Allow-Origin` |
| Soft10 customer ID leaked | aşağı | aşağı | Templating, env-dən oxu |

---

## 11. Test Strategiyası

### 11.1 Lokal test (dev)

```bash
# docker-compose up
make dev-up

# curl ile test:
curl -k https://localhost/                    # → landing.html
curl -k https://localhost/apply.html          # → apply.html
curl -k https://localhost/api/expert/queue    # → 403 Forbidden
curl -k https://localhost/detail.html         # → 403 Forbidden
curl -k https://localhost:8443/detail.html    # → 200 (internal proxy)
curl -k https://localhost:8443/api/expert/queue  # → 200 (internal proxy)
```

### 11.2 Prod pre-deploy checklist

- [ ] `ROUTE_MODE=public` set olunub (public instans)
- [ ] `MIGRATIONS_DROP_RECREATE=false`
- [ ] `DB_ENCRYPT=true`
- [ ] `MYGOV_REDIRECT_URI` prod URL
- [ ] `MYGOV_WEB_URL` prod URL
- [ ] Bütün `*_USE_MOCK=false`
- [ ] `AZMK_USERNAME`/`AZMK_PASSWORD` set olunub
- [ ] `CALLBACK_HMAC_SECRET` set olunub
- [ ] `TRUSTED_PROXIES` set olunub
- [ ] NGINX security headers aktiv
- [ ] NGINX rate limit zones aktiv
- [ ] TLS sertifikatı quraşdırılıb (Let's Encrypt və ya ACM)
- [ ] DNS A record `alpul.az` → public proxy IP
- [ ] DNS A record `expert.alpul.az` → internal proxy IP (VPN-only)
- [ ] Healthcheck `/healthz` 200 qaytarır
- [ ] Log lar JSON format-da mərkəzi log server-ə göndərilir

### 11.3 Post-deploy smoke test

```bash
# Public
curl -sI https://alpul.az/ | head -5          # 200, HSTS, X-Frame-Options
curl -sI https://alpul.az/detail.html         # 403
curl -sI https://alpul.az/api/expert/queue    # 403
curl -s https://alpul.az/healthz              # {"status":"ok",...}

# Internal (VPN-dən)
curl -sI https://expert.alpul.az/detail.html  # 200
curl -sI https://expert.alpul.az/api/expert/queue  # 200

# Rate limit test
for i in {1..5}; do
    curl -s -o /dev/null -w "%{http_code}\n" \
        -X POST https://alpul.az/api/otp/send \
        -H "Content-Type: application/json" \
        -d '{"phone":"501234567"}'
done
# Gözlənilən: 429 (Too Many Requests) 2-ci request-dən sonra
```

---

## 12. Monitorinq və Alerting

### 12.1 Metrikalar (gələcək PR)

- HTTP request count by status code
- HTTP request duration histogram
- DB connection pool utilization
- Rate limit hit count
- AZMK/LW/MyGov/SIMA/OTP outbound request duration və error rate
- Active application count by status

**Implementasiya:** Prometheus middleware (`/metrics` endpoint) — Phase 7+.

### 12.2 Log lar

Hazırkı logger JSON formatında loglayır (`slog.JSONHandler`). Tövsiyə:
- Log lar mərkəzi log server-a (ELK, Loki, və ya CloudWatch) göndərilsin
- Hər log entry-də `request_id` var (proxy-dən gəlir və ya app generasiya edir)
- `remote_addr` artıq real client IP-si olacaq (Phase 2-dən sonra)

### 12.3 Alert-lər

| Alert | Şərt |
|-------|------|
| Public app down | `/healthz` 5 dəq ərzində 200 qaytarmır |
| DB connection exhausted | `db.Stats().WaitCount` > 0 |
| Rate limit excessive | Rate limit hit count > 1000/dəq |
| AZMK unreachable | AZMK outbound error rate > 10% |
| TLS cert expiring | Sertifikat 14 gündən az müddətə bitir |

---

## 13. Nə Dəyişmir (Out of Scope)

Aşağıdakılar bu planın scope-xaricidir:

- ❌ App-level authentication (JWT/session) — Phase 7
- ❌ Multi-tenant/multi-region support
- ❌ Mobile app API (hazırda yalnız web)
- ❌ Websocket/SSE support
- ❌ GraphQL
- ❌ API versioning (`/v1/api/...`)
- ❌ OpenAPI/Swagger specification
- ❌ Automated load testing
- ❌ Blue-green deployment infrastructure
- ❌ Database read replicas

Bu elementlər gələcək PR-larda ayrıca planlaşdırılacaq.

---

## 14. Qərar Qeydləri (Decision Log)

| # | Qərar | Səbəb | Alternativlər |
|---|-------|-------|---------------|
| 1 | 2 instans (public + internal) | Risk azaldılması: public DDoS olsa, ekspert panel işləyir | 1 instans + path filtering (daha sadə, lakin riskli) |
| 2 | Eyni Go binary, `ROUTE_MODE` ilə | Kod deduplikasiyası, sadə build | 2 ayrı binary (daha çox maintenance) |
| 3 | NGINX reverse proxy | Mature, geniş istifadə, sadə konfiqurasiya | Cloudflare Workers, Traefik, Caddy |
| 4 | TLS termination proxy-də | Sertifikat idarəsi mərkəzləşir, app sadə qalır | App-də TLS (daha çox iş) |
| 5 | Security headers proxy-də | App koduna təsiri yoxdur, dərhal dəyişdirilə bilər | App middleware (daha çox kod) |
| 6 | Rate limit proxy-də | App-dən əvvəl bloklayır, app yükü azalır | App middleware (daha dəqiq, lakin yavaş) |
| 7 | HMAC callback auth | IP allowlist tək başına kifayət deyil (IP spoof) | mTLS (daha güclü, lakin kompleks) |
| 8 | `MIGRATIONS_DIR` env var | Container-də relative path işləmir | embed.FS (daha yaxşı, lakin kod dəyişikliyi tələb edir — Phase 2-də nəzərə alınacaq) |
| 9 | Self-host CDN gecikdirilir | Hazırda CDN-lər işləyir, prioritet aşağı | Dərhal self-host (UI risk) |
| 10 | App-level auth gecikdirilir | Proxy səviyyəsində Basic Auth + VPN kifayət edir | Dərhal JWT (çox iş, gecikmə) |

---

## 15. Növbəti Addımlar

1. **PR #141 merge** — bu sənəd təsdiq olunur
2. **PR #142** — Phase 2 app-səviyyəsi dəyişiklikləri (`ROUTE_MODE`, `/healthz`, timeout-lar, və s.)
3. **PR #143** — Phase 3 deployment artifaktları (Dockerfile, docker-compose, NGINX configs)
4. **PR #144** — Phase 4 təhlükəsizlik (HMAC, security headers middleware, mock exclude)
5. **PR #145** — Phase 5 CI/CD pipeline
6. **Manual deployment** — staging environment-da test
7. **PR #146+** — Phase 6 (self-host CDN) və Phase 7 (app auth) — prioritetə görə

---

## Əlavə A: Endpoint Tam Siyahısı və Təsnifatı

| Endpoint | Method | Route Mode | Auth Tələbi | Proxy |
|----------|--------|------------|-------------|-------|
| `/healthz` | GET | hər ikisi | yoxdur | hər ikisi |
| `/` | GET | hər ikisi | yoxdur | rewrite → landing.html (public) və ya index.html (internal) |
| `/landing.html` | GET | hər ikisi | yoxdur | public |
| `/apply.html` | GET | hər ikisi | yoxdur | public |
| `/detail.html` | GET | internal | VPN/Basic Auth | internal |
| `/index.html` | GET | internal | VPN/Basic Auth | internal |
| `/api/applications` | POST | hər ikisi | yoxdur | public |
| `/api/applications/init` | POST | hər ikisi | yoxdur | public |
| `/api/applications/init/verify` | POST | hər ikisi | yoxdur | public |
| `/api/applications/offer` | GET | hər ikisi | yoxdur | public |
| `/api/applications/{id}` | GET | hər ikisi | yoxdur | public (read-only) |
| `/api/applications/{id}/customer-confirm` | POST | hər ikisi | yoxdur | public |
| `/api/applications/{id}/contacts` | PUT | hər ikisi | yoxdur | public |
| `/api/applications/{id}/timer` | PUT | hər ikisi | yoxdur | public |
| `/api/applications/{id}/complete` | PUT | internal | auth tələb | internal |
| `/api/applications/{id}/status` | GET/PUT | GET: public, PUT: internal | auth (PUT) | internal (PUT) |
| `/api/applications/{id}/checks` | GET | internal | auth | internal |
| `/api/applications/{id}/loan-status` | GET | internal | auth | internal |
| `/api/expert/queue` | GET | internal | auth | internal |
| `/api/expert/{id}` | GET | internal | auth | internal |
| `/api/expert/{id}/approve` | PUT | internal | auth | internal |
| `/api/expert/{id}/reject` | PUT | internal | auth | internal |
| `/api/admin/feature-flags` | GET | internal | admin auth | internal |
| `/api/admin/feature-flags/{key}` | GET/PUT | internal | admin auth | internal |
| `/api/otp/send` | POST | hər ikisi | yoxdur | public (rate limited) |
| `/api/otp/verify` | POST | hər ikisi | yoxdur | public (rate limited) |
| `/api/discount-codes/validate` | GET | hər ikisi | yoxdur | public (rate limited) |
| `/api/mygov/permission-link` | POST | internal | auth | internal |
| `/api/mygov/fetch-data` | POST | internal | auth | internal |
| `/api/applications/{id}/mygov-*-request` | POST | internal | auth | internal |
| `/api/applications/{id}/mygov-*-verify` | POST | internal | auth | internal |
| `/api/router/*` | ALL | internal | auth | internal |
| `/api/lw/*` | ALL | internal | auth | internal |
| `/api/rdc/callback/*` | POST | internal | HMAC + IP | callback proxy |
| `/api/mock/*` | ALL | `ROUTE_MODE=all` (dev only) | yoxdur | deny in prod |

---

## Əlavə B: Mövcud Env Var-ların Tam Siyahısı

Aşağıdakı env var-lar artıq mövcuddur və prod-da mütləq düzgün dəyər almalıdır:

| Var | Default | Prod tələbi |
|-----|---------|-------------|
| `DB_HOST` | tələb olunur | prod DB host |
| `DB_PORT` | 1433 | 1433 |
| `DB_USER` | tələb olunur | prod DB user |
| `DB_PASSWORD` | tələb olunur | prod DB password |
| `DB_NAME` | RDC | RDC |
| `SERVER_ADDR` | :8000 | :8000 (public), :8001 (internal) |
| `MIGRATIONS_DROP_RECREATE` | **true** | **MÜTLƏQ false** |
| `LOG_LEVEL` | info | info (və ya warn) |
| `LW_BASE_URL` | http://localhost:8080 | LW-nin verdiyi URL (PR #378 — alpul.az deyil) |
| `LW_API_KEY` | (boş) | prod API key |
| `LW_USE_MOCK` | **true** | **false** |
| `LW_USE_STUB` | false | false |
| `LW_STUB_PORT` | 8090 | (işlənmir) |
| `LW_TIMEOUT_S` | 30 | 30 |
| `OTP_BASE_URL` | http://localhost:8081 | LW-nin verdiyi URL (alpul.az deyil) |
| `OTP_API_KEY` | (boş) | prod API key |
| `OTP_SENDER` | RDC | ALPUL |
| `OTP_USE_MOCK` | **true** | **false** |
| `OTP_TIMEOUT_S` | 10 | 10 |
| `SIMA_BASE_URL` | http://localhost:8082 | LW-nin verdiyi URL (alpul.az deyil) |
| `SIMA_API_KEY` | (boş) | prod API key |
| `SIMA_USE_MOCK` | **true** | **false** |
| `SIMA_TIMEOUT_S` | 15 | 15 |
| `MYGOV_BASE_URL` | http://localhost:8083 | LW-nin verdiyi URL (alpul.az deyil) |
| `MYGOV_API_KEY` | (boş) | prod API key |
| `MYGOV_USE_MOCK` | **true** | **false** |
| `MYGOV_TIMEOUT_S` | 15 | 15 |
| `MYGOV_CLIENT_ID` | (boş) | prod UUID |
| `MYGOV_REDIRECT_URI` | webhook.site | **prod URL** |
| `MYGOV_WEB_URL` | netlify.app | **prod URL** |
| `MIN_OFFICIAL_INCOME_AZN` | 300.0 | 300.0 |
| `AZMK_BASE_URL` | https://web.azmk.az:7077/... | eyni |
| `AZMK_BRANCH_CODE` | HO | HO |
| `AZMK_PRODUCT_ID` | L07 | L07 |
| `AZMK_CARD_EXPIRING` | 2030-01-01 | 2030-01-01 |
| `AZMK_DISBURSEMENT_FEE` | 0.0 | 0.0 |
| `AZMK_USE_MOCK` | **true** | **false** |
| `AZMK_TIMEOUT_S` | 30 | 30 |
| `AZMK_USERNAME` | (boş) | prod username |
| `AZMK_PASSWORD` | (boş) | prod password |

**Qalın** işarələnmiş dəyərlər prod-da mütləq dəyişdirilməlidir.

---

**Sənəd versiyası:** 1.0
**Son yenilənmə:** PR #141
**Müəllif:** main agent
