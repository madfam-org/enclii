"use client";

/**
 * ErrorsTab
 * Displays recent errors list
 */

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@enclii/ui-components/badge";
import type { RecentErrorsResponse } from "../observability-types";

interface ErrorsTabProps {
  errors: RecentErrorsResponse | null;
}

export function ErrorsTab({ errors }: ErrorsTabProps) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent Errors</CardTitle>
        <CardDescription>
          {errors?.total_count || 0} errors in the last {errors?.time_range}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {errors?.errors.length === 0 ? (
          <div className="py-12 text-center text-muted-foreground">
            <svg
              aria-hidden="true"
              className="mx-auto mb-4 h-12 w-12 text-status-success"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            <p className="font-medium">No errors detected</p>
            <p className="mt-1 text-sm">Your services are running smoothly</p>
          </div>
        ) : (
          <div className="space-y-4">
            {errors?.errors.map((err) => (
              <div
                key={err.id}
                className="rounded-lg border border-status-error/30 bg-status-error-muted p-4"
              >
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="mb-1 flex items-center gap-2">
                      <Badge variant="destructive">{err.level}</Badge>
                      {err.service_name && (
                        <span className="text-sm text-muted-foreground">
                          {err.service_name}
                        </span>
                      )}
                    </div>
                    <p className="font-mono text-sm">{err.message}</p>
                    {err.stack_trace && (
                      <pre className="mt-2 overflow-x-auto rounded bg-gray-900 p-2 text-xs text-gray-100">
                        {err.stack_trace}
                      </pre>
                    )}
                  </div>
                  <div className="ml-4 text-right text-xs text-muted-foreground">
                    <div>{new Date(err.timestamp).toLocaleString()}</div>
                    {err.count > 1 && <div className="mt-1">{err.count} occurrences</div>}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
