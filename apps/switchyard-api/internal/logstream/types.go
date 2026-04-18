package logstream

import (
	"time"
)

// Level is the normalized log-level enum exposed to the UI. Loki itself
// doesn't enforce levels — Fluent Bit tags each line with a detected
// level on ingest; we treat anything unknown as "info".
type Level string

const (
	LevelError Level = "error"
	LevelWarn  Level = "warn"
	LevelInfo  Level = "info"
	LevelDebug Level = "debug"
)

// AllLevels is the canonical enumeration for query-param validation.
var AllLevels = []Level{LevelError, LevelWarn, LevelInfo, LevelDebug}

// Entry is one log line as it appears to the UI. We deliberately emit
// `timestamp` as RFC3339Nano so the frontend can parse with `new Date()`
// without precision loss for sub-second ordering.
type Entry struct {
	Timestamp time.Time         `json:"timestamp"`
	Level     Level             `json:"level"`
	Pod       string            `json:"pod,omitempty"`
	Container string            `json:"container,omitempty"`
	Message   string            `json:"message"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// Query is the normalized input for the REST endpoint. Parsing and
// defaulting lives in handler.go so this struct stays agnostic of Gin.
type Query struct {
	Namespace string    // resolved server-side from project+env
	Service   string    // resolved server-side from service row
	Since     time.Time // inclusive lower bound
	Until     time.Time // exclusive upper bound
	Levels    []Level   // empty = all
	Search    string    // substring match (server-side grep via LogQL |=)
	Limit     int       // 1..2000, default 500
	Cursor    string    // opaque: serialized RFC3339Nano of last entry
}

// Response is the JSON shape for GET /v1/services/:id/logs.
type Response struct {
	Entries         []Entry `json:"entries"`
	NextCursor      string  `json:"next_cursor,omitempty"`
	ReachedLiveTail bool    `json:"reached_live_tail"`
}

// TailFrame is the JSON shape emitted on the WebSocket. A small
// Type-discriminator keeps the wire protocol self-describing without
// forcing the UI to rely on structural schemas.
type TailFrame struct {
	Type    string `json:"type"` // "entry" | "dropped" | "error" | "ping" | "bye"
	Entry   *Entry `json:"entry,omitempty"`
	Dropped int    `json:"dropped,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Defaults & bounds — exported so tests can reference without drift.
const (
	DefaultSince       = 1 * time.Hour
	DefaultLimit       = 500
	MaxLimit           = 2000
	MaxSearchLen       = 512
	TailSendBuffer     = 256
	TailHeartbeat      = 30 * time.Second
	TailIdleTimeout    = 60 * time.Second
	DefaultQueryBudget = 32 // queries per minute per caller
	DefaultQueryBurst  = 8
)
