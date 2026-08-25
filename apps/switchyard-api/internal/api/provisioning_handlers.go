package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ProvisionPostgres handles standalone Postgres provisioning.
// POST /v1/admin/provision/postgres
func (h *Handler) ProvisionPostgres(c *gin.Context) {
	ctx := c.Request.Context()

	var req types.ProvisionPostgresRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if h.postgresProvisioner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Postgres provisioning not configured (POSTGRES_ADMIN_URL not set)"})
		return
	}

	h.logger.Info(ctx, "Provisioning Postgres database",
		logging.String("namespace", req.Namespace),
		logging.String("database", req.Spec.DatabaseName))

	if err := h.postgresProvisioner.Provision(ctx, &req.Spec); err != nil {
		h.logger.Error(ctx, "Postgres provisioning failed",
			logging.String("database", req.Spec.DatabaseName),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update PgBouncer if available
	roleName := req.Spec.RoleName
	if roleName == "" {
		roleName = req.Spec.DatabaseName
	}
	var pgbouncerStatus string
	if h.pgbouncerUpdater != nil {
		if err := h.pgbouncerUpdater.AddDatabase(ctx, req.Spec.DatabaseName, roleName, req.Spec.RolePassword); err != nil {
			h.logger.Warn(ctx, "PgBouncer update failed (non-fatal)",
				logging.String("database", req.Spec.DatabaseName),
				logging.Error("error", err))
			pgbouncerStatus = "failed: " + err.Error()
		} else {
			pgbouncerStatus = "updated"
		}
	} else {
		pgbouncerStatus = "skipped (not configured)"
	}

	c.JSON(http.StatusOK, gin.H{
		"status":           "provisioned",
		"database":         req.Spec.DatabaseName,
		"role":             roleName,
		"pgbouncer_status": pgbouncerStatus,
	})
}

// ProvisionSecrets handles standalone K8s secret provisioning.
// POST /v1/admin/provision/secrets
func (h *Handler) ProvisionSecrets(c *gin.Context) {
	ctx := c.Request.Context()

	var req types.ProvisionSecretsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if h.secretsProvisioner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Secrets provisioning not configured (K8s client unavailable)"})
		return
	}

	// Derive project name from namespace (default convention)
	project := req.Namespace
	secretName := req.SecretName
	if secretName == "" {
		secretName = project + "-credentials"
	}

	h.logger.Info(ctx, "Provisioning K8s secrets",
		logging.String("namespace", req.Namespace),
		logging.String("secret", secretName),
		logging.String("count", fmt.Sprintf("%d", len(req.Secrets))))

	if err := h.secretsProvisioner.Create(ctx, req.Namespace, project, req.SecretName, req.Secrets); err != nil {
		h.logger.Error(ctx, "Secrets provisioning failed",
			logging.String("namespace", req.Namespace),
			logging.String("secret", secretName),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "provisioned",
		"namespace": req.Namespace,
		"secret":    secretName,
		"count":     len(req.Secrets),
	})
}

// ProvisionR2 handles standalone R2 bucket provisioning.
// POST /v1/admin/provision/r2
func (h *Handler) ProvisionR2(c *gin.Context) {
	ctx := c.Request.Context()

	var req types.ProvisionR2Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if h.r2Provisioner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "R2 provisioning not configured (Cloudflare credentials not set)"})
		return
	}

	h.logger.Info(ctx, "Provisioning R2 bucket",
		logging.String("bucket", req.BucketName))

	// Shares the day-2 implementation, so this operator path also mints
	// bucket-scoped credentials and records provenance rather than writing a
	// bucket name with no keys behind it.
	result, err := h.ensureProjectR2Bucket(ctx, r2ProvisionOptions{
		Project:   req.Namespace,
		Namespace: req.Namespace,
		Bucket:    req.BucketName,
	})
	if err != nil {
		h.logger.Error(ctx, "R2 provisioning failed",
			logging.String("bucket", req.BucketName),
			logging.Error("error", err))
		c.JSON(r2ProvisionStatusCode(err), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      result.Action,
		"bucket":      result.Bucket,
		"namespace":   result.Namespace,
		"secret_name": result.SecretName,
		"credentials": len(result.SecretKeys),
		"warnings":    result.Warnings,
	})
}

// ReconcilePgbouncerUserlist reports drift between the PgBouncer userlist and
// the login roles that actually exist in Postgres. Read-only: it never mutates
// the userlist (restoring a dropped credential needs the role's password, which
// lives in the consuming service's own secret). It exists because a
// hand-applied userlist silently dropped four users on 2026-08-23 and took
// api.fortuna.tube hard-down — a `missing` result is the fail-closed signal to
// page on.
// GET /v1/admin/provision/pgbouncer/reconcile
func (h *Handler) ReconcilePgbouncerUserlist(c *gin.Context) {
	ctx := c.Request.Context()

	if h.pgbouncerUpdater == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PgBouncer updater not configured (K8s client unavailable)"})
		return
	}
	if h.pgbouncerAdminURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "userlist reconcile not configured (POSTGRES_ADMIN_URL not set)"})
		return
	}

	drift, err := h.pgbouncerUpdater.ReconcileUserlist(ctx, h.pgbouncerAdminURL)
	if err != nil {
		h.logger.Error(ctx, "PgBouncer userlist reconcile failed", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// A missing login role is a real, page-worthy divergence — surface it as a
	// non-2xx so a probe or CronJob wrapping this endpoint fails loudly, exactly
	// the signal the 2026-08-23 outage lacked.
	status := http.StatusOK
	if drift.HasMissing() {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{
		"in_sync":               drift.Empty(),
		"missing_from_userlist": drift.MissingFromUserlist,
		"stale_in_userlist":     drift.StaleInUserlist,
	})
}
