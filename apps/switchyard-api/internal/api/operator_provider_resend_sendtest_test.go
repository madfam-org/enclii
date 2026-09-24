package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/notifications"
)

const sendTestOperation = "providers.resend.send-test-apply"

// resendRecorder stands in for the Resend API and records every request.
type resendRecorder struct {
	mu       sync.Mutex
	payloads []map[string]any
}

func (r *resendRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	r.mu.Lock()
	r.payloads = append(r.payloads, payload)
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":"email_test_1"}`)),
		Request:    req,
	}, nil
}

func (r *resendRecorder) sent() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.payloads...)
}

// sendTestHandler wires a configured email service whose Resend client talks
// to a recorder instead of the network.
func sendTestHandler(t *testing.T) (*Handler, *resendRecorder) {
	t.Helper()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	svc := notifications.NewEmailService(notifications.EmailConfig{
		APIKey:    "test-resend-key",
		FromEmail: "noreply@enclii.dev",
		FromName:  "Enclii",
	}, logger)
	rec := &resendRecorder{}
	svc.ResendClient().SetHTTPClient(&http.Client{Transport: rec})
	return &Handler{emailService: svc}, rec
}

func sendTestRequest(args map[string]string) operatorOperationRequest {
	return operatorOperationRequest{Operation: sendTestOperation, Args: args}
}

func TestResendSendTest_ApplySendsFromTheSenderTheDryRunShowed(t *testing.T) {
	cases := []struct {
		name     string
		target   string
		wantFrom string
	}{
		{name: "tenant domain uses the tenant's default sender", target: "creatumundo.mx", wantFrom: "noreply@creatumundo.mx"},
		{name: "no target uses the configured sender", wantFrom: "noreply@enclii.dev"},
		{name: "domain of no tenant uses the configured sender", target: "unrelated.example", wantFrom: "noreply@enclii.dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, rec := sendTestHandler(t)
			args := map[string]string{"to": "ops@example.com"}
			if tc.target != "" {
				args["target"] = tc.target
			}
			ctx := context.Background()

			dry := h.handleResendSendTestApplyDryRun(ctx, sendTestOperation, sendTestRequest(args))
			require.Equal(t, "ready_to_apply", dry.Status)
			dryData, ok := dry.Data.(gin.H)
			require.True(t, ok)
			assert.Equal(t, "Enclii <"+tc.wantFrom+">", dryData["from"])
			assert.Empty(t, rec.sent(), "the dry-run sends nothing")

			resp, status := h.handleResendSendTestApply(ctx, sendTestOperation, sendTestRequest(args))
			require.Equal(t, http.StatusAccepted, status, resp.Summary)
			assert.Equal(t, "succeeded", resp.Status)
			sent := rec.sent()
			require.Len(t, sent, 1)
			assert.Equal(t, dryData["from"], sent[0]["from"], "the real send uses the sender the dry-run showed")
			assert.Equal(t, []any{"ops@example.com"}, sent[0]["to"])
			assert.Equal(t, gin.H{"to": "ops@example.com", "from": tc.wantFrom}, resp.Data)
		})
	}
}

func TestResendSendTest_ApplyUnconfiguredIs503BeforeAnySend(t *testing.T) {
	ctx := context.Background()
	args := map[string]string{"to": "ops@example.com"}

	t.Run("no email service and no config", func(t *testing.T) {
		h := &Handler{}
		resp, status := h.handleResendSendTestApply(ctx, sendTestOperation, sendTestRequest(args))
		assert.Equal(t, http.StatusServiceUnavailable, status)
		assert.Equal(t, "adapter_unconfigured", resp.Status)
		assert.False(t, resp.DryRun)
		assert.Contains(t, resp.Summary, "ENCLII_RESEND_API_KEY")
	})

	t.Run("email service without an API key", func(t *testing.T) {
		logger := logrus.New()
		logger.SetOutput(io.Discard)
		h := &Handler{emailService: notifications.NewEmailService(notifications.EmailConfig{}, logger)}
		resp, status := h.handleResendSendTestApply(ctx, sendTestOperation, sendTestRequest(args))
		assert.Equal(t, http.StatusServiceUnavailable, status)
		assert.Equal(t, "adapter_unconfigured", resp.Status)
	})

	t.Run("API key configured but no email service", func(t *testing.T) {
		// Before: the send went out from " <>" and came back as a 502.
		h := &Handler{config: &config.Config{EmailAPIKey: "test-resend-key"}}
		resp, status := h.handleResendSendTestApply(ctx, sendTestOperation, sendTestRequest(args))
		assert.Equal(t, http.StatusServiceUnavailable, status)
		assert.Equal(t, "adapter_unconfigured", resp.Status)
		assert.Equal(t, "email service is not wired", resp.Summary)
	})
}

func TestResendSendTest_ApplyWithoutRecipientIs400(t *testing.T) {
	h, rec := sendTestHandler(t)
	resp, status := h.handleResendSendTestApply(context.Background(), sendTestOperation, sendTestRequest(nil))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "invalid_request", resp.Status)
	assert.False(t, resp.DryRun)
	assert.Empty(t, rec.sent())
}
