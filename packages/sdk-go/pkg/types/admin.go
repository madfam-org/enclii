package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// ADMIN CONTROL PLANE TYPES
// Universal Control Plane entities for fleet, infrastructure, multi-tenancy,
// governance, and cost tracking.
// ============================================================================

// ClusterType represents the type of Kubernetes cluster
type ClusterType string

const (
	ClusterTypeK3s      ClusterType = "k3s"
	ClusterTypeK8s      ClusterType = "k8s"
	ClusterTypeVCluster ClusterType = "vcluster"
)

// ClusterStatus represents the status of a cluster
type ClusterStatus string

const (
	ClusterStatusPending  ClusterStatus = "pending"
	ClusterStatusReady    ClusterStatus = "ready"
	ClusterStatusDegraded ClusterStatus = "degraded"
	ClusterStatusOffline  ClusterStatus = "offline"
	ClusterStatusDeleting ClusterStatus = "deleting"
)

// Cluster represents a registered Kubernetes cluster
type Cluster struct {
	ID                  uuid.UUID       `json:"id" db:"id"`
	Name                string          `json:"name" db:"name"`
	Slug                string          `json:"slug" db:"slug"`
	Type                ClusterType     `json:"type" db:"type"`
	Endpoint            string          `json:"endpoint,omitempty" db:"endpoint"`
	KubeconfigSecretRef string          `json:"kubeconfig_secret_ref,omitempty" db:"kubeconfig_secret_ref"`
	Region              string          `json:"region,omitempty" db:"region"`
	Status              ClusterStatus   `json:"status" db:"status"`
	Metadata            json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt           time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at" db:"updated_at"`
}

// BMHState represents the state of a Bare Metal Host
type BMHState string

const (
	BMHStateDiscovered     BMHState = "discovered"
	BMHStateInspecting     BMHState = "inspecting"
	BMHStateAvailable      BMHState = "available"
	BMHStateProvisioning   BMHState = "provisioning"
	BMHStateProvisioned    BMHState = "provisioned"
	BMHStateDeprovisioning BMHState = "deprovisioning"
	BMHStateError          BMHState = "error"
)

// BMHPowerState represents the power state of a Bare Metal Host
type BMHPowerState string

const (
	BMHPowerOn      BMHPowerState = "on"
	BMHPowerOff     BMHPowerState = "off"
	BMHPowerUnknown BMHPowerState = "unknown"
)

// BareMetalHost represents a physical server managed via Metal3/BMC
type BareMetalHost struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	Name              string          `json:"name" db:"name"`
	ClusterID         *uuid.UUID      `json:"cluster_id,omitempty" db:"cluster_id"`
	BMCAddress        string          `json:"bmc_address" db:"bmc_address"`
	BMCCredentialsRef string          `json:"bmc_credentials_ref" db:"bmc_credentials_ref"`
	MACAddress        string          `json:"mac_address,omitempty" db:"mac_address"`
	BootMode          string          `json:"boot_mode" db:"boot_mode"`
	State             BMHState        `json:"state" db:"state"`
	PowerState        BMHPowerState   `json:"power_state" db:"power_state"`
	HardwareProfile   json.RawMessage `json:"hardware_profile,omitempty" db:"hardware_profile"`
	FirmwareVersion   string          `json:"firmware_version,omitempty" db:"firmware_version"`
	RootDeviceHints   json.RawMessage `json:"root_device_hints,omitempty" db:"root_device_hints"`
	RAIDConfig        json.RawMessage `json:"raid_config,omitempty" db:"raid_config"`
	CostPerHourCents  int             `json:"cost_per_hour_cents" db:"cost_per_hour_cents"`
	LastInspectionAt  *time.Time      `json:"last_inspection_at,omitempty" db:"last_inspection_at"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

// ManagementPolicy for Crossplane managed resources
type ManagementPolicy string

const (
	ManagementPolicyFullControl    ManagementPolicy = "FullControl"
	ManagementPolicyObserveOnly    ManagementPolicy = "ObserveOnly"
	ManagementPolicyOrphanOnDelete ManagementPolicy = "OrphanOnDelete"
)

// SyncStatus for managed resources
type SyncStatus string

const (
	SyncStatusSynced    SyncStatus = "Synced"
	SyncStatusOutOfSync SyncStatus = "OutOfSync"
	SyncStatusUnknown   SyncStatus = "Unknown"
	SyncStatusError     SyncStatus = "Error"
)

// ManagedResource represents a Crossplane-managed infrastructure resource
type ManagedResource struct {
	ID               uuid.UUID        `json:"id" db:"id"`
	Name             string           `json:"name" db:"name"`
	APIVersion       string           `json:"api_version" db:"api_version"`
	Kind             string           `json:"kind" db:"kind"`
	Provider         string           `json:"provider" db:"provider"`
	ClusterID        *uuid.UUID       `json:"cluster_id,omitempty" db:"cluster_id"`
	ManagementPolicy ManagementPolicy `json:"management_policy" db:"management_policy"`
	SyncStatus       SyncStatus       `json:"sync_status" db:"sync_status"`
	Conditions       json.RawMessage  `json:"conditions,omitempty" db:"conditions"`
	SpecHash         string           `json:"spec_hash,omitempty" db:"spec_hash"`
	Metadata         json.RawMessage  `json:"metadata,omitempty" db:"metadata"`
	CreatedAt        time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at" db:"updated_at"`
}

// VClusterStatus represents the status of a virtual cluster
type VClusterStatus string

const (
	VClusterStatusPending  VClusterStatus = "pending"
	VClusterStatusCreating VClusterStatus = "creating"
	VClusterStatusRunning  VClusterStatus = "running"
	VClusterStatusPaused   VClusterStatus = "paused"
	VClusterStatusDeleting VClusterStatus = "deleting"
	VClusterStatusError    VClusterStatus = "error"
)

// VirtualCluster represents a vCluster tenant cluster
type VirtualCluster struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	Name            string          `json:"name" db:"name"`
	HostClusterID   uuid.UUID       `json:"host_cluster_id" db:"host_cluster_id"`
	TenantID        string          `json:"tenant_id,omitempty" db:"tenant_id"`
	Namespace       string          `json:"namespace" db:"namespace"`
	K8sVersion      string          `json:"k8s_version,omitempty" db:"k8s_version"`
	Status          VClusterStatus  `json:"status" db:"status"`
	HelmReleaseName string          `json:"helm_release_name,omitempty" db:"helm_release_name"`
	ResourceQuota   json.RawMessage `json:"resource_quota,omitempty" db:"resource_quota"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}

// PlacementStrategy for propagation policies
type PlacementStrategy string

const (
	PlacementStrategySpread      PlacementStrategy = "Spread"
	PlacementStrategyBinpack     PlacementStrategy = "Binpack"
	PlacementStrategyGPUAffinity PlacementStrategy = "GPUAffinity"
)

// PropagationPolicy defines how resources are distributed across clusters
type PropagationPolicy struct {
	ID                uuid.UUID         `json:"id" db:"id"`
	Name              string            `json:"name" db:"name"`
	ClusterIDs        []uuid.UUID       `json:"cluster_ids" db:"cluster_ids"`
	ResourceSelectors json.RawMessage   `json:"resource_selectors,omitempty" db:"resource_selectors"`
	PlacementStrategy PlacementStrategy `json:"placement_strategy" db:"placement_strategy"`
	GPURequired       bool              `json:"gpu_required" db:"gpu_required"`
	Priority          int               `json:"priority" db:"priority"`
	CreatedAt         time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at" db:"updated_at"`
}

// DriftSource identifies where drift was detected
type DriftSource string

const (
	DriftSourceArgoCD     DriftSource = "argocd"
	DriftSourceCrossplane DriftSource = "crossplane"
	DriftSourceManual     DriftSource = "manual"
)

// DriftSeverity represents the severity of a drift event
type DriftSeverity string

const (
	DriftSeverityLow      DriftSeverity = "low"
	DriftSeverityMedium   DriftSeverity = "medium"
	DriftSeverityHigh     DriftSeverity = "high"
	DriftSeverityCritical DriftSeverity = "critical"
)

// DriftEvent represents a detected configuration drift
type DriftEvent struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	Source       DriftSource     `json:"source" db:"source"`
	ResourceType string          `json:"resource_type" db:"resource_type"`
	ResourceName string          `json:"resource_name" db:"resource_name"`
	ClusterID    *uuid.UUID      `json:"cluster_id,omitempty" db:"cluster_id"`
	DriftDetails json.RawMessage `json:"drift_details,omitempty" db:"drift_details"`
	Severity     DriftSeverity   `json:"severity" db:"severity"`
	Resolved     bool            `json:"resolved" db:"resolved"`
	ResolvedAt   *time.Time      `json:"resolved_at,omitempty" db:"resolved_at"`
	DetectedAt   time.Time       `json:"detected_at" db:"detected_at"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
}

// CostAllocation tracks infrastructure cost attribution per tenant
type CostAllocation struct {
	ID                uuid.UUID `json:"id" db:"id"`
	BareMetalHostID   uuid.UUID `json:"bare_metal_host_id" db:"bare_metal_host_id"`
	TenantID          string    `json:"tenant_id" db:"tenant_id"`
	AllocationPercent float64   `json:"allocation_percent" db:"allocation_percent"`
	PeriodStart       time.Time `json:"period_start" db:"period_start"`
	PeriodEnd         time.Time `json:"period_end" db:"period_end"`
	CostCents         int       `json:"cost_cents" db:"cost_cents"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
}
