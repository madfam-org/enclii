"use client";

import { Card } from "@/components/ui/card";

export function ProjectCardCompactSkeleton() {
  return (
    <Card className="flex h-[240px] animate-pulse flex-col justify-between p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <div className="bg-muted h-6 w-6 rounded" />
          <div className="bg-muted h-4 w-28 rounded" />
        </div>
        <div className="bg-muted h-2.5 w-2.5 rounded-full" />
      </div>
      <div className="border-border/40 mt-2 space-y-1 rounded border p-2">
        <div className="bg-muted h-3 w-full rounded" />
        <div className="bg-muted h-3 w-5/6 rounded" />
        <div className="bg-muted h-3 w-4/6 rounded" />
      </div>
      <div className="bg-muted mt-2 h-3 w-3/4 rounded" />
      <div className="bg-muted mt-2 h-3 w-1/2 rounded" />
      <div className="border-border/50 mt-auto flex items-center justify-between border-t pt-2">
        <div className="bg-muted h-3 w-16 rounded" />
        <div className="bg-muted h-3 w-10 rounded" />
      </div>
    </Card>
  );
}
