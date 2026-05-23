package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// apiRequest is the canonical HTTP helper for cmd files that talk to the
// Switchyard API but don't (yet) have a typed APIClient method. Existing
// per-feature wrappers (billingRequest, jobsRequest, junctionsRequest) predate
// this and are kept for backwards compatibility — new commands should use this
// helper. Path may be a relative path ("/v1/foo") or an absolute URL.
//
//	apiRequest(ctx, cfg, "GET", "/v1/teams", nil, &out)
//	apiRequest(ctx, cfg, "POST", "/v1/teams", payload, &result)
//
// Non-2xx responses produce an error containing the API's body so the user
// sees the server-side validation message.
func apiRequest(ctx context.Context, cfg *config.Config, method, path string, payload, out interface{}) error {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(b)
	}

	endpoint := path
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimRight(cfg.APIEndpoint, "/") + path
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "enclii-cli/"+Version)
	if cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	}

	resp, err := httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized — run `enclii login` to refresh credentials")
	}
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("forbidden — your account lacks permission for %s", path)
	}
	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "API %s %s → HTTP %d\n", method, path, resp.StatusCode)
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// apiRequestResponse is like apiRequest but returns the raw *http.Response.
// The caller must close resp.Body. Used by legacy timetable/junction helpers
// that still stream or decode manually.
func apiRequestResponse(ctx context.Context, cfg *config.Config, method, path string, payload interface{}) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(b)
	}

	endpoint := path
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimRight(cfg.APIEndpoint, "/") + path
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "enclii-cli/"+Version)
	if cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	}

	return httpClient().Do(req)
}

// emitJSON writes the value to stdout as indented JSON. Used by `--json`
// flags throughout the CLI for stable, scriptable output.
func emitJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// queryString builds a URL query string from a flat map. Empty values are
// omitted. Returns "" if the result would be empty (so callers can append it
// unconditionally without producing a trailing "?").
func queryString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	q := url.Values{}
	for k, v := range params {
		if v == "" {
			continue
		}
		q.Set(k, v)
	}
	enc := q.Encode()
	if enc == "" {
		return ""
	}
	return "?" + enc
}
