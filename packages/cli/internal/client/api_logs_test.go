package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `enclii logs <svc> --since 24h` without --follow goes through
// GetServiceLogsHistoryRaw. The server applies the window from ?since=
// (switchyard-api GetLogsHistory), which accepts the RFC3339 timestamp sent
// here; this pins that the one-shot path keeps sending it.
func TestAPIClient_GetServiceLogsHistoryRaw_SendsSince(t *testing.T) {
	since := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	var gotSince string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSince = r.URL.Query().Get("since")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"logs": "ok\n"})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	_, err := client.GetServiceLogsHistoryRaw(context.Background(), "svc", "production", LogOptions{Lines: 10, Since: &since})
	require.NoError(t, err)
	assert.Equal(t, "2026-09-23T12:00:00Z", gotSince)

	parsed, err := time.Parse(time.RFC3339, gotSince)
	require.NoError(t, err, "since must be RFC3339, the form the server parses")
	assert.True(t, parsed.Equal(since))
}

func TestAPIClient_GetServiceLogsHistoryRaw_OmitsSinceWhenUnset(t *testing.T) {
	present := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.URL.Query()["since"]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"logs": ""})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	_, err := client.GetServiceLogsHistoryRaw(context.Background(), "svc", "production", LogOptions{Lines: 10})
	require.NoError(t, err)
	assert.False(t, present, "no --since must send no since parameter")
}
