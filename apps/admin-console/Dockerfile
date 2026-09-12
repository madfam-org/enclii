# Production Dockerfile for Enclii Dispatch (Infrastructure Control Tower)
# Build context should be the enclii root directory, not apps/admin-console
# Usage: docker build -f apps/admin-console/Dockerfile -t dispatch .

FROM public.ecr.aws/docker/library/node:22-alpine AS base
RUN apk add --no-cache libc6-compat
# Upgrade the bundled npm CLI: the npm shipped with node:22-alpine bundles
# vulnerable picomatch/sigstore versions that fail the Trivy HIGH/CRITICAL gate.
#
# Pinned to 11.19.1, NOT `latest`. npm vendors its whole dependency tree under
# /usr/local/lib/node_modules/npm/node_modules, so Trivy scans those bundled
# copies as part of the image. The previous 11.18.0 pin bundled
# brace-expansion 5.0.7 (CVE-2026-69152, CVE-2026-14257) and ip-address 10.2.0
# (CVE-2026-69192) — that is what failed this image's scan alongside
# switchyard-ui.
#
# 11.19.1 bundles the fixed brace-expansion 5.0.9 + ip-address 10.5.0.
# Do NOT "upgrade" to npm 12.x: 12.0.2 REGRESSES both back to the vulnerable
# 5.0.7 / 10.2.0. Verify the bundled versions before moving this pin.
RUN npm install -g npm@11.19.1
WORKDIR /app

# ---------------------------------------------------------------------------
# build-base stage — base + a corepack-prepared pnpm, for build stages only.
#
# pnpm is PINNED. `corepack enable pnpm` only installs a SHIM; with no
# `packageManager` field to read, corepack resolved it to whatever was newest
# at build time and floated onto pnpm 12.3.4 on 2026-09-09, breaking this
# build. The pin is now declared twice on purpose: here, and as
# `packageManager` in the root package.json that this context copies.
#
# It lives in its own stage, mirroring apps/switchyard-ui/Dockerfile:
# `corepack prepare` materialises pnpm (and its bundled glob/minimatch/tar)
# under /root/.cache/node/corepack, and pnpm 9.x carries HIGH CVEs with no 9.x
# fix line. Doing it in `base` would push that payload into the runner image
# and trip the Trivy HIGH/CRITICAL gate. Only the build stages inherit it.
# ---------------------------------------------------------------------------
FROM base AS build-base
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate

# Build the application
FROM build-base AS builder
WORKDIR /app

# Copy the workspace root files and packages needed by Dispatch. The app
# depends on @enclii/ui-components via workspace:*, so an isolated
# apps/admin-console install cannot resolve its local package graph.
ARG NPM_MADFAM_TOKEN
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml .npmrc ./
COPY packages ./packages
COPY apps/admin-console ./apps/admin-console
# --frozen-lockfile, not the old `--frozen-lockfile=false` (a spelling pnpm 12
# removed outright). The repo-root pnpm-lock.yaml is copied above and is
# correct for this context: pnpm tolerates the workspace members that this
# context deliberately omits (apps/landing, apps/status, apps/switchyard-ui)
# and still freezes everything it does install. Verified locally against
# pnpm 9.15.9 — a frozen install of apps/admin-console completes clean.
RUN pnpm --dir apps/admin-console install --frozen-lockfile --config.dangerouslyAllowAllBuilds=true
RUN rm -f .npmrc
RUN pnpm --dir packages/shared-lib run build
RUN pnpm --dir packages/ui-components run build
RUN pnpm --dir packages/ecosystem-tenants run build

# Build arguments for public env vars (production defaults)
ARG NEXT_PUBLIC_APP_URL=https://admin.enclii.dev
ARG NEXT_PUBLIC_API_URL=https://api.enclii.dev
ARG NEXT_PUBLIC_JANUA_URL=https://auth.madfam.io
ARG NEXT_PUBLIC_ALLOWED_ADMIN_DOMAINS=@madfam.io
ARG NEXT_PUBLIC_ALLOWED_ADMIN_ROLES=superadmin,admin,operator
ARG NEXT_PUBLIC_OAUTH_CLIENT_ID=jnc_lofqyf9LQXG_OwENAIw89p_XvngkWMi-
ENV NEXT_PUBLIC_APP_URL=$NEXT_PUBLIC_APP_URL
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_JANUA_URL=$NEXT_PUBLIC_JANUA_URL
ENV NEXT_PUBLIC_ALLOWED_ADMIN_DOMAINS=$NEXT_PUBLIC_ALLOWED_ADMIN_DOMAINS
ENV NEXT_PUBLIC_ALLOWED_ADMIN_ROLES=$NEXT_PUBLIC_ALLOWED_ADMIN_ROLES
ENV NEXT_PUBLIC_OAUTH_CLIENT_ID=$NEXT_PUBLIC_OAUTH_CLIENT_ID
ENV NEXT_TELEMETRY_DISABLED=1

# Build the static site
WORKDIR /app/apps/admin-console
RUN pnpm run build

# Production image
FROM base AS runner
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

WORKDIR /app

# Copy standalone build. In a pnpm monorepo, Next writes the standalone server
# under apps/admin-console/server.js, not /app/server.js.
COPY --from=builder --chown=nextjs:nodejs /app/apps/admin-console/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/apps/admin-console/public ./apps/admin-console/public
COPY --from=builder --chown=nextjs:nodejs /app/apps/admin-console/.next/static ./apps/admin-console/.next/static

WORKDIR /app/apps/admin-console

# apk upgrade first: pull the node:22-alpine base's pending security
# patches — the trivy gate blocks digest pins on fixable HIGH/CRITICAL
# CVEs in base packages (same pattern as switchyard-api, PR #311).
RUN apk upgrade --no-cache && apk add --no-cache wget

USER nextjs

EXPOSE 4203

ENV PORT=4203
ENV HOSTNAME="0.0.0.0"

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
  CMD wget -q --spider http://localhost:4203/api/health || exit 1

CMD ["node", "server.js"]
