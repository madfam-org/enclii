import { render, screen, waitFor } from '@testing-library/react';
import AuthCallbackPage from './page';
import { getStoredTokens } from '@/lib/auth-session';

jest.mock('next/navigation', () => ({ useSearchParams: () => new URLSearchParams('code=fixture-code') }));
jest.mock('@/lib/analytics/posthog', () => ({ identifyUser: jest.fn() }));
const reply = (body: unknown) => ({ ok: true, json: async () => body }) as Response;

afterEach(() => { jest.useRealTimers(); jest.restoreAllMocks(); });

it('publishes a new PKCE login generation after identity verification and server cookie establishment', async () => {
  localStorage.clear();
  localStorage.setItem('enclii_tokens', JSON.stringify({ sessionId: 'previous-login', accessToken: 'previous-access', expiresAt: Date.now() + 60_000 }));
  sessionStorage.setItem('enclii_code_verifier', 'fixture-verifier');
  jest.useFakeTimers();
  global.fetch = jest.fn().mockResolvedValueOnce(reply({ access_token: 'new-login-access', refresh_token: 'new-login-refresh', expires_in: 900 }))
    .mockResolvedValueOnce(reply({ id: 'fixture-user', email: 'fixture@example.test' }))
    .mockResolvedValueOnce(reply({ success: true }));
  render(<AuthCallbackPage />);
  await waitFor(() => expect(getStoredTokens()?.accessToken).toBe('new-login-access'));
  expect(getStoredTokens()?.sessionId).toBeTruthy();
  expect(getStoredTokens()?.sessionId).not.toBe('previous-login');
  expect(fetch).toHaveBeenNthCalledWith(2, expect.stringContaining('/api/v1/auth/me'), {
    headers: { Authorization: 'Bearer new-login-access' },
  });
  expect(fetch).toHaveBeenNthCalledWith(3, '/api/auth/session', expect.objectContaining({ method: 'POST' }));
  expect(screen.getByText(/success/i)).toBeInTheDocument();
});
