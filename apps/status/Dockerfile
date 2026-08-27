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
RUN npm install -g npm@11.19.1
RUN corepack enable pnpm
ENV npm_config_dangerously_allow_all_builds=true
WORKDIR /app

# Install dependencies
FROM base AS deps
WORKDIR /app/status
COPY apps/status/package.json apps/status/pnpm-lock.yaml ./
# Use pnpm fetch for better caching
RUN pnpm install --frozen-lockfile --config.dangerouslyAllowAllBuilds=true

# Build the application
FROM base AS builder
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
RUN apk upgrade --no-cache && apk add --no-cache wget

USER nextjs

EXPOSE 4204

ENV PORT=4204
ENV HOSTNAME="0.0.0.0"

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
  CMD wget -q --spider http://localhost:4204/api/health || exit 1

CMD ["node", "server.js"]
