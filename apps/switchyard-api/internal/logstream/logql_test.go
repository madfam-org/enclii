package logstream

import (
	"strings"
	"testing"
)

// TestBuildQuery_BaseSelector asserts the stream selector uses the
// namespace and pod-regex convention Fluent Bit writes. If this
// changes, every existing log query breaks — so we pin it.
func TestBuildQuery_BaseSelector(t *testing.T) {
	got := BuildQuery("enclii-madfam-production", "karafiel-api", "", nil)
	want := `{namespace="enclii-madfam-production", pod=~"karafiel-api.*"}`
	if got != want {
		t.Fatalf("BuildQuery base:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestBuildQuery_SearchAppendsLineFilter(t *testing.T) {
	got := BuildQuery("ns", "svc", "timeout", nil)
	if !strings.Contains(got, "|= `timeout`") {
		t.Errorf("expected line filter |= `timeout` in %q", got)
	}
}

func TestBuildQuery_SearchEscapesBackticks(t *testing.T) {
	got := BuildQuery("ns", "svc", "foo`bar", nil)
	if !strings.Contains(got, "foo\\`bar") {
		t.Errorf("backtick should be escaped in %q", got)
	}
}

func TestBuildQuery_LevelFilterSingle(t *testing.T) {
	got := BuildQuery("ns", "svc", "", []Level{LevelError})
	if !strings.Contains(got, "(?i)") {
		t.Errorf("expected case-insensitive regex in %q", got)
	}
	if !strings.Contains(got, "error") {
		t.Errorf("expected 'error' level in regex in %q", got)
	}
}

func TestBuildQuery_LevelFilterMultiple(t *testing.T) {
	got := BuildQuery("ns", "svc", "", []Level{LevelError, LevelWarn})
	if !strings.Contains(got, "error|warn") && !strings.Contains(got, "warn|error") {
		t.Errorf("expected error|warn alternation in %q", got)
	}
}

// When every level is selected the filter is redundant and should be
// omitted to let Loki's index do less work.
func TestBuildQuery_AllLevelsOmitsFilter(t *testing.T) {
	got := BuildQuery("ns", "svc", "", []Level{LevelError, LevelWarn, LevelInfo, LevelDebug})
	if strings.Contains(got, "|~") {
		t.Errorf("all-levels should produce no regex filter, got %q", got)
	}
}

func TestBuildQuery_ServiceWithSpecialCharsIsEscaped(t *testing.T) {
	// Service names shouldn't have regex metachars, but defense in
	// depth — confirm the escape helper is wired up. Sprintf uses %q
	// which additionally quotes backslashes for the wire, so we look
	// for the double-backslash form.
	got := BuildQuery("ns", "svc.with.dots", "", nil)
	if !strings.Contains(got, `svc\\.with\\.dots`) {
		t.Errorf("dots should be escaped in %q", got)
	}
}

func TestDetectLevel_FromLabels(t *testing.T) {
	cases := []struct {
		label string
		want  Level
	}{
		{"error", LevelError},
		{"ERR", LevelError},
		{"fatal", LevelError},
		{"warning", LevelWarn},
		{"info", LevelInfo},
		{"debug", LevelDebug},
	}
	for _, c := range cases {
		got := DetectLevel(map[string]string{"level": c.label}, "")
		if got != c.want {
			t.Errorf("DetectLevel(label=%q)=%v, want %v", c.label, got, c.want)
		}
	}
}

func TestDetectLevel_FromMessage(t *testing.T) {
	cases := []struct {
		msg  string
		want Level
	}{
		{"[ERROR] failed", LevelError},
		{"level=warn something", LevelWarn},
		{"TRACE: ok", LevelDebug},
		{"nothing special", LevelInfo},
	}
	for _, c := range cases {
		got := DetectLevel(nil, c.msg)
		if got != c.want {
			t.Errorf("DetectLevel(%q)=%v, want %v", c.msg, got, c.want)
		}
	}
}
