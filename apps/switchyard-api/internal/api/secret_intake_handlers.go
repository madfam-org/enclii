package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

const secretIntakeCachePrefix = "secretsintake:status:"

// intakeStatusFallback holds metadata when Redis is unavailable (single-process / tests).
var intakeStatusFallback sync.Map

// secretIntakeStatus is persisted metadata — never secret values.
type secretIntakeStatus struct {
	IntakeID                string    `json:"intake_id"`
	TargetID                string    `json:"target_id"`
	TargetLabel             string    `json:"target_label"`
	Status                  string    `json:"status"`
	VaultPath               string    `json:"vault_path"`
	Namespace               string    `json:"namespace"`
	ExternalSecret          string    `json:"external_secret,omitempty"`
	KeysWritten             []string  `json:"keys_written"`
	VaultVersion            int       `json:"vault_version,omitempty"`
	ExternalSecretRefreshed bool      `json:"external_secret_refreshed"`
	Reason                  string    `json:"reason,omitempty"`
	ActorSub                string    `json:"actor_sub,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	ExpiresAt               time.Time `json:"expires_at"`
	Message                 string    `json:"message,omitempty"`
}

type secretIntakeSubmitRequest struct {
	Target string            `json:"target"`
	Values map[string]string `json:"values"`
	Reason string            `json:"reason"`
}

// ListSecretIntakeTargets GET /v1/secrets/intake/targets
func (h *Handler) ListSecretIntakeTargets(c *gin.Context) {
	targets, err := secretsintake.ListTargets()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load intake registry"})
		return
	}
	// Public-safe fields only
	type row struct {
		ID          string   `json:"id"`
		Label       string   `json:"label"`
		Description string   `json:"description"`
		VaultPath   string   `json:"vault_path"`
		Namespace   string   `json:"namespace"`
		Keys        []string `json:"keys"`
	}
	out := make([]row, 0, len(targets))
	for _, t := range targets {
		out = append(out, row{
			ID:          t.ID,
			Label:       t.Label,
			Description: t.Description,
			VaultPath:   t.VaultPath,
			Namespace:   t.Namespace,
			Keys:        t.Keys,
		})
	}
	c.JSON(http.StatusOK, gin.H{"targets": out})
}

// SubmitSecretIntake POST /v1/secrets/intake — write-only; values never returned.
func (h *Handler) SubmitSecretIntake(c *gin.Context) {
	if h.vaultClient == nil || !h.vaultClient.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "vault_writer_disabled",
			"message": "Enable ENCLII_SECRET_ROTATION_ENABLED with ENCLII_VAULT_ADDRESS and ENCLII_VAULT_TOKEN",
		})
		return
	}

	var req secretIntakeSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
		return
	}
	req.Target = strings.TrimSpace(req.Target)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Target == "" || len(req.Values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "target and values are required"})
		return
	}
	if req.Reason == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "reason is required"})
		return
	}

	target, err := secretsintake.GetTarget(req.Target)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown_target", "message": err.Error()})
		return
	}

	updates, keysWritten, err := validateIntakeValues(target, req.Values)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_values", "message": err.Error()})
		return
	}

	ctx := c.Request.Context()
	vaultVersion, err := h.vaultClient.MergeSecretData(ctx, target.VaultPath, updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "vault_merge_failed", "message": "failed to write to Vault"})
		return
	}

	now := time.Now().UTC()
	intakeID := fmt.Sprintf("int_%d", now.UnixNano())
	refreshed := false
	if target.ExternalSecret != "" && target.Namespace != "" && h.k8sClient != nil && h.k8sClient.DynamicClient != nil {
		_, refreshErr := h.patchExternalSecretOpsAnnotations(ctx, target.Namespace, target.ExternalSecret, map[string]string{
			"force-sync":                      fmt.Sprintf("%d", now.Unix()),
			"enclii.dev/last-ops-operation":   "ops.secrets.intake",
			"enclii.dev/last-ops-reason":      req.Reason,
			"enclii.dev/last-ops-requested":   now.Format(time.RFC3339),
			"enclii.dev/secret-intake-id":     intakeID,
			"enclii.dev/secret-intake-target": target.ID,
		}, "")
		refreshed = refreshErr == nil
	}

	actorSub, _ := c.Get("user_id")
	actorStr, _ := actorSub.(string)

	status := secretIntakeStatus{
		IntakeID:                intakeID,
		TargetID:                target.ID,
		TargetLabel:             target.Label,
		Status:                  "ready",
		VaultPath:               target.VaultPath,
		Namespace:               target.Namespace,
		ExternalSecret:          target.ExternalSecret,
		KeysWritten:             keysWritten,
		VaultVersion:            vaultVersion,
		ExternalSecretRefreshed: refreshed,
		Reason:                  req.Reason,
		ActorSub:                actorStr,
		CreatedAt:               now,
		ExpiresAt:               now.Add(24 * time.Hour),
		Message:                 "Secret merged into Vault; values are not retrievable via this API",
	}

	if err := h.saveIntakeStatus(ctx, status); err != nil {
		// Vault write succeeded; status cache failure is non-fatal
		status.Message = "Secret merged into Vault; status cache unavailable"
	}

	c.JSON(http.StatusAccepted, status)
}

// GetSecretIntakeStatus GET /v1/secrets/intake/:id
func (h *Handler) GetSecretIntakeStatus(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	status, err := h.loadIntakeStatus(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "intake id not found or expired"})
		return
	}
	c.JSON(http.StatusOK, status)
}

func validateIntakeValues(target secretsintake.Target, values map[string]string) (map[string]interface{}, []string, error) {
	allowed := make(map[string]struct{}, len(target.Keys))
	for _, k := range target.Keys {
		allowed[strings.ToUpper(strings.TrimSpace(k))] = struct{}{}
	}
	updates := make(map[string]interface{})
	var keysWritten []string
	for rawKey, rawVal := range values {
		key := strings.ToUpper(strings.TrimSpace(rawKey))
		val := strings.TrimSpace(rawVal)
		if key == "" || val == "" {
			return nil, nil, fmt.Errorf("empty key or value")
		}
		if _, ok := allowed[key]; !ok {
			return nil, nil, fmt.Errorf("key %q is not allowed for target %q (allowed: %v)", key, target.ID, target.Keys)
		}
		normalized := normalizeVaultSecretKey(key)
		if normalized == "" {
			return nil, nil, fmt.Errorf("key %q normalizes to empty Vault key", key)
		}
		if _, exists := updates[normalized]; exists {
			return nil, nil, fmt.Errorf("duplicate key after normalization: %q", normalized)
		}
		updates[normalized] = val
		keysWritten = append(keysWritten, key)
	}
	if len(updates) == 0 {
		return nil, nil, fmt.Errorf("no values supplied")
	}
	sort.Strings(keysWritten)
	return updates, keysWritten, nil
}

func (h *Handler) saveIntakeStatus(ctx context.Context, status secretIntakeStatus) error {
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	intakeStatusFallback.Store(status.IntakeID, data)
	if h.cache == nil {
		return nil
	}
	ttl := time.Until(status.ExpiresAt)
	if ttl < time.Minute {
		ttl = 24 * time.Hour
	}
	if err := h.cache.Set(ctx, secretIntakeCachePrefix+status.IntakeID, data, ttl); err != nil {
		return err
	}
	return nil
}

func (h *Handler) loadIntakeStatus(ctx context.Context, id string) (secretIntakeStatus, error) {
	var zero secretIntakeStatus
	if raw, ok := intakeStatusFallback.Load(id); ok {
		if data, ok := raw.([]byte); ok {
			var status secretIntakeStatus
			if err := json.Unmarshal(data, &status); err == nil {
				return status, nil
			}
		}
	}
	if h.cache == nil {
		return zero, fmt.Errorf("not found")
	}
	data, err := h.cache.Get(ctx, secretIntakeCachePrefix+id)
	if err != nil || len(data) == 0 {
		return zero, fmt.Errorf("not found")
	}
	var status secretIntakeStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return zero, err
	}
	return status, nil
}
