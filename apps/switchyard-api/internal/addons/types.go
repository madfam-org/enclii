package addons

import (
	"context"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// AddonProvisioner defines the interface for database addon provisioners
type AddonProvisioner interface {
	// Provision creates a new database addon in Kubernetes
	Provision(ctx context.Context, req *ProvisionRequest) (*ProvisionResult, error)

	// Deprovision removes a database addon from Kubernetes
	Deprovision(ctx context.Context, addon *types.DatabaseAddon) error

	// GetStatus checks the current status of a provisioned addon
	GetStatus(ctx context.Context, addon *types.DatabaseAddon) (*StatusResult, error)

	// GetCredentials retrieves connection credentials from K8s secrets
	GetCredentials(ctx context.Context, addon *types.DatabaseAddon) (*types.DatabaseAddonCredentials, error)

	// GetConnectionURI builds the connection URI for the addon
	GetConnectionURI(ctx context.Context, addon *types.DatabaseAddon) (string, error)
}

// ProvisionRequest contains the details for provisioning a new addon
type ProvisionRequest struct {
	Addon     *types.DatabaseAddon
	Namespace string
	ProjectID uuid.UUID
}

// ProvisionResult contains the result of a provisioning operation
type ProvisionResult struct {
	K8sResourceName  string
	ConnectionSecret string
	Message          string
}

// StatusResult contains the current status of an addon
type StatusResult struct {
	Status        types.DatabaseAddonStatus
	StatusMessage string
	Host          string
	Port          int
	DatabaseName  string
	Username      string
	Ready         bool
}

// CloudNativePGCluster represents the CloudNativePG Cluster CRD spec
type CloudNativePGCluster struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Metadata   CloudNativePGMetadata    `json:"metadata"`
	Spec       CloudNativePGClusterSpec `json:"spec"`
}

type CloudNativePGMetadata struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type CloudNativePGClusterSpec struct {
	Instances             int                      `json:"instances"`
	ImageName             string                   `json:"imageName,omitempty"`
	PostgresVersion       int                      `json:"postgresVersion,omitempty"`
	PrimaryUpdateStrategy string                   `json:"primaryUpdateStrategy,omitempty"`
	Storage               CloudNativePGStorage     `json:"storage"`
	Resources             CloudNativePGResources   `json:"resources,omitempty"`
	Bootstrap             *CloudNativePGBootstrap  `json:"bootstrap,omitempty"`
	Monitoring            *CloudNativePGMonitoring `json:"monitoring,omitempty"`
	Backup                *CloudNativePGBackupSpec `json:"backup,omitempty"`
	SuperuserSecret       *CloudNativePGSecretRef  `json:"superuserSecret,omitempty"`
}

type CloudNativePGStorage struct {
	Size         string `json:"size"`
	StorageClass string `json:"storageClass,omitempty"`
}

type CloudNativePGResources struct {
	Requests CloudNativePGResourceList `json:"requests,omitempty"`
	Limits   CloudNativePGResourceList `json:"limits,omitempty"`
}

type CloudNativePGResourceList struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

type CloudNativePGBootstrap struct {
	InitDB *CloudNativePGInitDB `json:"initdb,omitempty"`
}

type CloudNativePGInitDB struct {
	Database string                  `json:"database"`
	Owner    string                  `json:"owner"`
	Secret   *CloudNativePGSecretRef `json:"secret,omitempty"`
}

type CloudNativePGSecretRef struct {
	Name string `json:"name"`
}

type CloudNativePGMonitoring struct {
	EnablePodMonitor bool `json:"enablePodMonitor,omitempty"`
}

type CloudNativePGBackupSpec struct {
	BarmanObjectStore *CloudNativePGBarmanStore `json:"barmanObjectStore,omitempty"`
	RetentionPolicy   string                    `json:"retentionPolicy,omitempty"`
}

type CloudNativePGBarmanStore struct {
	DestinationPath string                      `json:"destinationPath"`
	EndpointURL     string                      `json:"endpointURL,omitempty"`
	S3Credentials   *CloudNativePGS3Credentials `json:"s3Credentials,omitempty"`
	Wal             *CloudNativePGWalConfig     `json:"wal,omitempty"`
	Data            *CloudNativePGDataConfig    `json:"data,omitempty"`
}

type CloudNativePGS3Credentials struct {
	AccessKeyID     CloudNativePGSecretKeyRef `json:"accessKeyId"`
	SecretAccessKey CloudNativePGSecretKeyRef `json:"secretAccessKey"`
}

type CloudNativePGSecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type CloudNativePGWalConfig struct {
	Compression string `json:"compression,omitempty"`
}

type CloudNativePGDataConfig struct {
	Compression string `json:"compression,omitempty"`
}

// Default configuration values
const (
	// 18 matches what CNPG's operator has ACTUALLY been deploying while the
	// old `postgresVersion` field was silently pruned (live clusters run
	// 18.3) — pinning the default anywhere lower would make brand-new addons
	// OLDER than the unpinned fleet. The point of the constant is that it is
	// now honest: it becomes the imageName tag and therefore the truth.
	DefaultPostgresVersion = 18
	DefaultStorageSize     = "10Gi"
	DefaultCPU             = "100m"
	DefaultMemory          = "256Mi"
	DefaultInstances       = 1
	DefaultDatabase        = "app"

	// Backups. The barman object store lives OUTSIDE the cluster (R2); the
	// credentials Secret is replicated into each addon namespace from the
	// enclii namespace because CNPG resolves s3Credentials locally. An empty
	// destination base disables backup wiring — loudly, per provision.
	// #nosec G101 -- this is the NAME of a Kubernetes Secret, not a credential value.
	BackupCredentialsSecretName = "enclii-db-backup-credentials"
	BackupCredentialsSourceNS   = "enclii"
	BackupRetention             = "30d"
	BackupSchedule              = "0 0 4 * * *" // daily 04:00 UTC (22:00 CDMX)
	DefaultUser                 = "app"

	// CloudNativePG constants
	CloudNativePGAPIVersion = "postgresql.cnpg.io/v1"
	CloudNativePGKind       = "Cluster"

	// Labels
	LabelManagedBy    = "managed-by"
	LabelAddonID      = "enclii.dev/addon-id"
	LabelProjectID    = "enclii.dev/project-id"
	LabelAddonType    = "enclii.dev/addon-type"
	LabelManagedValue = "enclii"
)
