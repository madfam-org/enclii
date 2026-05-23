import {
  Archive,
  Copy,
  GitFork,
  Github,
  Globe,
  Lock,
  Star,
} from "lucide-react";
import type { CompactProject } from "./project-card-compact";
import { stripGithubRemoteUrl } from "@/lib/github-repo";

function formatCount(n: number | undefined): string {
  if (n === undefined || n === null) return "";
  if (n < 1000) return String(n);
  if (n < 10000) return (n / 1000).toFixed(1).replace(/\.0$/, "") + "k";
  return Math.round(n / 1000) + "k";
}

interface ProjectCardRepoMetadataProps {
  project: CompactProject;
  repoSlug: string;
}

export function ProjectCardRepoMetadata({
  project,
  repoSlug,
}: ProjectCardRepoMetadataProps) {
  return (
    <>
      {project.gitRepo && (
        <a
          href={
            project.gitRepo.startsWith("http")
              ? project.gitRepo
              : `https://github.com/${project.gitRepo}`
          }
          target="_blank"
          rel="noopener noreferrer"
          className="text-muted-foreground hover:text-foreground relative z-10 mt-1.5 flex w-fit items-center gap-1.5 truncate text-xs transition-colors"
          aria-label={`Open ${repoSlug || project.gitRepo} on GitHub in a new tab`}
        >
          <Github className="h-3 w-3 shrink-0" />
          <span className="truncate">
            {stripGithubRemoteUrl(project.gitRepo)}
          </span>
          {project.repoMeta?.visibility === "private" && (
            <Lock
              className="text-status-warning h-3 w-3 shrink-0"
              aria-label="Private repository"
            />
          )}
          {project.repoMeta?.visibility === "public" && (
            <Globe
              className="text-status-success h-3 w-3 shrink-0"
              aria-label="Public repository"
            />
          )}
          {project.repoMeta?.visibility === "internal" && (
            <Lock
              className="text-status-info h-3 w-3 shrink-0"
              aria-label="Internal repository"
            />
          )}
          {project.repoMeta?.archived && (
            <Archive
              className="text-muted-foreground h-3 w-3 shrink-0"
              aria-label="Archived repository"
            />
          )}
          {project.repoMeta?.fork && (
            <GitFork
              className="text-muted-foreground h-3 w-3 shrink-0"
              aria-label="Forked repository"
            />
          )}
          {project.repoMeta?.isTemplate && (
            <Copy
              className="text-muted-foreground h-3 w-3 shrink-0"
              aria-label="Template repository"
            />
          )}
        </a>
      )}

      {project.repoMeta &&
        (project.repoMeta.language ||
          (project.repoMeta.stars && project.repoMeta.stars > 0) ||
          project.repoMeta.license) && (
          <div className="text-muted-foreground mt-1 flex items-center gap-2 text-[10px]">
            {project.repoMeta.language && (
              <span
                className="flex items-center gap-1"
                title={`Primary language: ${project.repoMeta.language}`}
              >
                <span
                  className="bg-muted-foreground h-2 w-2 shrink-0 rounded-full"
                  aria-hidden="true"
                />
                {project.repoMeta.language}
              </span>
            )}
            {project.repoMeta.stars !== undefined &&
              project.repoMeta.stars > 0 && (
                <span
                  className="flex items-center gap-0.5"
                  title={`${project.repoMeta.stars} stars`}
                >
                  <Star className="h-2.5 w-2.5 shrink-0" />
                  {formatCount(project.repoMeta.stars)}
                </span>
              )}
            {project.repoMeta.license && (
              <span
                className="border-border/40 bg-muted/30 rounded border px-1 py-px font-mono leading-none"
                title={`License: ${project.repoMeta.license}`}
              >
                {project.repoMeta.license}
              </span>
            )}
          </div>
        )}
    </>
  );
}
