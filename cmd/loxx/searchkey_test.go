//go:build linux || darwin

package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// key types one raw key sequence into the session and waits a moment.
func (s *session) key(k string, wait time.Duration) {
	s.t.Helper()
	if _, err := s.master.WriteString(k); err != nil {
		s.t.Fatal(err)
	}
	time.Sleep(wait)
}

// waitFor waits until text is on the screen; a new binary's first start can
// take a second or more (macOS scans it).
func (s *session) waitFor(text string) {
	s.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(s.plain(), text); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			s.t.Fatalf("%q never appeared\nscreen:\n%s", text, s.out.String())
		}
	}
}

const (
	keyEsc      = "\x1b"
	keyCtrlAltG = "\x1b\x07"
)

// TestSearchKey opens the search with Ctrl-Alt-G in real bash and zsh.
func TestSearchKey(t *testing.T) {
	bin := buildLoc(t)
	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			s := startShell(t, sh.name, sh.args)
			s.typeLines(`eval "$('` + bin + `' init ` + sh.name + `)"`)
			name := "Ctrl-Alt-G"
			if runtime.GOOS == "darwin" && sh.name == "zsh" {
				name = "Ctrl-Option-G" // what a Mac keyboard calls it
			}
			s.waitFor("loxx is on: type `loxx` or press " + name)
			s.typeLines(`loxx add 'echo picked-$((40+2))'`)

			s.key("picked", 200*time.Millisecond)
			s.key(keyCtrlAltG, 1500*time.Millisecond)
			if !strings.Contains(s.out.String(), "loxx ❯") {
				t.Fatalf("Ctrl-Alt-G did not open the search\nscreen:\n%s", s.out.String())
			}
			s.key("\r", time.Second) // the first result fills the line
			if strings.Contains(s.out.String(), "picked-42") {
				t.Fatalf("choosing ran the command\nscreen:\n%s", s.out.String())
			}
			s.key("\r", 600*time.Millisecond) // now it runs
			if !strings.Contains(s.out.String(), "picked-42") {
				t.Fatalf("the chosen command was not on the line\nscreen:\n%s", s.out.String())
			}

			s.key("echo kept-", 200*time.Millisecond) // Esc keeps the typed line
			s.key(keyCtrlAltG, 1500*time.Millisecond)
			s.key(keyEsc, 800*time.Millisecond)
			s.key("$((1+1))\r", 600*time.Millisecond)
			s.typeLines("exit")
			s.wait()
			screen := s.out.String()
			if !strings.Contains(screen, "kept-2") {
				t.Errorf("Esc did not keep the typed line\nscreen:\n%s", screen)
			}
			if strings.Contains(screen, "\a") {
				t.Errorf("the shell beeped\nscreen:\n%s", screen)
			}
		})
	}
}

// TestSearchKeyKeepsYourBinding: loxx never takes a Ctrl-Alt-G you bound yourself.
func TestSearchKeyKeepsYourBinding(t *testing.T) {
	bin := buildLoc(t)
	for _, c := range []struct{ shell, bind, check string }{
		{"zsh", `bindkey '^[^G' beginning-of-line`, `bindkey '^[^G'`},
		{"bash", `bind '"\e\C-g": beginning-of-line'`, `bind -p | grep -F '"\e\C-g"'`},
	} {
		if _, err := exec.LookPath(c.shell); err != nil {
			continue
		}
		args := []string{"-f", "-i", "-c"}
		if c.shell == "bash" {
			args = []string{"--norc", "--noprofile", "-i", "-c"}
		}
		script := c.bind + `; eval "$('` + bin + `' init ` + c.shell + `)"; ` + c.check
		out, _ := exec.Command(c.shell, append(args, script)...).CombinedOutput()
		if !strings.Contains(string(out), "beginning-of-line") {
			t.Errorf("%s: your Ctrl-Alt-G binding was replaced\n%s", c.shell, out)
		}
	}
}

// TestHookPutsLoxxOnPath: with the hook, scripts find `loxx` by name (a new
// Mac has no ~/.local/bin on PATH). Its folder is added once, last.
func TestHookPutsLoxxOnPath(t *testing.T) {
	bin := buildLoc(t)
	for _, sh := range shells {
		if _, err := exec.LookPath(sh.name); err != nil {
			continue
		}
		hook := `eval "$('` + bin + `' init ` + sh.name + `)"`
		script := `PATH=/usr/bin:/bin; ` + hook + `; ` + hook + `; sh -c 'command -v loxx'; echo "PATH=$PATH"`
		out, _ := exec.Command(sh.name, append(sh.args, "-c", script)...).CombinedOutput()
		if !strings.Contains(string(out), "\n"+bin+"\n") && !strings.HasPrefix(string(out), bin+"\n") {
			t.Errorf("%s: a script started from the shell can't find loxx\n%s", sh.name, out)
		}
		if want := "PATH=/usr/bin:/bin:" + filepath.Dir(bin) + "\n"; !strings.Contains(string(out), want) {
			t.Errorf("%s: want %q\n%s", sh.name, want, out)
		}
	}
}

// TestHookInWrongShell: a hook eval'd in the other shell must load nothing.
func TestHookInWrongShell(t *testing.T) {
	bin := buildLoc(t)
	for _, c := range []struct{ hook, shell, check string }{
		{"zsh", "bash", "declare -F _loxx_precmd >/dev/null && echo LOADED"},
		{"bash", "zsh", "(( ${+functions[_loxx_precmd]} )) && echo LOADED"},
	} {
		if _, err := exec.LookPath(c.shell); err != nil {
			continue
		}
		script := `eval "$('` + bin + `' init ` + c.hook + `)"; ` + c.check + `; true`
		out, err := exec.Command(c.shell, "-c", script).CombinedOutput()
		if err != nil || strings.Contains(string(out), "LOADED") || !strings.Contains(string(out), "this is the "+c.hook+" hook") {
			t.Errorf("%s hook in %s: err=%v\n%s", c.hook, c.shell, err, out)
		}
	}
}
