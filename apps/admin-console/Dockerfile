# Production Dockerfile for Enclii Dispatch (Infrastructure Control Tower)
# Build context should be the enclii root directory, not apps/admin-console
# Usage: docker build -f apps/admin-console/Dockerfile -t dispatch .

FROM node:20-alpine AS base
RUN apk add --no-cache libc6-compat
RUN corepack enable pnpm
WORKDIR /app

# Build the application
FROM base AS builder
WORKDIR /app

# Copy the workspace root files and packages needed by Dispatch. The app
# depends on @enclii/ui-components via workspace:*, so an isolated
# apps/admin-console install cannot resolve its local package graph.
COPY pnpm-workspace.yaml pnpm-lock.yaml ./
COPY packages ./packages
COPY apps/admin-console ./apps/admin-console
RUN pnpm --dir apps/admin-console install --frozen-lockfile=false
RUN pnpm --dir packages/shared-lib run build
RUN pnpm --dir packages/ui-components run build

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

# Copy standalone build
COPY --from=builder /app/apps/admin-console/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/apps/admin-console/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/apps/admin-console/.next/static ./.next/static

RUN apk add --no-cache wget

USER nextjs

EXPOSE 4203

ENV PORT=4203
ENV HOSTNAME="0.0.0.0"

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
  CMD wget -q --spider http://localhost:4203/api/health || exit 1

CMD ["node", "server.js"]
