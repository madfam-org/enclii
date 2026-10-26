package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/rest"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/argocdbackup"
)

// Native REST configuration supplies existing TLS/auth in memory. The closed
// capture profile derives every relative path; no raw object leaves the backend.
type argoProtectedSnapshotReader struct {
	base   string
	client *http.Client
}

func newArgoProtectedSnapshotReader(config *rest.Config) (*argoProtectedSnapshotReader, error) {
	if config == nil || config.Insecure {
		return nil, argocdbackup.ErrHold
	}
	copy := rest.CopyConfig(config)
	base, err := url.Parse(copy.Host)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, argocdbackup.ErrHold
	}
	copy.Timeout = 10 * time.Second
	client, err := rest.HTTPClientFor(copy)
	if err != nil {
		return nil, argocdbackup.ErrHold
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &argoProtectedSnapshotReader{strings.TrimSuffix(base.String(), "/"), client}, nil
}

func (r *argoProtectedSnapshotReader) Get(ctx context.Context, key argocdbackup.Key) ([]byte, error) {
	path, err := argocdbackup.NativeSnapshotPath(key, "", 0)
	if err != nil {
		return nil, argocdbackup.ErrHold
	}
	return r.read(ctx, path, argocdbackup.SnapshotObjectLimit)
}

func (r *argoProtectedSnapshotReader) List(ctx context.Context, kind string, limit int) ([]byte, error) {
	path, err := argocdbackup.NativeSnapshotPath(argocdbackup.Key{}, kind, limit)
	if err != nil {
		return nil, argocdbackup.ErrHold
	}
	return r.read(ctx, path, argocdbackup.MaxPlaintextBytes)
}

func (r *argoProtectedSnapshotReader) read(ctx context.Context, path string, limit int) ([]byte, error) {
	if r == nil || r.client == nil {
		return nil, argocdbackup.ErrHold
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+path, nil)
	if err != nil {
		return nil, argocdbackup.ErrHold
	}
	request.Header.Set("Accept", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return nil, argocdbackup.ErrHold
	}
	if response.StatusCode != http.StatusOK {
		limit = 64 << 10
	}
	body, err := readArgoProtectedResponse(response, limit)
	if err != nil || response.StatusCode != http.StatusOK {
		clear(body)
		return nil, argocdbackup.ErrHold
	}
	return body, nil
}

func readArgoProtectedResponse(response *http.Response, limit int) (body []byte, err error) {
	invalid := fmt.Errorf("protected snapshot response unqualified")
	if response == nil || response.Body == nil || limit <= 0 || limit > argocdbackup.MaxPlaintextBytes {
		return nil, invalid
	}
	defer func() {
		if response.Body.Close() != nil {
			err = invalid
		}
		if err != nil {
			clear(body)
			body = nil
		}
	}()
	if response.ContentLength < -1 || response.ContentLength > int64(limit) {
		return nil, invalid
	}
	if response.ContentLength >= 0 {
		limit = int(response.ContentLength)
	}
	body = make([]byte, limit+1)
	used, idle := 0, 0
	for {
		n, readErr := response.Body.Read(body[used:])
		if n < 0 || n > len(body)-used {
			return body, invalid
		}
		used += n
		if used > limit {
			return body, invalid
		}
		if readErr != nil {
			if readErr != io.EOF || (response.ContentLength >= 0 && int64(used) != response.ContentLength) {
				return body, invalid
			}
			return body[:used], nil
		}
		if n == 0 {
			idle++
			if idle >= 16 {
				return body, invalid
			}
		} else {
			idle = 0
		}
	}
}
