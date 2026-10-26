package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/argocdbackup"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/storage"
)

type argoProtectedBackupDependencies struct {
	storage        argoProtectedStorage
	journal        argocdbackup.Journal
	reader         func() (argocdbackup.SnapshotReader, error)
	keyReader      vaultArgocdBackupEnvelopeReader
	keyInitializer vaultArgocdBackupEnvelopeInitializer
}

// Separate typed custody bindings cannot enable the generic SecretIntake
// writer or rotation. Initialization is only the reviewed fixed native profile.
func (h *Handler) SetArgocdBackupKeyInitializer(initializer vaultArgocdBackupEnvelopeInitializer) {
	if h.argocdBackup == nil {
		h.argocdBackup = &argoProtectedBackupDependencies{}
	}
	h.argocdBackup.keyInitializer = initializer
}

func (h *Handler) SetArgocdBackupKeyReader(reader vaultArgocdBackupEnvelopeReader) {
	if h.argocdBackup == nil {
		h.argocdBackup = &argoProtectedBackupDependencies{}
	}
	h.argocdBackup.keyReader = reader
}

type argoProtectedStorage interface {
	PutArgoBackupCiphertextOnce(context.Context, argocdbackup.Sealed) error
	ReadArgoBackupCiphertext(context.Context, argocdbackup.Reference) ([]byte, error)
}

func (h *Handler) SetArgocdBackupStorage(client *storage.R2Client) {
	if h.argocdBackup == nil {
		h.argocdBackup = &argoProtectedBackupDependencies{}
	}
	if client == nil {
		h.argocdBackup.storage = nil
		return
	}
	h.argocdBackup.storage = client
}
func registerArgoProtectedBackupRoutes(platform *gin.RouterGroup, h *Handler) {
	backups := platform.Group("/argocd/protected-backups", h.RequirePlatformOperator())
	backups.POST("/plan", h.prepareArgoProtectedBackup)
	backups.GET("/current", h.currentArgoProtectedBackup)
	backups.POST("/:operation_id/apply", h.applyArgoProtectedBackup)
	backups.POST("/:operation_id/retire", h.retireArgoProtectedBackup)
	backups.GET("/:operation_id", h.statusArgoProtectedBackup)
}
func (h *Handler) argoBackupHumanPlatform(c *gin.Context) (string, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	actor, err := uuid.Parse(c.GetString("user_id"))
	var allowed bool
	_, apiTokenPresent := c.Get("api_token_id")
	if err != nil || actor == uuid.Nil || actor.String() != c.GetString("user_id") || apiTokenPresent || c.GetString("auth_type") == "api_token" || c.GetString("api_token_name") != "" || h.repos == nil || h.repos.DB() == nil || h.repos.DB().QueryRowContext(c.Request.Context(), `SELECT is_platform_admin AND active FROM users WHERE id=$1`, actor).Scan(&allowed) != nil || !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "argocd_backup_human_platform_rank_required"})
		return "", false
	}
	return actor.String(), true
}
func (h *Handler) argoBackupAuthority(c *gin.Context) (string, bool) {
	actor, ok := h.argoBackupHumanPlatform(c)
	if !ok {
		return "", false
	}
	if h.config == nil || !h.config.ArgocdProtectedBackupEnabled || h.argocdBackup == nil || h.argocdBackup.storage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "argocd_protected_backup_disabled", "native_ready": false})
		return "", false
	}
	return actor, true
}

func (h *Handler) argoBackupJournal() argocdbackup.Journal {
	if h.argocdBackup != nil && h.argocdBackup.journal != nil {
		return h.argocdBackup.journal
	}
	return &argocdbackup.SQLJournal{DB: h.repos.DB()}
}
func (h *Handler) argoBackupSnapshot(ctx context.Context) (argocdbackup.ProtectedSnapshot, error) {
	var reader argocdbackup.SnapshotReader
	var err error
	if h.argocdBackup != nil && h.argocdBackup.reader != nil {
		reader, err = h.argocdBackup.reader()
	} else if h.k8sClient != nil {
		reader, err = newArgoProtectedSnapshotReader(h.k8sClient.Config())
	} else {
		return argocdbackup.ProtectedSnapshot{}, argocdbackup.ErrHold
	}
	if err != nil {
		return argocdbackup.ProtectedSnapshot{}, argocdbackup.ErrHold
	}
	before, err := argocdbackup.CaptureNonHA(ctx, reader)
	if err != nil {
		return argocdbackup.ProtectedSnapshot{}, err
	}
	after, err := argocdbackup.CaptureNonHA(ctx, reader)
	defer after.Clear()
	if err != nil || before.Stable(after) != nil {
		before.Clear()
		return argocdbackup.ProtectedSnapshot{}, argocdbackup.ErrHold
	}
	return before, nil
}
func argoBackupClosedBody(c *gin.Context, out interface{}) bool {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		c.JSON(http.StatusBadRequest, gin.H{"error": "argocd_backup_closed_request_required"})
		return false
	}
	if out == nil {
		var body []byte
		var err error
		if c.Request.Body != nil {
			body, err = io.ReadAll(io.LimitReader(c.Request.Body, 1))
		}
		if err != nil || len(body) != 0 || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "argocd_backup_closed_request_required"})
			return false
		}
		return true
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	if err != nil || k8s.DecodeInitialBootstrapJSON(body, out) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "argocd_backup_closed_request_required"})
		return false
	}
	return true
}
func (h *Handler) prepareArgoProtectedBackup(c *gin.Context) {
	actor, ok := h.argoBackupAuthority(c)
	if !ok {
		return
	}
	var request struct {
		KeyOriginID string `json:"key_origin_id"`
		KeyVersion  int    `json:"key_version"`
		Reason      string `json:"reason"`
	}
	if !argoBackupClosedBody(c, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if len(request.Reason) == 0 || len(request.Reason) > 1000 || !utf8.ValidString(request.Reason) || strings.ContainsFunc(request.Reason, unicode.IsControl) || request.KeyVersion <= 0 || request.KeyVersion > 2147483647 || !secretsintake.ValidArgocdBackupOriginID(request.KeyOriginID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "argocd_backup_fixed_custody_reference_required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	journal := h.argoBackupJournal()
	open, err := journal.GetOpen(ctx, actor)
	if err != nil {
		argoBackupHeld(c, "journal_unqualified", "")
		return
	}
	if open != nil {
		c.JSON(http.StatusConflict, argoBackupPublic(*open))
		return
	}
	snapshot, err := h.argoBackupSnapshot(ctx)
	if err != nil {
		argoBackupHeld(c, "protected_capture_unqualified", "")
		return
	}
	defer snapshot.Clear()
	key, err := h.argocdBackupEnvelopeKey(ctx, request.KeyOriginID, request.KeyVersion)
	if err != nil {
		argoBackupHeld(c, "key_custody_unqualified", "")
		return
	}
	defer clear(key)
	plaintext, err := snapshot.Plaintext()
	if err != nil {
		argoBackupHeld(c, "protected_capture_unqualified", "")
		return
	}
	defer clear(plaintext)
	operationID, e1 := uuid.NewRandom()
	planID, e2 := uuid.NewRandom()
	backupID, e3 := uuid.NewRandom()
	if e1 != nil || e2 != nil || e3 != nil {
		argoBackupHeld(c, "identity_unavailable", "")
		return
	}
	binding := argocdbackup.Context{Profile: argocdbackup.EnvelopeProfile, OperationID: operationID.String(), NamespaceUID: snapshot.NamespaceUID(), BackupID: backupID.String(), Topology: "NON_HA_LAYOUT_OBSERVED"}
	sealed, err := argocdbackup.Seal(key, plaintext, binding)
	if err != nil {
		argoBackupHeld(c, "protected_sealing_unqualified", operationID.String())
		return
	}
	now := time.Now().UTC()
	plan, err := argocdbackup.NewPlan(sealed, planID.String(), actor, request.KeyOriginID, request.KeyVersion, snapshot.Counts(), request.Reason, now, now.Add(10*time.Minute))
	if err != nil {
		argoBackupHeld(c, "immutable_plan_unqualified", operationID.String())
		return
	}
	record, err := journal.CreatePlan(ctx, plan)
	if err != nil {
		argoBackupHeld(c, "preparation_commit_unqualified", operationID.String())
		return
	}
	c.JSON(http.StatusCreated, argoBackupPublic(record))
}
func (h *Handler) currentArgoProtectedBackup(c *gin.Context) {
	actor, ok := h.argoBackupAuthority(c)
	if !ok || !argoBackupClosedBody(c, nil) {
		return
	}
	record, err := h.argoBackupJournal().GetOpen(c.Request.Context(), actor)
	if err != nil {
		argoBackupHeld(c, "journal_unqualified", "")
		return
	}
	if record == nil {
		c.JSON(http.StatusOK, gin.H{"phase": "NO_OPEN_OPERATION", "native_ready": false, "restore_qualified": false})
		return
	}
	c.JSON(http.StatusOK, argoBackupPublic(*record))
}
func (h *Handler) applyArgoProtectedBackup(c *gin.Context) {
	actor, ok := h.argoBackupAuthority(c)
	if !ok {
		return
	}
	var request struct {
		PlanID string `json:"plan_id"`
	}
	if !argoBackupClosedBody(c, &request) {
		return
	}
	operation, err := uuid.Parse(c.Param("operation_id"))
	planID, planErr := uuid.Parse(request.PlanID)
	if err != nil || planErr != nil || operation == uuid.Nil || operation.String() != c.Param("operation_id") || planID == uuid.Nil || planID.String() != request.PlanID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "argocd_backup_exact_plan_required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	journal := h.argoBackupJournal()
	record, err := journal.Get(ctx, operation.String(), actor)
	if err != nil || record.Plan.PlanID != request.PlanID {
		argoBackupHeld(c, "retained_plan_unqualified", operation.String())
		return
	}
	if record.Retired() {
		c.JSON(http.StatusConflict, argoBackupPublic(record))
		return
	}
	if record.DispatchAttempted() {
		h.recoverArgoProtectedBackup(c, ctx, actor, record)
		return
	}
	if !time.Now().UTC().Before(record.Plan.ExpiresAt) {
		argoBackupHeld(c, "review_plan_expired", operation.String())
		return
	}
	key, err := h.argocdBackupEnvelopeKey(ctx, record.Plan.KeyOriginID, record.Plan.KeyVersion)
	if err != nil {
		argoBackupHeld(c, "key_custody_unqualified", operation.String())
		return
	}
	defer clear(key)
	ciphertext := record.Plan.Ciphertext()
	defer clear(ciphertext)
	plaintext, err := argocdbackup.Open(key, ciphertext, record.Plan.Context)
	if err != nil {
		argoBackupHeld(c, "retained_envelope_unqualified", operation.String())
		return
	}
	defer clear(plaintext)
	snapshot, err := h.argoBackupSnapshot(ctx)
	if err != nil {
		argoBackupHeld(c, "protected_capture_unqualified", operation.String())
		return
	}
	defer snapshot.Clear()
	if snapshot.MatchesPlaintext(plaintext) != nil {
		argoBackupHeld(c, "reviewed_capture_changed", operation.String())
		return
	}
	sealed, err := argocdbackup.Authenticate(key, ciphertext, record.Plan.Context)
	if err != nil {
		argoBackupHeld(c, "retained_envelope_unqualified", operation.String())
		return
	}
	claimed, disposition, err := journal.ClaimDispatch(ctx, operation.String(), request.PlanID, actor)
	if err != nil {
		argoBackupHeld(c, "dispatch_commit_unqualified", operation.String())
		return
	}
	if disposition != argocdbackup.DispatchOnce {
		h.recoverArgoProtectedBackup(c, ctx, actor, claimed)
		return
	}
	putErr := h.argocdBackup.storage.PutArgoBackupCiphertextOnce(ctx, sealed)
	kind := argocdbackup.EventPutUnknown
	switch {
	case putErr == nil:
		kind = argocdbackup.EventPutAcknowledged
	case errors.Is(putErr, storage.ErrArgoBackupExists):
		kind = argocdbackup.EventPutExists
	case errors.Is(putErr, storage.ErrArgoBackupConflict):
		kind = argocdbackup.EventPutConflict
	case errors.Is(putErr, storage.ErrArgoBackupWriteRefused):
		kind = argocdbackup.EventPutRefused
	}
	record, err = journal.AppendOutcome(ctx, operation.String(), actor, argocdbackup.Outcome{Kind: kind})
	if err != nil {
		argoBackupHeld(c, "dispatch_result_commit_unqualified", operation.String())
		return
	}
	h.recoverArgoProtectedBackup(c, ctx, actor, record)
}
func (h *Handler) statusArgoProtectedBackup(c *gin.Context) {
	actor, ok := h.argoBackupAuthority(c)
	if !ok || !argoBackupClosedBody(c, nil) {
		return
	}
	operation, err := uuid.Parse(c.Param("operation_id"))
	if err != nil || operation == uuid.Nil || operation.String() != c.Param("operation_id") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "argocd_backup_exact_operation_required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	record, err := h.argoBackupJournal().Get(ctx, operation.String(), actor)
	if err != nil {
		argoBackupHeld(c, "journal_unqualified", operation.String())
		return
	}
	if record.DispatchAttempted() && !record.ReadbackAuthenticated() {
		h.recoverArgoProtectedBackup(c, ctx, actor, record)
		return
	}
	c.JSON(http.StatusOK, argoBackupPublic(record))
}
func (h *Handler) retireArgoProtectedBackup(c *gin.Context) {
	actor, ok := h.argoBackupAuthority(c)
	if !ok {
		return
	}
	var request struct {
		PlanID     string                        `json:"plan_id"`
		ReasonCode argocdbackup.RetirementReason `json:"reason_code"`
	}
	if !argoBackupClosedBody(c, &request) {
		return
	}
	operation, err := uuid.Parse(c.Param("operation_id"))
	planID, planErr := uuid.Parse(request.PlanID)
	if err != nil || planErr != nil || operation == uuid.Nil || operation.String() != c.Param("operation_id") || planID == uuid.Nil || planID.String() != request.PlanID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "argocd_backup_exact_plan_required"})
		return
	}
	switch request.ReasonCode {
	case argocdbackup.RetireExpired, argocdbackup.RetireSourceDrift, argocdbackup.RetireOperatorWithdrew:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "argocd_backup_closed_retirement_reason_required"})
		return
	}
	record, err := h.argoBackupJournal().RetirePrepared(c.Request.Context(), operation.String(), request.PlanID, actor, request.ReasonCode)
	if err != nil {
		argoBackupHeld(c, "preparation_retirement_unqualified", operation.String())
		return
	}
	c.JSON(http.StatusOK, argoBackupPublic(record))
}
func (h *Handler) recoverArgoProtectedBackup(c *gin.Context, ctx context.Context, actor string, record argocdbackup.Record) {
	operation := record.Plan.Context.OperationID
	if record.ReadbackAuthenticated() {
		c.JSON(http.StatusOK, argoBackupPublic(record))
		return
	}
	if !record.DispatchAttempted() {
		argoBackupHeld(c, "dispatch_unclaimed", operation)
		return
	}
	key, err := h.argocdBackupEnvelopeKey(ctx, record.Plan.KeyOriginID, record.Plan.KeyVersion)
	if err != nil {
		argoBackupHeld(c, "key_custody_unqualified", operation)
		return
	}
	defer clear(key)
	ref, err := record.Plan.Reference()
	if err != nil {
		argoBackupHeld(c, "retained_reference_unqualified", operation)
		return
	}
	ciphertext, readErr := h.argocdBackup.storage.ReadArgoBackupCiphertext(ctx, ref)
	defer clear(ciphertext)
	outcome := argocdbackup.Outcome{Kind: argocdbackup.EventReadbackUnqualified}
	if readErr == nil {
		if proof, err := argocdbackup.Authenticate(key, ciphertext, record.Plan.Context); err == nil {
			outcome = argocdbackup.Outcome{Kind: argocdbackup.EventReadbackAuthenticated, Authenticated: &proof}
		}
	}
	updated, err := h.argoBackupJournal().AppendOutcome(ctx, operation, actor, outcome)
	if err != nil {
		argoBackupHeld(c, "readback_commit_unqualified", operation)
		return
	}
	c.JSON(http.StatusOK, argoBackupPublic(updated))
}
func argoBackupHeld(c *gin.Context, phase, operation string) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "argocd_backup_held", "phase": phase, "operation_id": operation, "native_ready": false, "restore_qualified": false})
}
func argoBackupPublic(record argocdbackup.Record) interface{} {
	phase := "PREPARED_SQL_WRITING_PREPARATION"
	if record.DispatchAttempted() {
		phase = "DISPATCH_HELD_SAME_OPERATION_READ_ONLY"
	}
	if record.ReadbackAuthenticated() {
		phase = "CIPHERTEXT_READBACK_AUTHENTICATED_RESTORE_HELD"
	}
	if record.Retired() {
		phase = "PREPARATION_RETIRED_NO_DISPATCH"
	}
	return struct {
		OperationID      string                     `json:"operation_id"`
		PlanID           string                     `json:"plan_id"`
		Phase            string                     `json:"phase"`
		Counts           argocdbackup.CaptureCounts `json:"counts"`
		CiphertextSHA256 string                     `json:"ciphertext_sha256"`
		CiphertextBytes  int                        `json:"ciphertext_bytes"`
		ExpiresAt        time.Time                  `json:"expires_at"`
		NativeReady      bool                       `json:"native_ready"`
		RestoreQualified bool                       `json:"restore_qualified"`
	}{record.Plan.Context.OperationID, record.Plan.PlanID, phase, record.Plan.Counts, record.Plan.Receipt.CiphertextSHA256, record.Plan.Receipt.Bytes, record.Plan.ExpiresAt, false, false}
}
