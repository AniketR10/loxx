package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Stats summarizes the history database for `loxx status`.
type Stats struct {
	Commands                          int // unique commands
	LiveRuns, ImportedRuns, OtherRuns int
	Redactions                        int // secrets replaced across all commands
}

// Stats counts what the database holds.
func (db *DB) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	err := db.sql.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(redacted), 0) FROM commands`).Scan(&s.Commands, &s.Redactions)
	if err != nil {
		return s, err
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT source, count(*) FROM executions GROUP BY source`)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			source string
			n      int
		)
		if err := rows.Scan(&source, &n); err != nil {
			return s, err
		}
		switch Source(source) {
		case SourceLive:
			s.LiveRuns = n
		case SourceImport:
			s.ImportedRuns = n
		default:
			s.OtherRuns += n
		}
	}
	return s, rows.Err()
}

// FindCommands returns the commands containing text (case-sensitive), most
// recently run first.
func (db *DB) FindCommands(ctx context.Context, text string) ([]Result, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT id FROM commands WHERE instr(text, ?) > 0 ORDER BY last_seen DESC, id DESC`, text)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return db.resultsBatched(ctx, ids)
}

// resultsBatched is Results for any number of ids (SQLite limits how many
// parameters one statement may take).
func (db *DB) resultsBatched(ctx context.Context, ids []int64) ([]Result, error) {
	var all []Result
	for len(ids) > 0 {
		n := min(len(ids), 500)
		rs, err := db.Results(ctx, ids[:n])
		if err != nil {
			return nil, err
		}
		all = append(all, rs...)
		ids = ids[n:]
	}
	return all, nil
}

// Forget deletes the given commands with all their runs and vectors, and
// makes sure their text is gone from the files on disk too: secure_delete
// overwrites freed pages, the full-text index is compacted so no old segment
// still holds the words, and the WAL is checkpointed and truncated. It
// returns how many runs were removed.
func (db *DB) Forget(ctx context.Context, ids []int64) (runs int, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	conn, err := db.sql.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA secure_delete = ON`); err != nil {
		return 0, err
	}
	defer conn.ExecContext(context.Background(), `PRAGMA secure_delete = OFF`)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for len(ids) > 0 {
		batch := ids[:min(len(ids), 500)]
		ids = ids[len(batch):]
		in, args := placeholders(batch)
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM executions WHERE command_id IN (`+in+`)`, args...).Scan(&n); err != nil {
			return 0, err
		}
		runs += n
		// Executions and embeddings go with their command (ON DELETE CASCADE);
		// the FTS trigger removes the text from the index.
		if _, err := tx.ExecContext(ctx, `DELETE FROM commands WHERE id IN (`+in+`)`, args...); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO commands_fts(commands_fts) VALUES ('optimize')`); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	var busy, logFrames, checkpointed int
	err = conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed)
	return runs, err
}

func placeholders(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return "?" + strings.Repeat(", ?", len(ids)-1), args
}

// LastRunAt returns when the most recent live command started, or nil.
func (db *DB) LastRunAt(ctx context.Context) (*time.Time, error) {
	var ms sql.NullInt64
	if err := db.sql.QueryRowContext(ctx, `SELECT max(started_at) FROM executions WHERE source = 'live'`).Scan(&ms); err != nil {
		return nil, err
	}
	if !ms.Valid {
		return nil, nil
	}
	t := time.UnixMilli(ms.Int64)
	return &t, nil
}
