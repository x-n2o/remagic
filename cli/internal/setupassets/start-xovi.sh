#!/bin/bash
# Suppress reboot/crash loops while testing the newly installed hooks.
set -euo pipefail
xovi=/home/root/xovi
result=/tmp/remagic-start.result
guard=/run/systemd/system/xochitl.service.d/99-remagic-validation.conf
success=false
cleanup() {
    trap - EXIT INT TERM
    rm -f "$guard"
    systemctl daemon-reload
    if "$success"; then
        echo success > "$result"
    else
        # A later power-button shortcut must not retry hooks that just failed.
        if [ -f "$xovi/extensions.d/appload.so" ]; then
            mkdir -p "$xovi/exthome/appload"
            mv "$xovi/extensions.d/appload.so" "$xovi/exthome/appload/.remagic-appload.so"
        fi
        systemctl reset-failed xochitl || true
        "$xovi/stock" || true
        systemctl reset-failed xochitl || true
        systemctl start xochitl
        echo failed > "$result"
    fi
}
trap cleanup EXIT
trap 'exit 1' INT TERM
test -s "$xovi/exthome/qt-resource-rebuilder/hashtab"
if [ -s "$xovi/exthome/appload/.remagic-appload.so" ]; then
    mv "$xovi/exthome/appload/.remagic-appload.so" "$xovi/extensions.d/appload.so"
fi
test -s "$xovi/extensions.d/appload.so"
mkdir -p "$(dirname "$guard")"
cat > "$guard" <<'GUARD'
[Unit]
OnFailure=
StartLimitAction=none
[Service]
Restart=no
GUARD
systemctl daemon-reload
started=$(date +%s)
systemctl reset-failed xochitl
"$xovi/start"
pid=$(systemctl show -p MainPID --value xochitl)
test "$pid" -gt 0
for i in $(seq 1 20); do
    systemctl is-active --quiet xochitl
    test "$(systemctl show -p MainPID --value xochitl)" = "$pid"
    sleep 1
done
grep -q 'xovi.so' "/proc/$pid/maps"
grep -q 'appload.so' "/proc/$pid/maps"
logs=$(journalctl --no-pager -u xochitl --since="@$started")
if printf '%s\n' "$logs" | grep -E 'panicked|Couldn.t resolve the hashed identifier'; then
    exit 1
fi
printf '%s\n' "$logs" | grep -q 'Loaded external AppLoad hooks in main UI'
success=true
