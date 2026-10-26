/** @jest-environment node */
import { renderToString } from 'react-dom/server'
import { ThemeToggle } from '@/components/ThemeToggle'
import { SharedTooltip } from '@/components/SharedTooltip'
import { useTimelineBreakpoint } from '@/hooks/useTimelineBreakpoint'

test('server placeholders do not access browser state or create a tooltip portal', () => {
  expect(renderToString(<ThemeToggle />)).toContain('aria-hidden="true"')
  expect(renderToString(<SharedTooltip visible anchor={{ x: 40, y: 80 }} slot={{
    start: '2026-01-01T00:00:00Z', end: '2026-01-01T00:05:00Z',
    status: 'operational', checks: 3, avgResponseMs: 20,
  }} />)).toBe('')
  function Probe() { return <output>{useTimelineBreakpoint().windowMinutes}</output> }
  expect(renderToString(<Probe />)).toBe('<output>60</output>')
})
