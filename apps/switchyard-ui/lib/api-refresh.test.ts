import { waitFor } from '@testing-library/react';
let mockAuthMode = 'oidc';
jest.mock('@/lib/constants', () => ({
  API_BASE_URL: 'https://api.example.test',
  JANUA_BASE_URL: 'https://identity.example.test',
  OAUTH_CLIENT_ID: 'public-browser-client',
  get AUTH_MODE() { return mockAuthMode; },
}));

import { attemptTokenRefresh, apiRequest, apiFetchResponse, __resetCSRFForTesting, getAuthHeadersRecord } from './api';
import { clearStorage, getStoredTokens, setStoredTokens, SESSION_CHANGED } from './auth-session';

const reply = (body: unknown, status = 200) => ({ ok: status < 400, status, json: async () => body }) as Response;
let generation = 0;
const tokens = () => ({ sessionId: `fixture-session-${generation}`, accessToken: 'old-access', refreshToken: 'old-refresh', expiresAt: Date.now() + 60_000 });
const rotation = { access_token: 'new-access', refresh_token: 'new-refresh', expires_in: 3600 };

beforeEach(() => {
  ++generation;
  __resetCSRFForTesting();
  mockAuthMode = 'oidc';
  localStorage.clear();
  localStorage.setItem('enclii_tokens', JSON.stringify(tokens()));
  global.fetch = jest.fn();
});

afterEach(() => jest.restoreAllMocks());

it('uses the public Janua form grant and commits rotated tokens, expiry, cookie and event together', async () => {
  const onChange = jest.fn();
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
  expect(onChange).toHaveBeenCalledTimes(2);
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
  else localStorage.setItem('enclii_tokens', JSON.stringify({ ...tokens(), sessionId: 'replacement', accessToken: 'other-session' }));
  resolve(reply(rotation));
  expect(await pending).toBe(false);
  expect(getStoredTokens()?.accessToken ?? null).toBe(change === 'logout' ? null : 'other-session');
});

it.each([
  { ...rotation, access_token: '' }, { ...rotation, expires_in: undefined },
  { ...rotation, expires_in: 0 }, { ...rotation, expires_in: -1 },
  { ...rotation, expires_in: '3600' }, { ...rotation, refresh_token: null },
])('does not publish malformed token responses (%j)', async (data) => {
  jest.mocked(fetch).mockResolvedValue(reply(data));
  expect(await attemptTokenRefresh()).toBe(false);
  expect(getStoredTokens()).toBeNull();
  expect(await attemptTokenRefresh()).toBe(false);
  expect(fetch).toHaveBeenCalledTimes(1);
});

it('preserves credentials during an issuer outage, including an expired access token with a recoverable refresh token', async () => {
  const expired = { ...tokens(), expiresAt: Date.now() - 1000 };
  localStorage.setItem('enclii_tokens', JSON.stringify(expired));
  jest.mocked(fetch).mockResolvedValue(reply({}, 503));
  expect(await attemptTokenRefresh()).toBe(false);
  expect(getStoredTokens()).toEqual({ ...expired, refreshAttempted: true });
  expect(await attemptTokenRefresh()).toBe(false);
  expect(fetch).toHaveBeenCalledTimes(1);
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
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
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


it.each(['POST', 'PUT'])('does not replay a delayed %s from account A under account B', async (method) => {
  jest.spyOn(console, 'error').mockImplementation(() => {});
  let finish!: (value: Response) => void;
  jest.mocked(fetch).mockImplementation((url) => String(url).endsWith('/csrf')
    ? Promise.resolve({ ...reply({}), headers: new Headers({ 'X-CSRF-Token': 'fixture-csrf' }) })
    : new Promise((done) => { finish = done; }));
  const request = apiRequest('/v1/projects', { method, body: '{"name":"fixture-write"}' });
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  await setStoredTokens({ accessToken: 'account-b-access', expiresAt: Date.now() + 60_000 });
  finish(reply({}, 401));
  await expect(request).rejects.toThrow('Session changed');
  const writes = jest.mocked(fetch).mock.calls.filter(([url]) => String(url).endsWith('/projects'));
  expect(writes).toHaveLength(1);
  expect(writes[0][1]?.headers).toMatchObject({ Authorization: 'Bearer old-access' });
  expect(getStoredTokens()?.accessToken).toBe('account-b-access');
});

it('never retries a blob/stream request under a replacement login', async () => {
  let finish!: (value: Response) => void;
  jest.mocked(fetch).mockReturnValueOnce(new Promise((done) => { finish = done; }));
  const request = apiFetchResponse('/v1/export');
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
  await setStoredTokens({ accessToken: 'account-b-access', expiresAt: Date.now() + 60_000 });
  finish(reply({}, 401));
  expect((await request).status).toBe(401);
  expect(fetch).toHaveBeenCalledTimes(1);
});

it('does not send an account A write when the account changes while CSRF is pending', async () => {
  let finish!: (value: Response) => void;
  jest.mocked(fetch).mockReturnValueOnce(new Promise((done) => { finish = done; }));
  const request = apiRequest('/v1/projects', { method: 'POST', body: '{}' });
  jest.spyOn(console, 'error').mockImplementation(() => {});
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
  await setStoredTokens({ accessToken: 'account-b-access', expiresAt: Date.now() + 60_000 });
  finish({ ...reply({}), headers: new Headers({ 'X-CSRF-Token': 'fixture-csrf' }) });
  await expect(request).rejects.toThrow('Session changed');
  expect(fetch).toHaveBeenCalledTimes(1);
});

it('does not adopt a replacement account after waiting for the refresh lock', async () => {
  let enter!: () => Promise<boolean>;
  let release!: (value: boolean) => void;
  const original = Object.getOwnPropertyDescriptor(navigator, 'locks');
  Object.defineProperty(navigator, 'locks', { configurable: true, value: { request: jest.fn((_name, run) => {
    enter = run; return new Promise<boolean>((done) => { release = done; });
  }) } });
  try {
    const pending = attemptTokenRefresh();
    localStorage.setItem('enclii_tokens', JSON.stringify({ ...tokens(), sessionId: 'other-login', accessToken: 'other-access' }));
    release(await enter());
    expect(await pending).toBe(false);
    expect(fetch).not.toHaveBeenCalled();
  } finally {
    if (original) Object.defineProperty(navigator, 'locks', original);
    else Reflect.deleteProperty(navigator, 'locks');
  }
});

it.each(['throws', 'silently drops'])('quarantines a successful rotation when persistence %s, even when removal fails', async (failure) => {
  jest.mocked(fetch).mockImplementation(async () => {
    jest.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    if (failure === 'throws') throw new DOMException('Unavailable', 'QuotaExceededError');
    });
    jest.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new DOMException('Unavailable', 'SecurityError'); });
    return reply(rotation);
  });
  expect(await attemptTokenRefresh()).toBe(false);
  expect(await attemptTokenRefresh()).toBe(false);
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(getStoredTokens()).toBeNull();
  expect(getAuthHeadersRecord().Authorization).toBeUndefined();
});

it('denies refresh and bearer headers when storage reads fail', async () => {
  jest.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('Unavailable', 'SecurityError'); });
  expect(await attemptTokenRefresh()).toBe(false);
  expect(getAuthHeadersRecord().Authorization).toBeUndefined();
  expect(fetch).not.toHaveBeenCalled();
});


it('persists grant consumption across a lost response so timers and a reload cannot replay it', async () => {
  jest.mocked(fetch).mockRejectedValue(new TypeError('Response lost'));
  expect(await attemptTokenRefresh()).toBe(false);
  const persisted = JSON.parse(localStorage.getItem('enclii_tokens')!);
  expect(persisted.refreshAttempted).toBe(true);
  // Reload-equivalent state is only the stored record, not an in-flight promise.
  localStorage.setItem('enclii_tokens', JSON.stringify(persisted));
  expect(await attemptTokenRefresh()).toBe(false);
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(getAuthHeadersRecord().Authorization).toBe('Bearer old-access');
});

it('does not dispatch a grant when the durable attempt marker cannot be written', async () => {
  jest.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('Unavailable', 'QuotaExceededError'); });
  expect(await attemptTokenRefresh()).toBe(false);
  expect(fetch).not.toHaveBeenCalled();
  expect(getStoredTokens()).toBeNull();
});


it.each([undefined, 'old-refresh'])('never rearms a spent grant when the issuer omits or repeats its replacement (%s)', async (refreshToken) => {
  jest.mocked(fetch).mockResolvedValue(reply({ ...rotation, refresh_token: refreshToken }));
  expect(await attemptTokenRefresh()).toBe(true);
  expect(getStoredTokens()).toMatchObject({ accessToken: 'new-access', refreshToken: 'old-refresh', refreshAttempted: true });
  expect(await attemptTokenRefresh()).toBe(false);
  expect(fetch).toHaveBeenCalledTimes(1);
});
