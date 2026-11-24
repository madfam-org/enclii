// Package waybillmcp holds Enclii's cost-ledger tools for the native MCP
// (RFC 0051, wave A2): reads of Waybill's daily resource ledger
// (RFC 0049 Phase 2), usage and requests only, never money.
//
// Waybill has no route of its own on the internet and no Janua verification;
// switchyard-api already reaches it in-cluster for the billing proxy. Each
// tool therefore calls the Waybill HTTP route it covers (coverage.go) with
// the same base URL and service key as the billing proxy, and Waybill's own
// handler parses and validates the request: one implementation, no back
// door. The tool's output is a projection of that route's JSON, field by
// field as the output schema lists it, so a field Waybill adds later does not
// reach MCP callers until the drift guard and this file say so.
package waybillmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/mcp"
)

// MCP tool names. Anthropic tool names allow no dots, so the RFC 0051
// catalog names (usage.resource_daily, usage.node_daily) are the titles.
const (
	ToolResourceDaily = "usage_resource_daily"
	ToolNodeDaily     = "usage_node_daily"
)

// The Waybill routes the tools read.
const (
	PathResourceDaily = "/api/v1/resource-usage/daily"
	PathNodeDaily     = "/api/v1/resource-usage/nodes/daily"
)

// MaxUpstreamBytes caps one Waybill answer the MCP returns. A day of every
// namespace is a few hundred KiB; a long range of every namespace is not an
// MCP answer, so the caller narrows it.
const MaxUpstreamBytes = 4 << 20

// upstreamTimeout matches the billing proxy's.
const upstreamTimeout = 15 * time.Second

var (
	resourceDailyParams = []string{"from", "to", "namespace", "cluster"}
	nodeDailyParams     = []string{"from", "to", "cluster"}
	civilDate           = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Client reaches Waybill the way the billing proxy does.
type Client struct {
	// BaseURL is ENCLII_WAYBILL_BASE_URL (e.g. http://waybill).
	BaseURL string
	// APIKey is ENCLII_WAYBILL_INTERNAL_API_KEY, sent as X-API-Key when set.
	APIKey string
	// HTTP defaults to a client with the billing proxy's timeout.
	HTTP *http.Client
}

// Tools returns the read tools over c.
func Tools(c *Client) []mcp.Tool {
	return []mcp.Tool{
		{
			Name:  ToolResourceDaily,
			Title: "usage.resource_daily: daily workload usage and requests",
			Description: "Waybill's daily resource ledger per workload: CPU core-hours and memory GiB-hours used and " +
				"requested, limits, p95 and max, pod count and sample coverage, per civil day, node, namespace, workload " +
				"and container. Usage and requests only, no money. Same rows and filters as GET " +
				PathResourceDaily + ": from and to are civil dates (YYYY-MM-DD, both inclusive, at most 62 days) in the " +
				"ledger's time zone. captures says how much of each day Prometheus held; uncaptured_dates are days with " +
				"no capture yet, not zeros. CI runner namespaces are one row per namespace.",
			InputSchema:  resourceDailyInputSchema(),
			OutputSchema: resourceDailyOutputSchema(),
			Call: func(ctx context.Context, req mcp.Request) (any, error) {
				return c.read(ctx, req, PathResourceDaily, resourceDailyParams, resourceDailyOutputSchema())
			},
		},
		{
			Name:  ToolNodeDaily,
			Title: "usage.node_daily: daily node capacity, usage and requests",
			Description: "Waybill's daily resource ledger per node: allocatable CPU core-hours and memory GiB-hours, " +
				"what workloads requested and used, p95 and max, pod count and sample coverage, per civil day. Usage and " +
				"requests only, no money (there is no node cost yet). Same rows and filters as GET " + PathNodeDaily +
				": from and to are civil dates (YYYY-MM-DD, both inclusive, at most 62 days) in the ledger's time zone.",
			InputSchema:  nodeDailyInputSchema(),
			OutputSchema: nodeDailyOutputSchema(),
			Call: func(ctx context.Context, req mcp.Request) (any, error) {
				return c.read(ctx, req, PathNodeDaily, nodeDailyParams, nodeDailyOutputSchema())
			},
		},
	}
}

// errUpstream is a Waybill failure the caller cannot fix.
var errUpstream = errors.New("waybill read failed")

func (c *Client) read(ctx context.Context, req mcp.Request, path string, params []string, out map[string]any) (any, error) {
	if err := req.OnlyArgs(params...); err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, name := range params {
		v, ok, err := req.String(name)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if len(v) > 253 {
			return nil, mcp.Invalid("%s is longer than 253 characters", name)
		}
		q.Set(name, v)
	}
	for _, name := range []string{"from", "to"} {
		if !civilDate.MatchString(q.Get(name)) {
			return nil, mcp.Invalid("from and to are required civil dates (YYYY-MM-DD, both inclusive)")
		}
	}

	status, body, err := c.get(ctx, path, q)
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusOK:
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%w: undecodable answer: %v", errUpstream, err)
		}
		return project(v, out), nil
	case status == http.StatusBadRequest:
		// Waybill's handler refused the arguments (bad date, from after to,
		// longer than 62 days, bad namespace or cluster): the caller fixes it.
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) != nil || e.Error == "" {
			e.Error = "Waybill refused the arguments"
		}
		if len(e.Error) > 200 {
			e.Error = e.Error[:200]
		}
		return nil, mcp.Invalid("%s", e.Error)
	default:
		return nil, fmt.Errorf("%w: GET %s answered %d", errUpstream, path, status)
	}
}

func (c *Client) get(ctx context.Context, path string, q url.Values) (int, []byte, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		return 0, nil, fmt.Errorf("%w: no Waybill base URL", errUpstream)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+q.Encode(), nil)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", errUpstream, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "switchyard-mcp/1.0")
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: upstreamTimeout}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", errUpstream, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxUpstreamBytes+1))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", errUpstream, err)
	}
	if len(body) > MaxUpstreamBytes {
		return 0, nil, mcp.Invalid("the answer is larger than %d MiB: narrow from and to, or pass namespace", MaxUpstreamBytes>>20)
	}
	return resp.StatusCode, body, nil
}

// project keeps, recursively, only what schema lists: an object's listed
// properties and each array item projected by items.
func project(v any, schema map[string]any) any {
	if props, ok := schema["properties"].(map[string]any); ok {
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		out := make(map[string]any, len(props))
		for name, sub := range props {
			if val, present := m[name]; present {
				out[name] = project(val, sub.(map[string]any))
			}
		}
		return out
	}
	if items, ok := schema["items"].(map[string]any); ok {
		arr, ok := v.([]any)
		if !ok {
			return v
		}
		out := make([]any, len(arr))
		for i, val := range arr {
			out[i] = project(val, items)
		}
		return out
	}
	return v
}
