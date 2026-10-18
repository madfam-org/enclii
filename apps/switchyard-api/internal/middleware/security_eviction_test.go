package middleware

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"testing"
	"time"
)

func TestEvictOldestLimiters(t *testing.T) {
	base := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		ages      []int // lastAccess = base + ages[i] seconds
		count     int
		wantKept  []int // ages that must survive
		wantTotal int
	}{
		{name: "evicts the three oldest", ages: []int{5, 1, 9, 3, 7, 2, 8, 4, 6, 0}, count: 3,
			wantKept: []int{3, 4, 5, 6, 7, 8, 9}, wantTotal: 7},
		{name: "zero count evicts nothing", ages: []int{2, 1, 0}, count: 0,
			wantKept: []int{0, 1, 2}, wantTotal: 3},
		{name: "count above size empties the map", ages: []int{2, 1, 0}, count: 10,
			wantKept: nil, wantTotal: 0},
		{name: "already sorted input", ages: []int{0, 1, 2, 3, 4, 5}, count: 2,
			wantKept: []int{2, 3, 4, 5}, wantTotal: 4},
		{name: "reverse sorted input", ages: []int{5, 4, 3, 2, 1, 0}, count: 4,
			wantKept: []int{4, 5}, wantTotal: 2},
		{name: "single entry", ages: []int{0}, count: 1, wantKept: nil, wantTotal: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SecurityMiddleware{rateLimiters: map[string]*rateLimiterEntry{}}
			for _, a := range tt.ages {
				s.rateLimiters[fmt.Sprintf("ip-%d", a)] = &rateLimiterEntry{lastAccess: base.Add(time.Duration(a) * time.Second)}
			}
			s.evictOldestLimiters(tt.count)
			if len(s.rateLimiters) != tt.wantTotal {
				t.Fatalf("remaining = %d, want %d", len(s.rateLimiters), tt.wantTotal)
			}
			for _, a := range tt.wantKept {
				if _, ok := s.rateLimiters[fmt.Sprintf("ip-%d", a)]; !ok {
					t.Fatalf("entry with age %d was evicted; only older entries should go", a)
				}
			}
		})
	}
}

// TestSelectOldestMatchesSort checks the quickselect against a full sort on
// random inputs, including many equal timestamps.
func TestSelectOldestMatchesSort(t *testing.T) {
	base := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 200; trial++ {
		n := 1 + rng.IntN(300)
		spread := 1 + rng.IntN(50) // small spread forces ties
		entries := make([]limiterAge, n)
		for i := range entries {
			entries[i] = limiterAge{ip: fmt.Sprint(i), lastAccess: base.Add(time.Duration(rng.IntN(spread)) * time.Second)}
		}
		k := rng.IntN(n + 1)
		sorted := append([]limiterAge(nil), entries...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].lastAccess.Before(sorted[j].lastAccess) })

		selectOldest(entries, k)

		if k == 0 || k == n {
			continue
		}
		var maxSel time.Time
		for _, e := range entries[:k] {
			if e.lastAccess.After(maxSel) {
				maxSel = e.lastAccess
			}
		}
		for _, e := range entries[k:] {
			if e.lastAccess.Before(maxSel) {
				t.Fatalf("trial %d (n=%d k=%d): kept %v older than selected %v", trial, n, k, e.lastAccess, maxSel)
			}
		}
		if !maxSel.Equal(sorted[k-1].lastAccess) {
			t.Fatalf("trial %d (n=%d k=%d): k-th oldest = %v, sort says %v", trial, n, k, maxSel, sorted[k-1].lastAccess)
		}
	}
}

// TestEvictOldestLimitersAtCapacity runs the eviction the middleware performs
// when the map is full (100k entries, evict 10%). The old pairwise sort did
// about 5e9 comparisons here; this must finish well inside the test timeout.
func TestEvictOldestLimitersAtCapacity(t *testing.T) {
	if testing.Short() {
		t.Skip("capacity eviction is skipped in -short mode")
	}
	const n = 100000
	base := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	s := &SecurityMiddleware{rateLimiters: make(map[string]*rateLimiterEntry, n)}
	perm := rand.New(rand.NewPCG(3, 4)).Perm(n)
	for i, p := range perm {
		s.rateLimiters[fmt.Sprint(i)] = &rateLimiterEntry{lastAccess: base.Add(time.Duration(p) * time.Millisecond)}
	}
	start := time.Now()
	s.evictOldestLimiters(n / 10)
	elapsed := time.Since(start)
	if len(s.rateLimiters) != n-n/10 {
		t.Fatalf("remaining = %d, want %d", len(s.rateLimiters), n-n/10)
	}
	cutoff := base.Add(time.Duration(n/10) * time.Millisecond)
	for _, e := range s.rateLimiters {
		if e.lastAccess.Before(cutoff) {
			t.Fatalf("an entry older than the cutoff survived: %v", e.lastAccess)
		}
	}
	t.Logf("evicted %d of %d limiters in %v", n/10, n, elapsed)
}
