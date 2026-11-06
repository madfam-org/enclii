package resourceusage

import (
	"testing"
	"time"
)

func cdmx(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// A CDMX civil day runs 06:00Z to 06:00Z: Mexico has had no DST since
// October 2022, so a late-evening local time belongs to the local date even
// though it is already the next UTC day.
func TestDayOf_CDMXBoundaries(t *testing.T) {
	loc := cdmx(t)
	for _, tc := range []struct {
		in   time.Time
		date string
	}{
		{time.Date(2026, 11, 5, 5, 59, 59, 0, time.UTC), "2026-11-04"},
		{time.Date(2026, 11, 5, 6, 0, 0, 0, time.UTC), "2026-11-05"},
		{time.Date(2026, 11, 4, 23, 30, 0, 0, loc), "2026-11-04"},
	} {
		d := DayOf(tc.in, loc)
		if d.Date != tc.date {
			t.Errorf("DayOf(%s) = %s, want %s", tc.in, d.Date, tc.date)
		}
	}
	d := DayOf(time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC), loc)
	if want := time.Date(2026, 11, 4, 6, 0, 0, 0, time.UTC); !d.Start.Equal(want) {
		t.Errorf("Start = %s, want %s", d.Start.UTC(), want)
	}
	if want := time.Date(2026, 11, 5, 6, 0, 0, 0, time.UTC); !d.End.Equal(want) {
		t.Errorf("End = %s, want %s", d.End.UTC(), want)
	}
	if d.Slots() != 288 {
		t.Errorf("Slots = %d, want 288", d.Slots())
	}
}

// The first Sunday of April used to start DST and the last Sunday of October
// to end it. Since the 2022 abolition both are plain 24-hour days.
func TestDayOf_NoDSTSince2022(t *testing.T) {
	loc := cdmx(t)
	for _, date := range []string{"2026-04-05", "2026-10-25", "2025-04-06", "2023-10-29"} {
		d, err := ParseDay(date, loc)
		if err != nil {
			t.Fatal(err)
		}
		if got := d.End.Sub(d.Start); got != 24*time.Hour {
			t.Errorf("%s lasts %s, want 24h", date, got)
		}
		if h := d.Start.UTC().Hour(); h != 6 {
			t.Errorf("%s starts at %02d:00Z, want 06:00Z", date, h)
		}
	}
}

// The arithmetic never assumes 24 hours: the last DST change (2022-10-30)
// made a 25-hour day, and a pre-abolition summer day started at 05:00Z.
func TestDayOf_HistoricDSTDaysKeepTheirLength(t *testing.T) {
	loc := cdmx(t)
	d, _ := ParseDay("2022-10-30", loc)
	if got := d.End.Sub(d.Start); got != 25*time.Hour {
		t.Errorf("2022-10-30 lasts %s, want 25h", got)
	}
	if d.Slots() != 300 {
		t.Errorf("Slots = %d, want 300", d.Slots())
	}
	summer, _ := ParseDay("2022-07-01", loc)
	if h := summer.Start.UTC().Hour(); h != 5 {
		t.Errorf("2022-07-01 starts at %02d:00Z, want 05:00Z", h)
	}
}

func TestParseDay_RejectsBadDates(t *testing.T) {
	loc := cdmx(t)
	for _, in := range []string{"", "2026-13-01", "2026/11/04", "04-11-2026", "2026-11-04T00:00:00Z"} {
		if _, err := ParseDay(in, loc); err == nil {
			t.Errorf("ParseDay(%q) accepted", in)
		}
	}
}

func TestSlotIndex(t *testing.T) {
	loc := cdmx(t)
	d, _ := ParseDay("2026-11-04", loc)
	for _, tc := range []struct {
		ts   time.Time
		want int
	}{
		{d.Start, -1},                      // belongs to the previous day's last slot
		{d.Start.Add(Step), 0},             // first sample of the day
		{d.End, 287},                       // last sample of the day
		{d.End.Add(Step), -1},              // next day
		{d.Start.Add(7 * time.Minute), -1}, // off the grid
	} {
		if got := d.SlotIndex(tc.ts); got != tc.want {
			t.Errorf("SlotIndex(%s) = %d, want %d", tc.ts.UTC(), got, tc.want)
		}
	}
}

func TestCompleteDays_WaitsForSettleAndListsOldestFirst(t *testing.T) {
	loc := cdmx(t)
	// 00:10 CDMX on 2026-11-05: yesterday ended 10 minutes ago, inside the
	// 15-minute settle, so the newest complete day is 2026-11-03.
	early := time.Date(2026, 11, 5, 6, 10, 0, 0, time.UTC)
	days := CompleteDays(early, loc, 3, 15*time.Minute)
	assertDates(t, days, "2026-11-01", "2026-11-02", "2026-11-03")

	// 00:20 CDMX: yesterday is complete.
	later := time.Date(2026, 11, 5, 6, 20, 0, 0, time.UTC)
	days = CompleteDays(later, loc, 3, 15*time.Minute)
	assertDates(t, days, "2026-11-02", "2026-11-03", "2026-11-04")

	if got := CompleteDays(later, loc, 0, 0); len(got) != 0 {
		t.Errorf("lookback 0 returned %d days", len(got))
	}
}

func assertDates(t *testing.T, days []Day, want ...string) {
	t.Helper()
	if len(days) != len(want) {
		t.Fatalf("got %d days, want %d", len(days), len(want))
	}
	for i := range want {
		if days[i].Date != want[i] {
			t.Errorf("day %d = %s, want %s", i, days[i].Date, want[i])
		}
	}
}

// Range queries include both ends; the windows must tile the day's sample
// timestamps exactly once each.
func TestChunks_TileTheDayWithoutOverlap(t *testing.T) {
	loc := cdmx(t)
	for _, tc := range []struct {
		date  string
		hours int
		n     int
	}{{"2026-11-04", 6, 4}, {"2026-11-04", 5, 5}, {"2026-11-04", 24, 1}, {"2022-10-30", 6, 5}} {
		d, _ := ParseDay(tc.date, loc)
		ws := chunks(d, tc.hours)
		if len(ws) != tc.n {
			t.Errorf("%s/%dh: %d windows, want %d", tc.date, tc.hours, len(ws), tc.n)
		}
		seen := map[int]bool{}
		for _, w := range ws {
			for ts := w.start; !ts.After(w.end); ts = ts.Add(Step) {
				i := d.SlotIndex(ts)
				if i < 0 || seen[i] {
					t.Fatalf("%s/%dh: timestamp %s out of day or repeated", tc.date, tc.hours, ts.UTC())
				}
				seen[i] = true
			}
		}
		if len(seen) != d.Slots() {
			t.Errorf("%s/%dh: %d slots covered, want %d", tc.date, tc.hours, len(seen), d.Slots())
		}
	}
}
