'use client';

import { useState, useEffect } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { apiGet } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";
import { NetworkingTab } from "@/components/networking";
import { EnvVarsTab } from "@/components/env-vars";
import { PreviewsTab, RecentPreviews } from "@/components/previews";
import { SettingsTab } from "@/components/settings";
import { LogsTab } from "@/components/log-viewer";
import { DeploymentsTab, BuildLogsViewer } from "@/components/deployments";
import { Button } from "@enclii/ui-components/button";
import { ChevronLeft, Activity, FileText, RefreshCw } from "lucide-react";
import { HealthBadge } from "@/components/dashboard/health-badge";
import { SentryErrorBadge } from "@/components/dashboard/sentry-error-badge";
import { formatRelativeTime } from "@/lib/formatting";

interface ServiceDetail {
  id: string;
  name: string;
  project_id: string;
  project_name: string;
  project_slug?: string;
  environment: string;
  status: "healthy" | "unhealthy" | "unknown";
  version: string;
  replicas: string;
  created_at?: string;
  updated_at?: string;
  config?: {
    image?: string;
    port?: number;
    cpu_limit?: string;
    memory_limit?: string;
    env_vars?: Record<string, string>;
  };
  metrics?: {
    cpu_usage?: string;
    memory_usage?: string;
    request_count?: number;
    error_rate?: string;
  };
}

export default function ServiceDetailPage() {
  const params = useParams();
  const router = useRouter();
  const serviceId = params.id as string;

  const [service, setService] = useState<ServiceDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchService = async () => {
    try {
      setError(null);
      const data = await apiGet<ServiceDetail>(`/v1/services/${serviceId}`);
      setService(data);
      setLoading(false);
    } catch (err) {
      console.error("Failed to fetch service:", err);
      const message = err instanceof Error ? err.message : "Failed to fetch service details";
      // Handle specific error cases
      if (message.includes("not found") || message.includes("404")) {
        setError("Service not found");
      } else {
        setError(message);
      }
      setLoading(false);
    }
  };

  useEffect(() => {
    if (serviceId) {
      fetchService();
    }
  }, [serviceId]);

  if (loading) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-6">
          <Link href="/services" className="text-muted-foreground hover:text-foreground flex items-center gap-1 transition-colors text-sm">
            <ChevronLeft className="w-4 h-4" />
            Back to Services
          </Link>
        </div>
        <Card>
          <CardContent className="py-12">
            <div className="flex items-center justify-center">
              <Spinner size="lg" />
              <span className="ml-3 text-muted-foreground">Loading service details...</span>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (error) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-6">
          <Link href="/services" className="text-muted-foreground hover:text-foreground flex items-center gap-1 transition-colors text-sm">
            <ChevronLeft className="w-4 h-4" />
            Back to Services
          </Link>
        </div>
        <Card className="border-status-error/30 bg-status-error-muted">
          <CardContent className="py-8">
            <div className="text-center">
              <p className="text-status-error font-medium mb-4">{error}</p>
              <div className="space-x-4">
                <Button variant="outline" onClick={fetchService}>
                  Try Again
                </Button>
                <Button variant="default" onClick={() => router.push("/services")}>
                  Go to Services
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!service) {
    return null;
  }

  return (
    <div className="container mx-auto py-8">
      {/* Breadcrumb */}
      <div className="mb-6">
        <Link href="/services" className="text-muted-foreground hover:text-foreground flex items-center gap-1 transition-colors text-sm">
          <ChevronLeft className="w-4 h-4" />
          Back to Services
        </Link>
      </div>

      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-bold">{service.name}</h1>
          <p className="text-muted-foreground mt-2">
            Service details and configuration
          </p>
        </div>
        <div className="flex items-center gap-3">
          <span className={`inline-flex items-center px-3 py-1 rounded-full text-sm font-medium ${
            service.environment === "production" ? "bg-status-success/15 text-status-success" :
            service.environment === "staging" ? "bg-status-warning/15 text-status-warning" :
            "bg-status-info/15 text-status-info"
          }`}>
            {service.environment}
          </span>
          <HealthBadge serviceId={service.id} serviceName={service.name} />
          <SentryErrorBadge serviceId={service.id} serviceName={service.name} />
        </div>
      </div>

      {/* Tabs */}
      <Tabs defaultValue="overview" className="space-y-6">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="previews">Previews</TabsTrigger>
          <TabsTrigger value="env-vars">Environment</TabsTrigger>
          <TabsTrigger value="networking">Networking</TabsTrigger>
          <TabsTrigger value="deployments">Deployments</TabsTrigger>
          <TabsTrigger value="builds">Builds</TabsTrigger>
          <TabsTrigger value="logs">Logs</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        {/* Overview Tab */}
        <TabsContent value="overview">
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Overview Card */}
            <Card>
              <CardHeader>
                <CardTitle>Overview</CardTitle>
                <CardDescription>Basic service information</CardDescription>
              </CardHeader>
              <CardContent>
                <dl className="space-y-4">
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">Service ID</dt>
                    <dd className="font-mono text-sm">{service.id}</dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">Project</dt>
                    <dd>
                      <Link
                        href={`/projects/${service.project_slug || service.project_name?.toLowerCase().replace(/\s+/g, '-')}`}
                        className="text-enclii-blue hover:text-enclii-blue-dark transition-colors"
                      >
                        {service.project_name}
                      </Link>
                    </dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">Version</dt>
                    <dd className="font-mono text-sm">{service.version || "N/A"}</dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">Replicas</dt>
                    <dd>{service.replicas || "0/0"}</dd>
                  </div>
                  {service.created_at && (
                    <div className="flex justify-between">
                      <dt className="text-muted-foreground">Created</dt>
                      <dd>{formatRelativeTime(service.created_at)}</dd>
                    </div>
                  )}
                  {service.updated_at && (
                    <div className="flex justify-between">
                      <dt className="text-muted-foreground">Last Updated</dt>
                      <dd>{formatRelativeTime(service.updated_at)}</dd>
                    </div>
                  )}
                </dl>
              </CardContent>
            </Card>

            {/* Configuration Card */}
            <Card>
              <CardHeader>
                <CardTitle>Configuration</CardTitle>
                <CardDescription>Resource limits and settings</CardDescription>
              </CardHeader>
              <CardContent>
                <dl className="space-y-4">
                  {service.config?.image && (
                    <div className="flex justify-between">
                      <dt className="text-muted-foreground">Image</dt>
                      <dd className="font-mono text-sm truncate max-w-[200px]" title={service.config.image}>
                        {service.config.image}
                      </dd>
                    </div>
                  )}
                  {service.config?.port && (
                    <div className="flex justify-between">
                      <dt className="text-muted-foreground">Port</dt>
                      <dd>{service.config.port}</dd>
                    </div>
                  )}
                  {service.config?.cpu_limit && (
                    <div className="flex justify-between">
                      <dt className="text-muted-foreground">CPU Limit</dt>
                      <dd>{service.config.cpu_limit}</dd>
                    </div>
                  )}
                  {service.config?.memory_limit && (
                    <div className="flex justify-between">
                      <dt className="text-muted-foreground">Memory Limit</dt>
                      <dd>{service.config.memory_limit}</dd>
                    </div>
                  )}
                  {!service.config && (
                    <p className="text-muted-foreground text-sm">No configuration data available</p>
                  )}
                </dl>
              </CardContent>
            </Card>

            {/* Metrics Card */}
            {service.metrics && (
              <Card>
                <CardHeader>
                  <CardTitle>Metrics</CardTitle>
                  <CardDescription>Current resource usage</CardDescription>
                </CardHeader>
                <CardContent>
                  <dl className="space-y-4">
                    {service.metrics.cpu_usage && (
                      <div className="flex justify-between">
                        <dt className="text-muted-foreground">CPU Usage</dt>
                        <dd>{service.metrics.cpu_usage}</dd>
                      </div>
                    )}
                    {service.metrics.memory_usage && (
                      <div className="flex justify-between">
                        <dt className="text-muted-foreground">Memory Usage</dt>
                        <dd>{service.metrics.memory_usage}</dd>
                      </div>
                    )}
                    {service.metrics.request_count !== undefined && (
                      <div className="flex justify-between">
                        <dt className="text-muted-foreground">Requests (24h)</dt>
                        <dd>{service.metrics.request_count.toLocaleString()}</dd>
                      </div>
                    )}
                    {service.metrics.error_rate && (
                      <div className="flex justify-between">
                        <dt className="text-muted-foreground">Error Rate</dt>
                        <dd>{service.metrics.error_rate}</dd>
                      </div>
                    )}
                  </dl>
                </CardContent>
              </Card>
            )}

            {/* Actions Card */}
            <Card>
              <CardHeader>
                <CardTitle>Actions</CardTitle>
                <CardDescription>Service operations</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  <Button
                    variant="outline"
                    className="w-full justify-start"
                    onClick={() => router.push(`/deployments?service=${serviceId}`)}
                  >
                    <Activity className="w-4 h-4 mr-2" />
                    View Deployments
                  </Button>
                  <Button
                    variant="outline"
                    className="w-full justify-start"
                    onClick={() => {
                      // Switch to logs tab
                      const logsTab = document.querySelector('[data-state="inactive"][value="logs"]') as HTMLElement;
                      if (logsTab) logsTab.click();
                    }}
                  >
                    <FileText className="w-4 h-4 mr-2" />
                    View Logs
                  </Button>
                  <Button
                    variant="outline"
                    className="w-full justify-start"
                    onClick={fetchService}
                  >
                    <RefreshCw className="w-4 h-4 mr-2" />
                    Refresh
                  </Button>
                </div>
              </CardContent>
            </Card>

            {/* Recent Previews Card */}
            <RecentPreviews serviceId={serviceId} limit={3} />
          </div>
        </TabsContent>

        {/* Previews Tab */}
        <TabsContent value="previews">
          <PreviewsTab serviceId={serviceId} serviceName={service.name} />
        </TabsContent>

        {/* Environment Variables Tab */}
        <TabsContent value="env-vars">
          <EnvVarsTab serviceId={serviceId} serviceName={service.name} />
        </TabsContent>

        {/* Networking Tab */}
        <TabsContent value="networking">
          <NetworkingTab serviceId={serviceId} serviceName={service.name} />
        </TabsContent>

        {/* Deployments Tab */}
        <TabsContent value="deployments">
          <DeploymentsTab serviceId={serviceId} serviceName={service.name} />
        </TabsContent>

        {/* Builds Tab */}
        <TabsContent value="builds">
          <BuildLogsViewer serviceId={serviceId} serviceName={service.name} />
        </TabsContent>

        {/* Logs Tab */}
        <TabsContent value="logs">
          <LogsTab serviceId={serviceId} serviceName={service.name} env={service.environment} />
        </TabsContent>

        {/* Settings Tab */}
        <TabsContent value="settings">
          <SettingsTab serviceId={serviceId} serviceName={service.name} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
