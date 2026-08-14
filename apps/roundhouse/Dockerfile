# =============================================================================
# Roundhouse — published images from one Dockerfile via --target:
#   --target api        -> `roundhouse` image (slim alpine, HTTP API only)
#   --target worker     -> `roundhouse-worker` image (slim alpine, kaniko-only)
#   --target worker-dev -> dev-only image (dind + docker/pack/syft/cosign) for
#                          BUILD_MODE=docker local runs; never published/scanned
# CI passes the target per services.json entry (see the `target` field).
# The worker stage stays LAST so a bare `docker build` (no --target) still
# produces the published worker image.
#
# Why the published worker is slim (enclii#304): production runs
# BUILD_MODE=kaniko (infra/k8s/base/roundhouse.yaml — privileged:false,
# readOnlyRootFilesystem:true, no docker socket), where the worker only talks
# to the K8s API/Redis and delegates builds to Kaniko Jobs; SBOM generation
# and signing also run as separate Jobs (SyftImage/CosignImage in
# internal/builder/kaniko_postbuild.go). The kaniko executor execs no local
# binaries at all. The dind stack (dockerd/containerd/runc/ctr) plus the
# docker/pack CLIs only serve BUILD_MODE=docker (internal/builder/executor.go
# — dev-only per internal/worker/processor.go) and carried a rolling pile of
# upstream-unfixable HIGH/CRITICAL trivy findings that kept tripping the
# RFC 0017 digest-pin scan gate.
# =============================================================================

# Build stage
FROM public.ecr.aws/docker/library/golang:1.26.6-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Build cosign from source with the pinned CI Go toolchain (1.26.6).
# The official release binaries are compiled with an older Go stdlib that
# carries HIGH CVEs, so building here keeps the worker image free of known
# HIGH/CRITICAL findings while pinning the cosign version (same pattern as
# apps/switchyard-api/Dockerfile).
ENV COSIGN_VERSION=v3.1.3
# Built from a clone, not `go install`, because upstream cosign v3.1.3 still
# pins two dependencies below their fixed versions — golang.org/x/text
# v0.38.0 (CVE-2026-56852, fixed 0.39.0) and google.golang.org/grpc v1.82.0
# (GHSA-hrxh-6v49-42gf, fixed 1.82.1). `go install pkg@version` builds in
# module-authoritative mode and offers no way to raise them, so the scan
# fails on a binary we compile ourselves. A clone lets us `go get` exactly
# those two and nothing else. Drop this back to `go install` the moment a
# cosign release carries both fixes.
RUN git clone --depth 1 -b "${COSIGN_VERSION}" https://github.com/sigstore/cosign /tmp/cosign-src && \
    cd /tmp/cosign-src && \
    go get golang.org/x/text@v0.39.0 google.golang.org/grpc@v1.82.1 && \
    go mod tidy && \
    CGO_ENABLED=0 go build -o /usr/local/bin/cosign ./cmd/cosign && \
    rm -rf /tmp/cosign-src

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

# apk upgrade first: pull the base image's pending 3.24.x security patches —
# the trivy gate blocks digest pins on fixable HIGH/CRITICAL CVEs in base
# packages (same pattern as apps/switchyard-api/Dockerfile, PR #311).
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata

RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /roundhouse-api /usr/local/bin/roundhouse-api

USER 65532:65532

EXPOSE 8081

ENTRYPOINT ["/usr/local/bin/roundhouse-api"]

# Dev-only worker stage — dind + full docker-mode build tooling for
# BUILD_MODE=docker local runs (docker-compose mounts the Docker socket and
# targets this stage). NOT published to GHCR and NOT scanned by CI: the dind
# stack and the docker/pack CLIs embed dependency trees (grpc, otel, x/net,
# Go stdlib) whose HIGH/CRITICAL CVEs routinely have no released upstream fix.
# Pinned patch release per ENCLII_BASE_IMAGE_MIRROR_POLICY_2026-05-14 (no
# floating minor tags); 29.6.1 matches the Docker CLI version already used
# by apps/switchyard-api/Dockerfile.
FROM public.ecr.aws/docker/library/docker:29.6.1-dind AS worker-dev

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

# Install pack CLI for buildpacks (docker-mode buildpacks builds only).
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

# Worker runtime stage — published as the `roundhouse-worker` image.
# Slim alpine, mirroring the api stage: in production (BUILD_MODE=kaniko)
# the worker creates Kaniko/Syft/Cosign Jobs via the K8s API and execs no
# local binaries, so it needs no build tooling. Kept LAST so a bare
# `docker build` (no --target) produces this published image.
FROM public.ecr.aws/docker/library/alpine:3.24 AS worker

# apk upgrade first: pull the base image's pending 3.24.x security patches —
# the trivy gate blocks digest pins on fixable HIGH/CRITICAL CVEs in base
# packages (2026-07-11: CVE-2026-33630 c-ares, CVE-2026-56131/56407/56408
# libexpat; same pattern as apps/switchyard-api/Dockerfile, PR #311).
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata

RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

# Copy BOTH binaries so this image can run as either API or worker
COPY --from=builder /roundhouse-api /usr/local/bin/roundhouse-api
COPY --from=builder /roundhouse-worker /usr/local/bin/roundhouse-worker

# Build work directory (used only by BUILD_MODE=docker, which this image
# does not support — kept for env parity with the k8s manifest, which sets
# BUILD_WORK_DIR; the kaniko-mode worker never writes here).
RUN mkdir -p /var/lib/roundhouse/builds

ENV BUILD_WORK_DIR=/var/lib/roundhouse/builds

USER 65532:65532

# Default to worker mode; override with command to run API
ENTRYPOINT ["/usr/local/bin/roundhouse-worker"]
