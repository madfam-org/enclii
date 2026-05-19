"use client";

import { Fragment } from "react";
import Link from "next/link";
import { Check, Copy } from "lucide-react";
import { cn } from "@/lib/utils";
import { RolloutStateIndicator } from "./rollout-state-indicator";
import { ServiceLink, normalizeEnv } from "./service-link";
import { ServiceProcessIndicator } from "./project-process-feed";
import type { CompactService } from "./project-card-compact";

const MAX_VISIBLE_TABLE_ROWS = 5;

const serviceStatusColor: Record<string, string> = {
  running: "bg-status-success",
  failed: "bg-status-error",
  pending: "bg-status-warning",
  deploying: "bg-status-info animate-pulse",
  unknown: "bg-muted-foreground",
};

const serviceStatusLabel: Record<string, string> = {
  running: "Running",
  failed: "Failed",
  pending: "Pending",
  deploying: "Deploying",
  unknown: "Unknown",
};

export function ServiceStatusSummary({
  services,
}: {
  services: CompactService[];
}) {
  const counts: Record<string, number> = {};
  for (const s of services) {
    counts[s.status] = (counts[s.status] || 0) + 1;
  }
  const parts: string[] = [];
  if (counts.running) parts.push(`${counts.running} running`);
  if (counts.pending) parts.push(`${counts.pending} pending`);
  if (counts.deploying) parts.push(`${counts.deploying} deploying`);
  if (counts.failed) parts.push(`${counts.failed} failed`);
  if (counts.unknown) parts.push(`${counts.unknown} unknown`);
  return <>{parts.join(", ") || "No services"}</>;
}

interface ProjectCardServicesTableProps {
  copiedKey: string | null;
  onCopy: (key: string, value: string) => void;
  projectName: string;
  projectSlug: string;
  services: CompactService[];
  shortImageRef: (uri: string | undefined | null) => string;
}

export function ProjectCardServicesTable({
  copiedKey,
  onCopy,
  projectName,
  projectSlug,
  services,
  shortImageRef,
}: ProjectCardServicesTableProps) {
  if (services.length === 0) return null;

  const hasOverflow = services.length > MAX_VISIBLE_TABLE_ROWS;
  const visibleServices = hasOverflow
    ? services.slice(0, MAX_VISIBLE_TABLE_ROWS)
    : services;
  const overflowCount = services.length - MAX_VISIBLE_TABLE_ROWS;

  return (
    <div className="border-border/40 relative z-10 mt-2 overflow-hidden rounded border">
      <table className="w-full text-[11px]">
        <thead>
          <tr className="bg-muted/30 text-muted-foreground">
            <th className="py-1 pl-2 pr-1 text-left font-medium">Service</th>
            <th className="px-1 py-1 text-left font-medium">Status</th>
            <th className="px-1 py-1 text-right font-medium">Replicas</th>
            <th className="py-1 pl-1 pr-2 text-right font-medium">Env</th>
          </tr>
        </thead>
        <tbody className="divide-border/20 divide-y">
          {visibleServices.map((service) => (
            <ServiceRows
              copied={copiedKey === `digest-${service.id}`}
              key={service.id}
              onCopy={onCopy}
              projectSlug={projectSlug}
              service={service}
              shortImageRef={shortImageRef}
            />
          ))}
          {hasOverflow && (
            <tr>
              <td colSpan={4} className="py-0">
                <Link
                  href={`/projects/${projectSlug}#services`}
                  className="text-muted-foreground hover:bg-muted/30 hover:text-foreground block py-1 text-center text-[10px] transition-colors"
                  aria-label={`View all ${services.length} services for ${projectName}`}
                >
                  +{overflowCount} more service{overflowCount > 1 ? "s" : ""}
                </Link>
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

interface ServiceRowsProps {
  copied: boolean;
  onCopy: (key: string, value: string) => void;
  projectSlug: string;
  service: CompactService;
  shortImageRef: (uri: string | undefined | null) => string;
}

function ServiceRows({
  copied,
  onCopy,
  projectSlug,
  service,
  shortImageRef,
}: ServiceRowsProps) {
  const serviceHref = `/projects/${projectSlug}/services/${service.id}`;
  const logsHrefForRow = `${serviceHref}/logs`;
  const digestKey = `digest-${service.id}`;

  return (
    <Fragment>
      <tr className="hover:bg-muted/30 transition-colors">
        <td className="py-1 pl-2 pr-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <div
              className={cn(
                "h-1.5 w-1.5 shrink-0 rounded-full",
                serviceStatusColor[service.status] || "bg-muted-foreground",
              )}
            />
            <Link
              href={serviceHref}
              className="hover:text-primary max-w-[100px] truncate font-medium hover:underline"
              aria-label={`Open service ${service.name}`}
            >
              {service.name}
            </Link>
            {service.currentImageUri && (
              <button
                type="button"
                onClick={() => onCopy(digestKey, service.currentImageUri!)}
                className={cn(
                  "bg-muted/30 hidden shrink-0 rounded border px-1 py-0.5 font-mono text-[9px] leading-none transition-colors md:inline-flex md:items-center md:gap-1",
                  copied
                    ? "border-status-success/40 text-status-success"
                    : "border-border/40 text-muted-foreground hover:border-border hover:text-foreground",
                )}
                title={
                  copied ? "Copied!" : `Click to copy: ${service.currentImageUri}`
                }
                aria-label={
                  copied
                    ? "Image reference copied"
                    : `Copy running image reference: ${service.currentImageUri}`
                }
              >
                {shortImageRef(service.currentImageUri)}
                {copied ? (
                  <Check className="h-2 w-2" />
                ) : (
                  <Copy className="h-2 w-2 opacity-0 transition-opacity group-hover/card:opacity-60" />
                )}
              </button>
            )}
          </div>
        </td>
        <td className="px-1 py-1">
          <div className="flex min-w-0 items-center gap-1">
            <Link
              href={logsHrefForRow}
              className={cn(
                "inline-block rounded px-1 py-0.5 text-[10px] font-medium leading-none transition-colors hover:underline",
                service.status === "running" &&
                  "bg-status-success/15 text-status-success hover:bg-status-success/25",
                service.status === "failed" &&
                  "bg-status-error/15 text-status-error hover:bg-status-error/25",
                service.status === "pending" &&
                  "bg-status-warning/15 text-status-warning hover:bg-status-warning/25",
                service.status === "deploying" &&
                  "bg-status-info/15 text-status-info hover:bg-status-info/25 animate-pulse",
                service.status === "unknown" &&
                  "bg-muted text-muted-foreground hover:bg-muted/80",
              )}
              aria-label={`View ${service.name} logs (status: ${serviceStatusLabel[service.status] || "unknown"})`}
              title={`View logs — current status: ${serviceStatusLabel[service.status] || "unknown"}`}
            >
              {serviceStatusLabel[service.status] || "Unknown"}
            </Link>
            <RolloutStateIndicator
              state={service.rolloutState}
              reason={service.rolloutBlockedReason}
            />
            <ServiceProcessIndicator projectSlug={projectSlug} service={service} />
          </div>
        </td>
        <td className="text-muted-foreground px-1 py-1 text-right tabular-nums">
          {service.replicas ? (
            <Link
              href={serviceHref}
              className="hover:text-foreground hover:underline"
              aria-label={`${service.name} replicas: ${service.replicas} — open service`}
            >
              {service.replicas}
            </Link>
          ) : (
            "—"
          )}
        </td>
        <td className="text-muted-foreground max-w-[60px] truncate py-1 pl-1 pr-2 text-right">
          {service.environment || "—"}
        </td>
      </tr>
      {service.domain && (
        <tr className="hover:bg-muted/20 transition-colors">
          <td colSpan={4} className="py-1 pl-4 pr-2">
            <ServiceLink
              domain={service.domain}
              env={normalizeEnv(service.environment)}
              isHealthy={service.health !== "unhealthy"}
              ariaLabelService={service.name}
            />
          </td>
        </tr>
      )}
    </Fragment>
  );
}
