'use client';

/**
 * P2.1 in-UI log tail.
 *
 * Route: /projects/[slug]/services/[id]/logs
 *
 * The page resolves the service by ID (not slug, since service names
 * aren't unique across projects), hydrates the header with name + env,
 * and hands the heavy lifting to LogViewer. All filtering state lives
 * inside LogViewer and is URL-synced there.
 */

import { useEffect, useState } from 'react';
import { useParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { ChevronLeft } from 'lucide-react';

import { Spinner } from '@/components/ui/spinner';
import { Button } from "@enclii/ui-components/button";
import { apiGet } from '@/lib/api';
import { LogViewer } from '@/components/log-viewer-v2';

interface ServiceShape {
  id: string;
  name: string;
  project_id: string;
  project_name?: string;
  project_slug?: string;
  environment?: string;
}

export default function ServiceLogsPage() {
  const params = useParams<{ slug: string; id: string }>();
  const router = useRouter();

  const [service, setService] = useState<ServiceShape | null>(null);
  const [loadErr, setLoadErr] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await apiGet<ServiceShape>(`/v1/services/${params.id}`);
        if (!cancelled) setService(data);
      } catch (err) {
        if (!cancelled) {
          setLoadErr(err instanceof Error ? err.message : 'Failed to load service');
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [params.id]);

  if (loadErr) {
    return (
      <div className="p-8">
        <div className="mx-auto max-w-2xl rounded-md border border-red-500/40 bg-red-950/40 p-4 text-red-200">
          <p className="font-medium">Failed to load service</p>
          <p className="mt-1 text-sm opacity-80">{loadErr}</p>
          <Button
            variant="outline"
            size="sm"
            className="mt-3"
            onClick={() => router.back()}
          >
            Go back
          </Button>
        </div>
      </div>
    );
  }

  if (!service) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    );
  }

  const env = service.environment || 'production';

  return (
    <div className="flex h-[calc(100vh-4rem)] flex-col">
      {/* Breadcrumb + title */}
      <div className="bg-card border-b px-4 py-3">
        <div className="text-muted-foreground flex items-center gap-2 text-sm">
          <Link
            href={`/projects/${params.slug}`}
            className="hover:text-foreground"
          >
            {service.project_name || params.slug}
          </Link>
          <span>/</span>
          <Link
            href={`/services/${service.id}`}
            className="hover:text-foreground"
          >
            {service.name}
          </Link>
          <span>/</span>
          <span className="text-foreground">Logs</span>
        </div>
        <h1 className="mt-1 flex items-center gap-2 text-lg font-semibold">
          <Link
            href={`/services/${service.id}`}
            className="text-muted-foreground hover:text-foreground inline-flex items-center"
            aria-label="Back to service"
          >
            <ChevronLeft className="h-4 w-4" />
          </Link>
          Logs for {service.name}
          <span className="bg-muted ml-2 rounded px-2 py-0.5 text-xs font-normal">
            {env}
          </span>
        </h1>
      </div>

      {/* Full-bleed viewer */}
      <LogViewer serviceId={service.id} serviceName={service.name} env={env} />
    </div>
  );
}
