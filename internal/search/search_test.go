package search

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/AniketR10/loc/internal/embed"
	"github.com/AniketR10/loc/internal/scrub"
	"github.com/AniketR10/loc/internal/store"
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

func TestSearchHybrid(t *testing.T) {
	ctx := context.Background()
	s := New(ctx, history(t, true, cmds...))
	if err := s.SemanticErr(); err != nil {
		t.Fatal(err)
	}
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
	results, err := s.Search(ctx, "nginx", 3, DefaultParams, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Text != "sudo systemctl restart nginx" {
		t.Errorf("keyword fallback = %v", texts(results))
	}
}

func texts(rs []store.Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Text)
	}
	return out
}
