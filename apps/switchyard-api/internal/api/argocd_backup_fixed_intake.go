package api

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func (h *Handler) argoBackupKeyAuthority(c *gin.Context, initialize bool) (string, bool) {
	actor, ok := h.argoBackupHumanPlatform(c)
	if !ok {
		return "", false
	}
	if h.config == nil || !h.config.ArgocdProtectedBackupEnabled || h.argocdBackup == nil || (initialize && h.argocdBackup.keyInitializer == nil) || (!initialize && h.argocdBackup.keyReader == nil) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "fixed_backup_intake_disabled", "native_ready": false})
		return "", false
	}
	return actor, true
}

// Reason-only human initiation through the exact reviewed native profile.
// Backup preparation/apply never call this method or initialize custody.
func (h *Handler) initializeFixedBackupEnvelope(c *gin.Context) {
	actor, ok := h.argoBackupKeyAuthority(c, true)
	if !ok {
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if !argoBackupClosedBody(c, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if len(request.Reason) == 0 || len(request.Reason) > 1000 || !utf8.ValidString(request.Reason) || strings.ContainsFunc(request.Reason, unicode.IsControl) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fixed_backup_intake_reason_required"})
		return
	}
	target, err := secretsintake.GetTarget(secretsintake.ArgocdBackupTargetID)
	if err != nil || secretsintake.ValidateArgocdBackupTarget(target) != nil {
		argoBackupHeld(c, "fixed_intake_profile_unqualified", "")
		return
	}
	updates, keys, err := generatedProfileValues(target, []string{secretsintake.ArgocdBackupKey}, nil)
	if err != nil {
		argoBackupHeld(c, "fixed_intake_generation_unqualified", "")
		return
	}
	origin, err := appendArgocdBackupIntakeOrigin(target, updates)
	if err != nil {
		argoBackupHeld(c, "fixed_intake_origin_unqualified", "")
		return
	}
	version, err := h.argocdBackup.keyInitializer.InitializeArgocdBackupEnvelope(c.Request.Context(), origin, updates)
	if err != nil {
		if errors.Is(err, lockbox.ErrSecretInitializationOutcomeUnknown) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "UNKNOWN", "error": "backup_intake_ack_unqualified_do_not_retry", "origin_id": origin, "expected_version": version, "native_ready": false})
			return
		}
		if errors.Is(err, lockbox.ErrSecretInitializationConflict) {
			c.JSON(http.StatusConflict, gin.H{"status": "HELD", "error": "backup_custody_exists_or_changed_no_rotation", "native_ready": false})
			return
		}
		argoBackupHeld(c, "fixed_intake_custody_unqualified", "")
		return
	}
	if version <= 0 {
		argoBackupHeld(c, "fixed_intake_receipt_unqualified", "")
		return
	}
	now := time.Now().UTC()
	held := false
	status := secretIntakeStatus{IntakeID: fmt.Sprintf("int_%d", now.UnixNano()), TargetID: target.ID, TargetLabel: target.Label, Status: "initialized_held", VaultPath: target.VaultPath, Namespace: target.Namespace, KeysWritten: keys, KeysGenerated: keys, VaultVersion: version, OriginID: origin, NativeReady: &held, ActorSub: actor, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), Message: argocdBackupCustodyHold}
	if h.saveIntakeStatus(c.Request.Context(), status) != nil {
		status.Message = argocdBackupCustodyHold + "; ephemeral status cache unavailable"
	}
	c.JSON(http.StatusAccepted, status)
}

// Unknown acknowledgements are checked against the expected exact version and
// opaque origin. No latest read, key export, import or generation fallback.
func (h *Handler) readFixedBackupEnvelopeCustody(c *gin.Context) {
	_, ok := h.argoBackupKeyAuthority(c, false)
	if !ok || !argoBackupClosedBody(c, nil) {
		return
	}
	origin := c.Param("origin_id")
	rawVersion := c.Param("version")
	version, err := strconv.Atoi(rawVersion)
	if err != nil || version <= 0 || version > 2147483647 || strconv.Itoa(version) != rawVersion || !secretsintake.ValidArgocdBackupOriginID(origin) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "canonical_backup_custody_reference_required"})
		return
	}
	key, err := h.argocdBackupEnvelopeKey(c.Request.Context(), origin, version)
	clear(key)
	if err != nil {
		argoBackupHeld(c, "exact_custody_readback_unqualified", "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "initialized_held", "origin_id": origin, "vault_version": version, "native_ready": false, "restore_qualified": false})
}
