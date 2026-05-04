"use client";

import { useState } from "react";
import { Check, Clipboard, ExternalLink } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * ServiceLink — per-service deep-link element for project cards.
 *
 * Replaces the lossy project-level `domain` field that previously displayed
 * just the FIRST service's URL. Each service now gets its own row showing:
 *   [env badge]  url-preview  [↗ open]  [📋 copy]
 *
 * Design principles (from frontend-architect brief):
 * - Discoverability: external-link icon always visible, URL truncated but
 *   shown without hover
 * - Predictability: full URL on hover (title attr), opens new tab,
 *   noopener+noreferrer, descriptive aria-label
 * - Multi-env distinction: badge color encodes env (prod=neutral muted,
 *   staging=warning, preview=info, development=neutral)
 * - Status integration: `isHealthy === false` dims the row (opacity-60)
 *   but never colors the URL red — health is shown elsewhere on the card
 * - Copy: clipboard write + 1.5s checkmark swap, no toast
 * - Click handling: stopPropagation so clicks don't navigate the parent
 *   card Link
 * - Accessibility: focus-visible ring, screen-reader-only "(opens in new
 *   tab)" suffix
 * - Empty state: no domain → renders `[internal]` chip in muted color
 *   so layout stays predictable
 */

// Maximum URL preview length before ellipsis. Chosen to fit comfortably
// alongside the env badge + two icon buttons in the card's narrow column.
const URL_PREVIEW_MAX_LEN = 30;

// How long to show the checkmark after a successful copy before reverting
// to the clipboard icon. Matches typical "snackbar" perception window
// without needing an actual toast component.
const COPY_FEEDBACK_MS = 1500;

export type ServiceEnv =
  | "production"
  | "staging"
  | "preview"
  | "development";

export interface ServiceLinkProps {
  /** Bare domain, e.g. "api.example.com". Protocol is added automatically. */
  domain: string;
  /** Deployment environment — drives the badge variant. */
  env: ServiceEnv;
  /**
   * If false, the link is dimmed (opacity-60). The URL itself is never
   * colored red — operators read the URL as a destination, not as a
   * health signal. The HealthBadge elsewhere on the card communicates
   * health.
   */
  isHealthy?: boolean;
  /**
   * Service name, used to compose the aria-label
   * "Open <service> in <env>". Falls back to the domain when omitted so
   * screen readers still get a usable announcement.
   */
  ariaLabelService?: string;
  className?: string;
}

export interface ServiceLinkRowProps {
  /** One row per env. Order is preserved as given. */
  endpoints: Array<{ domain: string; env: ServiceEnv }>;
  /** Applied uniformly across all rows. See `ServiceLinkProps.isHealthy`. */
  isHealthy?: boolean;
  ariaLabelService?: string;
  className?: string;
}

/**
 * truncateUrl — right-truncation with ellipsis suffix. Pure helper.
 *
 * For URLs longer than max, keeps the leading host and appends "…".
 * Operators recognize hosts faster than paths, and CSS truncation in
 * the parent already handles narrow viewports — this helper guards
 * the wide-viewport case where we want a hard upper bound regardless
 * of container width.
 */
export function truncateUrl(url: string, max: number = URL_PREVIEW_MAX_LEN): string {
  if (!url) return "";
  if (url.length <= max) return url;
  return url.slice(0, max - 1) + "…";
}

/**
 * envBadgeClass — Tailwind classes for the env chip.
 *
 * Keeps colors aligned with existing service-status pills in the card:
 *   prod      → neutral muted (default destination, no special weight)
 *   staging   → warning (yellow) — operators must be careful not to
 *               confuse with prod
 *   preview   → info (blue) — ephemeral, PR-attached
 *   development → muted (grey) — local-ish, lowest weight
 *
 * Pure mapping, exported for tests.
 */
export function envBadgeClass(env: ServiceEnv): string {
  switch (env) {
    case "production":
      return "border-border/60 bg-muted/40 text-muted-foreground";
    case "staging":
      return "border-status-warning/30 bg-status-warning/15 text-status-warning";
    case "preview":
      return "border-status-info/30 bg-status-info/15 text-status-info";
    case "development":
      return "border-border/40 bg-muted/30 text-muted-foreground";
    default:
      // Exhaustiveness guard — never expected at runtime, but if a new env
      // type is added without a branch, fall back to neutral.
      return "border-border/60 bg-muted/40 text-muted-foreground";
  }
}

/**
 * envLabel — short uppercase label for the env chip.
 *
 * Pure helper, exported for tests.
 */
export function envLabel(env: ServiceEnv): string {
  switch (env) {
    case "production":
      return "prod";
    case "staging":
      return "staging";
    case "preview":
      return "preview";
    case "development":
      return "dev";
    default:
      return env;
  }
}

/**
 * ServiceLink — single service deep-link row.
 *
 * Use directly when a service has exactly one endpoint. For multi-env
 * services, prefer ServiceLinkRow which stacks rows uniformly.
 */
export function ServiceLink({
  domain,
  env,
  isHealthy = true,
  ariaLabelService,
  className,
}: ServiceLinkProps) {
  const [copied, setCopied] = useState(false);

  // Empty domain → render the [internal] chip instead of a broken link.
  // Predictable layout (always a row of fixed-ish height) is more useful
  // than a dynamic gap that operators have to learn around.
  if (!domain) {
    return (
      <div
        className={cn(
          "flex items-center gap-1.5 text-[10px] text-muted-foreground",
          className,
        )}
      >
        <span className="rounded-full border border-border/40 bg-muted/30 px-1.5 py-0.5 font-medium uppercase tracking-wide">
          internal
        </span>
      </div>
    );
  }

  const url = `https://${domain}`;
  const display = truncateUrl(domain);
  const labelSuffix = ariaLabelService ? `${ariaLabelService} in ${env}` : `${domain} in ${env}`;

  const handleCopy = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      window.setTimeout(() => setCopied(false), COPY_FEEDBACK_MS);
    } catch {
      // Browsers may reject clipboard writes outside secure contexts. We
      // intentionally don't surface an error toast — the parent card is
      // already information-dense, and the URL is visible+selectable so
      // the operator can fall back to manual selection. Future: consider
      // a discreet inline error chip if telemetry shows real failures.
    }
  };

  return (
    <div
      className={cn(
        "flex min-w-0 items-center gap-1.5 text-xs",
        !isHealthy && "opacity-60",
        className,
      )}
    >
      <span
        className={cn(
          "shrink-0 rounded-full border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide",
          envBadgeClass(env),
        )}
        aria-label={`Environment: ${env}`}
      >
        {envLabel(env)}
      </span>
      <a
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        title={url}
        aria-label={`Open ${labelSuffix} (opens in new tab)`}
        className="text-muted-foreground hover:text-foreground inline-flex min-w-0 items-center gap-1 truncate transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:rounded"
        onClick={(e) => e.stopPropagation()}
      >
        <span className="truncate">{display}</span>
        <ExternalLink className="h-3 w-3 shrink-0" aria-hidden="true" />
      </a>
      <button
        type="button"
        onClick={handleCopy}
        title={copied ? "Copied!" : "Copy URL"}
        aria-label={copied ? `Copied ${url}` : `Copy ${url} to clipboard`}
        className="text-muted-foreground hover:text-foreground inline-flex h-4 w-4 shrink-0 items-center justify-center rounded transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      >
        {copied ? (
          <Check className="h-3 w-3 text-status-success" aria-hidden="true" />
        ) : (
          <Clipboard className="h-3 w-3" aria-hidden="true" />
        )}
      </button>
    </div>
  );
}

/**
 * ServiceLinkRow — stacks one ServiceLink per endpoint.
 *
 * Designed for the future shape where the API returns
 * `domains: Array<{url, env}>` per service. For now most services have a
 * single endpoint and you can either wrap that in a single-element array
 * here, or use <ServiceLink> directly.
 *
 * Empty endpoints array → renders the same [internal] chip as the
 * single-link variant, preserving layout predictability.
 */
export function ServiceLinkRow({
  endpoints,
  isHealthy = true,
  ariaLabelService,
  className,
}: ServiceLinkRowProps) {
  if (!endpoints || endpoints.length === 0) {
    return (
      <ServiceLink
        domain=""
        env="production"
        isHealthy={isHealthy}
        ariaLabelService={ariaLabelService}
        className={className}
      />
    );
  }

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      {endpoints.map((ep) => (
        <ServiceLink
          key={`${ep.env}-${ep.domain}`}
          domain={ep.domain}
          env={ep.env}
          isHealthy={isHealthy}
          ariaLabelService={ariaLabelService}
        />
      ))}
    </div>
  );
}

/**
 * normalizeEnv — coerces an arbitrary string from the API
 * (`auto_deploy_env`) into a known ServiceEnv. Falls back to "production"
 * on empty/unknown values to match the old card behavior of showing a
 * domain link without env context.
 *
 * Discovered during this PR: `auto_deploy_env` is sometimes empty in the
 * API response (services that haven't been auto-deployed yet, or older
 * records pre-dating the field). The fallback is intentional — operators
 * still get a working link rather than `[internal]` masking a real URL.
 *
 * Pure helper, exported for tests.
 */
export function normalizeEnv(raw: string | undefined | null): ServiceEnv {
  if (!raw) return "production";
  const v = raw.toLowerCase().trim();
  if (v === "production" || v === "prod") return "production";
  if (v === "staging" || v === "stage") return "staging";
  if (v === "preview") return "preview";
  if (v === "development" || v === "dev") return "development";
  return "production";
}
