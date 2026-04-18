package logstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LokiClient wraps the subset of Loki's HTTP API we need: query_range
// for windowed scans and tail (over WebSocket) for live streaming. We
// don't expose Loki's full surface to callers — everything is funneled
// through BuildQuery so the LogQL we emit is auditable.
//
// LOKI_URL defaults to the in-cluster DNS name. In local-dev you can
// port-forward Loki and point ENCLII_LOKI_URL at http://localhost:3100.
type LokiClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewLokiClient constructs a client with a sane default HTTP timeout.
// Use the WebSocket methods (below) for tail — they need their own
// lifecycle so we never tie them to the HTTP client's Timeout.
func NewLokiClient(baseURL string) *LokiClient {
	return &LokiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ErrLokiUnavailable is returned when Loki can't be reached. Callers
// use errors.Is(err, ErrLokiUnavailable) to decide whether to surface
// a 503 vs a 500. We keep this sentinel separate from wrapping errors
// so we don't leak Loki's error-body text to end users.
var ErrLokiUnavailable = errors.New("loki unavailable")

// lokiQueryResponse mirrors the "streams" path of the v1 query_range
// response. Loki returns values as [timestampNs, line]; we normalize
// to RFC3339Nano + string on ingress.
//
// Ref: https://grafana.com/docs/loki/latest/reference/api/#query-logs-within-a-range-of-time
type lokiQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"` // [[timestamp_ns, line], ...]
		} `json:"result"`
		Stats json.RawMessage `json:"stats,omitempty"`
	} `json:"data"`
}

// QueryRange calls Loki's /loki/api/v1/query_range. Loki returns
// entries in descending order when `direction=backward` and ascending
// otherwise. We use backward+then-reverse so the final slice is
// chronological — matches what users expect from a tail.
func (c *LokiClient) QueryRange(ctx context.Context, logql string, start, end time.Time, limit int) ([]Entry, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("%w: baseURL empty", ErrLokiUnavailable)
	}

	u, err := url.Parse(c.baseURL + "/loki/api/v1/query_range")
	if err != nil {
		return nil, fmt.Errorf("parse loki url: %w", err)
	}
	q := u.Query()
	q.Set("query", logql)
	q.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	q.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("direction", "backward")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network-level failure — surface as Unavailable so the handler
		// can 503 the caller.
		return nil, fmt.Errorf("%w: %v", ErrLokiUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: loki returned %d", ErrLokiUnavailable, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		// 4xx means our query was malformed — programmer error, not
		// infra. Bubble up with the body so tests can assert.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("loki 4xx: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed lokiQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode loki response: %w", err)
	}

	// Flatten streams and normalize to Entry. We interleave by timestamp
	// because different pods live in different streams.
	entries := make([]Entry, 0, limit)
	for _, stream := range parsed.Data.Result {
		for _, v := range stream.Values {
			if len(v) != 2 {
				continue
			}
			ns, err := strconv.ParseInt(v[0], 10, 64)
			if err != nil {
				continue
			}
			entries = append(entries, Entry{
				Timestamp: time.Unix(0, ns).UTC(),
				Level:     DetectLevel(stream.Stream, v[1]),
				Pod:       stream.Stream["pod"],
				Container: stream.Stream["container"],
				Message:   v[1],
				Labels:    stream.Stream,
			})
		}
	}

	// Ascending by timestamp so the UI renders oldest-first, newest at
	// the bottom where the user's eye naturally lives when tailing.
	sortByTimestampAsc(entries)
	return entries, nil
}

// sortByTimestampAsc — small inline sort to avoid importing sort for
// one call site. insertion sort is fine at n ≤ MaxLimit = 2000 and
// avoids a heap alloc.
func sortByTimestampAsc(a []Entry) {
	for i := 1; i < len(a); i++ {
		x := a[i]
		j := i - 1
		for j >= 0 && a[j].Timestamp.After(x.Timestamp) {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = x
	}
}

// TailStream is the minimal interface the handler needs from a tail
// implementation. Defined here so the handler takes an interface (easy
// to mock in tests) and the real impl lives in loki_tail.go.
type TailStream interface {
	// Recv blocks until the next entry or an error. io.EOF signals the
	// server closed the connection.
	Recv() (Entry, error)
	// Close releases resources. Safe to call multiple times.
	Close() error
}
