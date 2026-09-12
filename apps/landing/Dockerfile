# Build stage
FROM docker.io/library/node:22-alpine AS builder

WORKDIR /app

# Install pnpm and dependencies.
#
# pnpm is PINNED, not `latest`. `corepack prepare pnpm@latest` floated onto
# pnpm 12.3.4 on 2026-09-09 and broke this build outright: pnpm 12 removed the
# `--flag=false` spelling ("error: unexpected value 'false' for
# '--frozen-lockfile'"). Keep this in sync with the `packageManager` field in
# apps/landing/package.json — corepack reads that field and will refuse to run
# a different version.
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate
COPY apps/landing/package.json ./
# --no-frozen-lockfile (not --frozen-lockfile) is deliberate here: this context
# copies ONLY apps/landing/package.json. landing is a workspace member, so its
# lockfile entries live in the repo-root pnpm-lock.yaml, which is never copied
# into this build — there is no lockfile to freeze against. Transitive versions
# in this image are therefore resolved at build time, unlike every other Node
# image here; making landing build from the workspace lockfile is a separate
# change (it also has to reconcile landing's React 19 against the root's
# @types/react ^18 override).
RUN pnpm install --no-frozen-lockfile --config.dangerouslyAllowAllBuilds=true

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
