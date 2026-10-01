//go:build linux || darwin

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// loginShell is the login shell of the fresh account install.sh runs in:
// bash on Linux, and zsh on macOS (its default, and the only shell loxx sets
// up there).
var loginShell = map[string]string{"linux": "bash", "darwin": "zsh"}[runtime.GOOS]

// release builds a local "GitHub release" directory, as install.sh expects
// one: loxx-<os>-<arch> and checksums.txt.
func release(t *testing.T, bin string, corrupt bool) string {
	t.Helper()
	dir := t.TempDir()
	asset := "loxx-" + runtime.GOOS + "-" + runtime.GOARCH
	b, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, asset), string(b))
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	if corrupt {
		hash = strings.Repeat("0", 64)
	}
	write(t, filepath.Join(dir, "checksums.txt"), hash+"  "+asset+"\n")
	return dir
}

// runInstaller runs install.sh in a clean environment: only the variables a
// fresh login would have, pointed at a temporary home.
func runInstaller(t *testing.T, home, releaseDir string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "../../install.sh")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"SHELL=/bin/" + loginShell,
		"LOXX_RELEASE_URL=file://" + releaseDir,
		"LOXX_BACKGROUND_EMBED=0",
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestInstallScript(t *testing.T) {
	bin := buildLoc(t)
	rel := release(t, bin, false)
	home := t.TempDir()
	rc := filepath.Join(home, "."+loginShell+"rc")
	original := "# my shell settings\nexport EDITOR=vim\n"
	write(t, rc, original)
	write(t, filepath.Join(home, "."+loginShell+"_history"), "git status\n")

	out, err := runInstaller(t, home, rel)
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	installed := filepath.Join(home, ".local", "bin", "loxx")
	if a, b := read(t, installed), read(t, bin); a != b {
		t.Error("the installed binary differs from the release")
	}
	if fi, _ := os.Stat(installed); fi.Mode().Perm()&0o111 == 0 {
		t.Error("the installed binary is not executable")
	}
	if got := read(t, filepath.Join(home, ".local", "state", "loxx", "installed-binary")); strings.TrimSpace(got) != installed {
		t.Errorf("installed-binary marker = %q", got)
	}
	if !strings.Contains(read(t, rc), "'"+installed+"' init "+loginShell) || !strings.Contains(out, "imported 1 command(s)") {
		t.Errorf("setup did not run as part of the install:\n%s\n%s:\n%s", out, rc, read(t, rc))
	}

	// Installing twice must not add a second block.
	if out, err := runInstaller(t, home, rel); err != nil {
		t.Fatalf("second install.sh: %v\n%s", err, out)
	}
	if n := strings.Count(read(t, rc), "# >>> loxx >>>"); n != 1 {
		t.Errorf("%d loxx blocks after installing twice, want 1", n)
	}

	// And uninstall takes it all back.
	un := exec.Command(installed, "uninstall", "--delete-data")
	un.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}
	if out, err := un.CombinedOutput(); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if got := read(t, rc); got != original {
		t.Errorf("%s after uninstall:\n got %q\nwant %q", rc, got, original)
	}
	if _, err := os.Stat(installed); !errors.Is(err, os.ErrNotExist) {
		t.Error("uninstall left the installed binary")
	}
}

func TestInstallScriptRefusesBadChecksum(t *testing.T) {
	bin := buildLoc(t)
	home := t.TempDir()
	out, err := runInstaller(t, home, release(t, bin, true))
	if err == nil || !strings.Contains(out, "checksum mismatch") {
		t.Fatalf("install.sh with a bad checksum: err=%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "loxx")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a binary with a bad checksum was installed")
	}
}

func TestInstallScriptMissingAsset(t *testing.T) {
	home := t.TempDir()
	out, err := runInstaller(t, home, t.TempDir()) // an empty "release"
	if err == nil || !strings.Contains(out, "could not download") {
		t.Fatalf("install.sh with no binary to download: err=%v\n%s", err, out)
	}
}
