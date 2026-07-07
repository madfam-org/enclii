# =============================================================================
# Roundhouse — two published images from one Dockerfile via --target:
#   --target api    -> `roundhouse` image (slim alpine, HTTP API only)
#   --target worker -> `roundhouse-worker` image (dind + build tooling)
# CI passes the target per services.json entry (see the `target` field).
# The worker stage stays LAST so a bare `docker build` (no --target) still
# produces the worker image, matching pre-split behavior.
# =============================================================================

# Build stage
FROM public.ecr.aws/docker/library/golang:1.26.4-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Build cosign from source with the pinned CI Go toolchain (1.26.4).
# The official release binaries are compiled with an older Go stdlib that
# carries HIGH CVEs, so building here keeps the worker image free of known
# HIGH/CRITICAL findings while pinning the cosign version (same pattern as
# apps/switchyard-api/Dockerfile).
ENV COSIGN_VERSION=v3.1.1
RUN CGO_ENABLED=0 GOBIN=/usr/local/bin go install "github.com/sigstore/cosign/v3/cmd/cosign@${COSIGN_VERSION}"

# Build syft from source for the same reason (the release binary currently
# ships a Go 1.26.3 stdlib with 2 HIGH findings, and the previous install.sh
# approach pulled an unpinned "latest" binary at build time).
ENV SYFT_VERSION=v1.46.0
RUN CGO_ENABLED=0 GOBIN=/usr/local/bin go install "github.com/anchore/syft/cmd/syft@${SYFT_VERSION}"

# Copy go mod files (support repo root context with apps/roundhouse/ path)
COPY apps/roundhouse/go.mod apps/roundhouse/go.sum ./
COPY packages/otel-go/go.mod packages/otel-go/go.sum /packages/otel-go/
COPY packages/sdk-go/go.mod /packages/sdk-go/
RUN go mod download

# Copy source code
COPY apps/roundhouse/ .
COPY packages/otel-go/ /packages/otel-go/
COPY packages/sdk-go/ /packages/sdk-go/

# Build API binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /roundhouse-api ./cmd/api

# Build Worker binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /roundhouse-worker ./cmd/worker

# API runtime stage (lightweight) — published as the `roundhouse` image
FROM public.ecr.aws/docker/library/alpine:3.24 AS api

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /roundhouse-api /usr/local/bin/roundhouse-api

USER 65532:65532

EXPOSE 8081

ENTRYPOINT ["/usr/local/bin/roundhouse-api"]

# Worker runtime stage — published as the `roundhouse-worker` image.
# Pinned patch release per ENCLII_BASE_IMAGE_MIRROR_POLICY_2026-05-14 (no
# floating minor tags); 29.6.1 matches the Docker CLI version already used
# by apps/switchyard-api/Dockerfile.
FROM public.ecr.aws/docker/library/docker:29.6.1-dind AS worker

# Drop CLI plugins the worker never invokes (it shells out to plain
# `docker build/push/inspect/login` only — see internal/builder/executor.go).
# buildx and compose each embed docker/docker + containerd module trees that
# carry their own HIGH CVEs; removing them shrinks the scan surface without
# changing worker behavior.
RUN rm -rf /usr/local/libexec/docker/cli-plugins

# Install additional tools for building
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    git \
    curl \
    bash

# Copy syft (SBOM generation) and cosign (image signing) — both built from
# source in the builder stage with the pinned Go toolchain. Release binaries
# ship with an older Go stdlib carrying HIGH CVEs, and the previous syft
# install.sh pulled an unpinned "latest" binary at build time.
COPY --from=builder /usr/local/bin/syft /usr/local/bin/syft
COPY --from=builder /usr/local/bin/cosign /usr/local/bin/cosign

# Install pack CLI for buildpacks. 0.40.7 vendors docker/docker v28.5.2 —
# the three embedded-docker CVEs are covered by the justified entries in
# the root .trivyignore (see the rotation condition there).
ENV PACK_VERSION=0.40.7
RUN curl -sSfL "https://github.com/buildpacks/pack/releases/download/v${PACK_VERSION}/pack-v${PACK_VERSION}-linux.tgz" | tar -xz -C /usr/local/bin

# Copy BOTH binaries so this image can run as either API or worker
COPY --from=builder /roundhouse-api /usr/local/bin/roundhouse-api
COPY --from=builder /roundhouse-worker /usr/local/bin/roundhouse-worker

# Create build work directory
RUN mkdir -p /var/lib/roundhouse/builds

ENV BUILD_WORK_DIR=/var/lib/roundhouse/builds

# Default to worker mode; override with command to run API
ENTRYPOINT ["/usr/local/bin/roundhouse-worker"]
