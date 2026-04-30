'use client';

import { useState } from 'react';
import { Button } from "@enclii/ui-components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@enclii/ui-components/dialog";
import { Spinner } from '@/components/ui/spinner';
import type { TemplateCategory } from "@/app/(protected)/templates/page";

interface ImportTemplateModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

interface ImportFormData {
  repoUrl: string;
  name: string;
  description: string;
  category: TemplateCategory;
  framework: string;
  language: string;
  branch: string;
  tags: string;
}

const CATEGORIES: { value: TemplateCategory; label: string }[] = [
  { value: 'fullstack', label: 'Full Stack' },
  { value: 'frontend', label: 'Frontend' },
  { value: 'backend', label: 'Backend' },
  { value: 'api', label: 'API' },
  { value: 'microservice', label: 'Microservice' },
  { value: 'static', label: 'Static Site' },
  { value: 'monorepo', label: 'Monorepo' },
  { value: 'database', label: 'Database' },
];

const FRAMEWORKS = [
  'Next.js', 'React', 'Vue', 'Nuxt.js', 'Svelte', 'SvelteKit', 'Astro', 'Remix',
  'Express', 'Fastify', 'NestJS', 'Hono',
  'FastAPI', 'Django', 'Flask',
  'Go', 'Gin', 'Fiber', 'Echo',
  'Ruby on Rails', 'Sinatra',
  'Spring Boot', 'Quarkus',
  'Other'
];

const LANGUAGES = [
  'TypeScript', 'JavaScript', 'Python', 'Go', 'Ruby', 'Java', 'Rust', 'PHP', 'Other'
];

export function ImportTemplateModal({ isOpen, onClose, onSuccess }: ImportTemplateModalProps) {
  const [formData, setFormData] = useState<ImportFormData>({
    repoUrl: '',
    name: '',
    description: '',
    category: 'fullstack',
    framework: 'Next.js',
    language: 'TypeScript',
    branch: 'main',
    tags: '',
  });
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const parseGitHubUrl = (url: string) => {
    // Try to extract repo name from GitHub URL
    const match = url.match(/github\.com[\/:]([^\/]+)\/([^\/\s.]+)/);
    if (match) {
      return {
        owner: match[1],
        repo: match[2].replace(/\.git$/, ''),
      };
    }
    return null;
  };

  const handleRepoUrlChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const url = e.target.value;
    setFormData(prev => ({ ...prev, repoUrl: url }));

    // Auto-fill name from repo name if not already set
    if (!formData.name) {
      const parsed = parseGitHubUrl(url);
      if (parsed) {
        const name = parsed.repo
          .split('-')
          .map(word => word.charAt(0).toUpperCase() + word.slice(1))
          .join(' ');
        setFormData(prev => ({ ...prev, name }));
      }
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    // Validate GitHub URL
    if (!formData.repoUrl.includes('github.com')) {
      setError('Please enter a valid GitHub repository URL');
      return;
    }

    setIsSubmitting(true);

    try {
      const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4200";
      const storedTokens = localStorage.getItem("enclii_tokens");
      const tokens = storedTokens ? JSON.parse(storedTokens) : {};

      const response = await fetch(`${API_BASE_URL}/v1/templates/import`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': tokens.accessToken ? `Bearer ${tokens.accessToken}` : '',
        },
        body: JSON.stringify({
          repo_url: formData.repoUrl,
          name: formData.name,
          description: formData.description,
          category: formData.category,
          framework: formData.framework,
          language: formData.language,
          branch: formData.branch || 'main',
          tags: formData.tags ? formData.tags.split(',').map(t => t.trim()).filter(Boolean) : [],
        }),
      });

      if (!response.ok) {
        const data = await response.json().catch(() => ({}));
        throw new Error(data.error || `Failed to import template: ${response.status}`);
      }

      // Success - reset form and close
      setFormData({
        repoUrl: '',
        name: '',
        description: '',
        category: 'fullstack',
        framework: 'Next.js',
        language: 'TypeScript',
        branch: 'main',
        tags: '',
      });
      onSuccess();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to import template');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleClose = () => {
    setFormData({
      repoUrl: '',
      name: '',
      description: '',
      category: 'fullstack',
      framework: 'Next.js',
      language: 'TypeScript',
      branch: 'main',
      tags: '',
    });
    setError(null);
    onClose();
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => { if (!open) handleClose(); }}>
      <DialogContent className="sm:max-w-lg max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <svg aria-hidden="true" className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
            </svg>
            Import Template from GitHub
          </DialogTitle>
          <DialogDescription>
            Import a GitHub repository as a reusable template.
          </DialogDescription>
        </DialogHeader>

        {/* Form */}
        <form onSubmit={handleSubmit} className="space-y-4">
          {error && (
            <div className="p-3 bg-status-error-muted border border-status-error/30 rounded-md">
              <p className="text-sm text-status-error">{error}</p>
            </div>
          )}

            {/* GitHub URL */}
            <div>
              <label htmlFor="repoUrl" className="block text-sm font-medium text-foreground mb-1">
                GitHub Repository URL <span className="text-destructive">*</span>
              </label>
              <input
                type="url"
                id="repoUrl"
                required
                placeholder="https://github.com/username/repository"
                className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent"
                value={formData.repoUrl}
                onChange={handleRepoUrlChange}
              />
            </div>

            {/* Name */}
            <div>
              <label htmlFor="name" className="block text-sm font-medium text-foreground mb-1">
                Template Name <span className="text-destructive">*</span>
              </label>
              <input
                type="text"
                id="name"
                required
                placeholder="My Awesome Template"
                className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent"
                value={formData.name}
                onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
              />
            </div>

            {/* Description */}
            <div>
              <label htmlFor="description" className="block text-sm font-medium text-foreground mb-1">
                Description
              </label>
              <textarea
                id="description"
                rows={2}
                placeholder="A brief description of this template"
                className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent resize-none"
                value={formData.description}
                onChange={(e) => setFormData(prev => ({ ...prev, description: e.target.value }))}
              />
            </div>

            {/* Category & Framework */}
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label htmlFor="category" className="block text-sm font-medium text-foreground mb-1">
                  Category <span className="text-destructive">*</span>
                </label>
                <select
                  id="category"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md bg-card focus:outline-none focus:ring-2 focus:ring-ring"
                  value={formData.category}
                  onChange={(e) => setFormData(prev => ({ ...prev, category: e.target.value as TemplateCategory }))}
                >
                  {CATEGORIES.map(cat => (
                    <option key={cat.value} value={cat.value}>{cat.label}</option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor="framework" className="block text-sm font-medium text-foreground mb-1">
                  Framework <span className="text-destructive">*</span>
                </label>
                <select
                  id="framework"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md bg-card focus:outline-none focus:ring-2 focus:ring-ring"
                  value={formData.framework}
                  onChange={(e) => setFormData(prev => ({ ...prev, framework: e.target.value }))}
                >
                  {FRAMEWORKS.map(fw => (
                    <option key={fw} value={fw}>{fw}</option>
                  ))}
                </select>
              </div>
            </div>

            {/* Language & Branch */}
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label htmlFor="language" className="block text-sm font-medium text-foreground mb-1">
                  Language <span className="text-destructive">*</span>
                </label>
                <select
                  id="language"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md bg-card focus:outline-none focus:ring-2 focus:ring-ring"
                  value={formData.language}
                  onChange={(e) => setFormData(prev => ({ ...prev, language: e.target.value }))}
                >
                  {LANGUAGES.map(lang => (
                    <option key={lang} value={lang}>{lang}</option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor="branch" className="block text-sm font-medium text-foreground mb-1">
                  Branch
                </label>
                <input
                  type="text"
                  id="branch"
                  placeholder="main"
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent"
                  value={formData.branch}
                  onChange={(e) => setFormData(prev => ({ ...prev, branch: e.target.value }))}
                />
              </div>
            </div>

            {/* Tags */}
            <div>
              <label htmlFor="tags" className="block text-sm font-medium text-foreground mb-1">
                Tags
              </label>
              <input
                type="text"
                id="tags"
                placeholder="react, typescript, tailwind (comma-separated)"
                className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent"
                value={formData.tags}
                onChange={(e) => setFormData(prev => ({ ...prev, tags: e.target.value }))}
              />
              <p className="mt-1 text-xs text-muted-foreground">Comma-separated list of tags</p>
            </div>

          {/* Actions */}
          <DialogFooter className="pt-4 border-t">
            <Button type="button" variant="outline" onClick={handleClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={isSubmitting || !formData.repoUrl || !formData.name}>
              {isSubmitting ? (
                <>
                  <Spinner size="sm" className="mr-2" />
                  Importing...
                </>
              ) : (
                <>
                  <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
                  </svg>
                  Import Template
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
