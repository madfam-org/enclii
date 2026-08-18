package addons

import (
	"context"
	"time"

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
	DefaultPostgresVersion = 16
	DefaultStorageSize     = "10Gi"
	DefaultCPU             = "100m"
	DefaultMemory          = "256Mi"
	DefaultInstances       = 1
	DefaultDatabase        = "app"
	DefaultUser            = "app"

	// DefaultDeletionRetention is the grace window a data-bearing addon spends
	// in the pending_deletion state before its CNPG Cluster + PVCs are actually
	// torn down. During this window a mistaken or malicious delete is
	// recoverable. 7 days mirrors the backup retention horizon and gives a
	// departing client a "dignified exit" window to pull an export
	// (2026-08-17 audit #10; relevant to ADR-0004).
	DefaultDeletionRetention = 7 * 24 * time.Hour

	// CloudNativePG constants
	CloudNativePGAPIVersion = "postgresql.cnpg.io/v1"
	CloudNativePGKind       = "Cluster"

	// RetainStorageClass is the Longhorn StorageClass whose reclaimPolicy is
	// Retain (infra/helm/longhorn/storageclass.yaml). Managed-DB PVCs are
	// pinned here so that when a Cluster is torn down — CNPG deletes the PVCs on
	// cluster deletion — the underlying PersistentVolume (and Longhorn volume)
	// survives and the data stays recoverable. The default `longhorn` class is
	// reclaimPolicy: Delete, which destroys the volume with the PVC; that was
	// the unrecoverable path the retention hold guards against (2026-08-17
	// audit #10).
	RetainStorageClass = "longhorn-replicated"

	// Labels
	LabelManagedBy    = "managed-by"
	LabelAddonID      = "enclii.dev/addon-id"
	LabelProjectID    = "enclii.dev/project-id"
	LabelAddonType    = "enclii.dev/addon-type"
	LabelManagedValue = "enclii"
)
