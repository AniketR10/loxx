// Package store keeps shell history in SQLite; writes take only scrubbed commands.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/AniketR10/loxx/internal/scrub"
)

// DB is an open history database.
type DB struct {
	sql *sql.DB
}

// DefaultPath is $LOXX_DB_PATH, else $XDG_DATA_HOME/loxx/history.db
// (~/.local/share). An old loc directory is moved into place first.
func DefaultPath() (string, error) {
	if p := os.Getenv("LOXX_DB_PATH"); p != "" {
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
	newDir := filepath.Join(dir, "loxx")
	if err := moveLegacyDir(filepath.Join(dir, "loc"), newDir); err != nil {
		return "", err
	}
	return filepath.Join(newDir, "history.db"), nil
}

// moveLegacyDir moves the old loc directory if it holds our history.db.
func moveLegacyDir(oldDir, newDir string) error {
	if _, err := os.Stat(newDir); !errors.Is(err, os.ErrNotExist) {
		return nil // already there (or not ours to judge)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "history.db")); err != nil {
		return nil // nothing of ours to move
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		if _, statErr := os.Stat(newDir); statErr == nil {
			return nil // another process moved it first
		}
		return fmt.Errorf("moving %s to %s: %w", oldDir, newDir, err)
	}
	return nil
}

// Open opens or creates (owner-only) the database at path and migrates it.
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

	// Serialize first setup across processes: SQLite can return SQLITE_BUSY
	// while switching to WAL, ignoring busy_timeout.
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()

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

// lockFile blocks until it holds an exclusive lock on path, creating the file
// owner-only if needed. The kernel releases the lock if the process dies.
func lockFile(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return func() { f.Close() }, nil
}

// dsn: busy_timeout first so WAL setup waits; _txlock=immediate avoids
// SQLITE_BUSY in read-then-write transactions.
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
	SourceManual Source = "manual" // added with `loxx add`
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
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := addTx(ctx, tx, e, time.Now())
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// AddBatch records many executions in one transaction, for importing history
// files. Either all are stored or none are.
func (db *DB) AddBatch(ctx context.Context, es []Execution) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now()
	for _, e := range es {
		if _, err := addTx(ctx, tx, e, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// addTx inserts e. Imported runs keep a NULL start time; now only fills
// first_seen/last_seen.
func addTx(ctx context.Context, tx *sql.Tx, e Execution, now time.Time) (int64, error) {
	text := e.Command.Text()
	if text == "" {
		return 0, errors.New("store: empty command")
	}
	seen := e.StartedAt
	if seen.IsZero() {
		seen = now
	}

	var id int64
	err := tx.QueryRowContext(ctx, `
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
	return id, err
}

// Meta returns the value stored under key in the meta table, and whether it
// exists.
func (db *DB) Meta(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := db.sql.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetMeta stores value under key in the meta table.
func (db *DB) SetMeta(ctx context.Context, key, value string) error {
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
