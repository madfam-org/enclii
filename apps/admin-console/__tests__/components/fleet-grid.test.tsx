/**
 * Tests for components/fleet/fleet-grid.tsx
 *
 * Tests the FleetGrid component which displays bare metal hosts
 * in a grid layout with power/wipe actions and host detail dialogs.
 */

import React from 'react'
import { render, screen, waitFor, act } from '@testing-library/react'

// Mock the admin-api module
jest.mock('@/lib/admin-api', () => ({
  fleetApi: {
    list: jest.fn(),
    power: jest.fn(),
    wipe: jest.fn(),
    update: jest.fn(),
  },
}))

// Mock lucide-react icons to simple spans (including X used by Dialog)
jest.mock('lucide-react', () => {
  const icon = (name: string) => {
    const Component = (props: any) => <span data-testid={`icon-${name}`} {...props} />
    Component.displayName = name
    return Component
  }
  return {
    Server: icon('server'),
    Power: icon('power'),
    HardDrive: icon('harddrive'),
    Trash2: icon('trash'),
    RefreshCw: icon('refresh'),
    Clock: icon('clock'),
    Cpu: icon('cpu'),
    DollarSign: icon('dollar'),
    X: icon('x'),
  }
})

import { FleetGrid } from '@/components/fleet/fleet-grid'
import { fleetApi } from '@/lib/admin-api'
import type { BareMetalHost } from '@/types/admin'

const mockFleetApi = fleetApi as jest.Mocked<typeof fleetApi>

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

function createHost(overrides: Partial<BareMetalHost> = {}): BareMetalHost {
  return {
    id: 'host-1',
    name: 'foundry-core',
    bmc_address: '10.0.0.1',
    bmc_credentials_ref: 'secret/bmc',
    mac_address: 'AA:BB:CC:DD:EE:FF',
    boot_mode: 'UEFI',
    state: 'provisioned',
    power_state: 'on',
    hardware_profile: { cpu_cores: 12, memory_gb: 64, arch: 'x86_64' },
    firmware_version: '1.2.3',
    cost_per_hour_cents: 7,
    last_inspection_at: new Date(Date.now() - 300000).toISOString(), // 5 min ago
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-06-01T00:00:00Z',
    ...overrides,
  }
}

beforeEach(() => {
  jest.clearAllMocks()
})

// =============================================================================
// Loading state
// =============================================================================

describe('FleetGrid loading', () => {
  it('shows a loading spinner while fetching', () => {
    // Never resolve the list call so it stays loading
    mockFleetApi.list.mockReturnValue(new Promise(() => {}))

    render(<FleetGrid />)

    // The loading spinner has animate-spin class
    const spinner = document.querySelector('.animate-spin')
    expect(spinner).toBeTruthy()
  })
})

// =============================================================================
// Empty state
// =============================================================================

describe('FleetGrid empty state', () => {
  it('shows empty state when no hosts returned', async () => {
    mockFleetApi.list.mockResolvedValueOnce({ hosts: [] })

    render(<FleetGrid />)

    await waitFor(() => {
      expect(screen.getByText('No Bare Metal Hosts')).toBeInTheDocument()
    })
    expect(screen.getByText(/Register your first bare metal host/)).toBeInTheDocument()
  })
})

// =============================================================================
// Error state
// =============================================================================

describe('FleetGrid error state', () => {
  it('shows error message with retry button on fetch failure', async () => {
    mockFleetApi.list.mockRejectedValueOnce(new Error('Network failure'))

    render(<FleetGrid />)

    await waitFor(() => {
      expect(screen.getByText('Network failure')).toBeInTheDocument()
    })
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })

  it('shows empty state for 401 errors (treats as no hosts)', async () => {
    mockFleetApi.list.mockRejectedValueOnce(new Error('401 Authorization required'))

    render(<FleetGrid />)

    await waitFor(() => {
      expect(screen.getByText('No Bare Metal Hosts')).toBeInTheDocument()
    })
  })

  it('calls fleetApi.list again when retry button is clicked', async () => {
    // First call fails
    mockFleetApi.list.mockRejectedValueOnce(new Error('Timeout'))
    // Second call also set up (may succeed or fail, we just verify it was called)
    mockFleetApi.list.mockResolvedValueOnce({ hosts: [createHost()] })

    render(<FleetGrid />)

    await waitFor(() => {
      expect(screen.getByText('Retry')).toBeInTheDocument()
    })

    expect(mockFleetApi.list).toHaveBeenCalledTimes(1)

    await act(async () => {
      screen.getByText('Retry').click()
    })

    // Verify the API was called a second time (retry attempt)
    await waitFor(() => {
      expect(mockFleetApi.list).toHaveBeenCalledTimes(2)
    })
  })
})

// =============================================================================
// Host rendering
// =============================================================================

describe('FleetGrid host rendering', () => {
  it('renders host cards with name and state', async () => {
    const hosts = [
      createHost({ id: 'h1', name: 'foundry-core', state: 'provisioned' }),
      createHost({ id: 'h2', name: 'builder-01', state: 'available' }),
    ]
    mockFleetApi.list.mockResolvedValueOnce({ hosts })

    render(<FleetGrid />)

    await waitFor(() => {
      expect(screen.getByText('foundry-core')).toBeInTheDocument()
    })
    expect(screen.getByText('builder-01')).toBeInTheDocument()
    expect(screen.getByText('provisioned')).toBeInTheDocument()
    expect(screen.getByText('available')).toBeInTheDocument()
  })

  it('renders MAC address when present', async () => {
    mockFleetApi.list.mockResolvedValueOnce({
      hosts: [createHost({ mac_address: 'AA:BB:CC:DD:EE:FF' })],
    })

    render(<FleetGrid />)

    await waitFor(() => {
      expect(screen.getByText('AA:BB:CC:DD:EE:FF')).toBeInTheDocument()
    })
  })

  it('renders hardware summary with CPU info', async () => {
    mockFleetApi.list.mockResolvedValueOnce({
      hosts: [createHost({ hardware_profile: { cpu_cores: 12, memory_gb: 64, arch: 'x86_64' } })],
    })

    render(<FleetGrid />)

    await waitFor(() => {
      expect(screen.getByText(/12 cores/)).toBeInTheDocument()
    })
    expect(screen.getByText(/64GB/)).toBeInTheDocument()
  })
})

// =============================================================================
// Host detail dialog
// =============================================================================

describe('FleetGrid host detail dialog', () => {
  it('opens detail dialog when clicking a host card', async () => {
    const host = createHost()
    mockFleetApi.list.mockResolvedValueOnce({ hosts: [host] })

    render(<FleetGrid />)

    await waitFor(() => {
      expect(screen.getByText('foundry-core')).toBeInTheDocument()
    })

    // Click the host card (the card is the div containing the name)
    await act(async () => {
      screen.getByText('foundry-core').closest('[class*="rounded-lg"]')?.dispatchEvent(
        new MouseEvent('click', { bubbles: true })
      )
    })

    // Detail dialog should show BMC Address
    await waitFor(() => {
      expect(screen.getByText('BMC Address')).toBeInTheDocument()
    })
    expect(screen.getByText('10.0.0.1')).toBeInTheDocument()
  })
})

// =============================================================================
// Power state indicators
// =============================================================================

describe('FleetGrid power state indicators', () => {
  it('shows green indicator for powered-on hosts', async () => {
    mockFleetApi.list.mockResolvedValueOnce({
      hosts: [createHost({ power_state: 'on' })],
    })

    render(<FleetGrid />)

    await waitFor(() => {
      const indicator = document.querySelector('.bg-green-400')
      expect(indicator).toBeTruthy()
    })
  })

  it('shows red indicator for powered-off hosts', async () => {
    mockFleetApi.list.mockResolvedValueOnce({
      hosts: [createHost({ power_state: 'off' })],
    })

    render(<FleetGrid />)

    await waitFor(() => {
      const indicator = document.querySelector('.bg-red-400')
      expect(indicator).toBeTruthy()
    })
  })
})
