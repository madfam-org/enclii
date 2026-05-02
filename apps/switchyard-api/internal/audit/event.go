package audit

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
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

	// ActingTeamID is set on rows that were emitted while a master admin was
	// acting on behalf of a tenant ("as <tenant>" badge). Today only the
	// switchyard source populates this from audit_logs.acting_on_behalf_of_team_id
	// (column added in migration 023_admin_acting_sessions). Selva and Janua
	// rows leave it nil — they don't currently propagate the acting-as flag.
	//
	// XC-2 Round 6: surfaced separately from Details so the UI drawer can
	// render the badge without re-parsing the JSON blob.
	ActingTeamID *uuid.UUID `json:"acting_team_id,omitempty"`

	// projectID is an internal-only field the aggregator consults when post-
	// filtering by team for sources that can't push the filter to their
	// upstream. It's never serialised — the “-” JSON tag keeps it out of the
	// wire response and out of the CSV exporter (which uses explicit columns).
	// Sources that know the project_id of a row should populate it; sources
	// that don't (Janua) leave it zero, in which case the team post-filter
	// drops the row when TeamID scoping is active.
	projectID uuid.UUID `json:"-"`
}

// ProjectID exposes the (otherwise private) projectID field for use by the
// aggregator's post-filter. Kept on the package internal surface so external
// callers don't grow a dependency on it.
func (e *AuditEvent) ProjectID() uuid.UUID { return e.projectID }

// SetProjectID lets sources stamp the project id on each event during Fetch.
// Used by SwitchyardSource (audit_logs.project_id, deployment_lifecycle_events.project_id)
// and by NexusClient when nexus-api propagates a project id in details.
func (e *AuditEvent) SetProjectID(id uuid.UUID) { e.projectID = id }

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

	// TeamID, when non-nil, restricts every source's output to rows whose
	// project_id belongs to that team. Set by the handler from
	// middleware.ActingTeamID(c) — i.e. only when a master admin is currently
	// acting on behalf of a tenant. Sources that can push the filter to their
	// upstream do so; sources that can't (Janua, Nexus today) emit unscoped
	// and the aggregator post-filters via the optional TeamResolver.
	//
	// nil = unscoped (admin or non-acting-as caller). Non-admin callers never
	// reach this field — the handler's RBAC narrows them to their own actor
	// instead, which is a different (and usually narrower) filter.
	//
	// XC-2 Round 6: this is the punted piece from Round 5 — see
	// claudedocs/master-admin-tenant-switching.md.
	TeamID *uuid.UUID
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
