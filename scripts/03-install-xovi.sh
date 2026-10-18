#!/usr/bin/env bash
# Firmware-aware xovi/AppLoad installation. Device steps are shared with Go.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
source "$HERE/lib.sh"
source "$HERE/sources.env"
ASSETS="$HERE/../cli/internal/setupassets"
select_appload "$(rm_os_version)" || die "unsupported tablet firmware"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"; rm_ssh_cleanup' EXIT

fetch() {
    step "Downloading $(basename "$2")"
    if command -v curl >/dev/null; then curl -fL --retry 3 -o "$2" "$1"
    else wget -O "$2" "$1"; fi
}

run_device_step() {
    local unit="$1" script="$2" attempts="$3" state
    rm_scp "$ASSETS/$script" "$(rm_dest):/tmp/$unit.sh"
    rm_ssh "rm -f /tmp/$unit.result; systemctl reset-failed $unit 2>/dev/null; systemd-run --unit=$unit --collect /bin/bash /tmp/$unit.sh" || return 1
    for ((i=0; i<attempts; i++)); do
        sleep 3
        state="$(rm_ssh "cat /tmp/$unit.result 2>/dev/null" 2>/dev/null || true)"
        case "$state" in success) return 0 ;; failed) return 1 ;; esac
    done
    rm_ssh "systemctl stop $unit" || true
    return 1
}

restore_stock() {
    if rm_ssh 'systemctl reset-failed xochitl && /home/root/xovi/stock && systemctl reset-failed xochitl && systemctl start xochitl && systemctl is-active --quiet xochitl'; then
        ok "Stock UI restored."
    else
        warn "Could not verify stock UI recovery; inspect the device over SSH."
    fi
}

# Never replace extensions while they are running.
rm_ssh 'systemctl reset-failed xochitl; if [ -x /home/root/xovi/stock ]; then /home/root/xovi/stock; fi'
rm_ssh 'if [ -f /home/root/xovi/extensions.d/appload.so ]; then
mkdir -p /home/root/xovi/exthome/appload
mv /home/root/xovi/extensions.d/appload.so /home/root/xovi/exthome/appload/.remagic-appload.so
fi'
fetch "$XOVI_BUNDLE_URL" "$WORK/xovi.tar.gz"
rm_scp "$WORK/xovi.tar.gz" "$(rm_dest):/tmp/xovi.tar.gz"
rm_ssh 'tar -xzf /tmp/xovi.tar.gz -C /home/root && rm -f /tmp/xovi.tar.gz'
rm_ssh '[ ! -f /home/root/xovi/inactive-extensions/xovi-message-broker.so ] || mv /home/root/xovi/inactive-extensions/xovi-message-broker.so /home/root/xovi/extensions.d/'

step "Installing AppLoad $APPLOAD_TAG"
fetch "$APPLOAD_URL" "$WORK/$APPLOAD_ASSET"
rm_scp "$WORK/$APPLOAD_ASSET" "$(rm_dest):/tmp/$APPLOAD_ASSET"
rm_scp "$ASSETS/install-appload.sh" "$(rm_dest):/tmp/remagic-install-appload.sh"
rm_ssh "bash /tmp/remagic-install-appload.sh /tmp/$APPLOAD_ASSET" || { restore_stock; die "AppLoad installation failed"; }

step "Building the required firmware-specific Qt resource table"
run_device_step remagic-hashtab rebuild-hashtable.sh 80 || {
    restore_stock; die "resource table rebuild failed. Inspect journalctl -u remagic-hashtab";
}

if rm_ssh 'systemctl is-active --quiet xovi-tripletap && systemctl is-enabled --quiet xovi-tripletap'; then
    ok "Reusing healthy xovi-tripletap and its configuration."
else
    fetch "$TRIPLETAP_INSTALL_URL" "$WORK/tripletap.sh"
    rm_ssh 'bash -s' < "$WORK/tripletap.sh"
    repair_ssh_keystore
fi

step "Starting and validating AppLoad"
run_device_step remagic-start start-xovi.sh 30 || {
    restore_stock; die "AppLoad startup failed. Inspect journalctl -u remagic-start";
}
ok "AppLoad hooks loaded and xochitl remained stable."
