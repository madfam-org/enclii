# Production Dockerfile for Enclii Switchyard UI (Next.js)
# Build context MUST be the enclii repo root, not apps/switchyard-ui.
# Usage: docker build -f apps/switchyard-ui/Dockerfile -t enclii-ui .

FROM public.ecr.aws/docker/library/node:22-alpine AS base
RUN apk add --no-cache libc6-compat
RUN corepack enable && corepack prepare pnpm@9 --activate
WORKDIR /repo

# ---------------------------------------------------------------------------
# deps stage — install workspace deps from the FULL repo root.
#
# The previous shape copied only `apps/switchyard-ui/package.json` and ran
# `pnpm install` in isolation. That fails because switchyard-ui declares
# `"@enclii/ui-components": "workspace:*"`, but pnpm has no way to resolve
# the workspace symlink without seeing pnpm-workspace.yaml + the package.
# Symptom: ERR_PNPM_WORKSPACE_PKG_NOT_FOUND.
#
# We mirror the nuit-one Dockerfile pattern — copy workspace manifests
# first (so cache invalidates only when deps change), then install once
# at the workspace root, then bring in source.
# ---------------------------------------------------------------------------
FROM base AS deps
ARG NPM_MADFAM_TOKEN
COPY pnpm-workspace.yaml pnpm-lock.yaml .npmrc ./
COPY apps/switchyard-ui/package.json apps/switchyard-ui/
COPY apps/switchyard-ui/.npmrc apps/switchyard-ui/
COPY packages/shared-lib/package.json packages/shared-lib/
COPY packages/ui-components/package.json packages/ui-components/
COPY packages/config/package.json packages/config/
COPY packages/sdk-ts/package.json packages/sdk-ts/
RUN pnpm install --frozen-lockfile=false --config.dangerouslyAllowAllBuilds=true
RUN rm -f .npmrc apps/switchyard-ui/.npmrc

# ---------------------------------------------------------------------------
# builder stage — bring in source, build workspace packages bottom-up,
# then build the Next.js app.
#
# `@enclii/ui-components` and `@enclii/shared-lib` ship `main: ./dist/...`
# so consumers can't import them until tsc has emitted dist/. Build order:
# shared-lib (leaf) → ui-components → switchyard-ui.
# ---------------------------------------------------------------------------
FROM base AS builder
WORKDIR /repo

# Carry the installed workspace forward
COPY --from=deps /repo/pnpm-workspace.yaml ./
COPY --from=deps /repo/pnpm-lock.yaml ./
COPY --from=deps /repo/node_modules ./node_modules
COPY --from=deps /repo/apps/switchyard-ui/node_modules ./apps/switchyard-ui/node_modules
COPY --from=deps /repo/packages/shared-lib/node_modules ./packages/shared-lib/node_modules
COPY --from=deps /repo/packages/ui-components/node_modules ./packages/ui-components/node_modules
COPY --from=deps /repo/packages/sdk-ts/node_modules ./packages/sdk-ts/node_modules

# Bring in source after deps so dep-only changes hit the layer cache
COPY apps/switchyard-ui ./apps/switchyard-ui
COPY packages/shared-lib ./packages/shared-lib
COPY packages/ui-components ./packages/ui-components
COPY packages/config ./packages/config
COPY packages/sdk-ts ./packages/sdk-ts

# Build arguments for public env vars (production defaults)
ARG NEXT_PUBLIC_API_URL=https://api.enclii.dev
ARG NEXT_PUBLIC_AUTH_MODE=oidc
ARG NEXT_PUBLIC_JANUA_URL=https://auth.madfam.io
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_AUTH_MODE=$NEXT_PUBLIC_AUTH_MODE
ENV NEXT_PUBLIC_JANUA_URL=$NEXT_PUBLIC_JANUA_URL
ENV NEXT_TELEMETRY_DISABLED=1

# Build workspace packages bottom-up (leaf → consumer → app)
RUN pnpm --filter @enclii/shared-lib run build
RUN pnpm --filter @enclii/ui-components run build
RUN pnpm --filter @madfam/enclii-sdk run build

# Build the Next.js app
WORKDIR /repo/apps/switchyard-ui
RUN pnpm run build

# ---------------------------------------------------------------------------
# runner stage — minimal Next.js standalone runtime.
# ---------------------------------------------------------------------------
FROM base AS runner
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

WORKDIR /app

# Copy standalone build (Next 13+ standalone target).
COPY --from=builder /repo/apps/switchyard-ui/public ./public
COPY --from=builder --chown=nextjs:nodejs /repo/apps/switchyard-ui/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /repo/apps/switchyard-ui/.next/static ./apps/switchyard-ui/.next/static

RUN apk add --no-cache wget

USER nextjs

EXPOSE 4201

ENV PORT=4201
ENV HOSTNAME="0.0.0.0"

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
  CMD wget -q --spider http://localhost:4201/api/health || exit 1

CMD ["node", "apps/switchyard-ui/server.js"]
