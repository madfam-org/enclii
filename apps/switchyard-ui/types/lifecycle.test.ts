/**
 * Unit tests for types/lifecycle.ts
 *
 * Pure helpers only — no React, no fetch mocking. Mirrors the test
 * style of components/dashboard/framework-icon.test.ts.
 */

import {
  groupLifecycleEvents,
  lifecycleEventCategory,
  lifecycleEventLabel,
  shortSHA,
  type LifecycleEvent,
} from './lifecycle';

// ---------------------------------------------------------------------------
// shortSHA
// ---------------------------------------------------------------------------

describe('shortSHA', () => {
  it('truncates a long SHA to 7 chars', () => {
    expect(shortSHA('abcdef1234567890')).toBe('abcdef1');
  });

  it('returns the full string when shorter than 7', () => {
    expect(shortSHA('abc')).toBe('abc');
  });

  it('returns empty string for empty input', () => {
    expect(shortSHA('')).toBe('');
  });

  it('returns empty string for undefined-typed input', () => {
    // The function is typed as string but defensively handles falsy.
    expect(shortSHA(undefined as unknown as string)).toBe('');
  });
});

// ---------------------------------------------------------------------------
// lifecycleEventCategory
// ---------------------------------------------------------------------------

describe('lifecycleEventCategory', () => {
  it('classifies build_succeeded as success', () => {
    expect(lifecycleEventCategory('build_succeeded')).toBe('success');
  });

  it('classifies image_pushed as success', () => {
    expect(lifecycleEventCategory('image_pushed')).toBe('success');
  });

  it('classifies digest_committed as success', () => {
    expect(lifecycleEventCategory('digest_committed')).toBe('success');
  });

  it('classifies deploy_synced as success', () => {
    expect(lifecycleEventCategory('deploy_synced')).toBe('success');
  });

  it('classifies deploy_healthy as success', () => {
    expect(lifecycleEventCategory('deploy_healthy')).toBe('success');
  });

  it('classifies preview_created as success', () => {
    expect(lifecycleEventCategory('preview_created')).toBe('success');
  });

  it('classifies rollback success (deploy.rolled_back) as success', () => {
    expect(lifecycleEventCategory('deploy.rolled_back')).toBe('success');
  });

  it('classifies build_failed as failure', () => {
    expect(lifecycleEventCategory('build_failed')).toBe('failure');
  });

  it('classifies deploy_failed as failure', () => {
    expect(lifecycleEventCategory('deploy_failed')).toBe('failure');
  });

  it('classifies deploy_degraded as failure', () => {
    expect(lifecycleEventCategory('deploy_degraded')).toBe('failure');
  });

  it('classifies deploy.rollback_failed as failure', () => {
    expect(lifecycleEventCategory('deploy.rollback_failed')).toBe('failure');
  });

  it('classifies build_started as in_progress', () => {
    expect(lifecycleEventCategory('build_started')).toBe('in_progress');
  });

  it('classifies deploy_started as in_progress', () => {
    expect(lifecycleEventCategory('deploy_started')).toBe('in_progress');
  });

  it('classifies push_received as neutral', () => {
    expect(lifecycleEventCategory('push_received')).toBe('neutral');
  });

  it('classifies preview_destroyed as neutral', () => {
    expect(lifecycleEventCategory('preview_destroyed')).toBe('neutral');
  });

  it('classifies unknown event type as neutral', () => {
    expect(lifecycleEventCategory('something_weird')).toBe('neutral');
  });
});

// ---------------------------------------------------------------------------
// lifecycleEventLabel
// ---------------------------------------------------------------------------

describe('lifecycleEventLabel', () => {
  it('returns mapped label for known event types', () => {
    expect(lifecycleEventLabel('push_received')).toBe('Push received');
    expect(lifecycleEventLabel('build_started')).toBe('Build started');
    expect(lifecycleEventLabel('deploy_healthy')).toBe('Deploy healthy');
  });

  it('returns mapped label for rollback events', () => {
    expect(lifecycleEventLabel('deploy.rolled_back')).toBe('Rolled back');
    expect(lifecycleEventLabel('deploy.rollback_failed')).toBe('Rollback failed');
  });

  it('title-cases unknown event types as a fallback', () => {
    expect(lifecycleEventLabel('foo_bar_baz')).toBe('Foo Bar Baz');
  });

  it('handles unknown dot-separated event types', () => {
    expect(lifecycleEventLabel('foo.bar')).toBe('Foo Bar');
  });
});

// ---------------------------------------------------------------------------
// groupLifecycleEvents
// ---------------------------------------------------------------------------

function makeEvent(
  overrides: Partial<LifecycleEvent> & {
    id: string;
    commit_sha: string;
    created_at: string;
  },
): LifecycleEvent {
  return {
    id: overrides.id,
    repo_full_name: 'org/repo',
    commit_sha: overrides.commit_sha,
    branch: 'main',
    ref: 'refs/heads/main',
    event_type: 'push_received',
    source: 'github_webhook',
    created_at: overrides.created_at,
    ...overrides,
  };
}

describe('groupLifecycleEvents', () => {
  it('returns empty array for empty input', () => {
    expect(groupLifecycleEvents([])).toEqual([]);
  });

  it('groups contiguous events sharing the same git SHA', () => {
    const events: LifecycleEvent[] = [
      makeEvent({
        id: '1',
        commit_sha: 'aaa',
        created_at: '2026-04-26T12:03:00Z',
        event_type: 'deploy_healthy',
      }),
      makeEvent({
        id: '2',
        commit_sha: 'aaa',
        created_at: '2026-04-26T12:01:00Z',
        event_type: 'build_succeeded',
      }),
      makeEvent({
        id: '3',
        commit_sha: 'aaa',
        created_at: '2026-04-26T12:00:00Z',
        event_type: 'push_received',
      }),
    ];
    const groups = groupLifecycleEvents(events);
    expect(groups).toHaveLength(1);
    expect(groups[0].git_sha).toBe('aaa');
    expect(groups[0].events).toHaveLength(3);
    expect(groups[0].earliest_at).toBe('2026-04-26T12:00:00Z');
    expect(groups[0].latest_at).toBe('2026-04-26T12:03:00Z');
  });

  it('splits non-contiguous SHA runs into separate groups', () => {
    const events: LifecycleEvent[] = [
      makeEvent({
        id: '1',
        commit_sha: 'bbb',
        created_at: '2026-04-26T13:00:00Z',
      }),
      makeEvent({
        id: '2',
        commit_sha: 'aaa',
        created_at: '2026-04-26T12:00:00Z',
      }),
    ];
    const groups = groupLifecycleEvents(events);
    expect(groups).toHaveLength(2);
    expect(groups[0].git_sha).toBe('bbb');
    expect(groups[1].git_sha).toBe('aaa');
  });

  it('extracts pr_number / pr_title / pr_url / author from event metadata', () => {
    const events: LifecycleEvent[] = [
      makeEvent({
        id: '1',
        commit_sha: 'ccc',
        created_at: '2026-04-26T14:00:00Z',
        metadata: {
          pr_number: 42,
          pr_title: 'Fix login flow',
          pr_url: 'https://github.com/org/repo/pull/42',
          author: 'octocat',
        },
      }),
    ];
    const [g] = groupLifecycleEvents(events);
    expect(g.pr_number).toBe(42);
    expect(g.pr_title).toBe('Fix login flow');
    expect(g.pr_url).toBe('https://github.com/org/repo/pull/42');
    expect(g.author).toBe('octocat');
  });

  it('coerces stringified pr_number from metadata', () => {
    const events: LifecycleEvent[] = [
      makeEvent({
        id: '1',
        commit_sha: 'ccc',
        created_at: '2026-04-26T14:00:00Z',
        metadata: { pr_number: '99' },
      }),
    ];
    const [g] = groupLifecycleEvents(events);
    expect(g.pr_number).toBe(99);
  });

  it('backfills PR metadata from later events in the same group', () => {
    const events: LifecycleEvent[] = [
      // First event has no PR metadata
      makeEvent({
        id: '1',
        commit_sha: 'ddd',
        created_at: '2026-04-26T15:02:00Z',
      }),
      // Second event in the same group carries the PR info
      makeEvent({
        id: '2',
        commit_sha: 'ddd',
        created_at: '2026-04-26T15:00:00Z',
        metadata: { pr_number: 7, author: 'alice' },
      }),
    ];
    const [g] = groupLifecycleEvents(events);
    expect(g.pr_number).toBe(7);
    expect(g.author).toBe('alice');
  });

  it('treats missing commit_sha as "(unknown)" group key', () => {
    const events: LifecycleEvent[] = [
      makeEvent({
        id: '1',
        commit_sha: '',
        created_at: '2026-04-26T16:00:00Z',
      }),
    ];
    const [g] = groupLifecycleEvents(events);
    expect(g.git_sha).toBe('(unknown)');
  });
});
