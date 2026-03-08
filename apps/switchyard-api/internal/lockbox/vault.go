package lockbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VaultClient handles interactions with HashiCorp Vault
type VaultClient struct {
	address    string
	token      string
	namespace  string
	httpClient *http.Client
	enabled    bool
}

// NewVaultClient creates a new Vault client
func NewVaultClient(cfg *VaultConfig) *VaultClient {
	if cfg == nil {
		return &VaultClient{enabled: false}
	}

	return &VaultClient{
		address:   strings.TrimSuffix(cfg.Address, "/"),
		token:     cfg.Token,
		namespace: cfg.Namespace,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		enabled: cfg.Enabled,
	}
}

// VaultSecretData represents Vault KV v2 response structure
type VaultSecretData struct {
	Data struct {
		Data     map[string]interface{} `json:"data"`
		Metadata struct {
			Version     int       `json:"version"`
			CreatedTime time.Time `json:"created_time"`
			Destroyed   bool      `json:"destroyed"`
		} `json:"metadata"`
	} `json:"data"`
}

// VaultSecretMetadata represents Vault secret metadata response
type VaultSecretMetadata struct {
	Data struct {
		CurrentVersion int                         `json:"current_version"`
		Versions       map[string]VaultVersionInfo `json:"versions"`
		CreatedTime    time.Time                   `json:"created_time"`
		UpdatedTime    time.Time                   `json:"updated_time"`
	} `json:"data"`
}

// VaultVersionInfo contains version-specific metadata
type VaultVersionInfo struct {
	Version     int       `json:"version"`
	CreatedTime time.Time `json:"created_time"`
	Destroyed   bool      `json:"destroyed"`
}

// GetSecret retrieves a secret from Vault
func (v *VaultClient) GetSecret(ctx context.Context, path string) (*Secret, error) {
	if !v.enabled {
		return nil, fmt.Errorf("Vault client is disabled")
	}

	// Vault KV v2 path format: /v1/secret/data/path
	url := fmt.Sprintf("%s/v1/%s", v.address, path)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Vault-Token", v.token)
	if v.namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.namespace)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve secret: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault returned status %d: %s", resp.StatusCode, string(body))
	}

	var secretData VaultSecretData
	if err := json.NewDecoder(resp.Body).Decode(&secretData); err != nil {
		return nil, fmt.Errorf("failed to decode secret response: %w", err)
	}

	// Convert to Secret struct
	secret := &Secret{
		Path:      path,
		Provider:  ProviderVault,
		Version:   secretData.Data.Metadata.Version,
		CreatedAt: secretData.Data.Metadata.CreatedTime,
		UpdatedAt: secretData.Data.Metadata.CreatedTime,
	}

	// Extract secret value (first key in data)
	for key, value := range secretData.Data.Data {
		secret.Name = key
		if strVal, ok := value.(string); ok {
			secret.Value = strVal
		} else {
			// Convert to JSON if not a string
			jsonVal, _ := json.Marshal(value)
			secret.Value = string(jsonVal)
		}
		break // Only get first key
	}

	return secret, nil
}

// GetSecretMetadata retrieves metadata for a secret without the actual value
func (v *VaultClient) GetSecretMetadata(ctx context.Context, path string) (*SecretMetadata, error) {
	if !v.enabled {
		return nil, fmt.Errorf("Vault client is disabled")
	}

	// Remove /data/ from path and add /metadata/ for metadata endpoint
	metadataPath := strings.Replace(path, "/data/", "/metadata/", 1)
	url := fmt.Sprintf("%s/v1/%s", v.address, metadataPath)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Vault-Token", v.token)
	if v.namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.namespace)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault returned status %d: %s", resp.StatusCode, string(body))
	}

	var metadata VaultSecretMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode metadata response: %w", err)
	}

	secretMetadata := &SecretMetadata{
		Path:     path,
		Provider: ProviderVault,
		Version:  metadata.Data.CurrentVersion,
	}

	// Get last rotation time from latest version
	if versionInfo, ok := metadata.Data.Versions[fmt.Sprintf("%d", metadata.Data.CurrentVersion)]; ok {
		secretMetadata.LastRotated = &versionInfo.CreatedTime
	}

	return secretMetadata, nil
}

// WatchSecret polls Vault for changes to a secret
// Returns a channel that emits SecretChangeEvents when version changes are detected
func (v *VaultClient) WatchSecret(ctx context.Context, path string, pollInterval time.Duration) <-chan *SecretChangeEvent {
	events := make(chan *SecretChangeEvent, 10)

	if !v.enabled {
		close(events)
		return events
	}

	go func() {
		defer close(events)

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		lastVersion := 0

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				metadata, err := v.GetSecretMetadata(ctx, path)
				if err != nil {
					// Log error but continue watching
					continue
				}

				// Check if version changed
				if lastVersion > 0 && metadata.Version > lastVersion {
					event := &SecretChangeEvent{
						SecretPath:  path,
						SecretName:  metadata.Name,
						Provider:    ProviderVault,
						OldVersion:  lastVersion,
						NewVersion:  metadata.Version,
						DetectedAt:  time.Now().UTC(),
						Status:      RotationPending,
						TriggeredBy: "watcher",
					}

					select {
					case events <- event:
					case <-ctx.Done():
						return
					}
				}

				lastVersion = metadata.Version
			}
		}
	}()

	return events
}

// IsEnabled returns whether the Vault client is enabled
func (v *VaultClient) IsEnabled() bool {
	return v.enabled
}

// ValidateConnection verifies connectivity to Vault
func (v *VaultClient) ValidateConnection(ctx context.Context) error {
	if !v.enabled {
		return fmt.Errorf("Vault client is disabled")
	}

	url := fmt.Sprintf("%s/v1/sys/health", v.address)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Vault: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Vault health endpoint returns 200 for healthy, 429/503 for sealed/standby
	if resp.StatusCode >= 500 {
		return fmt.Errorf("Vault is unhealthy (status %d)", resp.StatusCode)
	}

	return nil
}
