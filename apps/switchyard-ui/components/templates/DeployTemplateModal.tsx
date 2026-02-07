'use client';

import { useState } from 'react';
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Spinner } from '@/components/ui/spinner';
import type { Template } from "@/app/(protected)/templates/page";

interface DeployTemplateModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: { projectName: string; projectSlug: string }) => Promise<void>;
  template: Template | null;
}

export function DeployTemplateModal({ isOpen, onClose, onSubmit, template }: DeployTemplateModalProps) {
  const [projectName, setProjectName] = useState('');
  const [projectSlug, setProjectSlug] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const generateSlug = (name: string) => {
    return name
      .toLowerCase()
      .replace(/[^a-z0-9\s-]/g, '')
      .replace(/\s+/g, '-')
      .replace(/-+/g, '-')
      .replace(/^-|-$/g, '');
  };

  const handleNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const name = e.target.value;
    setProjectName(name);
    if (!projectSlug || projectSlug === generateSlug(projectName)) {
      setProjectSlug(generateSlug(name));
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setIsSubmitting(true);

    try {
      await onSubmit({
        projectName,
        projectSlug: projectSlug || generateSlug(projectName),
      });
      // Reset form on success
      setProjectName('');
      setProjectSlug('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to deploy template');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleClose = () => {
    setProjectName('');
    setProjectSlug('');
    setError(null);
    onClose();
  };

  return (
    <Dialog open={isOpen && !!template} onOpenChange={(open) => { if (!open) handleClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Deploy Template</DialogTitle>
          <DialogDescription>
            Create a new project from a template.
          </DialogDescription>
        </DialogHeader>

        {/* Template Info */}
        {template && (
          <div className="p-4 bg-muted/50 rounded-lg">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-card border">
                <svg aria-hidden="true" className="w-6 h-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 5a1 1 0 011-1h14a1 1 0 011 1v2a1 1 0 01-1 1H5a1 1 0 01-1-1V5zM4 13a1 1 0 011-1h6a1 1 0 011 1v6a1 1 0 01-1 1H5a1 1 0 01-1-1v-6zM16 13a1 1 0 011-1h2a1 1 0 011 1v6a1 1 0 01-1 1h-2a1 1 0 01-1-1v-6z" />
                </svg>
              </div>
              <div>
                <p className="font-medium text-foreground">{template.name}</p>
                <p className="text-sm text-muted-foreground">{template.framework} &bull; {template.language}</p>
              </div>
            </div>
          </div>
        )}

        {/* Form */}
        <form onSubmit={handleSubmit}>
          {error && (
            <div className="mb-4 p-3 bg-status-error-muted border border-status-error/30 rounded-md">
              <p className="text-sm text-status-error">{error}</p>
            </div>
          )}

          <div className="space-y-4">
            <div>
              <label htmlFor="projectName" className="block text-sm font-medium text-foreground mb-1">
                Project Name
              </label>
              <input
                type="text"
                id="projectName"
                required
                placeholder="My Awesome Project"
                className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent"
                value={projectName}
                onChange={handleNameChange}
              />
            </div>

            <div>
              <label htmlFor="projectSlug" className="block text-sm font-medium text-foreground mb-1">
                Project Slug
              </label>
              <input
                type="text"
                id="projectSlug"
                required
                placeholder="my-awesome-project"
                className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent"
                value={projectSlug}
                onChange={(e) => setProjectSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
              />
              <p className="mt-1 text-xs text-muted-foreground">
                Used in URLs: app.enclii.dev/projects/{projectSlug || 'your-project'}
              </p>
            </div>
          </div>

          {/* What will be created */}
          {template && (
            <div className="mt-4 p-3 bg-status-info-muted rounded-md">
              <p className="text-sm font-medium text-status-info-foreground mb-2">This will create:</p>
              <ul className="text-sm text-status-info space-y-1">
                <li className="flex items-center">
                  <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                  </svg>
                  A new project from the {template.name} template
                </li>
                <li className="flex items-center">
                  <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                  </svg>
                  Pre-configured with {template.framework}
                </li>
                <li className="flex items-center">
                  <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                  </svg>
                  Ready-to-deploy service configuration
                </li>
              </ul>
            </div>
          )}

          {/* Actions */}
          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" onClick={handleClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={isSubmitting || !projectName}>
              {isSubmitting ? (
                <>
                  <Spinner size="sm" className="mr-2" />
                  Deploying...
                </>
              ) : (
                <>
                  <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
                  </svg>
                  Deploy Template
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
