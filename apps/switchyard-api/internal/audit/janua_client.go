package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// JanuaClient fetches session-lifecycle audit rows from Janua's existing
// “GET /api/v1/audit-logs/“ endpoint (see janua/apps/api/app/routers/v1/
// audit_logs.py). Janua already surfaces login / logout / MFA / role-change
// as “AuditLog“ rows keyed by “user_id“ + “action“, so we map that
// response into AuditEvent without touching Janua's schema.
//
// We use an admin-scoped machine token (ENCLII_JANUA_ADMIN_TOKEN) so that
// switchyard-api can query across actors when the end-user requesting
// /v1/audit is an admin. When the end-user is not an admin, the switchyard
// handler forces “actor = user.sub“ before calling us, so Janua gets a
// pre-narrowed query anyway — double-safe.
type JanuaClient struct {
	baseURL    string // e.g. https://api.janua.dev
	adminToken string
	httpClient *http.Client
	// emptyIfNoToken surfaces gracefully if the deployment hasn't yet
	// provisioned a Janua admin token. We return (nil, nil) in that case
	// so the UI shows a "Janua events coming soon" gap rather than a 500.
	emptyIfNoToken bool
}

// NewJanuaClient constructs a client. A zero-value baseURL or adminToken
// disables the client (Fetch returns no rows, no error) — this matches
// the P1.5 spec's soft-degrade behavior for the Janua wiring gap.
func NewJanuaClient(baseURL, adminToken string) *JanuaClient {
	return &JanuaClient{
		baseURL:    baseURL,
		adminToken: adminToken,
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
		},
		emptyIfNoToken: true,
	}
}

// Name returns the Source identifier emitted on every event from this source.
func (j *JanuaClient) Name() string { return SourceJanua }

// januaListResponse mirrors the shape at janua/apps/api/app/routers/v1/
// audit_logs.py::AuditLogListResponse. We only decode the fields we need.
type januaListResponse struct {
	Logs []struct {
		ID           string                 `json:"id"`
		Action       string                 `json:"action"`
		UserID       string                 `json:"user_id,omitempty"`
		UserEmail    string                 `json:"user_email,omitempty"`
		ResourceType string                 `json:"resource_type,omitempty"`
		ResourceID   string                 `json:"resource_id,omitempty"`
		Details      map[string]interface{} `json:"details,omitempty"`
		IPAddress    string                 `json:"ip_address,omitempty"`
		UserAgent    string                 `json:"user_agent,omitempty"`
		Timestamp    time.Time              `json:"timestamp"`
	} `json:"logs"`
	Total   int    `json:"total"`
	Cursor  string `json:"cursor,omitempty"`
	HasMore bool   `json:"has_more"`
}

// Fetch pages Janua's audit log and maps rows into AuditEvent. We honor
// the caller's limit/since/until/actor/cursor but leave category/source
// filters to the aggregator — Janua rows are always category=auth,
// source=janua, so there's nothing to do per-row.
func (j *JanuaClient) Fetch(ctx context.Context, q Query) ([]AuditEvent, error) {
	// Respect caller-side filter to avoid a wasted HTTP roundtrip.
	if len(q.Sources) > 0 && !contains(q.Sources, SourceJanua) {
		return nil, nil
	}
	if len(q.Categories) > 0 && !contains(q.Categories, CategoryAuth) {
		return nil, nil
	}
	if j.baseURL == "" || j.adminToken == "" {
		if j.emptyIfNoToken {
			return nil, nil
		}
		return nil, fmt.Errorf("janua client not configured (baseURL/adminToken empty)")
	}

	u, err := url.Parse(j.baseURL + "/api/v1/audit-logs/")
	if err != nil {
		return nil, fmt.Errorf("janua: parse url: %w", err)
	}
	qp := u.Query()
	if q.Limit > 0 {
		qp.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Since != nil {
		qp.Set("start_date", q.Since.UTC().Format(time.RFC3339))
	}
	if q.Until != nil {
		qp.Set("end_date", q.Until.UTC().Format(time.RFC3339))
	}
	if q.Actor != "" {
		qp.Set("actor", q.Actor)
	}
	if q.Cursor != nil {
		qp.Set("cursor", q.Cursor.UTC().Format(time.RFC3339))
	}
	u.RawQuery = qp.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("janua: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+j.adminToken)
	req.Header.Set("Accept", "application/json")

	resp, err := j.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("janua: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("janua: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var parsed januaListResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("janua: decode: %w", err)
	}

	out := make([]AuditEvent, 0, len(parsed.Logs))
	for _, row := range parsed.Logs {
		// Janua emits free-form ``action`` strings — login, logout, mfa_*,
		// role_change, token_refresh, etc. All of them are auth-category.
		outcome := OutcomeSuccess
		if row.Details != nil {
			if status, ok := row.Details["outcome"].(string); ok {
				outcome = normalizeOutcome(status)
			} else if success, ok := row.Details["success"].(bool); ok && !success {
				outcome = OutcomeFailure
			}
		}

		details := map[string]any{
			"janua_audit_log_id": row.ID,
			"ip_address":         row.IPAddress,
			"user_agent":         row.UserAgent,
			"resource_type":      row.ResourceType,
			"resource_id":        row.ResourceID,
		}
		if row.Details != nil {
			details["raw"] = row.Details
		}
		rawDetails, _ := json.Marshal(details)

		out = append(out, AuditEvent{
			Timestamp:  row.Timestamp,
			Actor:      row.UserID,
			ActorEmail: row.UserEmail,
			Source:     SourceJanua,
			Category:   CategoryAuth,
			Action:     row.Action,
			Target:     row.ResourceID,
			Outcome:    outcome,
			Details:    rawDetails,
		})
	}
	return out, nil
}
