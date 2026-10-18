package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApploadFirmwareSelectionMatchesShell(t *testing.T) {
	for _, tc := range []struct{ os, version, suffix string }{
		{"3.26.0.0", "v0.5.3", ".zip"},
		{"3.27.3.0", "v0.5.3", ".zip"},
		{"3.28.0.172", "v0.6.0", ".tar.gz"},
		{"3.29.0.149", "v0.6.0", ".tar.gz"},
	} {
		t.Run(tc.os, func(t *testing.T) {
			release, err := selectAppload(tc.os)
			if err != nil || release.Version != tc.version || !strings.HasSuffix(release.Archive, tc.suffix) {
				t.Fatalf("selection = %+v, %v", release, err)
			}
			cmd := exec.Command("bash", "-c", `source ../scripts/sources.env; select_appload "$1"; printf '%s' "$APPLOAD_URL"`, "test", tc.os)
			cmd.Env = append(os.Environ(), "APPLOAD_TAG=", "RMPP_KIT_LATEST=0")
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != release.URL {
				t.Fatalf("shell = %q, %v; Go = %s", out, err, release.URL)
			}
		})
	}
	for _, version := range []string{"", "garbage", "3.29x", "4.0.0", "3.30.0"} {
		if _, err := selectAppload(version); err == nil {
			t.Errorf("accepted unknown firmware %q", version)
		}
		cmd := exec.Command("bash", "-c", `source ../scripts/sources.env; select_appload "$1"`, "test", version)
		cmd.Env = append(os.Environ(), "APPLOAD_TAG=", "RMPP_KIT_LATEST=0")
		if err := cmd.Run(); err == nil {
			t.Errorf("shell accepted unknown firmware %q", version)
		}
	}
}

func TestSkipSSHKeyPreservesExistingKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".ssh")
	os.Mkdir(dir, 0700)
	for _, name := range []string{"id_ed25519", "id_ed25519.pub", "config"} {
		os.WriteFile(filepath.Join(dir, name), []byte("preserve me"), 0600)
	}
	// A nil device also proves no remote key installation can run.
	if err := setupSSHKey(nil, true); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"id_ed25519", "id_ed25519.pub", "config"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != "preserve me" {
			t.Fatalf("changed %s", name)
		}
	}
}
