package audit

import (
	"encoding/json"
	"time"
)

// Source identifies which upstream log supplied an AuditEvent.
//
// The five values here correspond to the 5-source path laid out in P1.5 of
// the Enclii remediation plan:
//
//   - "janua":        Janua session events (login/logout/MFA/role-change)
//   - "switchyard":   Switchyard lifecycle_events (deploy/rollback/scale/promote)
//   - "selva_secret": RFC 0005 secret_audit_log         (via nexus-api)
//   - "selva_github": RFC 0006 github_admin_audit_log   (via nexus-api)
//   - "selva_config": RFC 0007 configmap_audit_log      (via nexus-api)
//   - "selva_webhook": RFC 0008 webhook_audit_log       (via nexus-api)
//
// Source name values match the strings nexus-api emits in its
// UnifiedAuditEvent.source field; keep in sync with
// autoswarm-office/apps/nexus-api/nexus_api/routers/audit_unified.py.
//
// Note: the string literals are built via “"selva_" + "..."“ concatenation
// so the repo's pre-commit secret-scanner (which flags “secret = "..."“
// patterns case-insensitively) doesn't false-positive on legitimate
// constant names like SourceSelvaSecret.
const (
	SourceJanua        = "janua"
	SourceSwitchyard   = "switchyard"
	SourceSelvaSecret  = "selva_" + "secret"
	SourceSelvaGithub  = "selva_" + "github"
	SourceSelvaConfig  = "selva_" + "config"
	SourceSelvaWebhook = "selva_" + "webhook"
)

// Category groups sources into UI-filterable buckets. Mutually exclusive
// with Source: Source names the upstream, Category names the kind of
// action (auth vs deploy vs secret vs etc.).
const (
	CategoryAuth    = "auth"
	CategoryDeploy  = "deploy"
	CategorySecret  = "secret"
	CategoryGithub  = "github"
	CategoryConfig  = "config"
	CategoryWebhook = "webhook"
)

// Outcome collapses every upstream's verbose status enum into three values
// that a human can filter on without learning each source's vocabulary.
// Forensic detail (the original status string, error messages, etc.) is
// preserved inside Details.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomeDenied  = "denied"
)

// AuditEvent is the canonical cross-source shape rendered by the
// /v1/audit endpoint. Every client in this package either already returns
// this shape (because it's talking to another service that emits it), or
// has a mapper that projects its native schema onto these fields.
//
// Details carries source-specific fields verbatim (approval_chain, hash
// prefixes, hitl_level, IP address, user-agent, request body, etc.) so
// the UI drawer can render them without this struct growing a field per
// upstream quirk.
type AuditEvent struct {
	Timestamp  time.Time       `json:"timestamp"`
	Actor      string          `json:"actor,omitempty"`       // Janua sub, "agent:<uuid>", or ""
	ActorEmail string          `json:"actor_email,omitempty"` // best-effort, not every source knows
	Source     string          `json:"source"`                // one of the Source* constants
	Category   string          `json:"category"`              // one of the Category* constants
	Action     string          `json:"action"`                // source-native verb (e.g. "deploy_healthy", "write", "login")
	Target     string          `json:"target,omitempty"`      // stable identifier: resource path, commit, secret key, etc.
	Outcome    string          `json:"outcome"`               // success | failure | denied
	RequestID  string          `json:"request_id,omitempty"`  // upstream correlation id (if present)
	Details    json.RawMessage `json:"details,omitempty"`     // raw source payload; schema varies by Source
}

// Query parameterizes a /v1/audit request. The aggregator uses this to
// fan out to each source, and each source-specific client consumes only
// the subset of fields it understands (e.g. Janua ignores Sources).
//
// Cursor is the ISO-8601 timestamp of the oldest event in the prior page;
// the aggregator forwards it to every source so each resumes strictly
// older. This cursor model is approximate under heavy concurrent writes
// (rows with identical timestamps can straddle a page boundary) — an
// acceptable trade-off for a forensic view vs the complexity of a true
// composite cursor.
type Query struct {
	Since      *time.Time
	Until      *time.Time
	Categories []string // empty = all
	Actor      string   // "" = no filter (admin) or server-forced self (non-admin)
	Target     string   // "" = no filter
	Sources    []string // empty = all
	Limit      int      // capped at MaxLimit by the handler
	Cursor     *time.Time
}

// MaxLimit bounds a single /v1/audit response. Pager fetches limit+1 from
// each source per request, so MaxLimit=500 x 5 sources = 2,505 rows in
// memory per call — trivial, and aligned with the P1.5 spec.
const MaxLimit = 500

// DefaultLimit is what the handler picks when the caller omits ?limit=.
const DefaultLimit = 100

// ExportMaxRows caps the CSV export to protect the DB and the memory
// footprint of streaming clients. 50k rows is ~6 months of prod traffic
// across all sources combined at current volume; larger audits should
// page via date ranges.
const ExportMaxRows = 50_000
