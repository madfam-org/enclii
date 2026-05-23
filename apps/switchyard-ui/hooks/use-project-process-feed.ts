"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { apiFetchResponse, apiGet, getAuthHeadersRecord } from "@/lib/api";
import { POLLING_NORMAL } from "@/lib/constants";
import { usePolling } from "@/hooks/use-polling";
import type {
  ProjectProcessSummary,
  ProjectProcessSummaryResponse,
} from "@/lib/project-process-feed";

interface ProjectIdentity {
  id: string;
}

export function useProjectProcessFeed(projects: ProjectIdentity[]) {
  const [summaries, setSummaries] = useState<
    Record<string, ProjectProcessSummary>
  >({});
  const [lastSyncedAt, setLastSyncedAt] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [streamConnected, setStreamConnected] = useState(false);

  const projectIds = useMemo(
    () => projects.map((p) => p.id).filter(Boolean).sort(),
    [projects],
  );
  const projectIdsKey = projectIds.join(",");

  const refresh = useCallback(async () => {
    if (!projectIdsKey) {
      setSummaries({});
      setError(null);
      return;
    }

    try {
      const response = await apiGet<ProjectProcessSummaryResponse>(
        `/v1/project-processes/summary?project_ids=${encodeURIComponent(
          projectIdsKey,
        )}&limit_per_project=5&active_only=true`,
      );
      const indexed: Record<string, ProjectProcessSummary> = {};
      for (const summary of response.summaries || []) {
        indexed[summary.project_id] = summary;
      }
      setSummaries(indexed);
      setLastSyncedAt(new Date().toISOString());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load process feed");
    }
  }, [projectIdsKey]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (!projectIdsKey || typeof window === "undefined") {
      setStreamConnected(false);
      return;
    }

    let stopped = false;
    let controller: AbortController | null = null;

    const run = async () => {
      while (!stopped) {
        controller = new AbortController();
        try {
          await streamProcessSummaries(projectIdsKey, controller.signal, (response) => {
            const indexed: Record<string, ProjectProcessSummary> = {};
            for (const summary of response.summaries || []) {
              indexed[summary.project_id] = summary;
            }
            setSummaries(indexed);
            setLastSyncedAt(new Date().toISOString());
            setError(null);
            setStreamConnected(true);
          });
        } catch (err) {
          if (!stopped) {
            setStreamConnected(false);
            setError(err instanceof Error ? err.message : "Process stream disconnected");
          }
        }

        if (!stopped) {
          setStreamConnected(false);
          await new Promise((resolve) => setTimeout(resolve, 5_000));
        }
      }
    };

    run();

    return () => {
      stopped = true;
      controller?.abort();
      setStreamConnected(false);
    };
  }, [projectIdsKey]);

  usePolling(refresh, POLLING_NORMAL, {
    enabled: projectIds.length > 0 && !streamConnected,
  });

  return {
    summaries,
    lastSyncedAt,
    error,
    streamConnected,
    refresh,
  };
}

async function streamProcessSummaries(
  projectIdsKey: string,
  signal: AbortSignal,
  onSummary: (summary: ProjectProcessSummaryResponse) => void,
) {
  const params = new URLSearchParams({
    project_ids: projectIdsKey,
    limit_per_project: "5",
    active_only: "true",
  });
  const response = await apiFetchResponse(
    `/v1/project-processes/stream?${params.toString()}`,
    {
      headers: {
        ...getAuthHeadersRecord(false),
        Accept: "text/event-stream",
      },
      signal,
    },
  );

  if (!response.ok || !response.body) {
    throw new Error(
      `Process stream failed: ${response.status} ${response.statusText}`,
    );
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const rawEvent = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      handleSSEEvent(rawEvent, onSummary);
      boundary = buffer.indexOf("\n\n");
    }
  }
}

function handleSSEEvent(
  rawEvent: string,
  onSummary: (summary: ProjectProcessSummaryResponse) => void,
) {
  let event = "message";
  const data: string[] = [];
  for (const line of rawEvent.split(/\r?\n/)) {
    if (!line || line.startsWith(":")) continue;
    if (line.startsWith("event:")) {
      event = line.slice("event:".length).trim();
    } else if (line.startsWith("data:")) {
      data.push(line.slice("data:".length).trimStart());
    }
  }
  if (event !== "summary" || data.length === 0) return;
  onSummary(JSON.parse(data.join("\n")) as ProjectProcessSummaryResponse);
}
