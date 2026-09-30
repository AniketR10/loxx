package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddRemoveRoundTrip(t *testing.T) {
	block := Block("bash", "/home/a/.local/bin/loxx")
	for name, before := range map[string]string{
		"empty file":         "",
		"ends with newline":  "export EDITOR=vim\nalias ll='ls -l'\n",
		"no final newline":   "export EDITOR=vim\nalias ll='ls -l'",
		"trailing blank":     "export EDITOR=vim\n\n",
		"windows line ends":  "export EDITOR=vim\r\n",
		"unicode and quotes": "PS1='λ \\w $ '\n",
	} {
		t.Run(name, func(t *testing.T) {
			added, err := Add(before, block)
			if err != nil {
				t.Fatal(err)
			}
			if !HasBlock(added) || !strings.Contains(added, "init bash") {
				t.Fatalf("block not added:\n%s", added)
			}
			again, err := Add(added, block)
			if err != nil || again != added {
				t.Errorf("adding twice changed the file (err %v):\n%q\nvs\n%q", err, again, added)
			}
			removed, had, err := Remove(added)
			if err != nil || !had {
				t.Fatalf("Remove: had=%v err=%v", had, err)
			}
			if removed != before {
				t.Errorf("not byte-identical after remove:\n got %q\nwant %q", removed, before)
			}
		})
	}
}

func TestAddUpdatesInPlace(t *testing.T) {
	old, _ := Add("a\n", Block("zsh", "/old/loxx"))
	old += "b\n" // the user added something after the block
	updated, err := Add(old, Block("zsh", "/new/loxx"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(updated, "/old/loxx") || !strings.Contains(updated, "/new/loxx") || !strings.HasSuffix(updated, "b\n") {
		t.Errorf("block was not replaced in place:\n%s", updated)
	}
	if strings.Count(updated, beginMarker) != 1 {
		t.Errorf("duplicate blocks:\n%s", updated)
	}
}

func TestAddRefusesHandWrittenHook(t *testing.T) {
	for _, line := range []string{
		`eval "$(~/Desktop/coding/loc/bin/loxx init bash)"`,
		`eval "$(loxx init zsh)"`,
		`eval "$('/opt/x/loxx' init bash)"`,
	} {
		if _, err := Add("x=1\n"+line+"\n", Block("bash", "/b/loxx")); !errors.Is(err, ErrHandWritten) {
			t.Errorf("Add with %q: err = %v, want ErrHandWritten", line, err)
		}
	}
	// Comments that merely mention it are fine.
	if _, err := Add("# eval \"$(loxx init bash)\"\n", Block("bash", "/b/loxx")); err != nil {
		t.Errorf("a commented-out line should not count: %v", err)
	}
}

func TestUnterminatedBlock(t *testing.T) {
	broken := "a\n" + beginMarker + "\nstuff\n"
	if _, err := Add(broken, Block("bash", "/b/loxx")); err == nil {
		t.Error("Add should refuse a block without an end marker")
	}
	if _, _, err := Remove(broken); err == nil {
		t.Error("Remove should refuse a block without an end marker")
	}
}

func TestBlockQuotesPath(t *testing.T) {
	b := Block("bash", "/home/o'brien/bin/loxx")
	if !strings.Contains(b, `'/home/o'\''brien/bin/loxx'`) {
		t.Errorf("path not shell-quoted:\n%s", b)
	}
}

func TestEditFileFollowsSymlinksAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "bashrc")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("x=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".bashrc")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	changed, err := EditFile(link, func(s string) (string, error) { return Add(s, Block("bash", "/b/loxx")) })
	if err != nil || !changed {
		t.Fatalf("EditFile: changed=%v err=%v", changed, err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a regular file")
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode changed to %o", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(real); !HasBlock(string(b)) {
		t.Error("the link target was not edited")
	}
	// No change → no write.
	if changed, _ := EditFile(link, func(s string) (string, error) { return s, nil }); changed {
		t.Error("an unchanged file was reported as changed")
	}
	// A missing file is created.
	missing := filepath.Join(dir, ".zshrc")
	if changed, err := EditFile(missing, func(s string) (string, error) { return Add(s, Block("zsh", "/b/loxx")) }); err != nil || !changed {
		t.Errorf("creating a missing rc file: changed=%v err=%v", changed, err)
	}
}

func TestInstallUninstallFiles(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, ".bashrc")
	original := "export A=1" // no final newline
	if err := os.WriteFile(existing, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(dir, ".zshrc") // does not exist yet
	for _, f := range []struct{ path, shell string }{{existing, "bash"}, {created, "zsh"}} {
		for i := range 2 { // twice: the second run must change nothing
			changed, err := Install(f.path, f.shell, "/b/loxx")
			if err != nil || changed != (i == 0) {
				t.Errorf("Install(%s) run %d: changed=%v err=%v", f.path, i+1, changed, err)
			}
		}
	}
	for _, p := range []string{existing, created} {
		if changed, err := Uninstall(p); err != nil || !changed {
			t.Errorf("Uninstall(%s): changed=%v err=%v", p, changed, err)
		}
	}
	if b, _ := os.ReadFile(existing); string(b) != original {
		t.Errorf("existing file not restored exactly: %q", b)
	}
	if _, err := os.Stat(created); !errors.Is(err, os.ErrNotExist) {
		t.Error("a file setup created should be deleted again")
	}
	if changed, err := Uninstall(created); err != nil || changed {
		t.Errorf("uninstalling a missing file: changed=%v err=%v", changed, err)
	}
}
