package search

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AniketR10/loc/internal/store"
)

// BenchmarkKeywordLatency measures what the user waits for before the first
// results appear: open the database, run the keyword query, fetch details.
// Point LOC_BENCH_DB at a large history (e.g. 100k commands) to run it.
func BenchmarkKeywordLatency(b *testing.B) {
	path := os.Getenv("LOC_BENCH_DB")
	if path == "" {
		b.Skip("set LOC_BENCH_DB to a history database")
	}
	ctx := context.Background()
	for _, q := range []string{"docker run", "kubectl get pods", "opt54321", "nothing matches this"} {
		b.Run(q, func(b *testing.B) {
			for b.Loop() {
				start := time.Now()
				db, err := store.Open(ctx, path)
				if err != nil {
					b.Fatal(err)
				}
				ids, err := db.KeywordIDs(ctx, q, DefaultParams.KeywordMode, candidates)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := db.Results(ctx, ids[:min(5, len(ids))]); err != nil {
					b.Fatal(err)
				}
				db.Close()
				b.ReportMetric(float64(time.Since(start).Microseconds())/1000, "ms/first-results")
			}
		})
	}
}
