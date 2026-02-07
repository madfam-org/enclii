'use client';

import { useState, useEffect, useMemo } from "react";
import { useRouter } from "next/navigation";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { apiGet, apiPost } from "@/lib/api";
import { Spinner } from "@/components/ui/spinner";
import { TemplateCard } from "@/components/templates/TemplateCard";
import { DeployTemplateModal } from "@/components/templates/DeployTemplateModal";
import { ImportTemplateModal } from "@/components/templates/ImportTemplateModal";

// Template types matching the API
export type TemplateCategory = 'fullstack' | 'frontend' | 'backend' | 'api' | 'database' | 'microservice' | 'monorepo' | 'static';

export interface Template {
  id: string;
  name: string;
  slug: string;
  description: string;
  category: TemplateCategory;
  framework: string;
  language: string;
  repo_url: string;
  branch: string;
  icon_url?: string;
  preview_url?: string;
  readme_url?: string;
  config?: Record<string, unknown>;
  env_template?: Record<string, string>;
  services?: Array<{
    name: string;
    type: string;
    port?: number;
  }>;
  tags?: string[];
  featured: boolean;
  official: boolean;
  deploy_count: number;
  created_at: string;
  updated_at: string;
}

interface ListTemplatesResponse {
  templates: Template[];
  count: number;
}

interface TemplateFiltersResponse {
  categories: Record<string, number>;
  frameworks: Record<string, number>;
}

interface DeployTemplateResponse {
  deployment: {
    id: string;
    status: string;
  };
  project: {
    id: string;
    name: string;
    slug: string;
  };
  message: string;
}

export default function TemplatesPage() {
  const router = useRouter();
  const [templates, setTemplates] = useState<Template[]>([]);
  const [featuredTemplates, setFeaturedTemplates] = useState<Template[]>([]);
  const [filters, setFilters] = useState<TemplateFiltersResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Filter state
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedCategory, setSelectedCategory] = useState<string>('');
  const [selectedFramework, setSelectedFramework] = useState<string>('');
  const [showFeaturedOnly, setShowFeaturedOnly] = useState(false);

  // Deploy modal state
  const [isDeployModalOpen, setIsDeployModalOpen] = useState(false);
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);

  // Import modal state
  const [isImportModalOpen, setIsImportModalOpen] = useState(false);

  const fetchTemplates = async () => {
    try {
      setError(null);

      // Build query params
      const params = new URLSearchParams();
      if (selectedCategory) params.append('category', selectedCategory);
      if (selectedFramework) params.append('framework', selectedFramework);
      if (searchQuery) params.append('search', searchQuery);
      if (showFeaturedOnly) params.append('featured', 'true');

      const queryString = params.toString();
      const endpoint = queryString ? `/v1/templates?${queryString}` : '/v1/templates';

      const [templatesData, featuredData, filtersData] = await Promise.all([
        apiGet<ListTemplatesResponse>(endpoint),
        apiGet<ListTemplatesResponse>('/v1/templates/featured?limit=4'),
        apiGet<TemplateFiltersResponse>('/v1/templates/filters'),
      ]);

      setTemplates(templatesData.templates || []);
      setFeaturedTemplates(featuredData.templates || []);
      setFilters(filtersData);
      setLoading(false);
    } catch (err) {
      console.error("Failed to fetch templates:", err);
      setError(err instanceof Error ? err.message : "Failed to fetch templates");
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchTemplates();
  }, [selectedCategory, selectedFramework, showFeaturedOnly]);

  // Debounced search
  useEffect(() => {
    const timer = setTimeout(() => {
      if (searchQuery !== '') {
        fetchTemplates();
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [searchQuery]);

  const handleDeploy = (template: Template) => {
    setSelectedTemplate(template);
    setIsDeployModalOpen(true);
  };

  const handleDeploySubmit = async (data: { projectName: string; projectSlug: string }) => {
    if (!selectedTemplate) return;

    try {
      const response = await apiPost<DeployTemplateResponse>(
        `/v1/templates/${selectedTemplate.slug}/deploy`,
        {
          project_name: data.projectName,
          project_slug: data.projectSlug,
        }
      );

      setIsDeployModalOpen(false);
      setSelectedTemplate(null);

      // Navigate to the new project
      router.push(`/projects/${response.project.slug}`);
    } catch (err) {
      throw err; // Let the modal handle the error
    }
  };

  const clearFilters = () => {
    setSearchQuery('');
    setSelectedCategory('');
    setSelectedFramework('');
    setShowFeaturedOnly(false);
  };

  const hasActiveFilters = searchQuery || selectedCategory || selectedFramework || showFeaturedOnly;

  // Filter templates for display (client-side filtering for search)
  const filteredTemplates = useMemo(() => {
    if (!searchQuery) return templates;
    const query = searchQuery.toLowerCase();
    return templates.filter(t =>
      t.name.toLowerCase().includes(query) ||
      t.description.toLowerCase().includes(query) ||
      t.framework.toLowerCase().includes(query) ||
      t.language.toLowerCase().includes(query) ||
      t.tags?.some(tag => tag.toLowerCase().includes(query))
    );
  }, [templates, searchQuery]);

  if (loading) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-8">
          <h1 className="text-3xl font-bold">Templates</h1>
          <p className="text-muted-foreground mt-2">
            Start your project with a pre-configured template
          </p>
        </div>
        <Card>
          <CardContent className="py-12">
            <div className="flex items-center justify-center">
              <Spinner size="lg" />
              <span className="ml-3 text-muted-foreground">Loading templates...</span>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (error) {
    return (
      <div className="container mx-auto py-8">
        <div className="mb-8">
          <h1 className="text-3xl font-bold">Templates</h1>
          <p className="text-muted-foreground mt-2">
            Start your project with a pre-configured template
          </p>
        </div>
        <Card className="border-status-error/30 bg-status-error-muted">
          <CardContent className="py-8">
            <div className="text-center">
              <p className="text-status-error font-medium mb-4">{error}</p>
              <Button variant="outline" onClick={fetchTemplates}>
                Try Again
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="container mx-auto py-8">
      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-bold">Templates</h1>
          <p className="text-muted-foreground mt-2">
            Start your project with a pre-configured template. Deploy in seconds.
          </p>
        </div>
        <Button onClick={() => setIsImportModalOpen(true)} variant="outline">
          <svg aria-hidden="true" className="w-4 h-4 mr-2" viewBox="0 0 24 24" fill="currentColor">
            <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
          </svg>
          Import Template
        </Button>
      </div>

      {/* Featured Templates (only show when no filters active) */}
      {!hasActiveFilters && featuredTemplates.length > 0 && (
        <div className="mb-10">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-xl font-semibold flex items-center gap-2">
              <svg aria-hidden="true" className="w-5 h-5 text-amber-500" fill="currentColor" viewBox="0 0 24 24">
                <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
              </svg>
              Featured Templates
            </h2>
            <Button variant="ghost" size="sm" onClick={() => setShowFeaturedOnly(true)}>
              View all featured
            </Button>
          </div>
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            {featuredTemplates.map((template) => (
              <TemplateCard
                key={template.id}
                template={template}
                onDeploy={handleDeploy}
              />
            ))}
          </div>
        </div>
      )}

      {/* Search and Filters */}
      <div className="mb-6 space-y-4">
        {/* Search Bar */}
        <div className="relative">
          <svg
            aria-hidden="true"
            className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-muted-foreground"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            type="text"
            placeholder="Search templates..."
            className="w-full pl-10 pr-4 py-2 border border-input rounded-lg focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent bg-background text-foreground"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
        </div>

        {/* Filter Pills */}
        <div className="flex flex-wrap items-center gap-3">
          {/* Category Filter */}
          <select
            className="px-3 py-2 border border-input rounded-lg bg-card focus:outline-none focus:ring-2 focus:ring-ring"
            value={selectedCategory}
            onChange={(e) => setSelectedCategory(e.target.value)}
          >
            <option value="">All Categories</option>
            {filters?.categories && Object.entries(filters.categories).map(([category, count]) => (
              <option key={category} value={category}>
                {category.charAt(0).toUpperCase() + category.slice(1)} ({count})
              </option>
            ))}
          </select>

          {/* Framework Filter */}
          <select
            className="px-3 py-2 border border-input rounded-lg bg-card focus:outline-none focus:ring-2 focus:ring-ring"
            value={selectedFramework}
            onChange={(e) => setSelectedFramework(e.target.value)}
          >
            <option value="">All Frameworks</option>
            {filters?.frameworks && Object.entries(filters.frameworks).map(([framework, count]) => (
              <option key={framework} value={framework}>
                {framework} ({count})
              </option>
            ))}
          </select>

          {/* Featured Toggle */}
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={showFeaturedOnly}
              onChange={(e) => setShowFeaturedOnly(e.target.checked)}
              className="w-4 h-4 rounded border-input text-primary focus:ring-ring"
            />
            <span className="text-sm text-muted-foreground">Featured only</span>
          </label>

          {/* Clear Filters */}
          {hasActiveFilters && (
            <Button variant="ghost" size="sm" onClick={clearFilters}>
              <svg aria-hidden="true" className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
              Clear filters
            </Button>
          )}

          {/* Results count */}
          <span className="text-sm text-muted-foreground ml-auto">
            {filteredTemplates.length} template{filteredTemplates.length !== 1 ? 's' : ''}
          </span>
        </div>
      </div>

      {/* Templates Grid */}
      {filteredTemplates.length === 0 ? (
        <Card>
          <CardContent className="py-16">
            <div className="text-center">
              <div className="mx-auto w-16 h-16 mb-4 rounded-full bg-muted flex items-center justify-center">
                <svg aria-hidden="true" className="w-8 h-8 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 5a1 1 0 011-1h14a1 1 0 011 1v2a1 1 0 01-1 1H5a1 1 0 01-1-1V5zM4 13a1 1 0 011-1h6a1 1 0 011 1v6a1 1 0 01-1 1H5a1 1 0 01-1-1v-6zM16 13a1 1 0 011-1h2a1 1 0 011 1v6a1 1 0 01-1 1h-2a1 1 0 01-1-1v-6z" />
                </svg>
              </div>
              <h3 className="text-lg font-medium mb-2">No templates found</h3>
              <p className="text-muted-foreground mb-6 max-w-md mx-auto">
                {hasActiveFilters
                  ? "Try adjusting your search or filters to find what you're looking for."
                  : "No templates are available at the moment. Check back later!"}
              </p>
              {hasActiveFilters && (
                <Button variant="outline" onClick={clearFilters}>
                  Clear filters
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {filteredTemplates.map((template) => (
            <TemplateCard
              key={template.id}
              template={template}
              onDeploy={handleDeploy}
            />
          ))}
        </div>
      )}

      {/* Import from GitHub CTA */}
      <div className="mt-12 p-6 bg-gradient-to-r from-muted/50 to-primary/5 rounded-lg border">
        <div className="flex flex-col md:flex-row items-center justify-between gap-4">
          <div className="flex items-center gap-4">
            <div className="p-3 bg-card rounded-lg shadow-sm">
              <svg aria-hidden="true" className="w-8 h-8 text-foreground" viewBox="0 0 24 24" fill="currentColor">
                <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
              </svg>
            </div>
            <div>
              <h3 className="font-semibold text-foreground">Have your own repository?</h3>
              <p className="text-sm text-muted-foreground">Import directly from GitHub to create a new service</p>
            </div>
          </div>
          <Button onClick={() => router.push('/services/import')}>
            <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="currentColor" viewBox="0 0 24 24">
              <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
            </svg>
            Import from GitHub
          </Button>
        </div>
      </div>

      {/* Deploy Modal */}
      <DeployTemplateModal
        isOpen={isDeployModalOpen}
        onClose={() => {
          setIsDeployModalOpen(false);
          setSelectedTemplate(null);
        }}
        onSubmit={handleDeploySubmit}
        template={selectedTemplate}
      />

      {/* Import Template Modal */}
      <ImportTemplateModal
        isOpen={isImportModalOpen}
        onClose={() => setIsImportModalOpen(false)}
        onSuccess={fetchTemplates}
      />
    </div>
  );
}
