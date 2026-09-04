#!/usr/bin/env bash
#
# set_static_ip.sh — Ubuntu server-də statik IP avtomatik konfiqurasiyası (netplan)
#
# SCREEN DƏSTƏYİ:
#   - Script screen içində deyilsə, avtomatik screen sessiyasına keçir (SSH kəsilsə
#     proses və rollback davam etsin deyə).
#   - screen quraşdırılmayıbsa apt-get ilə avtomatik quraşdırılır.
#   - Artıq screen/tmux içindəsinizsə birbaşa davam edir və bunu bildirir.
#   - NO_SCREEN=1 ilə screen-siz icra mümkündür (fiziki konsol olan hallar üçün).
#   - Screen çıxışı /root/set_static_ip.screen.log-a yazılır.
#
# Sənədləşmə: Docs/DEPLOYMENT.md → Bölmə 10 (PR #328)
#
# İstifadə:
#   sudo STATIC_IP=192.168.1.50 bash set_static_ip.sh
#   sudo STATIC_IP=192.168.1.50 GATEWAY=192.168.1.1 DNS_SERVERS=10.0.0.1,8.8.8.8 bash set_static_ip.sh
#   sudo STATIC_IP=10.20.30.40 PREFIX=16 INTERFACE=ens18 bash set_static_ip.sh
#
# Env variable-lar:
#   STATIC_IP    (MÜTLƏQ)  verilən statik IP, məs: 192.168.1.50
#   GATEWAY      (optional) boş qalsa: cari default route, yoxdursa subnet-in .1 hostu
#   DNS_SERVERS  (optional) default: 8.8.8.8,1.1.1.1   (vergüllə ayır)
#   PREFIX       (optional) default: 24   (/24 üçün; /16 isə 16 yaz)
#   INTERFACE    (optional) boş qalsa: ilk ethernet interfeys avtomatik tapılır
#   NO_SCREEN    (optional) 1 = screen icrasını deaktiv et
#
set -euo pipefail

# ------------- validation -------------
: "${STATIC_IP:?STATIC_IP mütləqdir. Misal: sudo STATIC_IP=192.168.1.50 bash set_static_ip.sh}"

if [ "$(id -u)" -ne 0 ]; then
  echo "XƏTA: root lazımdır — 'sudo' ilə işə salın."
  exit 1
fi

# ============ SCREEN İCRASI (SSH kəsilməsinə qarşı) ============
if [ "${NO_SCREEN:-0}" != "1" ] && [ -z "${STY:-}" ] && [ -z "${TMUX:-}" ] && [ "${IN_SCREEN:-0}" != "1" ]; then
  echo "→ screen içində deyilsiniz — screen yoxlanılır..."
  if ! command -v screen >/dev/null 2>&1; then
    echo "→ screen quraşdırılmayıb, apt-get ilə quraşdırılır..."
    if command -v apt-get >/dev/null 2>&1; then
      export DEBIAN_FRONTEND=noninteractive
      apt-get install -y screen >/dev/null 2>&1 || { apt-get update -qq; apt-get install -y screen; }
    else
      echo "XƏTA: apt-get tapılmadı — screen-i əl ilə quraşdırın və ya NO_SCREEN=1 ilə işə salın."
      exit 1
    fi
  fi
  echo "→ 'static_ip' screen sessiyası başladılır (log: /root/set_static_ip.screen.log)..."
  # env variable-lar (STATIC_IP və s.) screen uşaq prosesinə keçir.
  exec screen -L -Logfile /root/set_static_ip.screen.log -S static_ip env IN_SCREEN=1 bash "$0"
fi

if [ -n "${STY:-}" ]; then
  echo "✓ İCRA SCREEN İÇİNDƏDİR — sessiya: ${STY} (SSH kəsilsə proses davam edəcək)"
elif [ -n "${TMUX:-}" ]; then
  echo "✓ İCRA TMUX İÇİNDƏDİR — sessiya: ${TMUX} (SSH kəsilsə proses davam edəcək)"
elif [ "${NO_SCREEN:-0}" = "1" ]; then
  echo "! QEYD: NO_SCREEN=1 — screen-siz icra olunur (SSH kəsilsə rollback baş verməyə bilər!)."
fi

# ------------- defaultlar + rollback funksiyası -------------
PREFIX="${PREFIX:-24}"
DNS_SERVERS="${DNS_SERVERS:-8.8.8.8,1.1.1.1}"
INTERFACE="${INTERFACE:-}"
GATEWAY="${GATEWAY:-}"

restore_backup() {
  for f in "$BACKUP_DIR"/*.yaml; do
    [ -e "$f" ] && cp -a "$f" /etc/netplan/ 2>/dev/null || true
  done
  netplan apply >/dev/null 2>&1 || true
  echo "Köhnə konfiq geri qaytarıldı ($BACKUP_DIR -> /etc/netplan)."
}

if ! printf '%s' "$STATIC_IP" | grep -qE '^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$'; then
  echo "XƏTA: STATIC_IP formatı yanlışdır: $STATIC_IP (misal: 192.168.1.50)"
  exit 1
fi

# ------------- interface autodetect -------------
if [ -z "$INTERFACE" ]; then
  INTERFACE=$(ls -1 /sys/class/net | grep -E '^(en|eth)' | head -n 1 || true)
  if [ -z "$INTERFACE" ]; then
    echo "XƏTA: ethernet interfeys tapılmadı. INTERFACE=ens18 kimi əl ilə verin."
    exit 1
  fi
fi

# ------------- gateway autodetect -------------
if [ -z "$GATEWAY" ]; then
  GATEWAY=$(ip route show default 2>/dev/null | awk '{print $3; exit}' || true)
fi
if [ -z "$GATEWAY" ]; then
  GATEWAY="${STATIC_IP%.*}.1"
  echo "QEYD: GATEWAY verilməyib, avtomatik təxmin edilir: $GATEWAY (yanlışdırsa GATEWAY=... ilə verin)"
fi

echo "=========================================="
echo " Interfeys : $INTERFACE"
echo " Statik IP : $STATIC_IP/$PREFIX"
echo " Gateway   : $GATEWAY"
echo " DNS       : $DNS_SERVERS"
echo "=========================================="
read -r -p "Davam edilsin? [yes/NO] " CONFIRM
[ "$CONFIRM" = "yes" ] || { echo "Ləğv olundu."; exit 0; }

# ------------- backup -------------
BACKUP_DIR="/root/netplan_backup_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$BACKUP_DIR"
cp -a /etc/netplan/*.yaml "$BACKUP_DIR/" 2>/dev/null || true
echo "Yedek saxlanıldı: $BACKUP_DIR"

# ------------- cloud-init deaktivasiyası (reboot-dan sonra reset olmasın) -------------
if [ -d /etc/cloud/cloud.cfg.d ]; then
  printf 'network: {config: disabled}\n' > /etc/cloud/cloud.cfg.d/99-disable-network-config.cfg
  echo "cloud-init şəbəkə idarəsi deaktiv edildi."
fi

# ------------- köhnə konfiqləri kənarlaşdır (dhcp konflikti olmasın) -------------
NEW_FILE="/etc/netplan/99-static-ip.yaml"
for f in /etc/netplan/*.yaml; do
  [ "$f" = "$NEW_FILE" ] || mv "$f" "$BACKUP_DIR/" 2>/dev/null || true
done

# ------------- yeni netplan faylı -------------
cat > "$NEW_FILE" <<EOF
network:
  version: 2
  ethernets:
    ${INTERFACE}:
      dhcp4: false
      dhcp6: false
      addresses:
        - ${STATIC_IP}/${PREFIX}
      routes:
        - to: default
          via: ${GATEWAY}
      nameservers:
        addresses: [${DNS_SERVERS}]
EOF
chmod 600 "$NEW_FILE"

# ------------- sintaksis yoxlaması (apply etmədən) -------------
if ! netplan generate; then
  echo "XƏTA: netplan konfiqurasiyası yanlışdır — geri qaytarılır."
  rm -f "$NEW_FILE"
  restore_backup
  exit 1
fi

# ------------- tətbiq + canlı yoxlama + auto-rollback -------------
echo "Tətbiq olunur..."
netplan apply
sleep 5

PRIMARY_DNS=$(printf '%s' "$DNS_SERVERS" | cut -d, -f1)
if ping -c 2 -W 2 "$GATEWAY" >/dev/null 2>&1 || ping -c 2 -W 2 "$PRIMARY_DNS" >/dev/null 2>&1; then
  echo "OK: şəbəkə işləyir. Cari vəziyyət:"
  ip -4 addr show "$INTERFACE" | grep inet || true
  ip route show default || true
  echo "Hazırdır. Statik IP: $STATIC_IP (reboot-dan sonra da qalıcıdır)."
else
  echo "XƏTA: bağlantı yoxdur — köhnə konfiq avtomatik geri qaytarılır..."
  rm -f "$NEW_FILE"
  restore_backup
  exit 1
fi
