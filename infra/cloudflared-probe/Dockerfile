# Cloudflared probe — minimal, non-root, distroless-style alpine image.
# Base image follows the ecosystem norm: public.ecr.aws mirror to sidestep
# Docker Hub rate limits on ARC self-hosted runners.

FROM public.ecr.aws/docker/library/python:3.12-alpine

# OCI labels for image provenance (CI overrides revision via build args, but
# defaults below keep `docker inspect` informative for local builds).
LABEL org.opencontainers.image.title="cloudflared-probe" \
      org.opencontainers.image.description="Synthetic intra-cluster probe that proves cloudflared can reach Services" \
      org.opencontainers.image.source="https://github.com/madfam-org/enclii"

# Create the unprivileged user up front so file ownership is correct.
RUN adduser -D -u 65532 -s /sbin/nologin probe

WORKDIR /app

# Install Python deps as root, then drop privileges. --no-cache-dir keeps
# the layer small (~25 MB) and avoids leaving wheels behind.
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY probe.py .

USER 65532

# Default: read targets from ConfigMap mount, expose metrics on 9090.
ENV PYTHONUNBUFFERED=1 \
    TARGETS_PATH=/etc/cloudflared-probe/targets.json \
    METRICS_PORT=9090

EXPOSE 9090

ENTRYPOINT ["python", "/app/probe.py"]
