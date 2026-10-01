package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHome points every loxx path at a fresh temporary home directory, and
// makes setup behave as on Linux, where both shells are set up, whatever
// system the tests run on (TestSetupOnMacOS checks macOS).
func fakeHome(t *testing.T, loginShell string) string {
	t.Helper()
	pretendOS(t, "linux")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/"+loginShell)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("LOXX_DB_PATH", "")
	t.Setenv("LOXX_HOOK", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	return home
}

// pretendOS makes setup and status behave as on system (a GOOS value) until
// the test ends.
func pretendOS(t *testing.T, system string) {
	t.Helper()
	real := goos
	goos = system
	t.Cleanup(func() { goos = real })
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetupStatusUninstall(t *testing.T) {
	home := fakeHome(t, "bash")
	bashrc := filepath.Join(home, ".bashrc")
	original := "export EDITOR=vim\nalias ll='ls -l'" // no final newline, on purpose
	write(t, bashrc, original)
	write(t, filepath.Join(home, ".bash_history"), "git status\nmake build\n")

	_, stderr := runOK(t, "setup")
	if !strings.Contains(stderr, "added the hook to ~/.bashrc") || !strings.Contains(stderr, "imported 2 command(s)") {
		t.Errorf("setup output:\n%s", stderr)
	}
	self, _ := os.Executable()
	withHook := read(t, bashrc)
	if !strings.Contains(withHook, "# >>> loxx >>>") || !strings.Contains(withHook, "'"+self+"' init bash") {
		t.Errorf("~/.bashrc after setup:\n%s", withHook)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !errors.Is(err, os.ErrNotExist) {
		t.Error("setup created ~/.zshrc although zsh is neither in use nor the login shell")
	}

	// Running it again changes nothing.
	if _, stderr = runOK(t, "setup"); !strings.Contains(stderr, "already set up") || !strings.Contains(stderr, "already imported") {
		t.Errorf("second setup output:\n%s", stderr)
	}
	if read(t, bashrc) != withHook {
		t.Error("a second setup changed ~/.bashrc")
	}

	stdout, _ := runOK(t, "status")
	for _, want := range []string{"2 unique", "bash:", "hook installed", "this shell: hook not active"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status output lacks %q:\n%s", want, stdout)
		}
	}
	t.Setenv("LOXX_HOOK", "bash")
	if stdout, _ = runOK(t, "status"); !strings.Contains(stdout, "this shell: hook active (bash)") {
		t.Errorf("status with the hook loaded:\n%s", stdout)
	}

	// Pretend install.sh installed a binary, so uninstall removes it.
	bin := filepath.Join(home, ".local", "bin", "loxx")
	write(t, bin, "binary")
	write(t, filepath.Join(home, ".local", "state", "loxx", "installed-binary"), bin+"\n")

	runOK(t, "uninstall", "--delete-data")
	if got := read(t, bashrc); got != original {
		t.Errorf("~/.bashrc not restored byte for byte:\n got %q\nwant %q", got, original)
	}
	for _, gone := range []string{
		filepath.Join(home, ".local", "share", "loxx"),
		bin,
		filepath.Join(home, ".local", "state", "loxx"),
	} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists after uninstall --delete-data", gone)
		}
	}
}

func TestUninstallKeepsDataAndForeignBinary(t *testing.T) {
	home := fakeHome(t, "zsh")
	write(t, filepath.Join(home, ".zsh_history"), ": 1759000000:0;ls\n")
	runOK(t, "setup") // no ~/.zshrc, but zsh is the login shell: it gets created
	if !strings.Contains(read(t, filepath.Join(home, ".zshrc")), "init zsh") {
		t.Fatal("setup did not create ~/.zshrc for a zsh login shell")
	}
	_, stderr := runOK(t, "uninstall", "--keep-data")
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the ~/.zshrc that setup created was not removed again")
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "loxx", "history.db")); err != nil {
		t.Error("--keep-data deleted the history")
	}
	if !strings.Contains(stderr, "was not installed by install.sh") {
		t.Errorf("uninstall must leave a binary it didn't install:\n%s", stderr)
	}
}

// TestSetupOnMacOS checks that on macOS setup adds the hook to ~/.zshrc only,
// never to ~/.bashrc (macOS's bash is too old for it), and tells someone
// whose login shell is bash how to switch.
func TestSetupOnMacOS(t *testing.T) {
	home := fakeHome(t, "zsh")
	pretendOS(t, "darwin")
	bashrc := filepath.Join(home, ".bashrc")
	write(t, bashrc, "# from the bash days\n")
	write(t, filepath.Join(home, ".bash_history"), "brew update\n")

	_, stderr := runOK(t, "setup")
	if !strings.Contains(stderr, "added the hook to ~/.zshrc") || !strings.Contains(stderr, "imported 1 command(s)") {
		t.Errorf("setup output:\n%s", stderr)
	}
	if strings.Contains(stderr, "chsh") {
		t.Errorf("zsh is the login shell, so there's nothing to switch:\n%s", stderr)
	}
	if read(t, bashrc) != "# from the bash days\n" {
		t.Error("setup changed ~/.bashrc on macOS")
	}
	stdout, _ := runOK(t, "status")
	if strings.Contains(stdout, "bash:") || !strings.Contains(stdout, "zsh:") {
		t.Errorf("status on macOS should list zsh only:\n%s", stdout)
	}

	// A bash login shell with no ~/.zshrc: nothing to set up, and the
	// message says how to switch to zsh.
	home = fakeHome(t, "bash")
	pretendOS(t, "darwin")
	write(t, filepath.Join(home, ".bashrc"), "# bash\n")
	var out, errOut strings.Builder
	if code := run([]string{"setup", "--no-import"}, &out, &errOut); code != 1 {
		t.Errorf("setup with only bash on macOS: exit %d, want 1\n%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "chsh -s /bin/zsh") || !strings.Contains(errOut.String(), "found no ~/.zshrc") {
		t.Errorf("setup should explain how to switch to zsh:\n%s", errOut.String())
	}
	if read(t, filepath.Join(home, ".bashrc")) != "# bash\n" {
		t.Error("setup changed ~/.bashrc on macOS")
	}
}

func TestSetupSkipsHandWrittenHook(t *testing.T) {
	home := fakeHome(t, "bash")
	bashrc := filepath.Join(home, ".bashrc")
	mine := "export A=1\neval \"$(~/Desktop/code/bin/loxx init bash)\"\n"
	write(t, bashrc, mine)
	_, stderr := runOK(t, "setup", "--no-import")
	if read(t, bashrc) != mine {
		t.Error("setup edited a file that already loads the hook by hand")
	}
	if !strings.Contains(stderr, "already loads loxx with a line you added") {
		t.Errorf("setup should explain why it skipped the file:\n%s", stderr)
	}
	if stdout, _ := runOK(t, "status"); !strings.Contains(stdout, "hook installed (a line you added)") {
		t.Errorf("status should recognize the hand-written hook:\n%s", stdout)
	}
}

func TestForgetCommand(t *testing.T) {
	t.Setenv("LOXX_DB_PATH", filepath.Join(t.TempDir(), "history.db"))
	runOK(t, "add", "curl https://internal.example.com/token-abc")
	runOK(t, "add", "curl https://internal.example.com/other")
	runOK(t, "add", "git status")

	_, stderr := runOK(t, "forget", "--match", "internal.example.com", "--yes")
	if !strings.Contains(stderr, "forgot 2 command(s) (2 runs)") {
		t.Errorf("forget --match output:\n%s", stderr)
	}
	if code := run([]string{"search", "internal"}, &strings.Builder{}, &strings.Builder{}); code != 1 {
		t.Error("forgotten commands can still be found")
	}
	if _, stderr = runOK(t, "forget", "--last", "--yes"); !strings.Contains(stderr, "git status") {
		t.Errorf("forget --last should name the most recent command:\n%s", stderr)
	}
	for args, want := range map[string]int{
		"forget":                                 2, // neither --match nor --last
		"forget --match x --last":                2, // both
		"forget --match nothing-like-this --yes": 1,
	} {
		if code := run(strings.Fields(args), &strings.Builder{}, &strings.Builder{}); code != want {
			t.Errorf("loxx %s: exit %d, want %d", args, code, want)
		}
	}
}

// TestSetupOnFreshMachineNeverDoubleCounts reproduces what the fresh-Fedora
// container test found: with no history file at the first setup, bash writes
// one later (on exit) containing commands the hook already recorded live, and
// a later setup (e.g. re-running the installer) must not import them again.
func TestSetupOnFreshMachineNeverDoubleCounts(t *testing.T) {
	home := fakeHome(t, "bash")
	write(t, filepath.Join(home, ".bashrc"), "# fresh\n")
	runOK(t, "setup") // no ~/.bash_history yet
	write(t, filepath.Join(home, ".bash_history"), "echo recorded-live\n")
	_, stderr := runOK(t, "setup")
	if strings.Contains(stderr, "imported 1 command") {
		t.Errorf("a history file that appeared after the first setup was imported:\n%s", stderr)
	}
	// An explicit `loxx import --force` still works when someone wants it.
	if _, stderr = runOK(t, "import", "--force"); !strings.Contains(stderr, "imported 1 command") {
		t.Errorf("import --force should still import:\n%s", stderr)
	}
}
