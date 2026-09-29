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
	"time"

	"github.com/AniketR10/loc/internal/search"
	"github.com/AniketR10/loc/internal/store"
)

func runSearch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: loc search [--limit N] <query>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Finds commands by meaning and by keyword. All arguments together form the query.")
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

	s := search.New(ctx, db)
	results, err := s.Search(ctx, query, *limit, search.DefaultParams)
	if err != nil {
		fmt.Fprintln(stderr, "loc search:", err)
		return 1
	}
	if err := s.SemanticErr(); err != nil {
		fmt.Fprintln(stderr, "loc: semantic search unavailable, showing keyword matches only:", err)
	}
	if len(results) == 0 {
		fmt.Fprintln(stderr, "loc: no matches")
		return 1
	}
	home, _ := os.UserHomeDir()
	now := time.Now()
	for _, r := range results {
		fmt.Fprintln(stdout, r.Text)
		fmt.Fprintf(stdout, "    %s\n", details(r, home, now))
	}
	return 0
}

// details formats a result's metadata line, e.g.
// "~/work/api · 3 weeks ago · exit 0 · 4 runs".
func details(r store.Result, home string, now time.Time) string {
	var parts []string
	if r.Cwd != "" {
		parts = append(parts, tildePath(r.Cwd, home))
	}
	if r.LastRun != nil {
		parts = append(parts, relativeTime(*r.LastRun, now))
	} else {
		parts = append(parts, "imported") // no run time is known (user decision, ROADMAP §3)
	}
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

// relativeTime describes t relative to now in the largest whole unit, e.g.
// "just now", "5 minutes ago", "3 weeks ago".
func relativeTime(t, now time.Time) string {
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	for _, u := range []struct {
		size time.Duration
		name string
	}{
		{365 * 24 * time.Hour, "year"},
		{30 * 24 * time.Hour, "month"},
		{7 * 24 * time.Hour, "week"},
		{24 * time.Hour, "day"},
		{time.Hour, "hour"},
		{time.Minute, "minute"},
	} {
		if n := int(d / u.size); n >= 1 {
			if n == 1 {
				return "1 " + u.name + " ago"
			}
			return fmt.Sprintf("%d %ss ago", n, u.name)
		}
	}
	return "just now"
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
