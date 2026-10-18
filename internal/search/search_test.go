package search

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/AniketR10/loxx/internal/embed"
	"github.com/AniketR10/loxx/internal/library"
	"github.com/AniketR10/loxx/internal/scrub"
	"github.com/AniketR10/loxx/internal/store"
)

func TestFuse(t *testing.T) {
	p := Params{KeywordWeight: 1, SemanticWeight: 1, RRFK: 60}
	// 2 is second on both sides and beats 1 and 3, which are first on only one.
	if got := fuse([]int64{1, 2}, []int64{3, 2}, p); !slices.Equal(got, []int64{2, 1, 3}) {
		t.Errorf("fuse = %v, want [2 1 3]", got)
	}
	// Weights decide between one-sided winners.
	p.SemanticWeight = 2
	if got := fuse([]int64{1}, []int64{3}, p); !slices.Equal(got, []int64{3, 1}) {
		t.Errorf("semantic-weighted fuse = %v, want [3 1]", got)
	}
	// Ties break by id; an empty side is fine.
	if got := fuse([]int64{9, 4}, nil, Params{KeywordWeight: 1, RRFK: 60}); !slices.Equal(got, []int64{9, 4}) {
		t.Errorf("keyword-only fuse = %v", got)
	}
	if got := fuse(nil, nil, p); len(got) != 0 {
		t.Errorf("empty fuse = %v", got)
	}
}

// history adds cmds to a fresh database and, if embedAll, embeds them with
// the real model.
func history(t *testing.T, embedAll bool, cmds ...string) *store.DB {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, c := range cmds {
		if _, err := db.Add(ctx, store.Execution{Command: scrub.Scrub(c), StartedAt: time.Now(), Source: store.SourceManual}); err != nil {
			t.Fatal(err)
		}
	}
	if !embedAll {
		return db
	}
	model, err := embed.Default()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingEmbeddings(ctx, embed.ModelID, len(cmds))
	if err != nil {
		t.Fatal(err)
	}
	var es []store.Embedding
	for _, p := range pending {
		es = append(es, store.Embedding{CommandID: p.ID, Vector: model.Embed(p.Text)})
	}
	if err := db.SaveEmbeddings(ctx, embed.ModelID, es); err != nil {
		t.Fatal(err)
	}
	return db
}

var cmds = []string{
	"docker run -d -v pgdata:/var/lib/postgresql/data postgres:16",
	"git push --force-with-lease origin main",
	"sudo systemctl restart nginx",
	"kubectl get pods -n production",
	"tar -czvf backup.tar.gz ~/Documents",
}

// withLibrary replaces the built-in library with entries, so results don't
// depend on the real library or on what is installed.
func withLibrary(t *testing.T, s *Searcher, entries ...library.Entry) {
	t.Helper()
	if err := s.SemanticErr(); err != nil {
		t.Fatal(err)
	}
	s.lib, s.libRows, s.libPos, s.libVecs = nil, nil, nil, nil
	if len(entries) == 0 {
		return
	}
	var vectors [][]float32
	for _, e := range entries {
		vectors = append(vectors, s.model.Embed(e.Desc))
	}
	var buf bytes.Buffer
	if err := library.Write(&buf, embed.ModelID, "test", entries, vectors); err != nil {
		t.Fatal(err)
	}
	l, err := library.Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		s.libRows = append(s.libRows, i)
		s.libPos = append(s.libPos, int64(i))
		s.libVecs = append(s.libVecs, vectors[i]...)
	}
	s.lib = l
}

func TestSearchHybrid(t *testing.T) {
	ctx := context.Background()
	s := New(ctx, history(t, true, cmds...))
	withLibrary(t, s)
	for query, want := range map[string]string{
		// No shared words: only semantic search can find these.
		"restart the web server":       "sudo systemctl restart nginx",
		"compress my documents folder": "tar -czvf backup.tar.gz ~/Documents",
		// Exact token: keyword search helps.
		"pgdata": "docker run -d -v pgdata:/var/lib/postgresql/data postgres:16",
	} {
		results, err := s.Search(ctx, query, 3, DefaultParams, store.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) == 0 || results[0].Text != want {
			t.Errorf("Search(%q) top = %v, want %q", query, texts(results), want)
		}
	}
}

// TestSearchFallsBackToKeywords checks core principle P7: with nothing
// embedded, search still answers from keywords.
func TestSearchFallsBackToKeywords(t *testing.T) {
	ctx := context.Background()
	s := New(ctx, history(t, false, cmds...))
	withLibrary(t, s)
	results, err := s.Search(ctx, "nginx", 3, DefaultParams, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Text != "sudo systemctl restart nginx" {
		t.Errorf("keyword fallback = %v", texts(results))
	}
}

// TestSearchLibraryOrder: history with the query's words, then history and library alternating.
func TestSearchLibraryOrder(t *testing.T) {
	ctx := context.Background()
	s := New(ctx, history(t, true, cmds...))
	withLibrary(t, s,
		library.Entry{Tool: "chmod", Command: "chmod +x path/to/file", Cursor: 9, Desc: "Make a file executable"},
		library.Entry{Tool: "nginx", Command: "nginx -t", Cursor: 8, Desc: "Test the nginx configuration"},
		library.Entry{Tool: "systemctl", Command: "sudo systemctl restart nginx", Desc: "Restart nginx"},
	)

	results, err := s.Search(ctx, "nginx", 10, DefaultParams, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	got := texts(results)
	if len(got) < 2 || got[0] != "sudo systemctl restart nginx" || results[0].Library {
		t.Fatalf("a keyword match in history must come first: %v", got)
	}
	if !slices.ContainsFunc(results, func(r store.Result) bool { return r.Library && r.Text == "nginx -t" }) {
		t.Errorf("no library suggestion: %v", got)
	}
	if n := slices.Index(got, "sudo systemctl restart nginx"); slices.Index(got[n+1:], "sudo systemctl restart nginx") >= 0 {
		t.Errorf("a library entry already in history was shown twice: %v", got)
	}

	results, err = s.Search(ctx, "make a script executable", 10, DefaultParams, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 || results[0].Library || !results[1].Library || results[1].Text != "chmod +x path/to/file" || results[1].Cursor != 9 {
		t.Errorf("with no word match, history and library must alternate: %+v", results)
	}

	results, err = s.Search(ctx, "make a script executable", 10, DefaultParams, store.Filter{Failed: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Library {
			t.Errorf("a filter must show history only: %v", texts(results))
		}
	}
}

func texts(rs []store.Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Text)
	}
	return out
}
