/**
 * Unit tests for components/dashboard/framework-icon.ts
 *
 * Tests the pure utility functions: inferFrameworkFromContext, detectFramework,
 * getFrameworkLabel. These functions have no React dependencies and can be
 * tested directly.
 */

import {
  inferFrameworkFromContext,
  detectFramework,
  getFrameworkLabel,
} from './framework-icon';

// ---------------------------------------------------------------------------
// inferFrameworkFromContext — no app-specific catalog
// ---------------------------------------------------------------------------

describe('inferFrameworkFromContext — no app-specific catalog', () => {
  it('does not infer MADFAM repo slugs without generic framework hints', () => {
    expect(
      inferFrameworkFromContext('api', 'https://github.com/madfam-org/plain-product'),
    ).toBe('unknown');
    expect(
      inferFrameworkFromContext('api', 'https://github.com/madfam-org/plain-product.git'),
    ).toBe('unknown');
  });

  it('does not infer framework from product name prefixes', () => {
    expect(inferFrameworkFromContext('tezca-api', '')).toBe('unknown');
    expect(inferFrameworkFromContext('enclii-api', '')).toBe('unknown');
  });
});

// ---------------------------------------------------------------------------
// inferFrameworkFromContext — generic pattern matching
// ---------------------------------------------------------------------------

describe('inferFrameworkFromContext — generic patterns', () => {
  it('detects nextjs from repo name containing "nextjs"', () => {
    expect(
      inferFrameworkFromContext('my-app', 'https://github.com/org/my-nextjs-app'),
    ).toBe('nextjs');
  });

  it('detects fastapi from repo name containing "fastapi"', () => {
    expect(
      inferFrameworkFromContext('my-api', 'https://github.com/org/my-fastapi-svc'),
    ).toBe('fastapi');
  });

  it('detects django from repo name containing "django"', () => {
    expect(
      inferFrameworkFromContext('svc', 'https://github.com/org/django-app'),
    ).toBe('django');
  });

  it('infers nextjs for -ui suffix services', () => {
    expect(inferFrameworkFromContext('myproject-ui', '')).toBe('nextjs');
  });

  it('infers nextjs for -web suffix services', () => {
    expect(inferFrameworkFromContext('myproject-web', '')).toBe('nextjs');
  });

  it('infers nextjs for -dashboard suffix services', () => {
    expect(inferFrameworkFromContext('myproject-dashboard', '')).toBe('nextjs');
  });

  it('infers nextjs for -landing suffix services', () => {
    expect(inferFrameworkFromContext('myproject-landing', '')).toBe('nextjs');
  });

  it('infers nextjs for -docs suffix services', () => {
    expect(inferFrameworkFromContext('myproject-docs', '')).toBe('nextjs');
  });

  it('infers nextjs for -status suffix services', () => {
    expect(inferFrameworkFromContext('myproject-status', '')).toBe('nextjs');
  });

  it('returns unknown for generic -api services (no repo hints)', () => {
    expect(inferFrameworkFromContext('myproject-api', '')).toBe('unknown');
  });

  it('returns unknown for generic -server services (no repo hints)', () => {
    expect(inferFrameworkFromContext('myproject-server', '')).toBe('unknown');
  });

  it('detects python for -api with python repo hint', () => {
    expect(
      inferFrameworkFromContext('svc-api', 'https://github.com/org/python-svc'),
    ).toBe('python');
  });

  it('detects node for -api with express repo hint', () => {
    expect(
      inferFrameworkFromContext('svc-api', 'https://github.com/org/express-svc'),
    ).toBe('node');
  });

  it('infers go for cli suffix', () => {
    expect(inferFrameworkFromContext('mycli', '')).toBe('go');
  });

  it('infers go for sdk suffix', () => {
    expect(inferFrameworkFromContext('my-sdk', '')).toBe('go');
  });

  it('returns unknown for unrecognized names', () => {
    expect(inferFrameworkFromContext('random-thing', '')).toBe('unknown');
  });

  it('returns unknown when no arguments match', () => {
    expect(inferFrameworkFromContext('nope', undefined)).toBe('unknown');
  });
});

// ---------------------------------------------------------------------------
// inferFrameworkFromContext — repo hints take priority over name patterns
// ---------------------------------------------------------------------------

describe('inferFrameworkFromContext — priority', () => {
  it('generic repo hints take priority over name-suffix heuristics', () => {
    expect(
      inferFrameworkFromContext('my-ui', 'https://github.com/org/fastapi-service'),
    ).toBe('fastapi');
  });
});

// ---------------------------------------------------------------------------
// detectFramework
// ---------------------------------------------------------------------------

describe('detectFramework', () => {
  it('returns unknown for empty files', () => {
    expect(detectFramework([])).toBe('unknown');
  });

  it('returns unknown for undefined', () => {
    expect(detectFramework(undefined)).toBe('unknown');
  });

  it('detects nextjs from next.config.js', () => {
    expect(detectFramework(['next.config.js', 'package.json'])).toBe('nextjs');
  });

  it('detects nextjs from next.config.mjs', () => {
    expect(detectFramework(['next.config.mjs'])).toBe('nextjs');
  });

  it('detects django from manage.py + requirements.txt', () => {
    expect(detectFramework(['manage.py', 'requirements.txt'])).toBe('django');
  });

  it('detects python from pyproject.toml without manage.py', () => {
    expect(detectFramework(['pyproject.toml', 'src/main.py'])).toBe('python');
  });

  it('detects go from go.mod', () => {
    expect(detectFramework(['go.mod', 'main.go'])).toBe('go');
  });

  it('detects rust from Cargo.toml', () => {
    expect(detectFramework(['Cargo.toml', 'src/main.rs'])).toBe('rust');
  });

  it('detects angular from angular.json', () => {
    expect(detectFramework(['angular.json', 'package.json'])).toBe('angular');
  });

  it('detects nuxt from nuxt.config.ts', () => {
    expect(detectFramework(['nuxt.config.ts', 'package.json'])).toBe('nuxt');
  });

  it('detects svelte from svelte.config.js', () => {
    expect(detectFramework(['svelte.config.js'])).toBe('svelte');
  });

  it('detects rails from Gemfile + config.ru', () => {
    expect(detectFramework(['Gemfile', 'config.ru'])).toBe('rails');
  });

  it('detects node as fallback for package.json only', () => {
    expect(detectFramework(['package.json', 'index.js'])).toBe('node');
  });
});

// ---------------------------------------------------------------------------
// getFrameworkLabel
// ---------------------------------------------------------------------------

describe('getFrameworkLabel', () => {
  it('returns "Next.js" for nextjs', () => {
    expect(getFrameworkLabel('nextjs')).toBe('Next.js');
  });

  it('returns "Go" for go', () => {
    expect(getFrameworkLabel('go')).toBe('Go');
  });

  it('returns "FastAPI" for fastapi', () => {
    expect(getFrameworkLabel('fastapi')).toBe('FastAPI');
  });

  it('returns "Django" for django', () => {
    expect(getFrameworkLabel('django')).toBe('Django');
  });

  it('returns "Python" for python', () => {
    expect(getFrameworkLabel('python')).toBe('Python');
  });

  it('returns "Unknown" for unknown framework', () => {
    expect(getFrameworkLabel('unknown')).toBe('Unknown');
  });

  it('returns "Unknown" for unrecognized string', () => {
    expect(getFrameworkLabel('nonexistent')).toBe('Unknown');
  });

  it('is case-insensitive', () => {
    expect(getFrameworkLabel('NEXTJS')).toBe('Next.js');
    expect(getFrameworkLabel('Go')).toBe('Go');
  });
});
