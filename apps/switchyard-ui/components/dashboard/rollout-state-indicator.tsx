import { AlertTriangle, Loader2 } from "lucide-react";
import { rolloutBlockedReasonLabel, type RolloutState } from "./rollout-state";

export function RolloutStateIndicator({
  state,
  reason,
}: {
  state?: RolloutState;
  reason?: string;
}) {
  if (!state || state === "ok") return null;

  if (state === "blocked") {
    return (
      <span
        className="inline-flex items-center gap-1 rounded-full border border-status-error/40 bg-status-error/10 px-1.5 py-0.5 text-[9px] font-medium text-status-error transition-colors hover:bg-status-error/20"
        title={rolloutBlockedReasonLabel(reason)}
        aria-label={`Rollout blocked: ${rolloutBlockedReasonLabel(reason)}`}
      >
        <AlertTriangle className="h-2.5 w-2.5 shrink-0" />
        Blocked
      </span>
    );
  }

  return (
    <span
      className="inline-flex items-center gap-1 rounded-full border border-status-info/40 bg-status-info/10 px-1.5 py-0.5 text-[9px] font-medium text-status-info transition-colors"
      title="New ReplicaSet still rolling out"
      aria-label="Rollout in progress"
    >
      <Loader2 className="h-2.5 w-2.5 animate-spin shrink-0" />
      Rolling
    </span>
  );
}
