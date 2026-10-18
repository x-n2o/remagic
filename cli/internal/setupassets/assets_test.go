package setupassets

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runScript(t *testing.T, script, root string, args ...string) ([]byte, error) {
	t.Helper()
	script = strings.ReplaceAll(script, "/home/root/xovi", filepath.Join(root, "xovi"))
	script = strings.ReplaceAll(script, "/tmp/remagic-", filepath.Join(root, "remagic-"))
	script = strings.ReplaceAll(script, "/run/systemd/system", filepath.Join(root, "systemd"))
	script = strings.ReplaceAll(script, "/proc/", filepath.Join(root, "proc")+"/")
	// All tests run in a temporary tree; systemctl never touches the host.
	script = strings.ReplaceAll(script, "/sys/power/wake_", filepath.Join(root, "wake_"))
	path := filepath.Join(root, "step.sh")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0700)
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CALLS\"\n[ \"$1\" != show ] || echo 123\n"), 0700)
	os.WriteFile(filepath.Join(bin, "sleep"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	os.WriteFile(filepath.Join(bin, "journalctl"), []byte("#!/bin/sh\ncat \"$JOURNAL\"\n"), 0700)
	cmd := exec.Command("bash", append([]string{path}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "CALLS="+filepath.Join(root, "calls"), "JOURNAL="+filepath.Join(root, "journal"))
	return cmd.CombinedOutput()
}

func TestStartupPromotesHooksOnlyAfterSuccessfulValidation(t *testing.T) {
	for _, panicInHooks := range []bool{false, true} {
		name := "valid hooks"
		if panicInHooks {
			name = "hook panic"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{"xovi/extensions.d", "xovi/exthome/appload", "xovi/exthome/qt-resource-rebuilder", "proc/123"} {
				os.MkdirAll(filepath.Join(root, dir), 0700)
			}
			os.WriteFile(filepath.Join(root, "xovi/exthome/qt-resource-rebuilder/hashtab"), []byte("complete table"), 0600)
			os.WriteFile(filepath.Join(root, "xovi/exthome/appload/.remagic-appload.so"), []byte("new hooks"), 0600)
			os.WriteFile(filepath.Join(root, "proc/123/maps"), []byte("xovi.so\nappload.so\n"), 0600)
			os.WriteFile(filepath.Join(root, "xovi/start"), []byte("#!/bin/sh\nexit 0\n"), 0700)
			os.WriteFile(filepath.Join(root, "xovi/stock"), []byte("#!/bin/sh\necho stock >> \"$CALLS\"\n"), 0700)
			logs := "Loaded external AppLoad hooks in main UI\n"
			if panicInHooks {
				logs += "panicked: Couldn't resolve the hashed identifier\n"
			}
			os.WriteFile(filepath.Join(root, "journal"), []byte(logs), 0600)
			out, err := runScript(t, StartXovi, root)
			if (err != nil) != panicInHooks {
				t.Fatalf("err=%v, logs=%s", err, out)
			}
			result, _ := os.ReadFile(filepath.Join(root, "remagic-start.result"))
			if panicInHooks {
				if strings.TrimSpace(string(result)) != "failed" {
					t.Fatalf("result=%q", result)
				}
				if _, err := os.Stat(filepath.Join(root, "xovi/extensions.d/appload.so")); !os.IsNotExist(err) {
					t.Fatal("bad hooks stayed enabled")
				}
				if _, err := os.Stat(filepath.Join(root, "xovi/exthome/appload/.remagic-appload.so")); err != nil {
					t.Fatal("bad hooks were not quarantined")
				}
			} else {
				if strings.TrimSpace(string(result)) != "success" {
					t.Fatalf("result=%q", result)
				}
				if _, err := os.Stat(filepath.Join(root, "xovi/extensions.d/appload.so")); err != nil {
					t.Fatal("validated hooks were not enabled")
				}
			}
			if _, err := os.Stat(filepath.Join(root, "systemd/xochitl.service.d/99-remagic-validation.conf")); !os.IsNotExist(err) {
				t.Fatal("temporary validation guard was not removed")
			}
		})
	}
}

func TestInstallBothArchiveFormats(t *testing.T) {
	for _, suffix := range []string{".zip", ".tar.gz"} {
		t.Run(suffix, func(t *testing.T) {
			root := t.TempDir()
			os.MkdirAll(filepath.Join(root, "xovi/extensions.d"), 0700)
			os.WriteFile(filepath.Join(root, "xovi/extensions.d/appload.so"), []byte("old hooks"), 0600)
			archive := filepath.Join(root, "appload"+suffix)
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			files := map[string]string{"appload.so": "extension", "shims/qtfb-shim.so": "shim", "exthome/extra/data": "data"}
			if suffix == ".zip" {
				w := zip.NewWriter(file)
				for name, data := range files {
					entry, _ := w.Create(name)
					entry.Write([]byte(data))
				}
				w.Close()
			} else {
				gz := gzip.NewWriter(file)
				w := tar.NewWriter(gz)
				for name, data := range files {
					w.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0644})
					w.Write([]byte(data))
				}
				w.Close()
				gz.Close()
			}
			file.Close()
			if out, err := runScript(t, InstallAppload, root, archive); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			for name, expected := range map[string]string{"exthome/appload/.remagic-appload.so": "extension", "exthome/appload/shims/qtfb-shim.so": "shim", "exthome/extra/data": "data"} {
				data, err := os.ReadFile(filepath.Join(root, "xovi", name))
				if err != nil || string(data) != expected {
					t.Fatalf("%s: %q, %v", name, data, err)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "xovi/extensions.d/appload.so")); !os.IsNotExist(err) {
				t.Fatal("AppLoad was enabled before rebuilding its table")
			}
			if _, err := os.Stat(filepath.Join(root, "xovi/extensions.d/qtfb-shim.so")); !os.IsNotExist(err) {
				t.Fatal("shim installed as an extension")
			}
		})
	}
}

func TestRebuildFailureRestoresUI(t *testing.T) {
	root := t.TempDir()
	if out, err := runScript(t, RebuildHashtable, root); err == nil {
		t.Fatalf("accepted missing rebuilder: %s", out)
	}
	data, _ := os.ReadFile(filepath.Join(root, "remagic-hashtab.result"))
	if strings.TrimSpace(string(data)) != "failed" {
		t.Fatalf("result = %q", data)
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	if !strings.Contains(string(calls), "start xochitl") {
		t.Fatalf("UI not restored: %s", calls)
	}
	if strings.Index(string(calls), "reset-failed xochitl") < 0 || strings.Index(string(calls), "reset-failed xochitl") > strings.Index(string(calls), "start xochitl") {
		t.Fatalf("restart limit not reset before restoring UI: %s", calls)
	}
}

func TestStartupFailureRestoresStock(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "xovi"), 0700)
	os.MkdirAll(filepath.Join(root, "xovi/extensions.d"), 0700)
	os.WriteFile(filepath.Join(root, "xovi/extensions.d/appload.so"), []byte("incompatible hooks"), 0600)
	os.WriteFile(filepath.Join(root, "xovi/stock"), []byte("#!/bin/sh\necho stock >> \"$CALLS\"\n"), 0700)
	if out, err := runScript(t, StartXovi, root); err == nil {
		t.Fatalf("accepted missing table: %s", out)
	}
	data, _ := os.ReadFile(filepath.Join(root, "remagic-start.result"))
	if strings.TrimSpace(string(data)) != "failed" {
		t.Fatalf("result = %q", data)
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	if !strings.Contains(string(calls), "stock\n") || !strings.Contains(string(calls), "start xochitl") {
		t.Fatalf("stock UI not restored: %s", calls)
	}
	if _, err := os.Stat(filepath.Join(root, "xovi/extensions.d/appload.so")); !os.IsNotExist(err) {
		t.Fatal("failed hooks still enabled for the power-button shortcut")
	}
	data, err := os.ReadFile(filepath.Join(root, "xovi/exthome/appload/.remagic-appload.so"))
	if err != nil || string(data) != "incompatible hooks" {
		t.Fatalf("failed hooks not preserved for repair: %q, %v", data, err)
	}
}
