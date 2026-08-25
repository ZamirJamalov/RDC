#!/usr/bin/env bash
#
# run-remote.sh — RDC app-ı serverdə başladır (laptopdan işlədilir)
#
# İstifadə:
#   bash deploy/run-remote.sh root@server   # real server (ssh)
#   sudo bash deploy/run-remote.sh local    # LOKAL test — PR #310: ssh/sshd/açar tələb etmir
#
# Nə edir (qısa):
#   rdc.env (laptopda) → [ssh kanalı | lokal] → app-in env-i → nohup ilə başlayır
#   (serverdə heç bir fayla yazılmır; terminalı bağlasanız da app işləyir;
#    loglar /opt/rdc/monitoring/app.log-a → Loki)
#
# Tələb: root (uzaqda root@… ssh, lokaldа sudo); serverdə install.sh bir dəfə işlədilib olsun.

set -euo pipefail

TARGET="${1:?İstifadə: bash deploy/run-remote.sh root@server | local}"
ENV_FILE="$(cd "$(dirname "$0")" && pwd)/rdc.env"

[ -f "${ENV_FILE}" ] || { echo "XƏTA: ${ENV_FILE} yoxdur (cp env.example rdc.env)"; exit 1; }
grep -q '^MIGRATIONS_DROP_RECREATE=true' "${ENV_FILE}" && echo "⚠ XƏBƏRDARLIQ: DB SİLİNƏCƏK (MIGRATIONS_DROP_RECREATE=true)!" || true

# PR #309: 'su -m rdc' (parolsuz) yalnız ROOT ilə mümkündür (root@ ssh və ya sudo local).
case "${TARGET}" in
    root@*|local) ;;
    *)
        echo "[UYARI] Target root deyil ('${TARGET}') — 'su rdc' parol soruşub çökəcək."
        echo "[UYARI] Doğrusu:  bash deploy/run-remote.sh root@server  |  sudo bash deploy/run-remote.sh local"
        ;;
esac

# ===================== ƏSAS HİSSƏ =====================
# Ardıcıllıq (PR #303 düzəlişi):
#   1) env ROOT shell-də oxunur:        set -a; . /dev/stdin; set +a
#   2) su -m env-i rdc-yə ÖTÜRÜR (preserve) — proses env-ilə doğulur
#   3) { nohup ... & } — yalnız nohup background-a düşür
# Niyə env su-dan ƏVVƏL oxunur: stdin root-a aiddir (ssh pipe / lokal fayl redirect),
# rdc istifadəçisi onu /dev/stdin-dən AÇA BİLMƏZ ("Permission denied").
#
# PR #310: skript mətni REMOTE_SCRIPT dəyişənində saxlanılır və hər iki rejimdə
# eyni icra olunur: ssh (uzaq) və ya bash -c (lokal). Daxilində tək dırnaq YAZILMIR!
REMOTE_SCRIPT='
  # PR #309: root yoxlaması — su rdc (parolsuz) yalnız root üçün işləyir.
  # Əks halda su Password: soruşur (rdc-in parolu yoxdur) → Authentication failure.
  if [ "$(id -u)" -ne 0 ]; then
      echo "XƏTA: seans ROOT deyil — su rdc üçün root lazımdır."
      echo "İstifadə: bash deploy/run-remote.sh root@bu-server  |  sudo bash deploy/run-remote.sh local"
      exit 1
  fi
  pkill -x rdc 2>/dev/null && { echo "== köhnə rdc dayandırıldı"; sleep 1; }
  stat -c "== binary build vaxtı: %y (run-remote.sh build ETMİR!)" /opt/rdc/rdc 2>/dev/null || true
  set -a; . /dev/stdin; set +a
  su -m -s /bin/bash rdc -c "cd /opt/rdc && { nohup /opt/rdc/rdc >> /opt/rdc/monitoring/app.log 2>&1 < /dev/null & }"
  sleep 2
  if pgrep -x rdc >/dev/null; then
      echo "== rdc İŞLƏYİR ✓  PID: $(pgrep -x rdc)"
      tail -3 /opt/rdc/monitoring/app.log
  else
      echo "XƏTA: başlamadı — son loglar:"; tail -10 /opt/rdc/monitoring/app.log; exit 1
  fi
'

# PR #310: lokal rejim — localhost testi üçün ssh/sshd/açar tələb etmir.
# Eyni REMOTE_SCRIPT bu maşında icra olunur; root (sudo) tələb edir (su rdc üçün).
if [ "${TARGET}" = "local" ]; then
    if [ "$(id -u)" -ne 0 ]; then
        echo "XƏTA: lokal rejim root tələb edir: sudo bash deploy/run-remote.sh local"
        exit 1
    fi
    bash -c "${REMOTE_SCRIPT}" < "${ENV_FILE}"
else
    ssh "${TARGET}" "${REMOTE_SCRIPT}" < "${ENV_FILE}"
fi
