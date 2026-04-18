package logstream

import (
	"fmt"
	"strings"
)

// BuildSelector constructs the LogQL stream selector for a service's
// pods. The namespace convention (`enclii-<projectSlug>-<envName>`) and
// pod prefix (`<service>-`) match the existing k8s.StreamLogs labels at
// internal/api/logs_handlers.go.
//
// Example output:
//
//	{namespace="enclii-madfam-production", pod=~"karafiel-api.*"}
//
// Search and level filters are added as LogQL pipeline stages so Loki
// does the work — avoids streaming everything to the API and grepping
// in-process.
//
// Loki's LogQL syntax for label selectors and line filters is stable
// (see grafana/loki docs). We intentionally don't parse arbitrary user
// LogQL — the surface is just `search` (substring) and `level` (set),
// which keeps this package's blast radius small.
func BuildQuery(namespace, service, search string, levels []Level) string {
	// Escape quotes in namespace/service — these come from DB rows, so
	// not user-authored, but we belt-and-brace regardless.
	selector := fmt.Sprintf(
		`{namespace=%q, pod=~%q}`,
		namespace,
		escapeRegex(service)+".*",
	)

	var filters []string

	// Level filter — if any are set, narrow via regex. Fluent Bit tags
	// lines with `level="info|warn|error|debug"` where possible; we
	// match on the log line because that's the only universally-present
	// signal across language stacks.
	if len(levels) > 0 && !allLevels(levels) {
		alts := make([]string, 0, len(levels))
		for _, l := range levels {
			alts = append(alts, string(l))
		}
		// Case-insensitive regex match. `(?i)` anchors the flag.
		filters = append(filters, fmt.Sprintf("|~ `(?i)\\b(%s)\\b`", strings.Join(alts, "|")))
	}

	// Substring search — LogQL `|=` is case-sensitive; we lowercase the
	// line with `| lower()` when user explicitly opts to... actually,
	// we keep case-sensitive so exact error strings match. If we want
	// case-insensitive later, switch to `|~` with `(?i)`.
	if s := strings.TrimSpace(search); s != "" {
		// Escape backticks in the search term to avoid LogQL parse errors.
		filters = append(filters, fmt.Sprintf("|= `%s`", strings.ReplaceAll(s, "`", "\\`")))
	}

	if len(filters) == 0 {
		return selector
	}
	return selector + " " + strings.Join(filters, " ")
}

// allLevels reports whether the set contains every canonical level —
// in which case the filter is redundant and we can skip emitting it.
func allLevels(levels []Level) bool {
	if len(levels) < len(AllLevels) {
		return false
	}
	seen := make(map[Level]bool, len(AllLevels))
	for _, l := range levels {
		seen[l] = true
	}
	for _, l := range AllLevels {
		if !seen[l] {
			return false
		}
	}
	return true
}

// escapeRegex escapes a value so it can be used inside a LogQL pod=~"..."
// regex without accidentally matching as metacharacters. Service names
// are `[a-z0-9-]+` in practice, but hyphens need no escape and dots
// (regex wildcards) shouldn't appear — still, defense in depth.
func escapeRegex(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '.', '+', '*', '?', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// DetectLevel inspects a log line and returns the best-effort level.
// We prefer labels-provided levels (when Fluent Bit tagged them), then
// fall back to string inspection. This mirrors the UI's old
// getLogLevelFromMessage but lives server-side so the wire protocol
// carries a pre-classified level.
func DetectLevel(labels map[string]string, line string) Level {
	if lvl, ok := labels["level"]; ok {
		switch strings.ToLower(lvl) {
		case "error", "err", "fatal", "panic", "critical":
			return LevelError
		case "warn", "warning":
			return LevelWarn
		case "debug", "trace":
			return LevelDebug
		case "info":
			return LevelInfo
		}
	}
	// Cheap substring scan on the first 128 bytes — enough for
	// structured-log prefixes like `[ERROR]` / `level=error`.
	prefix := line
	if len(prefix) > 128 {
		prefix = prefix[:128]
	}
	lower := strings.ToLower(prefix)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic"):
		return LevelError
	case strings.Contains(lower, "warn"):
		return LevelWarn
	case strings.Contains(lower, "debug") || strings.Contains(lower, "trace"):
		return LevelDebug
	}
	return LevelInfo
}
