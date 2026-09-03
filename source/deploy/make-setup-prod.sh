#!/usr/bin/env bash
#
# make-setup-prod.sh — PR #330: Prod binary + self-contained setup generatoru
#
# Repo-nun aktual fayllarını oxuyub self-contained setup-prod.sh generasiya
# edir və versioned bundle yığır (dist/rdc-prod-<commit>.tgz).
#
# İstifadə (repo olan yerdə — laptopda və ya dev serverdə):
#   cd source/
#   bash deploy/make-setup-prod.sh
#   # → dist/rdc-prod-<commit>.tgz  (rdc + setup-prod.sh + VERSION)
#
# Sonra prod-a köçür və aç:
#   scp dist/rdc-prod-*.tgz root@PROD:/root/
#   ssh root@PROD
#   mkdir -p rdc-setup && tar xzf /root/rdc-prod-*.tgz -C rdc-setup
#   cd rdc-setup
#   sudo env GRAFANA_ADMIN_PASSWORD=güclüparol bash setup-prod.sh
#
# PR #331: rdc.env də bundle-a daxildir → setup-prod.sh onu /etc/rdc/env
# (chmod 600, root-only) kimi quraşdırır. App artıq serverdən başladılır:
#   sudo systemctl start rdc      (laptop/run-remote.sh lazım DEYİL)

set -euo pipefail

# ------------------------- Konfiqurasiya -------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_SCRIPT="${REPO_ROOT}/deploy/setup-prod.sh"
DIST_DIR="${REPO_ROOT}/dist"
RDC_BIN="${REPO_ROOT}/rdc"

# ------------------------- Köməkçilər -------------------------
log()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[UYARI]\033[0m %s\n' "$*"; }
err()  { printf '\033[1;31m[XETA]\033[0m %s\n' "$*" >&2; }

# ------------------------- 1) Yoxlamalar -------------------------
log "1) Fayllar yoxlanılır..."

REQUIRED_FILES=(
    "${REPO_ROOT}/docker-compose.yml"
    "${REPO_ROOT}/promtail-config.yml"
    "${REPO_ROOT}/deploy/rdc.service"
    "${REPO_ROOT}/loki-config.yml"
    "${REPO_ROOT}/grafana/provisioning/datasources/loki.yml"
)
for f in "${REQUIRED_FILES[@]}"; do
    if [[ ! -f "${f}" ]]; then
        err "Tələb olunan fayl tapılmadı: ${f}"
        exit 1
    fi
done
log "OK: 5 fayl mövcuddur"

# Delimiter toqquşması qoruması — embed olunan fayllarda delimiter yoxdursa yaxşıdır
for f in "${REQUIRED_FILES[@]}"; do
    if grep -Eq '^(COMPOSE_TPL_EOF|PROMTAIL_TPL_EOF|LOKI_CFG_TPL_EOF|LOKI_DS_TPL_EOF|UNIT_TPL_EOF)$' "${f}"; then
        err "Delimiter toqquşması: ${f} içində TPL_EOF sətri var — embed edilə bilməz"
        exit 1
    fi
done
log "OK: delimiter toqquşması yoxdur"

# PR #331: rdc.env mütləq lazımdır (bundle-a daxil olur)
ENV_FILE_SRC="${REPO_ROOT}/deploy/rdc.env"
if [[ ! -f "${ENV_FILE_SRC}" ]]; then
    err "deploy/rdc.env tapılmadı — prod bundle env tələb edir"
    err "  cp deploy/env.example deploy/rdc.env və parametrləri doldurun"
    exit 1
fi
# TƏHLÜKƏSİZLİK QAPISI: prod-a DROP_RECREATE=true GETMƏMƏLİ (app başlayanda DB silinər!)
if grep -q '^MIGRATIONS_DROP_RECREATE=true' "${ENV_FILE_SRC}"; then
    if [[ "${FORCE_BUNDLE:-0}" == "1" ]]; then
        warn "FORCE_BUNDLE=1 — MIGRATIONS_DROP_RECREATE=true ilə davam edilir (DİQQƏTLİ!)"
    else
        err "rdc.env-də MIGRATIONS_DROP_RECREATE=true — PROD üçün QADAĞANDIR (DB silinər!)"
        err "  1) nano deploy/rdc.env → MIGRATIONS_DROP_RECREATE=false"
        err "  2) və ya məcburi: FORCE_BUNDLE=1 bash deploy/make-setup-prod.sh"
        exit 1
    fi
fi
log "OK: deploy/rdc.env mövcuddur (bundle-a daxil olacaq)"

# ------------------------- 2) Git məlumatı -------------------------
GIT_HASH="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo nogit)"
GIT_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
log "Git: ${GIT_HASH} | ${GIT_DATE}"

# ------------------------- 3) Binary build -------------------------
log "3) Linux binary build olunur (GOOS=linux GOARCH=amd64)..."

if ! command -v go >/dev/null 2>&1; then
    err "Go tapılmadı — go binary PATH-də olmalıdır"
    exit 1
fi

(cd "${REPO_ROOT}" && GOOS=linux GOARCH=amd64 go build -o rdc .)
log "Binary build olundu: ${RDC_BIN}"

BIN_SHA256="$(sha256sum "${RDC_BIN}" | awk '{print $1}')"
log "sha256: ${BIN_SHA256}"

# ------------------------- 4) setup-prod.sh generasiyası -------------------------
log "4) setup-prod.sh generasiya olunur..."

# 4a) Header — QEYRİ-müəllifləşdirilmiş heredoc (generation-time expand)
cat > "${OUT_SCRIPT}" <<EOF
#!/usr/bin/env bash
# setup-prod.sh — PROD quraşdırma (PR #330)
# ⚠ GENERASIYA OLUNUB — ƏL İLƏ DƏYİŞMƏYİN!
#   Generator: deploy/make-setup-prod.sh
#   Source commit: ${GIT_HASH} | Generated: ${GIT_DATE}
#   Binary sha256: ${BIN_SHA256}
#
# İstifadə (prod serverdə):
#   sudo env GRAFANA_ADMIN_PASSWORD=güclüparol bash setup-prod.sh
#
# App-i başlatmaq üçün (serverdən — PR #331, laptop/run-remote.sh lazım deyil):
#   sudo systemctl start rdc

set -euo pipefail

# ------------------------- Konfiqurasiya -------------------------
RDC_HOME="/opt/rdc"
MON_HOME="\${RDC_HOME}/monitoring"
SERVICE_NAME="rdc"
RDC_USER="rdc"
SCRIPT_DIR="\$(cd "\$(dirname "\${BASH_SOURCE[0]}")" && pwd)"

# Standart dəyərlər (env ilə üstünə yazıla bilər)
DB_PORT="\${DB_PORT:-1433}"
DB_NAME="\${DB_NAME:-RDC}"
SERVER_ADDR="\${SERVER_ADDR:-:8000}"
LOG_LEVEL="\${LOG_LEVEL:-info}"
GRAFANA_ADMIN_PASSWORD="\${GRAFANA_ADMIN_PASSWORD:-admin}"
LOKI_MAX_OUTSTANDING_REQUESTS="\${LOKI_MAX_OUTSTANDING_REQUESTS:-1000}"
# PR #369: Loki retention — default 6 ay (4320h)
LOKI_RETENTION_PERIOD="\${LOKI_RETENTION_PERIOD:-4320h}"
SERVER_IP="\${SERVER_IP:-\$(hostname -I 2>/dev/null | awk '{print \$1}' || echo '')}"
AZMK_PUBLIC_IP="\${AZMK_PUBLIC_IP:-185.161.225.102}"
INSTALL_CADDYFILE_OVERWRITE="\${INSTALL_CADDYFILE_OVERWRITE:-0}"
APP_PORT="\${SERVER_ADDR##*:}"
APP_PORT="\${APP_PORT:-8000}"

GEN_COMMIT="${GIT_HASH}"
GEN_DATE="${GIT_DATE}"
GEN_BIN_SHA256="${BIN_SHA256}"

# ------------------------- Köməkçilər -------------------------
log()  { printf '\033[1;32m==>\033[0m %s\n' "\$*"; }
warn() { printf '\033[1;33m[UYARI]\033[0m %s\n' "\$*"; }
err()  { printf '\033[1;31m[XETA]\033[0m %s\n' "\$*" >&2; }
sed_escape() { printf '%s' "\$1" | sed -e 's/[\\|&]/\\&/g'; }

# ------------------------- Docker quraşdırma -------------------------
install_docker() {
    if command -v apt-get >/dev/null 2>&1; then
        . /etc/os-release
        apt-get update
        apt-get install -y ca-certificates curl
        install -m 0755 -d /etc/apt/keyrings
        curl -fsSL "https://download.docker.com/linux/\${ID}/gpg" -o /etc/apt/keyrings/docker.asc
        chmod a+r /etc/apt/keyrings/docker.asc
        printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' \\
            "\$(dpkg --print-architecture)" "\${ID}" "\${VERSION_CODENAME}" \\
            > /etc/apt/sources.list.d/docker.list
        apt-get update
        apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    elif command -v dnf >/dev/null 2>&1; then
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

install_caddy() {
    apt-get install -y debian-keyring debian-archive-keyring curl gnupg
    install -m 0755 -d /etc/apt/keyrings
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /etc/apt/keyrings/caddy-stable-archive-keyring.gpg
    chmod a+r /etc/apt/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
    apt-get update && apt-get install -y caddy
}

ensure_azmk_hosts() {
    if grep -Eq '^[^#]*[[:space:]]web\.azmk\.az([[:space:]]|\$)' /etc/hosts 2>/dev/null; then
        log "AZMK hosts artıq mövcuddur ✓"
    else
        printf '%s web.azmk.az\n' "\${AZMK_PUBLIC_IP}" >> /etc/hosts
        log "AZMK hosts əlavə olundu: \${AZMK_PUBLIC_IP} web.azmk.az"
    fi
}

# ------------------------- 1) Root + binary yoxlaması -------------------------
log "1/11 Root və binary yoxlanılır..."
if [[ \${EUID} -ne 0 ]]; then
    err "Bu script root ilə işlədilməlidir"
    exit 1
fi
if [[ ! -f "\${SCRIPT_DIR}/rdc" ]]; then
    err "\${SCRIPT_DIR}/rdc binary tapılmadı"
    exit 1
fi
if [[ "\${GRAFANA_ADMIN_PASSWORD}" == "admin" ]]; then
    warn "Grafana admin parolu default-dur (admin/admin)."
    warn "Prod üçün: sudo env GRAFANA_ADMIN_PASSWORD=güclüparol bash setup-prod.sh"
fi
log "OK: root, binary mövcuddur"

# ------------------------- 2) Docker + compose -------------------------
log "2/11 Docker yoxlanılır..."
if ! command -v docker >/dev/null 2>&1; then
    warn "docker yoxdur — rəsmi repo-dan quraşdırılır..."
    if ! install_docker; then
        err "Docker quraşdırıla bilmədi"
        exit 1
    fi
    systemctl enable --now docker 2>/dev/null || warn "docker başladıla bilmədi"
    log "Docker quraşdırıldı: \$(docker --version)"
fi
if ! docker compose version >/dev/null 2>&1; then
    warn "docker compose plugin yoxdur — quraşdırılır..."
    if ! install_compose_plugin; then
        err "docker compose plugin quraşdırıla bilmədi"
        exit 1
    fi
    log "Compose plugin: \$(docker compose version)"
fi
log "OK: docker + compose"

# ------------------------- 3) AZMK hosts -------------------------
log "3/11 AZMK hosts..."
ensure_azmk_hosts

# ------------------------- 4) İstifadəçi + qovluqlar -------------------------
log "4/11 İstifadəçi və qovluqlar..."
if id "\${RDC_USER}" >/dev/null 2>&1; then
    log "İstifadəçi '\${RDC_USER}' artıq mövcuddur"
else
    useradd --system --no-create-home --shell /usr/sbin/nologin "\${RDC_USER}"
    log "Sistem istifadəçisi yaradıldı: \${RDC_USER}"
fi
install -d -o root -g root -m 755 "\${RDC_HOME}"
install -d -o root -g root -m 755 "\${MON_HOME}"

# ------------------------- 5) Binary kopyalanması -------------------------
log "5/11 Binary kopyalanır..."
install -m 755 "\${SCRIPT_DIR}/rdc" "\${RDC_HOME}/rdc"
log "Binary: \${RDC_HOME}/rdc"

# ------------------------- 6) Monitorinq faylları -------------------------
log "6/11 Monitorinq faylları yerləşdirilir..."

EOF

# 4b) Embed docker-compose.yml (müəllifləşdirilmiş heredoc)
{
    printf 'cat > "${MON_HOME}/docker-compose.yml" <<%s\n' "'COMPOSE_TPL_EOF'"
    cat "${REPO_ROOT}/docker-compose.yml"
    printf '%s\n' 'COMPOSE_TPL_EOF'
} >> "${OUT_SCRIPT}"

# Runtime sed-lər və digər fayllar
cat >> "${OUT_SCRIPT}" <<'GEN_LOGIC_A_EOF'

# Grafana parolu
if [[ "${GRAFANA_ADMIN_PASSWORD}" != "admin" ]]; then
    sed -i "s|GF_SECURITY_ADMIN_PASSWORD=admin|GF_SECURITY_ADMIN_PASSWORD=$(sed_escape "${GRAFANA_ADMIN_PASSWORD}")|" \
        "${MON_HOME}/docker-compose.yml"
fi

# Loki sorğu limiti
if [[ "${LOKI_MAX_OUTSTANDING_REQUESTS}" != "1000" ]]; then
    sed -i "s|max-outstanding-requests-per-tenant=1000|max-outstanding-requests-per-tenant=${LOKI_MAX_OUTSTANDING_REQUESTS}|" \
        "${MON_HOME}/docker-compose.yml"
fi
GEN_LOGIC_A_EOF

# Embed promtail-config.yml
{
    printf 'cat > "${MON_HOME}/promtail-config.yml" <<%s\n' "'PROMTAIL_TPL_EOF'"
    cat "${REPO_ROOT}/promtail-config.yml"
    printf '%s\n' 'PROMTAIL_TPL_EOF'
} >> "${OUT_SCRIPT}"

# PR #369: Embed loki-config.yml (Loki retention konfiqi)
{
    printf 'cat > "${MON_HOME}/loki-config.yml" <<%s\n' "'LOKI_CFG_TPL_EOF'"
    cat "${REPO_ROOT}/loki-config.yml"
    printf '%s\n' 'LOKI_CFG_TPL_EOF'
} >> "${OUT_SCRIPT}"

# PR #369: retention override (generated script üçün runtime sed)
cat >> "${OUT_SCRIPT}" <<'GEN_LOGIC_RET_EOF'

# PR #369: Loki retention (loki-config.yml-də default 4320h; env ilə fərqli verilibsə)
if [[ "${LOKI_RETENTION_PERIOD}" != "4320h" ]]; then
    sed -i "s|retention_period: 4320h|retention_period: ${LOKI_RETENTION_PERIOD}|" \
        "${MON_HOME}/loki-config.yml"
fi
GEN_LOGIC_RET_EOF

# Grafana provisioning + app.log
cat >> "${OUT_SCRIPT}" <<'GEN_LOGIC_B_EOF'

install -d "${MON_HOME}/grafana/provisioning/datasources"
GEN_LOGIC_B_EOF

# Embed loki.yml datasource
{
    printf 'cat > "${MON_HOME}/grafana/provisioning/datasources/loki.yml" <<%s\n' "'LOKI_DS_TPL_EOF'"
    cat "${REPO_ROOT}/grafana/provisioning/datasources/loki.yml"
    printf '%s\n' 'LOKI_DS_TPL_EOF'
} >> "${OUT_SCRIPT}"

# app.log + systemd unit + sshd + caddy + container cleanup + compose up
cat >> "${OUT_SCRIPT}" <<'GEN_LOGIC_C_EOF'

# app.log
if [[ ! -f "${MON_HOME}/app.log" ]]; then
    touch "${MON_HOME}/app.log"
fi
chown "${RDC_USER}:${RDC_USER}" "${MON_HOME}/app.log"
chmod 640 "${MON_HOME}/app.log"

# PR #369: app.log gündəlik backup — Loki/volume hər nə olsa, xam tarixçə
# /opt/rdc/backups/ içində qalır (bərpa: app.log-u geri qoyub positions reset)
install -d -o root -g root -m 755 "${RDC_HOME}/backups"
cat > /etc/cron.d/rdc-log-backup <<'CRON_EOF'
# PR #369: hər gün 01:00 — app.log arxivlənir, 30 gündən köhnələr silinir
0 1 * * * root tar czf /opt/rdc/backups/app-$(date +\%Y\%m\%d).tar.gz -C /opt/rdc/monitoring app.log && find /opt/rdc/backups -name 'app-*.tar.gz' -mtime +30 -delete
CRON_EOF
chmod 644 /etc/cron.d/rdc-log-backup

# ------------------------- 7) systemd unit -------------------------
log "7/11 systemd unit..."
UNIT_SRC="/tmp/rdc.service.tpl"
UNIT_DST="/etc/systemd/system/${SERVICE_NAME}.service"
GEN_LOGIC_C_EOF

# Embed rdc.service
{
    printf 'cat > "${UNIT_SRC}" <<%s\n' "'UNIT_TPL_EOF'"
    cat "${REPO_ROOT}/deploy/rdc.service"
    printf '%s\n' 'UNIT_TPL_EOF'
} >> "${OUT_SCRIPT}"

# Rest of the logic
cat >> "${OUT_SCRIPT}" <<'GEN_LOGIC_D_EOF'

sed \
    -e "s|__DB_PORT__|$(sed_escape "${DB_PORT}")|g" \
    -e "s|__DB_NAME__|$(sed_escape "${DB_NAME}")|g" \
    -e "s|__SERVER_ADDR__|$(sed_escape "${SERVER_ADDR}")|g" \
    -e "s|__LOG_LEVEL__|$(sed_escape "${LOG_LEVEL}")|g" \
    "${UNIT_SRC}" > "${UNIT_DST}"
chmod 644 "${UNIT_DST}"
rm -f "${UNIT_SRC}"
systemctl daemon-reload
# PR #331: enable (boot-da avtomatik qalxır) — amma START etmirik
systemctl enable "${SERVICE_NAME}" 2>/dev/null || warn "systemctl enable ${SERVICE_NAME} alınmadı"
log "Unit quraşdırıldı + enable edildi: systemctl start ${SERVICE_NAME} ilə başladın"

# ------------------------- 7b) /etc/rdc/env (PR #331) -------------------------
log "7b) /etc/rdc/env (app parametrləri)..."
if [[ -f "${SCRIPT_DIR}/rdc.env" ]]; then
    install -d -o root -g root -m 755 /etc/rdc
    install -m 600 -o root -g root "${SCRIPT_DIR}/rdc.env" /etc/rdc/env
    rm -f "${SCRIPT_DIR}/rdc.env"
    log "/etc/rdc/env quraşdırıldı (chmod 600, root-only) — bundle nüsxəsi silindi"
    if grep -q '^MIGRATIONS_DROP_RECREATE=true' /etc/rdc/env; then
        warn "!!! /etc/rdc/env-də MIGRATIONS_DROP_RECREATE=true — DB SİLİNƏCƏK! Düzəldin: nano /etc/rdc/env"
    fi
elif [[ -f /etc/rdc/env ]]; then
    log "/etc/rdc/env artıq mövcuddur — dəyişdirilmir (yeniləmək üçün bundle-da rdc.env olmalıdır)"
else
    warn "rdc.env bundle-da yoxdur və /etc/rdc/env mövcud deyil — app parametrlərsiz başlaya bilməyəcək!"
fi

# ------------------------- 8) sshd -------------------------
log "8/11 sshd yoxlanılır..."
if ss -tln 2>/dev/null | grep -q ':22 ' || systemctl is-active --quiet ssh || systemctl is-active --quiet sshd; then
    log "sshd aktivdir ✓"
else
    warn "sshd yoxdur — openssh-server quraşdırılır..."
    if command -v apt-get >/dev/null 2>&1; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y openssh-server
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y openssh-server
    elif command -v yum >/dev/null 2>&1; then
        yum install -y openssh-server
    fi
    systemctl enable --now ssh 2>/dev/null || systemctl enable --now sshd 2>/dev/null || \
        warn "sshd başladıla bilmədi — əl ilə: systemctl enable --now ssh"
fi

# ------------------------- 9) Caddy -------------------------
log "9/11 Caddy (reverse proxy)..."
if [[ -z "${SERVER_IP}" ]]; then
    warn "SERVER_IP boşdur — Caddy konfiqurasiyası ötürülür"
else
    if ! command -v caddy >/dev/null 2>&1; then
        if command -v apt-get >/dev/null 2>&1; then
            log "Caddy quraşdırılır (apt)..."
            install_caddy
        else
            warn "Caddy yalnız apt (Debian/Ubuntu) ilə quraşdırılır — əl ilə quraşdırın"
        fi
    fi

    if command -v caddy >/dev/null 2>&1; then
        # Caddyfile yalnız 3 halda yazılır: yoxdur / apt default / overwrite=1
        WRITE_CADDYFILE=0
        if [[ ! -f /etc/caddy/Caddyfile ]]; then
            WRITE_CADDYFILE=1
        elif grep -q 'root \* /usr/share/caddy' /etc/caddy/Caddyfile 2>/dev/null; then
            WRITE_CADDYFILE=1
        elif [[ "${INSTALL_CADDYFILE_OVERWRITE}" == "1" ]]; then
            WRITE_CADDYFILE=1
        fi

        if [[ ${WRITE_CADDYFILE} -eq 1 ]]; then
            log "Caddyfile yazılır..."
            cat > /etc/caddy/Caddyfile <<CADDYFILE_EOF
# PR #330: RDC reverse proxy (əlavələriniz qorunur — üzərinə yazılmır)
https://${SERVER_IP} {
    tls internal
    reverse_proxy localhost:${APP_PORT}
}
CADDYFILE_EOF
            caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile 2>/dev/null || warn "Caddy validate uğursuz"
            systemctl enable --now caddy 2>/dev/null || warn "caddy başladıla bilmədi"
            systemctl reload caddy 2>/dev/null || systemctl restart caddy 2>/dev/null || warn "caddy reload/restart uğursuz"
            log "Caddy konfiqurasiya olundu: https://${SERVER_IP}"
        else
            log "Caddyfile artıq mövcuddur (üzərinə yazılmır) — INSTALL_CADDYFILE_OVERWRITE=1 ilə məcburi yenilə"
        fi
    fi
fi

# ------------------------- 10) Konteyner təmizləmə + monitorinq -------------------------
log "10/11 Monitorinq stack-i..."

# PR #307: köhnə konteyner conflict-ı
EXISTING="$(docker ps -a --format '{{.Names}}' | grep -Ex 'loki|promtail|grafana' || true)"
if [[ -n "${EXISTING}" ]]; then
    warn "Köhnə konteynerlər təmizlənir: $(echo "${EXISTING}" | tr '\n' ' ')"
    docker rm -f loki promtail grafana >/dev/null 2>&1 || true
fi

docker compose -f "${MON_HOME}/docker-compose.yml" --project-directory "${MON_HOME}" up -d
log "Container-lar: $(docker ps --filter name=loki --filter name=promtail --filter name=grafana --format '{{.Names}}' | tr '\n' ' ')"

# ------------------------- 11) VERSION + yoxlama -------------------------
log "11/11 VERSION və yoxlama..."

cat > "${RDC_HOME}/VERSION" <<VERSION_EOF
# RDC prod bundle (make-setup-prod.sh, PR #330)
source_commit=${GEN_COMMIT}
generated=${GEN_DATE}
binary_sha256=${GEN_BIN_SHA256}
installed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
host=$(hostname)
VERSION_EOF

# Loki ready?
if curl -sf "http://localhost:3100/ready" >/dev/null 2>&1; then
    log "Loki: READY (http://localhost:3100)"
else
    warn "Loki hazır deyil (10 san gözləyib təkrar yoxlayın)"
fi

# App port?
if curl -sf -o /dev/null "http://localhost:${APP_PORT}/" 2>/dev/null; then
    log "App: cavab verir (http://localhost:${APP_PORT})"
else
    warn "App port ${APP_PORT} cavab vermir — başladın: sudo systemctl start ${SERVICE_NAME} (normaldır, app hələ başlamayıb)"
fi

echo
echo "============================================================"
echo " QURAŞDIRMA TAMAMLANDI"
echo "============================================================"
echo " Versiya:        cat ${RDC_HOME}/VERSION"
echo " App başlatma:   sudo systemctl start ${SERVICE_NAME}   (serverdən — laptop lazım deyil, PR #331)"
echo " Update:         yeni bundle + setup-prod.sh + sudo systemctl restart ${SERVICE_NAME}"
echo " Env:            /etc/rdc/env (chmod 600) — dəyişmək: nano + systemctl restart ${SERVICE_NAME}"
echo " Təmizlik:       rm /root/rdc-prod-*.tgz   (içində parol var!)"
echo " Loglar:         tail -f ${MON_HOME}/app.log"
echo " Backups:        /opt/rdc/backups (app.log gündəlik tar.gz, 30 gün)"
echo " Caddy URL:      https://${SERVER_IP}"
echo " Grafana:        http://${SERVER_IP}:3001 (${GRAFANA_ADMIN_PASSWORD} / ***)"
echo " Loki sorğusu:   {job=\"go-app\"}"
echo " Firewall:       ufw allow 22,443/tcp  (8000/3100/3001 bağlı qalmalı)"
echo "============================================================"
exit 0
GEN_LOGIC_D_EOF

# ------------------------- 5) Exec bit + syntax check -------------------------
chmod +x "${OUT_SCRIPT}"

if ! bash -n "${OUT_SCRIPT}"; then
    err "setup-prod.sh syntax xətası — bundle yaradılmır"
    exit 1
fi
log "OK: setup-prod.sh syntax valid"

# ------------------------- 6) Bundle -------------------------
log "6) Bundle yığılır..."

STAGE="$(mktemp -d)"
cp "${RDC_BIN}" "${STAGE}/rdc"
cp "${OUT_SCRIPT}" "${STAGE}/setup-prod.sh"
chmod +x "${STAGE}/setup-prod.sh"
# PR #331: rdc.env də bundle-a daxildir (setup-prod.sh /etc/rdc/env-ə quraşdırır)
cp "${ENV_FILE_SRC}" "${STAGE}/rdc.env"

cat > "${STAGE}/VERSION" <<VERSION_EOF
# RDC prod bundle (make-setup-prod.sh, PR #330/#331)
source_commit=${GIT_HASH}
generated=${GIT_DATE}
binary_sha256=${BIN_SHA256}
rdc_env_bundled=yes
VERSION_EOF

mkdir -p "${DIST_DIR}"
BUNDLE="${DIST_DIR}/rdc-prod-${GIT_HASH}.tgz"
tar czf "${BUNDLE}" -C "${STAGE}" rdc setup-prod.sh VERSION rdc.env
rm -rf "${STAGE}"

BUNDLE_SIZE="$(du -h "${BUNDLE}" | awk '{print $1}')"

log "Bundle: ${BUNDLE} (${BUNDLE_SIZE})"
echo
warn "⚠ Bundle-da PLAINTEXT parollar var (rdc.env) — dist/ yalnız laptopda, serverdə quraşdırmadan sonra .tgz-i silin!"
echo
echo "============================================================"
echo " NÖVBƏTİ ADDIMLAR"
echo "============================================================"
echo " 1. Prod-a köçür:"
echo "    scp ${BUNDLE} root@PROD:/root/"
echo
echo " 2. Prod-da aç və qur (env daxil — /etc/rdc/env quraşdırılır):"
echo "    ssh root@PROD"
echo "    mkdir -p rdc-setup && tar xzf /root/rdc-prod-${GIT_HASH}.tgz -C rdc-setup"
echo "    cd rdc-setup"
echo "    sudo env GRAFANA_ADMIN_PASSWORD=güclüparol bash setup-prod.sh"
echo
echo " 3. App-i başlat (serverdən — laptop lazım deyil):"
echo "    sudo systemctl start rdc"
echo
echo " 4. Təmizlik (parol olan .tgz-i sil):"
echo "    rm /root/rdc-prod-*.tgz"
echo "============================================================"
