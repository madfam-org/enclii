/**
 * Tests for components/domain-matrix/index.tsx
 *
 * Tests the DomainMatrix component which displays Cloudflare zones
 * in a data table with commission dialog and refresh functionality.
 */

import React from 'react'
import { render, screen, waitFor, act } from '@testing-library/react'

// Mock lucide-react icons (must include all icons used by DomainMatrix + its children)
jest.mock('lucide-react', () => {
  const icon = (name: string) => {
    const Component = (props: any) => <span data-testid={`icon-${name}`} {...props} />
    Component.displayName = name
    return Component
  }
  return {
    Plus: icon('plus'),
    RefreshCw: icon('refresh'),
    Globe: icon('globe'),
    Shield: icon('shield'),
    Server: icon('server'),
    MoreHorizontal: icon('more'),
    Copy: icon('copy'),
    ExternalLink: icon('external'),
    Settings: icon('settings'),
    Trash2: icon('trash'),
    Search: icon('search'),
    SlidersHorizontal: icon('sliders'),
    ChevronLeft: icon('chevron-left'),
    ChevronRight: icon('chevron-right'),
    X: icon('x'),
  }
})

// Mock the commission dialog to a no-op
jest.mock('@/components/domain-matrix/commission-dialog', () => ({
  CommissionDialog: ({ open }: { open: boolean }) =>
    open ? <div data-testid="commission-dialog">Commission Dialog</div> : null,
}))

// Mock the utils module (copyToClipboard and formatRelativeTime)
jest.mock('@/lib/utils', () => ({
  cn: (...args: unknown[]) => args.filter(Boolean).join(' '),
  copyToClipboard: jest.fn().mockResolvedValue(true),
  formatRelativeTime: jest.fn().mockReturnValue('2d ago'),
}))

import { DomainMatrix } from '@/components/domain-matrix'

// ---------------------------------------------------------------------------
// Global fetch mock
// ---------------------------------------------------------------------------

const mockFetch = jest.fn()
global.fetch = mockFetch

function mockDomainsResponse(domains: unknown[]) {
  mockFetch.mockResolvedValueOnce({
    ok: true,
    json: () => Promise.resolve({ success: true, data: domains }),
  })
}

function mockDomainsError(error: string) {
  mockFetch.mockResolvedValueOnce({
    ok: true,
    json: () => Promise.resolve({ success: false, error }),
  })
}

beforeEach(() => {
  mockFetch.mockReset()
})

// =============================================================================
// Header rendering
// =============================================================================

describe('DomainMatrix header', () => {
  it('renders the component title and description', async () => {
    mockDomainsResponse([])

    render(<DomainMatrix />)

    expect(screen.getByText('Domain Matrix')).toBeInTheDocument()
    expect(screen.getByText(/Manage Cloudflare zones/)).toBeInTheDocument()
  })

  it('renders refresh and commission buttons', async () => {
    mockDomainsResponse([])

    render(<DomainMatrix />)

    expect(screen.getByText('Refresh')).toBeInTheDocument()
    expect(screen.getByText('Commission Domain')).toBeInTheDocument()
  })
})

// =============================================================================
// Loading state
// =============================================================================

describe('DomainMatrix loading', () => {
  it('shows loading indicator in table while fetching', () => {
    // Never resolve to keep in loading state
    mockFetch.mockReturnValue(new Promise(() => {}))

    render(<DomainMatrix />)

    expect(screen.getByText('Loading domains...')).toBeInTheDocument()
  })
})

// =============================================================================
// Error state
// =============================================================================

describe('DomainMatrix error state', () => {
  it('shows error message from API response', async () => {
    mockDomainsError('CLOUDFLARE_API_TOKEN is not configured')

    render(<DomainMatrix />)

    await waitFor(() => {
      expect(screen.getByText('CLOUDFLARE_API_TOKEN is not configured')).toBeInTheDocument()
    })
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })

  it('shows error message on network failure', async () => {
    mockFetch.mockRejectedValueOnce(new Error('Network error'))

    render(<DomainMatrix />)

    await waitFor(() => {
      expect(screen.getByText('Network error')).toBeInTheDocument()
    })
  })
})

// =============================================================================
// Commission dialog
// =============================================================================

describe('DomainMatrix commission dialog', () => {
  it('opens commission dialog when button is clicked', async () => {
    mockDomainsResponse([])

    render(<DomainMatrix />)

    await waitFor(() => {
      expect(screen.queryByText('Loading domains...')).not.toBeInTheDocument()
    })

    await act(async () => {
      screen.getByText('Commission Domain').click()
    })

    expect(screen.getByTestId('commission-dialog')).toBeInTheDocument()
  })
})

// =============================================================================
// Empty table state
// =============================================================================

describe('DomainMatrix empty state', () => {
  it('shows "No domains found" when data is empty', async () => {
    mockDomainsResponse([])

    render(<DomainMatrix />)

    await waitFor(() => {
      expect(screen.getByText('No domains found.')).toBeInTheDocument()
    })
  })

  it('shows domain count as "0 domain(s) total"', async () => {
    mockDomainsResponse([])

    render(<DomainMatrix />)

    await waitFor(() => {
      expect(screen.getByText('0 domain(s) total')).toBeInTheDocument()
    })
  })
})
