// Package search ranks recorded commands for a query by combining keyword
// (BM25) and semantic (embedding) similarity.
package search

import (
	"container/heap"
)

// scored is a command id with its similarity to the query.
type scored struct {
	id    int64
	score float32
}

// topKByDot returns the k rows of vectors (row-major, dims wide, row i
// belonging to ids[i]) with the highest dot product with q, best first,
// considering only ids in allowed (nil means all). The stored vectors and q
// are unit length, so the dot product is the cosine similarity.
func topKByDot(q []float32, ids []int64, vectors []float32, dims, k int, allowed map[int64]bool) []scored {
	if k <= 0 || len(ids) == 0 {
		return nil
	}
	q = q[:dims]
	h := make(minHeap, 0, k)
	for i, id := range ids {
		if allowed != nil && !allowed[id] {
			continue
		}
		row := vectors[i*dims : (i+1)*dims]
		var s0, s1, s2, s3 float32
		j := 0
		for ; j+4 <= dims; j += 4 {
			s0 += row[j] * q[j]
			s1 += row[j+1] * q[j+1]
			s2 += row[j+2] * q[j+2]
			s3 += row[j+3] * q[j+3]
		}
		for ; j < dims; j++ {
			s0 += row[j] * q[j]
		}
		s := (s0 + s1) + (s2 + s3)
		if len(h) < k {
			heap.Push(&h, scored{id, s})
		} else if s > h[0].score {
			h[0] = scored{id, s}
			heap.Fix(&h, 0)
		}
	}
	out := make([]scored, len(h))
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(&h).(scored)
	}
	return out
}

// minHeap keeps the k best scores seen so far, worst on top.
type minHeap []scored

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].score < h[j].score }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(scored)) }
func (h *minHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}
