"use client";

import { ExternalLink, GitBranch } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { formatRelativeTime } from "@/lib/formatting";
import type { CompactProject } from "./project-card-compact";

interface SidebarRecentPreviewsProps {
  projects: CompactProject[];
  className?: string;
}

interface PreviewItem {
  projectName: string;
  domain: string;
  branch: string;
  timestamp?: string;
}

export function SidebarRecentPreviews({
  projects,
  className,
}: SidebarRecentPreviewsProps) {
  // Extract services that have domains as preview URLs
  const previews: PreviewItem[] = projects
    .filter((p) => p.domain && p.lastDeployment)
    .map((p) => ({
      projectName: p.name,
      domain: p.domain!,
      branch: p.lastDeployment?.branch || "main",
      timestamp: p.lastDeployment?.timestamp,
    }))
    .sort((a, b) => {
      if (!a.timestamp || !b.timestamp) return 0;
      return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
    })
    .slice(0, 5);

  return (
    <Card className={cn(className)}>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">Recent Previews</CardTitle>
      </CardHeader>
      <CardContent>
        {previews.length === 0 ? (
          <p className="py-2 text-xs text-muted-foreground">
            No active previews
          </p>
        ) : (
          <div className="space-y-3">
            {previews.map((preview) => (
              <div key={preview.domain} className="space-y-0.5">
                <p className="truncate text-xs font-medium text-foreground">
                  {preview.projectName}
                </p>
                <a
                  href={`https://${preview.domain}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors truncate"
                >
                  <ExternalLink className="h-3 w-3 shrink-0" />
                  <span className="truncate">{preview.domain}</span>
                </a>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <div className="flex items-center gap-1">
                    <GitBranch className="h-3 w-3" />
                    <span className="truncate">{preview.branch}</span>
                  </div>
                  {preview.timestamp && (
                    <span className="shrink-0">
                      {formatRelativeTime(preview.timestamp)}
                    </span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
