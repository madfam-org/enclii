'use client';

import { LogViewer } from '@/components/log-viewer-v2';

export interface LogsTabProps {
  serviceId: string;
  serviceName: string;
  env?: string;
  deploymentId?: string;
}

export function LogsTab({ serviceId, serviceName, env = 'production' }: LogsTabProps) {
  return <LogViewer serviceId={serviceId} serviceName={serviceName} env={env} />;
}
