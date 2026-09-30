//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLocFunction types `loxx` in real bash and zsh sessions, picks a command
// in the panel, then runs what the shell prepared: in zsh the next prompt is
// pre-filled (Enter runs it); in bash it is one Up arrow away. The chosen
// command prints "picked-42" only when it actually runs.
func TestLocFunction(t *testing.T) {
	bin := buildLoc(t)
	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			s := startShell(t, sh.name, sh.args)
			use := "\r" // zsh: the prompt already holds the command
			if sh.name == "bash" {
				use = "\x1b[A\r" // bash: Up arrow fetches it from history
			}
			s.typeLines(
				`eval "$('`+bin+`' init `+sh.name+`)"`,
				`loxx add 'echo picked-$((40+2))'`,
				// A cancelled panel must leave nothing behind.
				"loxx", ctrlC,
				`echo after-cancel-$((1+1))`,
				"loxx",
			)
			for _, k := range []string{"picked", "\r", use} {
				if _, err := s.master.WriteString(k); err != nil {
					t.Fatal(err)
				}
				time.Sleep(800 * time.Millisecond)
			}
			s.typeLines("exit")
			s.wait()
			screen := s.out.String()
			if !strings.Contains(screen, "after-cancel-2") {
				t.Errorf("after cancelling, the next command did not run cleanly\nscreen:\n%s", screen)
			}
			if !strings.Contains(screen, "picked-42") {
				t.Errorf("the chosen command was not ready to run\nscreen:\n%s", screen)
			}
		})
	}
}

// TestPanel drives the real search panel in a pseudo-terminal: keys go in,
// and only the chosen command may come out on stdout.
func TestPanel(t *testing.T) {
	bin := buildLoc(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history.db")
	loxx := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "LOXX_DB_PATH="+dbPath, "LOXX_BACKGROUND_EMBED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("loxx %v: %v\n%s", args, err, out)
		}
	}
	loxx("add", "--exit", "0", "sudo systemctl restart nginx")
	loxx("add", "--exit", "2", "make deploy")
	loxx("add", "--exit", "0", "tar -czvf backup.tar.gz ~/Documents")
	loxx("add", "--exit", "0", "git status") // the most recent
	loxx("embed", "--pending")

	tests := []struct {
		name string
		keys []string // typed with a pause after each
		want string   // stdout; "" means cancelled
		code int
	}{
		{"keyword match", []string{"nginx", "\r"}, "sudo systemctl restart nginx\n", 0},
		{"empty query shows the most recent first", []string{"\r"}, "git status\n", 0},
		{"arrow keys move the selection", []string{"\x1b[B", "\r"}, "tar -czvf backup.tar.gz ~/Documents\n", 0},
		// No session id was given, so Tab skips "this session": all → this dir → failed.
		{"Tab cycles to the failed filter", []string{"\t", "\t", "\r"}, "make deploy\n", 0},
		{"meaning, with no shared words", []string{"compress my documents folder", "\r"}, "tar -czvf backup.tar.gz ~/Documents\n", 0},
		{"Esc cancels", []string{"nginx", "\x1b"}, "", 1},
		{"Ctrl-C cancels", []string{"\x03"}, "", 1},
	}
	// Meaning-based search always has a nearest command, so "no results"
	// needs an empty history.
	tests = append(tests, struct {
		name string
		keys []string
		want string
		code int
	}{"Enter with no results chooses nothing", []string{"anything", "\r"}, "", 1})

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := dbPath
			if i == len(tests)-1 {
				db = filepath.Join(t.TempDir(), "empty.db")
			}
			var stdout bytes.Buffer
			s := startPTY(t, dir, db, &stdout, bin, "panel")
			time.Sleep(300 * time.Millisecond) // first paint
			for _, k := range tt.keys {
				if _, err := s.master.WriteString(k); err != nil {
					t.Fatal(err)
				}
				// Typing pauses long enough for the meaning-based search.
				time.Sleep(700 * time.Millisecond)
			}
			err := s.cmd.Wait()
			<-s.copied
			code := 0
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if stdout.String() != tt.want || code != tt.code {
				t.Errorf("stdout %q, exit %d; want %q, exit %d\nscreen:\n%s", stdout.String(), code, tt.want, tt.code, s.out.String())
			}
		})
	}
}
