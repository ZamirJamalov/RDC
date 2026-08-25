#!/usr/bin/env bash
#
# run-remote.sh — RDC app-ı serverdə 1 əmrlə başlat (PR #298)
#
# HARADA İŞLƏDİLİR: LAPTOPDA (repo-nun source/ qovluğundan)
#
# İSTİFADƏ:
#   bash deploy/run-remote.sh user@server
#   Nümunə:  bash deploy/run-remote.sh root@203.0.113.10
#
# NƏ EDİR:
#   1. Laptopdakı deploy/rdc.env faylını yoxlayır (məcburi açarlar,
#      təhlükəli MIGRATIONS_DROP_RECREATE xəbərdarlığı)
#   2. Serverdə köhnə rdc prosesini dayandırır (pkill -x rdc)
#   3. rdc.env məzmununu şifrələnmiş ssh kanalı (stdin) ilə ötürür:
#        - serverdə HEÇ BİR FAYLA yazılmır (disk təmiz qalır)
#        - bash_history-yə düşmür
#        - birbaşa prosesin env-inə düşür
#   4. App-ı rdc istifadəçisi ilə nohup-la başladır:
#        - ssh/terminal bağlansa da proses yaşayır
#        - loglar /opt/rdc/monitoring/app.log-a → Promtail → Loki → Grafana
#          (zəncir dəyişmir — bax: DEPLOYMENT.md bölmə 5)
#   5. Prosesin qalxdığını yoxlayır + son logları göstərir
#
# TƏLƏBLƏR (serverdə, bir dəfə — install.sh ilə):
#   /opt/rdc/rdc binary, rdc istifadəçisi, /opt/rdc/monitoring/ stack-i
#
# QEYD: root kimi bağlanmaq ən sadədir (su parol soruşmur).
#       Qeyri-root istifadəçi üçün parolsuz sudo (sudo -n) tələb olunur.

set -euo pipefail

TARGET="${1:-}"
if [[ -z "${TARGET}" ]]; then
    echo "İstifadə: bash deploy/run-remote.sh user@server"
    echo "Nümunə:   bash deploy/run-remote.sh root@203.0.113.10"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ENV_FILE:-${SCRIPT_DIR}/rdc.env}"

# Server tərəfi yolları (test üçün env ilə üstündən yazıla bilir)
RDC_HOME="${RDC_HOME:-/opt/rdc}"
RDC_BIN="${RDC_BIN:-${RDC_HOME}/rdc}"
MON_DIR="${MON_DIR:-${RDC_HOME}/monitoring}"
RDC_LOG="${RDC_LOG:-${MON_DIR}/app.log}"
RDC_NAME="$(basename "${RDC_BIN}")"

# ------------------------- 1) Lokal yoxlama -------------------------
if [[ ! -f "${ENV_FILE}" ]]; then
    echo "XƏTA: ${ENV_FILE} tapılmadı."
    echo "Yarat:  cp ${SCRIPT_DIR}/env.example ${ENV_FILE}"
    echo "və parolları doldur (BU FAYL GIT-Ə GETMİR — .gitignore-dadır)."
    exit 1
fi

for key in DB_HOST DB_USER DB_PASSWORD; do
    if ! grep -qE "^${key}=" "${ENV_FILE}"; then
        echo "XƏTA: ${key} ${ENV_FILE} faylında yoxdur (şablon: env.example)"
        exit 1
    fi
done

if grep -qE '^MIGRATIONS_DROP_RECREATE="?true' "${ENV_FILE}"; then
    echo "XƏBƏRDARLIQ: MIGRATIONS_DROP_RECREATE=true — app başlayanda BÜTÜN DATA SİLİNƏCƏK!"
    read -rp "Davam edilsin? (y/N): " ans
    [[ "${ans}" == "y" || "${ans}" == "Y" ]] || exit 1
fi

# ------------------------- 2) Server tərəfi -------------------------
# Qeyd: env faylı ssh STDIN-dən gedir; serverdə fayl yaradılmır.
# DROP: root → su (parol soruşmur), qeyri-root → sudo -n (parolsuz sudo).
#
# Qeyd: aşağıdakı başlanğıc əmrində { ...; } qrupu vacibdir — & yalnız nohup-u
# background-a salır. Əks halda qeyri-interaktiv shell asinxron siyahının
# stdin-ini /dev/null edir → . /dev/stdin boş oxuyar → env prosesə çatmaz.
REMOTE_CMD="
set -e
if [ ! -x '${RDC_BIN}' ]; then
    echo 'XETA: ${RDC_BIN} tapilmadi — evvel binary kochurun ve ya install.sh ishledin.'
    exit 1
fi
if [ ! -d '${MON_DIR}' ]; then
    echo 'XETA: ${MON_DIR} yoxdur — monitorinq ucun: sudo bash deploy/install.sh'
    exit 1
fi
if [ \"\$(id -u)\" = 0 ]; then
    DROP='su -m -s /bin/bash rdc -c'
else
    sudo -n true 2>/dev/null || { echo 'XETA: parolsuz sudo (sudo -n) ishlemir — root kimi baglanin.'; exit 1; }
    DROP='sudo -n -u rdc bash -c'
fi
if \$DROP \"pkill -x '${RDC_NAME}'\" 2>/dev/null; then
    echo '== kohnə ${RDC_NAME} prosesi dayandirildi'
    sleep 1
else
    echo '== isleyen ${RDC_NAME} prosesi yoxdur — teze bashladilir'
fi
\$DROP \"cd '${RDC_HOME}' && set -a && . /dev/stdin && set +a && { nohup '${RDC_BIN}' >> '${RDC_LOG}' 2>&1 < /dev/null & }\"
sleep 2
if pgrep -x '${RDC_NAME}' >/dev/null; then
    echo \"== ${RDC_NAME} ISLEYIR ✓ (PID: \$(pgrep -x ${RDC_NAME} | tr '\n' ' '))\"
    echo '== son loglar:'
    tail -n 5 '${RDC_LOG}' 2>/dev/null || true
else
    echo 'XETA: proses bashlamadi — son loglar:'
    tail -n 20 '${RDC_LOG}' 2>/dev/null || true
    exit 1
fi
"

ssh "${TARGET}" "${REMOTE_CMD}" < "${ENV_FILE}"
