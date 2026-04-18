package export

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ProjectBundle is the bag of project-scoped data the pipeline hands to
// the builder. Each field is optional — a project may have no addons,
// no custom domains, no webhooks, etc. The bundle is intentionally
// concrete (no interface{} blobs) so tests can construct one without a
// live platform behind them.
type ProjectBundle struct {
	Project       *types.Project
	Environments  []*types.Environment
	Services      []*types.Service
	Deployments   []*DeploymentSnapshot
	CronJobs      []*types.CronJob
	EnvVars       []*EnvVarSnapshot // values are already redacted
	Addons        []*types.DatabaseAddon
	Webhooks      []*types.OutboundWebhookSubscription
	CustomDomains []*types.CustomDomain
}

// DeploymentSnapshot is a thinned-down view of a deployment for the
// tarball — enough to recreate a Deployment + Service manifest without
// pulling the full deployment_lifecycle history.
type DeploymentSnapshot struct {
	ID          uuid.UUID             `json:"id"`
	ServiceID   uuid.UUID             `json:"service_id"`
	ServiceName string                `json:"service_name"`
	Environment string                `json:"environment"`
	Image       string                `json:"image"`
	Replicas    int                   `json:"replicas"`
	Resources   *types.ResourceConfig `json:"resources,omitempty"`
	Namespace   string                `json:"namespace"`
	CreatedAt   time.Time             `json:"created_at"`
}

// EnvVarSnapshot carries env var metadata with values always redacted.
// The reference the customer cares about is the key name and type —
// values come from Vault post-leave, not from this tarball.
type EnvVarSnapshot struct {
	ServiceID   uuid.UUID `json:"service_id"`
	ServiceName string    `json:"service_name"`
	Environment string    `json:"environment,omitempty"`
	Key         string    `json:"key"`
	Kind        string    `json:"kind"` // "secret" | "plain"
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Value is always the literal string "<redacted>" for secret kind.
	// Plain env vars carry their value — same visibility as the Switchyard
	// UI's RevealEnvVar endpoint; the tarball is no more sensitive.
	Value string `json:"value,omitempty"`
}

// BlobManifest is the per-bucket inventory produced by r2Gatherer.
type BlobManifest struct {
	Bucket      string          `json:"bucket"`
	Prefix      string          `json:"prefix"`
	GeneratedAt time.Time       `json:"generated_at"`
	ObjectCount int             `json:"object_count"`
	TotalBytes  int64           `json:"total_bytes"`
	Objects     []BlobInventory `json:"objects"`
}

type BlobInventory struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag,omitempty"`
}

// SecretReference is entered into secrets/references.json. Values are
// never included. This is the whole point.
type SecretReference struct {
	Name          string     `json:"name"`
	Type          string     `json:"type"` // k8s secret type, e.g. "Opaque"
	CreatedAt     time.Time  `json:"created_at"`
	LastRotatedAt *time.Time `json:"last_rotated_at,omitempty"`
	KeyCount      int        `json:"key_count"` // count of keys in the secret
	Scope         string     `json:"scope"`     // "project" | "service:<id>"
}

// AuditEvent is one row of the audit timeline. Source identifies where
// the event came from (switchyard, janua, selva-rfc-0005, ...).
type AuditEvent struct {
	Source     string                 `json:"source"`
	Timestamp  time.Time              `json:"timestamp"`
	Actor      string                 `json:"actor,omitempty"`
	Action     string                 `json:"action"`
	ResourceID string                 `json:"resource_id,omitempty"`
	Detail     map[string]interface{} `json:"detail,omitempty"`
}

// marshalYAML writes v as YAML (2-space indent). Never returns nil bytes.
func marshalYAML(v interface{}) ([]byte, error) {
	out, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("yaml marshal: %w", err)
	}
	return out, nil
}

// marshalNDJSON writes v as newline-delimited JSON. Stable, sorted by the
// caller before passing in — this just does the line-per-item loop.
func marshalNDJSON(items []AuditEvent) ([]byte, error) {
	var buf []byte
	for i := range items {
		line, err := json.Marshal(items[i])
		if err != nil {
			return nil, fmt.Errorf("ndjson item %d: %w", i, err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return buf, nil
}

// AddProjectManifests writes the K8s-flavored manifests into the bundle:
// manifests/project.yaml, manifests/services/*.yaml, manifests/cron_jobs/*.yaml.
//
// The namespace is scrubbed to a placeholder so the customer can
// kubectl apply into whatever namespace they choose post-restore. Secret
// references are left in place (names only; values land in
// secrets/references.json).
func AddProjectManifests(b *Builder, bundle *ProjectBundle) error {
	if bundle.Project == nil {
		return fmt.Errorf("bundle missing project")
	}

	// project.yaml — the Enclii-native project spec.
	projectYAML, err := marshalYAML(map[string]interface{}{
		"apiVersion": "enclii.dev/v1",
		"kind":       "Project",
		"name":       bundle.Project.Name,
		"slug":       bundle.Project.Slug,
		"ci_runner":  string(bundle.Project.CIRunnerMode),
		"created_at": bundle.Project.CreatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	b.AddEntry(Entry{Path: "manifests/project.yaml", Content: projectYAML})

	// services/*.yaml — one file per service.
	sortedServices := append([]*types.Service(nil), bundle.Services...)
	sort.Slice(sortedServices, func(i, j int) bool {
		return sortedServices[i].Name < sortedServices[j].Name
	})
	for _, svc := range sortedServices {
		data, err := marshalYAML(map[string]interface{}{
			"apiVersion":   "enclii.dev/v1",
			"kind":         "Service",
			"name":         svc.Name,
			"git_repo":     svc.GitRepo,
			"app_path":     svc.AppPath,
			"build_config": svc.BuildConfig,
			"health_check": svc.HealthCheck,
			"resources":    svc.Resources,
		})
		if err != nil {
			return err
		}
		b.AddEntry(Entry{
			Path:    fmt.Sprintf("manifests/services/%s.yaml", svc.Name),
			Content: data,
		})
	}

	// deployments/*.yaml — latest per service.
	for _, dep := range bundle.Deployments {
		data, err := marshalYAML(map[string]interface{}{
			"apiVersion":  "enclii.dev/v1",
			"kind":        "Deployment",
			"service":     dep.ServiceName,
			"environment": dep.Environment,
			"image":       dep.Image,
			"replicas":    dep.Replicas,
			"resources":   dep.Resources,
			"namespace":   "<placeholder>", // scrubbed
			"created_at":  dep.CreatedAt.UTC().Format(time.RFC3339),
		})
		if err != nil {
			return err
		}
		b.AddEntry(Entry{
			Path:    fmt.Sprintf("manifests/deployments/%s.yaml", dep.ServiceName),
			Content: data,
		})
	}

	// cron_jobs/*.yaml
	for _, cj := range bundle.CronJobs {
		data, err := marshalYAML(map[string]interface{}{
			"apiVersion": "enclii.dev/v1",
			"kind":       "CronJob",
			"name":       cj.Name,
			"schedule":   cj.Schedule,
			"command":    cj.Command,
			"image":      cj.Image,
			"timeout":    cj.Timeout,
		})
		if err != nil {
			return err
		}
		b.AddEntry(Entry{
			Path:    fmt.Sprintf("manifests/cron_jobs/%s.yaml", cj.Name),
			Content: data,
		})
	}

	// env-vars metadata per service.
	byService := map[string][]*EnvVarSnapshot{}
	for _, ev := range bundle.EnvVars {
		byService[ev.ServiceName] = append(byService[ev.ServiceName], ev)
	}
	for svcName, evs := range byService {
		if err := b.AddJSON(
			fmt.Sprintf("manifests/envvars/%s.json", svcName),
			evs,
		); err != nil {
			return err
		}
	}

	return nil
}

// AddBlobManifests writes one blobs/<bucket>/manifest.json per R2 bucket.
// Empty-prefix buckets get skipped — if there's nothing in the bucket we
// don't need to tell the customer anything about it.
func AddBlobManifests(b *Builder, manifests []BlobManifest) error {
	for _, m := range manifests {
		if m.ObjectCount == 0 {
			continue
		}
		if err := b.AddJSON(
			fmt.Sprintf("blobs/%s/manifest.json", m.Bucket),
			m,
		); err != nil {
			return err
		}
	}
	return nil
}

// AddSecretReferences writes secrets/references.json. Always includes the
// file even if the list is empty — customers need a positive signal that
// the export *intentionally* does not carry values.
func AddSecretReferences(b *Builder, refs []SecretReference) error {
	return b.AddJSON("secrets/references.json", map[string]interface{}{
		"note":       "Secret values are intentionally excluded from tenant exports. Rotate post-leave via Vault.",
		"count":      len(refs),
		"references": refs,
	})
}

// AddAuditTimeline writes audit/timeline.ndjson and deployments.ndjson.
// The caller has already scoped events to the project.
func AddAuditTimeline(b *Builder, events []AuditEvent, deployments []AuditEvent) error {
	tl, err := marshalNDJSON(events)
	if err != nil {
		return err
	}
	b.AddEntry(Entry{Path: "audit/timeline.ndjson", Content: tl})

	dep, err := marshalNDJSON(deployments)
	if err != nil {
		return err
	}
	b.AddEntry(Entry{Path: "audit/deployments.ndjson", Content: dep})
	return nil
}

// AddDatabaseDumps writes databases/<addon>/... for each pg_dump the
// caller has already produced. The dumps are passed in as raw bytes —
// the caller is responsible for running pg_dump (either locally or via a
// K8s Job) and gzipping.
type DBDump struct {
	AddonName string
	AddonMeta *types.DatabaseAddon
	DumpGz    []byte // custom-format pg_dump, already gzipped
	SchemaSQL []byte // plain SQL schema (for grep-ability)
}

func AddDatabaseDumps(b *Builder, dumps []DBDump) error {
	for _, d := range dumps {
		if len(d.DumpGz) > 0 {
			b.AddEntry(Entry{
				Path:    fmt.Sprintf("databases/%s/pg_dump.sql.gz", d.AddonName),
				Content: d.DumpGz,
			})
		}
		if len(d.SchemaSQL) > 0 {
			b.AddEntry(Entry{
				Path:    fmt.Sprintf("databases/%s/schema.sql", d.AddonName),
				Content: d.SchemaSQL,
			})
		}
		if d.AddonMeta != nil {
			if err := b.AddJSON(
				fmt.Sprintf("databases/%s/addon.json", d.AddonName),
				addonMetaFor(d.AddonMeta),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// addonMetaFor trims the addon down to what the customer cares about
// (names, versions, sizes) — no provisioner internals.
func addonMetaFor(a *types.DatabaseAddon) map[string]interface{} {
	return map[string]interface{}{
		"name":               a.Name,
		"type":               string(a.Type),
		"version":            a.Config.Version,
		"storage_gb":         a.Config.StorageGB,
		"storage_used_bytes": a.StorageUsedBytes,
		"ha_enabled":         a.Config.HAEnabled,
		"replicas":           a.Config.Replicas,
		"created_at":         a.CreatedAt.UTC().Format(time.RFC3339),
		"database_name":      a.DatabaseName,
	}
}

// AddReadme writes the human-readable restore instructions. Content is
// deliberately short — the full restore doc lives in
// docs/runbooks/RESTORE_FROM_TENANT_EXPORT.md (separate follow-up PR).
func AddReadme(b *Builder, projectSlug, exportID string, createdAt time.Time) {
	body := fmt.Sprintf(`# Enclii Tenant Export — %s

Export ID: %s
Created:   %s UTC
Format:    enclii-tenant-export/v1

## Contents

- manifests/       Project, services, deployments, cron jobs, env-var metadata (values redacted).
- databases/       pg_dump (custom format) + schema.sql per addon.
- blobs/           R2 object inventories per bucket. Contents are NOT included — pull them yourself with your bucket credentials.
- secrets/         Secret NAMES only. Values are intentionally excluded. Rotate post-leave via Vault.
- audit/           Project-scoped audit timeline (NDJSON).
- MANIFEST.json    sha256 of every file; tarball-level sha256 is on your download receipt.

## Restore

A step-by-step restore guide is at
https://docs.enclii.dev/runbooks/restore-from-tenant-export

Short version:

1. kubectl apply -f manifests/ in your target namespace.
2. createdb; psql < databases/<addon>/schema.sql; pg_restore databases/<addon>/pg_dump.sql.gz.
3. Rotate secrets yourself — Enclii has no access to your post-leave cluster.
4. Pull blobs from R2 using your credentials and the manifests in blobs/.

## Integrity

Every file listed in MANIFEST.json carries an individual sha256.
The tarball's sha256 is shown on the download page and in the email.
Verify with: sha256sum enclii-export-*.tar.gz
`, projectSlug, exportID, createdAt.UTC().Format(time.RFC3339))

	b.AddEntry(Entry{Path: "README.md", Content: []byte(body)})
}
