import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { ServiceList } from '@/components/ServiceGroup'
import { ThemeToggle } from '@/components/ThemeToggle'
import { SharedTooltip } from '@/components/SharedTooltip'
import { useTimelineBreakpoint } from '@/hooks/useTimelineBreakpoint'
import type { HealthCheckResult } from '@/lib/types'

jest.mock('@/components/ServiceCard', () => ({
  ServiceCard: ({ service }: { service: HealthCheckResult }) => <span>{service.service}</span>,
  ServiceCardCompact: ({ service }: { service: HealthCheckResult }) => <span>{service.service}</span>,
}))

const services: HealthCheckResult[] = [
  { service: 'Fixture API', group: 'Fixture Group', family: 'Fixture Family', url: 'https://api.example.test/health',
    status: 'operational', responseTime: 10, lastChecked: '2026-01-01T00:00:00Z' },
]

let container: HTMLDivElement
let root: Root

beforeEach(() => {
  ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  localStorage.clear()
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  jest.restoreAllMocks()
})

async function click(label: string) {
  const button = [...container.querySelectorAll('button')].find(node => node.textContent?.includes(label))
  expect(button).toBeDefined()
  await act(async () => button!.click())
}

test('switching none/group/status views keeps hook order and content valid', async () => {
  for (const groupBy of ['none', 'group', 'status', 'none', 'group'] as const) {
    await act(async () => root.render(<ServiceList services={services} groupBy={groupBy} />))
    expect(container.textContent).toContain('Fixture API')
  }
})

test('persisted group/family collapse and user toggles remain separate and durable', async () => {
  localStorage.setItem('status-groups-expanded', '[]')
  localStorage.setItem('status-families-expanded', '[]')
  await act(async () => root.render(<ServiceList services={services} />))
  expect(container.textContent).not.toContain('Fixture Group')
  await click('Fixture Family')
  expect(container.textContent).toContain('Fixture Group')
  expect(container.textContent).not.toContain('Fixture API')
  await click('Fixture Group')
  expect(container.textContent).toContain('Fixture API')
  expect(JSON.parse(localStorage.getItem('status-groups-expanded')!)).toEqual(['Fixture Group'])
  expect(JSON.parse(localStorage.getItem('status-families-expanded')!)).toEqual(['Fixture Family'])
})

test('malformed or unavailable storage preserves usable default expansion and toggles', async () => {
  localStorage.setItem('status-groups-expanded', '{invalid')
  localStorage.setItem('status-families-expanded', '{invalid')
  await act(async () => root.render(<ServiceList services={services} />))
  expect(container.textContent).toContain('Fixture API')
  jest.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('fixture storage denied') })
  await click('Fixture Group')
  expect(container.textContent).not.toContain('Fixture API')
  await click('Fixture Group')
  expect(container.textContent).toContain('Fixture API')
})

test('theme uses stored choice and toggles document theme even if storage writes fail', async () => {
  localStorage.setItem('theme', 'light')
  await act(async () => root.render(<ThemeToggle />))
  const button = container.querySelector('button')!
  expect(button.getAttribute('aria-label')).toBe('Switch to dark theme')
  jest.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('fixture storage denied') })
  await act(async () => button.click())
  expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
  expect(button.getAttribute('aria-label')).toBe('Switch to light theme')
})

test('an observed storage theme change updates both the label and document theme', async () => {
  localStorage.setItem('theme', 'light')
  await act(async () => root.render(<ThemeToggle />))
  expect(document.documentElement.getAttribute('data-theme')).toBe('light')
  await act(async () => {
    localStorage.setItem('theme', 'dark')
    window.dispatchEvent(new StorageEvent('storage', { key: 'theme', newValue: 'dark' }))
  })
  expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
  expect(container.querySelector('button')!.getAttribute('aria-label')).toBe('Switch to light theme')
})

test('responsive timeline subscribes to threshold changes and removes all listeners', async () => {
  const matches = new Set(['(min-width: 768px)'])
  const listeners = new Map<string, Set<() => void>>()
  jest.spyOn(window, 'matchMedia').mockImplementation(query => ({
    get matches() { return matches.has(query) }, media: query, onchange: null,
    addEventListener: (_event: string, callback: () => void) => {
      if (!listeners.has(query)) listeners.set(query, new Set())
      listeners.get(query)!.add(callback)
    },
    removeEventListener: (_event: string, callback: () => void) => listeners.get(query)?.delete(callback),
    addListener: jest.fn(), removeListener: jest.fn(), dispatchEvent: jest.fn(),
  } as unknown as MediaQueryList))
  function Probe() { return <output>{useTimelineBreakpoint().windowMinutes}</output> }
  await act(async () => root.render(<Probe />))
  expect(container.textContent).toBe('15')
  await act(async () => {
    matches.add('(min-width: 1280px)')
    for (const listener of listeners.get('(min-width: 1280px)')!) listener()
  })
  expect(container.textContent).toBe('5')
  await act(async () => root.render(null))
  expect([...listeners.values()].every(callbacks => callbacks.size === 0)).toBe(true)
})

test('hydrated tooltip portals to the body and cleans up', async () => {
  await act(async () => root.render(<SharedTooltip visible anchor={{ x: 40, y: 80 }} slot={{
    start: '2026-01-01T00:00:00Z', end: '2026-01-01T00:05:00Z',
    status: 'operational', checks: 3, avgResponseMs: 20,
  }} />))
  expect(document.body.textContent).toContain('3 checks')
  await act(async () => root.render(null))
  expect(document.body.textContent).not.toContain('3 checks')
})
