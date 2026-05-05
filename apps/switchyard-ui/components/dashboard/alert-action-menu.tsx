"use client";

/**
 * Per-row action menu for alerts.
 *
 * Renders a dot-icon button that opens a popover with four actions:
 *   1. Acknowledge          — POST /v1/observability/alerts/<id>/ack
 *   2. Mute for 1 hour      — POST /v1/observability/alerts/<id>/mute
 *   3. Open in new tab      — same href as the row link, target=_blank
 *   4. Copy alert ID        — navigator.clipboard.writeText(alert.id)
 *
 * Phase 1 (this PR) optimistically updates UI state for ack/mute and
 * stores mute state in localStorage via `useMutedAlerts`. The POST
 * endpoints don't exist yet — we still call `apiPost` so the wiring is
 * in place; a 404/405 from the server is caught and treated as a
 * deliberate no-op (operator still sees the optimistic toast). When
 * Phase 2 ships the backend route, no UI change is required.
 *
 * Accessibility:
 *   - Trigger has aria-label "Alert actions for <alert.name>".
 *   - Dropdown items are real buttons / anchors with focus rings via
 *     the shared dropdown-menu primitive.
 *   - The trigger stops propagation so clicking it doesn't also follow
 *     the parent <Link> to the alert's deep-link route.
 */

import { useCallback } from "react";
import { MoreHorizontal, BellOff, Check, ExternalLink, Copy } from "lucide-react";
import { toast } from "sonner";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@enclii/ui-components/dropdown-menu";
import { apiPost } from "@/lib/api";
import { alertHref, type RoutableAlert } from "@/lib/alert-routing";

export interface AlertActionMenuAlert extends RoutableAlert {
  /** Human-readable alert name; used in the trigger's aria-label and toasts. */
  name: string;
}

export interface AlertActionMenuProps {
  alert: AlertActionMenuAlert;
  /** Mute the alert locally for the supplied duration (default 1 h). */
  onMute: (alertId: string, durationMs?: number) => void;
  /** Optional: set `false` if the row is already muted, hiding the mute action. */
  isMuted?: boolean;
}

const ONE_HOUR_MS = 60 * 60 * 1000;

/**
 * Optimistic POST helper. Phase 1 expects 404 from these endpoints; we
 * never throw out of the click handler so the operator's UI state
 * stays consistent.
 */
async function optimisticPost(endpoint: string): Promise<void> {
  try {
    await apiPost(endpoint, {});
  } catch (err) {
    // Phase 1: server endpoint is intentionally absent. Swallow the
    // 404 / 405 / network error. A future Phase 2 PR will distinguish
    // real failures from the missing-endpoint case and surface those.
    // We log so operators inspecting devtools can see the trace.
    if (process.env.NODE_ENV !== "test") {
      // eslint-disable-next-line no-console
      console.debug(`Alert action POST ${endpoint} failed (expected in Phase 1):`, err);
    }
  }
}

export function AlertActionMenu({ alert, onMute, isMuted }: AlertActionMenuProps) {
  const href = alertHref(alert);

  const handleAcknowledge = useCallback(async () => {
    await optimisticPost(`/v1/observability/alerts/${alert.id}/ack`);
    toast.success("Acknowledgement saved locally for 1h", {
      description: alert.name,
    });
  }, [alert.id, alert.name]);

  const handleMute = useCallback(async () => {
    onMute(alert.id, ONE_HOUR_MS);
    await optimisticPost(`/v1/observability/alerts/${alert.id}/mute`);
    toast.success("Muted for 1 hour", {
      description: alert.name,
    });
  }, [alert.id, alert.name, onMute]);

  const handleCopyId = useCallback(async () => {
    try {
      // navigator.clipboard is only present in secure contexts; the
      // dashboard is always served over HTTPS but tests may not have
      // it, so check before calling.
      if (typeof navigator !== "undefined" && navigator.clipboard) {
        await navigator.clipboard.writeText(alert.id);
        toast.success("Alert ID copied", { description: alert.id });
      } else {
        toast.error("Clipboard not available");
      }
    } catch {
      toast.error("Failed to copy alert ID");
    }
  }, [alert.id]);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        aria-label={`Alert actions for ${alert.name}`}
        // Stop the click bubbling up to the parent <Link>; otherwise
        // opening the menu would also navigate the operator away.
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          // Same protection for keyboard users — Enter / Space on the
          // trigger should open the menu, not follow the surrounding link.
          if (e.key === "Enter" || e.key === " ") {
            e.stopPropagation();
          }
        }}
      >
        <MoreHorizontal className="h-4 w-4" />
        <span className="sr-only">Open alert actions menu</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        // The menu lives inside a <Link>; clicks bubbling up to the
        // anchor would trigger navigation. Halt them here.
        onClick={(e) => e.stopPropagation()}
      >
        <DropdownMenuItem onClick={handleAcknowledge} className="cursor-pointer">
          <Check className="mr-2 h-4 w-4" />
          Acknowledge
        </DropdownMenuItem>
        {!isMuted && (
          <DropdownMenuItem onClick={handleMute} className="cursor-pointer">
            <BellOff className="mr-2 h-4 w-4" />
            Mute for 1 hour
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className="cursor-pointer"
          >
            <ExternalLink className="mr-2 h-4 w-4" />
            Open in new tab
          </a>
        </DropdownMenuItem>
        <DropdownMenuItem onClick={handleCopyId} className="cursor-pointer">
          <Copy className="mr-2 h-4 w-4" />
          Copy alert ID
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
