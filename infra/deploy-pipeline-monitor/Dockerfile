# Deploy-pipeline freshness monitor — small alpine + Python 3.12 image.
# Sibling pattern to infra/cloudflared-probe/Dockerfile. Runs once per
# CronJob invocation (every 5 min) and exits — no long-running daemon.
#
# Base image: mirror.gcr.io, Google's pull-through mirror of the Docker Hub
# official images (same digests). This image is built on a GitHub-HOSTED
# runner (.github/workflows/deploy-pipeline-monitor.yml, ubuntu-24.04), where
# public.ecr.aws fails with "toomanyrequests: Data limit exceeded": ECR
# Public's 500 GB/month anonymous cap is shared across every GitHub customer
# on the runner's egress IP (PR verify of this change, 2026-10-18). Docker Hub
# itself rate-limits those IPs too, which is why it was never used here.

FROM mirror.gcr.io/library/python:3.12-alpine

LABEL org.opencontainers.image.title="deploy-pipeline-monitor" \
      org.opencontainers.image.description="GitHub/Argo deploy health probes; pushes to Prometheus Pushgateway" \
      org.opencontainers.image.source="https://github.com/madfam-org/enclii"

# Unprivileged user up front so file ownership is correct.
RUN adduser -D -u 65532 -s /sbin/nologin monitor

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY check_main_ci.py .
COPY check_argo_main_parity.py .

USER 65532

ENV PYTHONUNBUFFERED=1 \
    REPOS_PATH=/etc/deploy-pipeline-monitor/repos.json \
    PUSHGATEWAY_URL=http://pushgateway-headless.monitoring.svc.cluster.local:9091

ENTRYPOINT ["python", "/app/check_main_ci.py"]
