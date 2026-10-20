package client

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostRawOnceDoesNotReplayRedirectOrChangeSharedClient(t *testing.T) {
	posts, redirected := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			redirected++
			_, _ = w.Write([]byte(`{"accepted":true}`))
			return
		}
		posts++
		http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := NewAPIClient(server.URL, "fixture-token")
	_, err := client.PostRawOnce(t.Context(), "/initialize", bytes.NewReader([]byte(`{"values":"do-not-echo"}`)))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "do-not-echo")
	require.Contains(t, err.Error(), "mutation outcome unconfirmed")
	require.Equal(t, 1, posts)
	require.Equal(t, 0, redirected)
	require.Nil(t, client.httpClient.CheckRedirect)
}

func TestPostRawOnceDoesNotRetryServerFailure(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		require.Empty(t, r.Header.Get("Idempotency-Key"))
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream","message":"sensitive-proxy-echo"}`))
	}))
	defer server.Close()
	_, err := NewAPIClient(server.URL, "").PostRawOnce(t.Context(), "/initialize", bytes.NewReader([]byte(`{}`)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "inspect custody")
	require.NotContains(t, err.Error(), "sensitive-proxy-echo")
	require.Equal(t, 1, posts)
}
