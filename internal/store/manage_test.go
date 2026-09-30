package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AniketR10/loxx/internal/scrub"
)

func TestStats(t *testing.T) {
	ctx := context.Background()
	db, _ := openTemp(t)
	add(t, db, "export DB_PASSWORD=hunter2") // one redaction, manual source
	if err := db.AddBatch(ctx, []Execution{
		{Command: scrub.Scrub("git status"), Source: SourceImport},
		{Command: scrub.Scrub("git status"), Source: SourceImport},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Add(ctx, Execution{Command: scrub.Scrub("ls"), Source: SourceLive}); err != nil {
		t.Fatal(err)
	}
	s, err := db.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{Commands: 3, LiveRuns: 1, ImportedRuns: 2, OtherRuns: 1, Redactions: 1}
	if s != want {
		t.Errorf("Stats = %+v, want %+v", s, want)
	}
}

// TestForgetLeavesNothingOnDisk checks that forgotten text is gone from the
// raw database files, not just hidden from queries.
func TestForgetLeavesNothingOnDisk(t *testing.T) {
	ctx := context.Background()
	db, path := openTemp(t)
	secret := "forget-me-7f3a9c"
	var forgetIDs []int64
	for _, c := range []string{"echo " + secret, "curl https://" + secret + ".example.com"} {
		forgetIDs = append(forgetIDs, add(t, db, c))
		add(t, db, c) // a second run of the same command
	}
	keep := add(t, db, "git status keep-me-1d2e")
	if err := db.SaveEmbeddings(ctx, "m", []Embedding{{CommandID: forgetIDs[0], Vector: []float32{1, 2}}, {CommandID: keep, Vector: []float32{3, 4}}}); err != nil {
		t.Fatal(err)
	}

	found, err := db.FindCommands(ctx, secret)
	if err != nil || len(found) != 2 {
		t.Fatalf("FindCommands = %v, %v; want 2", found, err)
	}
	runs, err := db.Forget(ctx, forgetIDs)
	if err != nil || runs != 4 {
		t.Fatalf("Forget = %d runs, %v; want 4", runs, err)
	}

	if n := count(t, db, `SELECT count(*) FROM commands`); n != 1 {
		t.Errorf("%d commands left, want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM executions`); n != 1 {
		t.Errorf("%d executions left, want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM embeddings`); n != 1 {
		t.Errorf("%d embeddings left, want 1", n)
	}
	if ids, _ := db.KeywordIDs(ctx, "forget", MatchAll, Filter{}, 10); len(ids) != 0 {
		t.Errorf("the full-text index still finds forgotten commands: %v", ids)
	}

	check := func(when string) {
		files, _ := filepath.Glob(path + "*")
		sawKeep := false
		for _, f := range files {
			data, _ := os.ReadFile(f)
			if bytes.Contains(data, []byte(secret)) {
				t.Errorf("%s: forgotten text still in %s", when, filepath.Base(f))
			}
			sawKeep = sawKeep || bytes.Contains(data, []byte("keep-me-1d2e"))
		}
		if !sawKeep {
			t.Errorf("%s: control text missing, so the check is not reading the stored data", when)
		}
	}
	check("while open")
	db.Close()
	check("after close")
}
