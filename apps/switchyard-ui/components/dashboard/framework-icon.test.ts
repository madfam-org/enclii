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
  type FrameworkType,
} from './framework-icon';

// ---------------------------------------------------------------------------
// inferFrameworkFromContext — known repo map
// ---------------------------------------------------------------------------

describe('inferFrameworkFromContext — known repo map', () => {
  it('maps janua repo to fastapi', () => {
    expect(
      inferFrameworkFromContext('janua-api', 'https://github.com/madfam-org/janua'),
    ).toBe('fastapi');
  });

  it('maps tezca repo (leyes-como-codigo-mx) to django', () => {
    expect(
      inferFrameworkFromContext(
        'tezca-api',
        'https://github.com/madfam-org/leyes-como-codigo-mx',
      ),
    ).toBe('django');
  });

  it('maps tezca by name prefix to django', () => {
    expect(inferFrameworkFromContext('tezca-web', '')).toBe('django');
  });

  it('maps pravara-mes repo to python', () => {
    expect(
      inferFrameworkFromContext(
        'pravara-api',
        'https://github.com/madfam-org/pravara-mes',
      ),
    ).toBe('python');
  });

  it('maps dhanam repo to nextjs', () => {
    expect(
      inferFrameworkFromContext('dhanam-web', 'https://github.com/madfam-org/dhanam'),
    ).toBe('nextjs');
  });

  it('maps forgesight repo to nextjs', () => {
    expect(
      inferFrameworkFromContext(
        'forgesight-app',
        'https://github.com/madfam-org/forgesight',
      ),
    ).toBe('nextjs');
  });

  it('maps karafiel repo to nextjs', () => {
    expect(
      inferFrameworkFromContext(
        'karafiel-web',
        'https://github.com/madfam-org/karafiel',
      ),
    ).toBe('nextjs');
  });

  it('maps yantra4d repo to nextjs', () => {
    expect(
      inferFrameworkFromContext(
        'yantra4d-frontend',
        'https://github.com/madfam-org/yantra4d',
      ),
    ).toBe('nextjs');
  });

  it('maps enclii repo to go', () => {
    expect(
      inferFrameworkFromContext(
        'switchyard-api',
        'https://github.com/madfam-org/enclii',
      ),
    ).toBe('go');
  });

  it('maps madfam-site repo to nextjs', () => {
    expect(
      inferFrameworkFromContext(
        'madfam-landing',
        'https://github.com/madfam-org/madfam-site',
      ),
    ).toBe('nextjs');
  });

  it('maps autoswarm-office repo to nextjs', () => {
    expect(
      inferFrameworkFromContext(
        'office-ui',
        'https://github.com/madfam-org/autoswarm-office',
      ),
    ).toBe('nextjs');
  });

  it('strips .git suffix from repo slug', () => {
    expect(
      inferFrameworkFromContext(
        'api',
        'https://github.com/madfam-org/janua.git',
      ),
    ).toBe('fastapi');
  });
});

// ---------------------------------------------------------------------------
// inferFrameworkFromContext — name prefix fallback
// ---------------------------------------------------------------------------

describe('inferFrameworkFromContext — name prefix fallback', () => {
  it('infers django from tezca-admin name (no repo)', () => {
    expect(inferFrameworkFromContext('tezca-admin', '')).toBe('django');
  });

  it('infers go from enclii-api name (no repo)', () => {
    expect(inferFrameworkFromContext('enclii-api', '')).toBe('go');
  });

  it('infers nextjs from dhanam-web name (no repo)', () => {
    expect(inferFrameworkFromContext('dhanam-web', '')).toBe('nextjs');
  });

  it('infers nextjs from forgesight-app name (no repo)', () => {
    expect(inferFrameworkFromContext('forgesight-app', '')).toBe('nextjs');
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
// inferFrameworkFromContext — repo slug takes priority over name patterns
// ---------------------------------------------------------------------------

describe('inferFrameworkFromContext — priority', () => {
  it('repo slug match takes priority over name-suffix heuristic', () => {
    // janua-api would match -api pattern → "unknown" via generic,
    // but repo slug "janua" should win → "fastapi"
    expect(
      inferFrameworkFromContext('janua-api', 'https://github.com/madfam-org/janua'),
    ).toBe('fastapi');
  });

  it('name prefix match takes priority over generic -ui pattern', () => {
    // tezca-ui would match -ui → nextjs via generic,
    // but name prefix "tezca" should win → "django"
    expect(inferFrameworkFromContext('tezca-ui', '')).toBe('django');
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
