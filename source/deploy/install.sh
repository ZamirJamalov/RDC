#!/usr/bin/env bash
#
# install.sh — RDC server quraşdırması (PR #296)
#
# NEÇƏ İŞLƏDİLİR:
#   source/ qovluğunda (rdc binary + docker-compose.yml + promtail-config.yml
#   + grafana/ bir yerdə olanda) root ilə:
#
#     sudo bash deploy/install.sh                          # interaktiv (env soruşur)
#     sudo env DB_HOST=10.0.0.5 DB_USER=sa DB_PASSWORD=*** bash deploy/install.sh
#     sudo env ... GRAFANA_ADMIN_PASSWORD=strongpass LOG_LEVEL=info bash deploy/install.sh
#
# NƏ EDİR:
#   1. Ön şərtləri yoxlayır (root, docker, compose, binary, systemd >= 240)
#   2. rdc sistem istifadəçisi yaradır (yoxdursa)
#   3. /opt/rdc/ strukturu qurur: binary + monitorinq faylları
#   4. app.log yaradır (rdc istifadəçisi yaza bilsin deyə)
#   5. rdc.service generasiya edir (placeholder-ləri doldurub)
#   6. systemd service işə salır + avtostart
#   7. Monitorinq stack-i (loki + promtail + grafana) qaldırır
#   8. Yoxlayır və nəticəni göstərir
#
# Təkrar işlətmək təhlükəsizdir (idempotent): mövcud olanı yeniden yaratmır,
# service-i restart edir.
#
# Yalnız Debian/Ubuntu (apt) və RHEL/CentOS (useradd) üzərində test edilib.

set -euo pipefail

# ------------------------- Konfiqurasiya -------------------------
RDC_HOME="/opt/rdc"
MON_HOME="${RDC_HOME}/monitoring"
SERVICE_NAME="rdc"
RDC_USER="rdc"

# Script-in öz yeri → repo kökü (rdc binary, docker-compose.yml orada axtarılır)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Standart dəyərlər (env ilə üstünə yazıla bilər)
DB_PORT="${DB_PORT:-1433}"
DB_NAME="${DB_NAME:-RDC}"
SERVER_ADDR="${SERVER_ADDR:-:8000}"
LOG_LEVEL="${LOG_LEVEL:-info}"
GRAFANA_ADMIN_PASSWORD="${GRAFANA_ADMIN_PASSWORD:-admin}"

# ------------------------- Köməkçi funksiyalar -------------------------
log()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[UYARI]\033[0m %s\n' "$*"; }
err()  { printf '\033[1;31m[XETA]\033[0m %s\n' "$*" >&2; }

# sed üçün xüsusi simvolları escape edir (&, |, \)
sed_escape() { printf '%s' "$1" | sed -e 's/[\\|&]/\\&/g'; }

# ------------------------- 0) Root yoxlaması -------------------------
if [[ ${EUID} -ne 0 ]]; then
    err "Bu script root ilə işlədilməlidir: sudo bash deploy/install.sh"
    exit 1
fi

# ------------------------- 1) Ön şərt yoxlamaları -------------------------
log "1/8 Ön şərtlər yoxlanılır..."

if [[ ! -f "${REPO_ROOT}/rdc" ]]; then
    err "${REPO_ROOT}/rdc binary tapılmadı."
    echo "  Əvvəl build edin (lokalda və ya burada):  go build -o rdc ."
    echo "  və ya hazır binary-ni ${REPO_ROOT}/ içinə qoyun."
    exit 1
fi

for f in docker-compose.yml promtail-config.yml grafana; do
    if [[ ! -e "${REPO_ROOT}/${f}" ]]; then
        err "${REPO_ROOT}/${f} tapılmadı — repo kökündən işlədin."
        exit 1
    fi
done

if ! command -v docker >/dev/null 2>&1; then
    err "docker yoxdur. Quraşdırın: https://docs.docker.com/engine/install/"
    exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
    err "docker compose plugin yoxdur (docker-compose-v2 / docker-compose-plugin)."
    exit 1
fi

# systemd >= 240 (StandardOutput=append: üçün)
SYSTEMD_VER="$(systemctl --version | head -1 | awk '{print $2}')"
if [[ "${SYSTEMD_VER}" -lt 240 ]]; then
    warn "systemd ${SYSTEMD_VER} < 240 — StandardOutput=append: dəstəklənmir!"
    warn "Unit-də append: işləməyəcək — loglar journal-a düşəcək. Daha yeni distro tövsiyə olunur."
fi

if [[ "${GRAFANA_ADMIN_PASSWORD}" == "admin" ]]; then
    warn "Grafana admin parolu default-dur (admin/admin)."
    warn "Prod üçün: sudo env GRAFANA_ADMIN_PASSWORD=güclüparol bash deploy/install.sh"
fi

# PR #297: parollar hələ doldurulmayıbsa (şablonda kommentdə qalıbsa) xəbərdarlıq
if [[ -f /etc/rdc/env ]] && grep -qE '^#[[:space:]]*(DB_PASSWORD|AZMK_PASSWORD)=' /etc/rdc/env; then
    warn "/etc/rdc/env içində parollar hələ doldurulmayıb (DB_PASSWORD, AZMK_PASSWORD)."
    warn "Doldurun:  sudo nano /etc/rdc/env   (kommentdən çıxarın, dəyəri yazın)"
    warn "Sonra:     sudo systemctl restart rdc"
fi

log "OK: binary, monitorinq faylları, docker, systemd ${SYSTEMD_VER}"

# ------------------------- 2) DB env-ləri -------------------------
log "2/8 DB parametrləri..."

if [[ -z "${DB_HOST:-}" ]]; then
    read -rp "DB_HOST (SQL Server hostu): " DB_HOST
fi
if [[ -z "${DB_USER:-}" ]]; then
    read -rp "DB_USER: " DB_USER
fi
if [[ -z "${DB_PASSWORD:-}" ]]; then
    read -rp "DB_PASSWORD: " -s DB_PASSWORD
    echo
fi
if [[ -z "${DB_HOST}" || -z "${DB_USER}" || -z "${DB_PASSWORD}" ]]; then
    err "DB_HOST / DB_USER / DB_PASSWORD boş ola bilməz."
    exit 1
fi

# ------------------------- 3) İstifadəçi + qovluqlar -------------------------
log "3/8 İstifadəçi və qovluqlar..."

if id "${RDC_USER}" >/dev/null 2>&1; then
    log "İstifadəçi '${RDC_USER}' artıq mövcuddur"
else
    useradd --system --no-create-home --shell /usr/sbin/nologin "${RDC_USER}"
    log "Sistem istifadəçisi yaradıldı: ${RDC_USER}"
fi

install -d -o root -g root -m 755 "${RDC_HOME}"
install -d -o root -g root -m 755 "${MON_HOME}"

# ------------------------- 4) Faylların kopyalanması -------------------------
log "4/8 Fayllar kopyalanır..."

install -m 755 "${REPO_ROOT}/rdc" "${RDC_HOME}/rdc"
install -m 644 "${REPO_ROOT}/docker-compose.yml"   "${MON_HOME}/docker-compose.yml"
install -m 644 "${REPO_ROOT}/promtail-config.yml"  "${MON_HOME}/promtail-config.yml"
cp -r "${REPO_ROOT}/grafana" "${MON_HOME}/grafana"

# Grafana parolu (default 'admin' deyilsə compose faylında əvəz olunur)
if [[ "${GRAFANA_ADMIN_PASSWORD}" != "admin" ]]; then
    sed -i "s|GF_SECURITY_ADMIN_PASSWORD=admin|GF_SECURITY_ADMIN_PASSWORD=$(sed_escape "${GRAFANA_ADMIN_PASSWORD}")|" \
        "${MON_HOME}/docker-compose.yml"
fi

# app.log — rdc istifadəçisi yazmalı, promtail (docker/root) oxumalı
if [[ ! -f "${MON_HOME}/app.log" ]]; then
    touch "${MON_HOME}/app.log"
fi
chown "${RDC_USER}:${RDC_USER}" "${MON_HOME}/app.log"
chmod 640 "${MON_HOME}/app.log"

# PR #297: /etc/rdc/env — əlavə parametrlər (AZMK, MyGov, LW, video və s.)
# Parollar YALNIZ burada saxlanılır (repo-da yoxdur) — chmod 600, root-only.
install -d -o root -g root -m 755 /etc/rdc
if [[ ! -f /etc/rdc/env ]]; then
    install -m 600 -o root -g root "${SCRIPT_DIR}/env.example" /etc/rdc/env
    log "Yaradıldı: /etc/rdc/env (şablon kopyalandı, chmod 600)"
else
    log "/etc/rdc/env artıq mövcuddur — toxunulmadı (mövcud dəyərlər qorunur)"
fi

log "Yerləşdirildi: ${RDC_HOME}/rdc, ${MON_HOME}/{docker-compose.yml,promtail-config.yml,grafana/,app.log}"

# ------------------------- 5) systemd unit generasiyası -------------------------
log "5/8 systemd unit generasiya olunur..."

UNIT_SRC="${SCRIPT_DIR}/rdc.service"
UNIT_DST="/etc/systemd/system/${SERVICE_NAME}.service"

sed \
    -e "s|__DB_HOST__|$(sed_escape "${DB_HOST}")|g" \
    -e "s|__DB_USER__|$(sed_escape "${DB_USER}")|g" \
    -e "s|__DB_PASSWORD__|$(sed_escape "${DB_PASSWORD}")|g" \
    -e "s|__DB_PORT__|$(sed_escape "${DB_PORT}")|g" \
    -e "s|__DB_NAME__|$(sed_escape "${DB_NAME}")|g" \
    -e "s|__SERVER_ADDR__|$(sed_escape "${SERVER_ADDR}")|g" \
    -e "s|__LOG_LEVEL__|$(sed_escape "${LOG_LEVEL}")|g" \
    "${UNIT_SRC}" > "${UNIT_DST}"
chmod 644 "${UNIT_DST}"

log "Yaradıldı: ${UNIT_DST}"

# ------------------------- 6) Service işəsalma -------------------------
log "6/8 systemd service..."

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}" >/dev/null
if systemctl is-active --quiet "${SERVICE_NAME}"; then
    systemctl restart "${SERVICE_NAME}"
    log "Service restart edildi (əvvəl aktiv idi)"
else
    systemctl start "${SERVICE_NAME}"
    log "Service başladıldı"
fi

sleep 2
if ! systemctl is-active --quiet "${SERVICE_NAME}"; then
    err "Service İŞƏ DÜŞMƏDİ. Diaqnostika:"
    systemctl --no-pager -l status "${SERVICE_NAME}" || true
    echo "--- app.log son sətirlər: ---"
    tail -20 "${MON_HOME}/app.log" || true
    exit 1
fi
log "Service ACTIVE: systemctl status ${SERVICE_NAME}"

# ------------------------- 7) Monitorinq stack-i -------------------------
log "7/8 Monitorinq stack-i (loki + promtail + grafana)..."

docker compose -f "${MON_HOME}/docker-compose.yml" --project-directory "${MON_HOME}" up -d

log "Container-lar: $(docker ps --filter name=loki --filter name=promtail --filter name=grafana --format '{{.Names}}' | tr '\n' ' ')"

# ------------------------- 8) Yoxlama -------------------------
log "8/8 Yoxlama..."

FAIL=0

# Loki ready?
if curl -sf "http://localhost:3100/ready" >/dev/null 2>&1; then
    log "Loki: READY (http://localhost:3100)"
else
    warn "Loki hazır deyil (10 san gözləyib təkrar yoxlayın: curl localhost:3100/ready)"
    FAIL=1
fi

# App port?
APP_PORT="${SERVER_ADDR#:}"
APP_PORT="${APP_PORT:-8000}"
if curl -sf -o /dev/null "http://localhost:${APP_PORT}/" ; then
    log "App: cavab verir (http://localhost:${APP_PORT})"
else
    warn "App port ${APP_PORT} cavab vermir — service statusunu yoxlayın"
    FAIL=1
fi

echo
echo "============================================================"
echo " QURAŞDIRMA TAMAMLANDI"
echo "============================================================"
echo " App:            systemctl status ${SERVICE_NAME}"
echo " Env (parollar): sudo nano /etc/rdc/env   → sonra systemctl restart rdc"
echo " Loglar:         tail -f ${MON_HOME}/app.log"
echo " Grafana:        http://<server-ip>:3001  (${GRAFANA_ADMIN_PASSWORD} / ***)"
echo " Loki sorğusu:   {job=\"go-app\"}"
echo " Dayandırma:     sudo systemctl stop ${SERVICE_NAME}"
echo " Monitorinq:     cd ${MON_HOME} && docker compose down"
echo "============================================================"
if [[ ${FAIL} -eq 1 ]]; then
    warn "Bəzi yoxlamalar keçmədi — yuxarıdakı uyari-lara baxın."
fi
exit 0
