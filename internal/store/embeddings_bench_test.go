package store

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"
)

// BenchmarkEmbeddingMatrix measures loading every vector at history sizes
// the Phase 3 latency target cares about.
func BenchmarkEmbeddingMatrix(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		db := seedVectors(b, n, 384)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for b.Loop() {
				if _, _, _, err := db.EmbeddingMatrix(context.Background(), "bench"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// seedVectors creates a database with n commands and random vectors, written
// in one transaction (store.Add per row would take minutes).
func seedVectors(b *testing.B, n, dims int) *DB {
	b.Helper()
	db, err := Open(context.Background(), filepath.Join(b.TempDir(), "history.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	tx, err := db.sql.Begin()
	if err != nil {
		b.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(1, 2))
	vec := make([]float32, dims)
	for i := range n {
		if _, err := tx.Exec(`INSERT INTO commands (id, text, first_seen, last_seen, run_count) VALUES (?, ?, 0, 0, 1)`,
			i+1, fmt.Sprintf("command number %d", i)); err != nil {
			b.Fatal(err)
		}
		for j := range vec {
			vec[j] = rng.Float32()*2 - 1
		}
		if _, err := tx.Exec(`INSERT INTO embeddings VALUES (?, 'bench', ?, ?)`, i+1, dims, encodeVector(vec)); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return db
}
