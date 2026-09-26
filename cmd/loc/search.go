package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AniketR10/loc/internal/store"
)

func runSearch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: loc search [--limit N] <query>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Finds commands containing every word of the query (words match as prefixes).")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	limit := fs.Int("limit", 5, "maximum number of results")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" || *limit < 1 {
		fs.Usage()
		return 2
	}

	ctx := context.Background()
	db, err := openDB(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "loc search:", err)
		return 1
	}
	defer db.Close()

	results, err := db.SearchKeyword(ctx, query, *limit)
	if err != nil {
		fmt.Fprintln(stderr, "loc search:", err)
		return 1
	}
	if len(results) == 0 {
		fmt.Fprintln(stderr, "loc: no matches")
		return 1
	}
	home, _ := os.UserHomeDir()
	for _, r := range results {
		fmt.Fprintln(stdout, r.Text)
		fmt.Fprintf(stdout, "    %s\n", details(r, home))
	}
	return 0
}

// details formats a result's metadata line, e.g.
// "~/work/api · 2026-09-27 14:03 · exit 0 · 4 runs".
func details(r store.Result, home string) string {
	var parts []string
	if r.Cwd != "" {
		parts = append(parts, tildePath(r.Cwd, home))
	}
	parts = append(parts, r.LastSeen.Local().Format("2006-01-02 15:04"))
	if r.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit %d", *r.ExitCode))
	}
	if r.RunCount == 1 {
		parts = append(parts, "1 run")
	} else {
		parts = append(parts, fmt.Sprintf("%d runs", r.RunCount))
	}
	return strings.Join(parts, " · ")
}

func tildePath(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rel
	}
	return path
}
