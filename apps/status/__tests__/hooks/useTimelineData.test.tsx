import { act, useLayoutEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { useTimelineData } from '@/hooks/useTimelineData'
import type { TimelineResponse } from '@/lib/types'

let container: HTMLDivElement
let root: Root
let snapshot: ReturnType<typeof useTimelineData>
let fetchMock: jest.Mock
const originalFetch = global.fetch
const originalVisibility = Object.getOwnPropertyDescriptor(document, 'visibilityState')

function Probe({ minutes }: { minutes: number }) {
  const value = useTimelineData(minutes)
  useLayoutEffect(() => { snapshot = value }, [value])
  return <output>{value.data?.windowMinutes ?? 'empty'}</output>
}
function result(minutes: number): TimelineResponse {
  return { services: [], from: '2026-01-01T00:00:00Z', to: '2026-01-02T00:00:00Z', windowMinutes: minutes }
}
function reply(minutes: number) { return { ok: true, json: async () => result(minutes) } }

beforeEach(() => {
  ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  jest.useFakeTimers()
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  fetchMock = jest.fn()
  global.fetch = fetchMock
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  global.fetch = originalFetch
  jest.useRealTimers()
  if (originalVisibility) Object.defineProperty(document, 'visibilityState', originalVisibility)
  else delete (document as unknown as { visibilityState?: string }).visibilityState
})

test('first settlement ends initial loading, while refetch keeps stale data without a skeleton', async () => {
  let finish: (value: ReturnType<typeof reply>) => void = () => {}
  fetchMock.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  await act(async () => root.render(<Probe minutes={5} />))
  expect(snapshot.loading).toBe(true)
  expect(snapshot.isInitialLoad).toBe(true)
  await act(async () => finish(reply(5)))
  expect(snapshot.loading).toBe(false)
  expect(snapshot.isInitialLoad).toBe(false)
  expect(snapshot.data?.windowMinutes).toBe(5)
  fetchMock.mockRejectedValueOnce(new Error('fixture refresh unavailable'))
  await act(async () => jest.advanceTimersByTime(60_000))
  expect(snapshot.data?.windowMinutes).toBe(5)
  expect(snapshot.loading).toBe(false)
  expect(snapshot.error).toBe('fixture refresh unavailable')
})

test('obsolete breakpoint response cannot replace the newer window', async () => {
  let finishOld: (value: ReturnType<typeof reply>) => void = () => {}
  fetchMock.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
  await act(async () => root.render(<Probe minutes={60} />))
  fetchMock.mockResolvedValueOnce(reply(5))
  await act(async () => root.render(<Probe minutes={5} />))
  expect(snapshot.data?.windowMinutes).toBe(5)
  await act(async () => finishOld(reply(60)))
  expect(snapshot.data?.windowMinutes).toBe(5)
  expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
    '/api/status/timeline?hours=24&window=60', '/api/status/timeline?hours=24&window=5',
  ])
})

test('hidden tab pauses polling; focus refreshes and unmount clears its timer', async () => {
  fetchMock.mockResolvedValue(reply(5))
  await act(async () => root.render(<Probe minutes={5} />))
  await act(async () => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    jest.advanceTimersByTime(120_000)
  })
  expect(fetchMock).toHaveBeenCalledTimes(1)
  await act(async () => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  expect(fetchMock).toHaveBeenCalledTimes(2)
  await act(async () => root.render(null))
  expect(jest.getTimerCount()).toBe(0)
})

test('initial HTTP failure is visible and stops initial loading', async () => {
  fetchMock.mockResolvedValueOnce({ ok: false, status: 503 })
  await act(async () => root.render(<Probe minutes={5} />))
  expect(snapshot.data).toBeNull()
  expect(snapshot.error).toBe('HTTP 503')
  expect(snapshot.loading).toBe(false)
})
