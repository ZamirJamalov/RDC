#!/usr/bin/env bash
#
# install.sh — RDC server quraşdırması (PR #296)
#
# NEÇƏ İŞLƏDİLİR:
#   source/ qovluğunda (rdc binary + docker-compose.yml + promtail-config.yml
#   + grafana/ bir yerdə olanda) root ilə:
#
#     sudo bash deploy/install.sh
#     sudo env GRAFANA_ADMIN_PASSWORD=strongpass LOG_LEVEL=info bash deploy/install.sh
#
# PR #298: app parametrləri (DB, AZMK və s.) burada SORUŞULMUR — onlar
# laptopdakı deploy/rdc.env-də saxlanılır və app başlanarkə
# deploy/run-remote.sh ilə birbaşa proses env-inə ötürülür (diske yazılmır).
#
# NƏ EDİR:
#   1. Ön şərtləri yoxlayır (root, binary, systemd >= 240); docker yoxdursa
#      AVTOMATİK quraşdırır (rəsmi repo, PR #324)
#   2. rdc sistem istifadəçisi yaradır (yoxdursa)
#   3. /opt/rdc/ strukturu qurur: binary + monitorinq faylları
#   4. app.log yaradır (rdc istifadəçisi yaza bilsin deyə)
#   5. rdc.service generasiya edir (opsional systemd yolu üçün)
#   6. systemd unit-i quraşdırır (BAŞLATMIR — app run-remote.sh ilə işə salınır)
#      + sshd yoxlayır, yoxdursa quraşdırır (run-remote.sh ssh ilə işləyir;
#        localhost testində laptopda da sshd lazımdır — PR #308)
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

# PR #324: Docker-ı rəsmi repo-dan quraşdırır (docker-ce + cli + containerd +
# buildx + compose plugin). Yalnız apt (Debian/Ubuntu) və dnf/yum (RHEL/CentOS).
# İnternet çıxışı tələb edir. Funksiya `if !` kontekstində çağırıldığı üçün
# set -e ilk xətada funksiyanı dayandırır və qeyri-sıfır status qaytarır →
# çağıran tərəf aydın xəta verir.
install_docker() {
    if command -v apt-get >/dev/null 2>&1; then
        # Debian / Ubuntu — rəsmi Docker apt repo-su
        . /etc/os-release
        apt-get update
        apt-get install -y ca-certificates curl
        install -m 0755 -d /etc/apt/keyrings
        curl -fsSL "https://download.docker.com/linux/${ID}/gpg" -o /etc/apt/keyrings/docker.asc
        chmod a+r /etc/apt/keyrings/docker.asc
        printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' \
            "$(dpkg --print-architecture)" "${ID}" "${VERSION_CODENAME}" \
            > /etc/apt/sources.list.d/docker.list
        apt-get update
        apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    elif command -v dnf >/dev/null 2>&1; then
        # RHEL / CentOS / Fedora — rəsmi Docker repo-su
        dnf install -y dnf-plugins-core
        dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
        dnf install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    elif command -v yum >/dev/null 2>&1; then
        yum install -y yum-utils
        yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
        yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    else
        warn "Tanınan paket meneceri yoxdur (apt/dnf/yum) — docker-i əl ilə quraşdırın"
        return 1
    fi
}

# PR #324: docker var, compose plugin yoxdur — yalnız plugin-i quraşdırır.
install_compose_plugin() {
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update && apt-get install -y docker-compose-plugin
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y docker-compose-plugin
    elif command -v yum >/dev/null 2>&1; then
        yum install -y docker-compose-plugin
    else
        return 1
    fi
}

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

# PR #324: docker yoxdursa AVTOMATİK quraşdırılır (əvvəl burada dayanırdı).
# Rəsmi repo-dan quraşdırılır — compose plugin də birlikdə gəlir.
if ! command -v docker >/dev/null 2>&1; then
    warn "docker yoxdur — rəsmi repo-dan quraşdırılır (internet tələb edir)..."
    if ! install_docker; then
        err "Docker quraşdırıla bilmədi — əl ilə: https://docs.docker.com/engine/install/"
        exit 1
    fi
    systemctl enable --now docker 2>/dev/null || warn "docker başladıla bilmədi — əl ilə: systemctl enable --now docker"
    log "Docker quraşdırıldı: $(docker --version)"
fi
if ! docker compose version >/dev/null 2>&1; then
    warn "docker compose plugin yoxdur — quraşdırılır..."
    if ! install_compose_plugin; then
        err "docker compose plugin quraşdırıla bilmədi (docker-compose-v2 / docker-compose-plugin)."
        exit 1
    fi
    log "Compose plugin: $(docker compose version)"
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

# PR #298: parollar artıq laptopda — deploy/rdc.env (şablon: deploy/env.example)
log "Parollar serverdə saxlanmır: laptopda deploy/rdc.env + run-remote.sh"

log "OK: binary, monitorinq faylları, docker, systemd ${SYSTEMD_VER}"

# ------------------------- 2) Parametrlər (PR #298: soruşulmur) -------------------------
log "2/8 Parametrlər..."
log "App parametrləri (DB, AZMK, MyGov və s.) install zamanı SORUŞULMUR."
log "Onlar laptopdakı deploy/rdc.env-də saxlanılır (şablon: deploy/env.example)"
log "və app başlanarkə deploy/run-remote.sh ilə ötürülür:"
log "  bash deploy/run-remote.sh user@bu-server"

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

# PR #298: parollar serverdə faylda saxlanMIR. deploy/env.example artıq
# laptop tərəfi şablondur (deploy/rdc.env kimi istifadə olunur — run-remote.sh).

log "Yerləşdirildi: ${RDC_HOME}/rdc, ${MON_HOME}/{docker-compose.yml,promtail-config.yml,grafana/,app.log}"

# ------------------------- 5) systemd unit generasiyası -------------------------
log "5/8 systemd unit generasiya olunur..."

UNIT_SRC="${SCRIPT_DIR}/rdc.service"
UNIT_DST="/etc/systemd/system/${SERVICE_NAME}.service"

sed \
    -e "s|__DB_PORT__|$(sed_escape "${DB_PORT}")|g" \
    -e "s|__DB_NAME__|$(sed_escape "${DB_NAME}")|g" \
    -e "s|__SERVER_ADDR__|$(sed_escape "${SERVER_ADDR}")|g" \
    -e "s|__LOG_LEVEL__|$(sed_escape "${LOG_LEVEL}")|g" \
    "${UNIT_SRC}" > "${UNIT_DST}"
chmod 644 "${UNIT_DST}"

log "Yaradıldı: ${UNIT_DST}"

# ------------------------- 6) systemd unit (BAŞLADILMIR) -------------------------
log "6/8 systemd unit (opsional yol)..."

systemctl daemon-reload
log "Unit quraşdırıldı (başladILMADI): systemctl status ${SERVICE_NAME}"
log "PR #298: app run-remote.sh ilə başladılır (laptopdan):"
log "  bash deploy/run-remote.sh user@bu-server"

# PR #308: run-remote.sh app-i ssh üzərindən başladır. Real serverdə port 22
# artıq dinlənilir → bu blok ötülür. Localhost testində (install.sh +
# run-remote.sh eyni maşında) sshd yoxdursa quraşdırılır.
log "sshd yoxlanılır (run-remote.sh ssh ilə işləyir)..."
if ss -tln 2>/dev/null | grep -q ':22 ' || systemctl is-active --quiet ssh || systemctl is-active --quiet sshd; then
    log "sshd aktivdir ✓ — run-remote.sh işləyəcək"
else
    warn "sshd yoxdur (port 22 dinləmir) — openssh-server quraşdırılır..."
    if command -v apt-get >/dev/null 2>&1; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y openssh-server
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y openssh-server
    elif command -v yum >/dev/null 2>&1; then
        yum install -y openssh-server
    elif command -v zypper >/dev/null 2>&1; then
        zypper --non-interactive install openssh-server
    else
        warn "Tanınan paket meneceri yoxdur — openssh-server-i əl ilə quraşdırın"
    fi
    # Servis adı fərqlidir: Debian/Ubuntu → ssh, RHEL/CentOS → sshd
    systemctl enable --now ssh 2>/dev/null || systemctl enable --now sshd 2>/dev/null || \
        warn "sshd başladıla bilmədi — əl ilə: systemctl enable --now ssh"
    if ss -tln 2>/dev/null | grep -q ':22 '; then
        log "sshd QALXDI (port 22) ✓"
        log "Parolsuz ssh üçün (opsional): ssh-keygen -t ed25519 && ssh-copy-id user@bu-server"
    else
        warn "sshd hələ dinləmir — yoxlayın: systemctl status ssh"
    fi
fi

# ------------------------- 7) Monitorinq stack-i -------------------------
log "7/8 Monitorinq stack-i (loki + promtail + grafana)..."

# PR #307: 'container name /loki is already in use' conflict-ının qarşısı.
# Səbəb: compose faylında container_name-lər SABİTDİR; eyni adlı konteyner
# başqa project-dən qalıbsa (məs. əvvəl source/ qovluğundan manual
# `docker compose up`), yeni project onları təkrar istifadə edə bilmir.
# Volume-lar (loki-data, grafana-data) silinMİR — yalnız konteynerlər yenidən yaranır.
EXISTING="$(docker ps -a --format '{{.Names}}' | grep -Ex 'loki|promtail|grafana' || true)"
if [[ -n "${EXISTING}" ]]; then
    warn "Eyni adlı köhnə konteynerlər təmizlənir: $(echo "${EXISTING}" | tr '\n' ' ')"
    docker rm -f loki promtail grafana >/dev/null 2>&1 || true
fi

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
    warn "App port ${APP_PORT} cavab vermir — run-remote.sh ilə başladın"
    FAIL=1
fi

echo
echo "============================================================"
echo " QURAŞDIRMA TAMAMLANDI"
echo "============================================================"
echo " App başlatma:   laptopdan: bash deploy/run-remote.sh user@bu-server"
echo " Parametrlər:    laptopda deploy/rdc.env (şablon: deploy/env.example)"
echo " Loglar:         tail -f ${MON_HOME}/app.log"
echo " Grafana:        http://<server-ip>:3001  (${GRAFANA_ADMIN_PASSWORD} / ***)"
echo " Loki sorğusu:   {job=\"go-app\"}"
echo " Dayandırma:     ssh user@server 'pkill -x rdc'"
echo " Monitorinq:     cd ${MON_HOME} && docker compose down"
echo "============================================================"
if [[ ${FAIL} -eq 1 ]]; then
    warn "Bəzi yoxlamalar keçmədi — yuxarıdakı uyari-lara baxın."
fi
exit 0
