package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	KeysGenerated           []string  `json:"keys_generated,omitempty"`
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
	// Generate names keys Switchyard should mint itself with crypto/rand. The
	// value is written to Vault and never returned, logged, or echoed — the
	// point is that a strong shared key can exist without any human, agent or
	// terminal scrollback ever holding a copy of it.
	Generate []string `json:"generate,omitempty"`
	Reason   string   `json:"reason"`
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
	if req.Target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "target is required"})
		return
	}
	if len(req.Values) == 0 && len(req.Generate) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "values or generate is required"})
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

	keysGenerated, err := applyGeneratedIntakeValues(target, req.Generate, updates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_generate", "message": err.Error()})
		return
	}
	if len(keysGenerated) > 0 {
		keysWritten = append(keysWritten, keysGenerated...)
		sort.Strings(keysWritten)
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_values", "message": "no values supplied"})
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
			"enclii.dev/secret-intake-source": intakeSourceLabel(keysWritten, keysGenerated),
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
		KeysGenerated:           keysGenerated,
		VaultVersion:            vaultVersion,
		ExternalSecretRefreshed: refreshed,
		Reason:                  req.Reason,
		ActorSub:                actorStr,
		CreatedAt:               now,
		ExpiresAt:               now.Add(24 * time.Hour),
		Message:                 intakeStatusMessage(keysGenerated),
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
	sort.Strings(keysWritten)
	return updates, keysWritten, nil
}

// applyGeneratedIntakeValues mints values for the requested keys and merges them
// into updates. Generated values are returned to nobody: they go into the Vault
// merge map and the key NAMES come back for the audit record.
func applyGeneratedIntakeValues(target secretsintake.Target, generate []string, updates map[string]interface{}) ([]string, error) {
	if len(generate) == 0 {
		return nil, nil
	}
	allowed := make(map[string]struct{}, len(target.Keys))
	for _, k := range target.Keys {
		allowed[strings.ToUpper(strings.TrimSpace(k))] = struct{}{}
	}
	var keysGenerated []string
	seen := make(map[string]struct{}, len(generate))
	for _, rawKey := range generate {
		key := strings.ToUpper(strings.TrimSpace(rawKey))
		if key == "" {
			return nil, fmt.Errorf("empty key in generate list")
		}
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("key %q is not allowed for target %q (allowed: %v)", key, target.ID, target.Keys)
		}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("key %q requested twice in generate", key)
		}
		seen[key] = struct{}{}
		normalized := normalizeVaultSecretKey(key)
		if normalized == "" {
			return nil, fmt.Errorf("key %q normalizes to empty Vault key", key)
		}
		// A key cannot be both supplied and generated: silently preferring one
		// would leave the operator unsure which value actually landed in Vault.
		if _, exists := updates[normalized]; exists {
			return nil, fmt.Errorf("key %q was both supplied and requested for generation", key)
		}
		value, err := generateSecretValue(target.GenerateBytes())
		if err != nil {
			return nil, fmt.Errorf("generate value for %q: %w", key, err)
		}
		updates[normalized] = value
		keysGenerated = append(keysGenerated, key)
	}
	sort.Strings(keysGenerated)
	return keysGenerated, nil
}

// generateSecretValue returns n bytes of crypto/rand as unpadded base64url —
// URL- and env-var-safe, so a generated key can travel in a feed URL or a
// container environment without re-encoding.
func generateSecretValue(n int) (string, error) {
	if n <= 0 {
		n = secretsintake.DefaultGenerateBytes
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
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

// intakeSourceLabel describes, for the audit annotation, whether the keys in
// this intake were supplied by the operator, generated by Switchyard, or both.
// It records key PROVENANCE, never key values.
func intakeSourceLabel(keysWritten, keysGenerated []string) string {
	switch {
	case len(keysGenerated) == 0:
		return "supplied"
	case len(keysGenerated) == len(keysWritten):
		return "generated"
	default:
		return "mixed"
	}
}

func intakeStatusMessage(keysGenerated []string) string {
	if len(keysGenerated) > 0 {
		return fmt.Sprintf("Secret merged into Vault; %d key(s) generated server-side and never returned by this API", len(keysGenerated))
	}
	return "Secret merged into Vault; values are not retrievable via this API"
}
