// Admin Control Plane TypeScript types mirroring Go types

export type ClusterType = 'k3s' | 'k8s' | 'vcluster'
export type ClusterStatus = 'pending' | 'ready' | 'degraded' | 'offline' | 'deleting'

export interface Cluster {
  id: string
  name: string
  slug: string
  type: ClusterType
  endpoint?: string
  kubeconfig_secret_ref?: string
  region?: string
  status: ClusterStatus
  metadata?: Record<string, unknown>
  created_at: string
  updated_at: string
}

export type BMHState = 'discovered' | 'inspecting' | 'available' | 'provisioning' | 'provisioned' | 'deprovisioning' | 'error'
export type BMHPowerState = 'on' | 'off' | 'unknown'

export interface BareMetalHost {
  id: string
  name: string
  cluster_id?: string
  bmc_address: string
  bmc_credentials_ref: string
  mac_address?: string
  boot_mode: string
  state: BMHState
  power_state: BMHPowerState
  hardware_profile?: Record<string, unknown>
  firmware_version?: string
  root_device_hints?: Record<string, unknown>
  raid_config?: Record<string, unknown>
  cost_per_hour_cents: number
  last_inspection_at?: string
  created_at: string
  updated_at: string
}

export type ManagementPolicy = 'FullControl' | 'ObserveOnly' | 'OrphanOnDelete'
export type SyncStatus = 'Synced' | 'OutOfSync' | 'Unknown' | 'Error'

export interface ManagedResource {
  id: string
  name: string
  api_version: string
  kind: string
  provider: string
  cluster_id?: string
  management_policy: ManagementPolicy
  sync_status: SyncStatus
  conditions?: unknown[]
  spec_hash?: string
  metadata?: Record<string, unknown>
  created_at: string
  updated_at: string
}

export type VClusterStatus = 'pending' | 'creating' | 'running' | 'paused' | 'deleting' | 'error'

export interface VirtualCluster {
  id: string
  name: string
  host_cluster_id: string
  tenant_id?: string
  namespace: string
  k8s_version?: string
  status: VClusterStatus
  helm_release_name?: string
  resource_quota?: Record<string, unknown>
  created_at: string
  updated_at: string
}

export type PlacementStrategy = 'Spread' | 'Binpack' | 'GPUAffinity'

export interface PropagationPolicy {
  id: string
  name: string
  cluster_ids: string[]
  resource_selectors?: unknown[]
  placement_strategy: PlacementStrategy
  gpu_required: boolean
  priority: number
  created_at: string
  updated_at: string
}

export type DriftSource = 'argocd' | 'crossplane' | 'manual'
export type DriftSeverity = 'low' | 'medium' | 'high' | 'critical'

export interface DriftEvent {
  id: string
  source: DriftSource
  resource_type: string
  resource_name: string
  cluster_id?: string
  drift_details?: Record<string, unknown>
  severity: DriftSeverity
  resolved: boolean
  resolved_at?: string
  detected_at: string
  created_at: string
}

export interface CostAllocation {
  id: string
  bare_metal_host_id: string
  tenant_id: string
  allocation_percent: number
  period_start: string
  period_end: string
  cost_cents: number
  created_at: string
}

export interface TopologyNode {
  id: string
  type: string
  label: string
  status: string
  data?: Record<string, unknown>
  position: { x: number; y: number }
}

export interface TopologyEdge {
  id: string
  source: string
  target: string
  label?: string
}
