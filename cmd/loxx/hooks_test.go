//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

type testShell struct {
	name string
	args []string
}

// shells are the interactive shells the hooks support on this system (see
// setupShells: zsh only on macOS), started without any rc files so only the
// loxx hook is loaded.
var shells = slices.DeleteFunc([]testShell{
	{"bash", []string{"--norc", "--noprofile", "-i"}},
	{"zsh", []string{"-f", "-i"}},
}, func(sh testShell) bool { return !slices.Contains(setupShells(), sh.name) })

// ctrlC typed into a session interrupts the running command.
const ctrlC = "\x03"

// TestShellHooks runs real interactive bash and zsh sessions with the hook
// from `loxx init` loaded, inside a pseudo-terminal so they behave as if a
// person were typing, then checks what was recorded.
func TestShellHooks(t *testing.T) {
	bin := buildLoc(t)
	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			s := startShell(t, sh.name, sh.args)
			histOut := filepath.Join(s.dir, "history-out")
			s.typeLines(
				`eval "$('`+bin+`' init `+sh.name+`)"`,
				`echo marker-one`,
				`false`,
				` echo marker-secret`,
				`echo café 日本`,
				`for i in 1 2; do`,
				`echo loop-$i`,
				`done`,
				`sleep 30`,
				ctrlC,
				`history > '`+histOut+`'`,
				`exit`,
			)
			s.wait()

			got := s.recordedWhen(func(es []execution) bool {
				return hasCommand(es, "echo marker-one") && hasCommand(es, "sleep 30")
			})
			byText := map[string]execution{}
			for _, e := range got {
				byText[e.text] = e
			}
			one, ok := byText["echo marker-one"]
			if !ok {
				t.Fatalf("echo marker-one was not recorded; got %+v\nsession:\n%s", got, s.out.String())
			}
			if one.exitCode.Int64 != 0 || one.cwd.String != s.dir || one.session.String == "" ||
				!one.startedAt.Valid || !one.durationMS.Valid || one.source.String != "live" {
				t.Errorf("echo marker-one recorded as %+v", one)
			}
			if f, ok := byText["false"]; !ok || f.exitCode.Int64 != 1 {
				t.Errorf("false: recorded=%v, exit=%d; want exit 1", ok, f.exitCode.Int64)
			}
			if _, ok := byText["echo café 日本"]; !ok {
				t.Error("the unicode command was not recorded exactly")
			}
			if n := countContaining(got, "for i in 1 2"); n != 1 || countContaining(got, "loop-$i") != 1 {
				t.Errorf("the multi-line loop should be one record containing all its lines; %d records mention it", n)
			}
			if sleep, ok := byText["sleep 30"]; !ok || sleep.exitCode.Int64 != 130 {
				t.Errorf("Ctrl-C'd sleep: recorded=%v, exit=%d; want exit 130", ok, sleep.exitCode.Int64)
			}
			if countContaining(got, "marker-secret") != 0 {
				t.Error("a space-prefixed command was recorded")
			}
			if sh.name == "bash" {
				// bash-preexec turns ignorespace off; the hook must still keep
				// space-prefixed commands out of bash's own history.
				hist, err := os.ReadFile(histOut)
				if err != nil {
					t.Fatalf("reading the history dump: %v\nsession:\n%s", err, s.out.String())
				}
				if strings.Contains(string(hist), "marker-secret") {
					t.Errorf("space-prefixed command is in bash's history:\n%s", hist)
				}
			}
		})
	}
}

// TestShellHooksHangup closes the terminal while a command runs, as closing a
// terminal window does. The shell dies without showing another prompt, so
// the command must be recorded by the exit hook, with its exit code unknown.
func TestShellHooksHangup(t *testing.T) {
	bin := buildLoc(t)
	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			s := startShell(t, sh.name, sh.args)
			s.typeLines(`eval "$('`+bin+`' init `+sh.name+`)"`, `echo before-hangup`, `sleep 30`)
			// Closing a terminal window makes the kernel send SIGHUP to the
			// shell. (Closing s.master here would not: Go defers the close
			// until the goroutine blocked reading it returns.)
			if err := s.cmd.Process.Signal(syscall.SIGHUP); err != nil {
				t.Fatal(err)
			}
			s.cmd.Wait() // killed by the hangup: an error is expected
			got := s.recordedWhen(func(es []execution) bool { return hasCommand(es, "sleep 30") })
			var sleep *execution
			for i := range got {
				if got[i].text == "sleep 30" {
					sleep = &got[i]
				}
			}
			if sleep == nil {
				t.Fatalf("the running command was lost on hangup; recorded %+v\nsession:\n%s", got, s.out.String())
			}
			if sleep.exitCode.Valid || sleep.durationMS.Valid || !sleep.startedAt.Valid || sleep.cwd.String != s.dir {
				t.Errorf("hangup record = %+v; want unknown exit and duration, known start and cwd", *sleep)
			}
			if countExact(got, "echo before-hangup") != 1 || countExact(got, "sleep 30") != 1 {
				t.Errorf("want exactly one record each, got %+v", got)
			}
		})
	}
}

// TestShellHooksRapidFire types a burst of commands faster than any person
// and checks that every single run is recorded: none lost, none doubled.
func TestShellHooksRapidFire(t *testing.T) {
	bin := buildLoc(t)
	const n = 100
	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			s := startShell(t, sh.name, sh.args)
			s.typeLines(`eval "$('` + bin + `' init ` + sh.name + `)"`)
			s.burst(n)
			s.typeLines("exit")
			s.wait()
			got := s.recordedWhen(func(es []execution) bool { return countExact(es, "true") >= n })
			if c := countExact(got, "true"); c != n {
				t.Errorf("recorded %d runs of `true`, want %d", c, n)
			}
		})
	}
}

// TestHookOverhead measures how much the hook slows the prompt: the time per
// command in a burst of typed-ahead commands, with and without the hook, and
// what one background `loxx record` costs. Opt-in: LOXX_BENCH_HOOKS=1.
func TestHookOverhead(t *testing.T) {
	if os.Getenv("LOXX_BENCH_HOOKS") == "" {
		t.Skip("set LOXX_BENCH_HOOKS=1 to measure")
	}
	bin := buildLoc(t)
	const n, rounds = 200, 5
	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			perCommand := func(hook bool) time.Duration {
				var runs []time.Duration
				for range rounds {
					s := startShell(t, sh.name, sh.args)
					if hook {
						s.typeLines(`eval "$('` + bin + `' init ` + sh.name + `)"`)
					}
					runs = append(runs, s.burst(n)/n)
					s.typeLines("exit")
					s.wait()
				}
				return median(runs)
			}
			without, with := perCommand(false), perCommand(true)
			t.Logf("%s: per command without hook %v, with hook %v, overhead %v (median of %d bursts of %d)",
				sh.name, without.Round(time.Microsecond), with.Round(time.Microsecond),
				(with - without).Round(time.Microsecond), rounds, n)
		})
	}

	// The background cost: one `loxx record`, start to exit.
	env := append(os.Environ(), "LOXX_DB_PATH="+filepath.Join(t.TempDir(), "history.db"), "LOXX_BACKGROUND_EMBED=0")
	var runs []time.Duration
	for i := range 30 {
		cmd := exec.Command(bin, "record", "--exit", "0", "--", fmt.Sprintf("echo record-cost-%d", i))
		cmd.Env = env
		start := time.Now()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("loxx record: %v\n%s", err, out)
		}
		runs = append(runs, time.Since(start))
	}
	t.Logf("one background `loxx record`: median %v (30 runs)", median(runs).Round(time.Microsecond))
}

func buildLoc(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds loxx and starts real shells")
	}
	bin := filepath.Join(t.TempDir(), "loxx")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building loxx: %v\n%s", err, out)
	}
	return bin
}

// session is an interactive shell running in a pseudo-terminal.
type session struct {
	t      *testing.T
	dir    string
	dbPath string
	master *os.File
	cmd    *exec.Cmd
	out    syncBuffer
	copied chan struct{}
	cancel context.CancelFunc
}

// startShell starts shell in a new pseudo-terminal with a temporary HOME and
// history file, so it can never touch the real ones.
func startShell(t *testing.T, shell string, args []string) *session {
	t.Helper()
	if _, err := exec.LookPath(shell); err != nil {
		t.Skipf("%s not installed", shell)
	}
	// The shell records its real working directory; on macOS the temporary
	// directory is behind a symlink (/var → /private/var).
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := startPTY(t, dir, filepath.Join(dir, "history.db"), nil, shell, args...)
	time.Sleep(500 * time.Millisecond) // let the shell start
	return s
}

// startPTY starts name in a new 80x24 pseudo-terminal, as its controlling
// terminal, with loxx pointed at dbPath. Its stdout goes to stdout when given,
// otherwise to the terminal.
func startPTY(t *testing.T, dir, dbPath string, stdout io.Writer, name string, args ...string) *session {
	t.Helper()
	s := &session{t: t, dir: dir, dbPath: dbPath, copied: make(chan struct{})}

	master, tty, err := openPTY()
	if err != nil {
		t.Fatal(err)
	}
	s.master = master
	// A real terminal has a size; with 0x0 a TUI has nowhere to draw.
	size := [4]uint16{24, 80, 0, 0} // rows, cols, xpixel, ypixel
	if err := ioctl(master, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&size))); err != nil {
		t.Fatalf("sizing pty: %v", err)
	}

	var ctx context.Context
	ctx, s.cancel = context.WithTimeout(context.Background(), 60*time.Second)
	s.cmd = exec.CommandContext(ctx, name, args...)
	s.cmd.Dir = dir
	s.cmd.Env = append(os.Environ(),
		"HOME="+dir, // never touch the real home directory or history
		"HISTFILE="+filepath.Join(dir, "shell-history"),
		"HISTCONTROL=ignorespace",
		"LOXX_DB_PATH="+dbPath,
		"LOXX_BACKGROUND_EMBED=0",
		"TERM=xterm-256color",
	)
	s.cmd.Stdin, s.cmd.Stdout, s.cmd.Stderr = tty, tty, tty
	if stdout != nil {
		s.cmd.Stdout = stdout
	}
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	err = s.cmd.Start()
	tty.Close() // the child has its own copy
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		io.Copy(&s.out, master) // ends with EIO once the process exits
		close(s.copied)
	}()
	t.Cleanup(func() {
		s.cancel()
		master.Close()
	})
	return s
}

func ioctl(f *os.File, req, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, arg); errno != 0 {
		return errno
	}
	return nil
}

// typeLines types each line (ctrlC is sent as a keystroke), pausing between
// them the way a person would.
func (s *session) typeLines(lines ...string) {
	s.t.Helper()
	for _, l := range lines {
		if l != ctrlC {
			l += "\n"
		}
		if _, err := s.master.WriteString(l); err != nil {
			s.t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// burst types n `true` commands at once, as typeahead, and returns how long
// the shell took to run them all and show the prompt again.
func (s *session) burst(n int) time.Duration {
	s.t.Helper()
	// The marker is printed only when the command runs; its typed form
	// "done-$((6*7))" never contains "done-42".
	const marker = "done-42"
	before := strings.Count(s.out.String(), marker)
	start := time.Now()
	if _, err := s.master.WriteString(strings.Repeat("true\n", n) + "echo done-$((6*7))\n"); err != nil {
		s.t.Fatal(err)
	}
	for deadline := start.Add(30 * time.Second); strings.Count(s.out.String(), marker) == before; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			s.t.Fatalf("burst did not finish\nsession:\n%s", s.out.String())
		}
	}
	return time.Since(start)
}

func (s *session) wait() {
	s.t.Helper()
	if err := s.cmd.Wait(); err != nil {
		s.t.Fatalf("shell: %v\nsession:\n%s", err, s.out.String())
	}
	<-s.copied
}

// recordedWhen waits (loxx record runs in the background) until done reports
// true for the recorded executions, or 20 seconds pass, and returns them.
func (s *session) recordedWhen(done func([]execution) bool) []execution {
	s.t.Helper()
	var got []execution
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(s.dbPath); err == nil {
			if got = executions(s.t, s.dbPath); done(got) {
				break
			}
		}
	}
	return got
}

// syncBuffer is a bytes.Buffer safe for one writer and concurrent readers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func hasCommand(es []execution, text string) bool { return countExact(es, text) > 0 }

func countExact(es []execution, text string) int {
	n := 0
	for _, e := range es {
		if e.text == text {
			n++
		}
	}
	return n
}

func countContaining(es []execution, s string) int {
	n := 0
	for _, e := range es {
		if strings.Contains(e.text, s) {
			n++
		}
	}
	return n
}

func median(ds []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), ds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}
