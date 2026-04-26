'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { ArrowLeft, GitMerge } from 'lucide-react';
import { apiGet } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import { Button } from '@/components/ui/button';
import {
  DeploymentTimeline,
  TimelineFilters,
} from '@/components/deployments/timeline';
import { LastSyncBadge } from '@/components/dashboard/last-sync-badge';
import { useLifecycleEvents } from '@/hooks/use-lifecycle-events';
import {
  lifecycleEventCategory,
  type LifecycleEventType,
  type LifecycleResultFilter,
  type LifecycleSinceOption,
} from '@/types/lifecycle';

interface ApiProject {
  id: string;
  name: string;
  slug: string;
  description?: string;
}

interface ApiService {
  id: string;
  name: string;
  git_repo: string;
}

/**
 * Extract `owner/repo` from a git_repo URL like:
 *   https://github.com/madfam-org/dhanam   → "madfam-org/dhanam"
 *   git@github.com:org/repo.git            → "org/repo"
 *   madfam-org/dhanam                      → "madfam-org/dhanam"
 */
function deriveRepoFullName(gitRepo: string | undefined): string | undefined {
  if (!gitRepo) return undefined;
  const trimmed = gitRepo.replace(/\.git$/, '');
  const httpsMatch = trimmed.match(/github\.com[/:]([^/]+\/[^/]+)/i);
  if (httpsMatch) return httpsMatch[1];
  // Already in owner/repo form?
  if (/^[^/]+\/[^/]+$/.test(trimmed)) return trimmed;
  return undefined;
}

/**
 * Per-project deployment timeline — closes parity gap #1.
 *
 * Fetches lifecycle events for the project's first service git repo
 * (most projects are single-repo) and renders them grouped by git SHA.
 */
export default function ProjectDeploymentsPage() {
  const params = useParams();
  const slug = params?.slug as string;

  const [project, setProject] = useState<ApiProject | null>(null);
  const [services, setServices] = useState<ApiService[]>([]);
  const [bootError, setBootError] = useState<string | null>(null);

  const [eventTypes, setEventTypes] = useState<LifecycleEventType[]>([]);
  const [since, setSince] = useState<LifecycleSinceOption>('7d');
  const [result, setResult] = useState<LifecycleResultFilter>('all');

  // Bootstrap project + services so we can derive repoFullName.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const projectData = await apiGet<ApiProject>(`/v1/projects/${slug}`);
        if (cancelled) return;
        setProject(projectData);
        const svcData = await apiGet<{ services: ApiService[] }>(
          `/v1/projects/${projectData.slug}/services`,
        );
        if (cancelled) return;
        setServices(svcData.services || []);
      } catch (e) {
        if (cancelled) return;
        setBootError(e instanceof Error ? e.message : 'Failed to load project');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [slug]);

  // Pick the first service's repo. Most projects are single-repo today;
  // we surface a service picker only when there are multiple.
  const [activeServiceId, setActiveServiceId] = useState<string | null>(null);
  const activeService =
    services.find((s) => s.id === activeServiceId) || services[0] || null;
  const repoFullName = deriveRepoFullName(activeService?.git_repo);

  const { events, loading, error, lastSyncedAt, refresh } = useLifecycleEvents(
    {
      repoFullName,
      eventTypes,
      since,
      limit: 50,
    },
  );

  const filteredEvents = useMemo(() => {
    if (result === 'all') return events;
    return events.filter((e) => {
      const cat = lifecycleEventCategory(e.event_type);
      if (result === 'success') return cat === 'success';
      if (result === 'failure') return cat === 'failure';
      return true;
    });
  }, [events, result]);

  if (bootError) {
    return (
      <div className="mx-auto max-w-5xl px-4 py-10">
        <p className="text-status-error">{bootError}</p>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 sm:px-6 lg:px-8">
      {/* Header */}
      <div className="mb-4 flex items-center gap-2">
        <Link
          href={`/projects/${slug}`}
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft aria-hidden="true" className="h-4 w-4" />
          Back to project
        </Link>
      </div>
      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
            <GitMerge aria-hidden="true" className="h-5 w-5" />
            Deployments
          </h1>
          <p className="text-sm text-muted-foreground">
            {project?.name
              ? `Lifecycle events for ${project.name}`
              : 'Lifecycle events'}
            {repoFullName && (
              <>
                {' · '}
                <span className="font-mono text-xs">{repoFullName}</span>
              </>
            )}
          </p>
        </div>
        <LastSyncBadge
          lastSyncedAt={lastSyncedAt}
          onRefresh={refresh}
          refreshing={loading}
        />
      </div>

      {/* Service picker (only shown for multi-service projects) */}
      {services.length > 1 && (
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <label
            htmlFor="timeline-service"
            className="text-xs font-medium text-muted-foreground"
          >
            Service
          </label>
          <select
            id="timeline-service"
            value={activeService?.id || ''}
            onChange={(e) => setActiveServiceId(e.target.value)}
            className="rounded border border-border bg-background px-2 py-1 text-xs"
          >
            {services.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
      )}

      {/* Filters */}
      <TimelineFilters
        selectedEventTypes={eventTypes}
        onEventTypesChange={setEventTypes}
        since={since}
        onSinceChange={setSince}
        result={result}
        onResultChange={setResult}
        className="mb-4"
      />

      {/* Body */}
      {!repoFullName ? (
        <p className="rounded border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
          This project has no git_repo we can resolve to a GitHub repo. Add a
          service with a github.com URL to see its deployment timeline.
        </p>
      ) : error ? (
        <div className="rounded border border-status-error/30 bg-status-error-muted p-4 text-sm">
          <p className="text-status-error">{error}</p>
          <Button
            variant="outline"
            size="sm"
            className="mt-2"
            onClick={refresh}
          >
            Try again
          </Button>
        </div>
      ) : loading && events.length === 0 ? (
        <div className="flex items-center justify-center py-16">
          <Spinner size="lg" />
        </div>
      ) : (
        <DeploymentTimeline
          events={filteredEvents}
          repoFullName={repoFullName}
        />
      )}
    </div>
  );
}
