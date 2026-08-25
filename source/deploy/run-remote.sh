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
# env ssh stdin ilə gedir → su içində . /dev/stdin oxuyur → set -a export edir
# → nohup background-a düşür. { ... & } qrupu vacibdir: & yalnız nohup-u
# background-a salır (yoxsa stdin /dev/null olar və env boş gələr).
ssh "${TARGET}" '
  pkill -x rdc 2>/dev/null && { echo "== köhnə rdc dayandırıldı"; sleep 1; }
  su -s /bin/bash rdc -c "cd /opt/rdc && set -a && . /dev/stdin && set +a && { nohup /opt/rdc/rdc >> /opt/rdc/monitoring/app.log 2>&1 < /dev/null & }"
  sleep 2
  if pgrep -x rdc >/dev/null; then
      echo "== rdc İŞLƏYİR ✓  PID: $(pgrep -x rdc)"
      tail -3 /opt/rdc/monitoring/app.log
  else
      echo "XƏTA: başlamadı — son loglar:"; tail -10 /opt/rdc/monitoring/app.log; exit 1
  fi
' < "${ENV_FILE}"
