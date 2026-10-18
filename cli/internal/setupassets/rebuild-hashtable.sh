#!/bin/bash
# AppLoad must stay disabled until a complete table for this firmware exists.
set -euo pipefail
xovi=/home/root/xovi
tab=$xovi/exthome/qt-resource-rebuilder/hashtab
result=/tmp/remagic-hashtab.result
log=/tmp/remagic-hashtab.log
scratch=
pid=
success=false
cleanup() {
    trap - EXIT INT TERM
    if [ -n "$pid" ]; then
        kill "$pid" 2>/dev/null || true
        for i in $(seq 1 5); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
        kill -9 "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi
    [ -z "$scratch" ] || rm -rf "$scratch"
    rm -f "$tab.new"
    systemctl reset-failed xochitl 2>/dev/null || true
    systemctl start xochitl || success=false
    [ ! -w /sys/power/wake_unlock ] || echo remagic-hashtab > /sys/power/wake_unlock
    if "$success"; then echo success > "$result"; else echo failed > "$result"; fi
}
trap cleanup EXIT
trap 'exit 1' INT TERM
test -s "$xovi/extensions.d/qt-resource-rebuilder.so"
[ ! -w /sys/power/wake_lock ] || echo remagic-hashtab > /sys/power/wake_lock
systemctl stop xochitl
scratch=$(mktemp -d /tmp/remagic-hashtab.XXXXXX)
mkdir -p "$scratch/extensions.d" "$(dirname "$tab")"
ln -s "$xovi/extensions.d/qt-resource-rebuilder.so" "$scratch/extensions.d/"
rm -f "$tab.new"
XOVI_ROOT="$scratch" QMLDIFF_HASHTAB_CREATE="$tab.new" QML_DISABLE_DISK_CACHE=1 \
    LD_PRELOAD="$xovi/xovi.so" /usr/bin/xochitl > "$log" 2>&1 &
pid=$!
for i in $(seq 1 150); do
    if [ -s "$tab.new" ] && grep -qF "Hashtab saved to $tab.new" "$log"; then
        mv "$tab.new" "$tab"
        success=true
        exit 0
    fi
    kill -0 "$pid" 2>/dev/null || { tail -n 30 "$log"; exit 1; }
    sleep 1
done
echo 'Qt resource table generation timed out; leaving stock xochitl active.' >&2
exit 1
