package types

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CIRunnerMode controls whether CI runs on GitHub-hosted or self-hosted runners
type CIRunnerMode string

const (
	CIRunnerModeGitHub     CIRunnerMode = "github"
	CIRunnerModeSelfHosted CIRunnerMode = "self-hosted"
)

// Project represents a collection of services
type Project struct {
	ID           uuid.UUID    `json:"id" db:"id"`
	Name         string       `json:"name" db:"name"`
	Slug         string       `json:"slug" db:"slug"`
	CIRunnerMode CIRunnerMode `json:"ci_runner_mode" db:"ci_runner_mode"`
	CreatedAt    time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at" db:"updated_at"`
}

// Environment represents a deployment target (dev, staging, prod, preview-*)
type Environment struct {
	ID            uuid.UUID `json:"id" db:"id"`
	ProjectID     uuid.UUID `json:"project_id" db:"project_id"`
	Name          string    `json:"name" db:"name"`
	KubeNamespace string    `json:"kube_namespace" db:"kube_namespace"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// ServiceType defines the type of workload for a service
type ServiceType string

const (
	ServiceTypeWeb      ServiceType = "web"
	ServiceTypeWorker   ServiceType = "worker"
	ServiceTypeFunction ServiceType = "function"
)

// Service represents a deployable application
type Service struct {
	ID          uuid.UUID   `json:"id" db:"id"`
	ProjectID   uuid.UUID   `json:"project_id" db:"project_id"`
	Name        string      `json:"name" db:"name"`
	Type        ServiceType `json:"type" db:"type"`     // Workload type (web, worker, function)
	Region      string      `json:"region" db:"region"` // Deployment region (e.g., us-east)
	GitRepo     string      `json:"git_repo" db:"git_repo"`
	AppPath     string      `json:"app_path" db:"app_path"`       // Monorepo subdirectory path (e.g., "apps/api", "packages/web")
	WatchPaths  []string    `json:"watch_paths" db:"watch_paths"` // Paths that trigger rebuild (e.g., ["apps/api/", "packages/shared/"])
	BuildConfig BuildConfig `json:"build_config" db:"build_config"`
	Volumes     []Volume    `json:"volumes,omitempty" db:"volumes"`
	Jobs        []JobSpec   `json:"jobs,omitempty" db:"jobs"`
	// HealthCheck configuration for Kubernetes probes
	HealthCheck *HealthCheckConfig `json:"health_check,omitempty" db:"health_check"`
	// Resource configuration for container limits
	Resources *ResourceConfig `json:"resources,omitempty" db:"resources"`
	// Headers defines custom HTTP response headers injected via nginx ingress annotations.
	// Keys are header names, values are header values (e.g., {"Cross-Origin-Opener-Policy": "same-origin"}).
	Headers map[string]string `json:"headers,omitempty" db:"headers"`
	// AutoDeploy configuration for webhook-triggered deployments
	AutoDeploy       bool   `json:"auto_deploy" db:"auto_deploy"`               // Enable auto-deploy on successful build
	AutoDeployBranch string `json:"auto_deploy_branch" db:"auto_deploy_branch"` // Branch to auto-deploy (e.g., "main", "master")
	AutoDeployEnv    string `json:"auto_deploy_env" db:"auto_deploy_env"`       // Target environment (e.g., "development", "staging")
	// Health tracking fields (populated by Cartographer from K8s)
	K8sNamespace    *string      `json:"k8s_namespace,omitempty" db:"k8s_namespace"` // Actual K8s namespace (may differ from project slug)
	Health          HealthStatus `json:"health" db:"health"`                         // Service health: unknown, healthy, unhealthy
	Status          string       `json:"status" db:"status"`                         // Service status: unknown, pending, running, failed
	DesiredReplicas int          `json:"desired_replicas" db:"desired_replicas"`     // Desired replica count from K8s
	ReadyReplicas   int          `json:"ready_replicas" db:"ready_replicas"`         // Ready replica count from K8s
	// RolloutState describes whether the *newest* ReplicaSet has actually
	// landed. The legacy `Health` field reports `healthy` whenever ANY pod
	// is Ready — including the case where a new RS has been failing
	// readiness for days while the previous RS keeps the lights on.
	// RolloutState surfaces that lie. Values: "ok", "progressing", "blocked",
	// "" (unknown). Computed at request time from K8s; not persisted.
	RolloutState         string     `json:"rollout_state,omitempty" db:"-"`
	RolloutBlockedReason string     `json:"rollout_blocked_reason,omitempty" db:"-"`
	MinReplicas          *int       `json:"min_replicas,omitempty" db:"min_replicas"`
	MaxReplicas          *int       `json:"max_replicas,omitempty" db:"max_replicas"`
	LastHealthCheck      *time.Time `json:"last_health_check,omitempty" db:"last_health_check"`
	LastDeployment       *time.Time `json:"last_deployment,omitempty" db:"-"`
	LastCommitMsg        string     `json:"last_commit_message,omitempty" db:"-"`
	LastCommitBranch     string     `json:"last_commit_branch,omitempty" db:"-"`
	Framework            string     `json:"framework,omitempty" db:"-"` // Latest backend-detected framework slug for project cards.
	// Current release tracking (populated by ListByProject from the latest deployment).
	// Lets the dashboard show the running image digest + recent release history without
	// a per-service round trip. CurrentImageURI is the digest-pinned image actually running.
	CurrentImageURI         string           `json:"current_image_uri,omitempty" db:"-"`
	CurrentReleaseID        *uuid.UUID       `json:"current_release_id,omitempty" db:"-"`
	CurrentReleaseCreatedAt *time.Time       `json:"current_release_created_at,omitempty" db:"-"`
	RecentReleases          []ReleaseSummary `json:"recent_releases,omitempty" db:"-"`
	CreatedAt               time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time        `json:"updated_at" db:"updated_at"`
}

// ReleaseSummary is a compact projection of recent releases joined into Service responses
// so the dashboard can show the last few deploys without a separate query per service.
type ReleaseSummary struct {
	ID        uuid.UUID `json:"id"`
	Version   string    `json:"version"`
	ImageURI  string    `json:"image_uri"`
	GitSHA    string    `json:"git_sha"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// HealthCheckConfig defines how Kubernetes probes should check service health
type HealthCheckConfig struct {
	// Path for HTTP health check endpoint (default: "/health")
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	// Port to check (default: container port from ENCLII_PORT or 8080)
	Port int `json:"port,omitempty" yaml:"port,omitempty"`
	// LivenessPath overrides Path for liveness probe only
	LivenessPath string `json:"liveness_path,omitempty" yaml:"livenessPath,omitempty"`
	// ReadinessPath overrides Path for readiness probe only
	ReadinessPath string `json:"readiness_path,omitempty" yaml:"readinessPath,omitempty"`
	// InitialDelaySeconds before starting probes (default: 10 for readiness, 30 for liveness)
	InitialDelaySeconds int `json:"initial_delay_seconds,omitempty" yaml:"initialDelaySeconds,omitempty"`
	// PeriodSeconds between probe checks (default: 10)
	PeriodSeconds int `json:"period_seconds,omitempty" yaml:"periodSeconds,omitempty"`
	// TimeoutSeconds for each probe (default: 5)
	TimeoutSeconds int `json:"timeout_seconds,omitempty" yaml:"timeoutSeconds,omitempty"`
	// FailureThreshold before marking unhealthy (default: 3)
	FailureThreshold int `json:"failure_threshold,omitempty" yaml:"failureThreshold,omitempty"`
	// HTTPHeaders are sent on probe requests (e.g. Host for Django ALLOWED_HOSTS).
	HTTPHeaders map[string]string `json:"http_headers,omitempty" yaml:"httpHeaders,omitempty"`
	// Disabled skips health checks entirely (use with caution)
	Disabled bool `json:"disabled,omitempty" yaml:"disabled,omitempty"`
}

// ResourceConfig defines container resource requests and limits
type ResourceConfig struct {
	// CPURequest is the minimum CPU (e.g., "100m", "0.5")
	CPURequest string `json:"cpu_request,omitempty" yaml:"cpuRequest,omitempty"`
	// CPULimit is the maximum CPU (e.g., "500m", "2")
	CPULimit string `json:"cpu_limit,omitempty" yaml:"cpuLimit,omitempty"`
	// MemoryRequest is the minimum memory (e.g., "128Mi", "1Gi")
	MemoryRequest string `json:"memory_request,omitempty" yaml:"memoryRequest,omitempty"`
	// MemoryLimit is the maximum memory (e.g., "512Mi", "2Gi")
	MemoryLimit string `json:"memory_limit,omitempty" yaml:"memoryLimit,omitempty"`
}

// BuildConfig defines how to build a service
type BuildConfig struct {
	Type       BuildType         `json:"type"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Buildpack  string            `json:"buildpack,omitempty"`
	Context    string            `json:"context,omitempty"`
	BuildArgs  map[string]string `json:"build_args,omitempty"`
	Target     string            `json:"target,omitempty"`
	// BuildOnly marks a service row as a build/webhook target with no Enclii-managed
	// runtime workload. These services can still build releases, but they must not
	// be auto-deployed or counted in runtime health/project-card rollups.
	BuildOnly bool `json:"build_only,omitempty" yaml:"buildOnly,omitempty"`
}

type BuildType string

const (
	BuildTypeAuto       BuildType = "auto"
	BuildTypeDockerfile BuildType = "dockerfile"
	BuildTypeBuildpack  BuildType = "buildpack"
)

// Release represents a built and immutable version of a service
type Release struct {
	ID                  uuid.UUID     `json:"id" db:"id"`
	ServiceID           uuid.UUID     `json:"service_id" db:"service_id"`
	Version             string        `json:"version" db:"version"`
	ImageURI            string        `json:"image_uri" db:"image_uri"`
	GitSHA              string        `json:"git_sha" db:"git_sha"`
	GitBranch           string        `json:"git_branch,omitempty" db:"git_branch"`
	CommitMessage       string        `json:"commit_message,omitempty" db:"commit_message"`
	CommitAuthorName    string        `json:"commit_author_name,omitempty" db:"commit_author_name"`
	CommitAuthorEmail   string        `json:"commit_author_email,omitempty" db:"commit_author_email"`
	PRNumber            *int          `json:"pr_number,omitempty" db:"pr_number"`
	PRTitle             string        `json:"pr_title,omitempty" db:"pr_title"`
	PRURL               string        `json:"pr_url,omitempty" db:"pr_url"`
	RepoURL             string        `json:"repo_url,omitempty" db:"repo_url"`
	Status              ReleaseStatus `json:"status" db:"status"`
	ErrorMessage        *string       `json:"error_message,omitempty" db:"error_message"`     // Error from build failure
	SBOM                string        `json:"sbom,omitempty" db:"sbom"`                       // Software Bill of Materials (JSON)
	SBOMFormat          string        `json:"sbom_format,omitempty" db:"sbom_format"`         // e.g., "cyclonedx-json", "spdx-json"
	ImageSignature      string        `json:"image_signature,omitempty" db:"image_signature"` // Cosign signature
	SignatureVerifiedAt *time.Time    `json:"signature_verified_at,omitempty" db:"signature_verified_at"`
	// FrameworkSlug is the canonical framework identifier detected by
	// roundhouse at build time (matches packages/sdk-go/pkg/frameworks
	// catalog). Legacy rows with NULL are served as empty string.
	FrameworkSlug string    `json:"framework_slug,omitempty" db:"framework_slug"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// DeploymentEnriched represents a deployment with joined release and service data
type DeploymentEnriched struct {
	Deployment
	ServiceID         uuid.UUID `json:"service_id"`
	ServiceName       string    `json:"service_name"`
	GitSHA            string    `json:"git_sha"`
	GitBranch         string    `json:"git_branch"`
	CommitMessage     string    `json:"commit_message"`
	CommitAuthor      string    `json:"commit_author"`
	CommitAuthorEmail string    `json:"commit_author_email"`
	PRNumber          *int      `json:"pr_number,omitempty"`
	PRTitle           string    `json:"pr_title"`
	PRURL             string    `json:"pr_url"`
	RepoURL           string    `json:"repo_url"`
}

type ReleaseStatus string

const (
	ReleaseStatusBuilding ReleaseStatus = "building"
	ReleaseStatusReady    ReleaseStatus = "ready"
	ReleaseStatusFailed   ReleaseStatus = "failed"
)

// Deployment represents a running instance of a release in an environment
type Deployment struct {
	ID            uuid.UUID        `json:"id" db:"id"`
	ReleaseID     uuid.UUID        `json:"release_id" db:"release_id"`
	EnvironmentID uuid.UUID        `json:"environment_id" db:"environment_id"`
	GroupID       *uuid.UUID       `json:"group_id,omitempty" db:"group_id"` // Deployment group for coordinated multi-service deploys
	DeployOrder   int              `json:"deploy_order" db:"deploy_order"`   // Order within deployment group (0 = no group or first)
	Replicas      int              `json:"replicas" db:"replicas"`
	Status        DeploymentStatus `json:"status" db:"status"`
	Health        HealthStatus     `json:"health" db:"health"`
	ErrorMessage  *string          `json:"error_message,omitempty" db:"error_message"` // Error from reconciliation failure
	// ServiceID is denormalized from releases.service_id to enforce the
	// (service_id, version_number) UNIQUE constraint and speed the
	// allocation query. Immutable after insert. Nullable for historical
	// rows until the 010 backfill lands in every env.
	ServiceID *uuid.UUID `json:"service_id,omitempty" db:"service_id"`
	// VersionNumber is the Heroku-style semantic version for this deployment
	// (v1, v2, …). Allocated monotonically per service at deploy-start;
	// never reused even across rollbacks. See P2.6.
	VersionNumber *int      `json:"version_number,omitempty" db:"version_number"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// VersionLabel returns the human-readable Heroku-style label ("v42") if the
// deployment has been allocated a version, or the empty string otherwise.
// Centralized so UI/CLI/API formatting stays consistent.
func (d *Deployment) VersionLabel() string {
	if d == nil || d.VersionNumber == nil {
		return ""
	}
	return fmt.Sprintf("v%d", *d.VersionNumber)
}

// ParseVersionLabel parses a Heroku-style label ("v42") into its integer
// component. Returns ok=false if the input is not a valid v-label (missing
// prefix, non-integer, or <= 0). Leading/trailing whitespace is tolerated.
func ParseVersionLabel(s string) (int, bool) {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 {
		return 0, false
	}
	if trimmed[0] != 'v' && trimmed[0] != 'V' {
		return 0, false
	}
	n, err := strconv.Atoi(trimmed[1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

type DeploymentStatus string

const (
	DeploymentStatusPending   DeploymentStatus = "pending"
	DeploymentStatusDeploying DeploymentStatus = "deploying"
	DeploymentStatusRunning   DeploymentStatus = "running"
	DeploymentStatusFailed    DeploymentStatus = "failed"
	DeploymentStatusCancelled DeploymentStatus = "cancelled"
)

type HealthStatus string

const (
	HealthStatusUnknown   HealthStatus = "unknown"
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// ServiceSpec represents the desired configuration for a service
type ServiceSpec struct {
	APIVersion string            `yaml:"apiVersion" json:"api_version"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   ServiceMetadata   `yaml:"metadata" json:"metadata"`
	Spec       ServiceSpecConfig `yaml:"spec" json:"spec"`
}

type ServiceMetadata struct {
	Name    string `yaml:"name" json:"name"`
	Project string `yaml:"project" json:"project"`
}

type ServiceSpecConfig struct {
	Build   BuildSpec   `yaml:"build" json:"build"`
	Runtime RuntimeSpec `yaml:"runtime" json:"runtime"`
	Env     []EnvVar    `yaml:"env,omitempty" json:"env,omitempty"`
	Volumes []Volume    `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Jobs    []JobSpec   `yaml:"jobs,omitempty" json:"jobs,omitempty"`
}

type JobSpec struct {
	Name     string   `yaml:"name" json:"name"`
	Schedule string   `yaml:"schedule" json:"schedule"`
	Timezone string   `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	Command  []string `yaml:"command" json:"command"`
	Timeout  int      `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retries  int      `yaml:"retries,omitempty" json:"retries,omitempty"`
}

type BuildSpec struct {
	Type       string `yaml:"type" json:"type"`
	Dockerfile string `yaml:"dockerfile,omitempty" json:"dockerfile,omitempty"`
}

type RuntimeSpec struct {
	Port        int    `yaml:"port" json:"port"`
	Replicas    int    `yaml:"replicas" json:"replicas"`
	MinReplicas int    `yaml:"minReplicas,omitempty" json:"min_replicas,omitempty"`
	MaxReplicas int    `yaml:"maxReplicas,omitempty" json:"max_replicas,omitempty"`
	HealthCheck string `yaml:"healthCheck" json:"health_check"`
}

type EnvVar struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

// Volume represents a persistent volume configuration for a service
type Volume struct {
	Name             string `yaml:"name" json:"name"`
	MountPath        string `yaml:"mountPath" json:"mount_path"`
	Size             string `yaml:"size" json:"size"`                                               // e.g., "10Gi", "100Mi"
	StorageClassName string `yaml:"storageClassName,omitempty" json:"storage_class_name,omitempty"` // defaults to "standard"
	AccessMode       string `yaml:"accessMode,omitempty" json:"access_mode,omitempty"`              // defaults to "ReadWriteOnce"
}

// Role represents a user's role in the system
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
	RoleSystem    Role = "system" // For automated system actions (webhooks, auto-deploy)
)

// User represents a user account in the system
type User struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	Email        string     `json:"email" db:"email"`
	PasswordHash string     `json:"-" db:"password_hash"` // Never expose password hash in JSON
	Name         string     `json:"name" db:"name"`
	Role         string     `json:"role" db:"role"`                           // admin, developer, or viewer
	OIDCSubject  *string    `json:"oidc_subject,omitempty" db:"oidc_subject"` // OIDC subject identifier (sub claim)
	OIDCIssuer   *string    `json:"oidc_issuer,omitempty" db:"oidc_issuer"`   // OIDC issuer URL (iss claim)
	FoundryTier  *string    `json:"foundry_tier,omitempty" db:"foundry_tier"`
	Active       bool       `json:"active" db:"active"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
}

// Team represents a group of users
type Team struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Slug      string    `json:"slug" db:"slug"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ProjectAccess represents a user's access to a project with environment-specific permissions
type ProjectAccess struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	UserID        uuid.UUID  `json:"user_id" db:"user_id"`
	ProjectID     uuid.UUID  `json:"project_id" db:"project_id"`
	EnvironmentID *uuid.UUID `json:"environment_id,omitempty" db:"environment_id"` // nil = all environments
	Role          Role       `json:"role" db:"role"`
	GrantedBy     uuid.UUID  `json:"granted_by" db:"granted_by"`
	GrantedAt     time.Time  `json:"granted_at" db:"granted_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty" db:"expires_at"` // for temporary access
}

// AuditLog represents an immutable audit trail entry
type AuditLog struct {
	ID            uuid.UUID              `json:"id" db:"id"`
	Timestamp     time.Time              `json:"timestamp" db:"timestamp"`
	ActorID       *uuid.UUID             `json:"actor_id,omitempty" db:"actor_id"` // nil for OIDC users without local user row
	ActorEmail    string                 `json:"actor_email" db:"actor_email"`
	ActorRole     Role                   `json:"actor_role" db:"actor_role"`
	Action        string                 `json:"action" db:"action"`               // 'deploy', 'scale', 'delete', 'access_logs'
	ResourceType  string                 `json:"resource_type" db:"resource_type"` // 'service', 'environment', 'secret'
	ResourceID    string                 `json:"resource_id" db:"resource_id"`
	ResourceName  string                 `json:"resource_name" db:"resource_name"`
	ProjectID     *uuid.UUID             `json:"project_id,omitempty" db:"project_id"`
	EnvironmentID *uuid.UUID             `json:"environment_id,omitempty" db:"environment_id"`
	IPAddress     string                 `json:"ip_address" db:"ip_address"`
	UserAgent     string                 `json:"user_agent" db:"user_agent"`
	Outcome       string                 `json:"outcome" db:"outcome"` // 'success', 'failure', 'denied'
	Context       map[string]interface{} `json:"context" db:"context"` // {pr_url, commit_sha, approver, change_ticket}
	Metadata      map[string]interface{} `json:"metadata,omitempty" db:"metadata"`
}

// ApprovalRecord represents deployment provenance and approval evidence
type ApprovalRecord struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	DeploymentID      uuid.UUID  `json:"deployment_id" db:"deployment_id"`
	PRURL             string     `json:"pr_url" db:"pr_url"`
	PRNumber          int        `json:"pr_number" db:"pr_number"`
	ApproverEmail     string     `json:"approver_email" db:"approver_email"`
	ApproverName      string     `json:"approver_name" db:"approver_name"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty" db:"approved_at"`
	CIStatus          string     `json:"ci_status" db:"ci_status"` // 'passed', 'failed', 'pending'
	ChangeTicketURL   string     `json:"change_ticket_url,omitempty" db:"change_ticket_url"`
	ComplianceReceipt string     `json:"compliance_receipt" db:"compliance_receipt"` // JSON receipt for auditors
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
}

// CustomDomain represents a custom domain mapping for a service
type CustomDomain struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	ServiceID          uuid.UUID  `json:"service_id" db:"service_id"`
	EnvironmentID      uuid.UUID  `json:"environment_id" db:"environment_id"`
	Domain             string     `json:"domain" db:"domain"` // e.g., "api.example.com"
	Verified           bool       `json:"verified" db:"verified"`
	TLSEnabled         bool       `json:"tls_enabled" db:"tls_enabled"`
	TLSIssuer          string     `json:"tls_issuer,omitempty" db:"tls_issuer"` // "letsencrypt-prod", "letsencrypt-staging", "selfsigned-issuer"
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
	VerifiedAt         *time.Time `json:"verified_at,omitempty" db:"verified_at"`
	CloudflareTunnelID *uuid.UUID `json:"cloudflare_tunnel_id,omitempty" db:"cloudflare_tunnel_id"`
	IsPlatformDomain   bool       `json:"is_platform_domain" db:"is_platform_domain"`
	ZeroTrustEnabled   bool       `json:"zero_trust_enabled" db:"zero_trust_enabled"`
	AccessPolicyID     string     `json:"access_policy_id,omitempty" db:"access_policy_id"`
	TLSProvider        string     `json:"tls_provider" db:"tls_provider"` // "cert-manager", "cloudflare-for-saas"
	Status             string     `json:"status" db:"status"`             // "pending", "verifying", "active", "error"
	DNSCNAME           string     `json:"dns_cname,omitempty" db:"dns_cname"`

	// Cloudflare for SaaS state. Populated only for domains provisioned via
	// the custom-hostname path (client-owned domains that do not delegate
	// their nameservers to us).
	//
	// CustomHostnameStatus / CustomHostnameSSLStatus mirror what Cloudflare
	// reported on the last read — they are never inferred from a successful
	// API call. PendingDNSRecords is what the domain owner still has to
	// create; while it is non-empty the domain is waiting on the client, not
	// on us. ProvisioningError carries the last provisioning failure so a
	// deploy-path failure stays visible instead of being logged and lost.
	CustomHostnameID        string             `json:"custom_hostname_id,omitempty" db:"custom_hostname_id"`
	CustomHostnameStatus    string             `json:"custom_hostname_status,omitempty" db:"custom_hostname_status"`
	CustomHostnameSSLStatus string             `json:"custom_hostname_ssl_status,omitempty" db:"custom_hostname_ssl_status"`
	PendingDNSRecords       []PendingDNSRecord `json:"pending_dns_records,omitempty" db:"pending_dns_records"`
	ProvisioningError       string             `json:"provisioning_error,omitempty" db:"provisioning_error"`
	ProvisioningCheckedAt   *time.Time         `json:"provisioning_checked_at,omitempty" db:"provisioning_checked_at"`
}

// PendingDNSRecord is a DNS record the domain owner must create on their own
// nameservers before a Cloudflare for SaaS custom hostname can serve traffic.
// We cannot create these ourselves — we do not control the zone.
type PendingDNSRecord struct {
	Purpose string `json:"purpose"` // "routing", "ownership", "ssl_validation"
	Type    string `json:"type"`    // "CNAME", "TXT"
	Name    string `json:"name"`
	Value   string `json:"value"`
}

// Route represents an HTTP route configuration for a service
type Route struct {
	ID            uuid.UUID `json:"id" db:"id"`
	ServiceID     uuid.UUID `json:"service_id" db:"service_id"`
	EnvironmentID uuid.UUID `json:"environment_id" db:"environment_id"`
	Path          string    `json:"path" db:"path"`           // e.g., "/api/v1"
	PathType      string    `json:"path_type" db:"path_type"` // "Prefix", "Exact", "ImplementationSpecific"
	Port          int       `json:"port" db:"port"`           // Target port
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// CloudflareAccount represents a platform-level Cloudflare account configuration
type CloudflareAccount struct {
	ID                uuid.UUID `json:"id" db:"id"`
	Name              string    `json:"name" db:"name"`
	AccountID         string    `json:"account_id" db:"account_id"`
	APITokenEncrypted string    `json:"-" db:"api_token_encrypted"` // Never expose in JSON
	ZoneID            string    `json:"zone_id,omitempty" db:"zone_id"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

// CloudflareTunnel represents an environment-scoped Cloudflare tunnel
type CloudflareTunnel struct {
	ID                   uuid.UUID  `json:"id" db:"id"`
	CloudflareAccountID  uuid.UUID  `json:"cloudflare_account_id" db:"cloudflare_account_id"`
	EnvironmentID        uuid.UUID  `json:"environment_id" db:"environment_id"`
	TunnelID             string     `json:"tunnel_id" db:"tunnel_id"`
	TunnelName           string     `json:"tunnel_name" db:"tunnel_name"`
	TunnelTokenEncrypted string     `json:"-" db:"tunnel_token_encrypted"` // Never expose in JSON
	CNAME                string     `json:"cname" db:"cname"`              // e.g., "abc123.cfargotunnel.com"
	Status               string     `json:"status" db:"status"`            // "active", "degraded", "offline"
	LastHealthCheck      *time.Time `json:"last_health_check,omitempty" db:"last_health_check"`
	CreatedAt            time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at" db:"updated_at"`
}

// TunnelStatus constants
const (
	TunnelStatusActive   = "active"
	TunnelStatusDegraded = "degraded"
	TunnelStatusOffline  = "offline"
)

// DomainStatus constants
const (
	DomainStatusPending   = "pending"
	DomainStatusVerifying = "verifying"
	DomainStatusActive    = "active"
	DomainStatusError     = "error"
)

// TLSProvider constants
const (
	TLSProviderCertManager       = "cert-manager"
	TLSProviderCloudflareForSaaS = "cloudflare-for-saas"
)

// ServiceNetworking represents the combined networking info for a service
type ServiceNetworking struct {
	ServiceID      uuid.UUID         `json:"service_id"`
	ServiceName    string            `json:"service_name"`
	Domains        []DomainInfo      `json:"domains"`
	InternalRoutes []InternalRoute   `json:"internal_routes"`
	TunnelStatus   *TunnelStatusInfo `json:"tunnel_status,omitempty"`
}

// DomainInfo represents detailed domain information for the UI
type DomainInfo struct {
	ID               uuid.UUID  `json:"id"`
	Domain           string     `json:"domain"`
	Environment      string     `json:"environment"`
	EnvironmentID    uuid.UUID  `json:"environment_id"`
	IsPlatformDomain bool       `json:"is_platform_domain"`
	Status           string     `json:"status"`
	TLSStatus        string     `json:"tls_status"` // "pending", "provisioning", "active"
	TLSProvider      string     `json:"tls_provider"`
	ZeroTrustEnabled bool       `json:"zero_trust_enabled"`
	DNSVerifiedAt    *time.Time `json:"dns_verified_at,omitempty"`
	VerificationTXT  string     `json:"verification_txt,omitempty"`
	DNSCNAME         string     `json:"dns_cname,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`

	// Provisioning diagnosis. A domain can be declared, stored and shown as
	// "pending" while nothing will ever provision it — a client-owned domain
	// declared before the operator configured the fallback origin, for
	// instance. Without these two fields that failure lives only in a log
	// line, and the read path shows a domain that simply never comes up.
	ProvisioningError     string             `json:"provisioning_error,omitempty"`
	ProvisioningCheckedAt *time.Time         `json:"provisioning_checked_at,omitempty"`
	PendingDNSRecords     []PendingDNSRecord `json:"pending_dns_records,omitempty"`
}

// TunnelStatusInfo represents tunnel health information
type TunnelStatusInfo struct {
	TunnelID    string    `json:"tunnel_id"`
	TunnelName  string    `json:"tunnel_name"`
	Status      string    `json:"status"`
	CNAME       string    `json:"cname"`
	Connectors  int       `json:"connectors"`
	LastHealthy time.Time `json:"last_healthy"`
}

// InternalRoute represents internal cluster routing info
type InternalRoute struct {
	Path          string `json:"path"`
	TargetService string `json:"target_service"`
	TargetPort    int    `json:"target_port"`
}

// EnvironmentVariable represents an environment variable for a service
// Values are encrypted at rest using AES-256-GCM
type EnvironmentVariable struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	ServiceID      uuid.UUID  `json:"service_id" db:"service_id"`
	EnvironmentID  *uuid.UUID `json:"environment_id,omitempty" db:"environment_id"` // NULL = all environments
	Key            string     `json:"key" db:"key"`
	Value          string     `json:"value" db:"-"`             // Decrypted value (not stored directly)
	ValueEncrypted string     `json:"-" db:"value_encrypted"`   // Encrypted value (stored in DB)
	IsSecret       bool       `json:"is_secret" db:"is_secret"` // If true, value is masked in API responses
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty" db:"created_by"`
	CreatedByEmail string     `json:"created_by_email,omitempty" db:"created_by_email"`
}

// EnvironmentVariableResponse is the API response for env vars (masks secrets)
type EnvironmentVariableResponse struct {
	ID            uuid.UUID  `json:"id"`
	ServiceID     uuid.UUID  `json:"service_id"`
	EnvironmentID *uuid.UUID `json:"environment_id,omitempty"`
	Key           string     `json:"key"`
	Value         string     `json:"value"` // Masked as "••••••" if is_secret=true
	IsSecret      bool       `json:"is_secret"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// EnvVarAuditLog represents an audit entry for env var changes
type EnvVarAuditLog struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	EnvVarID      uuid.UUID  `json:"env_var_id" db:"env_var_id"`
	ServiceID     uuid.UUID  `json:"service_id" db:"service_id"`
	EnvironmentID *uuid.UUID `json:"environment_id,omitempty" db:"environment_id"`
	Action        string     `json:"action" db:"action"` // created, updated, deleted, revealed
	Key           string     `json:"key" db:"key"`
	OldValueHash  string     `json:"old_value_hash,omitempty" db:"old_value_hash"`
	NewValueHash  string     `json:"new_value_hash,omitempty" db:"new_value_hash"`
	ActorID       *uuid.UUID `json:"actor_id,omitempty" db:"actor_id"`
	ActorEmail    string     `json:"actor_email" db:"actor_email"`
	ActorIP       string     `json:"actor_ip,omitempty" db:"actor_ip"`
	UserAgent     string     `json:"user_agent,omitempty" db:"user_agent"`
	Timestamp     time.Time  `json:"timestamp" db:"timestamp"`
}

// PreviewEnvironmentStatus represents the status of a preview environment
type PreviewEnvironmentStatus string

const (
	PreviewStatusPending   PreviewEnvironmentStatus = "pending"
	PreviewStatusBuilding  PreviewEnvironmentStatus = "building"
	PreviewStatusDeploying PreviewEnvironmentStatus = "deploying"
	PreviewStatusActive    PreviewEnvironmentStatus = "active"
	PreviewStatusSleeping  PreviewEnvironmentStatus = "sleeping"
	PreviewStatusFailed    PreviewEnvironmentStatus = "failed"
	PreviewStatusClosed    PreviewEnvironmentStatus = "closed"
)

// PreviewEnvironment represents an ephemeral environment for a pull request
// This is the killer feature for platform parity
type PreviewEnvironment struct {
	ID        uuid.UUID `json:"id" db:"id"`
	ProjectID uuid.UUID `json:"project_id" db:"project_id"`
	ServiceID uuid.UUID `json:"service_id" db:"service_id"`

	// PR Information
	PRNumber     int    `json:"pr_number" db:"pr_number"`
	PRTitle      string `json:"pr_title,omitempty" db:"pr_title"`
	PRURL        string `json:"pr_url,omitempty" db:"pr_url"`
	PRAuthor     string `json:"pr_author,omitempty" db:"pr_author"`
	PRBranch     string `json:"pr_branch" db:"pr_branch"`
	PRBaseBranch string `json:"pr_base_branch" db:"pr_base_branch"`
	CommitSHA    string `json:"commit_sha" db:"commit_sha"`

	// Preview URL (e.g., pr-123.preview.enclii.app)
	PreviewSubdomain string `json:"preview_subdomain" db:"preview_subdomain"`
	PreviewURL       string `json:"preview_url" db:"preview_url"`

	// Status
	Status        PreviewEnvironmentStatus `json:"status" db:"status"`
	StatusMessage string                   `json:"status_message,omitempty" db:"status_message"`

	// Auto-sleep configuration
	AutoSleepAfter int        `json:"auto_sleep_after" db:"auto_sleep_after"` // Minutes, 0 = never
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty" db:"last_accessed_at"`
	SleepingSince  *time.Time `json:"sleeping_since,omitempty" db:"sleeping_since"`

	// Resource tracking
	DeploymentID *uuid.UUID `json:"deployment_id,omitempty" db:"deployment_id"`
	BuildLogsURL string     `json:"build_logs_url,omitempty" db:"build_logs_url"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty" db:"closed_at"`
}

// PreviewCommentStatus represents the status of a preview comment
type PreviewCommentStatus string

const (
	CommentStatusActive   PreviewCommentStatus = "active"
	CommentStatusResolved PreviewCommentStatus = "resolved"
	CommentStatusDeleted  PreviewCommentStatus = "deleted"
)

// PreviewComment represents a comment on a preview deployment (like Vercel comments)
type PreviewComment struct {
	ID        uuid.UUID `json:"id" db:"id"`
	PreviewID uuid.UUID `json:"preview_id" db:"preview_id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	UserEmail string    `json:"user_email" db:"user_email"`
	UserName  string    `json:"user_name,omitempty" db:"user_name"`

	// Comment content
	Content string `json:"content" db:"content"`

	// Optional: attach to specific URL path or coordinate
	Path      string `json:"path,omitempty" db:"path"`
	XPosition *int   `json:"x_position,omitempty" db:"x_position"`
	YPosition *int   `json:"y_position,omitempty" db:"y_position"`

	// Status
	Status     PreviewCommentStatus `json:"status" db:"status"`
	ResolvedAt *time.Time           `json:"resolved_at,omitempty" db:"resolved_at"`
	ResolvedBy *uuid.UUID           `json:"resolved_by,omitempty" db:"resolved_by"`

	// Timestamps
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// PreviewAccessLog represents an access log entry for a preview environment
type PreviewAccessLog struct {
	ID         uuid.UUID `json:"id" db:"id"`
	PreviewID  uuid.UUID `json:"preview_id" db:"preview_id"`
	AccessedAt time.Time `json:"accessed_at" db:"accessed_at"`

	// Request metadata
	Path      string `json:"path,omitempty" db:"path"`
	UserAgent string `json:"user_agent,omitempty" db:"user_agent"`
	IPAddress string `json:"ip_address,omitempty" db:"ip_address"`

	// Optional: authenticated user
	UserID *uuid.UUID `json:"user_id,omitempty" db:"user_id"`

	// Response metadata
	StatusCode     *int `json:"status_code,omitempty" db:"status_code"`
	ResponseTimeMs *int `json:"response_time_ms,omitempty" db:"response_time_ms"`
}

// ============================================================================
// API TOKEN TYPES
// ============================================================================

// APIToken represents a programmatic access token for CLI/CI/CD use
type APIToken struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	UserID     uuid.UUID  `json:"user_id" db:"user_id"`
	Name       string     `json:"name" db:"name"`
	Prefix     string     `json:"prefix" db:"prefix"`           // First 8 chars for display
	TokenHash  string     `json:"-" db:"token_hash"`            // SHA-256 hash (never exposed)
	Scopes     []string   `json:"scopes,omitempty" db:"scopes"` // Permission scopes
	ExpiresAt  *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
	LastUsedIP string     `json:"last_used_ip,omitempty" db:"last_used_ip"`
	Revoked    bool       `json:"revoked" db:"revoked"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" db:"updated_at"`
}

// APITokenCreateResponse is returned when creating a new token
// This is the ONLY time the raw token is exposed
type APITokenCreateResponse struct {
	Token    string    `json:"token"`     // Full token (only shown once!)
	ID       uuid.UUID `json:"id"`        // Token ID for management
	Name     string    `json:"name"`      // User-provided name
	Prefix   string    `json:"prefix"`    // Display prefix
	ExpireAt *string   `json:"expire_at"` // ISO8601 expiration (if set)
}
