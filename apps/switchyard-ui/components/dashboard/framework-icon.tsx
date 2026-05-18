"use client";

import { cn } from "@/lib/utils";
import {
  catalog,
  getOrUnknown,
  type Framework,
} from "@/lib/frameworks/catalog";

// FrameworkType is retained for backwards compatibility with callers
// that use it as a type constraint. The canonical identifier is the
// catalog slug (string); FrameworkType is a narrowed subset.
export type FrameworkType =
  | "nextjs"
  | "react"
  | "vue"
  | "nuxt"
  | "nuxtjs"
  | "angular"
  | "svelte"
  | "sveltekit"
  | "astro"
  | "nestjs"
  | "vite"
  | "express"
  | "fastify"
  | "fastapi"
  | "flask"
  | "django"
  | "rails"
  | "phoenix"
  | "go"
  | "go-fiber"
  | "go-gin"
  | "go-chi"
  | "go-echo"
  | "go-stdlib"
  | "rust"
  | "rust-actix"
  | "rust-axum"
  | "node"
  | "python"
  | "dockerfile"
  | "static"
  | "unknown";

interface FrameworkIconProps {
  framework: FrameworkType | string;
  size?: "sm" | "md" | "lg";
  showLabel?: boolean;
  className?: string;
}

const sizes: Record<"sm" | "md" | "lg", string> = {
  sm: "w-4 h-4",
  md: "w-6 h-6",
  lg: "w-8 h-8",
};

// Legacy aliases: keep old slugs rendering correctly after the
// catalog rename (nuxt → nuxtjs, svelte → sveltekit, go → go-stdlib, etc.).
const SLUG_ALIASES: Record<string, string> = {
  nuxt: "nuxtjs",
  svelte: "sveltekit",
  go: "go-stdlib",
  rust: "rust-axum",
  node: "express",
};

// Legacy labels for aggregate slugs that don't map 1:1 to a catalog
// entry. `python` is a language, not a framework — we keep the label
// "Python" but reuse the FastAPI icon for rendering.
const LEGACY_LABELS: Record<string, string> = {
  python: "Python",
  node: "Node.js",
};

function resolveFramework(input: string | undefined | null): Framework {
  if (!input) return getOrUnknown(undefined);
  const lower = input.toLowerCase();
  // Aggregate-language slugs map to a representative catalog entry for
  // the icon but keep their own label.
  if (lower === "python") {
    return { ...getOrUnknown("fastapi"), displayName: LEGACY_LABELS.python };
  }
  const slug = SLUG_ALIASES[lower] ?? lower;
  return getOrUnknown(slug);
}

export function FrameworkIcon({
  framework,
  size = "md",
  showLabel = false,
  className,
}: FrameworkIconProps) {
  const fw = resolveFramework(framework);
  return (
    <div className={cn("flex items-center gap-1.5", className)}>
      {/*
        SVG string from the catalog is inline and trusted (static at
        build time, not user input). dangerouslySetInnerHTML is safe here.
      */}
      <span
        role="img"
        aria-label={fw.displayName}
        className={cn("inline-flex shrink-0", sizes[size], fw.colorClass)}
        dangerouslySetInnerHTML={{ __html: fw.iconSVG }}
      />
      {showLabel && (
        <span className="text-muted-foreground text-xs font-medium">
          {fw.displayName}
        </span>
      )}
    </div>
  );
}

// -------------------------------------------------------------------
// Compatibility helpers — kept so existing callers don't break.
// Prefer `getOrUnknown` / `catalog` from lib/frameworks/catalog for
// new code.
// -------------------------------------------------------------------

/**
 * detectFramework — heuristic detection from a file list.
 *
 * Retained as a fallback for contexts where we only have file names
 * (no package.json contents, no backend-emitted slug). For richer
 * detection see `@/lib/frameworks/catalog` + the Go detector.
 */
export function detectFramework(files?: string[]): FrameworkType {
  if (!files || files.length === 0) return "unknown";

  const fileSet = new Set(files.map((f) => f.toLowerCase()));

  if (
    fileSet.has("next.config.js") ||
    fileSet.has("next.config.ts") ||
    fileSet.has("next.config.mjs")
  ) {
    return "nextjs";
  }
  if (fileSet.has("nuxt.config.js") || fileSet.has("nuxt.config.ts")) {
    // Legacy callers expect "nuxt" from this helper; keep that contract.
    return "nuxt";
  }
  if (fileSet.has("angular.json") || fileSet.has(".angular-cli.json")) {
    return "angular";
  }
  if (fileSet.has("svelte.config.js")) {
    return "svelte";
  }
  if (fileSet.has("astro.config.mjs") || fileSet.has("astro.config.ts")) {
    return "astro";
  }
  if (fileSet.has("vue.config.js")) {
    return "vue";
  }
  if (fileSet.has("manage.py")) return "django";
  if (fileSet.has("requirements.txt") || fileSet.has("pyproject.toml")) {
    return "python";
  }
  if (fileSet.has("go.mod")) return "go";
  if (fileSet.has("cargo.toml")) return "rust";
  if (fileSet.has("gemfile") && fileSet.has("config.ru")) return "rails";
  if (fileSet.has("mix.exs")) return "phoenix";
  if (fileSet.has("dockerfile")) return "dockerfile";
  if (fileSet.has("package.json")) return "node";

  return "unknown";
}

/**
 * getFrameworkLabel returns the display name for any known slug,
 * "Unknown" for unrecognized input, case-insensitive.
 */
export function getFrameworkLabel(framework: FrameworkType | string): string {
  return resolveFramework(framework).displayName;
}

/**
 * Infer framework from service name and git repo URL when the API
 * returns no framework slug. This is generic pattern matching only; project
 * cards should prefer backend-emitted slugs so app-specific knowledge stays
 * out of the UI.
 */
export function inferFrameworkFromContext(
  serviceName: string,
  gitRepo?: string,
): FrameworkType {
  const name = serviceName.toLowerCase();
  const repo = (gitRepo || "").toLowerCase();

  // Repo name substring matches.
  if (repo.includes("nextjs") || repo.includes("next-")) return "nextjs";
  if (repo.includes("fastapi") || repo.includes("fast-api")) return "fastapi";
  if (repo.includes("django")) return "django";
  if (repo.includes("flask")) return "flask";
  if (repo.includes("rails")) return "rails";
  if (repo.includes("svelte")) return "svelte";
  if (repo.includes("nuxt")) return "nuxt";
  if (repo.includes("angular")) return "angular";

  // Service name patterns for frontend.
  if (/-ui$|-(web|app|frontend|dashboard|landing|docs|site|status|page)/.test(name)) {
    return "nextjs";
  }

  // Service name patterns for backend.
  if (/-api$|-server$|-backend$|-gateway$|-worker/.test(name)) {
    if (repo.includes("python") || repo.includes("fastapi") || repo.includes("flask")) {
      return "python";
    }
    if (repo.includes("node") || repo.includes("express") || repo.includes("typescript")) {
      return "node";
    }
    return "unknown";
  }

  // CLI/SDK patterns.
  if (/cli$|sdk/.test(name)) return "go";

  return "unknown";
}

// Re-export the catalog for consumers that want richer access.
export { catalog, getOrUnknown };
