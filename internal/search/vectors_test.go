package search

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func randomUnitVectors(n, dims int, seed uint64) []float32 {
	rng := rand.New(rand.NewPCG(seed, seed+1))
	v := make([]float32, n*dims)
	for i := range n {
		row := v[i*dims : (i+1)*dims]
		var norm float64
		for j := range row {
			row[j] = rng.Float32()*2 - 1
			norm += float64(row[j]) * float64(row[j])
		}
		for j := range row {
			row[j] /= float32(math.Sqrt(norm))
		}
	}
	return v
}

func TestTopKByDotMatchesSort(t *testing.T) {
	const n, dims = 1000, 384
	vectors := randomUnitVectors(n, dims, 1)
	q := randomUnitVectors(1, dims, 99)
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = int64(i + 100)
	}

	// Reference: score everything, sort.
	all := make([]scored, n)
	for i := range n {
		var s float32
		for j := range dims {
			s += vectors[i*dims+j] * q[j]
		}
		all[i] = scored{ids[i], s}
	}
	slices.SortFunc(all, func(a, b scored) int {
		if a.score > b.score {
			return -1
		}
		if a.score < b.score {
			return 1
		}
		return 0
	})

	for _, k := range []int{1, 5, 50, n, n + 10} {
		got := topKByDot(q, ids, vectors, dims, k)
		want := all[:min(k, n)]
		if len(got) != len(want) {
			t.Fatalf("k=%d: %d results, want %d", k, len(got), len(want))
		}
		for i := range got {
			if got[i].id != want[i].id || math.Abs(float64(got[i].score-want[i].score)) > 1e-5 {
				t.Errorf("k=%d rank %d: got %+v, want %+v", k, i, got[i], want[i])
				break
			}
		}
	}
	if got := topKByDot(q, nil, nil, dims, 5); got != nil {
		t.Errorf("empty index: got %v", got)
	}
}

func BenchmarkTopKByDot(b *testing.B) {
	const dims = 384
	q := randomUnitVectors(1, dims, 99)
	for _, n := range []int{10_000, 100_000, 500_000} {
		vectors := randomUnitVectors(n, dims, 1)
		ids := make([]int64, n)
		for i := range ids {
			ids[i] = int64(i)
		}
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for b.Loop() {
				topKByDot(q, ids, vectors, dims, 50)
			}
		})
	}
}
