# Deploy-pipeline freshness monitor — small alpine + Python 3.12 image.
# Sibling pattern to infra/cloudflared-probe/Dockerfile. Runs once per
# CronJob invocation (every 5 min) and exits — no long-running daemon.
#
# Base image follows the ecosystem norm: public.ecr.aws mirror to sidestep
# Docker Hub rate limits on ARC self-hosted runners.

FROM public.ecr.aws/docker/library/python:3.12-alpine

LABEL org.opencontainers.image.title="deploy-pipeline-monitor" \
      org.opencontainers.image.description="GitHub-API CI freshness probe; pushes to Prometheus Pushgateway" \
      org.opencontainers.image.source="https://github.com/madfam-org/enclii"

# Unprivileged user up front so file ownership is correct.
RUN adduser -D -u 65532 -s /sbin/nologin monitor

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY check_main_ci.py .

USER 65532

ENV PYTHONUNBUFFERED=1 \
    REPOS_PATH=/etc/deploy-pipeline-monitor/repos.json \
    PUSHGATEWAY_URL=http://pushgateway.monitoring.svc.cluster.local:9091

ENTRYPOINT ["python", "/app/check_main_ci.py"]
