//go:build linux || darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupRemembersTheLink: package managers start loxx through a link that
// stays put while the file behind it moves on every upgrade (Homebrew:
// bin/loxx -> Cellar/loxx/<version>/bin/loxx). setup and init must remember
// the link, or the hook silently stops working after an upgrade.
func TestSetupRemembersTheLink(t *testing.T) {
	bin := buildLoc(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cellar := func(version string) string { return filepath.Join(root, "Cellar", "loxx", version, "bin", "loxx") }
	install := func(version string) {
		if err := os.MkdirAll(filepath.Dir(cellar(version)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cellar(version), []byte(read(t, bin)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "bin", "loxx")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	install("1.0")
	if err := os.Symlink(cellar("1.0"), link); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	write(t, rc, "")
	env := []string{
		"HOME=" + home,
		"SHELL=/bin/zsh",
		"PATH=" + filepath.Dir(link) + ":/usr/bin:/bin",
		"LOXX_DB_PATH=" + filepath.Join(home, "history.db"),
		"LOXX_BACKGROUND_EMBED=0",
	}
	// Run it by name, the way a user types `loxx setup` after `brew install`.
	setup := &exec.Cmd{Path: link, Args: []string{"loxx", "setup", "--no-import"}, Env: env}
	if out, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("loxx setup: %v\n%s", err, out)
	}
	if got := read(t, rc); !strings.Contains(got, "'"+link+"' init zsh") || strings.Contains(got, "Cellar") {
		t.Fatalf("setup should remember the link %s, not the versioned file:\n%s", link, got)
	}

	// Upgrade: a new version, the link moved to it, the old version deleted.
	install("2.0")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cellar("2.0"), link); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "Cellar", "loxx", "1.0")); err != nil {
		t.Fatal(err)
	}
	// The rc block runs the link by its full path; the hook it prints must
	// keep using the link too.
	initCmd := &exec.Cmd{Path: link, Args: []string{link, "init", "zsh"}, Env: env}
	out, err := initCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("loxx init after the upgrade: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "_loxx_bin='"+link+"'") {
		t.Errorf("the hook should record through the link %s:\n%s", link, firstLines(string(out), 8))
	}

	// A start path that leads to a different file is not trusted.
	other := &exec.Cmd{Path: link, Args: []string{"/bin/sh", "init", "zsh"}, Env: env}
	if out, err := other.CombinedOutput(); err != nil || strings.Contains(string(out), "_loxx_bin='/bin/sh'") {
		t.Errorf("an unrelated start path was trusted: err=%v\n%s", err, firstLines(string(out), 8))
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	return strings.Join(lines[:min(n, len(lines))], "\n")
}
