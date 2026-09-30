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

	"github.com/AniketR10/loc/internal/format"
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
	results, err := s.Search(ctx, query, *limit, search.DefaultParams, store.Filter{})
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
		fmt.Fprintf(stdout, "    %s\n", format.Details(r, home, now))
	}
	return 0
}
