'use client';

import { useState } from 'react';
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Spinner } from '@/components/ui/spinner';
import type { DatabaseAddonType, Project } from "@/app/(protected)/databases/page";

interface CreateDatabaseModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: {
    projectSlug: string;
    type: DatabaseAddonType;
    name: string;
    config: {
      version?: string;
      storage_gb?: number;
      memory?: string;
      replicas?: number;
    };
  }) => Promise<void>;
  projects: Project[];
}

const DATABASE_TYPES = [
  {
    value: 'postgres' as DatabaseAddonType,
    label: 'PostgreSQL',
    description: 'Powerful, open source object-relational database',
    icon: (
      <svg aria-hidden="true" className="w-8 h-8 text-blue-600" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8z"/>
        <path d="M12 6c-3.31 0-6 2.69-6 6s2.69 6 6 6 6-2.69 6-6-2.69-6-6-6zm0 10c-2.21 0-4-1.79-4-4s1.79-4 4-4 4 1.79 4 4-1.79 4-4 4z"/>
      </svg>
    ),
    versions: ['16', '15', '14', '13'],
    defaultVersion: '16',
    hasStorage: true,
    defaultStorage: 10,
    storageOptions: [5, 10, 20, 50, 100],
  },
  {
    value: 'redis' as DatabaseAddonType,
    label: 'Redis',
    description: 'In-memory data structure store, used as cache',
    icon: (
      <svg aria-hidden="true" className="w-8 h-8 text-red-600" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/>
      </svg>
    ),
    versions: ['7', '6'],
    defaultVersion: '7',
    hasStorage: false,
    memoryOptions: ['128Mi', '256Mi', '512Mi', '1Gi', '2Gi'],
    defaultMemory: '256Mi',
  },
];

const MEMORY_OPTIONS = ['128Mi', '256Mi', '512Mi', '1Gi', '2Gi'];

export function CreateDatabaseModal({ isOpen, onClose, onSubmit, projects }: CreateDatabaseModalProps) {
  const [selectedProject, setSelectedProject] = useState<string>('');
  const [selectedType, setSelectedType] = useState<DatabaseAddonType | null>(null);
  const [name, setName] = useState('');
  const [version, setVersion] = useState('');
  const [storageGb, setStorageGb] = useState(10);
  const [memory, setMemory] = useState('256Mi');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const selectedTypeConfig = DATABASE_TYPES.find(t => t.value === selectedType);

  const handleTypeSelect = (type: DatabaseAddonType) => {
    setSelectedType(type);
    const config = DATABASE_TYPES.find(t => t.value === type);
    if (config) {
      setVersion(config.defaultVersion);
      if (config.hasStorage) {
        setStorageGb(config.defaultStorage || 10);
      }
      if (config.defaultMemory) {
        setMemory(config.defaultMemory);
      }
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!selectedProject || !selectedType || !name) {
      setError('Please fill in all required fields');
      return;
    }

    setIsSubmitting(true);
    setError(null);

    try {
      await onSubmit({
        projectSlug: selectedProject,
        type: selectedType,
        name: name.toLowerCase().replace(/[^a-z0-9-]/g, '-'),
        config: {
          version,
          storage_gb: selectedTypeConfig?.hasStorage ? storageGb : undefined,
          memory: selectedType === 'redis' ? memory : undefined,
          replicas: 1,
        },
      });
      // Reset form on success
      setSelectedProject('');
      setSelectedType(null);
      setName('');
      setVersion('');
      setStorageGb(10);
      setMemory('256Mi');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create database');
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-lg max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Create Database</DialogTitle>
          <DialogDescription>
            Provision a new database for your project.
          </DialogDescription>
        </DialogHeader>

        {error && (
          <div className="mb-4 p-3 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive text-sm">
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-6">
          {/* Project Selection */}
          <div className="space-y-2">
            <Label htmlFor="project">Project</Label>
            <Select value={selectedProject} onValueChange={setSelectedProject}>
              <SelectTrigger>
                <SelectValue placeholder="Select a project" />
              </SelectTrigger>
              <SelectContent>
                {projects.map((project) => (
                  <SelectItem key={project.id} value={project.slug}>
                    {project.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Database Type Selection */}
          <div className="space-y-2">
            <Label>Database Type</Label>
            <div className="grid grid-cols-2 gap-3">
              {DATABASE_TYPES.map((type) => (
                <button
                  key={type.value}
                  type="button"
                  onClick={() => handleTypeSelect(type.value)}
                  className={`p-4 border rounded-lg text-left transition-all ${
                    selectedType === type.value
                      ? 'border-primary bg-primary/10 ring-2 ring-primary'
                      : 'border-border hover:border-border/80'
                  }`}
                >
                  <div className="flex items-center gap-3 mb-2">
                    {type.icon}
                    <span className="font-medium">{type.label}</span>
                  </div>
                  <p className="text-xs text-muted-foreground">{type.description}</p>
                </button>
              ))}
            </div>
          </div>

          {/* Name Input */}
          <div className="space-y-2">
            <Label htmlFor="name">Database Name</Label>
            <Input
              id="name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-database"
              pattern="[a-z0-9-]+"
            />
            <p className="text-xs text-muted-foreground">
              Lowercase letters, numbers, and hyphens only
            </p>
          </div>

          {/* Version Selection */}
          {selectedTypeConfig && (
            <div className="space-y-2">
              <Label htmlFor="version">Version</Label>
              <Select value={version} onValueChange={setVersion}>
                <SelectTrigger>
                  <SelectValue placeholder="Select version" />
                </SelectTrigger>
                <SelectContent>
                  {selectedTypeConfig.versions.map((v) => (
                    <SelectItem key={v} value={v}>
                      {selectedTypeConfig.label} {v}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {/* Storage Selection (PostgreSQL) */}
          {selectedTypeConfig?.hasStorage && (
            <div className="space-y-2">
              <Label>Storage Size</Label>
              <div className="flex flex-wrap gap-2">
                {selectedTypeConfig.storageOptions?.map((size) => (
                  <button
                    key={size}
                    type="button"
                    onClick={() => setStorageGb(size)}
                    className={`px-4 py-2 border rounded-md text-sm transition-all ${
                      storageGb === size
                        ? 'border-primary bg-primary/10 text-primary'
                        : 'border-border hover:border-border/80'
                    }`}
                  >
                    {size} GB
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Memory Selection (Redis) */}
          {selectedType === 'redis' && (
            <div className="space-y-2">
              <Label>Memory</Label>
              <div className="flex flex-wrap gap-2">
                {MEMORY_OPTIONS.map((mem) => (
                  <button
                    key={mem}
                    type="button"
                    onClick={() => setMemory(mem)}
                    className={`px-4 py-2 border rounded-md text-sm transition-all ${
                      memory === mem
                        ? 'border-primary bg-primary/10 text-primary'
                        : 'border-border hover:border-border/80'
                    }`}
                  >
                    {mem}
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Actions */}
          <DialogFooter className="pt-4 border-t">
            <Button type="button" variant="outline" onClick={onClose} disabled={isSubmitting}>
              Cancel
            </Button>
            <Button type="submit" disabled={isSubmitting || !selectedProject || !selectedType || !name}>
              {isSubmitting ? (
                <>
                  <Spinner size="sm" className="mr-2" />
                  Creating...
                </>
              ) : (
                'Create Database'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
