package export

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// This file provides the two export providers that were previously left unwired
// in production, so exports shipped an empty secrets/references.json and an
// empty audit timeline. A tenant leaving must be able to see the shape of their
// secrets (names only, never values) and the full record of who did what.

// ── Audit provider ───────────────────────────────────────────────────────────

// auditQuerier is the slice of the audit-log repository this provider needs.
// Declared here (not imported as a concrete type) so the provider is trivially
// testable with a fake and does not widen the export package's dependency graph.
type auditQuerier interface {
	QueryByProject(ctx context.Context, projectID uuid.UUID, limit, offset int) ([]*types.AuditLog, error)
}

// RepoAuditProvider fills the export's audit timeline from the immutable
// audit_logs table, scoped to the leaving project.
type RepoAuditProvider struct {
	q auditQuerier
	// pageSize bounds each DB round-trip; the provider pages until drained or
	// maxEvents is reached so one enormous project can't OOM the export pod.
	pageSize  int
	maxEvents int
}

// NewRepoAuditProvider builds the provider. pageSize/maxEvents get sane defaults.
func NewRepoAuditProvider(q auditQuerier) *RepoAuditProvider {
	return &RepoAuditProvider{q: q, pageSize: 500, maxEvents: 100_000}
}

// deploymentActions are the audit actions that belong in deployments.ndjson
// rather than the general timeline. Anything not listed lands in the timeline.
var deploymentActions = map[string]bool{
	"deploy":             true,
	"deploy.rolled_back": true,
	"rollback":           true,
	"promote":            true,
	"scale":              true,
	"canary.promote":     true,
	"canary.rollback":    true,
}

// ProjectEvents returns (timeline, deployments) for the project, both sorted
// oldest-first so the exported NDJSON reads chronologically.
func (p *RepoAuditProvider) ProjectEvents(ctx context.Context, projectID uuid.UUID) (timeline, deployments []AuditEvent, err error) {
	offset := 0
	for {
		rows, qErr := p.q.QueryByProject(ctx, projectID, p.pageSize, offset)
		if qErr != nil {
			return nil, nil, fmt.Errorf("audit query (offset %d): %w", offset, qErr)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			ev := auditLogToEvent(r)
			if deploymentActions[r.Action] {
				deployments = append(deployments, ev)
			} else {
				timeline = append(timeline, ev)
			}
		}
		offset += len(rows)
		if len(rows) < p.pageSize || offset >= p.maxEvents {
			break
		}
	}
	reverseEvents(timeline) // QueryByProject returns newest-first; export reads oldest-first.
	reverseEvents(deployments)
	return timeline, deployments, nil
}

func auditLogToEvent(r *types.AuditLog) AuditEvent {
	detail := map[string]interface{}{}
	for k, v := range r.Context {
		detail[k] = v
	}
	if r.ResourceType != "" {
		detail["resource_type"] = r.ResourceType
	}
	if r.ResourceName != "" {
		detail["resource_name"] = r.ResourceName
	}
	if r.Outcome != "" {
		detail["outcome"] = r.Outcome
	}
	if len(detail) == 0 {
		detail = nil
	}
	return AuditEvent{
		Source:     "switchyard",
		Timestamp:  r.Timestamp,
		Actor:      r.ActorEmail,
		Action:     r.Action,
		ResourceID: r.ResourceID,
		Detail:     detail,
	}
}

func reverseEvents(s []AuditEvent) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// ── Secret provider ──────────────────────────────────────────────────────────

// namespaceForProject mirrors addon_service's `project-<uuid8>` scheme. Secrets
// for a project's addons/services live in that namespace.
func namespaceForProject(projectID uuid.UUID) string {
	return "project-" + projectID.String()[:8]
}

// K8sSecretProvider lists Secrets in the project's namespace and returns
// name+type+key-count metadata only — never a value.
type K8sSecretProvider struct {
	client   *k8s.Client
	projects projectSlugResolver
}

// projectSlugResolver maps a slug back to the project (for the namespace). The
// export service passes the slug; the namespace scheme keys on the UUID.
type projectSlugResolver interface {
	GetBySlug(slug string) (*types.Project, error)
}

// NewK8sSecretProvider builds the provider.
func NewK8sSecretProvider(client *k8s.Client, projects projectSlugResolver) *K8sSecretProvider {
	return &K8sSecretProvider{client: client, projects: projects}
}

// ListSecretReferences returns one SecretReference per Secret in the project
// namespace. Service-account token secrets and Helm release secrets are skipped
// — they are platform noise, not the tenant's own credentials.
func (p *K8sSecretProvider) ListSecretReferences(ctx context.Context, projectSlug string) ([]SecretReference, error) {
	project, err := p.projects.GetBySlug(projectSlug)
	if err != nil {
		return nil, fmt.Errorf("resolve project %q: %w", projectSlug, err)
	}
	ns := namespaceForProject(project.ID)

	list, err := p.client.Clientset.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list secrets in %s: %w", ns, err)
	}

	refs := make([]SecretReference, 0, len(list.Items))
	for i := range list.Items {
		s := &list.Items[i]
		if skipSecret(s) {
			continue
		}
		ref := SecretReference{
			Name:      s.Name,
			Type:      string(s.Type),
			CreatedAt: s.CreationTimestamp.Time,
			KeyCount:  len(s.Data),
			Scope:     "project",
		}
		if rotated := lastRotated(s); rotated != nil {
			ref.LastRotatedAt = rotated
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// skipSecret drops platform-managed secrets that aren't the tenant's own data.
func skipSecret(s *corev1.Secret) bool {
	switch s.Type {
	case corev1.SecretTypeServiceAccountToken:
		return true
	}
	if strings.HasPrefix(s.Name, "sh.helm.release.") {
		return true
	}
	if strings.HasPrefix(s.Name, "default-token-") {
		return true
	}
	return false
}

// lastRotated reads a rotation timestamp from a conventional annotation if the
// platform set one; nil when unknown (the export omits the field).
func lastRotated(s *corev1.Secret) *time.Time {
	for _, key := range []string{"enclii.dev/last-rotated-at", "enclii.dev/rotated-at"} {
		if v, ok := s.Annotations[key]; ok && v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return &t
			}
		}
	}
	return nil
}
