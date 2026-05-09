# Production Dockerfile for Enclii Status Page
# Build context should be the enclii root directory, not apps/status
# Usage: docker build -f apps/status/Dockerfile -t enclii-status .

FROM node:22-alpine AS base
RUN apk add --no-cache libc6-compat
RUN corepack enable pnpm
WORKDIR /app

# Install dependencies
FROM base AS deps
WORKDIR /app/status
COPY apps/status/package.json ./
# Use pnpm fetch for better caching
RUN pnpm install --frozen-lockfile=false

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

RUN apk add --no-cache wget

USER nextjs

EXPOSE 4204

ENV PORT=4204
ENV HOSTNAME="0.0.0.0"

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
  CMD wget -q --spider http://localhost:4204/api/health || exit 1

CMD ["node", "server.js"]
