"use client";

import { RouteError } from "@/components/ui/route-error";

export default function ServicesError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return <RouteError error={error} reset={reset} title="Failed to load services" />;
}
