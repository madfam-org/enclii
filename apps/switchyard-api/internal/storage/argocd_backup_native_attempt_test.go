package storage

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestArgoBackupNativeTransportPreservesAuthorityWithoutPoolingOrCookies(t *testing.T) {
	roots := x509.NewCertPool()
	proxy := func(*http.Request) (*url.URL, error) { return url.Parse("http://fixture.invalid") }
	native := &http.Transport{Proxy: proxy, TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "fixture.invalid", MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}}, TLSHandshakeTimeout: 3 * time.Second, ForceAttemptHTTP2: true}
	owned, err := argoBackupNativeTransport(native)
	require.NoError(t, err)
	defer owned.CloseIdleConnections()
	require.NotSame(t, native, owned)
	require.Same(t, roots, owned.TLSClientConfig.RootCAs)
	require.Equal(t, "fixture.invalid", owned.TLSClientConfig.ServerName)
	require.Equal(t, uint16(tls.VersionTLS12), owned.TLSClientConfig.MinVersion)
	require.Equal(t, reflect.ValueOf(proxy).Pointer(), reflect.ValueOf(owned.Proxy).Pointer())
	require.Equal(t, native.TLSHandshakeTimeout, owned.TLSHandshakeTimeout)
	require.True(t, owned.DisableKeepAlives)
	require.False(t, owned.ForceAttemptHTTP2)
	require.True(t, owned.Protocols.HTTP1())
	require.False(t, owned.Protocols.HTTP2())
	require.Empty(t, owned.TLSNextProto)
	require.Equal(t, []string{"http/1.1"}, owned.TLSClientConfig.NextProtos)
	require.False(t, native.DisableKeepAlives)
	require.True(t, native.ForceAttemptHTTP2)
	_, ref := argoBackupFixture(t)
	client := argoBackupClient(argoBackupFakeTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("unused") }), io.Discard)
	original := client.client.Options()
	httpClient := original.HTTPClient.(*http.Client)
	httpClient.Jar = argoCookieJar{}
	options, closeIdle, err := client.argoBackupOptions(ref, http.MethodPut)
	require.NoError(t, err)
	defer closeIdle()
	var configured s3.Options
	options(&configured)
	require.Nil(t, configured.HTTPClient.(*http.Client).Jar)
	require.NotNil(t, httpClient.Jar)
}

type argoCookieJar struct{}

func (argoCookieJar) SetCookies(*url.URL, []*http.Cookie) {}
func (argoCookieJar) Cookies(*url.URL) []*http.Cookie {
	return []*http.Cookie{{Name: "private-cookie-canary", Value: "fixture"}}
}

type argoOpaqueTransport func(*http.Request) (*http.Response, error)

func (f argoOpaqueTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestArgoBackupOpaqueTransportHeldBeforeAnyProviderAttempt(t *testing.T) {
	sealed, _ := argoBackupFixture(t)
	calls := 0
	client := argoBackupClient(argoOpaqueTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("private-provider-canary")
	}), io.Discard)
	require.ErrorIs(t, client.PutArgoBackupCiphertextOnce(t.Context(), sealed), ErrArgoBackupHeld)
	require.Zero(t, calls)
}
func TestArgoBackupActualNativeTransportDoesNotReplayUnknownPUTOrGET(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			var dials atomic.Int64
			requests := make(chan string, 2)
			native := &http.Transport{Proxy: nil, DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
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
				}()
				return local, nil
			}}
			client := argoBackupClient(native, io.Discard)
			sealed, ref := argoBackupFixture(t)
			if method == http.MethodPut {
				require.ErrorIs(t, client.PutArgoBackupCiphertextOnce(t.Context(), sealed), ErrArgoBackupOutcomeUnknown)
			} else {
				_, err := client.ReadArgoBackupCiphertext(t.Context(), ref)
				require.ErrorIs(t, err, ErrArgoBackupHeld)
			}
			require.Equal(t, int64(1), dials.Load())
			require.Equal(t, 1, len(requests))
			require.Equal(t, method, <-requests)
		})
	}
}
func TestTenantExportStorageCannotSelectPresignDeleteOrUploadBackupPrefix(t *testing.T) {
	store := (&R2Client{}).TenantExportStorage()
	for _, key := range []string{"argocd-backups", "argocd-backups/v1/namespace/operation/backup.aes256gcm", "/argocd-backups/v1/fixture"} {
		require.Error(t, store.Upload(t.Context(), key, bytes.NewReader(nil), "application/octet-stream"))
		require.Error(t, store.Delete(t.Context(), key))
		url, err := store.GetPresignedURL(t.Context(), key, time.Minute)
		require.Error(t, err)
		require.Empty(t, url)
	}
	for _, key := range []string{"tenant-exports/fixture/id/part.tar.gz", "artifacts/fixture/file", "build-logs/fixture/log", "project-data/fixture/data"} {
		require.True(t, allowedTenantExportKey(key))
	}
}
