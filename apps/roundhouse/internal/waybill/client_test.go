package waybill

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleBuild() BuildCompleted {
	return BuildCompleted{
		JobID:        uuid.MustParse("44444444-4444-4444-8444-444444444444"),
		ProjectID:    uuid.MustParse("55555555-5555-4555-8555-555555555555"),
		ServiceID:    uuid.MustParse("66666666-6666-4666-8666-666666666666"),
		ServiceName:  "switchyard-api",
		DurationSecs: 123.5,
		Success:      true,
		FinishedAt:   time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
}

func TestNewClient_UnsetBaseURLDisablesTheStream(t *testing.T) {
	assert.Nil(t, NewClient("", "key"))
	assert.Nil(t, NewClient("  ", "key"))
	assert.NotNil(t, NewClient("http://waybill", ""))
}

// This stream must be distinguishable from Weighbridge's at a glance, because
// summing the two double counts every T3 build.
func TestBuildPayload_IsTaggedAsACrossCheck(t *testing.T) {
	p, err := BuildPayload(sampleBuild())
	require.NoError(t, err)

	assert.Equal(t, EventSource, p.Metadata["source"])
	assert.Equal(t, "roundhouse", p.Metadata["source"])
	// A different resource type from Weighbridge's `ci_runner`: a build job
	// and a runner pod are two artefacts, and collapsing them would make the
	// cross-check impossible to run.
	assert.Equal(t, "roundhouse_build", p.ResourceType)
	assert.Equal(t, "build.completed", p.EventType)
}

// Idempotency key is the build id — a property of the artefact, minted once
// when the job was enqueued.
func TestBuildPayload_IdempotencyKeyIsTheBuildID(t *testing.T) {
	b := sampleBuild()
	p, err := BuildPayload(b)
	require.NoError(t, err)
	assert.Equal(t, b.JobID.String(), p.IdempotencyKey)
	assert.Equal(t, b.JobID, p.ResourceID)
}

// Roundhouse cannot see how long the host was held. Emitting slot_seconds
// would make this stream look like the meter of record.
func TestBuildPayload_CarriesDurationOnly(t *testing.T) {
	p, err := BuildPayload(sampleBuild())
	require.NoError(t, err)

	keys := make([]string, 0, len(p.Metrics))
	for k := range p.Metrics {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{"duration_seconds"}, keys)
	assert.InDelta(t, 123.5, p.Metrics["duration_seconds"], 1e-9)
}

// A failed build burns the same minutes as a successful one. Omitting failures
// would make this stream disagree with Weighbridge on every failure and look
// like a metering bug.
func TestBuildPayload_FailedBuildIsStillReported(t *testing.T) {
	b := sampleBuild()
	b.Success = false
	p, err := BuildPayload(b)
	require.NoError(t, err)
	assert.Equal(t, "failed", p.Metadata["outcome"])
	assert.InDelta(t, 123.5, p.Metrics["duration_seconds"], 1e-9)
}

// Waybill's ingest requires a project. Refusing beats filing the build under a
// placeholder somebody would eventually be invoiced for.
func TestBuildPayload_RefusesUnattributedBuilds(t *testing.T) {
	b := sampleBuild()
	b.ProjectID = uuid.Nil
	_, err := BuildPayload(b)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project id")

	b = sampleBuild()
	b.JobID = uuid.Nil
	_, err = BuildPayload(b)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "job id")
}

func TestReportBuildCompleted_SendsTheDocumentedRequest(t *testing.T) {
	var (
		gotPath string
		gotKey  string
		gotBody []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-API-Key")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	require.NotNil(t, c)
	require.NoError(t, c.ReportBuildCompleted(context.Background(), sampleBuild()))

	assert.Equal(t, "/internal/events", gotPath)
	// Same auth header and same idempotency field as every other internal
	// producer: one dialect at one endpoint, or the dedup index stops
	// deduping.
	assert.Equal(t, "test-key", gotKey)

	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(gotBody, &envelope))
	assert.Contains(t, envelope, "idempotency_key")
	assert.Contains(t, envelope, "timestamp")
}

func TestReportBuildCompleted_NonSuccessIsAnErrorAndLeaksNoBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("echoed-request-body"))
	}))
	defer srv.Close()

	err := NewClient(srv.URL, "").ReportBuildCompleted(context.Background(), sampleBuild())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
	assert.NotContains(t, err.Error(), "echoed-request-body")
}
