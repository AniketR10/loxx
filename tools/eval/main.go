// Command eval is a dev-only harness that scores loc's search on an eval set:
// for each natural-language query, where does the expected command rank?
//
// Usage:
//
//	go run ./tools/eval --db HISTORY_DB --queries QUERIES_JSONL
//
// The database must be fully embedded first (`loc embed --pending`). The eval
// set is JSON lines of {"query": "...", "expected": ["command text", ...]};
// see docs/decisions/0001-embedding-model.md.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/AniketR10/loc/internal/embed"
	"github.com/AniketR10/loc/internal/store"
)

const topK = 10

type query struct {
	Query    string   `json:"query"`
	Expected []string `json:"expected"`
}

// ranker returns command ids, best first.
type ranker func(ctx context.Context, query string) ([]int64, error)

func main() {
	dbPath := flag.String("db", "", "loc history database (fully embedded)")
	queriesPath := flag.String("queries", "", "eval set, JSON lines")
	flag.Parse()
	if *dbPath == "" || *queriesPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(context.Background(), *dbPath, *queriesPath); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dbPath, queriesPath string) error {
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
	vectors, err := db.Embeddings(ctx, embed.ModelID)
	if err != nil {
		return err
	}
	model, err := embed.Default()
	if err != nil {
		return err
	}
	fmt.Printf("pool: %d commands, %d queries, model %s\n\n", len(ids), len(queries), embed.ModelID)

	semantic := func(_ context.Context, q string) ([]int64, error) {
		qv := model.Embed(q)
		type scored struct {
			id    int64
			score float32
		}
		all := make([]scored, len(vectors))
		for i, e := range vectors {
			var s float32
			for j, v := range e.Vector { // vectors are unit length: dot = cosine
				s += v * qv[j]
			}
			all[i] = scored{e.CommandID, s}
		}
		slices.SortFunc(all, func(a, b scored) int {
			switch {
			case a.score > b.score:
				return -1
			case a.score < b.score:
				return 1
			}
			return 0
		})
		out := make([]int64, 0, topK)
		for _, s := range all[:min(topK, len(all))] {
			out = append(out, s.id)
		}
		return out, nil
	}
	keyword := func(ctx context.Context, q string) ([]int64, error) {
		results, err := db.SearchKeyword(ctx, q, topK)
		var out []int64
		for _, r := range results {
			out = append(out, r.CommandID)
		}
		return out, err
	}

	fmt.Printf("%-50s %5s %5s %5s  %s\n", "ranker", "top1", "top5", "MRR", "avg query time")
	for _, r := range []struct {
		name string
		rank ranker
	}{
		{"keyword AND (loc search today)", keyword},
		{"semantic (Go, " + embed.ModelID + ")", semantic},
	} {
		if err := score(ctx, r.name, r.rank, queries); err != nil {
			return err
		}
	}
	return nil
}

func score(ctx context.Context, name string, rank ranker, queries []evalQuery) error {
	var top1, top5, mrr float64
	var elapsed time.Duration
	for _, q := range queries {
		start := time.Now()
		got, err := rank(ctx, q.text)
		elapsed += time.Since(start)
		if err != nil {
			return fmt.Errorf("%s: %q: %w", name, q.text, err)
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
	fmt.Printf("%-50s %5.2f %5.2f %5.2f  %v\n", name, top1/n, top5/n, mrr/n,
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
		var q query
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
