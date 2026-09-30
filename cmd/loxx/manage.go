package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AniketR10/loxx/internal/embed"
	"github.com/AniketR10/loxx/internal/format"
	"github.com/AniketR10/loxx/internal/setup"
	"github.com/AniketR10/loxx/internal/store"
)

// supportedShells are the shells loxx integrates with, in the order they're handled.
var supportedShells = []string{"bash", "zsh"}

func runSetup(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("setup", "loxx setup [--no-import]",
		"Adds the loxx hook to ~/.bashrc and ~/.zshrc (each only if that file exists or it is\nyour login shell), then imports your existing history once. Safe to run again.", stderr)
	noImport := fs.Bool("no-import", false, "don't import existing shell history")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "loxx setup:", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "loxx setup:", err)
		return 1
	}
	login := filepath.Base(os.Getenv("SHELL"))

	configured := 0
	for _, sh := range supportedShells {
		rc, _ := setup.RCFile(sh, home)
		if _, err := os.Stat(rc); err != nil && login != sh {
			continue // this shell isn't in use
		}
		changed, err := setup.Install(rc, sh, self)
		switch {
		case errors.Is(err, setup.ErrHandWritten):
			fmt.Fprintf(stderr, "loxx: %s already loads loxx with a line you added (%v).\n      Remove that line and run `loxx setup` again, or keep it and skip this file.\n",
				format.TildePath(rc, home), strings.TrimPrefix(err.Error(), setup.ErrHandWritten.Error()+": "))
			configured++
			continue
		case err != nil:
			fmt.Fprintf(stderr, "loxx setup: %s: %v\n", rc, err)
			return 1
		case changed:
			fmt.Fprintf(stderr, "loxx: added the hook to %s\n", format.TildePath(rc, home))
		default:
			fmt.Fprintf(stderr, "loxx: %s is already set up\n", format.TildePath(rc, home))
		}
		configured++
	}
	if configured == 0 {
		fmt.Fprintln(stderr, "loxx setup: found neither ~/.bashrc nor ~/.zshrc, and your login shell is neither bash nor zsh")
		return 1
	}
	if !*noImport {
		if code := runImport(nil, stdout, stderr); code != 0 {
			return code
		}
	}
	fmt.Fprintln(stderr, "loxx: done. Open a new terminal and type `loxx` to search your history.")
	return 0
}

func runUninstall(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("uninstall", "loxx uninstall [--delete-data | --keep-data]",
		"Removes the loxx hook from ~/.bashrc and ~/.zshrc (restoring them exactly), asks\nwhether to delete your history, and removes the binary if install.sh installed it.", stderr)
	deleteData := fs.Bool("delete-data", false, "delete the history without asking")
	keepData := fs.Bool("keep-data", false, "keep the history without asking")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if *deleteData && *keepData {
		fmt.Fprintln(stderr, "loxx uninstall: choose either --delete-data or --keep-data")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "loxx uninstall:", err)
		return 1
	}

	for _, sh := range supportedShells {
		rc, _ := setup.RCFile(sh, home)
		if _, err := os.Stat(rc); err != nil {
			continue
		}
		changed, err := setup.Uninstall(rc)
		if err != nil {
			fmt.Fprintf(stderr, "loxx uninstall: %s: %v\n", rc, err)
			return 1
		}
		if changed {
			fmt.Fprintf(stderr, "loxx: removed the hook from %s\n", format.TildePath(rc, home))
		}
		if b, err := os.ReadFile(rc); err == nil {
			if line, ok := setup.HandWritten(string(b)); ok {
				fmt.Fprintf(stderr, "loxx: %s still loads loxx with a line you added; remove it yourself: %s\n", format.TildePath(rc, home), line)
			}
		}
	}

	dbPath, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "loxx uninstall:", err)
		return 1
	}
	if _, err := os.Stat(dbPath); err == nil {
		del := *deleteData
		if !del && !*keepData {
			del = askYesNo(fmt.Sprintf("Delete your loxx history (%s)? [y/N] ", format.TildePath(dbPath, home)))
		}
		if del {
			for _, suffix := range []string{"", "-wal", "-shm", ".lock", ".embed.lock"} {
				if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
					fmt.Fprintln(stderr, "loxx uninstall:", err)
					return 1
				}
			}
			os.Remove(filepath.Dir(dbPath)) // only succeeds if now empty
			fmt.Fprintln(stderr, "loxx: deleted your history")
		} else {
			fmt.Fprintf(stderr, "loxx: kept your history in %s\n", format.TildePath(dbPath, home))
		}
	}

	state := installedBinaryFile(home)
	if b, err := os.ReadFile(state); err == nil {
		bin := strings.TrimSpace(string(b))
		if err := os.Remove(bin); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stderr, "loxx uninstall:", err)
			return 1
		}
		os.Remove(state)
		os.Remove(filepath.Dir(state))
		fmt.Fprintf(stderr, "loxx: removed %s\n", format.TildePath(bin, home))
	} else if self, err := os.Executable(); err == nil {
		fmt.Fprintf(stderr, "loxx: %s was not installed by install.sh, so it was left in place\n", format.TildePath(self, home))
	}
	fmt.Fprintln(stderr, "loxx: uninstalled. Terminals that are already open keep the hook until you close them.")
	return 0
}

// installedBinaryFile is where install.sh records the binary it installed,
// so uninstall removes only what the installer put there.
func installedBinaryFile(home string) string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" || !filepath.IsAbs(dir) {
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "loxx", "installed-binary")
}

func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("status", "loxx status", "Shows what loxx has recorded and whether the hook is active.", stderr)
	if code, ok := parse(fs, args); !ok {
		return code
	}
	home, _ := os.UserHomeDir()
	self, _ := os.Executable()
	fmt.Fprintf(stdout, "loxx %s (%s)\n", version, format.TildePath(self, home))

	path, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "loxx status:", err)
		return 1
	}
	ctx := context.Background()
	db, err := store.Open(ctx, path)
	if err != nil {
		fmt.Fprintln(stderr, "loxx status:", err)
		return 1
	}
	defer db.Close()
	st, err := db.Stats(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "loxx status:", err)
		return 1
	}
	pending, err := db.CountPendingEmbeddings(ctx, embed.ModelID)
	if err != nil {
		fmt.Fprintln(stderr, "loxx status:", err)
		return 1
	}
	var size int64
	for _, suffix := range []string{"", "-wal"} {
		if fi, err := os.Stat(path + suffix); err == nil {
			size += fi.Size()
		}
	}
	fmt.Fprintf(stdout, "history:    %s (%.1f MB)\n", format.TildePath(path, home), float64(size)/1e6)
	fmt.Fprintf(stdout, "commands:   %d unique · %d runs recorded live, %d imported", st.Commands, st.LiveRuns, st.ImportedRuns)
	if st.OtherRuns > 0 {
		fmt.Fprintf(stdout, ", %d added by hand", st.OtherRuns)
	}
	fmt.Fprintln(stdout)
	if last, err := db.LastRunAt(ctx); err == nil && last != nil {
		fmt.Fprintf(stdout, "last live:  %s\n", format.RelativeTime(*last, time.Now()))
	}
	fmt.Fprintf(stdout, "redacted:   %d secret(s)\n", st.Redactions)
	switch {
	case pending == 0:
		fmt.Fprintln(stdout, "embeddings: all commands searchable by meaning")
	case embedRunning(path):
		fmt.Fprintf(stdout, "embeddings: %d pending, embedding now\n", pending)
	default:
		fmt.Fprintf(stdout, "embeddings: %d pending (run `loxx embed --pending`, or they're embedded after your next command)\n", pending)
	}

	for _, sh := range supportedShells {
		rc, _ := setup.RCFile(sh, home)
		b, err := os.ReadFile(rc)
		state := "no hook"
		switch {
		case err != nil:
			state = "no rc file"
		case setup.HasBlock(string(b)):
			state = "hook installed"
		default:
			if _, ok := setup.HandWritten(string(b)); ok {
				state = "hook installed (a line you added)"
			}
		}
		fmt.Fprintf(stdout, "%-11s %s: %s\n", sh+":", format.TildePath(rc, home), state)
	}
	if sh := os.Getenv("LOXX_HOOK"); sh != "" {
		fmt.Fprintf(stdout, "this shell: hook active (%s)\n", sh)
	} else {
		fmt.Fprintln(stdout, "this shell: hook not active (open a new terminal after `loxx setup`)")
	}
	return 0
}

// embedRunning reports whether a `loxx embed` holds the embed lock.
func embedRunning(dbPath string) bool {
	unlock, err := tryLock(dbPath + ".embed.lock")
	if err != nil {
		return errors.Is(err, errLocked)
	}
	unlock()
	return false
}

func runForget(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("forget", "loxx forget (--match TEXT | --last) [--yes]",
		"Deletes commands, with all their runs, for good: the text is also wiped from the\ndatabase files on disk.", stderr)
	match := fs.String("match", "", "forget every command containing TEXT (case-sensitive)")
	last := fs.Bool("last", false, "forget the most recently recorded command")
	yes := fs.Bool("yes", false, "don't ask for confirmation")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if (*match == "") == !*last {
		fs.Usage()
		return 2
	}
	ctx := context.Background()
	db, err := openDB(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "loxx forget:", err)
		return 1
	}
	defer db.Close()

	var found []store.Result
	if *last {
		ids, err := db.RecentIDs(ctx, store.Filter{}, 1)
		if err == nil {
			found, err = db.Results(ctx, ids)
		}
		if err != nil {
			fmt.Fprintln(stderr, "loxx forget:", err)
			return 1
		}
	} else if found, err = db.FindCommands(ctx, *match); err != nil {
		fmt.Fprintln(stderr, "loxx forget:", err)
		return 1
	}
	if len(found) == 0 {
		fmt.Fprintln(stderr, "loxx: nothing matches")
		return 1
	}
	const shown = 10
	for _, r := range found[:min(len(found), shown)] {
		fmt.Fprintln(stderr, "  "+strings.ReplaceAll(r.Text, "\n", " ↵ "))
	}
	if len(found) > shown {
		fmt.Fprintf(stderr, "  …and %d more\n", len(found)-shown)
	}
	if !*yes {
		if !hasTerminal() {
			fmt.Fprintln(stderr, "loxx forget: no terminal to confirm on; run again with --yes")
			return 2
		}
		if !askYesNo(fmt.Sprintf("Forget %d command(s) and all their runs? [y/N] ", len(found))) {
			fmt.Fprintln(stderr, "loxx: nothing forgotten")
			return 1
		}
	}
	ids := make([]int64, len(found))
	for i, r := range found {
		ids[i] = r.CommandID
	}
	runs, err := db.Forget(ctx, ids)
	if err != nil {
		fmt.Fprintln(stderr, "loxx forget:", err)
		return 1
	}
	fmt.Fprintf(stderr, "loxx: forgot %d command(s) (%d runs)\n", len(found), runs)
	return 0
}

// askYesNo asks on the terminal and reports whether the answer was yes. With
// no terminal it answers no.
func askYesNo(prompt string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	line, _ := bufio.NewReader(tty).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

func hasTerminal() bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	tty.Close()
	return true
}

func newFlags(name, usage, about string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: "+usage)
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, about)
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	return fs
}

// parse parses args; ok is false when the command should exit with code.
func parse(fs *flag.FlagSet, args []string) (code int, ok bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, false
		}
		return 2, false
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2, false
	}
	return 0, true
}
