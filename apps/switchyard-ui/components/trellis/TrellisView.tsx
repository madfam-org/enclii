'use client';

import * as React from 'react';
import { cn } from '@/lib/utils';
import { CircularGauge } from '@/components/ui/circular-gauge';
import { Badge } from "@enclii/ui-components/badge";
import {
  ChevronRight,
  ChevronDown,
  Layers,
  Box,
  Zap,
  Activity,
} from 'lucide-react';

// =============================================================================
// TRELLIS VIEW - Topological Visualization
//
// A hierarchical tree view showing: Organization → Projects → Services
// With health indicators (CPU/RAM gauges) at each node.
//
// Design Philosophy:
// - Enterprise Mode: Clean lines, professional spacing
// - Solarpunk Mode: Organic connectors with bioluminescent glow
// =============================================================================

// -----------------------------------------------------------------------------
// TYPES
// -----------------------------------------------------------------------------

export interface TrellisOrganization {
  id: string;
  name: string;
  slug: string;
  projects: TrellisProject[];
}

export interface TrellisProject {
  id: string;
  name: string;
  slug: string;
  services: TrellisService[];
  health: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
}

export interface TrellisService {
  id: string;
  name: string;
  status: 'running' | 'deploying' | 'stopped' | 'failed';
  health: 'healthy' | 'unhealthy' | 'unknown';
  metrics?: {
    cpu: number;
    memory: number;
    cpuLimit: number;
    memoryLimit: number;
  };
}

interface TrellisViewProps {
  organization: TrellisOrganization;
  /** View mode: tree (hierarchical) or grid (flat) */
  viewMode?: 'tree' | 'grid';
  /** Callback when a service is clicked */
  onServiceClick?: (serviceId: string, projectId: string) => void;
  /** Callback when a project is clicked */
  onProjectClick?: (projectId: string) => void;
  /** Initially expanded project IDs */
  defaultExpanded?: string[];
  /** Show resource gauges */
  showMetrics?: boolean;
  /** Additional class names */
  className?: string;
}

// -----------------------------------------------------------------------------
// ICONS (Theme-aware via Solarpunk organic metaphors)
// -----------------------------------------------------------------------------

const HEALTH_ICONS = {
  healthy: '🌿',
  degraded: '🌾',
  unhealthy: '🥀',
  unknown: '🌑',
} as const;

const STATUS_COLORS = {
  running: 'bg-status-success',
  deploying: 'bg-status-warning',
  stopped: 'bg-muted-foreground',
  failed: 'bg-status-error',
} as const;

// -----------------------------------------------------------------------------
// SUBCOMPONENTS
// -----------------------------------------------------------------------------

interface ServiceNodeProps {
  service: TrellisService;
  showMetrics: boolean;
  onClick?: () => void;
  isLast: boolean;
}

function ServiceNode({ service, showMetrics, onClick, isLast }: ServiceNodeProps) {
  return (
    <div
      className={cn(
        'trellis-node group relative flex items-center gap-3 p-3 rounded-lg',
        'border border-border bg-card hover:bg-accent/50 cursor-pointer',
        'transition-all duration-200 animate-node-appear',
        onClick && 'hover:border-primary'
      )}
      onClick={onClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => e.key === 'Enter' && onClick?.()}
    >
      {/* Connector line (vertical) */}
      <div className="absolute -left-6 top-1/2 w-6 h-px trellis-svg-connector" />
      {!isLast && (
        <div className="absolute -left-6 top-1/2 h-full w-px trellis-svg-connector" />
      )}

      {/* Status indicator */}
      <div className="relative flex-shrink-0">
        <div
          className={cn(
            'w-2 h-2 rounded-full',
            STATUS_COLORS[service.status]
          )}
        />
        {service.status === 'running' && (
          <div
            className={cn(
              'absolute inset-0 w-2 h-2 rounded-full animate-ping',
              STATUS_COLORS[service.status],
              'opacity-75'
            )}
          />
        )}
      </div>

      {/* Service info */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <Box className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="font-medium text-sm truncate">{service.name}</span>
        </div>
        <div className="flex items-center gap-2 mt-0.5">
          <Badge
            variant={service.health === 'healthy' ? 'default' : 'destructive'}
            className="text-[10px] px-1.5 py-0"
          >
            {service.health}
          </Badge>
          <span className="text-[10px] text-muted-foreground capitalize">
            {service.status}
          </span>
        </div>
      </div>

      {/* Metrics gauges */}
      {showMetrics && service.metrics && (
        <div className="flex gap-2">
          <CircularGauge
            value={service.metrics.cpu}
            max={service.metrics.cpuLimit}
            size={36}
            strokeWidth={3}
            variant="auto"
            showPercentage
            animationDuration={300}
          />
          <CircularGauge
            value={service.metrics.memory}
            max={service.metrics.memoryLimit}
            size={36}
            strokeWidth={3}
            variant="auto"
            showPercentage
            animationDuration={300}
          />
        </div>
      )}
    </div>
  );
}

interface ProjectNodeProps {
  project: TrellisProject;
  isExpanded: boolean;
  onToggle: () => void;
  onProjectClick?: () => void;
  onServiceClick?: (serviceId: string) => void;
  showMetrics: boolean;
}

function ProjectNode({
  project,
  isExpanded,
  onToggle,
  onProjectClick,
  onServiceClick,
  showMetrics,
}: ProjectNodeProps) {
  const healthIcon = HEALTH_ICONS[project.health];

  return (
    <div className="relative">
      {/* Project header */}
      <div
        className={cn(
          'trellis-node flex items-center gap-3 p-4 rounded-lg',
          'border border-border bg-card',
          'transition-all duration-200',
          'hover:border-primary cursor-pointer'
        )}
        onClick={onProjectClick}
        role="button"
        tabIndex={0}
      >
        {/* Expand/collapse toggle */}
        <button
          onClick={(e) => {
            e.stopPropagation();
            onToggle();
          }}
          className="flex-shrink-0 p-1 rounded hover:bg-accent transition-colors"
          aria-label={isExpanded ? 'Collapse project' : 'Expand project'}
        >
          {isExpanded ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </button>

        {/* Project icon */}
        <div className="flex-shrink-0">
          <Layers className="h-5 w-5 text-primary" />
        </div>

        {/* Project info */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-semibold truncate">{project.name}</span>
            <span className="text-lg" title={`Health: ${project.health}`}>
              {healthIcon}
            </span>
          </div>
          <div className="flex items-center gap-2 mt-0.5 text-xs text-muted-foreground">
            <span>{project.services.length} service{project.services.length !== 1 ? 's' : ''}</span>
            <span>•</span>
            <span className="font-mono">{project.slug}</span>
          </div>
        </div>

        {/* Health summary dots */}
        <div className="flex gap-1">
          {project.services.slice(0, 5).map((service) => (
            <div
              key={service.id}
              className={cn(
                'w-2 h-2 rounded-full',
                STATUS_COLORS[service.status]
              )}
              title={`${service.name}: ${service.status}`}
            />
          ))}
          {project.services.length > 5 && (
            <span className="text-xs text-muted-foreground">
              +{project.services.length - 5}
            </span>
          )}
        </div>
      </div>

      {/* Services (collapsible) */}
      {isExpanded && project.services.length > 0 && (
        <div className="ml-8 mt-2 space-y-2 relative">
          {/* Vertical connector line */}
          <div className="absolute left-0 top-0 h-full w-px trellis-svg-connector" />

          {project.services.map((service, index) => (
            <ServiceNode
              key={service.id}
              service={service}
              showMetrics={showMetrics}
              onClick={() => onServiceClick?.(service.id)}
              isLast={index === project.services.length - 1}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// -----------------------------------------------------------------------------
// MAIN COMPONENT
// -----------------------------------------------------------------------------

export function TrellisView({
  organization,
  viewMode = 'tree',
  onServiceClick,
  onProjectClick,
  defaultExpanded = [],
  showMetrics = true,
  className,
}: TrellisViewProps) {
  const [expandedProjects, setExpandedProjects] = React.useState<Set<string>>(
    new Set(defaultExpanded)
  );

  const toggleProject = (projectId: string) => {
    setExpandedProjects((prev) => {
      const next = new Set(prev);
      if (next.has(projectId)) {
        next.delete(projectId);
      } else {
        next.add(projectId);
      }
      return next;
    });
  };

  const expandAll = () => {
    setExpandedProjects(new Set(organization.projects.map((p) => p.id)));
  };

  const collapseAll = () => {
    setExpandedProjects(new Set());
  };

  // Calculate health summary
  const healthSummary = React.useMemo(() => {
    let healthy = 0;
    let degraded = 0;
    let unhealthy = 0;

    organization.projects.forEach((project) => {
      project.services.forEach((service) => {
        if (service.health === 'healthy' && service.status === 'running') healthy++;
        else if (service.status === 'deploying') degraded++;
        else unhealthy++;
      });
    });

    return { healthy, degraded, unhealthy, total: healthy + degraded + unhealthy };
  }, [organization.projects]);

  if (viewMode === 'grid') {
    return (
      <div className={cn('space-y-4', className)}>
        <TrellisHeader
          organization={organization}
          healthSummary={healthSummary}
          onExpandAll={expandAll}
          onCollapseAll={collapseAll}
        />
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {organization.projects.flatMap((project) =>
            project.services.map((service) => (
              <ServiceNode
                key={service.id}
                service={service}
                showMetrics={showMetrics}
                onClick={() => onServiceClick?.(service.id, project.id)}
                isLast={true}
              />
            ))
          )}
        </div>
      </div>
    );
  }

  return (
    <div className={cn('space-y-4', className)}>
      <TrellisHeader
        organization={organization}
        healthSummary={healthSummary}
        onExpandAll={expandAll}
        onCollapseAll={collapseAll}
      />

      {/* Organization root */}
      <div className="relative">
        {/* SVG connector overlay for organic look */}
        <TrellisConnectorSVG
          projects={organization.projects}
          expandedProjects={expandedProjects}
        />

        {/* Project list */}
        <div className="space-y-3">
          {organization.projects.map((project) => (
            <ProjectNode
              key={project.id}
              project={project}
              isExpanded={expandedProjects.has(project.id)}
              onToggle={() => toggleProject(project.id)}
              onProjectClick={() => onProjectClick?.(project.id)}
              onServiceClick={(serviceId) => onServiceClick?.(serviceId, project.id)}
              showMetrics={showMetrics}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

// -----------------------------------------------------------------------------
// HEADER COMPONENT
// -----------------------------------------------------------------------------

interface TrellisHeaderProps {
  organization: TrellisOrganization;
  healthSummary: { healthy: number; degraded: number; unhealthy: number; total: number };
  onExpandAll: () => void;
  onCollapseAll: () => void;
}

function TrellisHeader({
  organization,
  healthSummary,
  onExpandAll,
  onCollapseAll,
}: TrellisHeaderProps) {
  return (
    <div className="flex items-center justify-between p-4 bg-card rounded-lg border">
      <div className="flex items-center gap-4">
        <div className="p-2 bg-primary/10 rounded-lg">
          <Activity className="h-6 w-6 text-primary" />
        </div>
        <div>
          <h3 className="font-semibold text-lg font-display">{organization.name} Trellis</h3>
          <p className="text-sm text-muted-foreground">
            {organization.projects.length} projects • {healthSummary.total} services
          </p>
        </div>
      </div>

      <div className="flex items-center gap-4">
        {/* Health summary badges */}
        <div className="flex items-center gap-2">
          <Badge variant="default" className="bg-status-success-muted text-status-success-foreground border-status-success/20">
            <Zap className="h-3 w-3 mr-1" />
            {healthSummary.healthy} healthy
          </Badge>
          {healthSummary.degraded > 0 && (
            <Badge variant="outline" className="text-status-warning border-status-warning/30">
              {healthSummary.degraded} deploying
            </Badge>
          )}
          {healthSummary.unhealthy > 0 && (
            <Badge variant="destructive">
              {healthSummary.unhealthy} issues
            </Badge>
          )}
        </div>

        {/* Controls */}
        <div className="flex gap-1">
          <button
            onClick={onExpandAll}
            className="px-2 py-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            Expand all
          </button>
          <span className="text-muted-foreground">|</span>
          <button
            onClick={onCollapseAll}
            className="px-2 py-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            Collapse all
          </button>
        </div>
      </div>
    </div>
  );
}

// -----------------------------------------------------------------------------
// SVG CONNECTOR OVERLAY (For Solarpunk organic effect)
// -----------------------------------------------------------------------------

interface TrellisConnectorSVGProps {
  projects: TrellisProject[];
  expandedProjects: Set<string>;
}

function TrellisConnectorSVG({ projects, expandedProjects }: TrellisConnectorSVGProps) {
  // This is a simplified version - in production you'd calculate actual positions
  // The CSS-based connectors handle the basic case; this SVG overlay adds organic curves

  return (
    <svg
      className="absolute inset-0 w-full h-full pointer-events-none overflow-visible"
      style={{ zIndex: 0 }}
    >
      <defs>
        {/* Gradient for Solarpunk organic look */}
        <linearGradient id="trellis-gradient" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor="hsl(var(--trellis-connector))" stopOpacity="0.6" />
          <stop offset="100%" stopColor="hsl(var(--trellis-connector-active))" stopOpacity="0.3" />
        </linearGradient>

        {/* Glow filter for Solarpunk mode */}
        <filter id="trellis-glow" x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="2" result="blur" />
          <feMerge>
            <feMergeNode in="blur" />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>
      </defs>

      {/* Root connection from org to first project */}
      {projects.length > 0 && (
        <path
          d="M 20,0 Q 20,20 40,40"
          className="trellis-connector"
          stroke="url(#trellis-gradient)"
          strokeWidth="2"
          fill="none"
          filter="url(#trellis-glow)"
        />
      )}
    </svg>
  );
}

