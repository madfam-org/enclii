import React from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { TopologyCanvas } from '@/components/topology/topology-canvas'
import { fetchTopology, TopologyFetchError, type TopologyResponse } from '@/lib/topology-api'

jest.mock('@/lib/topology-api', () => ({
  ...jest.requireActual('@/lib/topology-api'), fetchTopology: jest.fn(),
}))
jest.mock('@xyflow/react', () => ({
  ReactFlow: ({ nodes }: { nodes: { id: string; data: { label: React.ReactNode } }[] }) => (
    <div>{nodes.map((node) => <div key={node.id}>{node.data.label}</div>)}</div>
  ),
  Background: () => null, Controls: () => null,
}))
jest.mock('@/components/topology/topology-drawers', () => ({
  TopologyNodeDrawer: () => null, TopologyPodDrawer: () => null,
}))
const mockFetch = jest.mocked(fetchTopology)
const snapshot: TopologyResponse = {
  nodes: [{
    name: 'node-a', role: 'builder', status: 'Ready', conditions: [], taints: [],
    used: { cpu_millicores: 500, memory_bytes: 256, pod_count: 1 },
    allocatable: { cpu_millicores: 1000, memory_bytes: 512, pods: 10 },
    capacity: { cpu_millicores: 1000, memory_bytes: 512, pods: 10 },
  }],
  namespaces: [{ name: 'tenant-a', pod_count: 1, deployment_count: 1, service_count: 0, pod_phases: { Pending: 1 } }],
  pods: [{ name: 'pending-a', namespace: 'tenant-a', node: '', phase: 'Pending', ready: '0/1', restart_count: 0, age_seconds: 30, containers: [] }],
  services: [], synced_at: '2026-10-22T12:00:00Z',
  totals: { nodes: 1, pods: 1, namespaces: 1, services: 0, cpu_capacity_millicores: 1000, cpu_used_millicores: 500, memory_capacity_bytes: 512, memory_used_bytes: 256 },
  display: { cpu_capacity: '1 core', cpu_used: '500m', memory_capacity: '512 B', memory_used: '256 B' },
}
beforeEach(() => { mockFetch.mockReset() })

it('shows builder roles and explains that requests are not utilization', async () => {
  mockFetch.mockResolvedValue(snapshot)
  render(<TopologyCanvas />)
  expect(await screen.findByText('node-a')).toBeInTheDocument()
  expect(screen.getByText(/builder/)).toBeInTheDocument()
  expect(screen.getByText('CPU requests')).toBeInTheDocument()
  expect(screen.getByText(/not live utilization/)).toBeInTheDocument()
  expect(screen.getByRole('region', { name: 'Unscheduled pods' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'tenant-a/pending-a' })).toBeInTheDocument()
  expect(screen.getByText('1 pending pods awaiting node assignment')).toBeInTheDocument()
})
it.each([401, 403])('discards cached inventory after access refusal %s', async (status) => {
  mockFetch.mockResolvedValueOnce(snapshot).mockRejectedValueOnce(new TopologyFetchError('Access refused', status))
  render(<TopologyCanvas />)
  await screen.findByText('node-a')
  fireEvent.click(screen.getByRole('button', { name: 'Refresh topology' }))
  await screen.findByText('Topology unavailable')
  expect(screen.queryByText('node-a')).not.toBeInTheDocument()
  expect(screen.queryByText(/Showing cached data/)).not.toBeInTheDocument()
})
it('marks retained inventory stale on an upstream outage and allows refresh', async () => {
  mockFetch.mockResolvedValueOnce(snapshot).mockRejectedValueOnce(new TopologyFetchError('Unavailable', 502)).mockResolvedValueOnce(snapshot)
  render(<TopologyCanvas />)
  await screen.findByText('node-a')
  fireEvent.click(screen.getByRole('button', { name: 'Refresh topology' }))
  expect(await screen.findByText(/Showing cached data/)).toBeInTheDocument()
  expect(screen.getByText('node-a')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Refresh topology' }))
  await waitFor(() => expect(screen.queryByText(/Showing cached data/)).not.toBeInTheDocument())
})
