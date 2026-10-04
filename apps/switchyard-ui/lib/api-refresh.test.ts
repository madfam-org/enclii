let mockAuthMode = 'oidc';
jest.mock('@/lib/constants', () => ({
  API_BASE_URL: 'https://api.example.test',
  JANUA_BASE_URL: 'https://identity.example.test',
  OAUTH_CLIENT_ID: 'public-browser-client',
  get AUTH_MODE() { return mockAuthMode; },
}));

import { attemptTokenRefresh, apiRequest } from './api';
import { clearStorage, getStoredTokens, SESSION_CHANGED } from './auth-session';

const reply = (body: unknown, status = 200) => ({ ok: status < 400, status, json: async () => body }) as Response;
const tokens = () => ({ accessToken: 'old-access', refreshToken: 'old-refresh', expiresAt: Date.now() + 60_000 });
const rotation = { access_token: 'new-access', refresh_token: 'new-refresh', expires_in: 3600 };

beforeEach(() => {
  mockAuthMode = 'oidc';
  localStorage.clear();
  localStorage.setItem('enclii_tokens', JSON.stringify(tokens()));
  global.fetch = jest.fn();
});

afterEach(() => jest.restoreAllMocks());

it('uses the public Janua form grant and commits rotated tokens, expiry, cookie and event together', async () => {
  const onChange = jest.fn(() => expect(getStoredTokens()?.refreshToken).toBe('new-refresh'));
  window.addEventListener(SESSION_CHANGED, onChange);
  jest.mocked(fetch).mockResolvedValue(reply(rotation));
  expect(await attemptTokenRefresh()).toBe(true);
  const [url, options] = jest.mocked(fetch).mock.calls[0];
  expect(url).toBe('https://identity.example.test/api/v1/oauth/token');
  expect(options?.headers).toEqual({ 'Content-Type': 'application/x-www-form-urlencoded' });
  expect(Object.fromEntries(new URLSearchParams(options?.body as string))).toEqual({
    grant_type: 'refresh_token', client_id: 'public-browser-client', refresh_token: 'old-refresh',
  });
  expect(options?.credentials).toBeUndefined();
  expect(getStoredTokens()?.accessToken).toBe('new-access');
  expect(getStoredTokens()?.expiresAt).toBeGreaterThan(Date.now() + 3_590_000);
  expect(document.cookie).toContain('enclii_auth=new-access');
  expect(onChange).toHaveBeenCalledTimes(1);
  window.removeEventListener(SESSION_CHANGED, onChange);
});

it('retains the local Switchyard JSON refresh contract', async () => {
  mockAuthMode = 'local';
  const expiresAt = Date.now() + 3600_000;
  jest.mocked(fetch).mockResolvedValue(reply({ access_token: 'local-access', expires_at: new Date(expiresAt).toISOString() }));
  expect(await attemptTokenRefresh()).toBe(true);
  expect(fetch).toHaveBeenCalledWith('https://api.example.test/v1/auth/refresh', expect.objectContaining({
    body: JSON.stringify({ refresh_token: 'old-refresh' }), credentials: 'include',
  }));
  expect(getStoredTokens()).toMatchObject({ accessToken: 'local-access', refreshToken: 'old-refresh', expiresAt });
});

it('shares one in-flight rotation between callers', async () => {
  let resolve!: (response: Response) => void;
  jest.mocked(fetch).mockReturnValue(new Promise((done) => { resolve = done; }));
  const first = attemptTokenRefresh();
  const second = attemptTokenRefresh();
  expect(fetch).toHaveBeenCalledTimes(1);
  resolve(reply(rotation));
  expect(await Promise.all([first, second])).toEqual([true, true]);
});

it.each(['logout', 'new-login'])('does not resurrect or overwrite a session after %s while refreshing', async (change) => {
  let resolve!: (response: Response) => void;
  jest.mocked(fetch).mockReturnValue(new Promise((done) => { resolve = done; }));
  const pending = attemptTokenRefresh();
  if (change === 'logout') clearStorage();
  else localStorage.setItem('enclii_tokens', JSON.stringify({ ...tokens(), accessToken: 'other-session' }));
  resolve(reply(rotation));
  expect(await pending).toBe(false);
  expect(getStoredTokens()?.accessToken ?? null).toBe(change === 'logout' ? null : 'other-session');
});

it.each([
  { ...rotation, access_token: '' }, { ...rotation, expires_in: undefined },
  { ...rotation, expires_in: 0 }, { ...rotation, expires_in: -1 },
  { ...rotation, expires_in: '3600' }, { ...rotation, refresh_token: null },
])('does not publish malformed token responses (%j)', async (data) => {
  const before = localStorage.getItem('enclii_tokens');
  jest.mocked(fetch).mockResolvedValue(reply(data));
  expect(await attemptTokenRefresh()).toBe(false);
  expect(localStorage.getItem('enclii_tokens')).toBe(before);
});

it('preserves credentials during an issuer outage, including an expired access token with a recoverable refresh token', async () => {
  const expired = { ...tokens(), expiresAt: Date.now() - 1000 };
  localStorage.setItem('enclii_tokens', JSON.stringify(expired));
  jest.mocked(fetch).mockResolvedValue(reply({}, 503));
  expect(await attemptTokenRefresh()).toBe(false);
  expect(getStoredTokens()).toEqual(expired);
});

it('clears an expired session on a definitive invalid_grant', async () => {
  localStorage.setItem('enclii_tokens', JSON.stringify({ ...tokens(), expiresAt: Date.now() - 1000 }));
  jest.mocked(fetch).mockResolvedValue(reply({ detail: 'invalid_grant: Invalid refresh token' }, 400));
  expect(await attemptTokenRefresh()).toBe(false);
  expect(getStoredTokens()).toBeNull();
});

it('retries an unauthorized request with the newly rotated bearer token', async () => {
  jest.mocked(fetch).mockResolvedValueOnce(reply({}, 401)).mockResolvedValueOnce(reply(rotation)).mockResolvedValueOnce(reply({ projects: [] }));
  await expect(apiRequest('/v1/projects')).resolves.toEqual({ projects: [] });
  expect(jest.mocked(fetch).mock.calls[2][1]?.headers).toMatchObject({ Authorization: 'Bearer new-access' });
});

it('reuses an already refreshed token when an older request returns a late 401', async () => {
  let finishOld!: (response: Response) => void;
  jest.mocked(fetch).mockReturnValueOnce(new Promise((done) => { finishOld = done; }))
    .mockResolvedValueOnce(reply(rotation)).mockResolvedValueOnce(reply({ projects: [] }));
  const request = apiRequest('/v1/projects');
  await attemptTokenRefresh();
  finishOld(reply({}, 401));
  await expect(request).resolves.toEqual({ projects: [] });
  expect(jest.mocked(fetch).mock.calls.filter(([url]) => String(url).includes('/oauth/token'))).toHaveLength(1);
});

it('waits for the browser refresh lock and adopts another tab rotation without spending its old refresh token', async () => {
  let enterLock!: () => Promise<boolean>;
  let release!: (value: boolean) => void;
  const original = Object.getOwnPropertyDescriptor(navigator, 'locks');
  const request = jest.fn((_name, callback) => {
    enterLock = callback;
    return new Promise<boolean>((resolve) => { release = resolve; });
  });
  Object.defineProperty(navigator, 'locks', { configurable: true, value: { request } });
  try {
    const pending = attemptTokenRefresh();
    localStorage.setItem('enclii_tokens', JSON.stringify({ ...tokens(), accessToken: 'another-tab-access', refreshToken: 'another-tab-refresh' }));
    release(await enterLock());
    expect(await pending).toBe(true);
    expect(fetch).not.toHaveBeenCalled();
    expect(getStoredTokens()?.refreshToken).toBe('another-tab-refresh');
  } finally {
    if (original) Object.defineProperty(navigator, 'locks', original);
    else Reflect.deleteProperty(navigator, 'locks');
  }
});
