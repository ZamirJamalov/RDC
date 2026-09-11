# TODO

Bu fayl layihənin gözləyən tasklarını izləmək üçün qlobal siyahıdır.
Konvensiya:

- Yeni task `- [ ]` kimi əlavə olunur (ən yuxarıya, "Backlog" bölməsinə)
- Hall olunanda `- [x]` edilir və **"Done"** bölməsinə keçirilir (yeni bitənlər üstdə)
- Hər taskda PR № və tarix qeyd olunur ki, tarixçə itmesin

---

## Backlog

### alpul.az aktivləşəndə keçid (domin 72 saat gözləmədədir)

- [ ] `config.go` — `MYGOV_WEB_URL` default-u `https://alpul.az/mygov.html` olmalı
      (hazırda müvəqqəti `https://185.161.225.102/mygov.html` — PR #492)
- [ ] Caddy — `alpul.az` blokunun `mygov.html`-i problemsiz serve etdiyini yoxla
      (bloklanan path-lərə düşmür, əlavə dəyişiklik lazım deyil — sadəcə test)
- [ ] Netlify səhifəsi söndürülsün (`lively-pie-17ab5c.netlify.app`)
- [ ] İstifadəçilərə gedən köhnə SMS-lərdəki Netlify linkinin ömrü — MyGov
      callback (`azmk.az/RDC/callback.php`) dəyişmir, keçid transparentdir
- [ ] 185 IP-nin public istifadəsini dayandır (əgər başqa iş üçün lazım deyilsə,
      Caddy bloku məhdudlaşdırıla bilər — Caddy əl ilə idarə olunur)

### MyGov client_id uyğunsuzluğu (yoxlanılmalı)

- [ ] `deploy/rdc.env` → `MYGOV_CLIENT_ID=82b54b39-...` ilə `web/mygov.html`
      içindəki `cbc7c4da-...` FƏRQLİDİR. Hansı doğrudur? Netlify versiyasında
      `cbc7c4da` işlənirdi (real axın onunla işləyib) — təsdiqlənəndə
      `rdc.env`-dəki də yenilənsin ki, DB-ə yazılan reference deeplink də düzgün olsun

---

## Done

- [x] mygov.html səhifəsinin Netlify-dan RDC-yə köçürülməsi — PR #492
- [x] SMS-dəki linkin `https://185.161.225.102/mygov.html`-ə keçirilməsi — PR #492
      (`MYGOV_WEB_URL` default; env-də override yoxdur)
