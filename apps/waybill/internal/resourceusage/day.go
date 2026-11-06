// Package resourceusage captures per-workload CPU and memory usage and
// requests from Prometheus into Waybill's daily ledger (resource_daily,
// node_daily, resource_daily_captures; RFC 0049 Phase 2).
//
// WHY IT EXISTS
// =============
// Prometheus keeps 7 to 9 days. Waybill meters builds, addons and CI minutes,
// but until this package nothing kept what each workload actually used and
// requested, so per-workload history was lost every day and digital COGS
// (RFC 0050) had nothing to allocate. Once a civil day in
// America/Mexico_City has ended, the aggregator reads it from Prometheus at
// 5-minute resolution, aggregates it per workload, container and node, and
// upserts it. Usage and requests only: no money, no allocation.
//
// HONESTY RULES
// =============
//   - Missing is missing. A value nobody measured is NULL, never 0, and
//     coverage says how much of the day each row had data for. A day with no
//     samples at all is not recorded as captured.
//   - A re-capture never degrades. A row is replaced only when the new read
//     saw at least as many samples, so a rerun after Prometheus dropped part
//     of a day keeps the earlier, fuller row.
//   - CI runner pods are not split per workload (each runner is its own
//     short-lived owner), but their namespace keeps one row per node and
//     container, so node totals still reconcile.
package resourceusage

import (
	"fmt"
	"time"

	// The civil day is the ledger's key. Embed the zone database so a missing
	// tzdata package in an image can never shift every day boundary.
	_ "time/tzdata"
)

// Step is the sample resolution: one value per 5 minutes, the resolution
// RFC 0049's rightsizing method reads p95 and max from.
const Step = 5 * time.Minute

// Day is one civil day in the ledger's time zone.
type Day struct {
	// Date is the civil date, YYYY-MM-DD.
	Date string
	// Start is local midnight; End is the next local midnight. Mexico has
	// had no DST since October 2022, so End-Start is 24h for every day since,
	// but the arithmetic never assumes it.
	Start time.Time
	End   time.Time
}

// DayOf returns the civil day in loc that contains t.
func DayOf(t time.Time, loc *time.Location) Day {
	lt := t.In(loc)
	start := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
	end := time.Date(lt.Year(), lt.Month(), lt.Day()+1, 0, 0, 0, 0, loc)
	return Day{Date: start.Format("2006-01-02"), Start: start, End: end}
}

// ParseDay returns the civil day named by a YYYY-MM-DD date in loc.
func ParseDay(date string, loc *time.Location) (Day, error) {
	t, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return Day{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", date)
	}
	return DayOf(t, loc), nil
}

// Slots is the number of 5-minute samples in the day. Slot i covers
// (Start + i*Step, Start + (i+1)*Step]; its sample is evaluated at the end of
// that interval, so the day's samples are at Start+Step ... End.
func (d Day) Slots() int {
	return int(d.End.Sub(d.Start) / Step)
}

// SlotIndex maps a sample timestamp to its slot, or -1 when the timestamp is
// outside the day or not on the 5-minute grid.
func (d Day) SlotIndex(ts time.Time) int {
	off := ts.Sub(d.Start)
	if off <= 0 || off > d.End.Sub(d.Start) || off%Step != 0 {
		return -1
	}
	return int(off/Step) - 1
}

// CompleteDays lists, oldest first, the civil days in loc that ended at
// least settle before now, going back lookback days from yesterday. settle
// leaves room for the last scrapes and 5-minute rate windows of the day.
func CompleteDays(now time.Time, loc *time.Location, lookback int, settle time.Duration) []Day {
	if lookback < 1 {
		return nil
	}
	// Walk back from today to the newest day that ended settle ago.
	latest := DayOf(now, loc)
	for latest.End.Add(settle).After(now) {
		latest = DayOf(latest.Start.Add(-time.Hour), loc)
	}
	days := make([]Day, 0, lookback)
	d := latest
	for i := 0; i < lookback; i++ {
		days = append(days, d)
		d = DayOf(d.Start.Add(-time.Hour), loc)
	}
	// Oldest first: the oldest day is the next one Prometheus will drop.
	for i, j := 0, len(days)-1; i < j; i, j = i+1, j-1 {
		days[i], days[j] = days[j], days[i]
	}
	return days
}
