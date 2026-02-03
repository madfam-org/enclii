# Production Dockerfile for Enclii Switchyard UI (Next.js)
# Build context should be the enclii root directory, not apps/switchyard-ui
# Usage: docker build -f apps/switchyard-ui/Dockerfile -t enclii-ui .

FROM node:20-alpine AS base
RUN apk add --no-cache libc6-compat
WORKDIR /app

# Install dependencies
FROM base AS deps
WORKDIR /app/switchyard-ui
COPY apps/switchyard-ui/package.json apps/switchyard-ui/package-lock.json ./
RUN npm install --legacy-peer-deps

# Build the application
FROM base AS builder
WORKDIR /app

# Copy switchyard-ui app
COPY apps/switchyard-ui ./switchyard-ui

# Install dependencies from deps stage
COPY --from=deps /app/switchyard-ui/node_modules ./switchyard-ui/node_modules

# Build arguments for public env vars (production defaults)
ARG NEXT_PUBLIC_API_URL=https://api.enclii.dev
ARG NEXT_PUBLIC_AUTH_MODE=oidc
ARG NEXT_PUBLIC_JANUA_URL=https://auth.madfam.io
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_AUTH_MODE=$NEXT_PUBLIC_AUTH_MODE
ENV NEXT_PUBLIC_JANUA_URL=$NEXT_PUBLIC_JANUA_URL
ENV NEXT_TELEMETRY_DISABLED=1

# Build the static site
WORKDIR /app/switchyard-ui
RUN npm run build

# Production image
FROM base AS runner
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

WORKDIR /app

# Copy standalone build
COPY --from=builder /app/switchyard-ui/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/switchyard-ui/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/switchyard-ui/.next/static ./.next/static

RUN apk add --no-cache wget

USER nextjs

EXPOSE 4201

ENV PORT=4201
ENV HOSTNAME="0.0.0.0"

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
  CMD wget -q --spider http://localhost:4201/api/health || exit 1

CMD ["node", "server.js"]
