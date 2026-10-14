package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AniketR10/loxx/internal/format"
	"github.com/AniketR10/loxx/internal/search"
	"github.com/AniketR10/loxx/internal/store"
)

// Records end with NUL; fields (command, details, ok/fail/"") are split by \x1f.
const (
	suggestRecordSep = "\x00"
	suggestFieldSep  = "\x1f"
)

// runSuggest prints matches for a partly typed line, for zsh's suggestions.
// --keyword-only never loads the model.
func runSuggest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("suggest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: loxx suggest [--keyword-only] [--limit N] -- <typed line>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Prints matches for the shell's suggestion list (used by the zsh hook).")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	limit := fs.Int("limit", 5, "maximum number of results")
	keywordOnly := fs.Bool("keyword-only", false, "match words only; never load the language model")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *limit < 1 {
		fs.Usage()
		return 2
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return 0
	}

	ctx := context.Background()
	db, err := openDB(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "loxx suggest:", err)
		return 1
	}
	defer db.Close()

	// One extra: the typed line itself is dropped below.
	want := *limit + 1
	var results []store.Result
	if *keywordOnly {
		ids, err := db.KeywordIDs(ctx, query, search.DefaultParams.KeywordMode, store.Filter{}, want)
		if err == nil {
			results, err = db.Results(ctx, ids)
		}
		if err != nil {
			fmt.Fprintln(stderr, "loxx suggest:", err)
			return 1
		}
	} else if results, err = search.New(ctx, db).Search(ctx, query, want, search.DefaultParams, store.Filter{}); err != nil {
		fmt.Fprintln(stderr, "loxx suggest:", err)
		return 1
	}

	home, _ := os.UserHomeDir()
	now := time.Now()
	var b strings.Builder
	shown := 0
	for _, r := range results {
		if r.Text == query || shown == *limit {
			continue
		}
		shown++
		b.WriteString(r.Text)
		b.WriteString(suggestFieldSep)
		b.WriteString(suggestDetails(r, home, now))
		b.WriteString(suggestFieldSep)
		switch {
		case r.ExitCode == nil:
		case *r.ExitCode == 0:
			b.WriteString("ok")
		default:
			b.WriteString("fail")
		}
		b.WriteString(suggestRecordSep)
	}
	io.WriteString(stdout, b.String())
	return 0
}

// suggestDetails is the panel-style "folder · when · N×".
func suggestDetails(r store.Result, home string, now time.Time) string {
	var parts []string
	if r.Cwd != "" {
		parts = append(parts, format.TildePath(r.Cwd, home))
	}
	if r.LastRun != nil {
		parts = append(parts, format.RelativeTime(*r.LastRun, now))
	} else {
		parts = append(parts, "imported")
	}
	if r.RunCount > 1 {
		parts = append(parts, fmt.Sprintf("%d×", r.RunCount))
	}
	return strings.Join(parts, " · ")
}
