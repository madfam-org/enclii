// Package waybill reports Roundhouse's own build durations to the usage
// pipeline.
//
// THIS IS A CROSS-CHECK, NOT THE METER OF RECORD.
// ==============================================
// Say it once here and again in the runbook, because the difference decides
// what anyone may do with these numbers.
//
// The meter of record is Weighbridge (`apps/waybill/internal/weighbridge`),
// which watches the runner pods the platform created and therefore sees every
// job whether or not the job cooperates. Roundhouse sees only the T3 builds
// that go through Roundhouse — a subset — and it reports a duration it
// computed about itself.
//
// So these events exist to be COMPARED against Weighbridge's, one build at a
// time, which is exactly what the `fragua.build-minute-truth` gate asks for:
// two independent counts of the same work, disagreeing visibly when one of
// them is wrong. They are tagged `source: roundhouse` for that purpose.
//
// NEVER SUM THE TWO STREAMS. Every T3 build that both observe appears in both,
// so a total across sources double counts. Any consumer that aggregates usage
// must filter on `source`.
package waybill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// emitTimeout bounds one delivery. This call sits at the end of the build
	// path; a slow biller must not become a slow build.
	emitTimeout = 5 * time.Second

	// ResourceType is the `resource_type` every Roundhouse usage event
	// carries. Distinct from Weighbridge's `ci_runner`: these are two
	// different artefacts (a build job and a runner pod), and collapsing them
	// onto one resource type would make the cross-check impossible to run.
	ResourceType = "roundhouse_build"

	// EventSource marks the stream. See the package comment: filter on this,
	// never sum across it.
	EventSource = "roundhouse"
)

// Client posts build.completed events to Waybill's internal ingest.
type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewClient returns nil when no base URL is configured, so `client != nil` is
// the single switch for the whole feature and a deployment without Waybill
// behaves exactly as it did before.
func NewClient(baseURL, apiKey string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: emitTimeout},
	}
}

// BuildCompleted is one finished build, as Roundhouse saw it.
type BuildCompleted struct {
	// JobID is the build id and the idempotency key. A property of the
	// artefact, minted once when the job was enqueued, so a retried callback
	// or a restarted worker re-reports the same build as a no-op rather than
	// as a second build.
	JobID     uuid.UUID
	ProjectID uuid.UUID
	ServiceID uuid.UUID
	// ServiceName is the human-readable name already carried on the job.
	ServiceName string
	// DurationSecs is BuildResult.DurationSecs — wall-clock time inside the
	// executor, which is what Roundhouse can honestly claim to have measured.
	// It is NOT slot time: the runner or pod that hosted the build was held
	// for longer, and only Weighbridge can see by how much.
	DurationSecs float64
	Success      bool
	FinishedAt   time.Time
}

// eventPayload is the wire shape of Waybill's EventRequest. Written out here
// rather than imported: roundhouse and waybill are separate Go modules and a
// shared struct would couple their release cycles for nine fields. Same
// reasoning the addon usage emitter in switchyard-api records for its own
// copy.
type eventPayload struct {
	EventType      string             `json:"event_type"`
	ProjectID      uuid.UUID          `json:"project_id"`
	ResourceType   string             `json:"resource_type"`
	ResourceID     uuid.UUID          `json:"resource_id"`
	ResourceName   string             `json:"resource_name,omitempty"`
	Metrics        map[string]float64 `json:"metrics"`
	Metadata       map[string]string  `json:"metadata,omitempty"`
	IdempotencyKey string             `json:"idempotency_key,omitempty"`
	Timestamp      *time.Time         `json:"timestamp,omitempty"`
}

// BuildPayload renders a completed build as Waybill's ingest request. Pure, so
// the exact envelope can be asserted without a server.
func BuildPayload(b BuildCompleted) (*eventPayload, error) {
	if b.JobID == uuid.Nil {
		return nil, fmt.Errorf("build has no job id")
	}
	if b.ProjectID == uuid.Nil {
		// Waybill's ingest requires a project. Refusing is better than filing
		// the build under a placeholder that somebody would be invoiced for.
		return nil, fmt.Errorf("build has no project id")
	}

	ts := b.FinishedAt
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	outcome := "failed"
	if b.Success {
		outcome = "succeeded"
	}

	return &eventPayload{
		// The same event type Weighbridge emits, on purpose: the two streams
		// are the same KIND of fact about the same work, told by two
		// witnesses. `source` in metadata is what tells them apart.
		EventType:    "build.completed",
		ProjectID:    b.ProjectID,
		ResourceType: ResourceType,
		ResourceID:   b.JobID,
		ResourceName: b.ServiceName,
		Metrics: map[string]float64{
			"duration_seconds": b.DurationSecs,
		},
		// NO slot_seconds. Roundhouse does not know how long the host was
		// held, and inventing the field would make this stream look like the
		// meter of record instead of a cross-check against it.
		Metadata: map[string]string{
			"source":     EventSource,
			"outcome":    outcome,
			"service_id": b.ServiceID.String(),
		},
		IdempotencyKey: b.JobID.String(),
		Timestamp:      &ts,
	}, nil
}

// ReportBuildCompleted delivers one event. Safe to call twice with the same
// job: Waybill's partial unique index refuses the second write and answers 2xx
// either way (migration 040).
func (c *Client) ReportBuildCompleted(ctx context.Context, b BuildCompleted) error {
	payload, err := BuildPayload(b)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal usage event: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, emitTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build usage event request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "roundhouse-waybill/1.0")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post usage event: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The status only. The body of an authenticated internal endpoint can
		// echo the request, and this error is logged.
		return fmt.Errorf("usage event rejected: HTTP %d", resp.StatusCode)
	}
	return nil
}
