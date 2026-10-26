# Enclii documentation site

This is a standalone npm project. Its `package-lock.json` is the dependency
source for the docs image; repository-root pnpm overrides do not apply here.
Use Node.js 20 or 22 and newer supported even-numbered releases (the image
build uses Node.js 22).

```sh
cd apps/docs-site
npm ci --ignore-scripts --legacy-peer-deps --no-audit --no-fund
npm run typecheck
npm run build
```

The TypeScript config extends the matching `@docusaurus/tsconfig` package, with
React 18 development types matching the React 18 runtime.

The build reads the repository's `docs/` directory and produces `build/` for the
nginx runtime. It keeps the Markdown API reference and its link to the canonical
OpenAPI specification. No Node.js dependencies are copied into that runtime.

## Dependency security

The October 2026 dependency remediation removes `redocusaurus`: it was not
configured as a preset, plugin or theme and no source imported it. This removes
its unused Redoc and DOMPurify dependencies without changing the API reference.

Most npm overrides set patched floors within the existing major version. Two
exceptions are scoped to `@docusaurus/core` 3.10.2 and pinned for review:

- `tinypool` 2.1.2 fixes both prototype-pollution worker execution advisories,
  [GHSA-5gmw-xhrv-c9v3](https://github.com/advisories/GHSA-5gmw-xhrv-c9v3) and
  [GHSA-85c8-ppgw-ccpr](https://github.com/advisories/GHSA-85c8-ppgw-ccpr).
  Its Node.js 20/22 requirement matches the supported docs build environment;
  Docusaurus server-side rendering is checked by the normal production build
  and a separate compatibility build with its optional worker mode enabled.
- `update-notifier` 7.3.1 preserves the notification API and uses
  `latest-version` 9 / `package-json` 10 / `ky`. This removes `got`,
  `cacheable-request` and `http-cache-semantics` from the dependency graph.
  [GHSA-ch52-4w7c-c8xp](https://github.com/advisories/GHSA-ch52-4w7c-c8xp)
  has no published patched `http-cache-semantics` version; this is dependency
  chain removal, with no replacement package or vulnerability waiver.

Keep the lockfile committed and use `npm ci` for image builds. Remove a security
override only after the corresponding upstream dependency requires an unaffected
version and a fresh audit plus the docs build pass. Recheck the two scoped major
upgrades when updating Docusaurus. A source audit and local build do not establish
hosted CI, image scan/signature, deployed-site or runtime acceptance.
