package lockbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
	"github.com/stretchr/testify/require"
)

func trainingFixtureCapabilities(t *testing.T) map[string][]string {
	t.Helper()
	paths, err := TrainingCustodyCapabilityPaths()
	require.NoError(t, err)
	values := make(map[string][]string, len(paths))
	for _, path := range paths {
		values[path] = []string{"update", "read", "create", "patch"}
		if strings.HasPrefix(path, "secret/metadata/") {
			values[path] = []string{"read", "list"}
		}
	}
	return values
}

func trainingFixtureBody(t *testing.T, form string) []byte {
	t.Helper()
	data := trainingFixtureCapabilities(t)
	var value any = data
	if form != "bare" {
		envelope := map[string]any{"data": data, "request_id": "fixture-request", "lease_id": "", "renewable": false,
			"lease_duration": 0, "warnings": nil, "wrap_info": nil, "auth": nil, "mount_type": "system"}
		if form == "native-injected" {
			for path, capabilities := range data {
				envelope[path] = capabilities
			}
		}
		value = envelope
	}
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return body
}

func TestTrainingCapabilitiesProbeHasOneFixedRequestAndNoValueReads(t *testing.T) {
	for _, form := range []string{"bare", "envelope", "native-injected"} {
		t.Run(form, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "/v1/sys/capabilities-self", r.URL.Path)
				require.Empty(t, r.URL.RawQuery)
				require.Equal(t, "test-vault-token", r.Header.Get("X-Vault-Token"))
				require.Equal(t, "fixture-namespace", r.Header.Get("X-Vault-Namespace"))
				var body map[string][]string
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Len(t, body, 1)
				paths, err := TrainingCustodyCapabilityPaths()
				require.NoError(t, err)
				require.Equal(t, paths, body["paths"])
				require.Len(t, paths, 10)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(trainingFixtureBody(t, form))
			}))
			defer server.Close()
			client := newTestVaultClientWithNamespace(t, server, "fixture-namespace")
			beforeTimeout := client.httpClient.Timeout
			result, err := client.ProbeTrainingCustodyCapabilities(t.Context())
			require.NoError(t, err)
			require.Len(t, result, 10)
			require.Equal(t, []string{"create", "patch", "read", "update"}, result["secret/data/crea-sim-gateway"])
			require.Equal(t, 1, calls)
			require.Equal(t, beforeTimeout, client.httpClient.Timeout)
			require.Nil(t, client.httpClient.CheckRedirect, "the existing client is not mutated")
		})
	}
}

func TestTrainingCapabilitiesRefusesIncompleteConflictingOrSensitiveBodies(t *testing.T) {
	paths, err := TrainingCustodyCapabilityPaths()
	require.NoError(t, err)
	for _, name := range []string{"empty", "null", "malformed", "trailing", "missing", "extra", "duplicate target", "duplicate capability", "empty capabilities",
		"unknown capability", "non-string capability", "scalar", "deny mixed", "partial injection", "conflicting injection", "auth", "wrap", "warning", "lease", "oversized", "wrong content type", "403"} {
		t.Run(name, func(t *testing.T) {
			data := trainingFixtureCapabilities(t)
			var value any = data
			var body []byte
			status, contentType := http.StatusOK, "application/json"
			switch name {
			case "empty":
				value = map[string]any{}
			case "null":
				value = nil
			case "malformed":
				body = []byte("{private-fixture")
			case "trailing":
				body = append(trainingFixtureBody(t, "bare"), []byte(" {}")...)
			case "missing":
				delete(data, paths[0])
			case "extra":
				data["secret/data/foreign-fixture"] = []string{"read"}
			case "duplicate target":
				encoded, _ := json.Marshal(paths[0])
				prefix := append([]byte("{"), encoded...)
				body = append(append(prefix, []byte(": [\"read\"],")...), trainingFixtureBody(t, "bare")[1:]...)
			case "duplicate capability":
				data[paths[0]] = []string{"read", "read"}
			case "empty capabilities":
				data[paths[0]] = []string{}
			case "unknown capability":
				data[paths[0]] = []string{"private-fixture"}
			case "non-string capability":
				mixed := map[string]any{}
				for path, caps := range data {
					mixed[path] = caps
				}
				mixed[paths[0]] = []any{"read", false}
				value = mixed
			case "scalar":
				mixed := map[string]any{}
				for path, caps := range data {
					mixed[path] = caps
				}
				mixed[paths[0]] = "read"
				value = mixed
			case "deny mixed":
				data[paths[0]] = []string{"deny", "read"}
			case "partial injection":
				value = map[string]any{"data": data, paths[0]: data[paths[0]]}
			case "conflicting injection":
				mixed := map[string]any{"data": data}
				for path, caps := range data {
					mixed[path] = caps
				}
				mixed[paths[0]] = []string{"root"}
				value = mixed
			case "auth":
				value = map[string]any{"data": data, "auth": map[string]any{"client_token": "private-fixture"}}
			case "wrap":
				value = map[string]any{"data": data, "wrap_info": map[string]any{"token": "private-fixture"}}
			case "warning":
				value = map[string]any{"data": data, "warnings": []string{"private-fixture"}}
			case "lease":
				value = map[string]any{"data": data, "lease_duration": 3}
			case "oversized":
				body = []byte(strings.Repeat("private-fixture", trainingCapabilityBodyLimit))
			case "wrong content type":
				contentType = "text/html"
			case "403":
				status, body = http.StatusForbidden, []byte("private-fixture")
			}
			if body == nil {
				body, err = json.Marshal(value)
				require.NoError(t, err)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", contentType)
				w.WriteHeader(status)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			result, err := newTestVaultClient(t, server).ProbeTrainingCustodyCapabilities(t.Context())
			require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
			require.Nil(t, result)
			require.NotContains(t, err.Error(), "private-fixture")
			require.Equal(t, 1, calls, "refusal never retries")
		})
	}
}

func TestTrainingCapabilitiesPreservesKnownElevatedAuthorityForRefusal(t *testing.T) {
	for _, capability := range []string{"root", "sudo", "delete", "list", "deny"} {
		values := trainingFixtureCapabilities(t)
		values["secret/data/crea-sim-gateway"] = []string{capability}
		validated, err := ValidateTrainingCustodyCapabilities(values)
		require.NoError(t, err)
		require.Equal(t, []string{capability}, validated["secret/data/crea-sim-gateway"])
		validated["secret/data/crea-sim-gateway"][0] = "read"
		require.Equal(t, []string{capability}, values["secret/data/crea-sim-gateway"], "validation copies slices")
	}
}

func TestTrainingCapabilitiesRedirectCancellationAndNetworkFailWithoutRetry(t *testing.T) {
	var forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	_, err := newTestVaultClient(t, source).ProbeTrainingCustodyCapabilities(t.Context())
	require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
	require.Zero(t, forwarded.Load(), "the token must never follow a redirect")
	var calls atomic.Int32
	started := make(chan struct{})
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer blocked.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := newTestVaultClient(t, blocked).ProbeTrainingCustodyCapabilities(ctx); done <- err }()
	<-started
	cancel()
	select {
	case err = <-done:
		require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
	case <-time.After(time.Second):
		t.Fatal("probe did not honor cancellation")
	}
	require.EqualValues(t, 1, calls.Load())
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()
	_, err = newTestVaultClient(t, closed).ProbeTrainingCustodyCapabilities(t.Context())
	require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
}

func TestTrainingCapabilitiesRegistryDriftAndConfigurationAreClosed(t *testing.T) {
	registry, err := secretsintake.LoadRegistry()
	require.NoError(t, err)
	for _, name := range []string{"foreign path", "foreign namespace", "missing destination", "missing ID with destination retained",
		"unreviewed ID on approved path", "reroute within retained destination set", "target ID alias", "two IDs swapped"} {
		t.Run(name, func(t *testing.T) {
			copy := make(map[string]secretsintake.Target, len(registry))
			for id, target := range registry {
				copy[id] = target
			}
			switch name {
			case "foreign path":
				target := copy["crea-sim/ui-oidc"]
				target.VaultPath = "secret/foreign-fixture"
				copy[target.ID] = target
			case "foreign namespace":
				target := copy["crea-sim/ui-oidc"]
				target.Namespace = "foreign-fixture"
				copy[target.ID] = target
			case "missing destination":
				for id, target := range copy {
					if target.VaultPath == "secret/crea-sim-selva" {
						delete(copy, id)
					}
				}
			case "missing ID with destination retained":
				delete(copy, "crea-sim/ui-oidc")
			case "unreviewed ID on approved path":
				target := copy["crea-sim/ui-oidc"]
				target.ID = "crea-sim/unreviewed"
				copy[target.ID] = target
			case "reroute within retained destination set":
				target := copy["crea-sim/ui-oidc"]
				target.VaultPath = "secret/crea-sim-captura"
				copy[target.ID] = target
			case "target ID alias":
				target := copy["crea-sim/ui-oidc"]
				target.ID = "crea-sim/mcp-oidc"
				copy["crea-sim/ui-oidc"] = target
			case "two IDs swapped":
				ui, selva := copy["crea-sim/ui-oidc"], copy["crea-sim/selva-oidc"]
				ui.VaultPath, selva.VaultPath = selva.VaultPath, ui.VaultPath
				copy[ui.ID], copy[selva.ID] = ui, selva
			}
			_, err := trainingCapabilityPaths(copy)
			require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid configuration must not send a request") }))
	defer server.Close()
	for _, address := range []string{server.URL + "/arbitrary", server.URL + "?token=private-fixture", server.URL + "?", "file:///fixture", "https://user:private-fixture@fixture.invalid"} {
		client := newTestVaultClient(t, server)
		client.address = address
		_, err = client.ProbeTrainingCustodyCapabilities(t.Context())
		require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
	}
	client := newTestVaultClient(t, server)
	client.enabled = false
	_, err = client.ProbeTrainingCustodyCapabilities(t.Context())
	require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
}

func TestTrainingCapabilitiesHonorsShorterClientTimeoutWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	client := newTestVaultClient(t, server)
	client.httpClient.Timeout = 25 * time.Millisecond
	started := time.Now()
	result, err := client.ProbeTrainingCustodyCapabilities(t.Context())
	require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
	require.Nil(t, result)
	require.Less(t, time.Since(started), time.Second)
	require.EqualValues(t, 1, calls.Load())
}

// The body parser rejects duplicate envelope keys too, even when the last
// value would otherwise mask the first under encoding/json's default behavior.
func TestTrainingCapabilitiesDuplicateEnvelopeIsUnverified(t *testing.T) {
	paths, err := TrainingCustodyCapabilityPaths()
	require.NoError(t, err)
	body := append([]byte("{\"data\":null,\"data\":"), trainingFixtureBody(t, "bare")...)
	body = append(body, '}')
	_, err = parseTrainingCapabilities(body, paths)
	require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
	_, err = parseTrainingCapabilities([]byte("[]"), paths)
	require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
	_, err = parseTrainingCapabilities([]byte("{\"data\":null}"), paths)
	require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
	_, err = parseTrainingCapabilities([]byte("{} trailing"), paths)
	require.ErrorIs(t, err, ErrTrainingCapabilitiesUnverified)
}
