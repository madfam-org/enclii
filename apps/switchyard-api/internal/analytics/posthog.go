// Package analytics provides a PostHog client wrapper for server-side product
// analytics. The client degrades gracefully when no API key is configured --
// all Track / Identify calls become no-ops, so callers never need nil checks.
//
// Environment variables (via config.Config):
//
//	ENCLII_POSTHOG_API_KEY      - PostHog project API key (empty = disabled)
//	ENCLII_POSTHOG_ENDPOINT     - Ingestion endpoint (default: https://analytics.enclii.dev)
//
// Usage:
//
//	client := analytics.New(cfg.PostHogAPIKey, cfg.PostHogEndpoint)
//	defer client.Close()
//
//	client.Track("user_abc", "deployment.created", posthog.NewProperties().
//	    Set("project_id", proj.ID).
//	    Set("environment", "production"))
package analytics

import (
	"time"

	"github.com/posthog/posthog-go"
	"github.com/sirupsen/logrus"
)

// defaultEndpoint is the self-hosted PostHog ingestion URL reverse-proxied
// through Cloudflare so that ad-blockers never drop server-side events.
const defaultEndpoint = "https://analytics.enclii.dev"

// defaultFlushInterval controls how often the SDK sends batched events.
const defaultFlushInterval = 30 * time.Second

// defaultBatchSize is the maximum number of events queued before a flush.
const defaultBatchSize = 100

// Client wraps the PostHog Go SDK. When disabled (no API key), every method
// is a safe no-op so callers do not need conditional logic.
type Client struct {
	ph      posthog.Client
	enabled bool
	logger  *logrus.Logger
}

// New creates a PostHog analytics client. If apiKey is empty the client is
// created in disabled mode -- all methods become no-ops. The endpoint
// parameter falls back to defaultEndpoint when empty.
func New(apiKey, endpoint string, logger *logrus.Logger) *Client {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	if apiKey == "" {
		logger.Info("PostHog analytics disabled (no API key configured)")
		return &Client{enabled: false, logger: logger}
	}

	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	client, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint:  endpoint,
		BatchSize: defaultBatchSize,
		Interval:  defaultFlushInterval,
		// Use a verbose logger in development; the SDK logger only fires on
		// actual errors so the volume is low.
		Logger: &posthogLogger{logger: logger},
	})
	if err != nil {
		logger.WithError(err).Warn("Failed to initialize PostHog client (analytics disabled)")
		return &Client{enabled: false, logger: logger}
	}

	logger.WithFields(logrus.Fields{
		"endpoint":   endpoint,
		"batch_size": defaultBatchSize,
		"flush_sec":  int(defaultFlushInterval.Seconds()),
	}).Info("PostHog analytics client initialized")

	return &Client{ph: client, enabled: true, logger: logger}
}

// Track captures a named event associated with a distinct user.
// Properties should contain event-specific metadata (project_id, env, etc.).
func (c *Client) Track(distinctID, event string, properties posthog.Properties) {
	if !c.enabled {
		return
	}
	if err := c.ph.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      event,
		Properties: properties,
	}); err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"event":       event,
			"distinct_id": distinctID,
		}).Warn("Failed to enqueue PostHog event")
	}
}

// Identify associates properties (email, org, plan) with a distinct user so
// that all subsequent events carry those traits.
func (c *Client) Identify(distinctID string, properties posthog.Properties) {
	if !c.enabled {
		return
	}
	if err := c.ph.Enqueue(posthog.Identify{
		DistinctId: distinctID,
		Properties: properties,
	}); err != nil {
		c.logger.WithError(err).WithField("distinct_id", distinctID).
			Warn("Failed to enqueue PostHog identify")
	}
}

// GroupIdentify associates properties with a named group (e.g. an
// organization). This is used for B2B analytics where events roll up to an
// org-level dashboard.
func (c *Client) GroupIdentify(groupType, groupKey string, properties posthog.Properties) {
	if !c.enabled {
		return
	}
	if err := c.ph.Enqueue(posthog.GroupIdentify{
		Type:       groupType,
		Key:        groupKey,
		Properties: properties,
	}); err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"group_type": groupType,
			"group_key":  groupKey,
		}).Warn("Failed to enqueue PostHog group identify")
	}
}

// IsFeatureEnabled checks a PostHog feature flag for the given distinct user.
// Returns false when the client is disabled or the flag lookup fails.
func (c *Client) IsFeatureEnabled(distinctID, flagKey string) bool {
	if !c.enabled {
		return false
	}
	enabled, err := c.ph.IsFeatureEnabled(posthog.FeatureFlagPayload{
		Key:        flagKey,
		DistinctId: distinctID,
	})
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"flag":        flagKey,
			"distinct_id": distinctID,
		}).Debug("PostHog feature flag check failed, defaulting to false")
		return false
	}
	return enabled
}

// Enabled reports whether the client is actively sending events.
func (c *Client) Enabled() bool {
	return c.enabled
}

// Close flushes pending events and releases resources. Safe to call on a
// disabled client.
func (c *Client) Close() {
	if !c.enabled {
		return
	}
	if err := c.ph.Close(); err != nil {
		c.logger.WithError(err).Warn("Error closing PostHog client")
	}
}

// posthogLogger adapts logrus to the posthog.Logger interface.
type posthogLogger struct {
	logger *logrus.Logger
}

func (l *posthogLogger) Logf(format string, args ...interface{}) {
	l.logger.Debugf("[posthog] "+format, args...)
}

func (l *posthogLogger) Errorf(format string, args ...interface{}) {
	l.logger.Errorf("[posthog] "+format, args...)
}
