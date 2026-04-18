"""Log entry models mirroring ``apps/switchyard-api/internal/logstream/types.go``."""

from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field

from enclii_sdk.models.common import StrEnum


class LogLevel(StrEnum):
    """Normalized log level as exposed by the P2.1 Loki-backed API."""

    ERROR = "error"
    WARN = "warn"
    INFO = "info"
    DEBUG = "debug"


class LogEntry(BaseModel):
    """A single log line.

    ``timestamp`` is delivered as RFC3339Nano — Pydantic parses it into a
    timezone-aware :class:`datetime` automatically.
    """

    model_config = ConfigDict(extra="allow")

    timestamp: datetime
    level: LogLevel = LogLevel.INFO
    pod: str = ""
    container: str = ""
    message: str
    labels: dict[str, str] = Field(default_factory=dict)


class LogQueryResponse(BaseModel):
    """Response shape for ``GET /v1/services/{id}/logs``."""

    entries: list[LogEntry] = Field(default_factory=list)
    next_cursor: str | None = None
    reached_live_tail: bool = False


__all__ = ["LogEntry", "LogLevel", "LogQueryResponse"]
