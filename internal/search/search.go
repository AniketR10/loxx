package search

import (
	"cmp"
	"context"
	"os"
	"slices"
	"sync"

	"github.com/AniketR10/loxx/internal/embed"
	"github.com/AniketR10/loxx/internal/library"
	"github.com/AniketR10/loxx/internal/store"
)

// libraryRows is the most library suggestions one search shows.
const libraryRows = 5

// candidates is how many results each side contributes to fusion.
const candidates = 50

// Params weight the two rankings in Reciprocal Rank Fusion: a command at rank
// r (1-based) on a side scores weight / (RRFK + r) from that side.
type Params struct {
	KeywordMode    store.MatchMode
	KeywordWeight  float64
	SemanticWeight float64
	RRFK           float64
}

// DefaultParams were tuned on two eval sets from real history.
var DefaultParams = Params{
	KeywordMode:    store.MatchAll,
	KeywordWeight:  0.5,
	SemanticWeight: 1,
	RRFK:           60,
}

// Searcher searches one database; the model loads in the background meanwhile.
type Searcher struct {
	db     *store.DB
	loaded chan struct{}

	// Set by the background load; read only after loaded is closed.
	model   *embed.Model
	ids     []int64
	vectors []float32
	dims    int
	loadErr error

	// Library entries whose program is installed, and their vectors.
	lib     *library.Library
	libRows []int
	libPos  []int64 // 0..len(libRows)-1, for topKByDot
	libVecs []float32
}

// New returns a Searcher and starts loading the model and vectors, in
// parallel with each other.
func New(ctx context.Context, db *store.DB) *Searcher {
	s := &Searcher{db: db, loaded: make(chan struct{})}
	go func() {
		defer close(s.loaded)
		var (
			wg       sync.WaitGroup
			modelErr error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.model, modelErr = embed.Default()
		}()
		go func() {
			defer wg.Done()
			s.loadLibrary()
		}()
		s.ids, s.vectors, s.dims, s.loadErr = db.EmbeddingMatrix(ctx, embed.ModelID)
		wg.Wait()
		if s.loadErr == nil {
			s.loadErr = modelErr
		}
	}()
	return s
}

// SemanticErr waits for the background load and reports why semantic search
// is unavailable, if it is. Search still works then, on keywords alone.
func (s *Searcher) SemanticErr() error {
	<-s.loaded
	return s.loadErr
}

// Search returns up to limit results for query kept by f, best first. With no
// filter, history containing the query's words comes first, then the rest of
// history alternates with library suggestions.
func (s *Searcher) Search(ctx context.Context, query string, limit int, p Params, f store.Filter) ([]store.Result, error) {
	ids, words, q, err := s.rank(ctx, query, limit, p, f)
	if err != nil {
		return nil, err
	}
	results, err := s.db.Results(ctx, ids)
	if err != nil || !f.IsZero() || q == nil {
		return results, err
	}
	seen := map[string]bool{}
	for _, r := range results {
		seen[r.Text] = true
	}
	var lib []store.Result
	for _, r := range s.librarySearch(q, libraryRows+len(results)) {
		if !seen[r.Text] && len(lib) < libraryRows {
			lib = append(lib, r)
		}
	}
	var merged, rest []store.Result
	for _, r := range results {
		if words[r.CommandID] {
			merged = append(merged, r)
		} else {
			rest = append(rest, r)
		}
	}
	for i := 0; i < max(len(rest), len(lib)); i++ {
		if i < len(rest) {
			merged = append(merged, rest[i])
		}
		if i < len(lib) {
			merged = append(merged, lib[i])
		}
	}
	return merged[:min(limit, len(merged))], nil
}

// Rank returns the ids of up to limit commands for query kept by f, best
// first. It waits for the model and vectors to load.
func (s *Searcher) Rank(ctx context.Context, query string, limit int, p Params, f store.Filter) ([]int64, error) {
	ids, _, _, err := s.rank(ctx, query, limit, p, f)
	return ids, err
}

// rank is Rank, also returning the keyword matches and the query's vector
// (nil without the model).
func (s *Searcher) rank(ctx context.Context, query string, limit int, p Params, f store.Filter) ([]int64, map[int64]bool, []float32, error) {
	words := map[int64]bool{}
	var keyword []int64
	if p.KeywordWeight > 0 {
		var err error
		if keyword, err = s.db.KeywordIDs(ctx, query, p.KeywordMode, f, candidates); err != nil {
			return nil, nil, nil, err
		}
		for _, id := range keyword {
			words[id] = true
		}
	}
	var semantic []int64
	var q []float32
	if p.SemanticWeight > 0 && s.SemanticErr() == nil {
		allowed, err := s.db.FilterIDs(ctx, f)
		if err != nil {
			return nil, nil, nil, err
		}
		q = s.model.Embed(query)
		for _, t := range s.semantic(q, allowed, candidates) {
			semantic = append(semantic, t.id)
		}
	}
	ids := fuse(keyword, semantic, p)
	return ids[:min(limit, len(ids))], words, q, nil
}

// Keyword returns up to limit commands matching query as keywords, kept by f.
// It never waits for the model, so the search panel can show it instantly.
func (s *Searcher) Keyword(ctx context.Context, query string, limit int, f store.Filter) ([]store.Result, error) {
	ids, err := s.db.KeywordIDs(ctx, query, DefaultParams.KeywordMode, f, limit)
	if err != nil {
		return nil, err
	}
	return s.db.Results(ctx, ids)
}

// Recent returns up to limit of the most recently run commands kept by f.
func (s *Searcher) Recent(ctx context.Context, limit int, f store.Filter) ([]store.Result, error) {
	ids, err := s.db.RecentIDs(ctx, f, limit)
	if err != nil {
		return nil, err
	}
	return s.db.Results(ctx, ids)
}

// semantic returns the k commands among allowed (nil: all) closest to q.
func (s *Searcher) semantic(q []float32, allowed map[int64]bool, k int) []scored {
	if len(s.ids) == 0 {
		return nil
	}
	return topKByDot(q, s.ids, s.vectors, s.dims, k, allowed)
}

// loadLibrary keeps the library entries whose program is installed. Without a
// library (or with one made by another model), search is history only.
func (s *Searcher) loadLibrary() {
	l, err := library.Default()
	if err != nil || l == nil || l.ModelID != embed.ModelID {
		return
	}
	rows := l.Installed(os.Getenv("PATH"))
	vecs := make([]float32, len(rows)*l.Dims())
	for i, r := range rows {
		l.Vector(r, vecs[i*l.Dims():])
	}
	pos := make([]int64, len(rows))
	for i := range pos {
		pos[i] = int64(i)
	}
	s.lib, s.libRows, s.libPos, s.libVecs = l, rows, pos, vecs
}

// librarySearch returns the k library entries closest to q, as results.
func (s *Searcher) librarySearch(q []float32, k int) []store.Result {
	if len(s.libRows) == 0 {
		return nil
	}
	var out []store.Result
	for _, t := range topKByDot(q, s.libPos, s.libVecs, s.lib.Dims(), k, nil) {
		e := s.lib.Entries[s.libRows[t.id]]
		out = append(out, store.Result{Text: e.Command, Library: true, Desc: e.Desc, Cursor: e.Cursor})
	}
	return out
}

// fuse merges two rankings with weighted Reciprocal Rank Fusion. Ties go to
// the lower id so results are deterministic.
func fuse(keyword, semantic []int64, p Params) []int64 {
	score := make(map[int64]float64, len(keyword)+len(semantic))
	for r, id := range keyword {
		score[id] += p.KeywordWeight / (p.RRFK + float64(r+1))
	}
	for r, id := range semantic {
		score[id] += p.SemanticWeight / (p.RRFK + float64(r+1))
	}
	ids := make([]int64, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b int64) int {
		if c := cmp.Compare(score[b], score[a]); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	return ids
}
