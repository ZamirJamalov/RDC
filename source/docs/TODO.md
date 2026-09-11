# TODO

Bu fayl layihənin gözləyən tasklarını izləmək üçün qlobal siyahıdır.
Konvensiya:

- Yeni task `- [ ]` kimi əlavə olunur (ən yuxarıya, "Backlog" bölməsinə)
- Hall olunanda `- [x]` edilir və **"Done"** bölməsinə keçirilir (yeni bitənlər üstdə)
- Hər taskda PR № və tarix qeyd olunur ki, tarixçə itmesin

---

## Backlog

### alpul.az aktivləşəndə keçid (domen 72 saat gözləmədədir) — YÜKSƏK

- [ ] `config.go` — `MYGOV_WEB_URL` default-u `https://alpul.az/mygov.html` olmalı
      (hazırda müvəqqəti `https://linkmygov.netlify.app/` — PR #495; 185 IP
      self-signed sertifikat xəbərdarlığı səbəbilə Netlify-a qaytarıldı, öz
      `web/mygov.html`-imiz hazırdır — alpul.az-da host olunacaq)
- [ ] Caddy — `alpul.az` blokunun `mygov.html`-i problemsiz serve etdiyini yoxla
      (bloklanan path-lərə düşmür, əlavə dəyişiklik lazım deyil — sadəcə test)
- [ ] Netlify səhifəsi söndürülsün (`linkmygov.netlify.app`)
- [ ] 185 IP-nin public istifadəsini dayandır (əgər başqa iş üçün lazım deyilsə,
      Caddy bloku məhdudlaşdırıla bilər — Caddy əl ilə idarə olunur)
      Not: IP-də `tls internal` xəbərdarlığı var — alpul.az keçidi bunu da aradan
      qaldırır (real LE sertifikat)

### MyGov konfiqurasiya uyğunsuzluqları — YÜKSƏK (yoxlanılmalı)

- [ ] `deploy/rdc.env` → `MYGOV_CLIENT_ID=82b54b39-...` ilə `web/mygov.html`
      içindəki `cbc7c4da-...` FƏRQLİDİR. Hansı doğrudur? Netlify versiyasında
      `cbc7c4da` işlənirdi (real axın onunla işləyib) — təsdiqlənəndə
      `rdc.env`-dəki də yenilənsin ki, DB-ə yazılan reference deeplink də düzgün olsun
- [ ] `deploy/rdc.env` → `MYGOV_REDIRECT_URI=https://webhook.site/...` (test!)
      ilə səhifədəki `https://azmk.az/RDC/callback.php` FƏRQLİDİR — prod env-də
      webhook.site qalmamalıdır

### Təhlükəsizlik / infra — ORTA

- [ ] PR #488 deploy-dan sonra real test: sehv serial → `SERIAL_MISMATCH` rəddi
      (live audit-logda təsdiq: PERSONAL_INFO result=0 → reject, KYC çağrılmır)
- [ ] `SERIAL_MISMATCH_LIMIT` / `SERIAL_MISMATCH_WINDOW_HOURS` env dəyişənləri —
      hazırda kodda sabit (3 cəhd / 1 saat — PR #490), ops tənzimləməsi üçün
- [ ] `deploy/rdc.env` (laptop) → `MIGRATIONS_DROP_RECREATE=true` hal-hazırda
      qalır — 172.17.1.24 (prod DB) qarşı istifadə EDİLMƏMƏLİ; localhost-dan
      başqa DB-yə deploy olanda bədbəxt silinmə riski. Xəbərdarlıq şərhi env-də

### Məhsul təkmilləşmələri — ORTA/AŞAĞI

- [ ] KYC sorğusunda real adlar: `runAzmkKycAndPartner` `FirstName/LastName`
      üçün `"-"` placeholder göndərir; identiklik qapısından (PR #487) sonra
      real ad mövcuddur — göndərmək AZMK match-ini yaxşılaşdıra bilər
- [ ] KYC polling sinxron bloklayır: `/init/verify` HTTP sorğusu müştəri KYC-i
      təsdiqləyənə qədər (maks 3 dəq) açıq qalır — paralel yüksək yükdə
      connection pool təzyiqi (max_open_conns=50). Asinxron/status-poll
      arxitekturaya keçid dəyərləndirilə bilər
- [ ] `card_number` server-side format validasiyası (16 rəqəm) — hazırda yalnız
      frontend-də; SQLi riski yoxdur (parameterized), data keyfiyyəti üçün
- [ ] `mygov.html` fallback-da App Store / Google Play düymələri — tətbiq
      quraşdırılmayıbsa istifadəçini mağazaya yönəltmək
- [ ] Təmiz `/mygov` route (uzantısız) — Go route və ya Caddy redir;
      `/mygov.html` də işləməyə davam edir, qırılma yoxdur (kosmetik)

### Texniki borc — AŞAĞI

- [ ] `internal/service` paketində 22 pre-existing test failure (LW/phase1
      infra) — main-də də eynidir; təmizlənməli və ya skip marker ilə
      sənədləşdirilməli (yeni PR-lərin 0-yeni-fail meyarını asanlaşdırar)
- [ ] `pkg/mygov/crypto.go` → `BuildWebURL` dead code (heç bir yerdə çağırılmır;
      PR #492-də mygov.html query-param dəstəyi əlavə olundu — istifadə olunacaqsa
      saxla, yoxsa sil)

---

## Done

- [x] mygov.html səhifəsinin Netlify-dan RDC-yə köçürülməsi — PR #492
- [x] SMS-dəki linkin müvəqqəti `https://185.161.225.102/mygov.html` — PR #493
      (öz mygov.html-imizdən serve)
- [x] SMS linkinin `https://linkmygov.netlify.app/`-ə qaytarılması — PR #495
      (185-in self-signed sertifikat xəbərdarlığı müştərilər üçün əlverişsizdir;
      alpul.az aktivləşəndə öz səhifəmizə qayıdacağıq — yuxarıdakı keçid taskı)
      (`MYGOV_WEB_URL` default; env-də override yoxdur)
