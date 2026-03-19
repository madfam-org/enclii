# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files (support repo root context with apps/roundhouse/ path)
COPY apps/roundhouse/go.mod apps/roundhouse/go.sum ./
RUN go mod download

# Copy source code
COPY apps/roundhouse/ .

# Build API binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /roundhouse-api ./cmd/api

# Build Worker binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /roundhouse-worker ./cmd/worker

# API runtime stage (lightweight)
FROM alpine:3.20 AS api

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /roundhouse-api /usr/local/bin/roundhouse-api

USER 65532:65532

EXPOSE 8081

ENTRYPOINT ["/usr/local/bin/roundhouse-api"]

# Worker runtime stage - use Docker 27 for API version 1.44+ compatibility
FROM docker:27-dind AS worker

# Install additional tools for building
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    git \
    curl \
    bash

# Install syft for SBOM generation
RUN curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /usr/local/bin

# Install cosign for image signing
RUN curl -sSfL https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64 -o /usr/local/bin/cosign && \
    chmod +x /usr/local/bin/cosign

# Install pack CLI for buildpacks
RUN curl -sSfL https://github.com/buildpacks/pack/releases/download/v0.32.1/pack-v0.32.1-linux.tgz | tar -xz -C /usr/local/bin

# Copy BOTH binaries so this image can run as either API or worker
COPY --from=builder /roundhouse-api /usr/local/bin/roundhouse-api
COPY --from=builder /roundhouse-worker /usr/local/bin/roundhouse-worker

# Create build work directory
RUN mkdir -p /var/lib/roundhouse/builds

ENV BUILD_WORK_DIR=/var/lib/roundhouse/builds

# Default to worker mode; override with command to run API
ENTRYPOINT ["/usr/local/bin/roundhouse-worker"]
