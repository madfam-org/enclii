"use client";

import { RouteError } from "@/components/ui/route-error";

export default function ProjectsError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return <RouteError error={error} reset={reset} title="Failed to load projects" />;
}
