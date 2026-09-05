# Build stage
FROM public.ecr.aws/docker/library/golang:1.26.6-alpine AS builder

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

# Build Weighbridge binary (the CI runner meter). Same module as the biller on
# purpose: the event it posts is waybill's own events.EventRequest, and a meter
# in another module has to hand-copy the wire struct.
RUN CGO_ENABLED=0 GOOS=linux go build -o /weighbridge ./cmd/weighbridge

# Aggregator runtime stage (for background jobs)
FROM public.ecr.aws/docker/library/alpine:3.20 AS aggregator

# apk upgrade first: pull pending 3.20.x security patches — the trivy gate
# blocks digest pins on fixable HIGH/CRITICAL CVEs in base packages (same
# pattern as switchyard-api, PR #311).
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata && \
    addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /waybill-aggregator /usr/local/bin/waybill-aggregator

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/waybill-aggregator"]

# Weighbridge runtime stage — the CI runner meter, deployed to the `monitoring`
# namespace (NOT alongside the API). Built as its own stage and its own image
# rather than a second entrypoint on the api image, so that a meter rollout and
# a biller rollout are independent events: repinning one must not restart the
# other.
FROM public.ecr.aws/docker/library/alpine:3.20 AS weighbridge

RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata && \
    addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /weighbridge /usr/local/bin/weighbridge

USER 65532:65532

EXPOSE 9102

ENTRYPOINT ["/usr/local/bin/weighbridge"]

# API runtime stage (default - HTTP server with health checks)
#
# KEEP THIS STAGE LAST. `docker build` with no `--target` builds the final
# stage, and services.json's `waybill` entry names no target — so moving a
# stage below this one silently changes which binary the `waybill` image runs.
FROM public.ecr.aws/docker/library/alpine:3.20 AS api

# apk upgrade first: pull pending 3.20.x security patches — the trivy gate
# blocks digest pins on fixable HIGH/CRITICAL CVEs in base packages (same
# pattern as switchyard-api, PR #311).
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata && \
    addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /waybill-api /usr/local/bin/waybill-api

USER 65532:65532

EXPOSE 4290

ENTRYPOINT ["/usr/local/bin/waybill-api"]
