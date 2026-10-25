//go:build linux || darwin

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// loginShell of a fresh account: bash on Linux, zsh on macOS.
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

// TestInstallScriptInTerminal runs install.sh in a terminal, as `curl … | sh`
// does, under each shell people run it with: it draws the progress bar and
// installs, and Ctrl-C stops the download at once and installs nothing.
func TestInstallScriptInTerminal(t *testing.T) {
	bin := buildLoc(t)
	rel := release(t, bin, false)
	installer, err := filepath.Abs("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"sh", "bash", "zsh", "dash"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skipf("%s not installed", shell)
			}
			t.Run("install", func(t *testing.T) {
				url, _ := releaseServer(t, rel, false)
				s, home := startInstaller(t, shell, installer, url)
				if code := s.exitCode(); code != 0 {
					t.Fatalf("install.sh exited %d\n%s", code, s.out.String())
				}
				out := s.out.String()
				// A fast local download shows one line under the bar: a joke or a tip.
				quip := strings.Contains(out, "tip: ") || strings.Contains(out, "...") || strings.Contains(out, "deal?")
				if !strings.Contains(out, "] 100%") || !strings.Contains(out, " MB") || !quip || strings.Contains(out, "0/0 MB") {
					t.Errorf("want a progress bar with the size and a joke or tip under it:\n%s", out)
				}
				if read(t, filepath.Join(home, ".local", "bin", "loxx")) != read(t, bin) {
					t.Error("the installed binary differs from the release")
				}
				if !strings.Contains(out, "loxx: done.") {
					t.Errorf("setup did not run:\n%s", out)
				}
			})
			t.Run("ctrl-c", func(t *testing.T) {
				url, hungUp := releaseServer(t, rel, true)
				// As in a real terminal, the user's own shell runs the installer
				// and stays open, so only the installer can stop its download.
				home := installerHome(t, url)
				term := shells[0]
				s := startPTY(t, home, filepath.Join(home, "history.db"), nil, term.name, term.args...)
				time.Sleep(500 * time.Millisecond) // let the shell start
				s.typeLines(shell + " '" + installer + "'; echo exit=$?")
				for deadline := time.Now().Add(30 * time.Second); !strings.Contains(s.out.String(), " MB"); time.Sleep(50 * time.Millisecond) {
					if time.Now().After(deadline) {
						t.Fatalf("no progress bar\n%s", s.out.String())
					}
				}
				if _, err := s.master.WriteString(ctrlC); err != nil {
					t.Fatal(err)
				}
				pressed := time.Now()
				for deadline := pressed.Add(5 * time.Second); !strings.Contains(s.out.String(), "exit=130"); time.Sleep(50 * time.Millisecond) {
					if time.Now().After(deadline) {
						t.Fatalf("install.sh did not exit with 130 after Ctrl-C\n%s", s.out.String())
					}
				}
				select {
				case <-hungUp:
				case <-time.After(5 * time.Second):
					t.Error("the download kept going after Ctrl-C")
				}
				if d := time.Since(pressed); d > 3*time.Second {
					t.Errorf("stopping took %v", d)
				}
				if _, err := os.Stat(filepath.Join(home, ".local", "bin", "loxx")); !errors.Is(err, os.ErrNotExist) {
					t.Error("loxx was installed although the download was cancelled")
				}
				s.typeLines("exit")
				s.wait()
			})
		})
	}
}

// startInstaller runs installer with shell in a new terminal, with a fresh home
// and the release at url.
func startInstaller(t *testing.T, shell, installer, url string) (*session, string) {
	t.Helper()
	home := installerHome(t, url)
	return startPTY(t, home, filepath.Join(home, "history.db"), nil, shell, installer), home
}

// installerHome returns a fresh home and points the installer at url.
func installerHome(t *testing.T, url string) string {
	t.Helper()
	home := t.TempDir()
	write(t, filepath.Join(home, "."+loginShell+"rc"), "")
	t.Setenv("SHELL", "/bin/"+loginShell)
	t.Setenv("LOXX_RELEASE_URL", url)
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("XDG_STATE_HOME", "")
	return home
}

// exitCode waits for the process to exit and returns its exit code.
func (s *session) exitCode() int {
	s.t.Helper()
	err := s.cmd.Wait()
	<-s.copied
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if err != nil {
		s.t.Fatalf("%v\n%s", err, s.out.String())
	}
	return 0
}

// releaseServer serves dir the way GitHub serves a release: /NAME redirects to
// /files/NAME, which has a Content-Length. With slow set it sends about 1 MB/s.
// hungUp is closed when a client stops a download before the end.
func releaseServer(t *testing.T, dir string, slow bool) (url string, hungUp <-chan struct{}) {
	t.Helper()
	gone := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := strings.CutPrefix(r.URL.Path, "/files/")
		if !ok {
			http.Redirect(w, r, "/files"+r.URL.Path, http.StatusFound)
			return
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.Base(name)))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		for len(b) > 0 {
			n := min(len(b), 64<<10)
			if _, err := w.Write(b[:n]); err != nil {
				once.Do(func() { close(gone) })
				return
			}
			b = b[n:]
			if !slow {
				continue
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				once.Do(func() { close(gone) })
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, gone
}
