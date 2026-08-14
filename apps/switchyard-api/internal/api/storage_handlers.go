package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
)

// Day-2 object-storage lifecycle for an existing project.
//
// These endpoints deliberately touch nothing but the bucket and the project's
// credential Secret — no ArgoCD registration, no namespace creation, no domain
// provisioning. That separation is the reason they exist: R2 provisioning used
// to be reachable only through `enclii onboard`, which is day-1 shaped and
// unsafe to re-run against a live project.

// CreateStorageBucketRequest is the body of POST /v1/projects/:slug/storage/buckets.
type CreateStorageBucketRequest struct {
	BucketName string `json:"bucket_name" binding:"required"`
	Namespace  string `json:"namespace,omitempty"`
	SecretName string `json:"secret_name,omitempty"`
	// Rotate mints a fresh token even when complete credentials already exist.
	Rotate bool `json:"rotate,omitempty"`
}

// CreateStorageBucket provisions (or adopts) an R2 bucket for a project and
// writes a complete, bucket-scoped credential set into the project's Secret.
// POST /v1/projects/:slug/storage/buckets
func (h *Handler) CreateStorageBucket(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}
	if !h.enforceUserProjectAccess(c, project.ID) {
		return
	}

	var req CreateStorageBucketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.ensureProjectR2Bucket(ctx, r2ProvisionOptions{
		Project:    project.Slug,
		Namespace:  req.Namespace,
		SecretName: req.SecretName,
		Bucket:     strings.TrimSpace(req.BucketName),
		Rotate:     req.Rotate,
	})
	if err != nil {
		status := r2ProvisionStatusCode(err)
		if status >= 500 {
			h.logger.Error(ctx, "R2 bucket provisioning failed",
				logging.String("project", project.Slug),
				logging.String("bucket", req.BucketName),
				logging.Error("error", err))
		} else {
			h.logger.Warn(ctx, "R2 bucket provisioning refused",
				logging.String("project", project.Slug),
				logging.String("bucket", req.BucketName),
				logging.Error("error", err))
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	code := http.StatusCreated
	if result.Action == r2ActionAdopted {
		code = http.StatusOK
	}
	c.JSON(code, gin.H{
		"bucket":  result,
		"message": storageBucketMessage(result),
	})
}

func storageBucketMessage(result *r2ProvisionResult) string {
	switch result.Action {
	case r2ActionAdopted:
		return "Bucket already provisioned for this project; existing credentials left in place"
	case r2ActionRotated:
		return "Bucket credentials rotated; redeploy the service to pick up the new keys"
	default:
		return "Bucket provisioned with its own scoped credentials"
	}
}

// ListStorageBuckets reports the object-storage bindings a project actually
// holds, read from the cluster rather than from an intent record — so a
// hand-patched or incomplete binding shows up as what it is.
// GET /v1/projects/:slug/storage/buckets
func (h *Handler) ListStorageBuckets(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}
	if !h.enforceUserProjectAccess(c, project.ID) {
		return
	}

	if h.secretsProvisioner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errR2SecretsNotConfigured.Error()})
		return
	}
	auditor := h.secretsProvisioner.R2Auditor()
	if auditor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errR2SecretsNotConfigured.Error()})
		return
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	if namespace == "" {
		namespace = project.Slug
	}

	bindings, err := auditor.Scan(ctx, []string{namespace})
	if err != nil {
		h.logger.Error(ctx, "Failed to scan R2 bindings",
			logging.String("namespace", namespace),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read storage bindings"})
		return
	}

	// Project-scoped findings so `storage ls` surfaces the same drift the
	// operator audit does, without the caller needing admin.
	findings := provisioning.AuditR2Bindings(bindings)

	c.JSON(http.StatusOK, gin.H{
		"namespace": namespace,
		"buckets":   bindings,
		"findings":  findings,
		"count":     len(bindings),
	})
}

// DeleteStorageBucketRequest carries the destructive-intent flags.
type DeleteStorageBucketRequest struct {
	Namespace  string `json:"namespace,omitempty"`
	SecretName string `json:"secret_name,omitempty"`
	// DeleteBucket also deletes the bucket in Cloudflare. Default false: the
	// unbind path is reversible, deleting stored objects is not.
	DeleteBucket bool `json:"delete_bucket,omitempty"`
}

// DeleteStorageBucket unbinds a bucket from a project: the token is revoked
// and the R2 keys are removed from the project Secret. The bucket itself is
// kept unless delete_bucket is set.
// DELETE /v1/projects/:slug/storage/buckets/:bucket
func (h *Handler) DeleteStorageBucket(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")
	bucket := strings.TrimSpace(c.Param("bucket"))

	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}
	if !h.enforceUserProjectAccess(c, project.ID) {
		return
	}

	if err := provisioning.ValidateR2BucketName(bucket); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var req DeleteStorageBucketRequest
	// Body is optional on DELETE.
	_ = c.ShouldBindJSON(&req)
	if strings.EqualFold(c.Query("delete_bucket"), "true") {
		req.DeleteBucket = true
	}

	if h.secretsProvisioner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errR2SecretsNotConfigured.Error()})
		return
	}
	auditor := h.secretsProvisioner.R2Auditor()
	if auditor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errR2SecretsNotConfigured.Error()})
		return
	}

	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = project.Slug
	}
	secretName := strings.TrimSpace(req.SecretName)
	if secretName == "" {
		secretName = project.Slug + "-credentials"
	}

	binding, err := auditor.GetBinding(ctx, namespace, secretName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "no storage binding found at " + namespace + "/" + secretName,
		})
		return
	}
	if binding.Bucket != "" && binding.Bucket != bucket {
		c.JSON(http.StatusConflict, gin.H{
			"error": (&r2RebindError{
				Namespace: namespace, SecretName: secretName,
				Existing: binding.Bucket, Requested: bucket,
			}).Error(),
		})
		return
	}

	warnings := []string{}

	// Revoke the minted token before dropping the keys, so a partial failure
	// leaves a revoked credential rather than a live orphaned one.
	if h.r2Provisioner != nil {
		accessKeyID, err := auditor.AccessKeyID(ctx, namespace, secretName)
		if err != nil {
			warnings = append(warnings, "could not read the access key to revoke: "+err.Error())
		} else if accessKeyID != "" {
			if err := h.r2Provisioner.RevokeToken(ctx, accessKeyID); err != nil {
				warnings = append(warnings, "token revocation failed: "+err.Error())
			}
		}
	}

	if err := h.secretsProvisioner.RemoveEntries(ctx, namespace, secretName,
		r2SecretKeyNames(),
		[]string{
			provisioning.AnnotationR2Bucket,
			provisioning.AnnotationR2Project,
			provisioning.AnnotationR2TokenName,
			provisioning.AnnotationR2ProvisionedAt,
		},
	); err != nil {
		h.logger.Error(ctx, "Failed to remove R2 keys from secret",
			logging.String("namespace", namespace),
			logging.String("secret", secretName),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unbind storage credentials"})
		return
	}

	bucketDeleted := false
	if req.DeleteBucket {
		if h.r2Provisioner == nil {
			warnings = append(warnings, "bucket deletion skipped: "+errR2NotConfigured.Error())
		} else if err := h.r2Provisioner.DeleteBucket(ctx, bucket); err != nil {
			warnings = append(warnings, "bucket deletion failed: "+err.Error())
		} else {
			bucketDeleted = true
		}
	}

	h.logger.Info(ctx, "Unbound R2 bucket from project",
		logging.String("project", project.Slug),
		logging.String("namespace", namespace),
		logging.String("bucket", bucket))

	c.JSON(http.StatusOK, gin.H{
		"bucket":         bucket,
		"namespace":      namespace,
		"secret_name":    secretName,
		"bucket_deleted": bucketDeleted,
		"warnings":       warnings,
		"message":        "Storage credentials unbound and token revoked",
	})
}
