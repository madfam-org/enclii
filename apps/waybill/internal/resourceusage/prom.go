package resourceusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Point is one sample of a series.
type Point struct {
	T time.Time
	V float64
}

// Series is one labelled series of a range or instant query. Instant
// queries return one point per series.
type Series struct {
	Labels map[string]string
	Points []Point
}

// Querier is the part of the Prometheus HTTP API the capture uses. Tests
// substitute a fake; production uses HTTPQuerier.
type Querier interface {
	// QueryRange evaluates query at start, start+step, ... <= end.
	QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]Series, error)
	// Query evaluates query at one instant.
	Query(ctx context.Context, query string, at time.Time) ([]Series, error)
}

// maxResponseBytes bounds one decoded response. A day is read in chunks so a
// response stays a few MB; a body past this limit is an error, not a
// truncated (and therefore wrong) day.
const maxResponseBytes = 64 << 20

// HTTPQuerier calls Prometheus' /api/v1/query and /api/v1/query_range.
type HTTPQuerier struct {
	base    *url.URL
	client  *http.Client
	timeout time.Duration
}

// NewHTTPQuerier returns a querier for the Prometheus at baseURL (no
// trailing /api/v1). timeout bounds each call.
func NewHTTPQuerier(baseURL string, timeout time.Duration) (*HTTPQuerier, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid Prometheus URL %q", baseURL)
	}
	return &HTTPQuerier{base: u, client: &http.Client{Timeout: timeout}, timeout: timeout}, nil
}

// QueryRange implements Querier.
func (q *HTTPQuerier) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]Series, error) {
	form := url.Values{}
	form.Set("query", query)
	form.Set("start", formatTime(start))
	form.Set("end", formatTime(end))
	form.Set("step", strconv.FormatFloat(step.Seconds(), 'f', -1, 64))
	form.Set("timeout", q.timeout.String())
	return q.do(ctx, "/api/v1/query_range", form)
}

// Query implements Querier.
func (q *HTTPQuerier) Query(ctx context.Context, query string, at time.Time) ([]Series, error) {
	form := url.Values{}
	form.Set("query", query)
	form.Set("time", formatTime(at))
	form.Set("timeout", q.timeout.String())
	return q.do(ctx, "/api/v1/query", form)
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', 3, 64)
}

// apiResponse is Prometheus' envelope. value/values carry [ts, "string"].
type apiResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string   `json:"metric"`
			Value  []json.RawMessage   `json:"value"`
			Values [][]json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func (q *HTTPQuerier) do(ctx context.Context, path string, form url.Values) ([]Series, error) {
	u := q.base.JoinPath(path)
	// POST keeps long PromQL out of URLs and access logs.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "enclii-waybill-aggregator (resource usage capture)")

	resp, err := q.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("prometheus %s: read body: %w", path, err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("prometheus %s: response over %d bytes; lower WAYBILL_RESOURCE_CHUNK_HOURS", path, maxResponseBytes)
	}
	return decodeResponse(path, resp.StatusCode, body)
}

func decodeResponse(path string, status int, body []byte) ([]Series, error) {
	var r apiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("prometheus %s: HTTP %d, undecodable body: %w", path, status, err)
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("prometheus %s: HTTP %d, %s: %s", path, status, r.ErrorType, r.Error)
	}
	out := make([]Series, 0, len(r.Data.Result))
	for _, res := range r.Data.Result {
		s := Series{Labels: res.Metric}
		switch r.Data.ResultType {
		case "matrix":
			s.Points = make([]Point, 0, len(res.Values))
			for _, v := range res.Values {
				p, ok, err := decodePoint(v)
				if err != nil {
					return nil, fmt.Errorf("prometheus %s: %w", path, err)
				}
				if ok {
					s.Points = append(s.Points, p)
				}
			}
		case "vector":
			p, ok, err := decodePoint(res.Value)
			if err != nil {
				return nil, fmt.Errorf("prometheus %s: %w", path, err)
			}
			if ok {
				s.Points = []Point{p}
			}
		default:
			return nil, fmt.Errorf("prometheus %s: unexpected result type %q", path, r.Data.ResultType)
		}
		out = append(out, s)
	}
	return out, nil
}

// decodePoint reads [<unix seconds>, "<value>"]. NaN and infinities are not
// measurements and are dropped (ok=false).
func decodePoint(raw []json.RawMessage) (Point, bool, error) {
	if len(raw) != 2 {
		return Point{}, false, errors.New("malformed sample")
	}
	var ts float64
	if err := json.Unmarshal(raw[0], &ts); err != nil {
		return Point{}, false, fmt.Errorf("malformed sample timestamp: %w", err)
	}
	var vs string
	if err := json.Unmarshal(raw[1], &vs); err != nil {
		return Point{}, false, fmt.Errorf("malformed sample value: %w", err)
	}
	v, err := strconv.ParseFloat(vs, 64)
	if err != nil {
		return Point{}, false, fmt.Errorf("malformed sample value %q", vs)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Point{}, false, nil
	}
	return Point{T: time.UnixMilli(int64(math.Round(ts * 1000))).UTC(), V: v}, true, nil
}
