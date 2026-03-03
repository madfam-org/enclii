package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// R2Provisioner creates Cloudflare R2 buckets.
type R2Provisioner struct {
	apiToken   string
	accountID  string
	logger     logging.Logger
	httpClient *http.Client
}

// NewR2Provisioner creates a new R2 provisioner.
func NewR2Provisioner(apiToken, accountID string, logger logging.Logger) *R2Provisioner {
	return &R2Provisioner{
		apiToken:   apiToken,
		accountID:  accountID,
		logger:     logger,
		httpClient: &http.Client{},
	}
}

// r2BucketRequest is the Cloudflare API request body for creating an R2 bucket.
type r2BucketRequest struct {
	Name string `json:"name"`
}

// r2APIResponse is the Cloudflare API response wrapper.
type r2APIResponse struct {
	Success bool        `json:"success"`
	Errors  []r2APIErr  `json:"errors"`
	Result  interface{} `json:"result"`
}

type r2APIErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CreateBucket creates an R2 bucket and returns the R2 credential entries to add to the K8s secret.
func (p *R2Provisioner) CreateBucket(ctx context.Context, bucketName string) ([]types.SecretEntry, error) {
	if p.apiToken == "" || p.accountID == "" {
		return nil, fmt.Errorf("R2 provisioning requires CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID")
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/r2/buckets", p.accountID)

	body, err := json.Marshal(r2BucketRequest{Name: bucketName})
	if err != nil {
		return nil, fmt.Errorf("marshal R2 request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create R2 request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("R2 API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// 409 = bucket already exists, treat as success
	if resp.StatusCode == http.StatusConflict {
		p.logger.Info(ctx, "R2 bucket already exists", logging.String("bucket", bucketName))
	} else if resp.StatusCode >= 300 {
		var apiResp r2APIResponse
		if err := json.Unmarshal(respBody, &apiResp); err == nil && len(apiResp.Errors) > 0 {
			return nil, fmt.Errorf("R2 API error: %s (code %d)", apiResp.Errors[0].Message, apiResp.Errors[0].Code)
		}
		return nil, fmt.Errorf("R2 API returned status %d: %s", resp.StatusCode, string(respBody))
	} else {
		p.logger.Info(ctx, "Created R2 bucket", logging.String("bucket", bucketName))
	}

	// Return credential entries to add to the K8s secret
	endpointURL := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", p.accountID)
	return []types.SecretEntry{
		{Key: "R2_BUCKET_NAME", Value: bucketName},
		{Key: "R2_ENDPOINT_URL", Value: endpointURL},
		{Key: "STORAGE_BACKEND", Value: "r2"},
	}, nil
}
