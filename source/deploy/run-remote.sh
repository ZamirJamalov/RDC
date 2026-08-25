#!/usr/bin/env bash
#
# run-remote.sh — RDC app-ı serverdə başladır (laptopdan işlədilir)
#
# İstifadə:  bash deploy/run-remote.sh root@server
#
# Nə edir (qısa):
#   rdc.env (laptopda) → ssh kanalı → app-in env-i → nohup ilə başlayır
#   (serverdə heç bir fayla yazılmır; terminalı bağlasanız da app işləyir;
#    loglar /opt/rdc/monitoring/app.log-a → Loki)
#
# Tələb: root kimi bağlanın; serverdə install.sh bir dəfə işlədilib olsun.

set -euo pipefail

TARGET="${1:?İstifadə: bash deploy/run-remote.sh root@server}"
ENV_FILE="$(cd "$(dirname "$0")" && pwd)/rdc.env"

[ -f "${ENV_FILE}" ] || { echo "XƏTA: ${ENV_FILE} yoxdur (cp env.example rdc.env)"; exit 1; }
grep -q '^MIGRATIONS_DROP_RECREATE=true' "${ENV_FILE}" && echo "⚠ XƏBƏRDARLIQ: DB SİLİNƏCƏK (MIGRATIONS_DROP_RECREATE=true)!" || true

# ===================== ƏSAS HİSSƏ =====================
# Ardıcıllıq (PR #303 düzəlişi):
#   1) env ROOT shell-də oxunur:        set -a; . /dev/stdin; set +a
#   2) su -m env-i rdc-yə ÖTÜRÜR (preserve) — proses env-ilə doğulur
#   3) { nohup ... & } — yalnız nohup background-a düşür
# Niyə env su-dan ƏVVƏL oxunur: ssh stdin-i root-a aid pipe-dır (mode 600),
# rdc istifadəçisi onu /dev/stdin-dən AÇA BİLMƏZ ("Permission denied").
ssh "${TARGET}" '
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
' < "${ENV_FILE}"
