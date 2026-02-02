package types

import (
	"time"

	"github.com/google/uuid"
)

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
	ConnectionURI string `json:"connection_uri"` // Full connection string
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
