# Synthetic flow probe — minimal, non-root, alpine-based image.
#
# Sibling of cloudflared-probe but layer-7: fires multi-step HTTP journeys
# (login → SSO → protected route) against the public stack and asserts
# end-to-end correctness. Lives in `cloudflare-tunnel` namespace so it sees
# the same network path users see (egress through the tunnel back to the
# public hostname).
#
# Base image follows the ecosystem norm: public.ecr.aws mirror to sidestep
# Docker Hub rate limits on ARC self-hosted runners.

FROM public.ecr.aws/docker/library/python:3.12-alpine

LABEL org.opencontainers.image.title="synthetic-flow-probe" \
      org.opencontainers.image.description="Layer-7 synthetic user-journey probe for the MADFAM ecosystem" \
      org.opencontainers.image.source="https://github.com/madfam-org/enclii"

# Create the unprivileged user up front so file ownership is correct.
RUN adduser -D -u 65532 -s /sbin/nologin probe

WORKDIR /app

# Install Python deps as root, then drop privileges. --no-cache-dir keeps
# the layer small and avoids leaving wheels behind.
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY probe.py .

USER 65532

# Defaults: read journeys from ConfigMap mount, expose metrics on 9090.
# JOURNEYS_DIR points at a directory of *.yaml files (one per journey).
# ENVIRONMENT gates mutating journeys (Phase 1 always 'production' so
# mutating journeys are refused — see probe.py::should_run_journey).
ENV PYTHONUNBUFFERED=1 \
    JOURNEYS_DIR=/etc/synthetic-flow-probe/journeys \
    METRICS_PORT=9090 \
    ENVIRONMENT=production

EXPOSE 9090

ENTRYPOINT ["python", "/app/probe.py"]
