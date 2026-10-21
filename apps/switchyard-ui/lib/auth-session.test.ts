import { captureSession, clearStorage, getStoredTokens, persistTokens, sameSession, setStoredTokens, withSessionLock } from './auth-session';

const credentials = () => ({ accessToken: 'fixture-access', refreshToken: 'fixture-refresh', expiresAt: Date.now() + 60_000 });
beforeEach(() => localStorage.clear());
afterEach(() => jest.restoreAllMocks());

it('retains a durable generation through rotation but assigns every sign-in a new generation', async () => {
  const first = await setStoredTokens(credentials());
  await withSessionLock(() => persistTokens({ ...first, accessToken: 'rotated-access' }, first));
  expect(getStoredTokens()?.sessionId).toBe(first.sessionId);
  const second = await setStoredTokens(credentials());
  expect(second.sessionId).not.toBe(first.sessionId);
  expect(sameSession(first, second)).toBe(false);
});

it('migrates legacy credentials once and rereads the durable generation', async () => {
  localStorage.setItem('enclii_tokens', JSON.stringify(credentials()));
  const first = await captureSession();
  const second = await captureSession();
  expect(first?.sessionId).toBeTruthy();
  expect(second?.sessionId).toBe(first?.sessionId);
});

it('does not remove a newer login during delayed cleanup of the previous generation', async () => {
  const first = await setStoredTokens(credentials());
  const second = await setStoredTokens({ ...credentials(), accessToken: 'replacement-access' });
  expect(await clearStorage(first)).toBe(false);
  expect(getStoredTokens()).toEqual(second);
});

it('serializes login cookie publication and cleanup using the same browser lock', async () => {
  const original = Object.getOwnPropertyDescriptor(navigator, 'locks');
  let queue: Promise<unknown> = Promise.resolve();
  const request = jest.fn((_name, run) => {
    const pending = queue.then(run);
    queue = pending.catch(() => {});
    return pending;
  });
  Object.defineProperty(navigator, 'locks', { configurable: true, value: { request } });
  try {
    const first = await setStoredTokens(credentials());
    let finish!: () => void;
    const establishing = new Promise<void>((done) => { finish = done; });
    const second = setStoredTokens({ ...credentials(), accessToken: 'replacement-access' }, () => establishing);
    const cleanup = clearStorage(first);
    finish();
    const replacement = await second;
    expect(await cleanup).toBe(false);
    expect(getStoredTokens()).toEqual(replacement);
    expect(request.mock.calls.every(([name]) => name === 'enclii-token-refresh')).toBe(true);
  } finally {
    if (original) Object.defineProperty(navigator, 'locks', original);
    else Reflect.deleteProperty(navigator, 'locks');
  }
});
