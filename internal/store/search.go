package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"
)

// Result is a command with details of its most recent execution.
type Result struct {
	CommandID int64
	Text      string
	RunCount  int
	LastRun   *time.Time // start of the most recent run with a known time; nil if none (e.g. imported)
	Cwd       string     // "" when unknown
	ExitCode  *int       // nil when unknown
}

// MatchMode says how the words of a keyword query combine.
type MatchMode int

const (
	// MatchAll requires every word. It is what search uses: precise for
	// typed fragments, and it matches nothing for most sentences, which
	// leaves those to semantic ranking.
	MatchAll MatchMode = iota
	// MatchAny requires at least one word and lets BM25 rank by how many and
	// how rare. It scored lower than MatchAll in hybrid search on both eval
	// sets; kept for the eval harness.
	MatchAny
)

// KeywordIDs returns up to limit ids of commands matching query and kept by
// f, best BM25 match first. Each word also matches as a prefix.
func (db *DB) KeywordIDs(ctx context.Context, query string, mode MatchMode, f Filter, limit int) ([]int64, error) {
	match := ftsQuery(query, mode)
	if match == "" {
		return nil, nil
	}
	cond, args := f.where()
	rows, err := db.sql.QueryContext(ctx, `
		SELECT commands_fts.rowid FROM commands_fts JOIN commands c ON c.id = commands_fts.rowid
		WHERE commands_fts MATCH ? AND `+cond+`
		ORDER BY bm25(commands_fts) LIMIT ?`, append(append([]any{match}, args...), limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Results returns the commands with the given ids, in the same order. Ids that
// no longer exist are skipped.
func (db *DB) Results(ctx context.Context, ids []int64) ([]Result, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.sql.QueryContext(ctx, `
		SELECT c.id, c.text, c.run_count, e.started_at, e.cwd, e.exit_code
		FROM commands c
		LEFT JOIN executions e ON e.id = (
			SELECT id FROM executions WHERE command_id = c.id
			ORDER BY started_at DESC, id DESC LIMIT 1)
		WHERE c.id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[int64]Result, len(ids))
	for rows.Next() {
		var (
			r         Result
			startedAt sql.NullInt64
			cwd       sql.NullString
			exitCode  sql.NullInt64
		)
		if err := rows.Scan(&r.CommandID, &r.Text, &r.RunCount, &startedAt, &cwd, &exitCode); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			t := time.UnixMilli(startedAt.Int64)
			r.LastRun = &t
		}
		r.Cwd = cwd.String
		if exitCode.Valid {
			code := int(exitCode.Int64)
			r.ExitCode = &code
		}
		byID[r.CommandID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			results = append(results, r)
		}
	}
	return results, nil
}

// ftsQuery turns free text into an FTS5 query where every word is a prefix
// match, joined by AND or OR. Splitting on anything but letters and digits
// mirrors the unicode61 tokenizer and keeps FTS5 syntax out of the query.
func ftsQuery(q string, mode MatchMode) string {
	words := strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for i, w := range words {
		words[i] = `"` + w + `"*`
	}
	sep := " "
	if mode == MatchAny {
		sep = " OR "
	}
	return strings.Join(words, sep)
}
