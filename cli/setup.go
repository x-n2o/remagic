package main

// remagic setup — the whole installer in one command, pure Go, no bash/ssh/git
// on the laptop. Mirrors scripts/01..03 of the shell installer:
//
//	1. preflight   (model check — Paper Pro only)
//	2. ssh key     (generated if you have none, then installed)
//	3. xovi bundle (loader + scripts, from asivery's pinned release)
//	4. AppLoad     (only appload.so into extensions.d; shims into exthome)
//	5. hashtable   (required for AppLoad's UI button; built non-interactively)
//	6. tripletap   (power-button persistence; script streamed from the laptop
//	                over verified TLS, executed on the device)
//	7. ssh repair  (tripletap's installer knocks over the host-key mount on
//	                fresh devices; we fix it in-band over the live connection)
//	8. start xovi  (no reboot needed)
//	9. the Store   (so the next app install needs no computer at all)
//
// One password prompt at most: the single SSH connection is reused throughout.

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maximerivest/remagic/cli/internal/device"
	"github.com/maximerivest/remagic/cli/internal/setupassets"
)

// Pinned upstream releases (same pins as scripts/sources.env).
const (
	xoviBundleURL = "https://github.com/asivery/rm-xovi-extensions/releases/download/v19-23052026/xovi-aarch64.tar.gz"
	tripletapURL  = "https://raw.githubusercontent.com/rmitchellscott/xovi-tripletap/main/install.sh"
)

type apploadRelease struct {
	Version string
	URL     string
	Archive string
}

func selectAppload(osVersion string) (apploadRelease, error) {
	parts := strings.Split(strings.TrimSpace(osVersion), ".")
	if len(parts) < 2 {
		return apploadRelease{}, fmt.Errorf("cannot select AppLoad for OS %q", osVersion)
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || major != 3 || minor < 0 || minor > 29 {
		return apploadRelease{}, fmt.Errorf("OS %q is outside the validated AppLoad range (3.x through 3.29)", osVersion)
	}
	version, archive := "v0.5.3", "appload-aarch64.zip"
	if minor >= 28 {
		version, archive = "v0.6.0", "appload-aarch64.tar.gz"
	}
	return apploadRelease{version, "https://github.com/asivery/rm-appload/releases/download/" + version + "/" + archive, archive}, nil
}

func setupSSHKey(d *device.Device, skip bool) error {
	if skip {
		return nil
	}
	return ensureSSHKey(d)
}

func restoreStock(d *device.Device) string {
	const command = `systemctl reset-failed xochitl && /home/root/xovi/stock && systemctl reset-failed xochitl && systemctl start xochitl && systemctl is-active --quiet xochitl`
	_, err := d.Run(command)
	if err != nil {
		fresh, ferr := d.Fresh()
		if ferr != nil {
			return fmt.Sprintf("could not verify stock UI recovery: %v", ferr)
		}
		defer fresh.Close()
		_, err = fresh.Run(command)
	}
	if err != nil {
		return fmt.Sprintf("stock UI recovery failed: %v", err)
	}
	return "stock UI restored"
}

func cmdSetup(d *device.Device, catalogURL string, skipSSHKey bool) {
	defer d.Close()

	fmt.Println(`
  remagic setup — make your reMarkable Paper Pro magical.

  This will, over the connection just opened:
    1. Verify this is a Paper Pro in developer mode
    2. Set up SSH authentication (or preserve keys with --skip-ssh-key)
    3. Install xovi + the AppLoad launcher (official upstream releases)
    4. Set up power-button persistence (preserving existing configuration)
    5. Install the Store app, so future installs need no computer

  Nothing here touches your notebooks or the bootloader. Rebooting returns
  you to stock reMarkable; triple-press enables xovi again.`)
	fmt.Println()

	// ---- 1. preflight -----------------------------------------------------
	step("checking the device")
	model, _ := d.Run(`cat /proc/device-tree/model 2>/dev/null | tr -d '\0'`)
	model = strings.TrimSpace(model)
	if !isPaperPro(model) {
		die("device reports %q — remagic targets the reMarkable Paper Pro (Ferrari/Chiappa/Tatsu); refusing to touch an unrecognized device", model)
	}
	ok("device: %s", model)
	osv, err := d.Run(`. /etc/os-release; printf %s "$IMG_VERSION"`)
	if err != nil {
		die("read OS version: %v", err)
	}
	release, err := selectAppload(osv)
	if err != nil {
		die("%v", err)
	}
	ok("OS %s: AppLoad %s", strings.TrimSpace(osv), release.Version)

	step("setting up SSH")
	if err := setupSSHKey(d, skipSSHKey); err != nil {
		die("SSH setup: %v", err)
	}
	if skipSSHKey {
		ok("preserving existing SSH keys (key setup skipped)")
	}
	// Stop old hooks before replacing any of their files.
	if _, err := d.Run(`systemctl reset-failed xochitl; if [ -x /home/root/xovi/stock ]; then /home/root/xovi/stock; fi`); err != nil {
		die("could not restore stock UI before setup: %v", err)
	}
	if _, err := d.Run(`if [ -f /home/root/xovi/extensions.d/appload.so ]; then
mkdir -p /home/root/xovi/exthome/appload
mv /home/root/xovi/extensions.d/appload.so /home/root/xovi/exthome/appload/.remagic-appload.so
fi`); err != nil {
		die("could not stage existing AppLoad hooks: %v", err)
	}

	// ---- 3. xovi bundle ----------------------------------------------------
	step("downloading the xovi bundle")
	xovi, err := fetchURL(xoviBundleURL)
	if err != nil {
		die("download xovi: %v", err)
	}
	ok("%d KB", len(xovi)/1024)
	step("installing xovi onto the tablet")
	if err := d.Push(xovi, "/tmp/xovi.tar.gz", "644"); err != nil {
		die("%v", err)
	}
	if out, err := d.Run("tar -xzf /tmp/xovi.tar.gz -C /home/root && rm -f /tmp/xovi.tar.gz"); err != nil {
		die("extract xovi: %v: %s", err, out)
	}
	// Activate the message broker (ships inactive; several community apps use it).
	d.Run(`[ -f /home/root/xovi/inactive-extensions/xovi-message-broker.so ] && \
mv -f /home/root/xovi/inactive-extensions/xovi-message-broker.so /home/root/xovi/extensions.d/ 2>/dev/null; true`)
	ok("xovi installed at /home/root/xovi")

	// ---- 4. AppLoad ---------------------------------------------------------
	step("downloading AppLoad")
	appload, err := fetchURL(release.URL)
	if err != nil {
		die("download AppLoad: %v", err)
	}
	ok("%d KB", len(appload)/1024)
	step("installing the AppLoad launcher")
	if err := d.Push(appload, "/tmp/"+release.Archive, "644"); err != nil {
		die("%v", err)
	}
	if err := d.Push([]byte(setupassets.InstallAppload), "/tmp/remagic-install-appload.sh", "755"); err != nil {
		die("%v", err)
	}
	if out, err := d.Run("bash /tmp/remagic-install-appload.sh /tmp/" + release.Archive); err != nil {
		die("install AppLoad: %v: %s; %s", err, out, restoreStock(d))
	}

	ok("AppLoad installed")

	// ---- 5. Qt hashtable -----------------------------------------------------
	// Without the hashtab, qt-resource-rebuilder can't patch xochitl's QML and
	// the AppLoad button never appears in the UI — this step is NOT optional.
	// The stock rebuild_hashtable script is interactive (press-enter prompt,
	// parses xochitl stdout), so we run our own non-interactive rebuild,
	// detached via systemd: stopping/starting xochitl drops Wi-Fi SSH
	// connections, so the work must survive our session and we poll for the
	// result over fresh connections.
	step("building the Qt resource hashtable (AppLoad's UI hook needs it; ~1-2 min)")
	if err := rebuildHashtable(d); err != nil {
		die("Qt resource table build failed: %v; %s", err, restoreStock(d))
	}
	ok("hashtable built")
	if _, err := d.Run("true"); err != nil {
		if fresh, cerr := d.Fresh(); cerr == nil {
			d.Close()
			*d = *fresh
		} else {
			die("reconnect: %v", cerr)
		}
	}

	// ---- 6. tripletap persistence -------------------------------------------
	step("installing power-button persistence (xovi-tripletap)")
	fmt.Println("    triple-press enables xovi after a reboot; existing settings are preserved")
	if _, err := d.Run("systemctl is-active --quiet xovi-tripletap && systemctl is-enabled --quiet xovi-tripletap"); err == nil {
		ok("reusing healthy xovi-tripletap and its configuration")
	} else {
		tt, err := fetchURL(tripletapURL)
		if err != nil {
			warn("download tripletap: %v — skipping; start xovi manually after reboots", err)
		} else if out, err := d.RunIn("bash -s", strings.NewReader(string(tt))); err != nil {
			warn("tripletap install didn't complete: %v: %s", err, tail(out, 300))
			warn("you can still start xovi manually: ssh root@%s '/home/root/xovi/start'", d.Addr)
		} else {
			ok("tripletap installed")
		}
	}

	// ---- 7. repair the SSH keystore tripletap knocks over -------------------
	step("checking the SSH server survived")
	repairSSH(d)
	if fresh, err := d.Fresh(); err == nil {
		fresh.Close()
		ok("SSH healthy — verified with a fresh connection")
	} else {
		warn("new SSH connections aren't completing; one reboot of the tablet fixes it")
	}

	// ---- 8. start xovi now ---------------------------------------------------
	step("starting xovi (no reboot needed)")
	if err := runSetupScript(d, "remagic-start", setupassets.StartXovi, 90*time.Second); err != nil {
		die("AppLoad startup failed: %v; %s", err, restoreStock(d))
	}
	ok("AppLoad hooks loaded; xochitl remained stable")

	// ---- 9. the Store ----------------------------------------------------------
	step("installing the Store app (browse + install apps on the tablet itself)")
	storeOK := false
	if err := installFromCatalog(d, "store", catalogURL); err == nil {
		storeOK = true
	} else {
		// Starting xovi restarts xochitl, which can drop the long-lived SSH
		// connection mid-push. A fresh connection almost always succeeds.
		if fresh, cerr := d.Fresh(); cerr == nil {
			if err2 := installFromCatalog(fresh, "store", catalogURL); err2 == nil {
				storeOK = true
			} else {
				warn("store install skipped: %v — you can add it later: remagic install store", err2)
			}
			fresh.Close()
		} else {
			warn("store install skipped: %v — you can add it later: remagic install store", err)
		}
	}
	if storeOK {
		ok("Store installed")
	}

	fmt.Println(`
  ✓ Setup complete. Try it now, on the tablet:

    1. Wake the tablet — the reMarkable home screen looks unchanged. That's
       normal: your apps live inside the AppLoad launcher.
    2. Open the top-left menu on My Files, then tap AppLoad.
       If you don't see it yet, triple-press the power button and wait a
       few seconds — that enables xovi.`)
	if storeOK {
		fmt.Println(`    3. Inside AppLoad, open the Store to browse and install apps —
       no computer needed from here on.`)
	} else {
		fmt.Println(`    3. Install apps from this computer with: remagic install <app>
       (the on-tablet Store was skipped; add it with: remagic install store)`)
	}
	fmt.Println(`
  Everyday commands:

    remagic install <app>     install apps from this computer
    remagic config <app>      configure an app from your browser
    remagic wifi on           make the cable optional
    remagic doctor            health check any time

  Triple-press enables xovi after reboot. Existing start/toggle settings remain.
  Re-run 'remagic setup' after a reMarkable OS update.`)
}

// rebuildHashtable builds qt-resource-rebuilder's hashtab non-interactively.
// It runs xochitl once with only qt-resource-rebuilder loaded (in a scratch
// XOVI_ROOT) and QMLDIFF_HASHTAB_CREATE set; qmldiff hashes every QML/resource
// and writes the hashtab ~60-90s in. The whole thing runs as a transient
// systemd unit because it stops xochitl, which can drop Wi-Fi SSH sessions —
// we poll for completion over fresh connections instead.
func rebuildHashtable(d *device.Device) error {
	return runSetupScript(d, "remagic-hashtab", setupassets.RebuildHashtable, 4*time.Minute)
}

func runSetupScript(d *device.Device, unit, script string, timeout time.Duration) error {
	path := "/tmp/" + unit + ".sh"
	result := "/tmp/" + unit + ".result"
	if err := d.Push([]byte(script), path, "755"); err != nil {
		return err
	}
	command := "rm -f " + result + "; systemctl reset-failed " + unit + " 2>/dev/null; systemd-run --unit=" + unit + " --collect /bin/bash " + path
	if out, err := d.Run(command); err != nil {
		return fmt.Errorf("launch %s: %v: %s", unit, err, tail(out, 300))
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		fresh, err := d.Fresh()
		if err != nil {
			continue
		}
		out, _ := fresh.Run("cat " + result + " 2>/dev/null")
		fresh.Close()
		switch strings.TrimSpace(out) {
		case "success":
			return nil
		case "failed":
			return fmt.Errorf("%s failed; inspect journalctl -u %s and /tmp/remagic-hashtab.log", unit, unit)
		}
	}
	// Do not let a late background success enable hooks after recovery begins.
	if fresh, err := d.Fresh(); err == nil {
		fresh.Run("systemctl stop " + unit)
		fresh.Close()
	}
	return fmt.Errorf("%s timed out", unit)
}

func isPaperPro(model string) bool {
	for _, m := range []string{"Ferrari", "Chiappa", "Tatsu"} {
		if strings.Contains(model, m) {
			return true
		}
	}
	return false
}

// fetchURL downloads a release asset with the laptop's verified TLS.
func fetchURL(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s from %s", resp.Status, url)
	}
	return io.ReadAll(resp.Body)
}

// tail returns the last n bytes of s, for compact error reporting.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
