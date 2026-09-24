# Production Dockerfile for Enclii Documentation Site (Docusaurus)
# Build context should be the enclii root directory, not apps/docs-site
# Usage: docker build -f apps/docs-site/Dockerfile -t enclii-docs .

FROM docker.io/library/node:22-alpine AS base
RUN apk add --no-cache libc6-compat
WORKDIR /app

# Install dependencies
FROM base AS deps
WORKDIR /app/docs-site
COPY apps/docs-site/package.json apps/docs-site/package-lock.json* ./
RUN npm install --legacy-peer-deps

# Build the application
FROM base AS builder
WORKDIR /app

# Copy docs-site app
COPY apps/docs-site ./docs-site

# Copy docs content
COPY docs ./docs

# Install dependencies from deps stage
COPY --from=deps /app/docs-site/node_modules ./docs-site/node_modules

# Build the static site
WORKDIR /app/docs-site
ENV DOCKER_BUILD=true
RUN npm run build

# Production image - serve static files with nginx
# Using standard nginx on port 80 (deployed image already uses this)
FROM docker.io/library/nginx:alpine AS runner

# Keep ALL runtime OS packages current when the nginx base tag lags Alpine
# fixes — the trivy gate blocks digest pins on fixable HIGH/CRITICAL CVEs in
# base packages (2026-07-10: CVE-2026-33630 c-ares, CVE-2026-56131/56407/56408
# libexpat; previously libxml2). Same pattern as switchyard-api (PR #311).
#
# APK_SECURITY_REFRESH is echoed into the command so bumping it busts the
# BuildKit layer cache: a cached `apk upgrade` layer keeps shipping the old
# package set after Alpine publishes a fix (see apps/switchyard-api/Dockerfile).
# The `libexpat>=…` floor fails the build loudly if the mirror serves an older
# package than the fix the scan gate expects.
# 2026-09-24: CVE-2026-93990 (libexpat 2.8.4-r0, fixed 2.8.5-r0) failed
# `Scan: docs-site` on #625's push run 36056625981.
ARG APK_SECURITY_REFRESH=2026-09-24
RUN echo "apk security refresh: ${APK_SECURITY_REFRESH}" && \
    apk upgrade --no-cache && \
    apk add --no-cache 'libexpat>=2.8.5-r0'

# Copy custom nginx config (standard port 80)
# Security headers applied at origin (defense-in-depth; Cloudflare may also set some).
# CSP keeps 'unsafe-inline' for Docusaurus runtime; revisit with nonces later.
# connect-src includes analytics.madfam.io for PostHog tracking.
RUN echo 'server { \
    listen 80; \
    server_name localhost; \
    root /usr/share/nginx/html; \
    index index.html; \
    gzip on; \
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript; \
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always; \
    add_header Content-Security-Policy "default-src '"'"'self'"'"'; script-src '"'"'self'"'"' '"'"'unsafe-inline'"'"' https://analytics.madfam.io; style-src '"'"'self'"'"' '"'"'unsafe-inline'"'"'; img-src '"'"'self'"'"' data: https:; font-src '"'"'self'"'"' data:; connect-src '"'"'self'"'"' https://analytics.madfam.io; frame-ancestors '"'"'none'"'"'; base-uri '"'"'self'"'"'; form-action '"'"'self'"'"'" always; \
    add_header X-Frame-Options "DENY" always; \
    add_header X-Content-Type-Options "nosniff" always; \
    add_header Referrer-Policy "strict-origin-when-cross-origin" always; \
    add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always; \
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2)$ { \
        expires 1y; \
        add_header Cache-Control "public, immutable" always; \
        add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always; \
        add_header X-Content-Type-Options "nosniff" always; \
    } \
    location / { \
        try_files $uri $uri/ /index.html; \
    } \
    location /health { \
        access_log off; \
        return 200 "OK"; \
        add_header Content-Type text/plain; \
    } \
}' > /etc/nginx/conf.d/default.conf

# Copy built static files
COPY --from=builder /app/docs-site/build /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
