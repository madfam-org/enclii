/**
 * Tests for components/PostHogProvider.tsx
 *
 * Verifies the PostHogProvider component initializes PostHog on mount
 * and renders children correctly.
 */

import React from 'react'
import { render, screen } from '@testing-library/react'

// Mock next/navigation
jest.mock('next/navigation', () => ({
  usePathname: jest.fn(() => '/'),
  useSearchParams: jest.fn(() => new URLSearchParams()),
}))

// Mock PostHog
jest.mock('posthog-js', () => ({
  __loaded: false,
  capture: jest.fn(),
}))

// Mock the posthog init module
jest.mock('@/lib/analytics/posthog', () => ({
  initPostHog: jest.fn(),
}))

import { PostHogProvider } from '@/components/PostHogProvider'
import { initPostHog } from '@/lib/analytics/posthog'

beforeEach(() => {
  jest.clearAllMocks()
})

describe('PostHogProvider', () => {
  it('renders children', () => {
    render(
      <PostHogProvider>
        <div data-testid="child">Hello</div>
      </PostHogProvider>
    )
    expect(screen.getByTestId('child')).toBeInTheDocument()
    expect(screen.getByText('Hello')).toBeInTheDocument()
  })

  it('calls initPostHog on mount', () => {
    render(
      <PostHogProvider>
        <span>Content</span>
      </PostHogProvider>
    )
    expect(initPostHog).toHaveBeenCalledTimes(1)
  })

  it('does not break rendering when PostHog is not loaded', () => {
    render(
      <PostHogProvider>
        <p>Safe render</p>
      </PostHogProvider>
    )
    expect(screen.getByText('Safe render')).toBeInTheDocument()
  })
})
