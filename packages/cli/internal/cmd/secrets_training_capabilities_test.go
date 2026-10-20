package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/stretchr/testify/require"
)

func trainingCapabilitiesFixture() trainingCapabilitiesReceipt {
	yes, no := true, false
	r := trainingCapabilitiesReceipt{Version: trainingCapabilitiesVersion, ObservedAt: "2026-10-20T12:34:56.123Z", Status: "verified_required_capabilities", AllWithin: &yes, Limits: trainingCapabilitiesLimits()}
	for _, path := range trainingCapabilitiesPaths() {
		required := []string{"create", "patch", "read", "update"}
		if strings.HasPrefix(path, "secret/metadata/") {
			required = []string{"list", "read"}
		}
		r.Paths = append(r.Paths, trainingCapabilitiesPath{Path: path, Capabilities: append([]string{}, required...), Required: required, Missing: []string{}, Outside: []string{}, Unknown: &no})
	}
	return r
}

func trainingCapabilitiesJSON(t *testing.T, receipt trainingCapabilitiesReceipt) []byte {
	t.Helper()
	body, err := json.Marshal(receipt)
	require.NoError(t, err)
	return body
}

func runTrainingCapabilities(t *testing.T, cfg *config.Config, args ...string) (string, error) {
	t.Helper()
	command := newSecretsTrainingCapabilitiesCommand(cfg)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	// Cobra's parent normally formats errors. Keep this check scoped to the
	// subcommand's deliberately safe output, with the returned error separate.
	command.SilenceErrors, command.SilenceUsage = true, true
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}

func TestTrainingCapabilitiesOneFixedGETAndTypedOutput(t *testing.T) {
	for _, jsonOut := range []bool{false, true} {
		t.Run(fmt.Sprint(jsonOut), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, trainingCapabilitiesRoute, r.URL.Path)
				require.Empty(t, r.URL.RawQuery)
				require.False(t, r.URL.ForceQuery)
				require.Equal(t, int64(0), r.ContentLength)
				require.Empty(t, r.TransferEncoding)
				require.Empty(t, r.Cookies())
				require.Equal(t, "Bearer fixture-explicit", r.Header.Get("Authorization"))
				_, _ = w.Write(trainingCapabilitiesJSON(t, trainingCapabilitiesFixture()))
			}))
			defer server.Close()
			args := []string{}
			if jsonOut {
				args = append(args, "--json")
			}
			out, err := runTrainingCapabilities(t, &config.Config{APIEndpoint: server.URL, APIToken: "fixture-explicit"}, args...)
			require.NoError(t, err)
			require.Equal(t, int32(1), requests.Load())
			require.Contains(t, out, "verified_required_capabilities")
			require.NotContains(t, out, "fixture-explicit")
			if jsonOut {
				decoded, err := decodeTrainingCapabilities([]byte(out), http.StatusOK)
				require.NoError(t, err)
				require.Len(t, decoded.Paths, 10)
			} else {
				require.Contains(t, out, "no credential mint, delivery, ownership or readiness authority")
			}
		})
	}
}

func TestTrainingCapabilitiesDerivesNegativeEvidence(t *testing.T) {
	for _, mode := range []string{"missing", "outside", "missing and outside", "deny", "unverified", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			r := trainingCapabilitiesFixture()
			no, yes := false, true
			r.AllWithin = &no
			status := http.StatusOK
			switch mode {
			case "missing":
				r.Status = "missing_required"
				r.Paths[0].Capabilities = []string{"read"}
				r.Paths[0].Missing = []string{"create", "patch", "update"}
			case "outside":
				r.Status = "outside_expected_scoped_contract"
				r.Paths[0].Capabilities = append(r.Paths[0].Capabilities, "sudo", "root")
				r.Paths[0].Outside = []string{"sudo", "root"}
			case "missing and outside":
				r.Status = "outside_expected_scoped_contract"
				r.Paths[0].Capabilities = []string{"read", "root"}
				r.Paths[0].Missing = []string{"create", "patch", "update"}
				r.Paths[0].Outside = []string{"root"}
			case "deny":
				r.Status = "missing_required"
				r.Paths[0].Capabilities = []string{"deny"}
				r.Paths[0].Missing = append([]string{}, r.Paths[0].Required...)
			case "unverified", "unavailable":
				r.Status = mode
				status = http.StatusBadGateway
				if mode == "unavailable" {
					status = http.StatusServiceUnavailable
				}
				for i := range r.Paths {
					r.Paths[i].Unknown = &yes
					r.Paths[i].Capabilities = []string{}
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write(trainingCapabilitiesJSON(t, r))
			}))
			defer server.Close()
			out, err := runTrainingCapabilities(t, &config.Config{APIEndpoint: server.URL, APIToken: "fixture"}, "--json")
			require.Error(t, err)
			require.Contains(t, err.Error(), r.Status)
			decoded, decodeErr := decodeTrainingCapabilities([]byte(out), status)
			require.NoError(t, decodeErr)
			require.False(t, *decoded.AllWithin)
		})
	}
}

func TestTrainingCapabilitiesRejectsMalformedOrUnboundReceipt(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*trainingCapabilitiesReceipt)
	}{
		{"version", func(r *trainingCapabilitiesReceipt) { r.Version = "future-version" }},
		{"unknown status", func(r *trainingCapabilitiesReceipt) { r.Status = "ready" }},
		{"false boolean", func(r *trainingCapabilitiesReceipt) { no := false; r.AllWithin = &no }},
		{"missing boolean", func(r *trainingCapabilitiesReceipt) { r.AllWithin = nil }},
		{"missing path", func(r *trainingCapabilitiesReceipt) { r.Paths = r.Paths[:9] }},
		{"extra path", func(r *trainingCapabilitiesReceipt) { r.Paths = append(r.Paths, r.Paths[0]) }},
		{"duplicate path", func(r *trainingCapabilitiesReceipt) { r.Paths[0].Path = r.Paths[1].Path }},
		{"arbitrary path", func(r *trainingCapabilitiesReceipt) { r.Paths[0].Path = "secret/data/unreviewed" }},
		{"null cap set", func(r *trainingCapabilitiesReceipt) { r.Paths[0].Capabilities = nil }},
		{"empty cap set", func(r *trainingCapabilitiesReceipt) { r.Paths[0].Capabilities = []string{} }},
		{"unknown capability", func(r *trainingCapabilitiesReceipt) { r.Paths[0].Capabilities[0] = "fixture-private-field" }},
		{"duplicate capability", func(r *trainingCapabilitiesReceipt) {
			r.Paths[0].Capabilities = append(r.Paths[0].Capabilities, "read")
		}},
		{"deny mixed", func(r *trainingCapabilitiesReceipt) {
			r.Paths[0].Capabilities = append(r.Paths[0].Capabilities, "deny")
		}},
		{"wrong required", func(r *trainingCapabilitiesReceipt) { r.Paths[0].Required = []string{"read"} }},
		{"missing list untrue", func(r *trainingCapabilitiesReceipt) { r.Paths[0].Missing = []string{"read"} }},
		{"outside list untrue", func(r *trainingCapabilitiesReceipt) { r.Paths[0].Outside = []string{"root"} }},
		{"unknown flag missing", func(r *trainingCapabilitiesReceipt) { r.Paths[0].Unknown = nil }},
		{"unknown flag inconsistent", func(r *trainingCapabilitiesReceipt) { yes := true; r.Paths[0].Unknown = &yes }},
		{"not UTC", func(r *trainingCapabilitiesReceipt) { r.ObservedAt = "2026-10-20T12:34:56+01:00" }},
		{"invalid timestamp", func(r *trainingCapabilitiesReceipt) { r.ObservedAt = "yesterday" }},
		{"zero timestamp", func(r *trainingCapabilitiesReceipt) { r.ObservedAt = "0001-01-01T00:00:00Z" }},
		{"raw limits", func(r *trainingCapabilitiesReceipt) { r.Limits[0] = "fixture-private-field" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := trainingCapabilitiesFixture()
			tc.mutate(&r)
			_, err := decodeTrainingCapabilities(trainingCapabilitiesJSON(t, r), http.StatusOK)
			require.Error(t, err)
		})
	}
	valid := trainingCapabilitiesJSON(t, trainingCapabilitiesFixture())
	for name, body := range map[string][]byte{
		"unknown key":        bytes.Replace(valid, []byte(`{"version":`), []byte(`{"raw_error":"fixture-private-field","version":`), 1),
		"case alias":         bytes.Replace(valid, []byte(`"version":`), []byte(`"Version":`), 1),
		"duplicate key":      bytes.Replace(valid, []byte(`{"version":`), []byte(`{"version":"old","version":`), 1),
		"trailing JSON":      append(append([]byte{}, valid...), []byte(` {}`)...),
		"non JSON proxy":     []byte(`<html>fixture-private-field</html>`),
		"invalid UTF8":       {0xff},
		"oversized":          bytes.Repeat([]byte(" "), trainingCapabilitiesMaxBody+1),
		"excessive nesting":  []byte(strings.Repeat("[", 18) + strings.Repeat("]", 18)),
		"missing path field": bytes.Replace(valid, []byte(`"unknown":false`), []byte(`"unknown":null`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeTrainingCapabilities(body, http.StatusOK)
			require.Error(t, err)
		})
	}
	for _, status := range []int{http.StatusCreated, http.StatusUnauthorized, http.StatusBadGateway, http.StatusServiceUnavailable} {
		_, err := decodeTrainingCapabilities(valid, status)
		require.Error(t, err)
	}
	unknown := trainingCapabilitiesFixture()
	unknown.Status = "unverified"
	no, yes := false, true
	unknown.AllWithin = &no
	for i := range unknown.Paths {
		unknown.Paths[i].Unknown = &yes
		unknown.Paths[i].Capabilities = []string{}
	}
	unknown.Paths[0].Capabilities = []string{"read"}
	_, err := decodeTrainingCapabilities(trainingCapabilitiesJSON(t, unknown), http.StatusBadGateway)
	require.Error(t, err, "an unknown receipt must not contain partial known capability evidence")
}

func TestTrainingCapabilitiesWithholdsProxyErrorsAndDoesNotRedirectOrRetry(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusOK} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("Location", "/fixture-private-field")
				w.Header().Set("Set-Cookie", "fixture-private-field=fixture")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`<html>fixture-private-field</html>`))
			}))
			defer server.Close()
			out, err := runTrainingCapabilities(t, &config.Config{APIEndpoint: server.URL, APIToken: "fixture"}, "--json")
			require.Error(t, err)
			require.Empty(t, out)
			require.NotContains(t, err.Error(), "fixture-private-field")
			require.Equal(t, int32(1), count.Load())
		})
	}
}

func TestTrainingCapabilitiesUsesManagedCredentialsAndExplicitTokenWins(t *testing.T) {
	for _, explicit := range []string{"", "fixture-explicit-invalid"} {
		t.Run(explicit, func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				if explicit != "" {
					require.Equal(t, "Bearer "+explicit, r.Header.Get("Authorization"))
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				require.Equal(t, "Bearer fixture-managed", r.Header.Get("Authorization"))
				_, _ = w.Write(trainingCapabilitiesJSON(t, trainingCapabilitiesFixture()))
			}))
			defer server.Close()
			cfg := &config.Config{APIEndpoint: server.URL, APIToken: explicit, Credentials: &config.Credentials{AccessToken: "fixture-managed"}}
			_, err := runTrainingCapabilities(t, cfg, "--json")
			if explicit == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, int32(1), count.Load(), "an explicit failure must not replay using managed credentials")
		})
	}
}

func TestTrainingCapabilitiesTransportFailureDoesNotReplay(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		_ = connection.Close()
	}))
	defer server.Close()
	out, err := runTrainingCapabilities(t, &config.Config{APIEndpoint: server.URL, APIToken: "fixture"}, "--json")
	require.Error(t, err)
	require.Empty(t, out)
	require.Equal(t, int32(1), requests.Load())
	require.NotContains(t, err.Error(), server.URL)
}

func TestTrainingCapabilitiesRejectsSelectorsAndUnsafeBaseBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	for _, endpoint := range []string{server.URL + "/path", server.URL + "?", server.URL + "?path=unreviewed", server.URL + "#fragment", "file:///fixture", "https://user:fixture@example.invalid"} {
		_, err := runTrainingCapabilities(t, &config.Config{APIEndpoint: endpoint, APIToken: "fixture"}, "--json")
		require.Error(t, err)
	}
	for _, args := range [][]string{{"secret/data/unreviewed"}, {"--path=secret/data/unreviewed"}, {"--apply"}} {
		_, err := runTrainingCapabilities(t, &config.Config{APIEndpoint: server.URL, APIToken: "fixture"}, args...)
		require.Error(t, err)
	}
	_, err := runTrainingCapabilities(t, &config.Config{APIEndpoint: server.URL}, "--json")
	require.Error(t, err)
	require.Zero(t, requests.Load())
	intake := findSubcommand(NewSecretsCommand(&config.Config{}), "intake")
	require.NotNil(t, findSubcommand(intake, "training-capabilities"))
}
