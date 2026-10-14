//go:build linux || darwin

package main

import (
	"os/exec"
	"regexp"
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
	keyDown = "\x1b[B"
	keyUp   = "\x1b[A"
	keyEsc  = "\x1b"
)

// TestZshSuggestions drives the as-you-type list in a real zsh.
func TestZshSuggestions(t *testing.T) {
	bin := buildLoc(t)
	t.Run("on", func(t *testing.T) {
		s := startShell(t, "zsh", []string{"-f", "-i"})
		s.typeLines(`eval "$('`+bin+`' init zsh)"`, `loxx add 'echo picked-$((40+2))'`)
		if !strings.Contains(s.out.String(), "loxx is on: type what you want to do") {
			t.Errorf("no startup message\nscreen:\n%s", s.out.String())
		}
		// The dim hint in the empty prompt (the startup line goes on with ", and").
		if !regexp.MustCompile(`like "undo my last commit"[^,]`).MatchString(s.out.String()) {
			t.Errorf("no hint in the empty prompt\nscreen:\n%s", s.out.String())
		}

		before := s.out.String()
		s.key("echo pic", 1500*time.Millisecond) // past both pauses
		if shown := strings.TrimPrefix(s.out.String(), before); !strings.Contains(shown, "just now") {
			t.Fatalf("no suggestions after a pause\nscreen:\n%s", shown)
		}
		s.key("\r", 600*time.Millisecond) // nothing chosen yet: runs the typed line
		s.key("echo pic", 1500*time.Millisecond)
		s.key(keyDown, 200*time.Millisecond)
		s.key("\r", 300*time.Millisecond) // chosen: into the prompt, not run
		if strings.Contains(s.out.String(), "picked-42") {
			t.Fatalf("choosing a suggestion ran it\nscreen:\n%s", s.out.String())
		}
		s.key("\r", 600*time.Millisecond) // now it runs

		// Ctrl-C while the meaning search still runs: its results must not
		// appear under the next prompt.
		s.key("echo pic", 700*time.Millisecond)
		s.key("\x03", 0)
		atCtrlC := s.out.String()
		time.Sleep(1500 * time.Millisecond)
		if late := strings.TrimPrefix(s.out.String(), atCtrlC); strings.Contains(late, "just now") {
			t.Errorf("a search for a cancelled line drew its results later\nafter Ctrl-C:\n%s", late)
		}
		s.typeLines("exit 0") // (Ctrl-C left $? at 130)
		s.wait()
		screen := s.out.String()
		if strings.Contains(screen, "\a") {
			t.Errorf("zsh beeped\nscreen:\n%s", screen)
		}
		// `echo pic` printed its own line (after zle's clear-to-end code, if any).
		if !regexp.MustCompile(`(\n|\r|\x1b\[J)pic\r\n`).MatchString(screen) {
			t.Errorf("Enter without Down did not run the typed line\nscreen:\n%s", screen)
		}
		if !strings.Contains(screen, "picked-42") {
			t.Errorf("the chosen suggestion did not run on the second Enter\nscreen:\n%s", screen)
		}
	})

	t.Run("off", func(t *testing.T) {
		s := startShell(t, "zsh", []string{"-f", "-i"})
		s.typeLines("export LOXX_SUGGEST=0 LOXX_QUIET=1", `eval "$('`+bin+`' init zsh)"`, `loxx add 'echo picked-$((40+2))'`)
		before := s.out.String()
		s.key("echo pic", 1500*time.Millisecond)
		if shown := strings.TrimPrefix(s.out.String(), before); strings.Contains(shown, "just now") {
			t.Errorf("LOXX_SUGGEST=0 still shows suggestions\nscreen:\n%s", shown)
		}
		s.key("\x15", 100*time.Millisecond) // clear the line
		s.typeLines("exit")
		s.wait()
		if strings.Contains(s.out.String(), "loxx is on") || strings.Contains(s.out.String(), "undo my last commit") {
			t.Errorf("LOXX_QUIET=1 still shows the startup message or the prompt hint\nscreen:\n%s", s.out.String())
		}
	})
}

// TestBashDescription drives loxx's Enter handling in a real bash.
func TestBashDescription(t *testing.T) {
	if !strings.Contains(strings.Join(setupShells(), " "), "bash") {
		t.Skip("loxx doesn't set up bash on this system")
	}
	bin := buildLoc(t)
	s := startShell(t, "bash", []string{"--norc", "--noprofile", "-i"})
	s.typeLines(
		// A handler the system already had must still answer for typos.
		`command_not_found_handle() { echo "system-handler-$1"; return 127; }`,
		`eval "$('`+bin+`' init bash)"`,
		`loxx add 'echo picked-$((40+2))'`,
		`loxx embed --pending`,
	)
	time.Sleep(2 * time.Second) // the embed above loads the model

	s.key("print the picked number\r", 2*time.Second)
	if !regexp.MustCompile(`loxx ❯ (\x1b\[[0-9;]*m)*print the picked number`).MatchString(s.out.String()) {
		t.Fatalf("the description did not open the list\nscreen:\n%s", s.out.String())
	}
	s.key("\r", time.Second) // choose the first result: it replaces the line
	if strings.Contains(s.out.String(), "picked-42") {
		t.Fatalf("choosing ran the command\nscreen:\n%s", s.out.String())
	}
	s.key("\r", 600*time.Millisecond) // the chosen command is on the line: Enter runs it
	if !strings.Contains(s.out.String(), "picked-42") {
		t.Fatalf("the chosen command was not on the command line\nscreen:\n%s", s.out.String())
	}

	s.key("nosuchthing here\r", 1500*time.Millisecond)
	s.key(keyEsc, 800*time.Millisecond)
	if strings.Contains(s.out.String(), "system-handler-nosuchthing") {
		t.Fatalf("Esc should keep the line, not run it\nscreen:\n%s", s.out.String())
	}
	s.key("\r", 600*time.Millisecond) // the same line again: runs as typed

	s.key("loxx\r", 1500*time.Millisecond) // plain loxx: the list, then the choice on the line
	s.key("picked", 800*time.Millisecond)  // (a pause, as a person makes, so the list updates)
	s.key("\r", time.Second)
	s.key("\r", 600*time.Millisecond)

	s.typeLines(`echo "exit=$?"`, `echo "x$(nosuchcmd)y"`, `history`, "exit")
	s.wait()
	screen := s.out.String()
	if n := strings.Count(screen, "picked-42"); n != 2 {
		t.Errorf("picked-42 printed %d times, want 2 (description, then plain loxx)\nscreen:\n%s", n, screen)
	}
	for _, want := range []string{"system-handler-nosuchthing", "exit=0"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q\nscreen:\n%s", want, screen)
		}
	}
	// Between typing the $(...) line and its output, no list may open.
	sub := screen[strings.Index(screen, `echo "x$(nosuchcmd)y"`):] // where it was typed, not the history listing
	// (The stand-in system handler prints inside the $(...): "x…y".)
	if out := strings.Index(sub, "xsystem-handler-nosuchcmdy"); out < 0 || strings.Contains(sub[:out], "loxx ❯") {
		t.Errorf("a missing command inside $(...) opened the list\nscreen:\n%s", sub)
	}
	if hist := screen[strings.LastIndex(screen, "history"):]; strings.Contains(hist, "print the picked number") {
		t.Errorf("the description is in bash's history:\n%s", hist)
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
