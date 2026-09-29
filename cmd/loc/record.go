package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AniketR10/loc/internal/scrub"
	"github.com/AniketR10/loc/internal/store"
)

// runRecord stores one finished command. The shell hooks (`loc init`) call it
// in the background after every command, so it must stay fast: open the
// database, scrub, insert, maybe start a background embed, exit. It never
// loads the embedding model.
func runRecord(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: loc record [--exit N] [--start T] [--end T] [--cwd DIR] [--session ID] -- <command>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Records a finished command. Called by the shell hooks; times are Unix epoch seconds.")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	var exitCode *int
	fs.Func("exit", "exit code of the command", func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil {
			return errors.New("must be an integer")
		}
		exitCode = &n
		return nil
	})
	start := fs.String("start", "", "when the command started (epoch seconds, fractions allowed)")
	end := fs.String("end", "", "when the command finished (epoch seconds, fractions allowed)")
	cwd := fs.String("cwd", "", "directory the command ran in")
	session := fs.String("session", "", "id of the shell session")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	raw := fs.Arg(0)
	if scrub.Ignored(raw) {
		return 0
	}
	cmd := scrub.Scrub(raw)
	if cmd.Text() == "" {
		return 0
	}

	e := store.Execution{
		Command:   cmd,
		Cwd:       *cwd,
		GitRoot:   gitRoot(*cwd),
		ExitCode:  exitCode,
		SessionID: *session,
		Source:    store.SourceLive,
	}
	e.Hostname, _ = os.Hostname()
	startedAt, err := parseEpoch(*start)
	if err != nil {
		fmt.Fprintln(stderr, "loc record: --start:", err)
		return 2
	}
	finishedAt, err := parseEpoch(*end)
	if err != nil {
		fmt.Fprintln(stderr, "loc record: --end:", err)
		return 2
	}
	e.StartedAt = startedAt
	if !startedAt.IsZero() && !finishedAt.IsZero() && !finishedAt.Before(startedAt) {
		d := finishedAt.Sub(startedAt)
		e.Duration = &d
	}

	path, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "loc record:", err)
		return 1
	}
	ctx := context.Background()
	db, err := store.Open(ctx, path)
	if err != nil {
		fmt.Fprintln(stderr, "loc record:", err)
		return 1
	}
	_, err = db.Add(ctx, e)
	db.Close()
	if err != nil {
		fmt.Fprintln(stderr, "loc record:", err)
		return 1
	}
	if err := startBackgroundEmbed(path); err != nil {
		fmt.Fprintln(stderr, "loc record: starting background embed:", err)
		return 1
	}
	return 0
}

// parseEpoch parses Unix epoch seconds such as "1759000000" or
// "1759000000.123456". bash's $EPOCHREALTIME uses the locale's decimal
// separator, so "1759000000,123456" is accepted too. Empty means unknown.
func parseEpoch(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	f, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return time.Time{}, fmt.Errorf("not epoch seconds: %q", s)
	}
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)).Round(time.Microsecond), nil
}

// gitRoot returns the nearest directory at or above dir that contains .git
// (a directory, or a file for worktrees and submodules), or "" if none.
func gitRoot(dir string) string {
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if d == filepath.Dir(d) {
			return ""
		}
	}
}
