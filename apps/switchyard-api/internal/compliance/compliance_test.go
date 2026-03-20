package compliance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	logger.SetOutput(&discardWriter{})
	return logger
}

// discardWriter silences log output during tests.
type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// sampleEvidence returns a fully-populated DeploymentEvidence for testing.
func sampleEvidence() *DeploymentEvidence {
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	return &DeploymentEvidence{
		EventType:         "deployment.completed",
		EventID:           "evt-abc-123",
		Timestamp:         now,
		ServiceName:       "switchyard-api",
		Environment:       "production",
		ProjectName:       "enclii",
		DeploymentID:      "dep-456",
		ReleaseVersion:    "v1.2.3",
		ImageURI:          "ghcr.io/madfam-org/switchyard-api:v1.2.3",
		GitSHA:            "abc123def456",
		GitRepo:           "madfam-org/enclii",
		CommitMessage:     "feat: add compliance exports",
		PRURL:             "https://github.com/madfam-org/enclii/pull/42",
		PRNumber:          42,
		ApprovedBy:        "reviewer@madfam.io",
		ApprovedAt:        now.Add(-1 * time.Hour),
		CIStatus:          "success",
		ChangeTicket:      "ENCLII-100",
		DeployedBy:        "deployer",
		DeployedByEmail:   "deployer@madfam.io",
		DeployedAt:        now,
		SBOM:              `{"bomFormat":"CycloneDX"}`,
		SBOMFormat:        "cyclonedx-json",
		ImageSignature:    "cosign-sig-data",
		SignatureVerified: true,
		ComplianceReceipt: "receipt-hash-xyz",
		ReceiptSignature:  "receipt-sig-xyz",
	}
}

// minimalEvidence returns a DeploymentEvidence with only the required fields.
func minimalEvidence() *DeploymentEvidence {
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	return &DeploymentEvidence{
		EventType:       "deployment.completed",
		EventID:         "evt-min-001",
		Timestamp:       now,
		ServiceName:     "status-page",
		Environment:     "staging",
		ProjectName:     "enclii",
		DeploymentID:    "dep-min-001",
		ReleaseVersion:  "v0.0.1",
		ImageURI:        "ghcr.io/madfam-org/status:v0.0.1",
		GitSHA:          "aaa111bbb222",
		GitRepo:         "madfam-org/enclii",
		DeployedBy:      "ci-bot",
		DeployedByEmail: "ci@madfam.io",
		DeployedAt:      now,
	}
}

// ---------------------------------------------------------------------------
// NewExporter
// ---------------------------------------------------------------------------

func TestNewExporter_DefaultConfig(t *testing.T) {
	cfg := &Config{Enabled: true}
	exp := NewExporter(cfg, newTestLogger())

	assert.True(t, exp.IsEnabled(), "exporter should be enabled")
	assert.Equal(t, 3, exp.maxRetries, "default maxRetries should be 3")
	assert.Equal(t, 2*time.Second, exp.retryDelay, "default retryDelay should be 2s")
}

func TestNewExporter_CustomConfig(t *testing.T) {
	cfg := &Config{
		Enabled:    false,
		MaxRetries: 5,
		RetryDelay: 500 * time.Millisecond,
	}
	exp := NewExporter(cfg, newTestLogger())

	assert.False(t, exp.IsEnabled(), "exporter should be disabled")
	assert.Equal(t, 5, exp.maxRetries)
	assert.Equal(t, 500*time.Millisecond, exp.retryDelay)
}

// ---------------------------------------------------------------------------
// SendWebhook - success path
// ---------------------------------------------------------------------------

func TestSendWebhook_Success(t *testing.T) {
	var receivedBody map[string]interface{}
	var receivedHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		err := json.NewDecoder(r.Body).Decode(&receivedBody)
		require.NoError(t, err, "server should decode request body")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	}))
	defer srv.Close()

	cfg := &Config{Enabled: true, MaxRetries: 1, RetryDelay: time.Millisecond}
	exp := NewExporter(cfg, newTestLogger())

	payload := map[string]string{"key": "value"}
	result := exp.SendWebhook(context.Background(), srv.URL, payload, "TestProvider")

	assert.True(t, result.Success, "webhook should succeed")
	assert.Equal(t, "TestProvider", result.Provider)
	assert.Equal(t, http.StatusOK, result.ResponseCode)
	assert.Equal(t, `{"status":"accepted"}`, result.ResponseBody)
	assert.Equal(t, 1, result.Attempts)
	assert.NoError(t, result.Error)

	// Verify headers
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
	assert.Equal(t, "Enclii-Switchyard/1.0", receivedHeaders.Get("User-Agent"))

	// Verify payload reached the server
	assert.Equal(t, "value", receivedBody["key"])
}

// ---------------------------------------------------------------------------
// SendWebhook - disabled / unconfigured
// ---------------------------------------------------------------------------

func TestSendWebhook_Disabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	exp := NewExporter(cfg, newTestLogger())

	result := exp.SendWebhook(context.Background(), "http://anything", nil, "Vanta")

	assert.True(t, result.Success, "disabled exporter should report success (no-op)")
	assert.Equal(t, 0, result.Attempts, "no HTTP attempts should be made when disabled")
}

func TestSendWebhook_EmptyURL(t *testing.T) {
	cfg := &Config{Enabled: true}
	exp := NewExporter(cfg, newTestLogger())

	result := exp.SendWebhook(context.Background(), "", nil, "Drata")

	assert.True(t, result.Success, "empty URL should report success (not configured)")
	assert.Equal(t, 0, result.Attempts)
}

// ---------------------------------------------------------------------------
// SendWebhook - 4xx errors (no retry)
// ---------------------------------------------------------------------------

func TestSendWebhook_ClientError_NoRetry(t *testing.T) {
	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad payload"}`))
	}))
	defer srv.Close()

	cfg := &Config{Enabled: true, MaxRetries: 3, RetryDelay: time.Millisecond}
	exp := NewExporter(cfg, newTestLogger())

	result := exp.SendWebhook(context.Background(), srv.URL, map[string]string{"a": "b"}, "Vanta")

	assert.False(t, result.Success, "4xx should fail")
	assert.Equal(t, http.StatusBadRequest, result.ResponseCode)
	assert.Contains(t, result.ResponseBody, "bad payload")
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount),
		"4xx errors must not trigger retries")
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "400")
}

// ---------------------------------------------------------------------------
// SendWebhook - 5xx errors (retry with backoff)
// ---------------------------------------------------------------------------

func TestSendWebhook_ServerError_RetriesExhausted(t *testing.T) {
	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	cfg := &Config{Enabled: true, MaxRetries: 3, RetryDelay: time.Millisecond}
	exp := NewExporter(cfg, newTestLogger())

	result := exp.SendWebhook(context.Background(), srv.URL, "payload", "Drata")

	assert.False(t, result.Success)
	assert.Equal(t, http.StatusInternalServerError, result.ResponseCode)
	assert.Equal(t, 3, result.Attempts, "should exhaust all retries on 5xx")
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount),
		"server should receive exactly maxRetries requests")
}

func TestSendWebhook_ServerError_EventualSuccess(t *testing.T) {
	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporarily unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := &Config{Enabled: true, MaxRetries: 5, RetryDelay: time.Millisecond}
	exp := NewExporter(cfg, newTestLogger())

	result := exp.SendWebhook(context.Background(), srv.URL, "payload", "Vanta")

	assert.True(t, result.Success, "should succeed after transient failures")
	assert.Equal(t, http.StatusOK, result.ResponseCode)
	assert.Equal(t, 3, result.Attempts, "should succeed on the third attempt")
}

// ---------------------------------------------------------------------------
// SendWebhook - network failure (connection refused)
// ---------------------------------------------------------------------------

func TestSendWebhook_NetworkFailure(t *testing.T) {
	cfg := &Config{Enabled: true, MaxRetries: 2, RetryDelay: time.Millisecond}
	exp := NewExporter(cfg, newTestLogger())

	// Use a URL with a port that is not listening
	result := exp.SendWebhook(context.Background(), "http://127.0.0.1:1", "payload", "Drata")

	assert.False(t, result.Success)
	assert.Equal(t, 2, result.Attempts, "should exhaust retries on connection failure")
	assert.Error(t, result.Error)
}

// ---------------------------------------------------------------------------
// FormatForVanta
// ---------------------------------------------------------------------------

func TestFormatForVanta_FullEvidence(t *testing.T) {
	ev := sampleEvidence()
	vanta := FormatForVanta(ev)

	// Event metadata
	assert.Equal(t, "deployment.completed", vanta.EventType)
	assert.Equal(t, ev.EventID, vanta.EventID)
	assert.Equal(t, "enclii-switchyard", vanta.Source)
	assert.Equal(t, "1.0", vanta.SourceVersion)

	// Resource
	assert.Equal(t, "deployment", vanta.Resource.Type)
	assert.Equal(t, ev.DeploymentID, vanta.Resource.ID)
	assert.Equal(t, ev.ServiceName, vanta.Resource.Name)
	assert.Equal(t, ev.Environment, vanta.Resource.Environment)

	// Evidence core fields
	assert.Equal(t, ev.ImageURI, vanta.Evidence.ImageURI)
	assert.Equal(t, ev.GitSHA, vanta.Evidence.GitSHA)
	assert.Equal(t, ev.ImageSignature, vanta.Evidence.ImageSignature)
	assert.True(t, vanta.Evidence.SignatureVerified)
	assert.Equal(t, ev.ComplianceReceipt, vanta.Evidence.ComplianceReceipt)

	// Code review (should be populated since PRURL is set)
	require.NotNil(t, vanta.Evidence.CodeReview, "code review should be present when PRURL is set")
	assert.Equal(t, ev.PRURL, vanta.Evidence.CodeReview.PRURL)
	assert.Equal(t, ev.PRNumber, vanta.Evidence.CodeReview.PRNumber)
	assert.Equal(t, ev.ApprovedBy, vanta.Evidence.CodeReview.ApprovedBy)
	assert.Equal(t, "success", vanta.Evidence.CodeReview.CIStatus)
	assert.True(t, vanta.Evidence.CodeReview.Verified)

	// SBOM (should be populated since SBOMFormat is set)
	require.NotNil(t, vanta.Evidence.SBOM, "SBOM should be present when SBOMFormat is set")
	assert.Equal(t, "cyclonedx-json", vanta.Evidence.SBOM.Format)
	assert.True(t, vanta.Evidence.SBOM.Generated)

	// Actor
	assert.Equal(t, ev.DeployedByEmail, vanta.Actor.Email)
	assert.Equal(t, ev.DeployedBy, vanta.Actor.Name)
}

func TestFormatForVanta_MinimalEvidence(t *testing.T) {
	ev := minimalEvidence()
	vanta := FormatForVanta(ev)

	// Metadata still populated
	assert.Equal(t, "deployment.completed", vanta.EventType)
	assert.Equal(t, ev.EventID, vanta.EventID)

	// Optional sections should be nil when source data is absent
	assert.Nil(t, vanta.Evidence.CodeReview, "code review should be nil without PRURL")
	assert.Nil(t, vanta.Evidence.SBOM, "SBOM should be nil without SBOMFormat")
}

// ---------------------------------------------------------------------------
// FormatForDrata
// ---------------------------------------------------------------------------

func TestFormatForDrata_FullEvidence(t *testing.T) {
	ev := sampleEvidence()
	drata := FormatForDrata(ev)

	// Event metadata
	assert.Equal(t, "deployment", drata.EventType)
	assert.Equal(t, ev.EventID, drata.EventID)
	assert.Equal(t, "enclii_switchyard", drata.Integration)

	// Entity
	assert.Equal(t, "deployment", drata.Entity.Type)
	assert.Equal(t, ev.DeploymentID, drata.Entity.ID)
	assert.Equal(t, ev.ServiceName, drata.Entity.Name)
	assert.Equal(t, ev.Environment, drata.Entity.Environment)
	assert.Equal(t, ev.ProjectName, drata.Entity.Tags["project"])
	assert.Equal(t, ev.ServiceName, drata.Entity.Tags["service"])

	// Attributes core
	assert.Equal(t, ev.ImageURI, drata.Attributes.ImageURI)
	assert.Equal(t, ev.GitSHA, drata.Attributes.CommitSHA)
	assert.Equal(t, ev.GitRepo, drata.Attributes.Repository)
	assert.Equal(t, "success", drata.Attributes.Status)

	// Change request
	require.NotNil(t, drata.Attributes.ChangeRequest, "change request should be present when ChangeTicket is set")
	assert.Equal(t, ev.ChangeTicket, drata.Attributes.ChangeRequest.TicketURL)
	assert.Equal(t, "approved", drata.Attributes.ChangeRequest.Status)

	// Pull request
	require.NotNil(t, drata.Attributes.PullRequest, "pull request should be present when PRURL is set")
	assert.Equal(t, ev.PRURL, drata.Attributes.PullRequest.URL)
	assert.Equal(t, ev.PRNumber, drata.Attributes.PullRequest.Number)
	assert.Equal(t, "merged", drata.Attributes.PullRequest.State)
	assert.Equal(t, ev.ApprovedBy, drata.Attributes.PullRequest.ApprovedBy)

	// Security
	require.NotNil(t, drata.Attributes.Security)
	assert.True(t, drata.Attributes.Security.ImageSigned)
	assert.True(t, drata.Attributes.Security.SignatureVerified)
	assert.True(t, drata.Attributes.Security.SBOMGenerated)
	assert.Equal(t, "cyclonedx-json", drata.Attributes.Security.SBOMFormat)

	// Evidence
	require.NotNil(t, drata.Attributes.Evidence, "evidence should be present when ComplianceReceipt is set")
	assert.Equal(t, "deployment_approval", drata.Attributes.Evidence.Type)
	assert.Equal(t, ev.ComplianceReceipt, drata.Attributes.Evidence.ComplianceReceipt)
	assert.Equal(t, ev.ReceiptSignature, drata.Attributes.Evidence.ReceiptSignature)

	// Personnel
	assert.Equal(t, ev.DeployedByEmail, drata.Personnel.Email)
	assert.Equal(t, ev.DeployedBy, drata.Personnel.Name)
}

func TestFormatForDrata_MinimalEvidence(t *testing.T) {
	ev := minimalEvidence()
	drata := FormatForDrata(ev)

	// Optional sections should be nil
	assert.Nil(t, drata.Attributes.ChangeRequest, "change request should be nil without ChangeTicket")
	assert.Nil(t, drata.Attributes.PullRequest, "pull request should be nil without PRURL")
	assert.Nil(t, drata.Attributes.Evidence, "evidence should be nil without ComplianceReceipt")

	// Security is always populated (with false/empty values for minimal evidence)
	require.NotNil(t, drata.Attributes.Security, "security section is always present")
	assert.False(t, drata.Attributes.Security.ImageSigned)
	assert.False(t, drata.Attributes.Security.SBOMGenerated)
}

// ---------------------------------------------------------------------------
// GetVantaControls
// ---------------------------------------------------------------------------

func TestGetVantaControls(t *testing.T) {
	tests := []struct {
		name     string
		evidence *DeploymentEvidence
		wantCC81 bool // Monitoring (always true)
		wantCC71 bool // Change management
		wantCC72 bool // Code review
		wantCC82 bool // Approval process
	}{
		{
			name:     "full evidence satisfies all controls",
			evidence: sampleEvidence(),
			wantCC81: true,
			wantCC71: true,
			wantCC72: true,
			wantCC82: true,
		},
		{
			name:     "minimal evidence satisfies only CC8.1",
			evidence: minimalEvidence(),
			wantCC81: true,
			wantCC71: false,
			wantCC72: false,
			wantCC82: false,
		},
		{
			name: "change ticket without PR approval",
			evidence: func() *DeploymentEvidence {
				ev := minimalEvidence()
				ev.ChangeTicket = "ENCLII-200"
				return ev
			}(),
			wantCC81: true,
			wantCC71: true,
			wantCC72: false,
			wantCC82: false,
		},
		{
			name: "PR without approver does not satisfy CC7.2 or CC8.2",
			evidence: func() *DeploymentEvidence {
				ev := minimalEvidence()
				ev.PRURL = "https://github.com/madfam-org/enclii/pull/10"
				return ev
			}(),
			wantCC81: true,
			wantCC71: false,
			wantCC72: false,
			wantCC82: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controls := GetVantaControls(tt.evidence)
			assert.Equal(t, tt.wantCC81, controls.CC81, "CC8.1 (monitoring)")
			assert.Equal(t, tt.wantCC71, controls.CC71, "CC7.1 (change management)")
			assert.Equal(t, tt.wantCC72, controls.CC72, "CC7.2 (code review)")
			assert.Equal(t, tt.wantCC82, controls.CC82, "CC8.2 (approval process)")
		})
	}
}

// ---------------------------------------------------------------------------
// GetDrataFrameworks
// ---------------------------------------------------------------------------

func TestGetDrataFrameworks(t *testing.T) {
	tests := []struct {
		name      string
		evidence  *DeploymentEvidence
		wantSOC2  []string
		wantISO   []string
		wantHIPAA []string
		wantPCI   []string
	}{
		{
			name:      "full evidence satisfies all framework controls",
			evidence:  sampleEvidence(),
			wantSOC2:  []string{"CC8.1", "CC7.1", "CC7.2", "CC8.2"},
			wantISO:   []string{"A.14.2.2", "A.14.2.1"},
			wantHIPAA: []string{"164.312(c)(1)"},
			wantPCI:   []string{"6.3.2"},
		},
		{
			name:      "minimal evidence satisfies only SOC2 CC8.1",
			evidence:  minimalEvidence(),
			wantSOC2:  []string{"CC8.1"},
			wantISO:   []string{},
			wantHIPAA: []string{},
			wantPCI:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fw := GetDrataFrameworks(tt.evidence)
			assert.Equal(t, tt.wantSOC2, fw.SOC2, "SOC2 controls")
			assert.Equal(t, tt.wantISO, fw.ISO27001, "ISO 27001 controls")
			assert.Equal(t, tt.wantHIPAA, fw.HIPAA, "HIPAA controls")
			assert.Equal(t, tt.wantPCI, fw.PCI, "PCI controls")
		})
	}
}

// ---------------------------------------------------------------------------
// ExportDeployment (integration of formatting + webhook send)
// ---------------------------------------------------------------------------

func TestExportDeployment_BothProviders(t *testing.T) {
	var vantaReceived, drataReceived bool

	vantaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vantaReceived = true

		// Verify the Vanta-specific payload structure
		var vantaPayload VantaEvent
		err := json.NewDecoder(r.Body).Decode(&vantaPayload)
		require.NoError(t, err)
		assert.Equal(t, "deployment.completed", vantaPayload.EventType)
		assert.Equal(t, "enclii-switchyard", vantaPayload.Source)

		w.WriteHeader(http.StatusOK)
	}))
	defer vantaSrv.Close()

	drataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drataReceived = true

		// Verify the Drata-specific payload structure
		var drataPayload DrataEvent
		err := json.NewDecoder(r.Body).Decode(&drataPayload)
		require.NoError(t, err)
		assert.Equal(t, "deployment", drataPayload.EventType)
		assert.Equal(t, "enclii_switchyard", drataPayload.Integration)

		w.WriteHeader(http.StatusOK)
	}))
	defer drataSrv.Close()

	cfg := &Config{Enabled: true, MaxRetries: 1, RetryDelay: time.Millisecond}
	exp := NewExporter(cfg, newTestLogger())

	results := exp.ExportDeployment(context.Background(), sampleEvidence(), vantaSrv.URL, drataSrv.URL)

	assert.True(t, vantaReceived, "Vanta server should have received a request")
	assert.True(t, drataReceived, "Drata server should have received a request")
	assert.Len(t, results, 2, "should have results for both providers")
	assert.True(t, results["vanta"].Success, "Vanta export should succeed")
	assert.True(t, results["drata"].Success, "Drata export should succeed")
}

func TestExportDeployment_SkipsUnconfiguredProviders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &Config{Enabled: true, MaxRetries: 1, RetryDelay: time.Millisecond}
	exp := NewExporter(cfg, newTestLogger())

	// Only Vanta URL provided, Drata is empty
	results := exp.ExportDeployment(context.Background(), sampleEvidence(), srv.URL, "")

	assert.Len(t, results, 1, "should only export to configured providers")
	assert.Contains(t, results, "vanta")
	assert.NotContains(t, results, "drata")
}
