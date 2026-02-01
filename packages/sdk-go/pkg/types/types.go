package types

import (
	"time"

	"github.com/google/uuid"
)

// Project represents a collection of services
type Project struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Slug      string    `json:"slug" db:"slug"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
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

// Service represents a deployable application
type Service struct {
	ID          uuid.UUID   `json:"id" db:"id"`
	ProjectID   uuid.UUID   `json:"project_id" db:"project_id"`
	Name        string      `json:"name" db:"name"`
	GitRepo     string      `json:"git_repo" db:"git_repo"`
	AppPath     string      `json:"app_path" db:"app_path"`       // Monorepo subdirectory path (e.g., "apps/api", "packages/web")
	WatchPaths  []string    `json:"watch_paths" db:"watch_paths"` // Paths that trigger rebuild (e.g., ["apps/api/", "packages/shared/"])
	BuildConfig BuildConfig `json:"build_config" db:"build_config"`
	Volumes     []Volume    `json:"volumes,omitempty" db:"volumes"`
	// HealthCheck configuration for Kubernetes probes
	HealthCheck *HealthCheckConfig `json:"health_check,omitempty" db:"health_check"`
	// Resource configuration for container limits
	Resources *ResourceConfig `json:"resources,omitempty" db:"resources"`
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
	LastHealthCheck *time.Time   `json:"last_health_check,omitempty" db:"last_health_check"`
	CreatedAt       time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at" db:"updated_at"`
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
	CreatedAt           time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at" db:"updated_at"`
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
	CreatedAt     time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at" db:"updated_at"`
}

type DeploymentStatus string

const (
	DeploymentStatusPending DeploymentStatus = "pending"
	DeploymentStatusRunning DeploymentStatus = "running"
	DeploymentStatusFailed  DeploymentStatus = "failed"
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
}

type BuildSpec struct {
	Type       string `yaml:"type" json:"type"`
	Dockerfile string `yaml:"dockerfile,omitempty" json:"dockerfile,omitempty"`
}

type RuntimeSpec struct {
	Port        int    `yaml:"port" json:"port"`
	Replicas    int    `yaml:"replicas" json:"replicas"`
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
// This is the killer feature for Vercel/Railway parity
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

// ============================================================================
// DATABASE ADDON TYPES
// One-click database provisioning for PostgreSQL, Redis, MySQL
// Matches Railway's core value proposition
// ============================================================================

// DatabaseAddonType represents the type of database addon
type DatabaseAddonType string

const (
	DatabaseAddonTypePostgres DatabaseAddonType = "postgres"
	DatabaseAddonTypeRedis    DatabaseAddonType = "redis"
	DatabaseAddonTypeMySQL    DatabaseAddonType = "mysql"
)

// DatabaseAddonStatus represents the provisioning status of a database addon
type DatabaseAddonStatus string

const (
	DatabaseAddonStatusPending      DatabaseAddonStatus = "pending"
	DatabaseAddonStatusProvisioning DatabaseAddonStatus = "provisioning"
	DatabaseAddonStatusReady        DatabaseAddonStatus = "ready"
	DatabaseAddonStatusFailed       DatabaseAddonStatus = "failed"
	DatabaseAddonStatusDeleting     DatabaseAddonStatus = "deleting"
	DatabaseAddonStatusDeleted      DatabaseAddonStatus = "deleted"
)

// DatabaseAddonConfig represents the configuration for a database addon
type DatabaseAddonConfig struct {
	Version   string `json:"version,omitempty"`    // e.g., "16" for PostgreSQL 16
	StorageGB int    `json:"storage_gb,omitempty"` // Storage size in GB
	CPU       string `json:"cpu,omitempty"`        // CPU request/limit (e.g., "100m", "500m")
	Memory    string `json:"memory,omitempty"`     // Memory request/limit (e.g., "256Mi", "1Gi")
	HAEnabled bool   `json:"ha_enabled,omitempty"` // High availability mode
	Replicas  int    `json:"replicas,omitempty"`   // Number of replicas (for HA)
}

// DatabaseAddon represents a provisioned database instance
type DatabaseAddon struct {
	ID            uuid.UUID           `json:"id" db:"id"`
	ProjectID     uuid.UUID           `json:"project_id" db:"project_id"`
	EnvironmentID *uuid.UUID          `json:"environment_id,omitempty" db:"environment_id"`
	Type          DatabaseAddonType   `json:"type" db:"type"`
	Name          string              `json:"name" db:"name"`
	Status        DatabaseAddonStatus `json:"status" db:"status"`
	StatusMessage string              `json:"status_message,omitempty" db:"status_message"`
	Config        DatabaseAddonConfig `json:"config" db:"config"`

	// Kubernetes resources
	K8sNamespace     string `json:"k8s_namespace,omitempty" db:"k8s_namespace"`
	K8sResourceName  string `json:"k8s_resource_name,omitempty" db:"k8s_resource_name"`
	ConnectionSecret string `json:"connection_secret,omitempty" db:"connection_secret"`

	// Connection info (populated after provisioning)
	Host         string `json:"host,omitempty" db:"host"`
	Port         int    `json:"port,omitempty" db:"port"`
	DatabaseName string `json:"database_name,omitempty" db:"database_name"`
	Username     string `json:"username,omitempty" db:"username"`

	// Resource tracking
	StorageUsedBytes  int64      `json:"storage_used_bytes" db:"storage_used_bytes"`
	ConnectionsActive int        `json:"connections_active" db:"connections_active"`
	LastBackupAt      *time.Time `json:"last_backup_at,omitempty" db:"last_backup_at"`

	// Audit fields
	CreatedBy      *uuid.UUID `json:"created_by,omitempty" db:"created_by"`
	CreatedByEmail string     `json:"created_by_email,omitempty" db:"created_by_email"`

	// Timestamps
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
	ProvisionedAt *time.Time `json:"provisioned_at,omitempty" db:"provisioned_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
}

// DatabaseAddonBindingStatus represents the status of a service binding
type DatabaseAddonBindingStatus string

const (
	DatabaseAddonBindingStatusActive    DatabaseAddonBindingStatus = "active"
	DatabaseAddonBindingStatusSuspended DatabaseAddonBindingStatus = "suspended"
	DatabaseAddonBindingStatusDeleted   DatabaseAddonBindingStatus = "deleted"
)

// DatabaseAddonBinding links a database addon to a service for env var injection
type DatabaseAddonBinding struct {
	ID         uuid.UUID                  `json:"id" db:"id"`
	AddonID    uuid.UUID                  `json:"addon_id" db:"addon_id"`
	ServiceID  uuid.UUID                  `json:"service_id" db:"service_id"`
	EnvVarName string                     `json:"env_var_name" db:"env_var_name"` // e.g., "DATABASE_URL", "REDIS_URL"
	Status     DatabaseAddonBindingStatus `json:"status" db:"status"`
	CreatedAt  time.Time                  `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time                  `json:"updated_at" db:"updated_at"`
}

// DatabaseAddonBackupType represents the type of backup
type DatabaseAddonBackupType string

const (
	DatabaseAddonBackupTypeScheduled DatabaseAddonBackupType = "scheduled"
	DatabaseAddonBackupTypeManual    DatabaseAddonBackupType = "manual"
	DatabaseAddonBackupTypePreDelete DatabaseAddonBackupType = "pre_delete"
)

// DatabaseAddonBackupStatus represents the status of a backup
type DatabaseAddonBackupStatus string

const (
	DatabaseAddonBackupStatusPending    DatabaseAddonBackupStatus = "pending"
	DatabaseAddonBackupStatusInProgress DatabaseAddonBackupStatus = "in_progress"
	DatabaseAddonBackupStatusCompleted  DatabaseAddonBackupStatus = "completed"
	DatabaseAddonBackupStatusFailed     DatabaseAddonBackupStatus = "failed"
)

// DatabaseAddonBackup represents a backup of a database addon
type DatabaseAddonBackup struct {
	ID            uuid.UUID                 `json:"id" db:"id"`
	AddonID       uuid.UUID                 `json:"addon_id" db:"addon_id"`
	BackupType    DatabaseAddonBackupType   `json:"backup_type" db:"backup_type"`
	Status        DatabaseAddonBackupStatus `json:"status" db:"status"`
	StatusMessage string                    `json:"status_message,omitempty" db:"status_message"`
	StoragePath   string                    `json:"storage_path,omitempty" db:"storage_path"`
	SizeBytes     int64                     `json:"size_bytes,omitempty" db:"size_bytes"`
	StartedAt     *time.Time                `json:"started_at,omitempty" db:"started_at"`
	CompletedAt   *time.Time                `json:"completed_at,omitempty" db:"completed_at"`
	ExpiresAt     *time.Time                `json:"expires_at,omitempty" db:"expires_at"`
	CreatedAt     time.Time                 `json:"created_at" db:"created_at"`
}

// DatabaseAddonCredentials contains connection credentials for a database addon
// Returned by the credentials API endpoint (requires authentication)
type DatabaseAddonCredentials struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	DatabaseName  string `json:"database_name"`
	Username      string `json:"username"`
	Password      string `json:"password"`       // Only exposed via secure API
	ConnectionURI string `json:"connection_uri"` // Full connection string (e.g., postgres://user:pass@host:port/db)
}

// DatabaseAddonCreateRequest is the API request for creating a database addon
type DatabaseAddonCreateRequest struct {
	Name          string              `json:"name" binding:"required"`
	Type          DatabaseAddonType   `json:"type" binding:"required"`
	EnvironmentID *uuid.UUID          `json:"environment_id,omitempty"`
	Config        DatabaseAddonConfig `json:"config,omitempty"`
}

// DatabaseAddonWithBindings includes the addon and its service bindings
type DatabaseAddonWithBindings struct {
	DatabaseAddon
	Bindings []DatabaseAddonBinding `json:"bindings,omitempty"`
}

// =============================================================================
// Templates (Starter Templates & Marketplace)
// =============================================================================

// TemplateCategory defines the category of a template
type TemplateCategory string

const (
	TemplateCategoryStarter   TemplateCategory = "starter"
	TemplateCategoryFramework TemplateCategory = "framework"
	TemplateCategoryDatabase  TemplateCategory = "database"
	TemplateCategoryFullstack TemplateCategory = "fullstack"
	TemplateCategoryAPI       TemplateCategory = "api"
	TemplateCategoryFrontend  TemplateCategory = "frontend"
)

// TemplateSourceType defines where the template source code is hosted
type TemplateSourceType string

const (
	TemplateSourceGitHub   TemplateSourceType = "github"
	TemplateSourceGitLab   TemplateSourceType = "gitlab"
	TemplateSourceInternal TemplateSourceType = "internal"
)

// TemplateDeploymentStatus defines the status of a template deployment
type TemplateDeploymentStatus string

const (
	TemplateDeploymentStatusPending    TemplateDeploymentStatus = "pending"
	TemplateDeploymentStatusInProgress TemplateDeploymentStatus = "in_progress"
	TemplateDeploymentStatusCompleted  TemplateDeploymentStatus = "completed"
	TemplateDeploymentStatusFailed     TemplateDeploymentStatus = "failed"
)

// TemplateConfig defines what resources to create when deploying a template
type TemplateConfig struct {
	Services  []TemplateServiceConfig  `json:"services,omitempty"`
	Databases []TemplateDatabaseConfig `json:"databases,omitempty"`
	EnvVars   map[string]string        `json:"env_vars,omitempty"`
}

// TemplateServiceConfig defines a service to create from a template
type TemplateServiceConfig struct {
	Name      string              `json:"name"`
	Type      string              `json:"type"` // web, worker, static
	Build     TemplateBuildConfig `json:"build"`
	Port      int                 `json:"port,omitempty"`
	EnvVars   map[string]string   `json:"env_vars,omitempty"`
	Resources *ResourceConfig     `json:"resources,omitempty"`
}

// TemplateBuildConfig defines build configuration for a template service
type TemplateBuildConfig struct {
	Type       string `json:"type"` // nixpacks, dockerfile, buildpack
	Dockerfile string `json:"dockerfile,omitempty"`
	OutputDir  string `json:"output_dir,omitempty"` // For static sites
}

// TemplateDatabaseConfig defines a database to create from a template
type TemplateDatabaseConfig struct {
	Type string `json:"type"` // postgres, redis, mysql
	Name string `json:"name"`
}

// Template represents a starter template or marketplace item
type Template struct {
	ID               uuid.UUID          `json:"id" db:"id"`
	Slug             string             `json:"slug" db:"slug"`
	Name             string             `json:"name" db:"name"`
	Description      string             `json:"description" db:"description"`
	LongDescription  string             `json:"long_description,omitempty" db:"long_description"`
	Category         TemplateCategory   `json:"category" db:"category"`
	Framework        string             `json:"framework,omitempty" db:"framework"`
	Language         string             `json:"language,omitempty" db:"language"`
	Tags             []string           `json:"tags,omitempty" db:"tags"`
	SourceType       TemplateSourceType `json:"source_type" db:"source_type"`
	SourceRepo       string             `json:"source_repo,omitempty" db:"source_repo"`
	SourceBranch     string             `json:"source_branch" db:"source_branch"`
	SourcePath       string             `json:"source_path" db:"source_path"`
	Config           TemplateConfig     `json:"config" db:"config"`
	IconURL          string             `json:"icon_url,omitempty" db:"icon_url"`
	PreviewURL       string             `json:"preview_url,omitempty" db:"preview_url"`
	ScreenshotURLs   []string           `json:"screenshot_urls,omitempty" db:"screenshot_urls"`
	Author           string             `json:"author,omitempty" db:"author"`
	AuthorURL        string             `json:"author_url,omitempty" db:"author_url"`
	DocumentationURL string             `json:"documentation_url,omitempty" db:"documentation_url"`
	DeployCount      int                `json:"deploy_count" db:"deploy_count"`
	StarCount        int                `json:"star_count" db:"star_count"`
	IsOfficial       bool               `json:"is_official" db:"is_official"`
	IsFeatured       bool               `json:"is_featured" db:"is_featured"`
	IsPublic         bool               `json:"is_public" db:"is_public"`
	CreatedAt        time.Time          `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at" db:"updated_at"`
}

// TemplateDeployment tracks a deployment from a template
type TemplateDeployment struct {
	ID           uuid.UUID                `json:"id" db:"id"`
	TemplateID   uuid.UUID                `json:"template_id" db:"template_id"`
	ProjectID    uuid.UUID                `json:"project_id" db:"project_id"`
	UserID       *uuid.UUID               `json:"user_id,omitempty" db:"user_id"`
	Status       TemplateDeploymentStatus `json:"status" db:"status"`
	ErrorMessage string                   `json:"error_message,omitempty" db:"error_message"`
	CreatedAt    time.Time                `json:"created_at" db:"created_at"`
	CompletedAt  *time.Time               `json:"completed_at,omitempty" db:"completed_at"`
}

// TemplateWithStats includes the template with additional stats for display
type TemplateWithStats struct {
	Template
	RecentDeployments int `json:"recent_deployments"` // Deployments in last 30 days
}

// TemplateListFilters defines filters for listing templates
type TemplateListFilters struct {
	Category  TemplateCategory `json:"category,omitempty"`
	Framework string           `json:"framework,omitempty"`
	Language  string           `json:"language,omitempty"`
	Tags      []string         `json:"tags,omitempty"`
	Search    string           `json:"search,omitempty"`
	Featured  *bool            `json:"featured,omitempty"`
	Official  *bool            `json:"official,omitempty"`
}

// DeployTemplateRequest is the API request for deploying a template
type DeployTemplateRequest struct {
	ProjectName string            `json:"project_name" binding:"required"`
	ProjectSlug string            `json:"project_slug,omitempty"` // Auto-generated if not provided
	EnvVars     map[string]string `json:"env_vars,omitempty"`     // Override template env vars
}

// ============================================================================
// NOTIFICATION WEBHOOK TYPES
// Slack, Discord, and Telegram notifications for deployment events
// Matches Vercel/Railway webhook functionality
// ============================================================================

// WebhookType represents the type of webhook destination
type WebhookType string

const (
	WebhookTypeSlack    WebhookType = "slack"
	WebhookTypeDiscord  WebhookType = "discord"
	WebhookTypeTelegram WebhookType = "telegram"
	WebhookTypeCustom   WebhookType = "custom"
)

// WebhookDeliveryStatus represents the status of a webhook delivery
type WebhookDeliveryStatus string

const (
	WebhookDeliveryStatusPending WebhookDeliveryStatus = "pending"
	WebhookDeliveryStatusSuccess WebhookDeliveryStatus = "success"
	WebhookDeliveryStatusFailed  WebhookDeliveryStatus = "failed"
)

// WebhookEventType defines the events that can trigger webhooks
type WebhookEventType string

const (
	// Deployment events
	WebhookEventDeploymentStarted   WebhookEventType = "deployment.started"
	WebhookEventDeploymentSucceeded WebhookEventType = "deployment.succeeded"
	WebhookEventDeploymentFailed    WebhookEventType = "deployment.failed"
	WebhookEventDeploymentCancelled WebhookEventType = "deployment.cancelled"

	// Build events
	WebhookEventBuildStarted   WebhookEventType = "build.started"
	WebhookEventBuildSucceeded WebhookEventType = "build.succeeded"
	WebhookEventBuildFailed    WebhookEventType = "build.failed"

	// Service events
	WebhookEventServiceCreated   WebhookEventType = "service.created"
	WebhookEventServiceDeleted   WebhookEventType = "service.deleted"
	WebhookEventServiceStarted   WebhookEventType = "service.started"
	WebhookEventServiceStopped   WebhookEventType = "service.stopped"
	WebhookEventServiceUnhealthy WebhookEventType = "service.unhealthy"

	// Database addon events
	WebhookEventDatabaseReady  WebhookEventType = "database.ready"
	WebhookEventDatabaseFailed WebhookEventType = "database.failed"
)

// WebhookDestination represents a configured webhook endpoint
type WebhookDestination struct {
	ID        uuid.UUID   `json:"id" db:"id"`
	ProjectID uuid.UUID   `json:"project_id" db:"project_id"`
	Name      string      `json:"name" db:"name"`
	Type      WebhookType `json:"type" db:"type"`

	// Webhook URL (for Slack, Discord, Custom)
	WebhookURL string `json:"webhook_url,omitempty" db:"webhook_url"`

	// Telegram-specific fields
	TelegramBotToken string `json:"telegram_bot_token,omitempty" db:"telegram_bot_token"` // Encrypted
	TelegramChatID   string `json:"telegram_chat_id,omitempty" db:"telegram_chat_id"`

	// Custom webhook fields
	CustomHeaders map[string]string `json:"custom_headers,omitempty" db:"custom_headers"`
	SigningSecret string            `json:"-" db:"signing_secret"` // Never exposed in API

	// Event subscriptions
	Events []WebhookEventType `json:"events" db:"events"`

	// Status
	Enabled bool `json:"enabled" db:"enabled"`

	// Delivery tracking
	LastDeliveryAt      *time.Time `json:"last_delivery_at,omitempty" db:"last_delivery_at"`
	LastDeliveryStatus  string     `json:"last_delivery_status,omitempty" db:"last_delivery_status"`
	LastDeliveryError   string     `json:"last_delivery_error,omitempty" db:"last_delivery_error"`
	ConsecutiveFailures int        `json:"consecutive_failures" db:"consecutive_failures"`
	AutoDisabledAt      *time.Time `json:"auto_disabled_at,omitempty" db:"auto_disabled_at"`

	// Audit fields
	CreatedBy      *uuid.UUID `json:"created_by,omitempty" db:"created_by"`
	CreatedByEmail string     `json:"created_by_email,omitempty" db:"created_by_email"`

	// Timestamps
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// WebhookDelivery represents a single webhook delivery attempt
type WebhookDelivery struct {
	ID            uuid.UUID             `json:"id" db:"id"`
	WebhookID     uuid.UUID             `json:"webhook_id" db:"webhook_id"`
	EventType     WebhookEventType      `json:"event_type" db:"event_type"`
	EventID       *uuid.UUID            `json:"event_id,omitempty" db:"event_id"`
	Payload       map[string]any        `json:"payload" db:"payload"`
	Status        WebhookDeliveryStatus `json:"status" db:"status"`
	StatusCode    *int                  `json:"status_code,omitempty" db:"status_code"`
	ResponseBody  string                `json:"response_body,omitempty" db:"response_body"`
	ErrorMessage  string                `json:"error_message,omitempty" db:"error_message"`
	AttemptedAt   time.Time             `json:"attempted_at" db:"attempted_at"`
	CompletedAt   *time.Time            `json:"completed_at,omitempty" db:"completed_at"`
	DurationMs    *int                  `json:"duration_ms,omitempty" db:"duration_ms"`
	AttemptNumber int                   `json:"attempt_number" db:"attempt_number"`
}

// WebhookCreateRequest is the API request for creating a webhook
type WebhookCreateRequest struct {
	Name             string             `json:"name" binding:"required"`
	Type             WebhookType        `json:"type" binding:"required"`
	WebhookURL       string             `json:"webhook_url,omitempty"`
	TelegramBotToken string             `json:"telegram_bot_token,omitempty"`
	TelegramChatID   string             `json:"telegram_chat_id,omitempty"`
	Events           []WebhookEventType `json:"events" binding:"required"`
	Enabled          *bool              `json:"enabled,omitempty"` // Defaults to true
}

// WebhookUpdateRequest is the API request for updating a webhook
type WebhookUpdateRequest struct {
	Name             *string            `json:"name,omitempty"`
	WebhookURL       *string            `json:"webhook_url,omitempty"`
	TelegramBotToken *string            `json:"telegram_bot_token,omitempty"`
	TelegramChatID   *string            `json:"telegram_chat_id,omitempty"`
	Events           []WebhookEventType `json:"events,omitempty"`
	Enabled          *bool              `json:"enabled,omitempty"`
}

// WebhookTestRequest is the API request for testing a webhook
type WebhookTestRequest struct {
	EventType WebhookEventType `json:"event_type" binding:"required"`
}

// WebhookEvent represents an event payload sent to webhooks
type WebhookEvent struct {
	ID        uuid.UUID          `json:"id"`
	Type      WebhookEventType   `json:"type"`
	Timestamp time.Time          `json:"timestamp"`
	ProjectID uuid.UUID          `json:"project_id"`
	Project   WebhookProjectInfo `json:"project"`

	// Event-specific data (one of these will be populated)
	Deployment *WebhookDeploymentInfo `json:"deployment,omitempty"`
	Build      *WebhookBuildInfo      `json:"build,omitempty"`
	Service    *WebhookServiceInfo    `json:"service,omitempty"`
	Database   *WebhookDatabaseInfo   `json:"database,omitempty"`
}

// WebhookProjectInfo contains project info included in webhook payloads
type WebhookProjectInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

// WebhookDeploymentInfo contains deployment info for webhook payloads
type WebhookDeploymentInfo struct {
	ID            uuid.UUID `json:"id"`
	ServiceName   string    `json:"service_name"`
	Environment   string    `json:"environment"`
	Status        string    `json:"status"`
	CommitSHA     string    `json:"commit_sha,omitempty"`
	CommitMessage string    `json:"commit_message,omitempty"`
	Branch        string    `json:"branch,omitempty"`
	URL           string    `json:"url,omitempty"`
	Duration      *int      `json:"duration_seconds,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// WebhookBuildInfo contains build info for webhook payloads
type WebhookBuildInfo struct {
	ID          uuid.UUID `json:"id"`
	ServiceName string    `json:"service_name"`
	Status      string    `json:"status"`
	CommitSHA   string    `json:"commit_sha,omitempty"`
	Duration    *int      `json:"duration_seconds,omitempty"`
	ImageTag    string    `json:"image_tag,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// WebhookServiceInfo contains service info for webhook payloads
type WebhookServiceInfo struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	URL    string    `json:"url,omitempty"`
	Error  string    `json:"error,omitempty"`
}

// WebhookDatabaseInfo contains database addon info for webhook payloads
type WebhookDatabaseInfo struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Type   string    `json:"type"`
	Status string    `json:"status"`
	Error  string    `json:"error,omitempty"`
}

// CIRunStatus represents the status of a CI workflow run
type CIRunStatus string

const (
	CIRunStatusQueued     CIRunStatus = "queued"
	CIRunStatusInProgress CIRunStatus = "in_progress"
	CIRunStatusCompleted  CIRunStatus = "completed"
)

// CIRunConclusion represents the final result of a completed CI run
type CIRunConclusion string

const (
	CIRunConclusionSuccess        CIRunConclusion = "success"
	CIRunConclusionFailure        CIRunConclusion = "failure"
	CIRunConclusionCancelled      CIRunConclusion = "cancelled"
	CIRunConclusionSkipped        CIRunConclusion = "skipped"
	CIRunConclusionTimedOut       CIRunConclusion = "timed_out"
	CIRunConclusionActionRequired CIRunConclusion = "action_required"
)

// CIRun represents a GitHub Actions workflow run for tracking CI status
type CIRun struct {
	ID           uuid.UUID        `json:"id" db:"id"`
	ServiceID    uuid.UUID        `json:"service_id" db:"service_id"`
	CommitSHA    string           `json:"commit_sha" db:"commit_sha"`
	WorkflowName string           `json:"workflow_name" db:"workflow_name"`
	WorkflowID   int64            `json:"workflow_id" db:"workflow_id"`
	RunID        int64            `json:"run_id" db:"run_id"`
	RunNumber    int              `json:"run_number" db:"run_number"`
	Status       CIRunStatus      `json:"status" db:"status"`
	Conclusion   *CIRunConclusion `json:"conclusion,omitempty" db:"conclusion"`
	HTMLURL      string           `json:"html_url,omitempty" db:"html_url"`
	Branch       string           `json:"branch,omitempty" db:"branch"`
	EventType    string           `json:"event_type,omitempty" db:"event_type"`
	Actor        string           `json:"actor,omitempty" db:"actor"`
	StartedAt    *time.Time       `json:"started_at,omitempty" db:"started_at"`
	CompletedAt  *time.Time       `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt    time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at" db:"updated_at"`
}
