package export

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ErrExportNotFound is surfaced when a caller targets a missing export.
var ErrExportNotFound = errors.New("tenant export not found")

// ErrUnauthorizedExport is surfaced when the caller lacks project-admin.
var ErrUnauthorizedExport = errors.New("unauthorized for this export")

// ErrApprovalRequired is returned when a prod export is initiated without
// the approval flow having been resolved.
var ErrApprovalRequired = errors.New("export requires approval before running")

// ErrSelfApproval is returned when the approver equals the requester in
// production. Non-prod accepts same-actor for dev ergonomics.
var ErrSelfApproval = errors.New("approver must differ from requester in production")

// Storage is the minimal R2 surface the service depends on. Matches what
// storage.R2Client already provides. Isolating it here makes the service
// testable without a live R2.
type Storage interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) error
	Delete(ctx context.Context, key string) error
	GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error)
}

// BundleProvider fetches a ProjectBundle for the given project. The
// production impl lives in bundle_provider.go and reaches into Switchyard
// repos + the K8s client.
type BundleProvider interface {
	Fetch(ctx context.Context, projectID uuid.UUID) (*ProjectBundle, error)
}

// DumpProvider runs pg_dump for each bound database addon and returns
// gzipped bytes. Production impl submits a K8s Job; tests feed canned
// bytes.
type DumpProvider interface {
	Dump(ctx context.Context, addons []*types.DatabaseAddon) ([]DBDump, error)
}

// BlobProvider enumerates R2 blobs owned by the project (by prefix) and
// returns per-bucket manifests.
type BlobProvider interface {
	ListProjectBlobs(ctx context.Context, projectSlug string) ([]BlobManifest, error)
}

// AuditProvider fetches project-scoped audit events from the consolidated
// audit surface (Selva RFC 0005-0008 + Switchyard lifecycle).
type AuditProvider interface {
	ProjectEvents(ctx context.Context, projectID uuid.UUID) (timeline, deployments []AuditEvent, err error)
}

// SecretProvider lists k8s Secrets in the project's namespace and returns
// name+type metadata only (never values).
type SecretProvider interface {
	ListSecretReferences(ctx context.Context, projectSlug string) ([]SecretReference, error)
}

// Notifier sends "your export is ready" emails. In practice this is
// notifications.EmailService; tests use a stub.
type Notifier interface {
	ExportReady(ctx context.Context, to, projectSlug, exportID string) error
	ExportApprovalRequested(ctx context.Context, projectSlug, exportID, requestedBy string) error
}

// Service orchestrates a tenant export end-to-end. One Service per
// API-pod; it spawns goroutines per export.
type Service struct {
	repo           *db.TenantExportRepository
	projects       *db.ProjectRepository
	projectAccess  *db.ProjectAccessRepository
	storage        Storage
	bundleProvider BundleProvider
	dumpProvider   DumpProvider
	blobProvider   BlobProvider
	auditProvider  AuditProvider
	secretProvider SecretProvider
	notifier       Notifier

	logger *logrus.Logger

	r2BucketPrefix string // e.g. "tenant-exports"
	isProd         bool
}

// Config assembles a Service. Nil providers are tolerated — a real
// deployment may not have every surface wired yet (e.g. audit via
// nexus-api), and the pipeline simply emits empty sections rather than
// refusing to run. This is deliberate: a partial export is better than
// no export for a customer trying to leave.
type Config struct {
	Repo           *db.TenantExportRepository
	Projects       *db.ProjectRepository
	ProjectAccess  *db.ProjectAccessRepository
	Storage        Storage
	BundleProvider BundleProvider
	DumpProvider   DumpProvider
	BlobProvider   BlobProvider
	AuditProvider  AuditProvider
	SecretProvider SecretProvider
	Notifier       Notifier
	Logger         *logrus.Logger
	R2Prefix       string
	IsProduction   bool
}

// NewService builds a Service. Required dependencies are repo + projects
// + storage. The rest are optional; the pipeline tolerates nils.
func NewService(cfg Config) (*Service, error) {
	if cfg.Repo == nil {
		return nil, fmt.Errorf("tenant export: repo required")
	}
	if cfg.Projects == nil {
		return nil, fmt.Errorf("tenant export: projects repo required")
	}
	if cfg.Storage == nil {
		return nil, fmt.Errorf("tenant export: storage required")
	}
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.R2Prefix == "" {
		cfg.R2Prefix = "tenant-exports"
	}
	return &Service{
		repo:           cfg.Repo,
		projects:       cfg.Projects,
		projectAccess:  cfg.ProjectAccess,
		storage:        cfg.Storage,
		bundleProvider: cfg.BundleProvider,
		dumpProvider:   cfg.DumpProvider,
		blobProvider:   cfg.BlobProvider,
		auditProvider:  cfg.AuditProvider,
		secretProvider: cfg.SecretProvider,
		notifier:       cfg.Notifier,
		logger:         cfg.Logger,
		r2BucketPrefix: cfg.R2Prefix,
		isProd:         cfg.IsProduction,
	}, nil
}

// InitiateRequest captures the caller context when a POST /exports lands.
type InitiateRequest struct {
	ProjectSlug string
	UserID      uuid.UUID // Janua sub / user id
	UserEmail   string
	UserRole    string // "admin" | "developer" | "viewer"
}

// Initiate creates the tenant_exports row and, in non-prod, kicks off the
// pipeline. Returns the created row for the handler to echo back.
func (s *Service) Initiate(ctx context.Context, req InitiateRequest) (*types.TenantExport, error) {
	// Project lookup.
	project, err := s.projects.GetBySlug(req.ProjectSlug)
	if err != nil {
		return nil, fmt.Errorf("project lookup: %w", err)
	}
	if project == nil {
		return nil, ErrUnauthorizedExport // don't leak existence
	}

	// Authz: project-admin required. Platform admins bypass.
	if !s.isPlatformAdmin(req.UserRole) {
		if err := s.requireProjectAdmin(ctx, req.UserID, project.ID); err != nil {
			return nil, err
		}
	}

	// Build the row. Prod -> pending + HITL; non-prod -> running.
	status := types.TenantExportStatusRunning
	if s.isProd {
		status = types.TenantExportStatusPending
	}

	export := &types.TenantExport{
		ProjectID:   project.ID,
		Status:      status,
		RequestedBy: requestedByLabel(req),
		PartCount:   1,
	}
	if err := s.repo.Create(ctx, export); err != nil {
		return nil, fmt.Errorf("create export row: %w", err)
	}

	if s.isProd {
		// Emit approval request (email to project admins). Failures here
		// are non-fatal — the row is already pending and an ops dashboard
		// will surface it; we just log.
		if s.notifier != nil {
			if err := s.notifier.ExportApprovalRequested(
				ctx, project.Slug, export.ID.String(), export.RequestedBy,
			); err != nil {
				s.logger.WithError(err).
					WithField("export_id", export.ID).
					Warn("tenant export: approval request email failed")
			}
		}
		return export, nil
	}

	// Non-prod: run async.
	go s.runPipeline(context.Background(), project, export)
	return export, nil
}

// Approve transitions a pending row to running and launches the pipeline.
// Only meaningful in production. Enforces approver != requester.
func (s *Service) Approve(ctx context.Context, req InitiateRequest, exportID uuid.UUID) (*types.TenantExport, error) {
	export, err := s.repo.GetByID(ctx, exportID)
	if err != nil {
		return nil, err
	}
	if export.Status != types.TenantExportStatusPending {
		return nil, fmt.Errorf("cannot approve export in status %s", export.Status)
	}

	if !s.isPlatformAdmin(req.UserRole) {
		if err := s.requireProjectAdmin(ctx, req.UserID, export.ProjectID); err != nil {
			return nil, err
		}
	}

	approver := requestedByLabel(req)
	if s.isProd && approver == export.RequestedBy {
		return nil, ErrSelfApproval
	}

	if err := s.repo.Update(ctx, export.ID, db.TenantExportUpdate{
		Status:     types.TenantExportStatusRunning,
		ApprovedBy: &approver,
	}); err != nil {
		return nil, fmt.Errorf("approve: %w", err)
	}

	project, err := s.projects.GetByID(ctx, export.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project lookup for approved export: %w", err)
	}
	if project == nil {
		return nil, fmt.Errorf("approved export references missing project %s", export.ProjectID)
	}
	go s.runPipeline(context.Background(), project, export)

	// Reload for the response.
	return s.repo.GetByID(ctx, export.ID)
}

// Get returns one export with a freshly pre-signed download URL when
// status=ready. Enforces project-admin on the project.
func (s *Service) Get(ctx context.Context, req InitiateRequest, exportID uuid.UUID) (*types.TenantExportDownload, error) {
	export, err := s.repo.GetByID(ctx, exportID)
	if err != nil {
		return nil, err
	}
	if !s.isPlatformAdmin(req.UserRole) {
		if err := s.requireProjectAdmin(ctx, req.UserID, export.ProjectID); err != nil {
			return nil, err
		}
	}

	resp := &types.TenantExportDownload{Export: export}
	if export.Status == types.TenantExportStatusReady && export.TarballR2Key != nil {
		url, err := s.storage.GetPresignedURL(ctx, *export.TarballR2Key, 15*time.Minute)
		if err != nil {
			s.logger.WithError(err).WithField("export_id", export.ID).
				Warn("tenant export: presign failed")
		} else {
			resp.DownloadURL = url
			resp.ExpiresIn = int((15 * time.Minute).Seconds())
		}
	}
	return resp, nil
}

// List returns recent exports for a project (newest first).
func (s *Service) List(ctx context.Context, req InitiateRequest, projectSlug string) ([]*types.TenantExport, error) {
	project, err := s.projects.GetBySlug(projectSlug)
	if err != nil {
		return nil, fmt.Errorf("project lookup: %w", err)
	}
	if project == nil {
		return nil, ErrUnauthorizedExport
	}
	// Any project member can list; the download is still gated.
	return s.repo.ListByProject(ctx, project.ID, 100)
}

// Delete soft-deletes and purges the tarball from R2 immediately.
func (s *Service) Delete(ctx context.Context, req InitiateRequest, exportID uuid.UUID) error {
	export, err := s.repo.GetByID(ctx, exportID)
	if err != nil {
		return err
	}
	if !s.isPlatformAdmin(req.UserRole) {
		if err := s.requireProjectAdmin(ctx, req.UserID, export.ProjectID); err != nil {
			return err
		}
	}

	if export.TarballR2Key != nil {
		if err := s.storage.Delete(ctx, *export.TarballR2Key); err != nil {
			s.logger.WithError(err).WithField("export_id", export.ID).
				Warn("tenant export: R2 delete failed (row will still be marked deleted)")
		}
	}

	reason := "deleted by user"
	return s.repo.Update(ctx, export.ID, db.TenantExportUpdate{
		Status:       types.TenantExportStatusDeleted,
		ErrorMessage: &reason,
	})
}

// ---------------------------------------------------------------------------
// Pipeline
// ---------------------------------------------------------------------------

// runPipeline is the fire-and-forget worker. It takes a detached context
// so the request that triggered it can return 202 immediately.
func (s *Service) runPipeline(ctx context.Context, project *types.Project, export *types.TenantExport) {
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()

	log := s.logger.WithFields(logrus.Fields{
		"export_id":    export.ID,
		"project_id":   project.ID,
		"project_slug": project.Slug,
	})
	log.Info("tenant export: pipeline started")

	// Partial-tarball cleanup on error. We track any keys we uploaded so
	// a failure path can purge them.
	var uploadedKeys []string
	failExport := func(errMsg string) {
		log.WithField("error", errMsg).Error("tenant export: pipeline failed")
		for _, k := range uploadedKeys {
			if err := s.storage.Delete(ctx, k); err != nil {
				log.WithError(err).WithField("r2_key", redactR2Key(k)).
					Warn("tenant export: cleanup delete failed")
			}
		}
		if err := s.repo.Update(ctx, export.ID, db.TenantExportUpdate{
			Status:       types.TenantExportStatusFailed,
			ErrorMessage: &errMsg,
		}); err != nil {
			log.WithError(err).Error("tenant export: failed to persist failure")
		}
	}

	// 1. Bundle — manifests, services, deployments, addons, env-var refs.
	bundle := &ProjectBundle{Project: project}
	if s.bundleProvider != nil {
		b, err := s.bundleProvider.Fetch(ctx, project.ID)
		if err != nil {
			failExport(fmt.Sprintf("bundle fetch: %v", err))
			return
		}
		if b != nil {
			bundle = b
			bundle.Project = project
		}
	}

	// 2. pg_dump per addon.
	var dumps []DBDump
	if s.dumpProvider != nil && len(bundle.Addons) > 0 {
		d, err := s.dumpProvider.Dump(ctx, bundle.Addons)
		if err != nil {
			failExport(fmt.Sprintf("pg_dump: %v", err))
			return
		}
		dumps = d
	}

	// 3. R2 blob inventory.
	var blobs []BlobManifest
	if s.blobProvider != nil {
		b, err := s.blobProvider.ListProjectBlobs(ctx, project.Slug)
		if err != nil {
			// Non-fatal — log a warning and continue with empty blobs.
			log.WithError(err).Warn("tenant export: blob listing failed; continuing with empty inventory")
		} else {
			blobs = b
		}
	}

	// 4. Secret references (names only, never values).
	var secretRefs []SecretReference
	if s.secretProvider != nil {
		refs, err := s.secretProvider.ListSecretReferences(ctx, project.Slug)
		if err != nil {
			log.WithError(err).Warn("tenant export: secret listing failed; continuing")
		} else {
			secretRefs = refs
		}
	}

	// 5. Audit timeline scoped to project.
	var timeline, deployments []AuditEvent
	if s.auditProvider != nil {
		tl, dp, err := s.auditProvider.ProjectEvents(ctx, project.ID)
		if err != nil {
			log.WithError(err).Warn("tenant export: audit pull failed; continuing")
		} else {
			timeline = tl
			deployments = dp
		}
	}

	// 6. Assemble.
	builder := NewBuilder()
	AddReadme(builder, project.Slug, export.ID.String(), time.Now().UTC())
	if err := AddProjectManifests(builder, bundle); err != nil {
		failExport(fmt.Sprintf("manifests: %v", err))
		return
	}
	if err := AddDatabaseDumps(builder, dumps); err != nil {
		failExport(fmt.Sprintf("database dumps: %v", err))
		return
	}
	if err := AddBlobManifests(builder, blobs); err != nil {
		failExport(fmt.Sprintf("blob manifests: %v", err))
		return
	}
	if err := AddSecretReferences(builder, secretRefs); err != nil {
		failExport(fmt.Sprintf("secret refs: %v", err))
		return
	}
	if err := AddAuditTimeline(builder, timeline, deployments); err != nil {
		failExport(fmt.Sprintf("audit timeline: %v", err))
		return
	}

	parts, manifest, err := builder.Build()
	if err != nil {
		failExport(fmt.Sprintf("builder: %v", err))
		return
	}

	// Inject MANIFEST.json by rebuilding (once) — Build is deterministic.
	// Instead of mutating entries, we just build a second time with the
	// manifest at the head. This is cheaper than an in-place re-tar.
	if err := builder.AddJSON("MANIFEST.json", manifest); err != nil {
		failExport(fmt.Sprintf("manifest.json: %v", err))
		return
	}
	parts, _, err = builder.Build()
	if err != nil {
		failExport(fmt.Sprintf("builder (with manifest): %v", err))
		return
	}

	// 7. Upload parts to R2.
	baseKey := fmt.Sprintf("%s/%s/%s", s.r2BucketPrefix, project.Slug, export.ID)
	var totalSize int64
	var tarballKey string
	var tarballSha string

	if len(parts) == 1 {
		part := parts[0]
		key := fmt.Sprintf("%s/part001.tar.gz", baseKey)
		if err := s.storage.Upload(ctx, key, readerOf(part.Data), "application/gzip"); err != nil {
			failExport(fmt.Sprintf("upload: %v", err))
			return
		}
		uploadedKeys = append(uploadedKeys, key)
		tarballKey = key
		tarballSha = part.SHA256
		totalSize = part.Size
	} else {
		// Multi-part: upload each, write index.
		index := make([]map[string]interface{}, 0, len(parts))
		for _, p := range parts {
			key := fmt.Sprintf("%s/part%03d.tar.gz", baseKey, p.Index)
			if err := s.storage.Upload(ctx, key, readerOf(p.Data), "application/gzip"); err != nil {
				failExport(fmt.Sprintf("upload part %d: %v", p.Index, err))
				return
			}
			uploadedKeys = append(uploadedKeys, key)
			index = append(index, map[string]interface{}{
				"part":   p.Index,
				"key":    key,
				"size":   p.Size,
				"sha256": p.SHA256,
				"paths":  p.Paths,
			})
			totalSize += p.Size
		}
		// The row's tarball_r2_key points at the index.json in multi-part.
		indexKey := fmt.Sprintf("%s/index.json", baseKey)
		indexBody, err := marshalIndex(index, manifest)
		if err != nil {
			failExport(fmt.Sprintf("index: %v", err))
			return
		}
		if err := s.storage.Upload(ctx, indexKey, readerOf(indexBody), "application/json"); err != nil {
			failExport(fmt.Sprintf("upload index: %v", err))
			return
		}
		uploadedKeys = append(uploadedKeys, indexKey)
		tarballKey = indexKey
		tarballSha = computeIndexSha(indexBody)
	}

	// 8. Mark ready.
	expires := time.Now().UTC().Add(14 * 24 * time.Hour)
	partCount := len(parts)
	if err := s.repo.Update(ctx, export.ID, db.TenantExportUpdate{
		Status:           types.TenantExportStatusReady,
		TarballR2Key:     &tarballKey,
		TarballSizeBytes: &totalSize,
		SHA256:           &tarballSha,
		PartCount:        &partCount,
		ExpiresAt:        &expires,
	}); err != nil {
		failExport(fmt.Sprintf("update ready: %v", err))
		return
	}

	// 9. Email the customer. Email failures are logged but don't fail
	// the export — they can still download from the dashboard.
	if s.notifier != nil && s.notifier != Notifier(nil) {
		if err := s.notifier.ExportReady(ctx, export.RequestedBy, project.Slug, export.ID.String()); err != nil {
			log.WithError(err).Warn("tenant export: notification email failed")
		}
	}

	log.WithFields(logrus.Fields{
		"size_bytes":  totalSize,
		"part_count":  partCount,
		"sha256":      tarballSha,
		"duration_ms": time.Since(export.RequestedAt).Milliseconds(),
	}).Info("tenant export: ready")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Service) isPlatformAdmin(role string) bool {
	return strings.EqualFold(role, string(types.RoleAdmin))
}

// requireProjectAdmin asserts the user has admin role on the project. The
// project_access table stores per-(user, project) role.
func (s *Service) requireProjectAdmin(ctx context.Context, userID, projectID uuid.UUID) error {
	if s.projectAccess == nil {
		// Without RBAC wired, fail closed.
		return ErrUnauthorizedExport
	}
	has, err := s.projectAccess.HasAccess(ctx, userID, projectID, nil, types.RoleAdmin)
	if err != nil {
		return fmt.Errorf("project access check: %w", err)
	}
	if !has {
		return ErrUnauthorizedExport
	}
	return nil
}

// requestedByLabel builds a stable identity string for audit: UUID|email.
// Prefers email when present because audit readers expect human-readable
// actors.
func requestedByLabel(req InitiateRequest) string {
	if req.UserEmail != "" {
		return req.UserEmail
	}
	if req.UserID != uuid.Nil {
		return req.UserID.String()
	}
	return "unknown"
}

// redactR2Key strips pre-signed URL query strings and leaves just the
// bucket/key form for safe logging. Logs never get the full URL.
func redactR2Key(key string) string {
	if idx := strings.IndexByte(key, '?'); idx >= 0 {
		return key[:idx] + "?<redacted>"
	}
	return key
}

// readerOf wraps a byte slice as an io.Reader without pulling bytes.NewReader
// into the caller's namespace — keeps the pipeline readable.
func readerOf(data []byte) io.Reader { return &byteReader{data: data} }

type byteReader struct {
	data []byte
	pos  int
}

func (b *byteReader) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

// marshalIndex writes the index.json body for multi-part exports.
func marshalIndex(parts []map[string]interface{}, manifest Manifest) ([]byte, error) {
	payload := map[string]interface{}{
		"format":       "enclii-tenant-export-index/v1",
		"created_at":   time.Now().UTC(),
		"manifest":     manifest,
		"parts":        parts,
		"instructions": "Download each part, verify its sha256, then: cat part*.tar.gz | tar -xz",
	}
	return jsonMarshalIndent(payload)
}

// computeIndexSha returns the sha256 of the index body. Used only when
// the tenant_exports row's tarball_r2_key points at the index (multi-
// part). Single-part exports store the tarball's own sha.
func computeIndexSha(body []byte) string {
	return hashSHA256(body)
}

// presignKeyRegex is kept here as a future-proofing aid: any log line
// that shouldn't carry a pre-signed URL gets matched against this to
// guard against accidental inclusion. It's not yet used but intentionally
// exported within the package for static analysis + future hooks.
var presignKeyRegex = regexp.MustCompile(`(?i)X-Amz-Signature=[^&]+`)

// redactQuerySignatures strips AWS signatures from any URL we might log.
func redactQuerySignatures(s string) string {
	return presignKeyRegex.ReplaceAllString(s, "X-Amz-Signature=<redacted>")
}
