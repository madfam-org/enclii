/**
 * Tests for lib/k8s-client.ts pure helpers.
 *
 * The KubeConfig loading is deliberately NOT tested here — it depends on
 * environment / in-cluster ServiceAccount which the unit suite cannot
 * meaningfully exercise. Coverage of those code paths happens during
 * integration smoke testing against a real cluster.
 *
 * @kubernetes/client-node is mocked at module scope because it transitively
 * imports jsonpath-plus, an ESM-only package Jest cannot transform without
 * additional config. The pure helpers don't touch the SDK so the mock is benign.
 */
jest.mock('@kubernetes/client-node', () => ({
  KubeConfig: class {
    loadFromCluster() {}
    loadFromDefault() {}
    makeApiClient() { return {} }
  },
  CoreV1Api: class {},
  AppsV1Api: class {},
  NetworkingV1Api: class {},
  CustomObjectsApi: class {},
}))

import {
  cached,
  formatBytes,
  formatCpu,
  invalidateCache,
  parseCpu,
  parseMemory,
} from '@/lib/k8s-client'

describe('parseCpu', () => {
  it('returns 0 for empty input', () => {
    expect(parseCpu(undefined)).toBe(0)
    expect(parseCpu('')).toBe(0)
  })

  it('parses millicore suffix', () => {
    expect(parseCpu('500m')).toBe(500)
    expect(parseCpu('2000m')).toBe(2000)
  })

  it('parses bare integer/float as cores', () => {
    expect(parseCpu('2')).toBe(2000)
    expect(parseCpu('0.5')).toBe(500)
    expect(parseCpu('1.25')).toBe(1250)
  })

  it('parses nanocore suffix', () => {
    expect(parseCpu('1500000000n')).toBe(1500)
  })
})

describe('parseMemory', () => {
  it('returns 0 for empty input', () => {
    expect(parseMemory(undefined)).toBe(0)
    expect(parseMemory('')).toBe(0)
  })

  it('parses bytes literal', () => {
    expect(parseMemory('1024')).toBe(1024)
  })

  it('parses Ki/Mi/Gi suffixes (binary)', () => {
    expect(parseMemory('1Ki')).toBe(1024)
    expect(parseMemory('1Mi')).toBe(1024 * 1024)
    expect(parseMemory('2Gi')).toBe(2 * 1024 ** 3)
  })

  it('parses K/M/G suffixes (decimal)', () => {
    expect(parseMemory('1K')).toBe(1000)
    expect(parseMemory('1M')).toBe(1_000_000)
    expect(parseMemory('1G')).toBe(1_000_000_000)
  })

  it('handles fractional sizes', () => {
    expect(parseMemory('1.5Gi')).toBe(Math.round(1.5 * 1024 ** 3))
  })
})

describe('formatBytes', () => {
  it('handles zero', () => {
    expect(formatBytes(0)).toBe('0 B')
  })

  it('formats KiB/MiB/GiB', () => {
    expect(formatBytes(1024)).toBe('1.0 KiB')
    expect(formatBytes(1024 * 1024)).toBe('1.0 MiB')
    expect(formatBytes(2 * 1024 ** 3)).toBe('2.0 GiB')
  })
})

describe('formatCpu', () => {
  it('handles zero', () => {
    expect(formatCpu(0)).toBe('0')
  })

  it('formats sub-1000 millicores', () => {
    expect(formatCpu(500)).toBe('500m')
  })

  it('formats >=1000 as cores', () => {
    expect(formatCpu(2000)).toBe('2.0 cores')
    expect(formatCpu(1500)).toBe('1.5 cores')
  })
})

describe('cached', () => {
  beforeEach(() => {
    invalidateCache()
  })

  it('returns the inner value on first call', async () => {
    const result = await cached('test:1', 1000, async () => 42)
    expect(result).toBe(42)
  })

  it('returns cached value on subsequent calls within TTL', async () => {
    let calls = 0
    const fn = async () => {
      calls += 1
      return calls
    }
    const a = await cached('test:2', 1000, fn)
    const b = await cached('test:2', 1000, fn)
    expect(a).toBe(1)
    expect(b).toBe(1)
    expect(calls).toBe(1)
  })

  it('refreshes after the TTL expires', async () => {
    let calls = 0
    const fn = async () => {
      calls += 1
      return calls
    }
    const a = await cached('test:3', 5, fn)
    await new Promise((r) => setTimeout(r, 15))
    const b = await cached('test:3', 5, fn)
    expect(a).toBe(1)
    expect(b).toBe(2)
  })

  it('invalidateCache(prefix) only clears matching keys', async () => {
    let topoCalls = 0
    let argoCalls = 0
    const topo = async () => ++topoCalls
    const argo = async () => ++argoCalls

    await cached('topology:v1', 1000, topo)
    await cached('argocd:apps:v1', 1000, argo)

    invalidateCache('topology:')

    await cached('topology:v1', 1000, topo)
    await cached('argocd:apps:v1', 1000, argo)

    expect(topoCalls).toBe(2) // refetched after invalidate
    expect(argoCalls).toBe(1) // still cached
  })
})
