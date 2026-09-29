package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/AniketR10/loc/internal/scrub"
	"github.com/AniketR10/loc/internal/secretcorpus"
)

// The concurrency test re-runs this test binary as writer processes.
const (
	writerEnv       = "LOC_STORE_TEST_WRITER"
	writerCount     = 20
	writesPerWriter = 500
	distinctCmds    = 25
)

func TestMain(m *testing.M) {
	if path := os.Getenv(writerEnv); path != "" {
		os.Exit(writerMain(path))
	}
	os.Exit(m.Run())
}

func writerMain(path string) int {
	ctx := context.Background()
	db, err := Open(ctx, path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		return 1
	}
	defer db.Close()
	for i := range writesPerWriter {
		cmd := scrub.Scrub(fmt.Sprintf("echo shared-%d", i%distinctCmds))
		if _, err := db.Add(ctx, Execution{Command: cmd, Source: SourceManual, StartedAt: time.Now()}); err != nil {
			fmt.Fprintln(os.Stderr, "add:", err)
			return 1
		}
	}
	return 0
}

func openTemp(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func add(t *testing.T, db *DB, raw string) int64 {
	t.Helper()
	exit := 0
	id, err := db.Add(context.Background(), Execution{
		Command:   scrub.Scrub(raw),
		Cwd:       "/home/alice/work/api",
		ExitCode:  &exit,
		StartedAt: time.Now(),
		Source:    SourceManual,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func count(t *testing.T, db *DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOpenPermissions(t *testing.T) {
	// A space and URI-special characters check that dsn escapes the path.
	dir := filepath.Join(t.TempDir(), "data dir #1?", "loc")
	path := filepath.Join(dir, "history.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	add(t, db, "git status") // forces the -wal and -shm files to exist

	for p, want := range map[string]os.FileMode{
		dir:           0o700,
		path:          0o600,
		path + "-wal": 0o600,
		path + "-shm": 0o600,
	} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: mode %o, want %o", filepath.Base(p), got, want)
		}
	}
}

func TestAddDeduplicates(t *testing.T) {
	db, _ := openTemp(t)
	first := add(t, db, "docker compose up -d")
	second := add(t, db, "  docker compose up -d\n")
	if first != second {
		t.Errorf("same command got ids %d and %d", first, second)
	}
	if n := count(t, db, `SELECT run_count FROM commands WHERE id = ?`, first); n != 2 {
		t.Errorf("run_count = %d, want 2", n)
	}
	if n := count(t, db, `SELECT count(*) FROM executions WHERE command_id = ?`, first); n != 2 {
		t.Errorf("executions = %d, want 2", n)
	}
}

func TestAddRejectsEmpty(t *testing.T) {
	db, _ := openTemp(t)
	if _, err := db.Add(context.Background(), Execution{Command: scrub.Scrub("   "), Source: SourceManual}); err == nil {
		t.Error("expected an error for an empty command")
	}
}

func TestAddUnknownFieldsAreNull(t *testing.T) {
	db, _ := openTemp(t)
	id, err := db.Add(context.Background(), Execution{Command: scrub.Scrub("ls"), Source: SourceImport})
	if err != nil {
		t.Fatal(err)
	}
	n := count(t, db, `SELECT count(*) FROM executions WHERE command_id = ? AND cwd IS NULL
		AND exit_code IS NULL AND duration_ms IS NULL AND started_at IS NULL`, id)
	if n != 1 {
		t.Error("unknown fields were not stored as NULL")
	}
}

func TestKeywordIDs(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	pg := add(t, db, "docker run -v pgdata:/var/lib/postgresql/data postgres:16")
	ps := add(t, db, "docker ps")
	gs := add(t, db, "git status")

	tests := []struct {
		query string
		mode  MatchMode
		want  []int64
	}{
		{"postgres", MatchAll, []int64{pg}},
		{"dock postg", MatchAll, []int64{pg}}, // prefixes, ANDed
		{"git", MatchAll, []int64{gs}},
		{"kubectl", MatchAll, nil},
		{"docker status", MatchAll, nil},
		{"docker status", MatchAny, []int64{ps, gs, pg}},  // any word; shorter docs rank higher
		{`"unbalanced (quote* -AND- :col`, MatchAll, nil}, // FTS5 syntax must not leak into the query
		{`"unbalanced (quote* -AND- :col`, MatchAny, nil},
		{"", MatchAny, nil},
	}
	for _, tt := range tests {
		got, err := db.KeywordIDs(ctx, tt.query, tt.mode, Filter{}, 5)
		if err != nil {
			t.Errorf("KeywordIDs(%q, %v): %v", tt.query, tt.mode, err)
			continue
		}
		if tt.mode == MatchAny {
			slices.Sort(got) // BM25 order between near-equal docs is not the point here
			tt.want = slices.Sorted(slices.Values(tt.want))
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("KeywordIDs(%q, %v) = %v, want %v", tt.query, tt.mode, got, tt.want)
		}
	}
}

func TestResults(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	a := add(t, db, "git status")
	b := add(t, db, "docker ps")
	add(t, db, "docker ps") // second run

	results, err := db.Results(ctx, []int64{b, 999, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].CommandID != b || results[1].CommandID != a {
		t.Fatalf("Results kept order %v and skipped unknown ids? got %+v", []int64{b, a}, results)
	}
	r := results[0]
	if r.Text != "docker ps" || r.RunCount != 2 || r.Cwd != "/home/alice/work/api" || r.ExitCode == nil || *r.ExitCode != 0 {
		t.Errorf("result details wrong: %+v", r)
	}
	if got, _ := db.Results(ctx, nil); got != nil {
		t.Errorf("Results(nil) = %v", got)
	}
}

func TestMigrate(t *testing.T) {
	db, path := openTemp(t)
	db.Close()

	// Reopening an up-to-date database is a no-op.
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT value FROM meta WHERE key = 'schema_version'`); n != SchemaVersion {
		t.Errorf("schema_version = %d, want %d", n, SchemaVersion)
	}

	// A database from a newer loc must be refused, not silently used.
	if _, err := db.sql.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, strconv.Itoa(SchemaVersion+1)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if db, err := Open(context.Background(), path); err == nil {
		db.Close()
		t.Error("expected an error opening a database with a newer schema")
	}
}

// TestNoSecretsOnDisk is the Phase 1 exit check for core principle P2: after
// storing every command in the secret corpus, no secret appears anywhere in
// the raw database files, including the WAL and the FTS index.
func TestNoSecretsOnDisk(t *testing.T) {
	positives, err := secretcorpus.Positives()
	if err != nil {
		t.Fatal(err)
	}
	db, path := openTemp(t)
	for _, p := range positives {
		add(t, db, p.Cmd)
	}

	check := func(when string) {
		files, _ := filepath.Glob(path + "*")
		// Positive control: non-secret text must be found, proving the search
		// really inspects the stored data.
		foundControl := false
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			foundControl = foundControl || bytes.Contains(data, []byte("aws configure set aws_access_key_id"))
			for _, p := range positives {
				for _, secret := range p.Secrets {
					if bytes.Contains(data, []byte(secret)) {
						t.Errorf("%s: secret %q found in %s", when, secret, filepath.Base(f))
					}
				}
			}
		}
		if !foundControl {
			t.Errorf("%s: control text not found in %v; the check is not inspecting stored data", when, files)
		}
	}
	check("while open")
	db.Close()
	check("after close")
}

// TestConcurrentWriters simulates many terminal tabs recording at once: 20
// separate processes race to create, migrate and write to one new database.
func TestConcurrentWriters(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns writer processes")
	}
	path := filepath.Join(t.TempDir(), "history.db")

	cmds := make([]*exec.Cmd, writerCount)
	outputs := make([]bytes.Buffer, writerCount)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0], "-test.run=^$")
		cmds[i].Env = append(os.Environ(), writerEnv+"="+path)
		cmds[i].Stdout = &outputs[i]
		cmds[i].Stderr = &outputs[i]
	}
	start := time.Now()
	for _, c := range cmds {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Errorf("writer %d: %v\n%s", i, err, outputs[i].String())
		}
	}
	t.Logf("%d writers x %d inserts took %v", writerCount, writesPerWriter, time.Since(start))

	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	total := writerCount * writesPerWriter
	if n := count(t, db, `SELECT count(*) FROM executions`); n != total {
		t.Errorf("executions = %d, want %d", n, total)
	}
	if n := count(t, db, `SELECT count(*) FROM commands`); n != distinctCmds {
		t.Errorf("commands = %d, want %d", n, distinctCmds)
	}
	if n := count(t, db, `SELECT count(*) FROM commands WHERE run_count != ?`, total/distinctCmds); n != 0 {
		t.Errorf("%d commands have a wrong run_count", n)
	}
}
