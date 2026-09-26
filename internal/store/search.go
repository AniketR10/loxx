package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"
)

// Result is a command matched by a search, with details of its most recent
// execution.
type Result struct {
	CommandID int64
	Text      string
	RunCount  int
	LastSeen  time.Time
	Cwd       string // "" when unknown
	ExitCode  *int   // nil when unknown
}

// SearchKeyword returns up to limit commands containing every word of query
// (each word also matches as a prefix), best BM25 match first.
func (db *DB) SearchKeyword(ctx context.Context, query string, limit int) ([]Result, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	rows, err := db.sql.QueryContext(ctx, `
		SELECT c.id, c.text, c.run_count, c.last_seen, e.cwd, e.exit_code
		FROM commands_fts
		JOIN commands c ON c.id = commands_fts.rowid
		LEFT JOIN executions e ON e.id = (
			SELECT id FROM executions WHERE command_id = c.id
			ORDER BY started_at DESC, id DESC LIMIT 1)
		WHERE commands_fts MATCH ?
		ORDER BY bm25(commands_fts)
		LIMIT ?`, match, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var (
			r        Result
			lastSeen int64
			cwd      sql.NullString
			exitCode sql.NullInt64
		)
		if err := rows.Scan(&r.CommandID, &r.Text, &r.RunCount, &lastSeen, &cwd, &exitCode); err != nil {
			return nil, err
		}
		r.LastSeen = time.UnixMilli(lastSeen)
		r.Cwd = cwd.String
		if exitCode.Valid {
			code := int(exitCode.Int64)
			r.ExitCode = &code
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ftsQuery turns free text into an FTS5 query that ANDs every word as a
// prefix match. Splitting on anything but letters and digits mirrors the
// unicode61 tokenizer and keeps FTS5 syntax characters out of the query.
func ftsQuery(q string) string {
	words := strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for i, w := range words {
		words[i] = `"` + w + `"*`
	}
	return strings.Join(words, " ")
}
