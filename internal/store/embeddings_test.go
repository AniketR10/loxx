package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"github.com/AniketR10/loxx/internal/scrub"
)

func TestEmbeddingsLifecycle(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	gitID := add(t, db, "git status")
	dockerID := add(t, db, "docker ps")

	pending := func(model string) []int64 {
		t.Helper()
		ps, err := db.PendingEmbeddings(ctx, model, 10)
		if err != nil {
			t.Fatal(err)
		}
		n, err := db.CountPendingEmbeddings(ctx, model)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(ps) {
			t.Errorf("CountPendingEmbeddings = %d, but PendingEmbeddings returned %d", n, len(ps))
		}
		var ids []int64
		for _, p := range ps {
			ids = append(ids, p.ID)
		}
		slices.Sort(ids)
		return ids
	}

	if got := pending("m1"); !slices.Equal(got, []int64{gitID, dockerID}) {
		t.Fatalf("pending = %v, want both commands", got)
	}

	vec := []float32{0.25, -1.5, 3.0e-8, 1}
	if err := db.SaveEmbeddings(ctx, "m1", []Embedding{{CommandID: gitID, Vector: vec}}); err != nil {
		t.Fatal(err)
	}
	if got := pending("m1"); !slices.Equal(got, []int64{dockerID}) {
		t.Errorf("after saving git status, pending = %v, want only docker ps", got)
	}

	// Vectors round-trip exactly and only for their model.
	ids, vecs, dims, err := db.EmbeddingMatrix(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []int64{gitID}) || dims != len(vec) || !slices.Equal(vecs, vec) {
		t.Errorf("EmbeddingMatrix(m1) = %v, %v, %d", ids, vecs, dims)
	}

	// A new model makes everything pending again; saving replaces the old vector.
	if got := pending("m2"); !slices.Equal(got, []int64{gitID, dockerID}) {
		t.Errorf("pending for a new model = %v, want both", got)
	}
	if err := db.SaveEmbeddings(ctx, "m2", []Embedding{{CommandID: gitID, Vector: []float32{9}}}); err != nil {
		t.Fatal(err)
	}
	if ids, _, _, _ := db.EmbeddingMatrix(ctx, "m1"); len(ids) != 0 {
		t.Errorf("old model's vector should be replaced, still have %v", ids)
	}
}

func TestSaveEmbeddingsSkipsDeletedCommands(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	id := add(t, db, "git status")
	if _, err := db.sql.Exec(`DELETE FROM commands WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveEmbeddings(ctx, "m1", []Embedding{{CommandID: id, Vector: []float32{1}}}); err != nil {
		t.Fatalf("saving for a deleted command should be skipped, got %v", err)
	}
	if n := count(t, db, `SELECT count(*) FROM embeddings`); n != 0 {
		t.Errorf("embeddings = %d, want 0", n)
	}
}

func TestEmbeddingsDeletedWithCommand(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	id := add(t, db, "git status")
	if err := db.SaveEmbeddings(ctx, "m1", []Embedding{{CommandID: id, Vector: []float32{1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`DELETE FROM commands WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM embeddings`); n != 0 {
		t.Errorf("embedding survived its command's deletion")
	}
}

// TestMigrateFromV1 upgrades a database written by the Phase 1 schema, with
// data in it, to the current version.
func TestMigrateFromV1(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	raw, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT`,
		migrations[0],
		`INSERT INTO meta VALUES ('schema_version', '1')`,
		`INSERT INTO commands (text, first_seen, last_seen, run_count) VALUES ('git status', 1, 1, 1)`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n := count(t, db, `SELECT value FROM meta WHERE key = 'schema_version'`); n != SchemaVersion {
		t.Errorf("schema_version = %d, want %d", n, SchemaVersion)
	}
	if n, err := db.CountPendingEmbeddings(ctx, "m1"); err != nil || n != 1 {
		t.Errorf("after upgrade: pending = %d, %v; want the existing command pending", n, err)
	}
	if n := count(t, db, `SELECT count(*) FROM pragma_table_info('commands') WHERE name = 'embedded_at'`); n != 0 {
		t.Error("commands.embedded_at should be dropped")
	}
	if n := count(t, db, `SELECT count(*) FROM commands_fts WHERE commands_fts MATCH 'status'`); n != 1 {
		t.Errorf("FTS index lost the upgraded command: %d matches", n)
	}
	if _, err := db.Add(ctx, Execution{Command: scrub.Scrub("ls"), Source: SourceManual}); err != nil {
		t.Errorf("writing after upgrade: %v", err)
	}
}
