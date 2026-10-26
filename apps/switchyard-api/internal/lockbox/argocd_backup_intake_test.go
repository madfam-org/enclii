package lockbox

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

const backupOriginFixture = "123e4567-e89b-42d3-a456-426614174000"

type backupVaultTransport func(*http.Request) (*http.Response, error)

func (f backupVaultTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func (backupVaultTransport) singleAttemptIntake()                                {}

type backupVaultBody struct {
	reader io.Reader
	reads  int
	bytes  int
	closed int
	err    error
}

func (b *backupVaultBody) Read(p []byte) (int, error) {
	b.reads++
	n, err := b.reader.Read(p)
	b.bytes += n
	return n, err
}
func (b *backupVaultBody) Close() error { b.closed++; return b.err }

type backupInfiniteReader struct{}

func (backupInfiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

type backupFaultReader struct{}

func (backupFaultReader) Read([]byte) (int, error) { return 0, errors.New("read-private-canary") }

type backupIdleReader struct{}

func (backupIdleReader) Read([]byte) (int, error) { return 0, nil }

func backupCustodyUpdates(t *testing.T) map[string]interface{} {
	t.Helper()
	origin, err := secretsintake.NewArgocdBackupOrigin(backupOriginFixture, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, err)
	return map[string]interface{}{
		secretsintake.ArgocdBackupKey:       base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		secretsintake.ArgocdBackupOriginKey: origin,
	}
}

func backupCustodyResponse(t *testing.T, data map[string]interface{}) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]interface{}{"data": map[string]interface{}{
		"data": data, "metadata": map[string]interface{}{"version": 7, "destroyed": false, "deletion_time": ""},
	}})
	require.NoError(t, err)
	return string(encoded)
}

func backupTestClient(transport backupVaultTransport) *VaultClient {
	return &VaultClient{address: "https://vault.invalid", token: "fake-test-only", namespace: "test", enabled: true, httpClient: &http.Client{Transport: transport}}
}

func TestArgocdBackupRetainedExactVersionReadSelectsOnlyQualifiedKey(t *testing.T) {
	values := backupCustodyUpdates(t)
	values["internal_api_key"] = "unrelated-private-canary"
	values["unrelated_origin"] = "other-origin-canary"
	body := &backupVaultBody{reader: strings.NewReader(backupCustodyResponse(t, values))}
	calls := 0
	client := backupTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "/v1/secret/data/enclii", req.URL.Path)
		require.Equal(t, "version=7", req.URL.RawQuery)
		require.Equal(t, "fake-test-only", req.Header.Get("X-Vault-Token"))
		require.Equal(t, "test", req.Header.Get("X-Vault-Namespace"))
		require.Empty(t, req.Header.Get("Cookie"))
		return &http.Response{StatusCode: 200, Body: body, ContentLength: -1, Header: make(http.Header)}, nil
	})
	key, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), backupOriginFixture, 7)
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{9}, 32), key)
	clear(key)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, body.closed)
}

func TestArgocdBackupVersionOriginAndSnapshotFailuresNeverFallback(t *testing.T) {
	valid := backupCustodyResponse(t, backupCustodyUpdates(t))
	for _, mutate := range []func(string) string{
		func(s string) string { return strings.Replace(s, `"version":7`, `"version":8`, 1) },
		func(s string) string { return strings.Replace(s, `"version":7`, `"version":7.0`, 1) },
		func(s string) string { return strings.Replace(s, `"destroyed":false`, `"destroyed":true`, 1) },
		func(s string) string { return strings.Replace(s, `"deletion_time":""`, `"deletion_time":"deleted"`, 1) },
		func(s string) string { return strings.Replace(s, `"deletion_time":"",`, ``, 1) },
		func(s string) string {
			return strings.Replace(s, backupOriginFixture, "123e4567-e89b-42d3-a456-426614174001", 1)
		},
		func(s string) string {
			return strings.Replace(s, secretsintake.ArgocdBackupKey, "ARGOCD_BACKUP_ENCRYPTION_KEY", 1)
		},
		func(s string) string { return strings.Replace(s, `"version":7`, `"version":7,"version":7`, 1) },
		func(s string) string { return s + `{}` },
		func(string) string { return `{}` },
		func(string) string { return `null` },
		func(string) string { return strings.Repeat(`[`, 18) + `0` + strings.Repeat(`]`, 18) },
	} {
		calls := 0
		body := &backupVaultBody{reader: strings.NewReader(mutate(valid))}
		client := backupTestClient(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Body: body, ContentLength: -1}, nil
		})
		key, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), backupOriginFixture, 7)
		require.Nil(t, key)
		require.Error(t, err)
		require.Equal(t, 1, calls)
		require.Equal(t, 1, body.closed)
		require.NotContains(t, err.Error(), "canary")
	}
	for _, badKey := range []interface{}{nil, 32, "", strings.Repeat("x", 32), base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)), base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 31))} {
		values := backupCustodyUpdates(t)
		values[secretsintake.ArgocdBackupKey] = badKey
		client := backupTestClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader(backupCustodyResponse(t, values)))}, nil
		})
		_, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), backupOriginFixture, 7)
		require.Error(t, err)
	}
	client := backupTestClient(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid reference must never send request")
		return nil, nil
	})
	for _, version := range []int{0, -1} {
		_, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), backupOriginFixture, version)
		require.Error(t, err)
	}
	_, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), "not-canonical", 7)
	require.Error(t, err)
}

func TestArgocdBackupAllHTTPStatusesBoundBeforeClassification(t *testing.T) {
	for status := 100; status <= 599; status++ {
		for _, known := range []bool{false, true} {
			body := &backupVaultBody{reader: backupInfiniteReader{}}
			length := int64(-1)
			if known {
				length = 2 << 20
			}
			calls := 0
			client := backupTestClient(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, ContentLength: length, Body: body, Header: http.Header{"Location": {"https://redirect.invalid"}}}, nil
			})
			key, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), backupOriginFixture, 7)
			require.Nil(t, key)
			require.Error(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, 1, body.closed)
			if known {
				require.Zero(t, body.bytes)
			} else {
				require.LessOrEqual(t, body.bytes, intakeResponseByteLimit+1)
			}
		}
	}
}

func TestArgocdBackupBoundedBodyFaultsCloseAndNeverExposeProviderErrors(t *testing.T) {
	valid := backupCustodyResponse(t, backupCustodyUpdates(t))
	for _, tc := range []struct {
		name   string
		reader io.Reader
		length int64
		close  error
	}{
		{"read", backupFaultReader{}, -1, nil},
		{"idle", backupIdleReader{}, -1, nil},
		{"close", strings.NewReader(valid), -1, errors.New("close-private-canary")},
		{"truncated", strings.NewReader(valid), int64(len(valid) + 1), nil},
		{"lying-length", strings.NewReader(valid), 1, nil},
		{"invalid-length", strings.NewReader(valid), -2, nil},
		{"unexpected-eof", io.MultiReader(strings.NewReader(valid), backupUnexpectedEOF{}), -1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &backupVaultBody{reader: tc.reader, err: tc.close}
			client := backupTestClient(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: tc.length, Body: body}, nil
			})
			_, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), backupOriginFixture, 7)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "canary")
			require.Equal(t, 1, body.closed)
			require.LessOrEqual(t, body.bytes, intakeResponseByteLimit+1)
			require.LessOrEqual(t, body.reads, 17)
		})
	}
	client := backupTestClient(func(*http.Request) (*http.Response, error) { return nil, errors.New("provider-private-canary") })
	_, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), backupOriginFixture, 7)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "canary")
}

type backupUnexpectedEOF struct{}

func (backupUnexpectedEOF) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestArgocdBackupTypedCASPreservesUnrelatedKeysAndOpaqueOrigins(t *testing.T) {
	updates := backupCustodyUpdates(t)
	calls := 0
	client := backupTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "/v1/secret/data/enclii", req.URL.Path)
		require.Empty(t, req.URL.RawQuery)
		if req.Method == http.MethodGet {
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader(`{"data":{"data":{"internal_api_key":"unrelated-canary","pgbouncer_auth_password":"other-canary","old_origin":"old-canary","large":9007199254740993},"metadata":{"version":6,"destroyed":false,"deletion_time":""}}}`))}, nil
		}
		require.Equal(t, http.MethodPost, req.Method)
		var payload struct {
			Data    map[string]interface{} `json:"data"`
			Options map[string]int         `json:"options"`
		}
		d := json.NewDecoder(req.Body)
		d.UseNumber()
		require.NoError(t, d.Decode(&payload))
		require.Equal(t, 6, payload.Options["cas"])
		require.Len(t, payload.Data, 6)
		require.Equal(t, "unrelated-canary", payload.Data["internal_api_key"])
		require.Equal(t, "other-canary", payload.Data["pgbouncer_auth_password"])
		require.Equal(t, "old-canary", payload.Data["old_origin"])
		require.Equal(t, json.Number("9007199254740993"), payload.Data["large"])
		for name, value := range updates {
			require.Equal(t, value, payload.Data[name])
		}
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader(`{"data":{"version":7}}`))}, nil
	})
	version, err := client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, updates)
	require.NoError(t, err)
	require.Equal(t, 7, version)
	require.Equal(t, 2, calls)
}

func TestArgocdBackupTypedCASRefusesPartialExistingOrMalformedGroups(t *testing.T) {
	for _, existing := range []string{
		fmt.Sprintf(`{"%s":"existing-canary"}`, secretsintake.ArgocdBackupKey),
		fmt.Sprintf(`{"%s":"existing-origin-canary"}`, secretsintake.ArgocdBackupOriginKey),
		`{"ARGOCD-BACKUP.ENCRYPTION.KEY":"existing-alias-canary"}`,
		`{"ENCLII.ARGOCD-BACKUP.ORIGIN-V1":"existing-alias-origin-canary"}`,
	} {
		calls := 0
		client := backupTestClient(func(req *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, http.MethodGet, req.Method)
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"data":{"data":%s,"metadata":{"version":7,"destroyed":false,"deletion_time":""}}}`, existing)))}, nil
		})
		_, err := client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, backupCustodyUpdates(t))
		require.ErrorIs(t, err, ErrSecretInitializationConflict)
		require.Equal(t, 1, calls)
	}
	for _, mutate := range []func(map[string]interface{}){
		func(v map[string]interface{}) { delete(v, secretsintake.ArgocdBackupKey) },
		func(v map[string]interface{}) { delete(v, secretsintake.ArgocdBackupOriginKey) },
		func(v map[string]interface{}) { v["extra"] = "forbidden" },
		func(v map[string]interface{}) { v[secretsintake.ArgocdBackupKey] = "short" },
		func(v map[string]interface{}) { v[secretsintake.ArgocdBackupOriginKey] = "invalid-origin" },
		func(v map[string]interface{}) {
			v[strings.ToUpper(secretsintake.ArgocdBackupKey)] = v[secretsintake.ArgocdBackupKey]
			delete(v, secretsintake.ArgocdBackupKey)
		},
	} {
		updates := backupCustodyUpdates(t)
		mutate(updates)
		client := backupTestClient(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid typed group must not reach any transport")
			return nil, nil
		})
		_, err := client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, updates)
		require.Error(t, err)
	}
}

func TestArgocdBackupTypedCASBoundsEveryReadAndACKStatus(t *testing.T) {
	for _, phase := range []string{"read", "ack"} {
		for status := 100; status <= 599; status++ {
			body := &backupVaultBody{reader: backupInfiniteReader{}}
			calls := 0
			client := backupTestClient(func(req *http.Request) (*http.Response, error) {
				calls++
				if phase == "ack" && req.Method == http.MethodGet {
					return &http.Response{StatusCode: 404, ContentLength: 0, Body: http.NoBody}, nil
				}
				return &http.Response{StatusCode: status, ContentLength: -1, Body: body, Header: http.Header{"Location": {"https://redirect.invalid"}}}, nil
			})
			_, err := client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, backupCustodyUpdates(t))
			require.Error(t, err)
			if phase == "ack" {
				require.ErrorIs(t, err, ErrSecretInitializationOutcomeUnknown)
				require.Equal(t, 2, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			require.Equal(t, 1, body.closed)
			require.LessOrEqual(t, body.bytes, intakeResponseByteLimit+1)
		}
	}
}

func TestArgocdBackupTypedCASFiniteStatusAndFaultQualification(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		close  error
		want   error
	}{
		{200, `{"data":{"version":1}}`, nil, nil},
		{400, `private-provider-canary`, nil, ErrSecretInitializationConflict},
		{409, `private-provider-canary`, nil, ErrSecretInitializationConflict},
		{500, `private-provider-canary`, nil, ErrSecretInitializationOutcomeUnknown},
		{200, `{"data":{"version":1}}`, errors.New("close-private-canary"), ErrSecretInitializationOutcomeUnknown},
		{409, `private-provider-canary`, errors.New("close-private-canary"), ErrSecretInitializationOutcomeUnknown},
		{200, `{"data":{"version":1,"version":1}}`, nil, ErrSecretInitializationOutcomeUnknown},
		{200, `{"data":{"version":1}}{}`, nil, ErrSecretInitializationOutcomeUnknown},
		{200, `{"data":{"version":2}}`, nil, ErrSecretInitializationOutcomeUnknown},
	} {
		calls := 0
		body := &backupVaultBody{reader: strings.NewReader(tc.body), err: tc.close}
		client := backupTestClient(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method == http.MethodGet {
				return &http.Response{StatusCode: 404, ContentLength: 0, Body: http.NoBody}, nil
			}
			return &http.Response{StatusCode: tc.status, ContentLength: -1, Body: body}, nil
		})
		version, err := client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, backupCustodyUpdates(t))
		if tc.want == nil {
			require.NoError(t, err)
			require.Equal(t, 1, version)
		} else {
			require.ErrorIs(t, err, tc.want)
			require.NotContains(t, err.Error(), "canary")
		}
		require.Equal(t, 2, calls)
		require.Equal(t, 1, body.closed)
	}
	for _, phase := range []string{"read", "ack"} {
		for _, tc := range []struct {
			reader io.Reader
			length int64
			close  error
		}{
			{backupInfiniteReader{}, 2 << 20, nil},
			{backupFaultReader{}, -1, nil},
			{backupIdleReader{}, -1, nil},
			{strings.NewReader(`{"data":{"version":1}}`), -1, errors.New("close-private-canary")},
		} {
			calls := 0
			body := &backupVaultBody{reader: tc.reader, err: tc.close}
			client := backupTestClient(func(req *http.Request) (*http.Response, error) {
				calls++
				if phase == "ack" && req.Method == http.MethodGet {
					return &http.Response{StatusCode: 404, ContentLength: 0, Body: http.NoBody}, nil
				}
				return &http.Response{StatusCode: 200, ContentLength: tc.length, Body: body}, nil
			})
			_, err := client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, backupCustodyUpdates(t))
			require.Error(t, err)
			if phase == "ack" {
				require.ErrorIs(t, err, ErrSecretInitializationOutcomeUnknown)
				require.Equal(t, 2, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			require.NotContains(t, err.Error(), "canary")
			require.Equal(t, 1, body.closed)
			if tc.length > intakeResponseByteLimit {
				require.Zero(t, body.bytes)
			}
			require.LessOrEqual(t, body.bytes, intakeResponseByteLimit+1)
			require.LessOrEqual(t, body.reads, 16)
		}
	}
}

func TestArgocdBackupTypedCASRefusesAmbiguousSnapshotsWithoutWriting(t *testing.T) {
	for _, snapshot := range []string{
		fmt.Sprintf(`{"data":{"data":{},"metadata":{"version":%d,"destroyed":false,"deletion_time":""}}}`, math.MaxInt),
		`{"data":{"data":{"unrelated":"\ud800"},"metadata":{"version":7,"destroyed":false,"deletion_time":""}}}`,
		`{"data":{"data":{"unrelated":"one","unrelated":"two"},"metadata":{"version":7,"destroyed":false,"deletion_time":""}}}`,
		`{"data":{"data":{},"metadata":{"version":7,"destroyed":false,"deletion_time":"deleted"}}}`,
		`{"data":{"data":{},"metadata":{"version":7,"destroyed":false}}}`,
		"{\"data\":\"\xff\"}",
	} {
		calls := 0
		client := backupTestClient(func(req *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, http.MethodGet, req.Method)
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader(snapshot))}, nil
		})
		_, err := client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, backupCustodyUpdates(t))
		require.Error(t, err)
		require.Equal(t, 1, calls)
	}
}

func TestLegacyInitializerSizeCompatibilityUnchangedByBackupWrapper(t *testing.T) {
	large := strings.Repeat("a", intakeResponseByteLimit+100)
	calls := 0
	client := backupTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method == http.MethodGet {
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"data":{"data":{"existing":"%s"},"metadata":{"version":3,"destroyed":false}}}`, large)))}, nil
		}
		var payload struct {
			Data    map[string]interface{} `json:"data"`
			Options map[string]int         `json:"options"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		require.Equal(t, large, payload.Data["existing"])
		require.Equal(t, 3, payload.Options["cas"])
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader(`{"data":{"version":4}}`))}, nil
	})
	version, err := client.InitializeSecretData(t.Context(), "secret/legacy", map[string]interface{}{"new_key": "fixture"})
	require.NoError(t, err)
	require.Equal(t, 4, version)
	require.Equal(t, 2, calls)
}

func TestArgocdBackupNativeTransportClonePreservesAuthorityAndDisablesReplay(t *testing.T) {
	roots := x509.NewCertPool()
	proxy := func(*http.Request) (*url.URL, error) { return url.Parse("http://proxy.invalid") }
	native := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "vault.invalid", MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}}, Proxy: proxy, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second, IdleConnTimeout: 5 * time.Second, ForceAttemptHTTP2: true}
	client := &VaultClient{httpClient: &http.Client{Transport: native, Timeout: 10 * time.Second}}
	isolated, closeTransport, err := client.backupIntakeClient()
	require.NoError(t, err)
	defer closeTransport()
	transport := isolated.Transport.(*http.Transport)
	require.NotSame(t, native, transport)
	require.NotSame(t, native.TLSClientConfig, transport.TLSClientConfig)
	require.Same(t, roots, transport.TLSClientConfig.RootCAs)
	require.Equal(t, "vault.invalid", transport.TLSClientConfig.ServerName)
	require.Equal(t, uint16(tls.VersionTLS12), transport.TLSClientConfig.MinVersion)
	require.Equal(t, reflect.ValueOf(proxy).Pointer(), reflect.ValueOf(transport.Proxy).Pointer())
	require.Equal(t, native.TLSHandshakeTimeout, transport.TLSHandshakeTimeout)
	require.Equal(t, native.ResponseHeaderTimeout, transport.ResponseHeaderTimeout)
	require.Equal(t, native.IdleConnTimeout, transport.IdleConnTimeout)
	require.Equal(t, 10*time.Second, isolated.Timeout)
	require.Nil(t, isolated.Jar)
	require.True(t, transport.DisableKeepAlives)
	require.False(t, transport.ForceAttemptHTTP2)
	require.True(t, transport.Protocols.HTTP1())
	require.False(t, transport.Protocols.HTTP2())
	require.Empty(t, transport.TLSNextProto)
	require.Equal(t, []string{"http/1.1"}, transport.TLSClientConfig.NextProtos)
	require.False(t, native.DisableKeepAlives)
	require.True(t, native.ForceAttemptHTTP2)
	require.Equal(t, []string{"h2", "http/1.1"}, native.TLSClientConfig.NextProtos)
}

type opaqueBackupTransport func(*http.Request) (*http.Response, error)

func (f opaqueBackupTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestArgocdBackupOpaqueTransportCannotClaimSingleAttempt(t *testing.T) {
	client := backupTestClient(nil)
	client.httpClient.Transport = opaqueBackupTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("opaque transport must remain held")
		return nil, nil
	})
	_, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), backupOriginFixture, 7)
	require.Error(t, err)
	_, err = client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, backupCustodyUpdates(t))
	require.Error(t, err)
}

func TestArgocdBackupActualNativeTransportHasNoHTTPReplay(t *testing.T) {
	for _, phase := range []string{"read", "ack"} {
		t.Run(phase, func(t *testing.T) {
			var dials atomic.Int64
			requests := make(chan string, 4)
			native := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				local, peer := net.Pipe()
				go func() {
					defer peer.Close()
					_ = peer.SetDeadline(time.Now().Add(5 * time.Second))
					req, err := http.ReadRequest(bufio.NewReader(peer))
					if err != nil {
						return
					}
					_, _ = io.Copy(io.Discard, req.Body)
					_ = req.Body.Close()
					requests <- req.Method
					if phase == "ack" && req.Method == http.MethodGet {
						_, _ = io.WriteString(peer, "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
					}
				}()
				return local, nil
			}}
			client := backupTestClient(nil)
			client.address = "http://vault.invalid"
			client.httpClient = &http.Client{Transport: native, Timeout: 5 * time.Second}
			if phase == "read" {
				_, err := client.ReadArgocdBackupEnvelopeKey(t.Context(), backupOriginFixture, 7)
				require.Error(t, err)
				require.Equal(t, int64(1), dials.Load())
				require.Equal(t, 1, len(requests))
				require.Equal(t, http.MethodGet, <-requests)
			} else {
				_, err := client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, backupCustodyUpdates(t))
				require.ErrorIs(t, err, ErrSecretInitializationOutcomeUnknown)
				require.Equal(t, int64(2), dials.Load())
				require.Equal(t, 2, len(requests))
				require.Equal(t, http.MethodGet, <-requests)
				require.Equal(t, http.MethodPost, <-requests)
			}
		})
	}
}

func TestArgocdBackupUnknownACKRetainsExpectedVersionWithoutQualifyingCommit(t *testing.T) {
	calls := 0
	client := backupTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method == http.MethodGet {
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader(`{"data":{"data":{"unrelated":"retained"},"metadata":{"version":6,"destroyed":false,"deletion_time":""}}}`))}, nil
		}
		return nil, errors.New("private-lost-ack-canary")
	})
	expected, err := client.InitializeArgocdBackupEnvelope(t.Context(), backupOriginFixture, backupCustodyUpdates(t))
	require.ErrorIs(t, err, ErrSecretInitializationOutcomeUnknown)
	require.Equal(t, 7, expected)
	require.Equal(t, 2, calls)
	require.NotContains(t, err.Error(), "canary")
	calls = 0
	legacy := backupTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method == http.MethodGet {
			return &http.Response{StatusCode: 404, ContentLength: 0, Body: http.NoBody}, nil
		}
		return nil, errors.New("private-lost-ack-canary")
	})
	version, err := legacy.InitializeSecretData(t.Context(), "secret/legacy", map[string]interface{}{"new_key": "fixture"})
	require.ErrorIs(t, err, ErrSecretInitializationOutcomeUnknown)
	require.Zero(t, version)
	require.Equal(t, 2, calls)
}
