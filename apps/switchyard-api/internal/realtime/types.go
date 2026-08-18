package realtime

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrNotReady signals that an addon exists but is not in a state that can
// accept realtime subscriptions (e.g. still provisioning). The WS handler maps
// it to a 409 before upgrading.
var ErrNotReady = errors.New("addon not ready for realtime")

// Channel is the single Postgres NOTIFY channel every enclii realtime trigger
// publishes on. One channel per addon (not per table) keeps the LISTEN
// connection count at one-per-addon regardless of how many tables are watched;
// the payload's schema/table fields let the hub route to subscribers.
const Channel = "enclii_realtime"

// EventType is the DML operation that produced a change.
type EventType string

const (
	EventInsert EventType = "INSERT"
	EventUpdate EventType = "UPDATE"
	EventDelete EventType = "DELETE"
)

// Frame-type discriminators for the client-facing WS protocol. A small type
// tag keeps the wire self-describing without forcing the UI onto a structural
// schema (same approach as logstream.TailFrame).
const (
	FrameChange     = "change"     // a row change
	FrameSubscribed = "subscribed" // ack of a subscribe request
	FrameError      = "error"      // a protocol / auth error
	FrameBye        = "bye"        // server closing the stream
	FramePing       = "ping"       // liveness (also sent as a WS control ping)
)

// Message-type discriminators for the client → server direction.
const (
	MsgSubscribe   = "subscribe"
	MsgUnsubscribe = "unsubscribe"
)

// Bounds & tuning. Exported so tests and the trigger SQL reference the same
// numbers without drift.
const (
	// NotifyPayloadCeiling is the byte budget the trigger keeps its JSON
	// envelope under. Postgres caps a NOTIFY payload at 8000 bytes and raises
	// an error on the *writer's* transaction if exceeded — so the trigger
	// truncates to key columns below this ceiling rather than risk failing an
	// application write. 7500 leaves headroom for the fixed envelope keys.
	NotifyPayloadCeiling = 7500

	// SendBuffer bounds a subscriber's outbound queue; on overflow the oldest
	// change is dropped (a live feed favors freshness over completeness).
	SendBuffer = 256

	// Heartbeat / idle mirror logstream's WS lifecycle.
	Heartbeat   = 30 * time.Second
	IdleTimeout = 60 * time.Second

	// MaxIdentLen bounds a schema/table identifier accepted by the trigger
	// install path. Postgres identifiers are capped at 63 bytes (NAMEDATALEN).
	MaxIdentLen = 63
)

// Filter is a single column-equality predicate applied server-side in the hub
// against the decoded record. Kept intentionally minimal in v1 so that many
// subscribers with different filters can share one trigger and one listener.
// A zero Filter (empty Column) matches everything.
type Filter struct {
	Column string `json:"column"`
	Value  string `json:"value"`
}

// IsZero reports whether the filter is the match-everything filter.
func (f Filter) IsZero() bool { return f.Column == "" }

// TableRef identifies a watched table. Schema defaults to "public" when empty
// (normalized by Normalize).
type TableRef struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
}

// Normalize fills in the default schema. It does NOT validate — call
// ValidateIdentifier for that.
func (t TableRef) Normalize() TableRef {
	if t.Schema == "" {
		t.Schema = "public"
	}
	return t
}

// Key is the routing key for a table: "schema.table". Assumes Normalize has
// run.
func (t TableRef) Key() string { return t.Schema + "." + t.Table }

// ClientMessage is a frame the client sends to the server.
type ClientMessage struct {
	Type   string  `json:"type"` // subscribe | unsubscribe
	Schema string  `json:"schema,omitempty"`
	Table  string  `json:"table,omitempty"`
	Filter *Filter `json:"filter,omitempty"`
}

// Ref returns the normalized TableRef named by a client message.
func (m ClientMessage) Ref() TableRef {
	return TableRef{Schema: m.Schema, Table: m.Table}.Normalize()
}

// Change is the decoded NOTIFY payload — the shape the trigger emits and the
// hub routes on. It is also (minus internal fields) what the client receives
// inside a change frame.
type Change struct {
	Event     EventType       `json:"event"`
	Schema    string          `json:"schema"`
	Table     string          `json:"table"`
	Record    json.RawMessage `json:"record,omitempty"`
	OldRecord json.RawMessage `json:"old_record,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
	CommitTS  string          `json:"commit_ts,omitempty"`
}

// Ref is the routing key of the change.
func (c Change) Ref() TableRef {
	return TableRef{Schema: c.Schema, Table: c.Table}.Normalize()
}

// ServerFrame is what the hub writes to a WS client. Type discriminates; the
// change payload is inlined so the client sees a flat object.
type ServerFrame struct {
	Type string `json:"type"` // change | subscribed | error | bye | ping

	// change
	Event     EventType       `json:"event,omitempty"`
	Schema    string          `json:"schema,omitempty"`
	Table     string          `json:"table,omitempty"`
	Record    json.RawMessage `json:"record,omitempty"`
	OldRecord json.RawMessage `json:"old_record,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
	CommitTS  string          `json:"commit_ts,omitempty"`

	// error
	Error string `json:"error,omitempty"`
}

// changeFrame builds a change ServerFrame from a decoded Change.
func changeFrame(ch Change) ServerFrame {
	return ServerFrame{
		Type:      FrameChange,
		Event:     ch.Event,
		Schema:    ch.Schema,
		Table:     ch.Table,
		Record:    ch.Record,
		OldRecord: ch.OldRecord,
		Truncated: ch.Truncated,
		CommitTS:  ch.CommitTS,
	}
}
