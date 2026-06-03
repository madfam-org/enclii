package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NexusClient fetches the four Selva RFC ledgers via selva-office's
// “GET /api/v1/audit/unified“ endpoint. That endpoint already returns
// the canonical AuditEvent shape (see nexus_api/routers/audit_unified.py),
// so this client is a thin HTTP wrapper + filter bridge.
//
// We authenticate with the shared worker token (WORKER_API_TOKEN on
// selva-office's side; ENCLII_NEXUS_API_TOKEN on ours) — the same
// secret already trusted by nexus-api's get_current_user to mint a
// service role.
type NexusClient struct {
	baseURL    string // e.g. https://api.selva.internal
	apiToken   string
	httpClient *http.Client
}

// NewNexusClient constructs a client. Like JanuaClient, a zero-value
// baseURL disables the client gracefully (returns nil rows).
func NewNexusClient(baseURL, apiToken string) *NexusClient {
	return &NexusClient{
		baseURL:  baseURL,
		apiToken: apiToken,
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
		},
	}
}

// Name returns "nexus" here for logging, though individual events carry
// one of SourceSelva* in their Source field (set upstream by nexus-api).
func (n *NexusClient) Name() string { return "nexus" }

// unifiedResponse mirrors UnifiedAuditListResponse. Details comes through
// as arbitrary JSON, so we keep it as json.RawMessage and pass through.
type unifiedResponse struct {
	Events []struct {
		Timestamp  time.Time       `json:"timestamp"`
		Actor      string          `json:"actor,omitempty"`
		ActorEmail string          `json:"actor_email,omitempty"`
		Source     string          `json:"source"`
		Category   string          `json:"category"`
		Action     string          `json:"action"`
		Target     string          `json:"target,omitempty"`
		Outcome    string          `json:"outcome"`
		RequestID  string          `json:"request_id,omitempty"`
		Details    json.RawMessage `json:"details,omitempty"`
	} `json:"events"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Fetch forwards the aggregator Query to /api/v1/audit/unified and maps
// the response to []AuditEvent. We honor the Sources filter by only
// forwarding selva_* values (nexus only knows about those).
func (n *NexusClient) Fetch(ctx context.Context, q Query) ([]AuditEvent, error) {
	// Compute the selva-prefixed source subset the caller wants.
	selvaSources := selvaSourcesFrom(q.Sources)
	if len(q.Sources) > 0 && len(selvaSources) == 0 {
		// Caller asked only for non-selva sources; skip this hop.
		return nil, nil
	}
	// Skip if the category filter excludes everything this source can emit.
	if len(q.Categories) > 0 && !anyMatch(q.Categories, []string{
		CategorySecret, CategoryGithub, CategoryConfig, CategoryWebhook,
	}) {
		return nil, nil
	}
	if n.baseURL == "" || n.apiToken == "" {
		return nil, nil
	}

	u, err := url.Parse(strings.TrimRight(n.baseURL, "/") + "/api/v1/audit/unified/")
	if err != nil {
		return nil, fmt.Errorf("nexus: parse url: %w", err)
	}
	qp := u.Query()
	if q.Limit > 0 {
		qp.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Since != nil {
		qp.Set("since", q.Since.UTC().Format(time.RFC3339))
	}
	if q.Until != nil {
		qp.Set("until", q.Until.UTC().Format(time.RFC3339))
	}
	if q.Cursor != nil {
		qp.Set("cursor", q.Cursor.UTC().Format(time.RFC3339))
	}
	if q.Actor != "" {
		qp.Set("actor", q.Actor)
	}
	for _, src := range selvaSources {
		qp.Add("source", src)
	}
	u.RawQuery = qp.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("nexus: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+n.apiToken)
	req.Header.Set("Accept", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nexus: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("nexus: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var parsed unifiedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("nexus: decode: %w", err)
	}

	out := make([]AuditEvent, 0, len(parsed.Events))
	for _, e := range parsed.Events {
		// Honor category filter on the aggregator side too, in case
		// nexus ever broadens what it returns.
		if len(q.Categories) > 0 && !contains(q.Categories, e.Category) {
			continue
		}
		out = append(out, AuditEvent{
			Timestamp:  e.Timestamp,
			Actor:      e.Actor,
			ActorEmail: e.ActorEmail,
			Source:     e.Source,
			Category:   e.Category,
			Action:     e.Action,
			Target:     e.Target,
			Outcome:    e.Outcome,
			RequestID:  e.RequestID,
			Details:    e.Details,
		})
	}
	return out, nil
}

// selvaSourcesFrom returns the subset of sources that nexus-api knows
// about. Empty input means "all four".
func selvaSourcesFrom(sources []string) []string {
	if len(sources) == 0 {
		return []string{
			SourceSelvaSecret,
			SourceSelvaGithub,
			SourceSelvaConfig,
			SourceSelvaWebhook,
		}
	}
	var out []string
	for _, s := range sources {
		switch s {
		case SourceSelvaSecret, SourceSelvaGithub,
			SourceSelvaConfig, SourceSelvaWebhook:
			out = append(out, s)
		}
	}
	return out
}

func anyMatch(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}
