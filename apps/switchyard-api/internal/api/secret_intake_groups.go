package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

// Shared intake groups: one value Switchyard mints and writes to every member
// target, so several services can hold the same key (an HMAC key one verifies
// and others sign with) without any human, terminal or file holding it.
//
// The run is convergent rather than transactional, because Vault has no
// multi-path transaction:
//   - Without rotate, the value already held by the members is kept and copied
//     to any member that lacks it; with no member holding one, a new value is
//     minted. A re-run after a partial failure therefore completes the group
//     with the same value, and a re-run of a complete group writes nothing.
//   - Members holding DIFFERENT values (an interrupted rotation, a hand edit)
//     are refused without a write; only rotate converges them.
//   - With rotate, a new value is minted and written to every member.
//   - Each member write is a compare-and-set against what the run read, so a
//     concurrent run or hand edit fails that member instead of mixing values.
//   - Writes stop at the first failure; the response names the member that
//     failed and those not attempted.

const (
	sharedGroupMemberWritten      = "written"
	sharedGroupMemberUnchanged    = "unchanged"
	sharedGroupMemberFailed       = "failed"
	sharedGroupMemberNotAttempted = "not_attempted"
)

var errSharedGroupMemberChanged = errors.New("member changed since it was read")

// One run at a time per process; the per-path compare-and-set covers runs on
// other replicas.
var sharedGroupRunMu sync.Mutex

type sharedGroupVault interface {
	GetSecretData(ctx context.Context, path string) (map[string]interface{}, error)
	MutateSecretData(ctx context.Context, path string, update func(map[string]interface{}) (map[string]interface{}, error)) (int, error)
}

type sharedGroupRequest struct {
	Group  string `json:"group"`
	Reason string `json:"reason"`
	// Rotate replaces the value on every member. Without it a run never
	// changes a value a member already holds.
	Rotate bool `json:"rotate,omitempty"`
}

// sharedGroupMemberStatus is metadata only — never the value.
type sharedGroupMemberStatus struct {
	TargetID                string `json:"target_id"`
	VaultPath               string `json:"vault_path"`
	Key                     string `json:"key"`
	Namespace               string `json:"namespace"`
	ExternalSecret          string `json:"external_secret,omitempty"`
	Result                  string `json:"result"`
	VaultVersion            int    `json:"vault_version,omitempty"`
	ExternalSecretRefreshed bool   `json:"external_secret_refreshed"`
}

// ProvisionSharedIntakeGroup POST /v1/secrets/intake/groups — writes one
// server-generated value to every member of a shared group; never returns it.
func (h *Handler) ProvisionSharedIntakeGroup(c *gin.Context) {
	if h.vaultClient == nil || !h.vaultClient.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "vault_writer_disabled",
			"message": "Enable ENCLII_SECRET_ROTATION_ENABLED with ENCLII_VAULT_ADDRESS and ENCLII_VAULT_TOKEN",
		})
		return
	}
	var req sharedGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
		return
	}
	req.Group, req.Reason = strings.TrimSpace(req.Group), strings.TrimSpace(req.Reason)
	if req.Group == "" || req.Reason == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "group and reason are required"})
		return
	}
	group, err := secretsintake.GetGroup(req.Group)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown_group", "message": err.Error()})
		return
	}
	vault, ok := h.vaultClient.(sharedGroupVault)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vault_mutation_unavailable", "message": "compare-and-set writes are unavailable; nothing was written"})
		return
	}
	members := make([]secretsintake.Target, 0, len(group.Members))
	for _, id := range group.Members {
		t, err := secretsintake.GetTarget(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load intake registry"})
			return
		}
		members = append(members, t)
	}

	sharedGroupRunMu.Lock()
	defer sharedGroupRunMu.Unlock()
	ctx := c.Request.Context()

	statuses := make([]sharedGroupMemberStatus, len(members))
	observed := make([]string, len(members))
	held := map[string]struct{}{}
	for i, t := range members {
		prop := sharedGroupProperty(t)
		statuses[i] = sharedGroupMemberStatus{
			TargetID: t.ID, VaultPath: t.VaultPath, Key: prop, Namespace: t.Namespace,
			ExternalSecret: t.ExternalSecret, Result: sharedGroupMemberNotAttempted, // pragma: allowlist secret -- ExternalSecret object name; not a credential
		}
		current, err := vault.GetSecretData(ctx, t.VaultPath)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vault_read_failed", "message": fmt.Sprintf("could not read %s; nothing was written", t.ID)})
			return
		}
		value, isString := current[prop].(string)
		if current[prop] != nil && !isString {
			c.JSON(http.StatusConflict, gin.H{"error": "group_out_of_step", "message": fmt.Sprintf("%s holds a non-string value; nothing was written. Re-run with rotate to give every member one new value.", t.ID)})
			return
		}
		observed[i] = value
		if value != "" {
			held[value] = struct{}{}
		}
	}

	var value, mode string
	switch {
	case req.Rotate:
		mode = "rotated"
	case len(held) == 0:
		mode = "generated"
	case len(held) == 1:
		mode = "kept"
		for v := range held {
			value = v
		}
	default:
		c.JSON(http.StatusConflict, gin.H{
			"error":   "group_out_of_step",
			"message": "the members hold different values (an interrupted rotation or a hand edit); nothing was written. Re-run with rotate to give every member one new value.",
			"members": statuses,
		})
		return
	}
	if value == "" {
		buf := make([]byte, group.GenerateBytes())
		if _, err := rand.Read(buf); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generation_failed", "message": "could not generate the group value; nothing was written"})
			return
		}
		value = hex.EncodeToString(buf)
	}

	now := time.Now().UTC()
	intakeID := fmt.Sprintf("int_%d", now.UnixNano())
	failedAt := -1
	for i, t := range members {
		if observed[i] == value {
			statuses[i].Result = sharedGroupMemberUnchanged
			continue
		}
		prop, expected := statuses[i].Key, observed[i]
		version, err := vault.MutateSecretData(ctx, t.VaultPath, func(current map[string]interface{}) (map[string]interface{}, error) {
			got, _ := current[prop].(string)
			if got == value {
				return nil, nil
			}
			if got != expected {
				return nil, errSharedGroupMemberChanged
			}
			return map[string]interface{}{prop: value}, nil
		})
		if err != nil {
			statuses[i].Result = sharedGroupMemberFailed
			failedAt = i
			break
		}
		statuses[i].Result = sharedGroupMemberWritten
		statuses[i].VaultVersion = version
	}

	var written []string
	for i, t := range members {
		if statuses[i].Result != sharedGroupMemberWritten {
			continue
		}
		written = append(written, t.ID+":"+statuses[i].Key)
		if t.ExternalSecret == "" || t.Namespace == "" || h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
			continue
		}
		// A missing ExternalSecret is not an error: the value is in Vault and
		// ESO reads it on the object's first sync.
		_, refreshErr := h.patchExternalSecretOpsAnnotations(ctx, t.Namespace, t.ExternalSecret, map[string]string{
			"force-sync":                      fmt.Sprintf("%d", now.Unix()),
			"enclii.dev/last-ops-operation":   "ops.secrets.intake.group",
			"enclii.dev/last-ops-reason":      req.Reason,
			"enclii.dev/last-ops-requested":   now.Format(time.RFC3339),
			"enclii.dev/secret-intake-id":     intakeID,
			"enclii.dev/secret-intake-target": t.ID,
			"enclii.dev/secret-intake-source": "generated",
		}, "")
		statuses[i].ExternalSecretRefreshed = refreshErr == nil // pragma: allowlist secret -- boolean refresh flag; not a credential
	}

	actorSub, _ := c.Get("user_id")
	actorStr, _ := actorSub.(string)
	status := secretIntakeStatus{
		IntakeID:      intakeID,
		TargetID:      group.ID,
		TargetLabel:   group.Label,
		Status:        "ready",
		KeysWritten:   written,
		KeysGenerated: written,
		Reason:        req.Reason,
		ActorSub:      actorStr,
		CreatedAt:     now,
		ExpiresAt:     now.Add(24 * time.Hour),
		ValueSource:   mode,
		Members:       statuses,
		Message:       sharedGroupMessage(mode, statuses),
	}
	code := http.StatusOK
	if failedAt >= 0 {
		code = http.StatusMultiStatus
		status.Status = "partial"
		rerun := "Re-run the same command to complete the group with the same value."
		if mode == "rotated" && len(written) > 0 {
			rerun = "The members now hold different values: re-run with rotate to give every member one new value."
		}
		status.Message = fmt.Sprintf("writing %s failed (Vault refused it, or it changed during the run); members after it were not attempted. %s", members[failedAt].ID, rerun)
	}
	if err := h.saveIntakeStatus(ctx, status); err != nil && failedAt < 0 {
		status.Message = "Group written to Vault; status cache unavailable"
	}
	c.JSON(code, status)
}

// sharedGroupProperty is the Vault property a member's one key lands in, with
// the same normalization a single-target intake applies.
func sharedGroupProperty(t secretsintake.Target) string {
	return normalizeVaultSecretKey(strings.ToUpper(strings.TrimSpace(t.Keys[0])))
}

func sharedGroupMessage(mode string, statuses []sharedGroupMemberStatus) string {
	written := 0
	for _, s := range statuses {
		if s.Result == sharedGroupMemberWritten {
			written++
		}
	}
	if written == 0 {
		return "Every member already holds the group value; nothing was written."
	}
	switch mode {
	case "rotated":
		return "A new value was generated and written to every member; restart every member service."
	case "kept":
		return "The value the group already held was copied to the members that lacked it; restart those services."
	default:
		return "A new value was generated and written to every member; restart every member service."
	}
}
