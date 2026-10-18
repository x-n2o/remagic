#!/bin/bash
set -euo pipefail
archive=$1
work=$(mktemp -d /tmp/remagic-appload.XXXXXX)
trap 'rm -rf "$work"' EXIT
case "$archive" in
    *.zip) unzip -oq "$archive" -d "$work" ;;
    *.tar.gz) tar -xzf "$archive" -C "$work" ;;
    *) echo "unsupported AppLoad archive: $archive" >&2; exit 1 ;;
esac
test -s "$work/appload.so"
mkdir -p /home/root/xovi/exthome/appload
# Keep hooks outside extensions.d until the firmware table has been rebuilt.
# A power-button shortcut during setup must not activate incompatible hooks.
cp "$work/appload.so" /home/root/xovi/exthome/appload/.remagic-appload.so
rm -f /home/root/xovi/extensions.d/appload.so
if [ -d "$work/shims" ]; then
    cp -R "$work/shims" /home/root/xovi/exthome/appload/
fi
if [ -d "$work/exthome" ]; then
    cp -R "$work/exthome/." /home/root/xovi/exthome/
fi
rm -f "$archive"
