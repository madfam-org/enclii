package export

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/notifications"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/storage"
)

// ---------------------------------------------------------------------------
// Bundle provider — pulls project data from the Switchyard repositories.
// ---------------------------------------------------------------------------

// RepoBundleProvider is the production BundleProvider. It reads directly
// from Switchyard repos rather than calling back into the HTTP API —
// faster, no auth dance, and we already have the project's UUID.
type RepoBundleProvider struct {
	repos *db.Repositories
	log   *logrus.Logger
}

// NewRepoBundleProvider builds a provider wired to the given repositories.
func NewRepoBundleProvider(repos *db.Repositories, log *logrus.Logger) *RepoBundleProvider {
	if log == nil {
		log = logrus.StandardLogger()
	}
	return &RepoBundleProvider{repos: repos, log: log}
}

// Fetch loads the project's services, latest deployments, cron jobs,
// env-vars (values redacted for secrets), and database addons.
func (p *RepoBundleProvider) Fetch(ctx context.Context, projectID uuid.UUID) (*ProjectBundle, error) {
	bundle := &ProjectBundle{}

	// Services under the project.
	if p.repos.Services != nil {
		services, err := p.repos.Services.ListByProject(projectID)
		if err != nil {
			return nil, fmt.Errorf("list services: %w", err)
		}
		bundle.Services = services
	}

	// Environments under the project.
	if p.repos.Environments != nil {
		envs, err := p.repos.Environments.ListByProject(projectID)
		if err != nil {
			return nil, fmt.Errorf("list environments: %w", err)
		}
		bundle.Environments = envs
	}

	// Latest deployment per service. The service's release carries the
	// image tag; we enrich the snapshot with that when it resolves.
	for _, svc := range bundle.Services {
		dep, err := p.repos.Deployments.GetLatestByService(ctx, svc.ID.String())
		if err != nil || dep == nil {
			continue
		}
		envName := ""
		for _, e := range bundle.Environments {
			if e.ID == dep.EnvironmentID {
				envName = e.Name
				break
			}
		}
		namespace := ""
		if svc.K8sNamespace != nil {
			namespace = *svc.K8sNamespace
		}
		image := ""
		if p.repos.Releases != nil {
			if rel, err := p.repos.Releases.GetByID(dep.ReleaseID); err == nil && rel != nil {
				image = rel.ImageURI
			}
		}
		bundle.Deployments = append(bundle.Deployments, &DeploymentSnapshot{
			ID:          dep.ID,
			ServiceID:   svc.ID,
			ServiceName: svc.Name,
			Environment: envName,
			Image:       image,
			Replicas:    dep.Replicas,
			Resources:   svc.Resources,
			Namespace:   namespace,
			CreatedAt:   dep.CreatedAt,
		})
	}

	// Env-vars. Values for non-secret kind are included; secrets get
	// "<redacted>" per the secret-scope policy.
	if p.repos.EnvVars != nil {
		for _, svc := range bundle.Services {
			list, err := p.repos.EnvVars.List(ctx, svc.ID, nil)
			if err != nil {
				p.log.WithError(err).
					WithField("service_id", svc.ID).
					Warn("tenant export: env-var listing failed; skipping")
				continue
			}
			for _, ev := range list {
				kind := "plain"
				if ev.IsSecret {
					kind = "secret"
				}
				snap := &EnvVarSnapshot{
					ServiceID:   svc.ID,
					ServiceName: svc.Name,
					Key:         ev.Key,
					Kind:        kind,
					CreatedAt:   ev.CreatedAt,
					UpdatedAt:   ev.UpdatedAt,
				}
				if ev.IsSecret {
					snap.Value = "<redacted>"
				} else {
					snap.Value = ev.Value
				}
				bundle.EnvVars = append(bundle.EnvVars, snap)
			}
		}
	}

	// Database addons bound to the project.
	if p.repos.DatabaseAddons != nil {
		addons, err := p.repos.DatabaseAddons.ListByProject(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("list addons: %w", err)
		}
		bundle.Addons = addons
	}

	// Cron jobs.
	if p.repos.CronJobs != nil {
		crons, err := p.repos.CronJobs.ListByProject(ctx, projectID)
		if err == nil {
			bundle.CronJobs = crons
		}
	}

	// Custom domains — optional, best-effort.
	if p.repos.CustomDomains != nil {
		for _, svc := range bundle.Services {
			domains, err := p.repos.CustomDomains.GetByServiceID(ctx, svc.ID.String())
			if err == nil {
				for i := range domains {
					d := domains[i]
					bundle.CustomDomains = append(bundle.CustomDomains, &d)
				}
			}
		}
	}

	return bundle, nil
}

// ---------------------------------------------------------------------------
// Blob provider — R2 enumeration.
// ---------------------------------------------------------------------------

// R2BlobProvider enumerates project-owned R2 prefixes. Projects conventionally
// own one or more prefixes under the shared storage bucket (artifacts,
// build-logs, project-data/<slug>/). The list here is the superset of
// prefixes we currently use; projects without content in a prefix yield
// an empty manifest and are omitted from the export.
type R2BlobProvider struct {
	r2  *storage.R2Client
	log *logrus.Logger

	// Prefixes to scan, each formatted with the project slug. Use
	// "{slug}" as the placeholder token.
	prefixes []string
}

// NewR2BlobProvider builds a provider. Pass an empty prefixes slice to
// get sensible defaults (artifacts + build-logs + project data).
func NewR2BlobProvider(r2 *storage.R2Client, prefixes []string, log *logrus.Logger) *R2BlobProvider {
	if log == nil {
		log = logrus.StandardLogger()
	}
	if len(prefixes) == 0 {
		prefixes = []string{
			"artifacts/{slug}/",
			"build-logs/{slug}/",
			"project-data/{slug}/",
		}
	}
	return &R2BlobProvider{r2: r2, prefixes: prefixes, log: log}
}

// ListProjectBlobs returns one BlobManifest per non-empty prefix.
func (p *R2BlobProvider) ListProjectBlobs(ctx context.Context, projectSlug string) ([]BlobManifest, error) {
	if p.r2 == nil {
		return nil, nil
	}

	var manifests []BlobManifest
	for _, tmpl := range p.prefixes {
		prefix := strings.ReplaceAll(tmpl, "{slug}", projectSlug)
		objs, err := p.r2.List(ctx, prefix, 1000)
		if err != nil {
			p.log.WithError(err).
				WithField("prefix", prefix).
				Warn("tenant export: R2 list failed; skipping prefix")
			continue
		}
		if len(objs) == 0 {
			continue
		}

		m := BlobManifest{
			Bucket:      bucketNameFromPrefix(tmpl),
			Prefix:      prefix,
			GeneratedAt: time.Now().UTC(),
			ObjectCount: len(objs),
		}
		for _, o := range objs {
			m.TotalBytes += o.Size
			m.Objects = append(m.Objects, BlobInventory{
				Key:          o.Key,
				Size:         o.Size,
				LastModified: o.LastModified,
				ETag:         o.ETag,
			})
		}
		manifests = append(manifests, m)
	}
	return manifests, nil
}

// bucketNameFromPrefix derives a short, file-system-safe bucket tag from
// the prefix template. "artifacts/{slug}/" -> "artifacts".
func bucketNameFromPrefix(tmpl string) string {
	head := tmpl
	if i := strings.Index(head, "/"); i >= 0 {
		head = head[:i]
	}
	return head
}

// ---------------------------------------------------------------------------
// Notifier adapter — thin wrapper over notifications.EmailService.
// ---------------------------------------------------------------------------

// EmailNotifier adapts the platform email service to the Notifier
// interface expected by the export pipeline.
type EmailNotifier struct {
	email *notifications.EmailService
	base  string // app base URL for deep links (e.g. https://app.enclii.dev)
}

// NewEmailNotifier wires a notifier. baseURL is used to build dashboard
// deep links (never signed R2 URLs).
func NewEmailNotifier(email *notifications.EmailService, baseURL string) *EmailNotifier {
	if baseURL == "" {
		baseURL = "https://app.enclii.dev"
	}
	return &EmailNotifier{email: email, base: baseURL}
}

// ExportReady emails the requester with a dashboard deep link. The link
// points at the Switchyard UI, which re-auths the user and issues a fresh
// pre-signed R2 URL server-side — pre-signed URLs never end up in email.
func (n *EmailNotifier) ExportReady(ctx context.Context, to, projectSlug, exportID string) error {
	if n.email == nil {
		return nil
	}
	link := fmt.Sprintf("%s/projects/%s/exports?highlight=%s", n.base, projectSlug, exportID)
	subject := fmt.Sprintf("Your Enclii export for %s is ready", projectSlug)
	text := fmt.Sprintf(
		"Your tenant export for project %q is ready.\n\n"+
			"Download: %s\n\n"+
			"The export is available for 14 days. Each download link is a fresh 15-minute pre-signed URL.\n"+
			"See the README.md inside the tarball for restore instructions.\n",
		projectSlug, link,
	)
	return n.email.SendGeneric(ctx, to, subject, text)
}

// ExportApprovalRequested emails project admins that a production export
// needs a second admin's approval.
func (n *EmailNotifier) ExportApprovalRequested(ctx context.Context, projectSlug, exportID, requestedBy string) error {
	if n.email == nil {
		return nil
	}
	link := fmt.Sprintf("%s/projects/%s/exports?pending=%s", n.base, projectSlug, exportID)
	subject := fmt.Sprintf("Approval required: Enclii export for %s", projectSlug)
	text := fmt.Sprintf(
		"%s has requested a full tenant export of project %q.\n\n"+
			"Approve (or reject): %s\n\n"+
			"If this request is unexpected, investigate and reject. A full export "+
			"includes manifests, pg_dump, blob manifests, and audit timeline.\n",
		requestedBy, projectSlug, link,
	)
	// Target: the project's admin mailing list. For the MVP we send to
	// a fixed platform ops address; follow-up ticket will fan out to
	// project admins once the team-membership query lands in the email
	// service.
	return n.email.SendGeneric(ctx, "", subject, text)
}
