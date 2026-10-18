package main

import (
	"time"

	"github.com/maximerivest/remagic/cli/internal/device"
	"github.com/maximerivest/remagic/cli/internal/setupassets"
)

const hashtabPath = "/home/root/xovi/exthome/qt-resource-rebuilder/hashtab"

// Recovery command and doctor diagnostics from upstream PR #3. Reuse the
// guarded setup scripts rather than maintaining another rebuild implementation.
func cmdRebuildHashtab(d *device.Device) {
	defer d.Close()
	step("preparing stock xochitl for the resource rebuild")
	if _, err := d.Run(`test -s /home/root/xovi/extensions.d/qt-resource-rebuilder.so && systemctl reset-failed xochitl && /home/root/xovi/stock`); err != nil {
		die("could not prepare resource rebuild: %v", err)
	}
	if _, err := d.Run(`if [ -f /home/root/xovi/extensions.d/appload.so ]; then
mkdir -p /home/root/xovi/exthome/appload
mv /home/root/xovi/extensions.d/appload.so /home/root/xovi/exthome/appload/.remagic-appload.so
fi`); err != nil {
		die("could not stage AppLoad hooks: %v", err)
	}
	step("rebuilding the Qt resource hashtable")
	if err := rebuildHashtable(d); err != nil {
		die("resource rebuild failed: %v; %s", err, restoreStock(d))
	}
	fresh, err := d.Fresh()
	if err != nil {
		die("reconnect after rebuild: %v", err)
	}
	d.Close()
	*d = *fresh
	step("starting and validating AppLoad")
	if err := runSetupScript(d, "remagic-start", setupassets.StartXovi, 90*time.Second); err != nil {
		die("AppLoad startup failed: %v; %s", err, restoreStock(d))
	}
	ok("resource table rebuilt and AppLoad hooks validated")
}

func hasHashtab(d *device.Device) bool {
	_, err := d.Run("test -s " + hashtabPath)
	return err == nil
}
