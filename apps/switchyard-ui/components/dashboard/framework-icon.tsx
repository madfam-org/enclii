"use client";

import { cn } from "@/lib/utils";

// Framework type detection and icon mapping
export type FrameworkType =
  | "nextjs"
  | "react"
  | "vue"
  | "nuxt"
  | "angular"
  | "svelte"
  | "express"
  | "fastapi"
  | "flask"
  | "django"
  | "rails"
  | "go"
  | "rust"
  | "node"
  | "python"
  | "unknown";

interface FrameworkIconProps {
  framework: FrameworkType | string;
  size?: "sm" | "md" | "lg";
  showLabel?: boolean;
  className?: string;
}

// SVG paths for framework icons (simplified logos)
const frameworkIcons: Record<string, { path: JSX.Element; color: string; label: string }> = {
  nextjs: {
    path: (
      <path
        fill="currentColor"
        d="M11.5 9.5v5h-1v-3.667l-1.5 2.334h-.5l-1.5-2.334v3.667h-1v-5h1l2 3 2-3h.5z"
      />
    ),
    color: "text-foreground",
    label: "Next.js",
  },
  react: {
    path: (
      <>
        <circle cx="12" cy="12" r="2" fill="currentColor" />
        <ellipse
          cx="12"
          cy="12"
          rx="8"
          ry="3"
          stroke="currentColor"
          strokeWidth="1.5"
          fill="none"
        />
        <ellipse
          cx="12"
          cy="12"
          rx="8"
          ry="3"
          stroke="currentColor"
          strokeWidth="1.5"
          fill="none"
          transform="rotate(60 12 12)"
        />
        <ellipse
          cx="12"
          cy="12"
          rx="8"
          ry="3"
          stroke="currentColor"
          strokeWidth="1.5"
          fill="none"
          transform="rotate(120 12 12)"
        />
      </>
    ),
    color: "text-cyan-500",
    label: "React",
  },
  vue: {
    path: (
      <path
        fill="currentColor"
        d="M12 4l7 12H5l7-12zm0 3l-4 7h8l-4-7z"
      />
    ),
    color: "text-emerald-500",
    label: "Vue.js",
  },
  nuxt: {
    path: (
      <path
        fill="currentColor"
        d="M12 4l8 14H4l8-14zm0 4l-4.5 8h9l-4.5-8z"
      />
    ),
    color: "text-green-500",
    label: "Nuxt",
  },
  angular: {
    path: (
      <path
        fill="currentColor"
        d="M12 3l8 3-1 13-7 4-7-4-1-13 8-3zm0 2.5L6.5 17h2l1-2.5h5l1 2.5h2L12 5.5zm0 3l1.5 4h-3l1.5-4z"
      />
    ),
    color: "text-red-500",
    label: "Angular",
  },
  svelte: {
    path: (
      <path
        fill="currentColor"
        d="M12 4c3 0 6 2 6 5s-3 5-6 5-6-2-6-5 3-5 6-5zm0 2c-2 0-4 1.5-4 3s2 3 4 3 4-1.5 4-3-2-3-4-3z"
      />
    ),
    color: "text-orange-500",
    label: "Svelte",
  },
  express: {
    path: (
      <path
        fill="currentColor"
        d="M5 12h14M5 8h10M5 16h8"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    ),
    color: "text-muted-foreground",
    label: "Express",
  },
  fastapi: {
    path: (
      <path
        fill="currentColor"
        d="M12 4l-1 4h6l-5 8 1-4H7l5-8z"
      />
    ),
    color: "text-teal-500",
    label: "FastAPI",
  },
  flask: {
    path: (
      <path
        fill="currentColor"
        d="M12 4v4M10 8h4M8 12c0 4 2 6 4 6s4-2 4-6M10 10c-2 1-3 2-2 4 1 3 3 4 4 4s3-1 4-4c1-2 0-3-2-4"
      />
    ),
    color: "text-muted-foreground",
    label: "Flask",
  },
  django: {
    path: (
      <path
        fill="currentColor"
        d="M9 6v12M9 6h3c2 0 3 1.5 3 3.5S14 13 12 13H9m0 0v5"
      />
    ),
    color: "text-green-700",
    label: "Django",
  },
  rails: {
    path: (
      <path
        fill="currentColor"
        d="M4 17h16M4 12h16M7 7h10M9 7v10M15 7v10"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    ),
    color: "text-red-600",
    label: "Rails",
  },
  go: {
    path: (
      <path
        fill="currentColor"
        d="M6 12c0 3.3 2.7 6 6 6s6-2.7 6-6-2.7-6-6-6-6 2.7-6 6zm3-1h3v2H9v-2zm3 0h3v2h-3v-2z"
      />
    ),
    color: "text-cyan-600",
    label: "Go",
  },
  rust: {
    path: (
      <path
        fill="currentColor"
        d="M12 4l2 3h3l-1 3 2 3h-3l-2 3-2-3H6l2-3-2-3h3l3-3z"
      />
    ),
    color: "text-orange-700",
    label: "Rust",
  },
  node: {
    path: (
      <path
        fill="currentColor"
        d="M12 4l7 4v8l-7 4-7-4V8l7-4z"
      />
    ),
    color: "text-green-600",
    label: "Node.js",
  },
  python: {
    path: (
      <path
        fill="currentColor"
        d="M12 4c-2 0-4 .5-4 2v2h4v1H6c-2 0-3 1.5-3 4 0 2.5 1 4 3 4h2v-2c0-2 1-3 3-3h4c2 0 3-1 3-3V6c0-1.5-2-2-4-2h-2zm-1 1.5a.75.75 0 110 1.5.75.75 0 010-1.5z"
      />
    ),
    color: "text-yellow-600",
    label: "Python",
  },
  unknown: {
    path: (
      <path
        fill="currentColor"
        d="M12 4C7.6 4 4 7.6 4 12s3.6 8 8 8 8-3.6 8-8-3.6-8-8-8zm0 2c3.3 0 6 2.7 6 6s-2.7 6-6 6-6-2.7-6-6 2.7-6 6-6zm0 2v4m0 2v1"
      />
    ),
    color: "text-muted-foreground",
    label: "Unknown",
  },
};

const sizes = {
  sm: "w-4 h-4",
  md: "w-6 h-6",
  lg: "w-8 h-8",
};

export function FrameworkIcon({
  framework,
  size = "md",
  showLabel = false,
  className,
}: FrameworkIconProps) {
  const normalizedFramework = framework.toLowerCase() as FrameworkType;
  const iconData = frameworkIcons[normalizedFramework] || frameworkIcons.unknown;

  return (
    <div className={cn("flex items-center gap-1.5", className)}>
      <svg
        viewBox="0 0 24 24"
        className={cn(sizes[size], iconData.color)}
        aria-label={iconData.label}
      >
        {iconData.path}
      </svg>
      {showLabel && (
        <span className="text-xs font-medium text-muted-foreground">
          {iconData.label}
        </span>
      )}
    </div>
  );
}

// Utility function to detect framework from file patterns
export function detectFramework(files?: string[]): FrameworkType {
  if (!files || files.length === 0) return "unknown";

  const fileSet = new Set(files.map(f => f.toLowerCase()));

  // Next.js detection
  if (fileSet.has("next.config.js") || fileSet.has("next.config.ts") || fileSet.has("next.config.mjs")) {
    return "nextjs";
  }

  // Nuxt detection
  if (fileSet.has("nuxt.config.js") || fileSet.has("nuxt.config.ts")) {
    return "nuxt";
  }

  // Vue detection
  if (fileSet.has("vue.config.js") || fileSet.has("vite.config.ts")) {
    // Check if it's Vue (not Nuxt)
    if (!fileSet.has("nuxt.config.js") && !fileSet.has("nuxt.config.ts")) {
      return "vue";
    }
  }

  // Angular detection
  if (fileSet.has("angular.json") || fileSet.has(".angular-cli.json")) {
    return "angular";
  }

  // Svelte detection
  if (fileSet.has("svelte.config.js")) {
    return "svelte";
  }

  // React detection (after Next.js check)
  if (fileSet.has("package.json")) {
    // Would need to check package.json contents for react dependency
    // For now, assume it's React if there's a package.json with no other framework detected
  }

  // FastAPI/Flask/Django detection
  if (fileSet.has("requirements.txt") || fileSet.has("pyproject.toml")) {
    if (fileSet.has("manage.py")) return "django";
    // Would need to check requirements.txt for fastapi/flask
    return "python";
  }

  // Go detection
  if (fileSet.has("go.mod")) {
    return "go";
  }

  // Rust detection
  if (fileSet.has("cargo.toml")) {
    return "rust";
  }

  // Rails detection
  if (fileSet.has("gemfile") && fileSet.has("config.ru")) {
    return "rails";
  }

  // Express/Node detection
  if (fileSet.has("package.json")) {
    return "node";
  }

  return "unknown";
}

export function getFrameworkLabel(framework: FrameworkType | string): string {
  const normalizedFramework = framework.toLowerCase() as FrameworkType;
  return frameworkIcons[normalizedFramework]?.label || "Unknown";
}
