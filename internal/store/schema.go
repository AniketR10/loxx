package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// migrations are applied in order; migrations[i] upgrades the schema from
// version i to i+1. Never edit a released migration: append a new one.
var migrations = []string{
	// v1: commands deduplicated by text, one row per execution, and an FTS5
	// index over command text kept in sync by triggers.
	`
CREATE TABLE commands (
	id          INTEGER PRIMARY KEY,
	text        TEXT    NOT NULL UNIQUE,
	first_seen  INTEGER NOT NULL, -- unix milliseconds
	last_seen   INTEGER NOT NULL, -- unix milliseconds
	run_count   INTEGER NOT NULL DEFAULT 0,
	redacted    INTEGER NOT NULL DEFAULT 0, -- number of secrets replaced
	embedded_at INTEGER                     -- unix milliseconds; NULL until embedded
) STRICT;

CREATE TABLE executions (
	id          INTEGER PRIMARY KEY,
	command_id  INTEGER NOT NULL REFERENCES commands(id) ON DELETE CASCADE,
	cwd         TEXT,    -- NULL when unknown (imported history)
	git_root    TEXT,
	exit_code   INTEGER, -- NULL when unknown
	duration_ms INTEGER, -- NULL when unknown
	started_at  INTEGER, -- unix milliseconds; NULL when unknown
	session_id  TEXT,
	hostname    TEXT,
	source      TEXT NOT NULL CHECK (source IN ('live', 'import', 'manual'))
) STRICT;

CREATE INDEX executions_command_id ON executions(command_id);

CREATE VIRTUAL TABLE commands_fts USING fts5(text, content='commands', content_rowid='id');

CREATE TRIGGER commands_fts_insert AFTER INSERT ON commands BEGIN
	INSERT INTO commands_fts(rowid, text) VALUES (new.id, new.text);
END;

CREATE TRIGGER commands_fts_delete AFTER DELETE ON commands BEGIN
	INSERT INTO commands_fts(commands_fts, rowid, text) VALUES ('delete', old.id, old.text);
END;

CREATE TRIGGER commands_fts_update AFTER UPDATE OF text ON commands BEGIN
	INSERT INTO commands_fts(commands_fts, rowid, text) VALUES ('delete', old.id, old.text);
	INSERT INTO commands_fts(rowid, text) VALUES (new.id, new.text);
END;
`,

	// v2: one embedding per command. model_id records which model produced
	// the vector, so after a model change every command counts as pending
	// again and vectors from different models are never compared.
	`
CREATE TABLE embeddings (
	command_id INTEGER PRIMARY KEY REFERENCES commands(id) ON DELETE CASCADE,
	model_id   TEXT    NOT NULL,
	dims       INTEGER NOT NULL,
	vector     BLOB    NOT NULL -- dims little-endian float32 values
) STRICT;
`,

	// v3: drop commands.embedded_at. Whether a command is embedded is decided
	// by the embeddings table, which also covers model changes.
	`ALTER TABLE commands DROP COLUMN embedded_at;`,
}

// SchemaVersion is the schema version this build of loxx writes.
var SchemaVersion = len(migrations)

// migrate brings the database up to SchemaVersion. The version is first read
// without a write lock, so the common already-migrated case stays cheap for
// the many short-lived loxx processes that open the database.
func migrate(ctx context.Context, db *sql.DB) error {
	if v, err := schemaVersion(ctx, db); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	} else if v == SchemaVersion {
		return nil
	}

	// Begin is BEGIN IMMEDIATE (see dsn), so concurrent migrators serialize here
	// and each re-reads the version after acquiring the write lock.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting migration: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT`); err != nil {
		return err
	}
	v, err := schemaVersion(ctx, tx)
	if err != nil {
		return err
	}
	if v > SchemaVersion {
		return fmt.Errorf("database schema version %d is newer than this loxx supports (%d); upgrade loxx", v, SchemaVersion)
	}
	for ; v < SchemaVersion; v++ {
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			return fmt.Errorf("migrating schema to version %d: %w", v+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES ('schema_version', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, strconv.Itoa(v)); err != nil {
		return err
	}
	return tx.Commit()
}

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// schemaVersion returns the stored schema version, or 0 for a new database.
func schemaVersion(ctx context.Context, q querier) (int, error) {
	var exists bool
	if err := q.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'meta')`).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	var s string
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}
