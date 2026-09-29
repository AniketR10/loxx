package search

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/AniketR10/loc/internal/embed"
	"github.com/AniketR10/loc/internal/store"
)

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

// DefaultParams are the weights loc ships with, chosen on two local eval sets
// drawn from real history (2026-09-27, see ROADMAP Phase 3):
//
//   - 40 natural-language questions: semantic-only 0.55/0.80/0.65
//     (top-1/top-5/MRR); these defaults match it exactly, because an
//     AND-query of a sentence rarely matches any command, leaving semantic
//     ranking in charge.
//   - 25 typed fragments ("stash pop", "port-forward"): keyword 0.98 MRR,
//     semantic 0.91, these defaults 1.00.
//
// Neighbouring settings (k=60 kw=0.25, k=20 kw=0.5) score the same, so this
// is a flat optimum rather than a lucky point. OR-mode keywords and the
// earlier equal-weight default scored lower on both sets.
var DefaultParams = Params{
	KeywordMode:    store.MatchAll,
	KeywordWeight:  0.5,
	SemanticWeight: 1,
	RRFK:           60,
}

// Searcher answers queries over one history database. The embedding model
// and the stored vectors load in the background as soon as it is created, so
// keyword search can run meanwhile.
type Searcher struct {
	db     *store.DB
	loaded chan struct{}

	// Set by the background load; read only after loaded is closed.
	model   *embed.Model
	ids     []int64
	vectors []float32
	dims    int
	loadErr error
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.model, modelErr = embed.Default()
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

// Search returns up to limit commands for query kept by f, best first.
func (s *Searcher) Search(ctx context.Context, query string, limit int, p Params, f store.Filter) ([]store.Result, error) {
	ids, err := s.Rank(ctx, query, limit, p, f)
	if err != nil {
		return nil, err
	}
	return s.db.Results(ctx, ids)
}

// Rank returns the ids of up to limit commands for query kept by f, best
// first. It waits for the model and vectors to load.
func (s *Searcher) Rank(ctx context.Context, query string, limit int, p Params, f store.Filter) ([]int64, error) {
	var keyword []int64
	if p.KeywordWeight > 0 {
		var err error
		if keyword, err = s.db.KeywordIDs(ctx, query, p.KeywordMode, f, candidates); err != nil {
			return nil, err
		}
	}
	var semantic []int64
	if p.SemanticWeight > 0 {
		allowed, err := s.db.FilterIDs(ctx, f)
		if err != nil {
			return nil, err
		}
		semantic = s.semantic(query, allowed, candidates)
	}
	ids := fuse(keyword, semantic, p)
	return ids[:min(limit, len(ids))], nil
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

// semantic returns the ids of the k stored vectors closest to query among
// allowed (nil means all), or nil when semantic search is unavailable (core
// principle P7: fall back to keywords instead of failing).
func (s *Searcher) semantic(query string, allowed map[int64]bool, k int) []int64 {
	if s.SemanticErr() != nil || len(s.ids) == 0 {
		return nil
	}
	top := topKByDot(s.model.Embed(query), s.ids, s.vectors, s.dims, k, allowed)
	ids := make([]int64, len(top))
	for i, t := range top {
		ids[i] = t.id
	}
	return ids
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
