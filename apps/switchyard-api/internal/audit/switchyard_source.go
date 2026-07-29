package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SwitchyardSource exposes Switchyard's local audit tables as a single
// AuditEvent stream. It merges two on-prem tables:
//
//   - “audit_logs“ — request-level mutations captured by audit.Middleware.
//     Category: auth | deploy (derived from the action string).
//   - “deployment_lifecycle_events“ — CI/CD lifecycle signals (build,
//     deploy, rollback, promote). Category: always "deploy".
//
// Both tables live in the same Postgres instance as this service, so the
// aggregator treats this as a direct-DB source (no HTTP hop).
//
// We intentionally avoid introducing a cross-table UNION ALL view: the
// schemas are dissimilar enough that translating at the Go layer keeps
// the SQL simple and the mapper explicit.
type SwitchyardSource struct {
	db *sql.DB
}

// NewSwitchyardSource takes the shared *sql.DB already used by the
// service's repositories; we do not open a new connection pool.
func NewSwitchyardSource(db *sql.DB) *SwitchyardSource {
	return &SwitchyardSource{db: db}
}

// Name returns the Source identifier emitted on every event from this source.
func (s *SwitchyardSource) Name() string { return SourceSwitchyard }

// Fetch returns up to q.Limit events from both audit_logs and
// deployment_lifecycle_events, merged in timestamp-DESC order.
//
// The caller passes limit = page + 1 so we can signal has-more without a
// separate count query. We return the raw limited slice; the aggregator
// applies the final merge.
func (s *SwitchyardSource) Fetch(ctx context.Context, q Query) ([]AuditEvent, error) {
	// Skip if caller filtered this source out.
	if len(q.Sources) > 0 && !contains(q.Sources, SourceSwitchyard) {
		return nil, nil
	}
	// Skip if caller restricted categories and neither audit/deploy is asked.
	if len(q.Categories) > 0 &&
		!contains(q.Categories, CategoryAuth) &&
		!contains(q.Categories, CategoryDeploy) {
		return nil, nil
	}

	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	var all []AuditEvent

	// --- audit_logs (middleware-captured API mutations) --------------
	if categoryRequested(q, CategoryAuth, CategoryDeploy) {
		rows, err := s.fetchAuditLogs(ctx, q, limit)
		if err != nil {
			return nil, fmt.Errorf("switchyard: fetch audit_logs: %w", err)
		}
		all = append(all, rows...)
	}

	// --- deployment_lifecycle_events (CI/CD signals) -----------------
	if categoryRequested(q, CategoryDeploy) {
		rows, err := s.fetchLifecycle(ctx, q, limit)
		if err != nil {
			return nil, fmt.Errorf("switchyard: fetch lifecycle_events: %w", err)
		}
		all = append(all, rows...)
	}

	return all, nil
}

// fetchAuditLogs pulls the middleware-written rows. These already have a
// mature schema (actor_email, outcome, action, resource_*) that maps
// almost 1:1 onto AuditEvent; the only translation is Category.
//
// XC-2 Round 6: when q.TeamID is set, the WHERE clause restricts to rows
// whose project_id belongs to that team OR whose acting_on_behalf_of_team_id
// already names that team (the latter covers cross-tenant operator actions
// where the row was logged before any project linkage was established —
// e.g. tenant.enter / tenant.exit themselves). Mirrors AuditLogRepository.
// QueryByTeam from Round 5; see internal/db/audit_log_repository.go:53.
func (s *SwitchyardSource) fetchAuditLogs(ctx context.Context, q Query, limit int) ([]AuditEvent, error) {
	var conditions []string
	var args []interface{}
	argN := 1

	if q.Since != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp >= $%d", argN))
		args = append(args, *q.Since)
		argN++
	}
	if q.Until != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp <= $%d", argN))
		args = append(args, *q.Until)
		argN++
	}
	if q.Cursor != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp < $%d", argN))
		args = append(args, *q.Cursor)
		argN++
	}
	if q.Actor != "" {
		// actor_id is uuid; actor_email may carry sub-form. We match either.
		conditions = append(conditions, fmt.Sprintf("(actor_email = $%d OR actor_id::text = $%d)", argN, argN))
		args = append(args, q.Actor)
		argN++
	}
	if q.Target != "" {
		conditions = append(conditions, fmt.Sprintf("resource_id = $%d", argN))
		args = append(args, q.Target)
		argN++
	}
	if q.TeamID != nil {
		conditions = append(conditions, fmt.Sprintf(
			"(project_id IN (SELECT id FROM projects WHERE team_id = $%d) OR acting_on_behalf_of_team_id = $%d)",
			argN, argN,
		))
		args = append(args, *q.TeamID)
		argN++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	// ip_address is `inet`. Cast to text BEFORE COALESCE so Postgres doesn't
	// try to coerce the empty-string default back into inet — that path
	// fails with `invalid input syntax for type inet: ""` and was the cause
	// of /v1/audit returning a "switchyard upstream unavailable" banner.
	//
	// project_id and acting_on_behalf_of_team_id are nullable uuids; we
	// cast both to text and rely on sql.NullString below so a NULL surfaces
	// as the zero AuditEvent.projectID / nil ActingTeamID rather than a
	// scan error.
	//
	// #nosec G201 -- WHERE fragments above are compile-time constants with
	// $n placeholders; every caller value is bound via args. No user input
	// can reach the SQL text.
	query := fmt.Sprintf(`
		SELECT id, timestamp, COALESCE(actor_email,''), COALESCE(actor_id::text,''),
			action, resource_type, COALESCE(resource_id,''), COALESCE(resource_name,''),
			COALESCE(outcome,'success'), COALESCE(ip_address::text,''), COALESCE(user_agent,''),
			context, metadata,
			project_id::text, acting_on_behalf_of_team_id::text
		FROM audit_logs
		%s
		ORDER BY timestamp DESC
		LIMIT $%d
	`, where, argN)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []AuditEvent
	for rows.Next() {
		var (
			id, actorEmail, actorID, action, resourceType, resourceID, resourceName string
			outcome, ip, ua                                                         string
			ts                                                                      time.Time
			contextJSON, metadataJSON                                               []byte
			projectIDStr, actingTeamStr                                             sql.NullString
		)
		if err := rows.Scan(&id, &ts, &actorEmail, &actorID, &action,
			&resourceType, &resourceID, &resourceName,
			&outcome, &ip, &ua, &contextJSON, &metadataJSON,
			&projectIDStr, &actingTeamStr); err != nil {
			return nil, err
		}

		// Build a synthetic details payload that preserves the original
		// context/metadata plus the network fields the CSV exporter cares
		// about. We keep this untouched even if individual fields are
		// zero strings.
		details := map[string]any{
			"audit_log_id":  id,
			"resource_type": resourceType,
			"resource_name": resourceName,
			"ip_address":    ip,
			"user_agent":    ua,
		}
		if len(contextJSON) > 0 {
			var c map[string]any
			if json.Unmarshal(contextJSON, &c) == nil {
				details["context"] = c
			}
		}
		if len(metadataJSON) > 0 {
			var m map[string]any
			if json.Unmarshal(metadataJSON, &m) == nil {
				details["metadata"] = m
			}
		}
		raw, _ := json.Marshal(details)

		// auth-vs-deploy is keyed off the action verb, which the
		// middleware already prefixes ("login", "logout", "deploy",
		// "rollback", "create_project", "update_service", ...).
		category := CategoryAuth
		al := strings.ToLower(action)
		if strings.Contains(al, "deploy") || strings.Contains(al, "rollback") ||
			strings.Contains(al, "build") || strings.Contains(al, "scale") ||
			strings.Contains(al, "promote") {
			category = CategoryDeploy
		}

		// Per-category filter (we already know audit/deploy was asked,
		// but not which one — filter if only one was requested).
		if len(q.Categories) > 0 && !contains(q.Categories, category) {
			continue
		}

		actor := actorID
		if actor == "" {
			actor = actorEmail
		}

		ev := AuditEvent{
			Timestamp:  ts,
			Actor:      actor,
			ActorEmail: actorEmail,
			Source:     SourceSwitchyard,
			Category:   category,
			Action:     action,
			Target:     composeTarget(resourceType, resourceID),
			Outcome:    normalizeOutcome(outcome),
			Details:    raw,
		}
		// Stamp project + acting-team for downstream filtering and "as <tenant>"
		// badge enrichment. Both come from columns on audit_logs; either may
		// be NULL on legacy rows, in which case we leave the fields zero.
		if projectIDStr.Valid {
			if pid, perr := uuid.Parse(projectIDStr.String); perr == nil {
				ev.SetProjectID(pid)
			}
		}
		if actingTeamStr.Valid {
			if tid, terr := uuid.Parse(actingTeamStr.String); terr == nil {
				ev.ActingTeamID = &tid
			}
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// fetchLifecycle pulls deployment_lifecycle_events. These don't have a
// direct "actor" — they're emitted by CI (GitHub Actions webhook) — so
// we derive the actor from the metadata payload when present, else fall
// back to the literal "source" column (e.g. "github-actions").
//
// XC-2 Round 6: when q.TeamID is set, scope to rows whose project_id
// belongs to that team. lifecycle_events.project_id is nullable (CI events
// can land before the onboarding registry resolves the project), so a row
// with NULL project_id is excluded from the team-scoped view — operators
// looking at unbound CI traffic should drop out of acting-as mode.
func (s *SwitchyardSource) fetchLifecycle(ctx context.Context, q Query, limit int) ([]AuditEvent, error) {
	var conditions []string
	var args []interface{}
	argN := 1

	if q.Since != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argN))
		args = append(args, *q.Since)
		argN++
	}
	if q.Until != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argN))
		args = append(args, *q.Until)
		argN++
	}
	if q.Cursor != nil {
		conditions = append(conditions, fmt.Sprintf("created_at < $%d", argN))
		args = append(args, *q.Cursor)
		argN++
	}
	if q.Target != "" {
		// Target can match repo, commit SHA, or repo/commit composite.
		conditions = append(conditions,
			fmt.Sprintf("(repo_full_name = $%d OR commit_sha = $%d)", argN, argN))
		args = append(args, q.Target)
		argN++
	}
	// Actor filter against lifecycle events is lossy: the table has no
	// actor column. We honor it only if it happens to match the "source"
	// (e.g. "github-actions") so admins can filter by CI identity.
	if q.Actor != "" {
		conditions = append(conditions, fmt.Sprintf("source = $%d", argN))
		args = append(args, q.Actor)
		argN++
	}
	if q.TeamID != nil {
		conditions = append(conditions, fmt.Sprintf(
			"project_id IN (SELECT id FROM projects WHERE team_id = $%d)",
			argN,
		))
		args = append(args, *q.TeamID)
		argN++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	// #nosec G201 -- WHERE fragments above are compile-time constants with
	// $n placeholders; every caller value is bound via args. No user input
	// can reach the SQL text.
	query := fmt.Sprintf(`
		SELECT id, created_at, event_type, source, COALESCE(message,''),
			repo_full_name, commit_sha, branch, COALESCE(target_env,''), metadata,
			project_id::text
		FROM deployment_lifecycle_events
		%s
		ORDER BY created_at DESC
		LIMIT $%d
	`, where, argN)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []AuditEvent
	for rows.Next() {
		var (
			id, eventType, source, message, repo, sha, branch, env string
			ts                                                     time.Time
			metadataJSON                                           []byte
			projectIDStr                                           sql.NullString
		)
		if err := rows.Scan(&id, &ts, &eventType, &source, &message,
			&repo, &sha, &branch, &env, &metadataJSON,
			&projectIDStr); err != nil {
			return nil, err
		}

		details := map[string]any{
			"lifecycle_event_id": id,
			"source":             source,
			"branch":             branch,
			"commit_sha":         sha,
			"repo":               repo,
			"target_env":         env,
			"message":            message,
		}
		if len(metadataJSON) > 0 {
			var m map[string]any
			if json.Unmarshal(metadataJSON, &m) == nil {
				details["metadata"] = m
				// Pull actor out of metadata if CI recorded it.
				if sender, ok := m["sender"].(string); ok && sender != "" {
					details["actor_sender"] = sender
				}
			}
		}
		raw, _ := json.Marshal(details)

		// Outcome: derive from event_type (no explicit column).
		// "*_failed" → failure; "deploy_healthy" / "*_succeeded" → success;
		// everything else (deploy_started, image_pushed) → success
		// (they're forward-progress events, not outcomes per se).
		outcome := OutcomeSuccess
		if strings.Contains(strings.ToLower(eventType), "fail") {
			outcome = OutcomeFailure
		}

		// Best-effort actor from metadata.sender.
		actor := ""
		if len(metadataJSON) > 0 {
			var m map[string]any
			if json.Unmarshal(metadataJSON, &m) == nil {
				if s, ok := m["sender"].(string); ok {
					actor = s
				}
			}
		}
		if actor == "" {
			actor = source // "github-actions" / "argocd" / "enclii-cli"
		}

		ev := AuditEvent{
			Timestamp: ts,
			Actor:     actor,
			Source:    SourceSwitchyard,
			Category:  CategoryDeploy,
			Action:    eventType,
			Target:    fmt.Sprintf("%s@%s", repo, shortSHA(sha)),
			Outcome:   outcome,
			Details:   raw,
		}
		if projectIDStr.Valid {
			if pid, perr := uuid.Parse(projectIDStr.String); perr == nil {
				ev.SetProjectID(pid)
			}
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// -- helpers -----------------------------------------------------------

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func categoryRequested(q Query, allowed ...string) bool {
	if len(q.Categories) == 0 {
		return true
	}
	for _, a := range allowed {
		if contains(q.Categories, a) {
			return true
		}
	}
	return false
}

func composeTarget(resourceType, resourceID string) string {
	if resourceType == "" && resourceID == "" {
		return ""
	}
	if resourceID == "" {
		return resourceType
	}
	if resourceType == "" {
		return resourceID
	}
	return resourceType + ":" + resourceID
}

func normalizeOutcome(s string) string {
	switch strings.ToLower(s) {
	case "success", "succeeded", "applied", "completed":
		return OutcomeSuccess
	case "denied", "rejected", "forbidden":
		return OutcomeDenied
	case "", "unknown":
		// empty outcome in audit_logs means the middleware didn't
		// classify it — treat as success (2xx default path).
		return OutcomeSuccess
	default:
		return OutcomeFailure
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
