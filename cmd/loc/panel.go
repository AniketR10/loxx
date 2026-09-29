package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/AniketR10/loc/internal/search"
	"github.com/AniketR10/loc/internal/tui"
)

// runPanel opens the search panel on the terminal (what typing `loc` does)
// and prints the chosen command to stdout. It exits 1 without output when
// the user cancels, so the shell hook can tell the two apart.
func runPanel(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("panel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: loc [panel [--cwd DIR] [--session ID]]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Opens the search panel. Enter prints the chosen command; Esc cancels.")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	cwd := fs.String("cwd", "", "directory for the \"this dir\" filter (default: current directory)")
	session := fs.String("session", "", "shell session id for the \"this session\" filter")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	if *cwd == "" {
		*cwd, _ = os.Getwd()
	}

	// The panel talks to the terminal directly, so stdout carries only the
	// chosen command, even when a shell function captures it.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(stderr, "loc: the search panel needs a terminal:", err)
		return 1
	}
	defer tty.Close()

	ctx := context.Background()
	db, err := openDB(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "loc:", err)
		return 1
	}
	defer db.Close()

	home, _ := os.UserHomeDir()
	chosen, err := tui.Run(ctx, search.New(ctx, db), tty, tui.Options{
		Dir: *cwd, GitRoot: gitRoot(*cwd), Session: *session, Home: home,
	})
	if err != nil {
		fmt.Fprintln(stderr, "loc:", err)
		return 1
	}
	if chosen == "" {
		return 1
	}
	fmt.Fprintln(stdout, chosen)
	return 0
}
