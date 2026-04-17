'use strict';

const https = require('https');

/**
 * verdaccio-auth-janua
 *
 * Verdaccio v5 auth plugin that validates Bearer tokens against Janua's
 * API key verification endpoint. Falls through to htpasswd on failure
 * so the two auth backends can coexist during migration.
 *
 * Config (verdaccio config.yaml):
 *   auth:
 *     janua-keys:
 *       janua_url: https://api.janua.dev   # base URL, no trailing slash
 *       cache_ttl_ms: 300000               # 5 min default
 */

const DEFAULT_JANUA_URL = 'https://api.janua.dev';
const DEFAULT_CACHE_TTL_MS = 5 * 60 * 1000; // 5 minutes

class JanuaAuthPlugin {
  constructor(config, options) {
    this.januaUrl = (config && config.janua_url) || DEFAULT_JANUA_URL;
    this.cacheTtlMs = (config && config.cache_ttl_ms) || DEFAULT_CACHE_TTL_MS;
    this.logger = options.logger;

    // In-memory cache: key -> { scopes, user, expiresAt }
    this._cache = new Map();

    this.logger.info(
      { janua_url: this.januaUrl, cache_ttl_ms: this.cacheTtlMs },
      'janua-keys auth plugin loaded'
    );
  }

  // ---------------------------------------------------------------
  // authenticate(user, password, callback)
  //   password = Bearer token sent by npm CLI via _authToken
  //   On success: callback(null, [user])
  //   On fall-through: callback(null, false)
  // ---------------------------------------------------------------
  authenticate(user, password, callback) {
    if (!password) {
      return callback(null, false); // fall through to htpasswd
    }

    // Check cache first
    const cached = this._cache.get(password);
    if (cached && Date.now() < cached.expiresAt) {
      this.logger.debug({ user: cached.user }, 'janua-keys: cache hit');
      // Attach scopes to the callback groups array so allow_access can read them
      const groups = (cached.scopes || []).concat([cached.user, '$authenticated']);
      return callback(null, groups);
    }

    // Call Janua API key verification
    const payload = JSON.stringify({ key: password });
    const url = new URL('/api/v1/api-keys/verify', this.januaUrl);

    const reqOptions = {
      hostname: url.hostname,
      port: url.port || 443,
      path: url.pathname,
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(payload),
      },
      timeout: 5000,
    };

    const req = https.request(reqOptions, (res) => {
      let body = '';
      res.on('data', (chunk) => { body += chunk; });
      res.on('end', () => {
        try {
          const data = JSON.parse(body);

          if (res.statusCode === 200 && data.valid === true) {
            const resolvedUser = data.owner || data.name || user;
            const scopes = Array.isArray(data.scopes) ? data.scopes : [];

            // Populate cache
            this._cache.set(password, {
              user: resolvedUser,
              scopes,
              expiresAt: Date.now() + this.cacheTtlMs,
            });

            // Evict expired entries lazily (keep map from growing unbounded)
            if (this._cache.size > 500) {
              this._evictExpired();
            }

            this.logger.info(
              { user: resolvedUser, scopes },
              'janua-keys: auth success'
            );

            const groups = scopes.concat([resolvedUser, '$authenticated']);
            return callback(null, groups);
          }

          // Invalid key or unexpected response -- fall through
          this.logger.warn(
            { status: res.statusCode, valid: data.valid, user },
            'janua-keys: auth rejected'
          );
          return callback(null, false);
        } catch (parseErr) {
          this.logger.error(
            { err: parseErr.message },
            'janua-keys: failed to parse response'
          );
          return callback(null, false);
        }
      });
    });

    req.on('error', (err) => {
      this.logger.error(
        { err: err.message },
        'janua-keys: request failed, falling through'
      );
      return callback(null, false); // network error -> fall through
    });

    req.on('timeout', () => {
      req.destroy();
      this.logger.error('janua-keys: request timed out, falling through');
      return callback(null, false);
    });

    req.write(payload);
    req.end();
  }

  // ---------------------------------------------------------------
  // allow_access -- checks for npm:install scope
  // ---------------------------------------------------------------
  allow_access(user, pkg, callback) {
    // $all packages are always accessible
    if (pkg.access && pkg.access.includes('$all')) {
      return callback(null, true);
    }

    // Authenticated users with the right groups
    if (user && user.name) {
      const groups = user.groups || [];
      // If user was authenticated via htpasswd, groups won't contain scopes.
      // In that case $authenticated is enough (htpasswd users get full access).
      if (groups.includes('$authenticated')) {
        // If groups contain Janua scopes, enforce npm:install
        const hasJanuaScopes = groups.some((g) => g.startsWith('npm:'));
        if (hasJanuaScopes && !groups.includes('npm:install')) {
          this.logger.warn(
            { user: user.name, pkg: pkg.name },
            'janua-keys: access denied, missing npm:install scope'
          );
          return callback(
            new Error('npm:install scope required for package access')
          );
        }
        return callback(null, true);
      }
    }

    // Fall through -- let Verdaccio decide
    return callback(null, false);
  }

  // ---------------------------------------------------------------
  // allow_publish -- checks for npm:publish scope
  // ---------------------------------------------------------------
  allow_publish(user, pkg, callback) {
    if (user && user.name) {
      const groups = user.groups || [];
      if (groups.includes('$authenticated')) {
        const hasJanuaScopes = groups.some((g) => g.startsWith('npm:'));
        if (hasJanuaScopes && !groups.includes('npm:publish')) {
          this.logger.warn(
            { user: user.name, pkg: pkg.name },
            'janua-keys: publish denied, missing npm:publish scope'
          );
          return callback(
            new Error('npm:publish scope required for package publishing')
          );
        }
        return callback(null, true);
      }
    }

    return callback(null, false);
  }

  // ---------------------------------------------------------------
  // Internal: evict expired cache entries
  // ---------------------------------------------------------------
  _evictExpired() {
    const now = Date.now();
    for (const [key, entry] of this._cache) {
      if (now >= entry.expiresAt) {
        this._cache.delete(key);
      }
    }
  }
}

module.exports = (config, options) => new JanuaAuthPlugin(config, options);
