# MinIO server, built from its public source at one pinned release.
#
# Why this exists: quay.io/minio/minio and docker.io/minio/minio stopped serving
# anonymous pulls. A workload pinned to an upstream digest now starts only on a
# node that still holds that digest in its image cache, so any reschedule can
# leave it in ImagePullBackOff. Building the same release here and publishing it
# to GHCR makes restarts independent of the upstream registries.
#
# The release and commit are the ones production runs today. The build refuses a
# tag that does not resolve to that commit. MinIO is AGPLv3; the source for this
# image is the upstream tag named below.
ARG MINIO_RELEASE=RELEASE.2025-09-07T16-13-09Z
ARG MINIO_COMMIT=07c3a429bfed433e49018cb0f78a52145d4bedeb

FROM golang:1.24.2-bookworm@sha256:79390b5e5af9ee6e7b1173ee3eac7fadf6751a545297672916b59bfa0ecf6f71 AS build
ARG MINIO_RELEASE
ARG MINIO_COMMIT
RUN git clone --filter=blob:none --branch "${MINIO_RELEASE}" https://github.com/minio/minio.git /src \
 && test "$(git -C /src rev-parse HEAD)" = "${MINIO_COMMIT}"
WORKDIR /src
# Same flags as upstream's Makefile (`make build`): static, kqueue tag, version
# and commit stamped by buildscripts/gen-ldflags.go from the tag checked out.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -tags kqueue -trimpath -ldflags "$(go run buildscripts/gen-ldflags.go)" -o /out/minio

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG MINIO_RELEASE
ARG MINIO_COMMIT
LABEL org.opencontainers.image.title="minio" \
      org.opencontainers.image.version="${MINIO_RELEASE}" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.description="MinIO ${MINIO_RELEASE} (commit ${MINIO_COMMIT}) built from https://github.com/minio/minio"
COPY --from=build /out/minio /usr/bin/minio
EXPOSE 9000 9001
# Manifests pass only args (`server /data --console-address :9001`), exactly as
# they did with the upstream image.
ENTRYPOINT ["/usr/bin/minio"]
