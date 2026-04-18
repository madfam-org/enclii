package budgets

import (
	"testing"
	"time"
)

func TestPeriodBoundsMonthly(t *testing.T) {
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	start, end := PeriodBounds(PeriodMonthly, now)
	if !start.Equal(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected monthly start: %v", start)
	}
	if !end.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected monthly end: %v", end)
	}
}

func TestPeriodBoundsWeekly(t *testing.T) {
	// April 17, 2026 is a Friday. Week should span Mon Apr 13 → Mon Apr 20.
	now := time.Date(2026, 4, 17, 8, 30, 0, 0, time.UTC)
	start, end := PeriodBounds(PeriodWeekly, now)
	if start.Weekday() != time.Monday {
		t.Fatalf("weekly start must be Monday, got %v", start.Weekday())
	}
	if end.Sub(start) != 7*24*time.Hour {
		t.Fatalf("weekly range must be 7 days, got %v", end.Sub(start))
	}
}

func TestPeriodBoundsQuarterly(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	start, end := PeriodBounds(PeriodQuarterly, now)
	if !start.Equal(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected quarterly start: %v", start)
	}
	if !end.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected quarterly end: %v", end)
	}
}

func TestNormalizeThresholds(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want []int
	}{
		{"default when empty", nil, []int{50, 80, 100}},
		{"dedupes and sorts", []int{100, 50, 100, 80}, []int{50, 80, 100}},
		{"filters invalid", []int{-5, 0, 50, 600}, []int{50}},
		{"fallback when all invalid", []int{-1, 0, 700}, []int{50, 80, 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeThresholds(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("index %d: got %v want %v", i, got, tc.want)
				}
			}
		})
	}
}

func TestPeriodIsValid(t *testing.T) {
	if !Period("monthly").IsValid() {
		t.Fatal("monthly should be valid")
	}
	if Period("yearly").IsValid() {
		t.Fatal("yearly should not be valid")
	}
}
