"""Resource namespaces exposed on :class:`AsyncEncliiClient`."""

from enclii_sdk.resources.audit import AuditResource
from enclii_sdk.resources.canary import CanaryResource
from enclii_sdk.resources.deployments import DeploymentsResource
from enclii_sdk.resources.jobs import JobsResource
from enclii_sdk.resources.logs import LogsResource
from enclii_sdk.resources.projects import ProjectsResource
from enclii_sdk.resources.rollback import RollbackResource
from enclii_sdk.resources.secrets import SecretsResource
from enclii_sdk.resources.services import ServicesResource
from enclii_sdk.resources.webhooks import WebhooksResource

__all__ = [
    "AuditResource",
    "CanaryResource",
    "DeploymentsResource",
    "JobsResource",
    "LogsResource",
    "ProjectsResource",
    "RollbackResource",
    "SecretsResource",
    "ServicesResource",
    "WebhooksResource",
]
