// Command eval is a dev-only harness that scores loc's search on an eval set:
// for each natural-language query, where does the expected command rank? It
// ranks through internal/search, so what it measures is what loc ships.
//
// Usage:
//
//	go run ./tools/eval --db HISTORY_DB --queries QUERIES_JSONL [--grid]
//
// The database must be fully embedded first (`loc embed --pending`). The eval
// set is JSON lines of {"query": "...", "expected": ["command text", ...]};
// see docs/decisions/0001-embedding-model.md. --grid also scores a small grid
// of fusion settings.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/AniketR10/loc/internal/embed"
	"github.com/AniketR10/loc/internal/search"
	"github.com/AniketR10/loc/internal/store"
)

const topK = 10

func main() {
	dbPath := flag.String("db", "", "loc history database (fully embedded)")
	queriesPath := flag.String("queries", "", "eval set, JSON lines")
	grid := flag.Bool("grid", false, "also score a grid of fusion settings")
	flag.Parse()
	if *dbPath == "" || *queriesPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(context.Background(), *dbPath, *queriesPath, *grid); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

type row struct {
	name string
	p    search.Params
}

func run(ctx context.Context, dbPath, queriesPath string, grid bool) error {
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if n, err := db.CountPendingEmbeddings(ctx, embed.ModelID); err != nil {
		return err
	} else if n > 0 {
		return fmt.Errorf("%d commands are not embedded yet; run `LOC_DB_PATH=%s loc embed --pending`", n, dbPath)
	}
	ids, err := commandIDs(dbPath)
	if err != nil {
		return err
	}
	queries, err := loadQueries(queriesPath, ids)
	if err != nil {
		return err
	}

	s := search.New(ctx, db)
	if err := s.SemanticErr(); err != nil {
		return err
	}
	fmt.Printf("pool: %d commands, %d queries, model %s\n\n", len(ids), len(queries), embed.ModelID)

	keywordOnly := func(mode store.MatchMode) search.Params {
		return search.Params{KeywordMode: mode, KeywordWeight: 1, RRFK: 60}
	}
	rows := []row{
		{"keyword AND", keywordOnly(store.MatchAll)},
		{"keyword OR", keywordOnly(store.MatchAny)},
		{"semantic only", search.Params{SemanticWeight: 1, RRFK: 60}},
		{"hybrid (DefaultParams)", search.DefaultParams},
	}
	if grid {
		for _, mode := range []store.MatchMode{store.MatchAll, store.MatchAny} {
			for _, k := range []float64{5, 20, 60} {
				for _, kw := range []float64{0.25, 0.5, 1} {
					rows = append(rows, row{
						fmt.Sprintf("hybrid %s k=%g kw=%g sem=1", modeName(mode), k, kw),
						search.Params{KeywordMode: mode, KeywordWeight: kw, SemanticWeight: 1, RRFK: k},
					})
				}
			}
		}
	}

	fmt.Printf("%-38s %5s %5s %5s  %s\n", "ranker", "top1", "top5", "MRR", "avg rank time")
	for _, r := range rows {
		if err := score(ctx, s, r, queries); err != nil {
			return err
		}
	}
	return nil
}

func modeName(m store.MatchMode) string {
	if m == store.MatchAll {
		return "AND"
	}
	return "OR"
}

func score(ctx context.Context, s *search.Searcher, r row, queries []evalQuery) error {
	return scoreWith(ctx, r, queries, func(q string) ([]int64, error) { return s.Rank(ctx, q, topK, r.p, store.Filter{}) })
}

func scoreWith(ctx context.Context, r row, queries []evalQuery, rank func(string) ([]int64, error)) error {
	var top1, top5, mrr float64
	var elapsed time.Duration
	for _, q := range queries {
		start := time.Now()
		got, err := rank(q.text)
		elapsed += time.Since(start)
		if err != nil {
			return fmt.Errorf("%s: %q: %w", r.name, q.text, err)
		}
		for pos, id := range got {
			if q.expected[id] {
				mrr += 1 / float64(pos+1)
				if pos == 0 {
					top1++
				}
				if pos < 5 {
					top5++
				}
				break
			}
		}
	}
	n := float64(len(queries))
	fmt.Printf("%-38s %5.2f %5.2f %5.2f  %v\n", r.name, top1/n, top5/n, mrr/n,
		(elapsed / time.Duration(len(queries))).Round(time.Microsecond))
	return nil
}

type evalQuery struct {
	text     string
	expected map[int64]bool
}

// commandIDs maps command text to id. It reads the database directly because
// the store package has no need to list every command.
func commandIDs(dbPath string) (map[string]int64, error) {
	raw, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer raw.Close()
	rows, err := raw.Query(`SELECT id, text FROM commands`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]int64{}
	for rows.Next() {
		var (
			id   int64
			text string
		)
		if err := rows.Scan(&id, &text); err != nil {
			return nil, err
		}
		ids[text] = id
	}
	return ids, rows.Err()
}

func loadQueries(path string, ids map[string]int64) ([]evalQuery, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var queries []evalQuery
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var q struct {
			Query    string   `json:"query"`
			Expected []string `json:"expected"`
		}
		if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
			return nil, err
		}
		eq := evalQuery{text: q.Query, expected: map[int64]bool{}}
		for _, e := range q.Expected {
			id, ok := ids[e]
			if !ok {
				return nil, fmt.Errorf("expected command for %q is not in the database: %q", q.Query, e)
			}
			eq.expected[id] = true
		}
		queries = append(queries, eq)
	}
	return queries, sc.Err()
}
