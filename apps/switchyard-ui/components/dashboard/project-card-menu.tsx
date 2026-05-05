"use client";

import Link from "next/link";
import {
  Check,
  Copy,
  ExternalLink,
  FileText,
  GitMerge,
  Github,
  MoreHorizontal,
  Receipt,
  Settings,
  Webhook,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@enclii/ui-components/dropdown-menu";

interface ProjectCardMenuProps {
  projectId: string;
  projectName: string;
  projectSlug: string;
  firstServiceId?: string;
  githubRepoUrl?: string;
  copiedKey: string | null;
  onCopy: (key: string, value: string) => void;
}

// Kebab-menu of granular project actions for ProjectCardCompact. Lives in
// its own file so the parent stays under the 800-line lint cap, and so
// the menu surface is independently testable / replaceable. The trigger
// renders a 3-dot icon; items are a mix of internal Links + external
// anchors + copy actions.
export function ProjectCardMenu({
  projectId,
  projectName,
  projectSlug,
  firstServiceId,
  githubRepoUrl,
  copiedKey,
  onCopy,
}: ProjectCardMenuProps) {
  const logsHref = firstServiceId
    ? `/projects/${projectSlug}/services/${firstServiceId}/logs`
    : `/projects/${projectSlug}`;
  const slugCopied = copiedKey === `slug-${projectId}`;
  const idCopied = copiedKey === `id-${projectId}`;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-primary/40 -mr-1 rounded p-1 transition-colors focus-visible:outline-none focus-visible:ring-2"
        aria-label={`Actions for ${projectName}`}
      >
        <MoreHorizontal className="h-4 w-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuLabel className="text-xs">{projectName}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link href={`/projects/${projectSlug}`}>
            <ExternalLink className="mr-2 h-3.5 w-3.5" />
            Open project
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link href={`/projects/${projectSlug}/deployments`}>
            <GitMerge className="mr-2 h-3.5 w-3.5" />
            Deployments
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild disabled={!firstServiceId}>
          <Link href={logsHref}>
            <FileText className="mr-2 h-3.5 w-3.5" />
            Logs
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link href={`/projects/${projectSlug}/webhooks`}>
            <Webhook className="mr-2 h-3.5 w-3.5" />
            Webhooks
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link href={`/projects/${projectSlug}/billing`}>
            <Receipt className="mr-2 h-3.5 w-3.5" />
            Billing
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link href={`/projects/${projectSlug}/addons`}>
            <Settings className="mr-2 h-3.5 w-3.5" />
            Add-ons
          </Link>
        </DropdownMenuItem>
        {githubRepoUrl && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <a href={githubRepoUrl} target="_blank" rel="noopener noreferrer">
                <Github className="mr-2 h-3.5 w-3.5" />
                Open in GitHub
              </a>
            </DropdownMenuItem>
          </>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={(e) => {
            e.preventDefault();
            onCopy(`slug-${projectId}`, projectSlug);
          }}
        >
          {slugCopied ? (
            <Check className="text-status-success mr-2 h-3.5 w-3.5" />
          ) : (
            <Copy className="mr-2 h-3.5 w-3.5" />
          )}
          Copy slug
        </DropdownMenuItem>
        <DropdownMenuItem
          onSelect={(e) => {
            e.preventDefault();
            onCopy(`id-${projectId}`, projectId);
          }}
        >
          {idCopied ? (
            <Check className="text-status-success mr-2 h-3.5 w-3.5" />
          ) : (
            <Copy className="mr-2 h-3.5 w-3.5" />
          )}
          Copy ID
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
