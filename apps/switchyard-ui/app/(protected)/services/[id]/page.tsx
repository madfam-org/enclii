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

  const getStatusBadgeClass = (status: string) => {
    switch (status) {
      case "healthy":
        return "bg-status-success-muted text-status-success-foreground";
      case "unhealthy":
        return "bg-status-error-muted text-status-error-foreground";
      default:
        return "bg-muted text-muted-foreground";
    }
  };

  const getEnvironmentBadgeClass = (env: string) => {
    switch (env) {
      case "production":
        return "bg-status-success-muted text-status-success-foreground";
      case "staging":
        return "bg-status-warning-muted text-status-warning-foreground";
      default:
        return "bg-status-info-muted text-status-info-foreground";
    }
  };

  if (loading) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-6">
          <Link href="/services" className="text-primary hover:text-primary/80 flex items-center gap-1">
            <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
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
          <Link href="/services" className="text-primary hover:text-primary/80 flex items-center gap-1">
            <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
            Back to Services
          </Link>
        </div>
        <Card className="border-status-error/30 bg-status-error-muted">
          <CardContent className="py-8">
            <div className="text-center">
              <p className="text-status-error font-medium mb-4">{error}</p>
              <div className="space-x-4">
                <button
                  onClick={fetchService}
                  className="inline-flex items-center px-4 py-2 border border-status-error/30 rounded-md shadow-sm text-sm font-medium text-status-error-foreground bg-card hover:bg-status-error-muted"
                >
                  Try Again
                </button>
                <button
                  onClick={() => router.push("/services")}
                  className="inline-flex items-center px-4 py-2 border border-input rounded-md shadow-sm text-sm font-medium text-foreground bg-card hover:bg-accent"
                >
                  Go to Services
                </button>
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
        <Link href="/services" className="text-primary hover:text-primary/80 flex items-center gap-1">
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
          </svg>
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
          <span className={`inline-flex items-center px-3 py-1 rounded-full text-sm font-medium ${getEnvironmentBadgeClass(service.environment)}`}>
            {service.environment}
          </span>
          <span className={`inline-flex items-center px-3 py-1 rounded-full text-sm font-medium ${getStatusBadgeClass(service.status)}`}>
            {service.status}
          </span>
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
                        className="text-primary hover:text-primary/80"
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
                      <dd>{new Date(service.created_at).toLocaleDateString()}</dd>
                    </div>
                  )}
                  {service.updated_at && (
                    <div className="flex justify-between">
                      <dt className="text-muted-foreground">Last Updated</dt>
                      <dd>{new Date(service.updated_at).toLocaleDateString()}</dd>
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
                  <button
                    className="w-full inline-flex items-center justify-center px-4 py-2 border border-input rounded-md shadow-sm text-sm font-medium text-foreground bg-card hover:bg-accent"
                    onClick={() => router.push(`/deployments?service=${serviceId}`)}
                  >
                    <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
                    </svg>
                    View Deployments
                  </button>
                  <button
                    className="w-full inline-flex items-center justify-center px-4 py-2 border border-input rounded-md shadow-sm text-sm font-medium text-foreground bg-card hover:bg-accent"
                    onClick={() => {
                      // Switch to logs tab
                      const logsTab = document.querySelector('[data-state="inactive"][value="logs"]') as HTMLElement;
                      if (logsTab) logsTab.click();
                    }}
                  >
                    <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                    </svg>
                    View Logs
                  </button>
                  <button
                    className="w-full inline-flex items-center justify-center px-4 py-2 border border-primary/30 rounded-md shadow-sm text-sm font-medium text-primary bg-primary/5 hover:bg-primary/10"
                    onClick={fetchService}
                  >
                    <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                    </svg>
                    Refresh
                  </button>
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
