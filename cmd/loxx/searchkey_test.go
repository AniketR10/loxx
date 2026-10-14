//go:build linux || darwin

package main

import (
	"os/exec"
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

const (
	keyUp    = "\x1b[A"
	keyEsc   = "\x1b"
	keyCtrlG = "\x07"
)

// TestSearchKey opens the search with Ctrl-G in real bash and zsh.
func TestSearchKey(t *testing.T) {
	bin := buildLoc(t)
	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			s := startShell(t, sh.name, sh.args)
			s.typeLines(`eval "$('`+bin+`' init `+sh.name+`)"`, `loxx add 'echo picked-$((40+2))'`)
			if !strings.Contains(s.out.String(), "loxx is on: press Ctrl-G") {
				t.Errorf("no startup message\nscreen:\n%s", s.out.String())
			}

			s.key("picked", 200*time.Millisecond)
			s.key(keyCtrlG, 1500*time.Millisecond)
			if !strings.Contains(s.out.String(), "loxx ❯") {
				t.Fatalf("Ctrl-G did not open the search\nscreen:\n%s", s.out.String())
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
			s.key(keyCtrlG, 1500*time.Millisecond)
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

// TestSearchKeyKeepsYourBinding: loxx never takes a Ctrl-G you bound yourself.
func TestSearchKeyKeepsYourBinding(t *testing.T) {
	bin := buildLoc(t)
	for _, c := range []struct{ shell, bind, check string }{
		{"zsh", `bindkey '^G' beginning-of-line`, `bindkey '^G'`},
		{"bash", `bind '"\C-g": beginning-of-line'`, `bind -p | grep -F '"\C-g"'`},
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
			t.Errorf("%s: your Ctrl-G binding was replaced\n%s", c.shell, out)
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
