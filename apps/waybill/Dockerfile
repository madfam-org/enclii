# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files (support repo root context with apps/waybill/ path)
COPY apps/waybill/go.mod apps/waybill/go.sum ./
COPY packages/otel-go/go.mod packages/otel-go/go.sum /packages/otel-go/
RUN go mod download

# Copy source code
COPY apps/waybill/ .
COPY packages/otel-go/ /packages/otel-go/

# Build API binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /waybill-api ./cmd/api

# Build Aggregator binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /waybill-aggregator ./cmd/aggregator

# Aggregator runtime stage (for background jobs)
FROM alpine:3.20 AS aggregator

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /waybill-aggregator /usr/local/bin/waybill-aggregator

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/waybill-aggregator"]

# API runtime stage (default - HTTP server with health checks)
FROM alpine:3.20 AS api

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /waybill-api /usr/local/bin/waybill-api

USER 65532:65532

EXPOSE 4290

ENTRYPOINT ["/usr/local/bin/waybill-api"]
