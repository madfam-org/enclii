'use client';

// Canary rollout visualization + controls (P2.7).
//
// The page polls /v1/services/:id/canary/:rollout_id every 5 seconds and
// displays:
//   - A progress timeline (pending → running → validating → promoting → done)
//   - The current stable vs canary replica split (animated gauge-style)
//   - A countdown remaining in the validation window
//   - "Promote now" / "Rollback now" buttons in validating state
//
// `rollout_id` is read from the ?rollout= query param. If absent, the page
// falls back to the latest rollout for the service.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useParams, useSearchParams } from "next/navigation";
import Link from "next/link";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import { apiGet, apiPost } from "@/lib/api";

// -------------------------------------------------------------------------
// Types — mirror the API response shape. Kept inline to avoid a new shared
// types file for a single page.
// -------------------------------------------------------------------------

type RolloutState =
  | "pending"
  | "running"
  | "validating"
  | "promoting"
  | "succeeded"
  | "auto_rolled_back"
  | "manual_rolled_back"
  | "failed";

interface CanaryRollout {
  id: string;
  service_id: string;
  environment_id: string;
  canary_digest: string;
  canary_percentage: number;
  total_replicas: number;
  canary_replicas: number;
  stable_replicas: number;
  validation_window_seconds: number;
  smoke_endpoint?: string;
  error_rate_threshold: number;
  state: RolloutState;
  started_at?: string;
  validating_started_at?: string;
  promoting_started_at?: string;
  terminal_at?: string;
  change_ticket_url?: string;
  last_error?: string;
  rollback_reason?: string;
  created_at: string;
  updated_at: string;
  // Joined/enriched
  actual_percentage?: number;
}

interface RolloutsListResponse {
  rollouts: CanaryRollout[];
}

// Timeline stages, ordered. Terminal states collapse into "done".
const TIMELINE_STAGES: { key: RolloutState; label: string }[] = [
  { key: "pending", label: "Pending" },
  { key: "running", label: "Running" },
  { key: "validating", label: "Validating" },
  { key: "promoting", label: "Promoting" },
  { key: "succeeded", label: "Done" },
];

function isTerminal(s: RolloutState): boolean {
  return (
    s === "succeeded" ||
    s === "auto_rolled_back" ||
    s === "manual_rolled_back" ||
    s === "failed"
  );
}

function stageIndex(state: RolloutState): number {
  if (isTerminal(state)) return 4; // collapse to final stage
  const i = TIMELINE_STAGES.findIndex((s) => s.key === state);
  return i < 0 ? 0 : i;
}

function stateBadgeVariant(state: RolloutState): "default" | "secondary" | "destructive" | "outline" {
  switch (state) {
    case "succeeded":
      return "default";
    case "auto_rolled_back":
    case "manual_rolled_back":
    case "failed":
      return "destructive";
    case "validating":
    case "promoting":
      return "secondary";
    default:
      return "outline";
  }
}

// -------------------------------------------------------------------------
// Main page
// -------------------------------------------------------------------------

export default function CanaryPage() {
  const params = useParams();
  const searchParams = useSearchParams();
  const serviceId = params.id as string;
  const rolloutIdParam = searchParams.get("rollout");

  const [rollout, setRollout] = useState<CanaryRollout | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionPending, setActionPending] = useState<"promote" | "rollback" | null>(null);

  const fetchRollout = useCallback(async () => {
    try {
      let target = rolloutIdParam;
      if (!target) {
        // Fetch list and pick the latest.
        const list = await apiGet<RolloutsListResponse>(`/v1/services/${serviceId}/canary`);
        if (!list.rollouts?.length) {
          setRollout(null);
          setLoading(false);
          setError(null);
          return;
        }
        target = list.rollouts[0].id;
      }
      const data = await apiGet<CanaryRollout>(`/v1/services/${serviceId}/canary/${target}`);
      setRollout(data);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load rollout");
    } finally {
      setLoading(false);
    }
  }, [serviceId, rolloutIdParam]);

  useEffect(() => {
    fetchRollout();
    const id = setInterval(fetchRollout, 5000);
    return () => clearInterval(id);
  }, [fetchRollout]);

  const handlePromote = async () => {
    if (!rollout) return;
    if (!confirm("Promote this canary now? This will short-circuit the validation window.")) return;
    setActionPending("promote");
    try {
      await apiPost(`/v1/services/${serviceId}/canary/${rollout.id}/promote`, {});
      await fetchRollout();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Promote failed");
    } finally {
      setActionPending(null);
    }
  };

  const handleRollback = async () => {
    if (!rollout) return;
    const reason = prompt("Rollback reason (optional):") ?? "";
    if (!confirm("Roll back this canary now? Traffic returns to 100% stable immediately.")) return;
    setActionPending("rollback");
    try {
      await apiPost(`/v1/services/${serviceId}/canary/${rollout.id}/rollback`, { reason });
      await fetchRollout();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Rollback failed");
    } finally {
      setActionPending(null);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Spinner />
      </div>
    );
  }

  if (error && !rollout) {
    return (
      <Card className="mx-auto max-w-2xl">
        <CardHeader>
          <CardTitle>Canary rollout</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-destructive">{error}</p>
          <Link href={`/services/${serviceId}`} className="mt-4 inline-block underline">
            Back to service
          </Link>
        </CardContent>
      </Card>
    );
  }

  if (!rollout) {
    return (
      <Card className="mx-auto max-w-2xl">
        <CardHeader>
          <CardTitle>No canary rollouts yet</CardTitle>
          <CardDescription>
            Start one with <code className="rounded bg-muted px-1">enclii deploy --canary=20%</code>.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <div className="mx-auto max-w-5xl space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Canary rollout</h1>
          <p className="text-sm text-muted-foreground">
            Rollout <code>{rollout.id.slice(0, 8)}</code> ·{" "}
            <Badge variant={stateBadgeVariant(rollout.state)}>{rollout.state}</Badge>
          </p>
        </div>
        <Link href={`/services/${serviceId}`} className="text-sm underline">
          ← Service
        </Link>
      </div>

      <RolloutTimeline rollout={rollout} />
      <TrafficSplit rollout={rollout} />
      <ValidationInfo rollout={rollout} />

      {rollout.state === "validating" || rollout.state === "running" ? (
        <Card>
          <CardHeader>
            <CardTitle>Operator actions</CardTitle>
            <CardDescription>
              Promote now to skip the remaining validation window, or roll back to abort the rollout.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex gap-3">
            <Button onClick={handlePromote} disabled={actionPending !== null} variant="default">
              {actionPending === "promote" ? "Promoting..." : "Promote now"}
            </Button>
            <Button onClick={handleRollback} disabled={actionPending !== null} variant="destructive">
              {actionPending === "rollback" ? "Rolling back..." : "Rollback now"}
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {rollout.last_error && (
        <Card>
          <CardHeader>
            <CardTitle className="text-destructive">Last error</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="whitespace-pre-wrap text-sm">{rollout.last_error}</pre>
          </CardContent>
        </Card>
      )}

      {rollout.rollback_reason && (
        <Card>
          <CardHeader>
            <CardTitle>Rollback reason</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm">{rollout.rollback_reason}</p>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

// -------------------------------------------------------------------------
// Sub-components
// -------------------------------------------------------------------------

function RolloutTimeline({ rollout }: { rollout: CanaryRollout }) {
  const current = stageIndex(rollout.state);
  const isFailed =
    rollout.state === "auto_rolled_back" ||
    rollout.state === "manual_rolled_back" ||
    rollout.state === "failed";

  return (
    <Card>
      <CardHeader>
        <CardTitle>Timeline</CardTitle>
      </CardHeader>
      <CardContent>
        <ol className="flex items-center justify-between gap-2">
          {TIMELINE_STAGES.map((stage, i) => {
            const active = i <= current;
            const isCurrent = i === current;
            return (
              <li key={stage.key} className="flex flex-1 flex-col items-center">
                <div
                  className={`flex h-8 w-8 items-center justify-center rounded-full border-2 text-xs font-semibold ${
                    active
                      ? isFailed && isCurrent
                        ? "border-destructive bg-destructive text-destructive-foreground"
                        : "border-primary bg-primary text-primary-foreground"
                      : "border-muted-foreground/30 text-muted-foreground"
                  }`}
                >
                  {i + 1}
                </div>
                <span
                  className={`mt-1 text-xs ${
                    active ? "font-medium" : "text-muted-foreground"
                  }`}
                >
                  {stage.label}
                </span>
              </li>
            );
          })}
        </ol>
      </CardContent>
    </Card>
  );
}

function TrafficSplit({ rollout }: { rollout: CanaryRollout }) {
  const stablePct = (rollout.stable_replicas / rollout.total_replicas) * 100;
  const canaryPct = (rollout.canary_replicas / rollout.total_replicas) * 100;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Traffic split</CardTitle>
        <CardDescription>
          {rollout.canary_replicas} canary pod{rollout.canary_replicas === 1 ? "" : "s"} +{" "}
          {rollout.stable_replicas} stable pod{rollout.stable_replicas === 1 ? "" : "s"} ={" "}
          {rollout.total_replicas} total. Requested {rollout.canary_percentage}%, actual{" "}
          {(rollout.actual_percentage ?? canaryPct).toFixed(1)}%.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="relative h-8 overflow-hidden rounded-md border bg-background">
          <div
            className="absolute left-0 top-0 h-full bg-primary/70 transition-all duration-700"
            style={{ width: `${stablePct}%` }}
            title={`Stable: ${rollout.stable_replicas} pods`}
          />
          <div
            className="absolute right-0 top-0 h-full bg-amber-500 transition-all duration-700"
            style={{ width: `${canaryPct}%` }}
            title={`Canary: ${rollout.canary_replicas} pods`}
          />
        </div>
        <div className="mt-2 flex justify-between text-xs text-muted-foreground">
          <span>Stable {stablePct.toFixed(0)}%</span>
          <span>Canary {canaryPct.toFixed(0)}%</span>
        </div>
      </CardContent>
    </Card>
  );
}

function ValidationInfo({ rollout }: { rollout: CanaryRollout }) {
  // Live countdown during validation.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const remaining = useMemo(() => {
    if (rollout.state !== "validating" || !rollout.validating_started_at) return null;
    const started = new Date(rollout.validating_started_at).getTime();
    const elapsedSec = Math.max(0, Math.floor((now - started) / 1000));
    const remainingSec = Math.max(0, rollout.validation_window_seconds - elapsedSec);
    return { elapsedSec, remainingSec };
  }, [rollout, now]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Validation</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        <div className="grid grid-cols-2 gap-x-4 gap-y-1">
          <span className="text-muted-foreground">Validation window</span>
          <span>{Math.floor(rollout.validation_window_seconds / 60)} min</span>
          <span className="text-muted-foreground">Error rate threshold</span>
          <span>{(rollout.error_rate_threshold * 100).toFixed(1)}%</span>
          {rollout.smoke_endpoint && (
            <>
              <span className="text-muted-foreground">Smoke endpoint</span>
              <span className="truncate font-mono text-xs">{rollout.smoke_endpoint}</span>
            </>
          )}
          {rollout.change_ticket_url && (
            <>
              <span className="text-muted-foreground">Change ticket</span>
              <a
                className="truncate text-xs underline"
                href={rollout.change_ticket_url}
                target="_blank"
                rel="noopener noreferrer"
              >
                {rollout.change_ticket_url}
              </a>
            </>
          )}
        </div>
        {remaining && (
          <div className="rounded-md border bg-muted/50 p-3">
            <div className="text-xs text-muted-foreground">Remaining in validation window</div>
            <div className="font-mono text-lg">
              {Math.floor(remaining.remainingSec / 60)}m {remaining.remainingSec % 60}s
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
