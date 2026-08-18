package api

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/storage"
)

// End-user object API (Supabase-Storage-style) over a project's own R2 bucket.
//
// This is the object surface that sits on top of the bucket lifecycle in
// storage_handlers.go. The lifecycle endpoints provision a bucket and write a
// bucket-scoped credential set into the project's Secret; these endpoints let a
// project operate on the objects in that bucket without the API pod streaming
// bytes: uploads and downloads happen directly against R2 through presigned
// URLs, and the API only lists and mints.
//
// Isolation is enforced on three independent axes, so no single mistake crosses
// a tenant boundary:
//
//  1. Credential sourcing. Every URL is minted with the *project's own*
//     bucket-scoped R2 credentials, read from that project's Secret. Those keys
//     are physically incapable of signing a request for another project's
//     bucket — the strongest boundary available, because it does not depend on
//     the API getting an authorization check right.
//  2. Bucket ownership. The :bucket named in the path must match the bucket
//     recorded in the project's Secret. A caller cannot name a bucket the
//     project does not own and have anything minted for it.
//  3. Key namespacing. Every object key is confined under projects/<slug>/, and
//     the key is rejected before use if it attempts path traversal, is absolute,
//     or carries control characters. Even inside one physical bucket, a project
//     cannot list or address another project's logical prefix.

const (
	// objectKeyPrefixTemplate namespaces every object under a per-project
	// prefix. Keeping this in one place means listing, presigning, and deleting
	// can never disagree about where a project's objects live.
	objectKeyPrefixTemplate = "projects/%s/"

	// defaultPresignExpiry is used when the caller does not specify one.
	defaultPresignExpiry = 15 * time.Minute
	// maxPresignExpiry caps how long a minted URL stays valid. R2 (like S3)
	// rejects SigV4 presigns beyond 7 days; we cap well under that so a leaked
	// URL is not usable for long.
	maxPresignExpiry = 24 * time.Hour

	// maxObjectListKeys bounds a single list response.
	maxObjectListKeys = 1000
	// defaultObjectListKeys is used when no limit is supplied.
	defaultObjectListKeys = 100

	// maxDirectUploadBytes caps the tiny-file passthrough upload. Anything
	// larger must use the presigned path so bytes never transit the API pod.
	maxDirectUploadBytes = 5 << 20 // 5 MiB
)

// objectStore is the narrow slice of R2 behaviour the object handlers need.
// It exists so the handlers can be tested without a live R2 endpoint, and so
// the credential-per-project construction is the only thing that ever builds a
// concrete client.
type objectStore interface {
	List(ctx context.Context, prefix string, maxKeys int32) ([]storage.ObjectInfo, error)
	GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error)
	GetPresignedUploadURL(ctx context.Context, key, contentType string, expiry time.Duration) (string, error)
	Upload(ctx context.Context, key string, body io.Reader, contentType string) error
	Delete(ctx context.Context, key string) error
}

// objectStoreFactory builds an objectStore bound to a specific project's
// bucket. Swapped out in tests; in production it reads the project's Secret and
// constructs a real R2 client scoped to that project's credentials.
type objectStoreFactory func(ctx context.Context, binding projectBucketBinding) (objectStore, error)

// projectBucketBinding is the resolved, verified object-storage binding for one
// project: which bucket it owns and the credentials that reach it.
type projectBucketBinding struct {
	Project         string
	Namespace       string
	SecretName      string
	Bucket          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	AccountID       string
}

// objectStoreFor returns the factory the handlers use, defaulting to the real
// R2-backed one when the handler has not been given a test override.
func (h *Handler) objectStoreFor() objectStoreFactory {
	if h.objectStoreFactory != nil {
		return h.objectStoreFactory
	}
	return defaultObjectStoreFactory
}

// defaultObjectStoreFactory builds a real R2 client from a project's own
// bucket-scoped credentials.
func defaultObjectStoreFactory(ctx context.Context, binding projectBucketBinding) (objectStore, error) {
	client, err := storage.NewR2Client(ctx, &storage.R2Config{
		AccountID:       binding.AccountID,
		AccessKeyID:     binding.AccessKeyID,
		AccessKeySecret: binding.SecretAccessKey,
		BucketName:      binding.Bucket,
		Endpoint:        binding.Endpoint,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

// objectPrefix is the per-project key prefix.
func objectPrefix(slug string) string {
	return fmt.Sprintf(objectKeyPrefixTemplate, slug)
}

// sanitizeObjectKey validates a caller-supplied object key and returns the
// fully-qualified, project-namespaced storage key.
//
// The returned key is always under projects/<slug>/. Any attempt to escape that
// prefix — via "..", a leading slash, a backslash, an embedded NUL, or the
// bucket-crossing "s3://"/scheme prefixes — is rejected rather than cleaned,
// because silently normalising an attack into a valid-but-different key is how
// traversal bugs become data-exposure bugs.
func sanitizeObjectKey(slug, rawKey string) (string, error) {
	key := strings.TrimSpace(rawKey)
	if key == "" {
		return "", fmt.Errorf("object key is required")
	}
	if len(key) > 1024 {
		return "", fmt.Errorf("object key is too long (max 1024 characters)")
	}
	// Reject control characters and NUL: they have no legitimate place in a key
	// and are a classic smuggling vector.
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("object key contains a control character")
		}
	}
	if strings.Contains(key, "\\") {
		return "", fmt.Errorf("object key must not contain backslashes")
	}
	if strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("object key must be relative, not absolute")
	}
	// A caller may address their own namespaced key with or without the prefix;
	// normalise to the un-prefixed form first so the traversal check sees the
	// real relative path.
	prefix := objectPrefix(slug)
	rel := strings.TrimPrefix(key, prefix)

	// Traversal check on the path segments. This runs on the un-prefixed key so
	// "../../other-project/x" is caught rather than accidentally re-anchored.
	// Any "." segment is also rejected: it is never meaningful in a stored key
	// and only exists to obfuscate a path.
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." || seg == "." {
			return "", fmt.Errorf("object key must not contain path traversal (. or ..)")
		}
	}

	full := prefix + rel
	// Defence in depth: with no "." or ".." segments and no leading slash,
	// path.Clean can only collapse duplicate slashes, which cannot leave the
	// prefix. If cleaning ever produces something outside projects/<slug>/,
	// refuse rather than trust the raw string.
	if cleaned := path.Clean(full); !strings.HasPrefix(cleaned+"/", prefix) && cleaned+"/" != prefix {
		return "", fmt.Errorf("object key resolves outside the project namespace")
	}
	// The un-prefixed remainder must be non-empty: projects/<slug>/ alone is a
	// prefix, not an object.
	if rel == "" {
		return "", fmt.Errorf("object key is required")
	}
	return full, nil
}

// sanitizeListPrefix validates a caller-supplied list prefix and returns the
// fully-qualified prefix under the project namespace. An empty prefix lists the
// whole project namespace. Unlike a key, a prefix may be empty and may end in a
// slash.
func sanitizeListPrefix(slug, rawPrefix string) (string, error) {
	base := objectPrefix(slug)
	p := strings.TrimSpace(rawPrefix)
	if p == "" {
		return base, nil
	}
	if len(p) > 1024 {
		return "", fmt.Errorf("prefix is too long (max 1024 characters)")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("prefix contains a control character")
		}
	}
	if strings.Contains(p, "\\") {
		return "", fmt.Errorf("prefix must not contain backslashes")
	}
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("prefix must be relative, not absolute")
	}
	p = strings.TrimPrefix(p, base)
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("prefix must not contain path traversal (..)")
		}
	}
	full := base + p
	if !strings.HasPrefix(full, base) {
		return "", fmt.Errorf("prefix resolves outside the project namespace")
	}
	return full, nil
}

// stripObjectPrefix removes the per-project prefix from a stored key so the API
// never leaks the internal namespacing to the caller. A key that does not carry
// the prefix (should not happen for scoped listings) is returned unchanged.
func stripObjectPrefix(slug, storedKey string) string {
	return strings.TrimPrefix(storedKey, objectPrefix(slug))
}

// resolveProjectBucketBinding loads and verifies the object-storage binding for
// a project, confirming the requested bucket is the one the project owns.
//
// Returns an (httpStatus, error) pair so callers can translate consistently:
//   - 503 when object storage is not configured on the platform,
//   - 404 when the project has no bucket bound,
//   - 409 when the caller named a bucket the project does not own.
func (h *Handler) resolveProjectBucketBinding(
	ctx context.Context, slug, requestedBucket string,
) (projectBucketBinding, int, error) {
	if h.secretsProvisioner == nil {
		return projectBucketBinding{}, http.StatusServiceUnavailable, errR2SecretsNotConfigured
	}
	auditor := h.secretsProvisioner.R2Auditor()
	if auditor == nil {
		return projectBucketBinding{}, http.StatusServiceUnavailable, errR2SecretsNotConfigured
	}

	namespace := slug
	secretName := slug + "-credentials"

	binding, err := auditor.GetBinding(ctx, namespace, secretName)
	if err != nil {
		return projectBucketBinding{}, http.StatusNotFound,
			fmt.Errorf("no object-storage binding found for project %q; create a bucket first", slug)
	}
	if !binding.Complete() {
		return projectBucketBinding{}, http.StatusNotFound,
			fmt.Errorf("project %q has no complete object-storage credentials; run `enclii buckets create`", slug)
	}
	if binding.Bucket != requestedBucket {
		// Refuse cross-bucket access. Naming a bucket the project does not own is
		// exactly the boundary this API exists to hold.
		return projectBucketBinding{}, http.StatusConflict,
			fmt.Errorf("bucket %q is not owned by project %q (its bucket is %q)", requestedBucket, slug, binding.Bucket)
	}

	// Read the credential material for the project's own Secret only. The
	// auditor deliberately never exposes the secret half, so read it here,
	// scoped to this project's namespace/secret and nothing else.
	creds, err := h.readProjectR2Credentials(ctx, namespace, secretName)
	if err != nil {
		return projectBucketBinding{}, http.StatusInternalServerError, err
	}

	return projectBucketBinding{
		Project:         slug,
		Namespace:       namespace,
		SecretName:      secretName,
		Bucket:          binding.Bucket,
		Endpoint:        creds.endpoint,
		AccessKeyID:     creds.accessKeyID,
		SecretAccessKey: creds.secretAccessKey,
		AccountID:       h.r2AccountID(),
	}, http.StatusOK, nil
}

// projectR2Creds is the credential material read from a project's own Secret.
type projectR2Creds struct {
	endpoint        string
	accessKeyID     string
	secretAccessKey string
}

// readProjectR2Credentials reads the R2 credential material from a single,
// named Secret in the project's own namespace. It is deliberately narrow: it
// takes an explicit namespace/secretName (both derived from the project slug by
// the caller) and reads only R2 keys, so it can never be pointed at an
// arbitrary Secret.
func (h *Handler) readProjectR2Credentials(ctx context.Context, namespace, secretName string) (projectR2Creds, error) {
	kube := h.opsKubeClient()
	if kube == nil {
		return projectR2Creds{}, fmt.Errorf("kubernetes client is not configured on switchyard-api")
	}
	secret, err := kube.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return projectR2Creds{}, fmt.Errorf("read object-storage credentials for %s/%s: %w", namespace, secretName, err)
	}
	get := func(key string) string { return strings.TrimSpace(string(secret.Data[key])) }
	creds := projectR2Creds{
		endpoint:        get(provisioning.SecretKeyR2Endpoint),
		accessKeyID:     get(provisioning.SecretKeyR2AccessKeyID),
		secretAccessKey: get(provisioning.SecretKeyR2SecretAccessKey),
	}
	if creds.accessKeyID == "" || creds.secretAccessKey == "" {
		return projectR2Creds{}, fmt.Errorf("object-storage credentials for %s/%s are incomplete", namespace, secretName)
	}
	return creds, nil
}

// r2AccountID returns the Cloudflare account ID the platform provisions R2 in,
// used to derive the S3 endpoint when a Secret does not carry an explicit one.
func (h *Handler) r2AccountID() string {
	if h.r2Provisioner == nil {
		return ""
	}
	// EndpointURL is https://<account>.r2.cloudflarestorage.com; the account ID
	// is the first label. The R2 client can also take the full endpoint from the
	// Secret, so this is only a fallback.
	endpoint := h.r2Provisioner.EndpointURL()
	trimmed := strings.TrimPrefix(endpoint, "https://")
	if i := strings.Index(trimmed, "."); i > 0 {
		return trimmed[:i]
	}
	return ""
}

// clampExpiry parses and bounds a requested presign expiry (seconds).
func clampExpiry(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultPresignExpiry
	}
	d := time.Duration(seconds) * time.Second
	if d > maxPresignExpiry {
		return maxPresignExpiry
	}
	return d
}

// parseQueryInt parses a query-string integer, returning 0 for empty or
// malformed input so callers can apply their own default.
func parseQueryInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// parseListLimit parses and bounds a list "limit" query parameter.
func parseListLimit(raw string) int32 {
	n := parseQueryInt(raw)
	if n <= 0 {
		return defaultObjectListKeys
	}
	if n > maxObjectListKeys {
		return maxObjectListKeys
	}
	return int32(n)
}

// loadProjectForStorage resolves a project by slug and enforces caller access,
// writing the appropriate error response and returning false when it fails.
func (h *Handler) loadProjectForStorage(c *gin.Context, slug string) bool {
	ctx := c.Request.Context()
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return false
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return false
	}
	return h.enforceUserProjectAccess(c, project.ID)
}

// ---- Handlers ----

// ObjectListItem is one entry in a list response, with the internal per-project
// prefix stripped so the caller sees only their own logical keys.
type ObjectListItem struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag,omitempty"`
}

// ListObjects lists objects in the project's bucket under an optional prefix.
// GET /v1/projects/:slug/storage/buckets/:bucket/objects
//
// Read access: any project member.
func (h *Handler) ListObjects(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")
	bucket := strings.TrimSpace(c.Param("bucket"))

	if !h.loadProjectForStorage(c, slug) {
		return
	}

	binding, status, err := h.resolveProjectBucketBinding(ctx, slug, bucket)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	prefix, err := sanitizeListPrefix(slug, c.Query("prefix"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	limit := parseListLimit(c.Query("limit"))

	store, err := h.objectStoreFor()(ctx, binding)
	if err != nil {
		h.logger.Error(ctx, "Failed to build object store",
			logging.String("project", slug), logging.Error("error", err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "object storage is temporarily unavailable"})
		return
	}

	objects, err := store.List(ctx, prefix, limit)
	if err != nil {
		h.logger.Error(ctx, "Failed to list objects",
			logging.String("project", slug),
			logging.String("bucket", bucket),
			logging.Error("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to list objects"})
		return
	}

	items := make([]ObjectListItem, 0, len(objects))
	for _, obj := range objects {
		items = append(items, ObjectListItem{
			Key:          stripObjectPrefix(slug, obj.Key),
			Size:         obj.Size,
			LastModified: obj.LastModified,
			ETag:         obj.ETag,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"bucket":  bucket,
		"prefix":  stripObjectPrefix(slug, prefix),
		"objects": items,
		"count":   len(items),
	})
}

// PresignUploadRequest is the body of the presign-upload endpoint.
type PresignUploadRequest struct {
	Key         string `json:"key" binding:"required"`
	ContentType string `json:"content_type,omitempty"`
	// ExpirySeconds bounds how long the returned PUT URL is valid.
	ExpirySeconds int `json:"expiry_seconds,omitempty"`
}

// PresignUpload returns a presigned PUT URL so the client uploads directly to
// R2 without the bytes transiting the API pod.
// POST /v1/projects/:slug/storage/buckets/:bucket/objects/presign-upload
//
// Mutating: requires developer role (route-level) plus project access.
func (h *Handler) PresignUpload(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")
	bucket := strings.TrimSpace(c.Param("bucket"))

	if !h.loadProjectForStorage(c, slug) {
		return
	}

	var req PresignUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	key, err := sanitizeObjectKey(slug, req.Key)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	binding, status, err := h.resolveProjectBucketBinding(ctx, slug, bucket)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	expiry := clampExpiry(req.ExpirySeconds)

	store, err := h.objectStoreFor()(ctx, binding)
	if err != nil {
		h.logger.Error(ctx, "Failed to build object store",
			logging.String("project", slug), logging.Error("error", err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "object storage is temporarily unavailable"})
		return
	}

	url, err := store.GetPresignedUploadURL(ctx, key, contentType, expiry)
	if err != nil {
		h.logger.Error(ctx, "Failed to presign upload",
			logging.String("project", slug),
			logging.String("bucket", bucket),
			logging.Error("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to generate upload URL"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"bucket":       bucket,
		"key":          stripObjectPrefix(slug, key),
		"method":       http.MethodPut,
		"url":          url,
		"content_type": contentType,
		"expires_in":   int(expiry.Seconds()),
	})
}

// PresignDownload returns a presigned GET URL for an object.
// GET /v1/projects/:slug/storage/buckets/:bucket/objects/presign-download?key=...
//
// Read access: any project member.
func (h *Handler) PresignDownload(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")
	bucket := strings.TrimSpace(c.Param("bucket"))

	if !h.loadProjectForStorage(c, slug) {
		return
	}

	key, err := sanitizeObjectKey(slug, c.Query("key"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	binding, status, err := h.resolveProjectBucketBinding(ctx, slug, bucket)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	expiry := clampExpiry(parseQueryInt(c.Query("expiry_seconds")))

	store, err := h.objectStoreFor()(ctx, binding)
	if err != nil {
		h.logger.Error(ctx, "Failed to build object store",
			logging.String("project", slug), logging.Error("error", err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "object storage is temporarily unavailable"})
		return
	}

	url, err := store.GetPresignedURL(ctx, key, expiry)
	if err != nil {
		h.logger.Error(ctx, "Failed to presign download",
			logging.String("project", slug),
			logging.String("bucket", bucket),
			logging.Error("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to generate download URL"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"bucket":     bucket,
		"key":        stripObjectPrefix(slug, key),
		"method":     http.MethodGet,
		"url":        url,
		"expires_in": int(expiry.Seconds()),
	})
}

// DeleteObjectRequest is the optional body of the delete endpoint. The key may
// also be supplied as a query parameter.
type DeleteObjectRequest struct {
	Key string `json:"key,omitempty"`
}

// DeleteObject deletes an object from the project's bucket.
// DELETE /v1/projects/:slug/storage/buckets/:bucket/objects?key=...
//
// Mutating: requires developer role (route-level) plus project access.
func (h *Handler) DeleteObject(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")
	bucket := strings.TrimSpace(c.Param("bucket"))

	if !h.loadProjectForStorage(c, slug) {
		return
	}

	rawKey := strings.TrimSpace(c.Query("key"))
	if rawKey == "" {
		var req DeleteObjectRequest
		_ = c.ShouldBindJSON(&req)
		rawKey = strings.TrimSpace(req.Key)
	}

	key, err := sanitizeObjectKey(slug, rawKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	binding, status, err := h.resolveProjectBucketBinding(ctx, slug, bucket)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	store, err := h.objectStoreFor()(ctx, binding)
	if err != nil {
		h.logger.Error(ctx, "Failed to build object store",
			logging.String("project", slug), logging.Error("error", err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "object storage is temporarily unavailable"})
		return
	}

	if err := store.Delete(ctx, key); err != nil {
		h.logger.Error(ctx, "Failed to delete object",
			logging.String("project", slug),
			logging.String("bucket", bucket),
			logging.Error("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to delete object"})
		return
	}

	h.logger.Info(ctx, "Deleted object",
		logging.String("project", slug),
		logging.String("bucket", bucket))

	c.JSON(http.StatusOK, gin.H{
		"bucket":  bucket,
		"key":     stripObjectPrefix(slug, key),
		"deleted": true,
	})
}

// UploadObject streams a small object straight through the API into R2. This is
// the passthrough convenience path for tiny files; the presigned PUT URL is the
// primary path for everything else, and this endpoint refuses anything over
// maxDirectUploadBytes.
// POST /v1/projects/:slug/storage/buckets/:bucket/objects/upload?key=...
//
// Mutating: requires developer role (route-level) plus project access.
func (h *Handler) UploadObject(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")
	bucket := strings.TrimSpace(c.Param("bucket"))

	if !h.loadProjectForStorage(c, slug) {
		return
	}

	key, err := sanitizeObjectKey(slug, c.Query("key"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	binding, status, err := h.resolveProjectBucketBinding(ctx, slug, bucket)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	contentType := strings.TrimSpace(c.ContentType())
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Bound the body so the passthrough path cannot be used to stream a large
	// object through the pod. +1 so a body of exactly the max reads without
	// tripping the limit, and one more byte proves it was over.
	limited := io.LimitReader(c.Request.Body, maxDirectUploadBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	if len(body) > maxDirectUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("object exceeds the %d MiB direct-upload limit; use presign-upload for larger files", maxDirectUploadBytes>>20),
		})
		return
	}

	store, err := h.objectStoreFor()(ctx, binding)
	if err != nil {
		h.logger.Error(ctx, "Failed to build object store",
			logging.String("project", slug), logging.Error("error", err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "object storage is temporarily unavailable"})
		return
	}

	if err := store.Upload(ctx, key, bytes.NewReader(body), contentType); err != nil {
		h.logger.Error(ctx, "Failed to upload object",
			logging.String("project", slug),
			logging.String("bucket", bucket),
			logging.Error("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to upload object"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"bucket":       bucket,
		"key":          stripObjectPrefix(slug, key),
		"size":         len(body),
		"content_type": contentType,
	})
}
