# Build stage
FROM docker.io/library/node:22-alpine AS builder

WORKDIR /app

# Install pnpm and dependencies
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY apps/landing/package.json ./
RUN pnpm install --frozen-lockfile=false --config.dangerouslyAllowAllBuilds=true

# Copy source files
COPY apps/landing/ .

# Build static site
RUN pnpm run build

# Production stage — unprivileged nginx (runs as uid 101, no root required)
FROM nginxinc/nginx-unprivileged:alpine

# Keep runtime OS packages current when the nginx base tag lags Alpine fixes —
# the trivy gate blocks digest pins on fixable HIGH/CRITICAL CVEs in base
# packages (2026-07-10: CVE-2026-33630, c-ares, fixed in 1.34.8-r0). Same
# pattern as switchyard-api (PR #311); apk needs root, so drop back to the
# base image's unprivileged user (uid 101) right after.
USER root
RUN apk upgrade --no-cache
USER 101

# Copy nginx config
COPY apps/landing/nginx.conf /etc/nginx/conf.d/default.conf

# Copy built static files
COPY --from=builder /app/out /usr/share/nginx/html

EXPOSE 4204

CMD ["nginx", "-g", "daemon off;"]
