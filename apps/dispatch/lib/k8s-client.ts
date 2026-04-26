/**
 * In-cluster Kubernetes client for Dispatch.
 *
 * Dispatch runs as a pod inside the cluster. We load the in-cluster
 * ServiceAccount config via KubeConfig.loadFromCluster() and reuse a single
 * KubeConfig instance across requests.
 *
 * For local development (no in-cluster SA), falls back to KUBECONFIG file.
 *
 * RBAC requirements live in apps/dispatch/k8s/rbac.yaml — the ServiceAccount
 * must be granted read on: nodes, pods, services, deployments, statefulsets,
 * daemonsets, namespaces, networkpolicies, persistentvolumeclaims, plus
 * applications.argoproj.io and *.longhorn.io custom resources.
 */
import * as k8s from '@kubernetes/client-node'

let cachedConfig: k8s.KubeConfig | null = null

export function getKubeConfig(): k8s.KubeConfig {
  if (cachedConfig) return cachedConfig
  const kc = new k8s.KubeConfig()
  try {
    kc.loadFromCluster()
  } catch {
    kc.loadFromDefault()
  }
  cachedConfig = kc
  return kc
}

export function coreApi(): k8s.CoreV1Api {
  return getKubeConfig().makeApiClient(k8s.CoreV1Api)
}

export function appsApi(): k8s.AppsV1Api {
  return getKubeConfig().makeApiClient(k8s.AppsV1Api)
}

export function networkingApi(): k8s.NetworkingV1Api {
  return getKubeConfig().makeApiClient(k8s.NetworkingV1Api)
}

export function customObjectsApi(): k8s.CustomObjectsApi {
  return getKubeConfig().makeApiClient(k8s.CustomObjectsApi)
}

/**
 * Parse Kubernetes resource quantity strings into numeric base units.
 * - CPU: returns millicores (e.g. "500m" → 500, "2" → 2000)
 * - Memory/storage: returns bytes (e.g. "1Gi" → 1073741824)
 */
export function parseCpu(value: string | undefined): number {
  if (!value) return 0
  if (value.endsWith('m')) return parseInt(value.slice(0, -1), 10)
  if (value.endsWith('n')) return Math.round(parseInt(value.slice(0, -1), 10) / 1_000_000)
  return Math.round(parseFloat(value) * 1000)
}

export function parseMemory(value: string | undefined): number {
  if (!value) return 0
  const match = value.match(/^(\d+(?:\.\d+)?)([KMGTPE]i?)?$/)
  if (!match) return parseInt(value, 10) || 0
  const num = parseFloat(match[1])
  const unit = match[2] || ''
  const multipliers: Record<string, number> = {
    '': 1,
    K: 1000,
    Ki: 1024,
    M: 1000 * 1000,
    Mi: 1024 * 1024,
    G: 1000 ** 3,
    Gi: 1024 ** 3,
    T: 1000 ** 4,
    Ti: 1024 ** 4,
    P: 1000 ** 5,
    Pi: 1024 ** 5,
    E: 1000 ** 6,
    Ei: 1024 ** 6,
  }
  return Math.round(num * (multipliers[unit] ?? 1))
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  const idx = Math.min(units.length - 1, Math.floor(Math.log2(bytes) / 10))
  return `${(bytes / Math.pow(1024, idx)).toFixed(1)} ${units[idx]}`
}

export function formatCpu(millicores: number): string {
  if (millicores === 0) return '0'
  if (millicores < 1000) return `${millicores}m`
  return `${(millicores / 1000).toFixed(1)} cores`
}

const memoryCache = new Map<string, { value: unknown; expiresAt: number }>()

export async function cached<T>(key: string, ttlMs: number, fn: () => Promise<T>): Promise<T> {
  const now = Date.now()
  const hit = memoryCache.get(key)
  if (hit && hit.expiresAt > now) return hit.value as T
  const value = await fn()
  memoryCache.set(key, { value, expiresAt: now + ttlMs })
  return value
}

export function invalidateCache(prefix?: string): void {
  if (!prefix) {
    memoryCache.clear()
    return
  }
  for (const key of memoryCache.keys()) {
    if (key.startsWith(prefix)) memoryCache.delete(key)
  }
}
