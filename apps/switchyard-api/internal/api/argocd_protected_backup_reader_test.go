package api

import (
	"errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/argocdbackup"
	"github.com/stretchr/testify/require"
	"io"
	"k8s.io/client-go/rest"
	"net/http"
	"strings"
	"testing"
)

type protectedReaderTransport func(*http.Request) (*http.Response, error)

func (f protectedReaderTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type protectedReaderBody struct {
	reader        io.Reader
	bytes, closed int
	closeErr      error
}

func (b *protectedReaderBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.bytes += n
	return n, err
}
func (b *protectedReaderBody) Close() error { b.closed++; return b.closeErr }

type protectedReaderInfinite struct{}

func (protectedReaderInfinite) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
func TestProtectedSnapshotNativeReaderBoundsAllStatusPathsBeforeJSON(t *testing.T) {
	for status := 100; status <= 599; status++ {
		body := &protectedReaderBody{reader: protectedReaderInfinite{}}
		calls := 0
		reader := &argoProtectedSnapshotReader{base: "https://fixture.invalid", client: &http.Client{Transport: protectedReaderTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, http.MethodGet, req.Method)
			require.Equal(t, "/api/v1/namespaces/argocd/secrets", req.URL.Path)
			require.Equal(t, "limit=128", req.URL.RawQuery)
			return &http.Response{StatusCode: status, ContentLength: -1, Body: body}, nil
		})}}
		// Call the bounded reader with a small explicit success ceiling; production
		// Get/List choose only the fixed 2/32 MiB profile ceilings.
		_, err := reader.read(t.Context(), "/api/v1/namespaces/argocd/secrets?limit=128", 64<<10)
		require.Error(t, err)
		require.Equal(t, 1, calls)
		require.Equal(t, 1, body.closed)
		require.LessOrEqual(t, body.bytes, 65537)
	}
	for _, length := range []int64{2 << 20, -1} {
		body := &protectedReaderBody{reader: strings.NewReader(`{"private":"canary"}`), closeErr: errors.New("private-close-canary")}
		_, err := readArgoProtectedResponse(&http.Response{ContentLength: length, Body: body}, 65536)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "canary")
		require.Equal(t, 1, body.closed)
	}
}
func TestProtectedSnapshotNativeReaderIsClosedToServerHeldAuthority(t *testing.T) {
	for _, host := range []string{"http://fixture.invalid", "https://user:pass@fixture.invalid", "https://fixture.invalid/path", "https://fixture.invalid?query=1"} {
		_, err := newArgoProtectedSnapshotReader(&rest.Config{Host: host})
		require.Error(t, err)
	}
	reader := &argoProtectedSnapshotReader{base: "https://fixture.invalid", client: &http.Client{Transport: protectedReaderTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("foreign selector must never dispatch")
		return nil, nil
	})}}
	_, err := reader.Get(t.Context(), argocdbackup.Key{APIVersion: "v1", Kind: "Secret", Namespace: "other", Name: "private"})
	require.Error(t, err)
	_, err = reader.List(t.Context(), "Secret", 129)
	require.Error(t, err)
	_, err = reader.List(t.Context(), "Node", 128)
	require.Error(t, err)
}
