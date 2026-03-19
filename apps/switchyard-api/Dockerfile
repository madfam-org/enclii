# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy go.work and all module files first
COPY go.work ./
COPY apps/switchyard-api/go.mod apps/switchyard-api/go.sum ./apps/switchyard-api/
COPY packages/sdk-go/go.mod ./packages/sdk-go/

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

# Build the application
WORKDIR /app/apps/switchyard-api
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/switchyard-api ./cmd/api

# Final stage - includes build tooling for CI/CD pipeline
FROM alpine:3.20

# Install required packages:
# - ca-certificates: for HTTPS connections
# - tzdata: for timezone support
# - wget: for health checks
# - git: for cloning repositories
# - curl: for downloading pack CLI and Docker CLI
RUN apk --no-cache add ca-certificates tzdata wget git curl bash

# Install Docker CLI from official releases (Alpine's docker-cli is too old)
# Required for building and pushing images via Docker socket mount
ENV DOCKER_VERSION=27.4.1
RUN curl -fsSL "https://download.docker.com/linux/static/stable/x86_64/docker-${DOCKER_VERSION}.tgz" | tar -xz -C /tmp \
    && mv /tmp/docker/docker /usr/local/bin/ \
    && rm -rf /tmp/docker

# Install Cloud Native Buildpacks pack CLI
ENV PACK_VERSION=0.35.1
RUN curl -sSL "https://github.com/buildpacks/pack/releases/download/v${PACK_VERSION}/pack-v${PACK_VERSION}-linux.tgz" | tar -xz -C /usr/local/bin

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
