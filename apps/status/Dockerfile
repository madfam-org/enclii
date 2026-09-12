# Production Dockerfile for Enclii Status Page
# Build context should be the enclii root directory, not apps/status
# Usage: docker build -f apps/status/Dockerfile -t enclii-status .

FROM public.ecr.aws/docker/library/node:22-alpine AS base
RUN apk add --no-cache libc6-compat
# Upgrade the bundled npm CLI: the npm shipped with node:22-alpine bundles
# vulnerable picomatch/sigstore versions that fail the Trivy HIGH/CRITICAL gate.
#
# Pinned to 11.19.1, NOT `latest`. npm vendors its whole dependency tree under
# /usr/local/lib/node_modules/npm/node_modules, so Trivy scans those bundled
# copies as part of the image. The previous 11.18.0 pin bundled
# brace-expansion 5.0.7 (CVE-2026-69152, CVE-2026-14257) and ip-address 10.2.0
# (CVE-2026-69192).
#
# 11.19.1 bundles the fixed brace-expansion 5.0.9 + ip-address 10.5.0.
# Do NOT "upgrade" to npm 12.x: 12.0.2 REGRESSES both back to the vulnerable
# 5.0.7 / 10.2.0. Verify the bundled versions before moving this pin.
# `npm install -g` also leaves every tarball it downloaded in the npm cache at
# /root/.npm/_cacache (~30 MB here). `base` is the stage the RUNNER inherits, so
# that cache shipped inside the published image: Trivy walks it as node-pkg
# content and warns "The size of the scanned file is too large ...
# file_path=root/.npm/_cacache/content-v2/sha...". A cached tarball of any
# vulnerable package is a finding on its own, independent of what the app
# installs. Clean it in the SAME layer that creates it so no stage carries it.
RUN npm install -g npm@11.19.1 \
  && npm cache clean --force \
  && rm -rf /root/.npm
ENV npm_config_dangerously_allow_all_builds=true
WORKDIR /app

# ---------------------------------------------------------------------------
# build-base stage — base + a corepack-prepared pnpm, for build stages only.
#
# pnpm is PINNED. `corepack enable pnpm` only installs a SHIM; with no
# `packageManager` field to read, corepack resolved it to whatever was newest
# at build time and floated onto pnpm 12.3.4 on 2026-09-09, breaking this
# build. The pin is now declared twice on purpose: here, and as
# `packageManager` in apps/status/package.json, the only manifest this
# standalone context copies.
#
# It lives in its own stage, mirroring apps/switchyard-ui/Dockerfile:
# `corepack prepare` materialises pnpm (and its bundled glob/minimatch/tar)
# under /root/.cache/node/corepack, and pnpm 9.x carries HIGH CVEs with no 9.x
# fix line. Doing it in `base` would push that payload into the runner image
# and trip the Trivy HIGH/CRITICAL gate. Only the build stages inherit it.
# ---------------------------------------------------------------------------
FROM base AS build-base
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate

# Install dependencies
FROM build-base AS deps
WORKDIR /app/status
COPY apps/status/package.json apps/status/pnpm-lock.yaml ./
# Use pnpm fetch for better caching
RUN pnpm install --frozen-lockfile --config.dangerouslyAllowAllBuilds=true

# Build the application
FROM build-base AS builder
WORKDIR /app

# Copy status app
COPY apps/status ./status

# Install dependencies from deps stage
COPY --from=deps /app/status/node_modules ./status/node_modules

# Build arguments for public env vars (defaults can be overridden at build time)
ARG NEXT_PUBLIC_APP_URL=https://status.enclii.dev
ARG SITE_NAME="Enclii Status"
# Build identity (ST-2): exposed in the page footer so operators can
# verify which version of the status app is live during incident triage.
# CI passes `${{ github.sha }}` and ISO-8601 build date; defaults below
# keep local builds working without surfacing a misleading SHA.
ARG NEXT_PUBLIC_COMMIT_SHA=local
ARG NEXT_PUBLIC_BUILD_DATE=unknown
ENV NEXT_PUBLIC_APP_URL=$NEXT_PUBLIC_APP_URL
ENV SITE_NAME=$SITE_NAME
ENV NEXT_PUBLIC_COMMIT_SHA=$NEXT_PUBLIC_COMMIT_SHA
ENV NEXT_PUBLIC_BUILD_DATE=$NEXT_PUBLIC_BUILD_DATE
ENV NEXT_TELEMETRY_DISABLED=1

# Build the static site
WORKDIR /app/status
RUN pnpm run build

# Production image
FROM base AS runner
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

WORKDIR /app

# Copy standalone build
# Create public directory and copy contents (handle empty public dir)
RUN mkdir -p ./public
COPY --from=builder /app/status/public/ ./public/
COPY --from=builder --chown=nextjs:nodejs /app/status/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/status/.next/static ./.next/static

# apk upgrade first: pull the node:22-alpine base's pending security
# patches — the trivy gate blocks digest pins on fixable HIGH/CRITICAL
# CVEs in base packages (same pattern as switchyard-api, PR #311).
# The `rm -rf` is a backstop, not a duplicate: it is the last layer that touches
# these paths in the published stage, so the image Trivy scans can never carry a
# package-manager cache even if `base` or a future COPY reintroduces one.
RUN apk upgrade --no-cache && apk add --no-cache wget \
  && rm -rf /root/.npm /root/.cache /root/.local/share/pnpm

USER nextjs

EXPOSE 4204

ENV PORT=4204
ENV HOSTNAME="0.0.0.0"

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
  CMD wget -q --spider http://localhost:4204/api/health || exit 1

CMD ["node", "server.js"]
