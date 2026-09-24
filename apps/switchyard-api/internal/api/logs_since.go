package api

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// maxLogsSinceWindow caps the ?since= window on the log history endpoint, the
// way ?lines= is capped at 10000. Kubernetes only serves logs for the
// container's current (and previous) lifetime anyway, so a larger window would
// not return more; the cap keeps SinceSeconds a sane value.
const maxLogsSinceWindow = 30 * 24 * time.Hour

// parseLogsSince turns the optional ?since= query value into the
// PodLogOptions.SinceSeconds for a log read. An empty value means no window
// (nil). Two forms are accepted:
//
//   - an RFC3339 timestamp, e.g. 2026-09-24T10:00:00Z. This is what the CLI
//     sends: `enclii logs --since 24h` resolves the duration client-side and
//     sends now-24h (packages/cli/internal/client/api.go).
//   - a positive Go duration, e.g. 90m or 24h, for direct API callers.
//
// Anything else, a timestamp in the future, or a non-positive duration is an
// error for the caller to return as 400. Windows above maxLogsSinceWindow are
// clamped to it; sub-second windows round up to 1s, the Kubernetes minimum.
func parseLogsSince(raw string, now time.Time) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var window time.Duration
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		if ts.After(now) {
			return nil, fmt.Errorf("invalid since %q: timestamp is in the future", raw)
		}
		window = now.Sub(ts)
	} else if d, derr := time.ParseDuration(raw); derr == nil {
		if d <= 0 {
			return nil, fmt.Errorf("invalid since %q: duration must be positive", raw)
		}
		window = d
	} else {
		return nil, fmt.Errorf("invalid since %q: use an RFC3339 timestamp (2026-09-24T10:00:00Z) or a duration (5m, 1h, 24h)", raw)
	}

	if window > maxLogsSinceWindow {
		window = maxLogsSinceWindow
	}
	seconds := int64(math.Ceil(window.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return &seconds, nil
}
