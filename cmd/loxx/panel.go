package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"github.com/AniketR10/loxx/internal/search"
	"github.com/AniketR10/loxx/internal/tui"
)

// runPanel opens the search panel and prints the choice; exit 1 on cancel.
func runPanel(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("panel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: loxx [panel [--cwd DIR] [--session ID] [--query TEXT] [--cursor]]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Opens the search panel. Enter prints the chosen command; Esc cancels.")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	cwd := fs.String("cwd", "", "directory for the \"this dir\" filter (default: current directory)")
	session := fs.String("session", "", "shell session id for the \"this session\" filter")
	query := fs.String("query", "", "start with this already typed in the search")
	cursor := fs.Bool("cursor", false, "print where the cursor goes (in characters) and a tab before the command")
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
		say(stderr, "the search panel needs a terminal: %v\n", err)
		return 1
	}
	defer tty.Close()

	ctx := context.Background()
	db, err := openDB(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "loxx:", err)
		return 1
	}
	defer db.Close()

	home, _ := os.UserHomeDir()
	chosen, err := tui.Run(ctx, search.New(ctx, db), tty, tui.Options{
		Dir: *cwd, GitRoot: gitRoot(*cwd), Session: *session, Home: home, Query: *query,
	})
	if err != nil {
		fmt.Fprintln(stderr, "loxx:", err)
		return 1
	}
	if chosen.Text == "" {
		return 1
	}
	if *cursor {
		at := len(chosen.Text)
		if chosen.Library {
			at = chosen.Cursor
		}
		fmt.Fprintf(stdout, "%d\t", utf8.RuneCountInString(chosen.Text[:at]))
	}
	fmt.Fprintln(stdout, chosen.Text)
	return 0
}
