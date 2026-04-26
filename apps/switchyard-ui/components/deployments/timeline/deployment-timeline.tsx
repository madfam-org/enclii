'use client';

import { GitBranch, GitCommit, User } from 'lucide-react';
import { Card } from '@/components/ui/card';
import { cn } from '@/lib/utils';
import { formatRelativeTime, formatFullTimestamp } from '@/lib/formatting';
import {
  groupLifecycleEvents,
  shortSHA,
  type LifecycleEvent,
} from '@/types/lifecycle';
import { TimelineEvent } from './timeline-event';

interface DeploymentTimelineProps {
  events: LifecycleEvent[];
  /** "owner/repo" — used to link to GitHub for commits/PRs. */
  repoFullName?: string;
  className?: string;
}

function ghCommitURL(repoFullName: string | undefined, sha: string): string | null {
  if (!repoFullName || !sha) return null;
  return `https://github.com/${repoFullName}/commit/${sha}`;
}

function ghPRURL(
  repoFullName: string | undefined,
  prNumber: number | undefined,
): string | null {
  if (!repoFullName || !prNumber) return null;
  return `https://github.com/${repoFullName}/pull/${prNumber}`;
}

/**
 * Pure rendering component. The parent owns data fetching and filter
 * state; this component just groups + lays out the timeline.
 *
 * Accessibility:
 *   - Each deployment group is a <section> with an <h3> header.
 *   - Events render inside an <ol role="list"> for screen readers and
 *     keyboard navigation.
 *   - Mobile: groups stack tighter and time chips wrap.
 */
export function DeploymentTimeline({
  events,
  repoFullName,
  className,
}: DeploymentTimelineProps) {
  if (events.length === 0) {
    return (
      <Card
        className={cn(
          'border-dashed bg-transparent p-10 text-center',
          className,
        )}
      >
        <p className="text-sm text-muted-foreground">
          No deployment events match the current filters.
        </p>
      </Card>
    );
  }

  const groups = groupLifecycleEvents(events);

  return (
    <div className={cn('space-y-4', className)}>
      {groups.map((group) => {
        const commitURL = ghCommitURL(repoFullName, group.git_sha);
        const prURL = group.pr_url || ghPRURL(repoFullName, group.pr_number);
        const headline =
          group.pr_title ||
          (group.pr_number ? `PR #${group.pr_number}` : null) ||
          shortSHA(group.git_sha) ||
          'Unknown deployment';

        return (
          <Card key={group.key} className="overflow-hidden p-4 sm:p-5">
            <section aria-labelledby={`deployment-group-${group.key}`}>
              <header className="mb-3 flex flex-wrap items-start justify-between gap-2 border-b border-border/50 pb-3">
                <div className="min-w-0">
                  <h3
                    id={`deployment-group-${group.key}`}
                    className="truncate text-sm font-semibold text-foreground"
                  >
                    {prURL ? (
                      <a
                        href={prURL}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="hover:text-primary hover:underline"
                      >
                        {headline}
                      </a>
                    ) : (
                      headline
                    )}
                  </h3>
                  <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                    {group.branch && (
                      <span className="inline-flex items-center gap-1">
                        <GitBranch
                          aria-hidden="true"
                          className="h-3 w-3 shrink-0"
                        />
                        <span className="max-w-[180px] truncate">
                          {group.branch}
                        </span>
                      </span>
                    )}
                    {group.git_sha && (
                      <span className="inline-flex items-center gap-1">
                        <GitCommit
                          aria-hidden="true"
                          className="h-3 w-3 shrink-0"
                        />
                        {commitURL ? (
                          <a
                            href={commitURL}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="font-mono hover:text-foreground hover:underline"
                            title={group.git_sha}
                          >
                            {shortSHA(group.git_sha)}
                          </a>
                        ) : (
                          <span className="font-mono" title={group.git_sha}>
                            {shortSHA(group.git_sha)}
                          </span>
                        )}
                      </span>
                    )}
                    {group.author && (
                      <span className="inline-flex items-center gap-1">
                        <User aria-hidden="true" className="h-3 w-3 shrink-0" />
                        <span className="max-w-[140px] truncate">
                          {group.author}
                        </span>
                      </span>
                    )}
                  </div>
                </div>
                <time
                  className="shrink-0 text-xs text-muted-foreground"
                  dateTime={group.latest_at}
                  title={formatFullTimestamp(group.latest_at)}
                >
                  {formatRelativeTime(group.latest_at)}
                </time>
              </header>

              <ol role="list" className="space-y-0">
                {group.events.map((event, idx) => (
                  <TimelineEvent
                    key={event.id}
                    event={event}
                    previousEvent={group.events[idx + 1]}
                    isFirst={idx === 0}
                    isLast={idx === group.events.length - 1}
                  />
                ))}
              </ol>
            </section>
          </Card>
        );
      })}
    </div>
  );
}
