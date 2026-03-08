package client

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// LogOptions configures log retrieval.
type LogOptions struct {
	Follow bool
	Lines  int
	Since  *time.Time
}

// LogLine represents a single log entry.
type LogLine struct {
	Timestamp time.Time `json:"timestamp"`
	Pod       string    `json:"pod"`
	Message   string    `json:"message"`
	Level     string    `json:"level,omitempty"`
}

// GetLogs retrieves logs for a deployment.
func (c *Client) GetLogs(ctx context.Context, deploymentID string, opts LogOptions) ([]LogLine, error) {
	endpoint := fmt.Sprintf("/v1/deployments/%s/logs", deploymentID)
	params := url.Values{}
	if opts.Follow {
		params.Set("follow", "true")
	}
	if opts.Lines > 0 {
		params.Set("lines", fmt.Sprintf("%d", opts.Lines))
	}
	if opts.Since != nil {
		params.Set("since", opts.Since.Format(time.RFC3339))
	}
	if q := params.Encode(); q != "" {
		endpoint += "?" + q
	}

	var resp struct {
		Logs []LogLine `json:"logs"`
	}
	if err := c.get(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("get logs: %w", err)
	}
	return resp.Logs, nil
}
