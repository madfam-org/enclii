"use client";

import { useState, useEffect } from "react";
import { usePolling } from "@/hooks/use-polling";
import { POLLING_SLOW } from "@/lib/constants";
import Link from "next/link";
import { CheckCircle2, AlertTriangle, BarChart3, Clock, RefreshCw } from "lucide-react";
import { StatCard, StatCardSkeleton } from "@/components/dashboard/stat-card";

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4200";

interface DashboardStats {
  healthy_services: number;
  deployments_today: number;
  active_projects: number;
  avg_deploy_time: string;
}

interface RecentActivity {
  id: string;
  type: string;
  message: string;
  timestamp: string;
  status: "success" | "running" | "failed" | "pending";
  metadata?: any;
}

interface ServiceOverview {
  id: string;
  name: string;
  project_name: string;
  environment: string;
  status: "healthy" | "unhealthy" | "unknown";
  version: string;
  replicas: string;
}

interface DashboardResponse {
  stats: DashboardStats;
  activities: RecentActivity[];
  services: ServiceOverview[];
}

export default function Dashboard() {
  const [stats, setStats] = useState<DashboardStats>({
    healthy_services: 0,
    deployments_today: 0,
    active_projects: 0,
    avg_deploy_time: "N/A",
  });
  const [activities, setActivities] = useState<RecentActivity[]>([]);
  const [services, setServices] = useState<ServiceOverview[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchDashboardData = async () => {
    try {
      setError(null);
      const response = await fetch(`${API_BASE_URL}/v1/dashboard/stats`);

      if (!response.ok) {
        throw new Error(`API error: ${response.status} ${response.statusText}`);
      }

      const data: DashboardResponse = await response.json();

      setStats(data.stats);
      setActivities(data.activities || []);
      setServices(data.services || []);
      setLoading(false);
    } catch (err) {
      console.error("Failed to fetch dashboard data:", err);
      setError(
        err instanceof Error ? err.message : "Failed to fetch dashboard data",
      );
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchDashboardData();
  }, []);

  usePolling(fetchDashboardData, POLLING_SLOW);

  const formatTimeAgo = (timestamp: string) => {
    const now = new Date();
    const time = new Date(timestamp);
    const diffInSeconds = Math.floor((now.getTime() - time.getTime()) / 1000);

    if (diffInSeconds < 60) {
      return `${diffInSeconds} seconds ago`;
    } else if (diffInSeconds < 3600) {
      const minutes = Math.floor(diffInSeconds / 60);
      return `${minutes} minute${minutes > 1 ? "s" : ""} ago`;
    } else if (diffInSeconds < 86400) {
      const hours = Math.floor(diffInSeconds / 3600);
      return `${hours} hour${hours > 1 ? "s" : ""} ago`;
    } else {
      const days = Math.floor(diffInSeconds / 86400);
      return `${days} day${days > 1 ? "s" : ""} ago`;
    }
  };

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="px-4 py-6 sm:px-0">
          <div className="h-8 bg-muted rounded w-1/4 mb-8 animate-pulse"></div>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
            {[1, 2, 3, 4].map((i) => (
              <StatCardSkeleton key={i} />
            ))}
          </div>
          <div className="space-y-6">
            <div className="h-64 bg-muted rounded animate-pulse"></div>
            <div className="h-64 bg-muted rounded animate-pulse"></div>
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="bg-destructive/10 border border-destructive/20 rounded-lg p-6">
          <h2 className="text-lg font-semibold text-destructive mb-2">
            Error Loading Dashboard
          </h2>
          <p className="text-destructive/80 mb-4">{error}</p>
          <button
            onClick={fetchDashboardData}
            className="inline-flex items-center px-4 py-2 border border-destructive/30 rounded-md shadow-sm text-sm font-medium text-destructive bg-background hover:bg-destructive/10"
          >
            Try Again
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
      <div className="px-4 py-6 sm:px-0">
        <div className="flex items-center justify-between mb-8">
          <h1 className="text-3xl font-bold text-foreground">Dashboard</h1>
          <button
            onClick={fetchDashboardData}
            className="inline-flex items-center px-4 py-2 border border-input rounded-md shadow-sm text-sm font-medium text-foreground bg-background hover:bg-accent"
          >
            <RefreshCw className="w-4 h-4 mr-2" />
            Refresh
          </button>
        </div>

        {/* Status Cards */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
          <StatCard
            title="Healthy Services"
            value={stats.healthy_services}
            icon={CheckCircle2}
            variant="success"
          />
          <StatCard
            title="Deployments Today"
            value={stats.deployments_today}
            icon={AlertTriangle}
            variant="warning"
          />
          <StatCard
            title="Active Projects"
            value={stats.active_projects}
            icon={BarChart3}
            variant="info"
          />
          <StatCard
            title="Avg Deploy Time"
            value={stats.avg_deploy_time}
            icon={Clock}
            variant="neutral"
          />
        </div>

        {/* Recent Activity */}
        <div className="bg-card shadow overflow-hidden sm:rounded-md mb-8 border border-border">
          <div className="px-4 py-5 sm:px-6">
            <h3 className="text-lg leading-6 font-medium text-foreground">
              Recent Activity
            </h3>
            <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
              Latest deployments and system events
            </p>
          </div>
          <ul className="divide-y divide-border">
            {activities.length === 0 ? (
              <li className="px-4 py-8 text-center text-muted-foreground">
                No recent activity
              </li>
            ) : (
              activities.map((activity) => (
                <li key={activity.id} className="px-4 py-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center">
                      <div
                        className={`w-2 h-2 rounded-full mr-3 ${
                          activity.status === "success"
                            ? "bg-status-success"
                            : activity.status === "running"
                              ? "bg-status-info"
                              : activity.status === "failed"
                                ? "bg-status-error"
                                : "bg-status-warning"
                        }`}
                      ></div>
                      <div>
                        <p className="text-sm font-medium text-foreground">
                          {activity.message}
                        </p>
                        <p className="text-sm text-muted-foreground">
                          {activity.metadata?.version ||
                            activity.metadata?.environment ||
                            ""}{" "}
                          • {formatTimeAgo(activity.timestamp)}
                        </p>
                      </div>
                    </div>
                    <span
                      className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                        activity.status === "success"
                          ? "bg-status-success-muted text-status-success-foreground"
                          : activity.status === "running"
                            ? "bg-status-info-muted text-status-info-foreground"
                            : activity.status === "failed"
                              ? "bg-status-error-muted text-status-error-foreground"
                              : "bg-status-warning-muted text-status-warning-foreground"
                      }`}
                    >
                      {activity.status.charAt(0).toUpperCase() +
                        activity.status.slice(1)}
                    </span>
                  </div>
                </li>
              ))
            )}
          </ul>
        </div>

        {/* Services Overview */}
        <div className="bg-card shadow overflow-hidden sm:rounded-md border border-border">
          <div className="px-4 py-5 sm:px-6">
            <h3 className="text-lg leading-6 font-medium text-foreground">
              Services Overview
            </h3>
            <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
              Current status of all services
            </p>
          </div>
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-border">
              <thead className="bg-muted">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Service
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Environment
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Status
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Version
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Replicas
                  </th>
                </tr>
              </thead>
              <tbody className="bg-card divide-y divide-border">
                {services.length === 0 ? (
                  <tr>
                    <td
                      colSpan={5}
                      className="px-6 py-8 text-center text-muted-foreground"
                    >
                      No services found
                    </td>
                  </tr>
                ) : (
                  services.map((service) => (
                    <tr key={service.id} className="hover:bg-accent">
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="flex items-center">
                          <Link
                            href={`/services/${service.id}`}
                            className="text-sm font-medium text-foreground hover:text-primary"
                          >
                            {service.name}
                          </Link>
                          <div className="text-xs text-muted-foreground ml-2">
                            in {service.project_name}
                          </div>
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span
                          className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                            service.environment === "production"
                              ? "bg-status-success-muted text-status-success-foreground"
                              : service.environment === "staging"
                                ? "bg-status-warning-muted text-status-warning-foreground"
                                : "bg-status-info-muted text-status-info-foreground"
                          }`}
                        >
                          {service.environment}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span
                          className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                            service.status === "healthy"
                              ? "bg-status-success-muted text-status-success-foreground"
                              : service.status === "unhealthy"
                                ? "bg-status-error-muted text-status-error-foreground"
                                : "bg-muted text-muted-foreground"
                          }`}
                        >
                          {service.status}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-foreground">
                        {service.version}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-foreground">
                        {service.replicas}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  );
}
