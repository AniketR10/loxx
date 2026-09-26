// Package store persists shell history in SQLite.
//
// Commands are deduplicated by their scrubbed text; each time a command runs
// is a separate execution row. Every write takes a scrub.Command, so nothing
// reaches the database without passing through the scrubber.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/AniketR10/loc/internal/scrub"
)

// DB is an open history database.
type DB struct {
	sql *sql.DB
}

// DefaultPath returns the history database location: $LOC_DB_PATH if set,
// otherwise $XDG_DATA_HOME/loc/history.db, falling back to
// ~/.local/share/loc/history.db as the XDG spec requires.
func DefaultPath() (string, error) {
	if p := os.Getenv("LOC_DB_PATH"); p != "" {
		return p, nil
	}
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" || !filepath.IsAbs(dir) { // the XDG spec says to ignore relative paths
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "loc", "history.db"), nil
}

// Open opens the database at path, creating it and any missing parent
// directories with owner-only permissions, and migrates it to the current
// schema.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// Create the file ourselves so it is owner-only from the start. SQLite
	// gives the -wal and -shm files the same permissions as the database.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}

	sqlDB, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	if err := migrate(ctx, sqlDB); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return &DB{sql: sqlDB}, nil
}

// dsn builds the modernc.org/sqlite connection string. Pragmas run on every
// new connection, in order: busy_timeout comes first so that switching to WAL
// waits for other processes instead of failing. _txlock=immediate makes every
// transaction take the write lock up front, which avoids the SQLITE_BUSY
// errors that WAL returns when a read transaction later tries to write.
func dsn(path string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Set("_txlock", "immediate")
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// Close closes the database.
func (db *DB) Close() error {
	return db.sql.Close()
}

// Source says how an execution was recorded.
type Source string

const (
	SourceLive   Source = "live"   // captured by the shell hook
	SourceImport Source = "import" // imported from an existing history file
	SourceManual Source = "manual" // added with `loc add`
)

// Execution is one run of a command. Zero values mean "unknown" and are
// stored as NULL.
type Execution struct {
	Command   scrub.Command
	Cwd       string
	GitRoot   string
	ExitCode  *int
	Duration  *time.Duration
	StartedAt time.Time
	SessionID string
	Hostname  string
	Source    Source
}

// Add records an execution, creating the command if it is new, and returns
// the command's id.
func (db *DB) Add(ctx context.Context, e Execution) (int64, error) {
	text := e.Command.Text()
	if text == "" {
		return 0, errors.New("store: empty command")
	}
	seen := e.StartedAt
	if seen.IsZero() {
		seen = time.Now()
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO commands (text, first_seen, last_seen, run_count, redacted)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT (text) DO UPDATE SET
			first_seen = min(first_seen, excluded.first_seen),
			last_seen  = max(last_seen, excluded.last_seen),
			run_count  = run_count + 1
		RETURNING id`,
		text, seen.UnixMilli(), seen.UnixMilli(), e.Command.Redactions()).Scan(&id)
	if err != nil {
		return 0, err
	}

	var durationMS, startedAt any
	if e.Duration != nil {
		durationMS = e.Duration.Milliseconds()
	}
	if !e.StartedAt.IsZero() {
		startedAt = e.StartedAt.UnixMilli()
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO executions
			(command_id, cwd, git_root, exit_code, duration_ms, started_at, session_id, hostname, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nullIfEmpty(e.Cwd), nullIfEmpty(e.GitRoot), e.ExitCode, durationMS, startedAt,
		nullIfEmpty(e.SessionID), nullIfEmpty(e.Hostname), string(e.Source))
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
