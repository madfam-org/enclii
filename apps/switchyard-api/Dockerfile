# Build stage
FROM public.ecr.aws/docker/library/golang:1.26.6-alpine AS builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Build cosign from source with the pinned CI Go toolchain (1.26.6).
# The official release binaries are compiled with an older Go stdlib that
# carries HIGH CVEs, so building here keeps the runtime image free of known
# HIGH/CRITICAL findings while pinning the cosign version.
ENV COSIGN_VERSION=v3.1.1
RUN CGO_ENABLED=0 GOBIN=/usr/local/bin go install "github.com/sigstore/cosign/v3/cmd/cosign@${COSIGN_VERSION}"

# Copy go.work and all module files first
COPY go.work ./
COPY apps/switchyard-api/go.mod apps/switchyard-api/go.sum ./apps/switchyard-api/
COPY packages/sdk-go/go.mod ./packages/sdk-go/
COPY packages/otel-go/go.mod packages/otel-go/go.sum ./packages/otel-go/

# Create stub directories for other modules referenced in go.work
RUN mkdir -p apps/reconcilers apps/roundhouse apps/waybill packages/cli && \
    echo 'module github.com/madfam/enclii/apps/reconcilers' > apps/reconcilers/go.mod && \
    echo 'go 1.24' >> apps/reconcilers/go.mod && \
    echo 'module github.com/madfam/enclii/apps/roundhouse' > apps/roundhouse/go.mod && \
    echo 'go 1.24' >> apps/roundhouse/go.mod && \
    echo 'module github.com/madfam/enclii/apps/waybill' > apps/waybill/go.mod && \
    echo 'go 1.24' >> apps/waybill/go.mod && \
    echo 'module github.com/madfam/enclii/packages/cli' > packages/cli/go.mod && \
    echo 'go 1.24' >> packages/cli/go.mod

# Download dependencies
WORKDIR /app/apps/switchyard-api
RUN go mod download

# Copy source code
WORKDIR /app
COPY apps/switchyard-api/ ./apps/switchyard-api/
COPY packages/sdk-go/ ./packages/sdk-go/
COPY packages/otel-go/ ./packages/otel-go/

# Build the application
WORKDIR /app/apps/switchyard-api
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/switchyard-api ./cmd/api

# Runtime base — everything the API needs in production. Production build
# execution is delegated to Roundhouse (ENCLII_BUILD_MODE=roundhouse in
# infra/k8s/components/environment/common.env and the production patch), so
# the published image does NOT carry the docker CLI or pack: those static
# binaries embed dependency trees (docker/docker, Go stdlib) whose
# HIGH/CRITICAL CVEs routinely have no released upstream fix (2026-07-11:
# CVE-2026-39822, Go stdlib 1.26.4/1.25.11) and kept tripping the RFC 0017
# digest-pin scan gate. The in-process build fallback (internal/builder,
# BUILD_MODE != roundhouse) needs a Docker socket that production never
# mounts — for local in-process builds use the `dev` target below.
FROM public.ecr.aws/docker/library/alpine:3.24 AS runtime-base

# Install required packages:
# - ca-certificates: for HTTPS connections
# - tzdata: for timezone support
# - wget: for health checks
# - git: for cloning repositories
# - curl: for release-download tooling (dev stage) and debugging
# apk upgrade first: pull the base image's pending 3.24.x security patches —
# the trivy gate blocks digest pins on fixable HIGH/CRITICAL CVEs in base
# packages (2026-07-10: CVE-2026-33630, c-ares via curl, fixed in 1.34.8-r0).
RUN apk --no-cache upgrade && apk --no-cache add ca-certificates tzdata wget git curl bash

# Copy cosign (built from source in the builder stage) so the API can verify
# signed image digests before committing production GitOps pins from platform
# build callbacks.
COPY --from=builder /usr/local/bin/cosign /usr/local/bin/cosign

# Create non-root user for running the application
RUN addgroup -g 65532 nonroot && \
    adduser -u 65532 -G nonroot -s /bin/sh -D nonroot

WORKDIR /app

# Create build directories with proper permissions
RUN mkdir -p /tmp/enclii-builds /var/cache/enclii-buildpacks && \
    chown -R nonroot:nonroot /tmp/enclii-builds /var/cache/enclii-buildpacks

# Copy the binary from builder stage and set permissions
COPY --from=builder /app/switchyard-api .
RUN chmod +x /app/switchyard-api && chown nonroot:nonroot /app/switchyard-api

USER nonroot

EXPOSE 4200

# Port 4200 per PORT_ALLOCATION.md in solarpunk-foundry (Enclii block: 4200-4299)
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
  CMD wget -q --spider http://localhost:4200/health || exit 1

CMD ["./switchyard-api"]

# Dev stage — adds the docker CLI + pack for in-process builds against a
# mounted Docker socket (docker-compose's `api` service targets this stage).
# Never published to GHCR and never scanned by CI.
FROM runtime-base AS dev

USER root

# Install Docker CLI from official releases (Alpine's docker-cli is too old)
# Required for building and pushing images via Docker socket mount
ENV DOCKER_VERSION=29.6.1
RUN curl -fsSL "https://download.docker.com/linux/static/stable/x86_64/docker-${DOCKER_VERSION}.tgz" | tar -xz -C /tmp \
    && mv /tmp/docker/docker /usr/local/bin/ \
    && rm -rf /tmp/docker

# Install Cloud Native Buildpacks pack CLI
ENV PACK_VERSION=0.40.7
RUN curl -sSL "https://github.com/buildpacks/pack/releases/download/v${PACK_VERSION}/pack-v${PACK_VERSION}-linux.tgz" | tar -xz -C /usr/local/bin

USER nonroot

# Final stage — the published image. Kept LAST (and empty) so both a bare
# `docker build` and CI's target-less build of this Dockerfile produce the
# slim production runtime without the docker/pack build tooling.
FROM runtime-base
