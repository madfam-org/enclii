'use strict';

/**
 * Offline test harness for verdaccio-auth-janua.
 *
 * Runs the plugin's authenticate/allow_access/allow_publish against a real
 * local HTTP server that mimics Janua's POST /api/v1/api-keys/verify contract
 * (verified live 2026-09-07: 200 {valid, org_id, scopes, key_id}).
 *
 * No network, no cluster, no dependencies:
 *   node infra/k8s/base/verdaccio/plugins/verdaccio-auth-janua/test/plugin.test.js
 */

const assert = require('assert');
const http = require('http');
const createPlugin = require('../index.js');

const KEY_PUBLISH = 'jnk_publisher_key';
const KEY_INSTALL = 'jnk_installer_key';
const KEY_NOSCOPE = 'jnk_no_scope_key';
const KEY_INVALID = 'jnk_revoked_key';
const KEY_OTHER_ORG = 'jnk_other_org_publisher';
// The format POST /api/v1/api-keys mints today: sk_live_ + 64 hex characters.
const KEY_SKLIVE_INSTALL = `sk_live_${'a1'.repeat(32)}`;
const KEY_SKLIVE_PUBLISH = `sk_live_${'b2'.repeat(32)}`;

// Mirrors Janua's real records: key -> {org_id, scopes, key_id}
const KEYS = {
  [KEY_SKLIVE_INSTALL]: {
    org_id: 'org-madfam',
    scopes: ['npm:install'],
    key_id: 'key-sklive-install-1',
  },
  [KEY_SKLIVE_PUBLISH]: {
    org_id: 'org-madfam',
    scopes: ['npm:install', 'npm:publish'],
    key_id: 'key-sklive-publish-1',
  },
  [KEY_PUBLISH]: {
    org_id: 'org-madfam',
    scopes: ['npm:install', 'npm:publish'],
    key_id: 'key-publish-1',
  },
  [KEY_INSTALL]: {
    org_id: 'org-madfam',
    scopes: ['npm:install'],
    key_id: 'key-install-1',
  },
  [KEY_NOSCOPE]: {
    org_id: 'org-madfam',
    scopes: ['read:users'],
    key_id: 'key-noscope-1',
  },
  // A key minted in some other Janua organisation, with registry scopes.
  [KEY_OTHER_ORG]: {
    org_id: 'org-other',
    scopes: ['npm:install', 'npm:publish'],
    key_id: 'key-other-1',
  },
};

// The marker groups the plugin adds to every verified key of org-madfam.
const JANUA = ['janua:key', 'janua:org:org-madfam'];

const silentLogger = {
  info() {}, warn() {}, error() {}, debug() {}, trace() {},
};

let verifyCallCount = 0;
let mockJanuaUrl = null; // set once the mock server listens

function startMockJanua() {
  return new Promise((resolve) => {
    const server = http.createServer((req, res) => {
      if (req.method !== 'POST' || req.url !== '/api/v1/api-keys/verify') {
        res.writeHead(404, { 'Content-Type': 'application/json' });
        return res.end('{"detail":"Not Found"}');
      }

      let body = '';
      req.on('data', (c) => { body += c; });
      req.on('end', () => {
        verifyCallCount += 1;
        let parsed;
        try {
          parsed = JSON.parse(body);
        } catch (e) {
          res.writeHead(422, { 'Content-Type': 'application/json' });
          return res.end('{"detail":"invalid"}');
        }

        const record = KEYS[parsed.key];
        res.writeHead(200, { 'Content-Type': 'application/json' });

        if (!record) {
          // Janua returns valid=false (NOT an HTTP error) for a bad key.
          return res.end(
            JSON.stringify({ valid: false, org_id: null, scopes: [], key_id: null })
          );
        }

        return res.end(
          JSON.stringify({
            valid: true,
            org_id: record.org_id,
            scopes: record.scopes,
            key_id: record.key_id,
          })
        );
      });
    });

    server.listen(0, '127.0.0.1', () => resolve(server));
  });
}

const authenticate = (plugin, user, password) =>
  new Promise((resolve, reject) => {
    plugin.authenticate(user, password, (err, groups) => {
      if (err) return reject(err);
      resolve(groups);
    });
  });

const allowAccess = (plugin, user, pkg) =>
  new Promise((resolve) => {
    plugin.allow_access(user, pkg, (err, ok) => resolve({ err, ok }));
  });

const allowPublish = (plugin, user, pkg) =>
  new Promise((resolve) => {
    plugin.allow_publish(user, pkg, (err, ok) => resolve({ err, ok }));
  });

// Verdaccio builds the remote user itself: createRemoteUser(username, groups).
const remoteUser = (name, groups) => ({ name, groups, real_groups: groups });
// A remote user as Verdaccio builds it from a verified org-madfam key.
const januaUser = (name, scopes, org = 'org-madfam') =>
  remoteUser(name, [...scopes, 'janua:key', `janua:org:${org}`, '$authenticated']);

const tests = [];
const test = (name, fn) => tests.push({ name, fn });

// ---------------------------------------------------------------------
// authenticate
// ---------------------------------------------------------------------

test('valid key with npm:publish authenticates and yields both scopes', async (p) => {
  const groups = await authenticate(p, 'anyuser', KEY_PUBLISH);
  assert.ok(Array.isArray(groups), 'groups must be an array');
  assert.ok(groups.includes('npm:install'));
  assert.ok(groups.includes('npm:publish'));
  assert.ok(groups.includes('$authenticated'));
});

test('groups never contain undefined (Janua has no owner/name field)', async (p) => {
  const groups = await authenticate(p, undefined, KEY_INSTALL);
  assert.ok(
    groups.every((g) => typeof g === 'string' && g.length > 0),
    `groups must all be non-empty strings, got ${JSON.stringify(groups)}`
  );
});

test('groups carry only npm:* scopes, the Janua markers and $authenticated', async (p) => {
  const groups = await authenticate(p, 'anyuser', KEY_NOSCOPE);
  assert.deepStrictEqual(groups, [...JANUA, '$authenticated']);
});

test('invalid key falls through (false), so htpasswd can try', async (p) => {
  const groups = await authenticate(p, 'anyuser', KEY_INVALID);
  assert.strictEqual(groups, false);
});

test('non-key password falls through WITHOUT calling Janua', async (p) => {
  const before = verifyCallCount;
  const groups = await authenticate(p, 'ci-service', 'a-plain-htpasswd-password');
  assert.strictEqual(groups, false);
  assert.strictEqual(verifyCallCount, before, 'Janua must not be called');
});

test('no password falls through (false)', async (p) => {
  const groups = await authenticate(p, 'anyuser', '');
  assert.strictEqual(groups, false);
});

test('current-format sk_live_ key authenticates (what Janua mints today)', async (p) => {
  const groups = await authenticate(p, 'janua', KEY_SKLIVE_INSTALL);
  assert.deepStrictEqual(groups, ['npm:install', ...JANUA, '$authenticated']);
});

test('current-format sk_live_ key with npm:publish yields both scopes', async (p) => {
  const groups = await authenticate(p, 'janua', KEY_SKLIVE_PUBLISH);
  assert.ok(groups.includes('npm:install'));
  assert.ok(groups.includes('npm:publish'));
});

test('a password with an unknown prefix falls through WITHOUT calling Janua', async (p) => {
  const before = verifyCallCount;
  const groups = await authenticate(p, 'ci-service', `pk_test_${'c3'.repeat(32)}`);
  assert.strictEqual(groups, false);
  assert.strictEqual(verifyCallCount, before, 'Janua must not be called');
});

test('no prefix setting: both formats are claimed (the default)', async () => {
  const plugin = createPlugin(
    { janua_url: mockJanuaUrl, cache_ttl_ms: 1000 },
    { logger: silentLogger }
  );
  assert.deepStrictEqual(plugin.keyPrefixes, ['jnk_', 'sk_live_']);
  assert.deepStrictEqual(
    await authenticate(plugin, 'janua', KEY_SKLIVE_INSTALL),
    ['npm:install', ...JANUA, '$authenticated']
  );
  assert.ok((await authenticate(plugin, 'janua', KEY_INSTALL)).includes('npm:install'));
});

test('the older key_prefix setting is still honoured exactly', async () => {
  const plugin = createPlugin(
    { janua_url: mockJanuaUrl, cache_ttl_ms: 1000, key_prefix: 'jnk_' },
    { logger: silentLogger }
  );
  const before = verifyCallCount;
  assert.strictEqual(await authenticate(plugin, 'janua', KEY_SKLIVE_INSTALL), false);
  assert.strictEqual(verifyCallCount, before, 'an unclaimed prefix never reaches Janua');
});

test('an empty prefix is ignored, so a plain password never reaches Janua', async () => {
  const plugin = createPlugin(
    { janua_url: mockJanuaUrl, cache_ttl_ms: 1000, key_prefixes: ['', 'sk_live_'] },
    { logger: silentLogger }
  );
  assert.deepStrictEqual(plugin.keyPrefixes, ['sk_live_']);
  const before = verifyCallCount;
  assert.strictEqual(await authenticate(plugin, 'ci-service', 'a-plain-htpasswd-password'), false);
  assert.strictEqual(verifyCallCount, before, 'Janua must not be called');
});

test('repeated auth with the same key is served from cache', async (p) => {
  await authenticate(p, 'anyuser', KEY_PUBLISH);
  const before = verifyCallCount;
  await authenticate(p, 'anyuser', KEY_PUBLISH);
  assert.strictEqual(verifyCallCount, before, 'second call must hit cache');
});

// ---------------------------------------------------------------------
// allow_access
// ---------------------------------------------------------------------

test('install scope allows access to a restricted package', async (p) => {
  const user = januaUser('janua-key:key-install-1', ['npm:install']);
  const { err, ok } = await allowAccess(p, user, {
    name: '@madfam/thing',
    access: ['$authenticated'],
  });
  assert.strictEqual(err, null);
  assert.strictEqual(ok, true);
});

test('missing npm:install denies access', async (p) => {
  const user = januaUser('janua-key:key-publish-1', ['npm:publish']);
  const { err } = await allowAccess(p, user, {
    name: '@madfam/thing',
    access: ['$authenticated'],
  });
  assert.ok(err instanceof Error);
  assert.match(err.message, /npm:install/);
});

test('$all package is accessible without any scope', async (p) => {
  const { err, ok } = await allowAccess(p, remoteUser(undefined, []), {
    name: '@janua/sdk',
    access: ['$all'],
  });
  assert.strictEqual(err, null);
  assert.strictEqual(ok, true);
});

test('htpasswd user (no npm:* groups) falls through to Verdaccio', async (p) => {
  const user = remoteUser('admin@madfam.io', ['$authenticated']);
  const { err, ok } = await allowAccess(p, user, {
    name: '@madfam/thing',
    access: ['$authenticated'],
  });
  assert.strictEqual(err, null);
  assert.strictEqual(ok, false, 'must fall through, not grant');
});

// ---------------------------------------------------------------------
// allow_publish
// ---------------------------------------------------------------------

test('publish scope allows publishing', async (p) => {
  const user = januaUser('janua-key:key-publish-1', ['npm:install', 'npm:publish']);
  const { err, ok } = await allowPublish(p, user, {
    name: '@madfam/thing',
    publish: ['$authenticated'],
  });
  assert.strictEqual(err, null);
  assert.strictEqual(ok, true);
});

test('install-only key is DENIED publish but allowed install', async (p) => {
  const user = januaUser('janua-key:key-install-1', ['npm:install']);

  const pub = await allowPublish(p, user, {
    name: '@madfam/thing',
    publish: ['$authenticated'],
  });
  assert.ok(pub.err instanceof Error);
  assert.match(pub.err.message, /npm:publish/);

  const acc = await allowAccess(p, user, {
    name: '@madfam/thing',
    access: ['$authenticated'],
  });
  assert.strictEqual(acc.err, null);
  assert.strictEqual(acc.ok, true);
});

test('htpasswd user falls through on publish too', async (p) => {
  const user = remoteUser('admin@madfam.io', ['$authenticated']);
  const { err, ok } = await allowPublish(p, user, {
    name: '@madfam/thing',
    publish: ['$authenticated'],
  });
  assert.strictEqual(err, null);
  assert.strictEqual(ok, false);
});

// ---------------------------------------------------------------------
// Janua users are always decided by the plugin (never by the package ACL)
// ---------------------------------------------------------------------

// What Verdaccio does: authenticate, then build the remote user from groups.
const userFromKey = async (p, key) => {
  const groups = await authenticate(p, 'janua', key);
  assert.ok(Array.isArray(groups), 'key must authenticate');
  return remoteUser('janua', groups);
};

test('verified key with no npm:* scope is DENIED publish (no fall-through)', async (p) => {
  const user = await userFromKey(p, KEY_NOSCOPE);
  const { err, ok } = await allowPublish(p, user, { name: '@madfam/thing', publish: ['$authenticated'] });
  assert.ok(err instanceof Error, 'must be an explicit denial');
  assert.notStrictEqual(ok, false, 'must not fall through to the $authenticated ACL');
  assert.match(err.message, /npm:publish/);
});

test('verified key with no npm:* scope is DENIED restricted access', async (p) => {
  const user = await userFromKey(p, KEY_NOSCOPE);
  const { err } = await allowAccess(p, user, { name: '@madfam/thing', access: ['$authenticated'] });
  assert.ok(err instanceof Error);
  assert.match(err.message, /npm:install/);
});

test('key from an org outside the allow-list is DENIED publish and access', async (p) => {
  const user = await userFromKey(p, KEY_OTHER_ORG);
  assert.ok(user.groups.includes('npm:publish'), 'fixture carries npm:publish');
  const pub = await allowPublish(p, user, { name: '@madfam/thing', publish: ['$authenticated'] });
  assert.ok(pub.err instanceof Error);
  assert.match(pub.err.message, /allowed Janua organisation/);
  const acc = await allowAccess(p, user, { name: '@madfam/thing', access: ['$authenticated'] });
  assert.ok(acc.err instanceof Error);
});

test('allowed org with npm:publish publishes end to end', async (p) => {
  const user = await userFromKey(p, KEY_SKLIVE_PUBLISH);
  const { err, ok } = await allowPublish(p, user, { name: '@madfam/thing', publish: ['$authenticated'] });
  assert.strictEqual(err, null);
  assert.strictEqual(ok, true);
});

test('$all packages stay readable for any verified key', async (p) => {
  const user = await userFromKey(p, KEY_OTHER_ORG);
  const { err, ok } = await allowAccess(p, user, { name: '@janua/sdk', access: ['$all'] });
  assert.strictEqual(err, null);
  assert.strictEqual(ok, true);
});

test('an npm:* group without the org marker is denied, not granted', async (p) => {
  // e.g. an htpasswd account whose NAME is "npm:publish" (htpasswd groups are
  // the username): it must never be treated as a publish-scoped Janua key.
  const user = remoteUser('npm:publish', ['npm:publish', '$authenticated']);
  const { err } = await allowPublish(p, user, { name: '@madfam/thing', publish: ['$authenticated'] });
  assert.ok(err instanceof Error);
});

test('no allowed org configured: every Janua key is refused', async () => {
  const plugin = createPlugin({ janua_url: mockJanuaUrl, cache_ttl_ms: 1000 }, { logger: silentLogger, env: {} });
  assert.deepStrictEqual(plugin.allowedOrgIds, []);
  const user = await userFromKey(plugin, KEY_PUBLISH);
  const { err } = await allowPublish(plugin, user, { name: '@madfam/thing', publish: ['$authenticated'] });
  assert.ok(err instanceof Error);
});

test('an unfilled <PLACEHOLDER> allows no org', async () => {
  const plugin = createPlugin(
    { janua_url: mockJanuaUrl, cache_ttl_ms: 1000, allowed_org_ids: ['<MADFAM_ORG_ID>'] },
    { logger: silentLogger, env: {} }
  );
  assert.deepStrictEqual(plugin.allowedOrgIds, []);
});

test('JANUA_ALLOWED_ORG_IDS is read when the config sets none', async () => {
  const plugin = createPlugin(
    { janua_url: mockJanuaUrl, cache_ttl_ms: 1000 },
    { logger: silentLogger, env: { JANUA_ALLOWED_ORG_IDS: ' org-madfam , org-two ' } }
  );
  assert.deepStrictEqual(plugin.allowedOrgIds, ['org-madfam', 'org-two']);
  const user = await userFromKey(plugin, KEY_PUBLISH);
  const { err, ok } = await allowPublish(plugin, user, { name: '@madfam/thing', publish: ['$authenticated'] });
  assert.strictEqual(err, null);
  assert.strictEqual(ok, true);
});

test('config allowed_org_ids wins over the environment', async () => {
  const plugin = createPlugin(
    { janua_url: mockJanuaUrl, cache_ttl_ms: 1000, allowed_org_ids: ['org-madfam'] },
    { logger: silentLogger, env: { JANUA_ALLOWED_ORG_IDS: 'org-other' } }
  );
  assert.deepStrictEqual(plugin.allowedOrgIds, ['org-madfam']);
});

// ---------------------------------------------------------------------
// resilience
// ---------------------------------------------------------------------

test('Janua unreachable falls through instead of 500ing', async () => {
  const offline = createPlugin(
    { janua_url: 'http://127.0.0.1:1', cache_ttl_ms: 1000, timeout_ms: 500 },
    { logger: silentLogger }
  );
  const groups = await authenticate(offline, 'anyuser', KEY_PUBLISH);
  assert.strictEqual(groups, false);
});

// ---------------------------------------------------------------------

(async () => {
  const server = await startMockJanua();
  const { port } = server.address();
  mockJanuaUrl = `http://127.0.0.1:${port}`;

  // The same prefix list as configmap.yaml.
  const plugin = createPlugin(
    {
      janua_url: mockJanuaUrl,
      cache_ttl_ms: 300000,
      key_prefixes: ['jnk_', 'sk_live_'],
      allowed_org_ids: ['org-madfam'],
    },
    { logger: silentLogger, env: {} }
  );

  let passed = 0;
  let failed = 0;

  for (const { name, fn } of tests) {
    try {
      await fn(plugin);
      console.log(`  ok   ${name}`);
      passed += 1;
    } catch (err) {
      console.log(`  FAIL ${name}`);
      console.log(`       ${err.message}`);
      failed += 1;
    }
  }

  server.close();

  console.log(`\n${passed} passed, ${failed} failed (${tests.length} total)`);
  process.exit(failed === 0 ? 0 : 1);
})();
