package compliance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// Exporter sends compliance evidence to external systems (Vanta, Drata, etc.)
type Exporter struct {
	httpClient *http.Client
	logger     *logrus.Logger
	maxRetries int
	retryDelay time.Duration
	enabled    bool
}

// Config holds configuration for compliance exports
type Config struct {
	Enabled      bool
	VantaWebhook string
	DrataWebhook string
	MaxRetries   int
	RetryDelay   time.Duration
}

// NewExporter creates a new compliance exporter
func NewExporter(cfg *Config, logger *logrus.Logger) *Exporter {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 2 * time.Second
	}

	return &Exporter{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:     logger,
		maxRetries: cfg.MaxRetries,
		retryDelay: cfg.RetryDelay,
		enabled:    cfg.Enabled,
	}
}

// DeploymentEvidence represents compliance evidence for a deployment
type DeploymentEvidence struct {
	// Event metadata
	EventType string    `json:"event_type"`
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`

	// Service information
	ServiceName string `json:"service_name"`
	Environment string `json:"environment"`
	ProjectName string `json:"project_name"`

	// Deployment details
	DeploymentID   string `json:"deployment_id"`
	ReleaseVersion string `json:"release_version"`
	ImageURI       string `json:"image_uri"`

	// Source control
	GitSHA        string `json:"git_sha"`
	GitRepo       string `json:"git_repo"`
	CommitMessage string `json:"commit_message,omitempty"`

	// Provenance (from PR Approval Tracking)
	PRURL        string    `json:"pr_url,omitempty"`
	PRNumber     int       `json:"pr_number,omitempty"`
	ApprovedBy   string    `json:"approved_by,omitempty"`
	ApprovedAt   time.Time `json:"approved_at,omitempty"`
	CIStatus     string    `json:"ci_status,omitempty"`
	ChangeTicket string    `json:"change_ticket,omitempty"`

	// Deployment actor
	DeployedBy      string    `json:"deployed_by"`
	DeployedByEmail string    `json:"deployed_by_email"`
	DeployedAt      time.Time `json:"deployed_at"`

	// Supply chain security
	SBOM              string `json:"sbom,omitempty"`
	SBOMFormat        string `json:"sbom_format,omitempty"`
	ImageSignature    string `json:"image_signature,omitempty"`
	SignatureVerified bool   `json:"signature_verified,omitempty"`

	// Compliance receipt (cryptographic proof)
	ComplianceReceipt string `json:"compliance_receipt,omitempty"`
	ReceiptSignature  string `json:"receipt_signature,omitempty"`
}

// ExportResult represents the result of exporting evidence
type ExportResult struct {
	Success      bool
	Provider     string // "vanta", "drata"
	Attempts     int
	Error        error
	ResponseCode int
	ResponseBody string
}

// SendWebhook sends a webhook to a URL with retry logic
func (e *Exporter) SendWebhook(ctx context.Context, url string, payload interface{}, provider string) *ExportResult {
	result := &ExportResult{
		Provider: provider,
	}

	if !e.enabled {
		e.logger.Debug("Compliance webhooks disabled, skipping export")
		result.Success = true // Not an error, just disabled
		return result
	}

	if url == "" {
		e.logger.Debugf("%s webhook URL not configured, skipping", provider)
		result.Success = true // Not an error, just not configured
		return result
	}

	// Marshal payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		result.Error = fmt.Errorf("failed to marshal payload: %w", err)
		return result
	}

	// Retry loop with exponential backoff
	for attempt := 1; attempt <= e.maxRetries; attempt++ {
		result.Attempts = attempt

		e.logger.Infof("Sending compliance evidence to %s (attempt %d/%d)", provider, attempt, e.maxRetries)

		// Create request
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			result.Error = fmt.Errorf("failed to create request: %w", err)
			e.logger.Warnf("Failed to create %s request: %v", provider, err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Enclii-Switchyard/1.0")

		// Send request
		resp, err := e.httpClient.Do(req)
		if err != nil {
			result.Error = fmt.Errorf("failed to send request: %w", err)
			e.logger.Warnf("Failed to send %s webhook: %v", provider, err)

			// Retry with exponential backoff
			if attempt < e.maxRetries {
				backoff := e.retryDelay * time.Duration(attempt)
				e.logger.Infof("Retrying in %v...", backoff)
				time.Sleep(backoff)
			}
			continue
		}
		defer func() { _ = resp.Body.Close() }()

		result.ResponseCode = resp.StatusCode

		// Read response body
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		result.ResponseBody = buf.String()

		// Check status code
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			result.Success = true
			e.logger.Infof("✓ Successfully sent compliance evidence to %s (status: %d)", provider, resp.StatusCode)
			return result
		}

		// Non-2xx status code
		result.Error = fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, result.ResponseBody)
		e.logger.Warnf("Webhook failed with status %d: %s", resp.StatusCode, result.ResponseBody)

		// Retry on 5xx errors (server errors), don't retry on 4xx (client errors)
		if resp.StatusCode >= 500 && attempt < e.maxRetries {
			backoff := e.retryDelay * time.Duration(attempt)
			e.logger.Infof("Retrying in %v...", backoff)
			time.Sleep(backoff)
			continue
		}

		// 4xx error or final attempt - don't retry
		break
	}

	return result
}

// ExportDeployment exports deployment evidence to all configured providers
func (e *Exporter) ExportDeployment(ctx context.Context, evidence *DeploymentEvidence, vantaURL, drataURL string) map[string]*ExportResult {
	results := make(map[string]*ExportResult)

	// Export to Vanta (if configured)
	if vantaURL != "" {
		vantaPayload := FormatForVanta(evidence)
		results["vanta"] = e.SendWebhook(ctx, vantaURL, vantaPayload, "Vanta")
	}

	// Export to Drata (if configured)
	if drataURL != "" {
		drataPayload := FormatForDrata(evidence)
		results["drata"] = e.SendWebhook(ctx, drataURL, drataPayload, "Drata")
	}

	return results
}

// IsEnabled returns whether compliance exports are enabled
func (e *Exporter) IsEnabled() bool {
	return e.enabled
}

// LogExportResults logs the results of exports
func (e *Exporter) LogExportResults(results map[string]*ExportResult) {
	for provider, result := range results {
		if result.Success {
			e.logger.Infof("✓ %s export successful", provider)
		} else {
			e.logger.Errorf("✗ %s export failed: %v", provider, result.Error)
		}
	}
}
