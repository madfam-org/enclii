'use client';

import { useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { FunctionRuntime, FunctionConfig } from './FunctionCard';

interface Project {
  id: string;
  name: string;
  slug: string;
}

interface CreateFunctionModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projects: Project[];
  onSubmit: (data: {
    projectSlug: string;
    name: string;
    config: Partial<FunctionConfig>;
  }) => Promise<void>;
}

const RUNTIMES: { value: FunctionRuntime; label: string; description: string }[] = [
  { value: 'go', label: 'Go', description: 'Fast cold starts, great for APIs' },
  { value: 'python', label: 'Python', description: 'Great for data processing and ML' },
  { value: 'node', label: 'Node.js', description: 'Ideal for web backends' },
  { value: 'rust', label: 'Rust', description: 'Maximum performance, fast cold starts' },
];

const MEMORY_OPTIONS = [
  { value: '128Mi', label: '128 MB' },
  { value: '256Mi', label: '256 MB' },
  { value: '512Mi', label: '512 MB' },
  { value: '1Gi', label: '1 GB' },
  { value: '2Gi', label: '2 GB' },
];

const DEFAULT_HANDLERS: Record<FunctionRuntime, string> = {
  go: 'main.Handler',
  python: 'handler.main',
  node: 'handler.main',
  rust: 'handler',
};

export function CreateFunctionModal({ open, onOpenChange, projects, onSubmit }: CreateFunctionModalProps) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [formData, setFormData] = useState({
    projectSlug: '',
    name: '',
    runtime: 'go' as FunctionRuntime,
    handler: DEFAULT_HANDLERS['go'],
    memory: '256Mi',
    timeout: 30,
    minReplicas: 0,
    maxReplicas: 10,
  });

  const handleRuntimeChange = (runtime: FunctionRuntime) => {
    setFormData(prev => ({
      ...prev,
      runtime,
      handler: DEFAULT_HANDLERS[runtime],
    }));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);

    // Validate
    if (!formData.projectSlug) {
      setError('Please select a project');
      setLoading(false);
      return;
    }
    if (!formData.name) {
      setError('Please enter a function name');
      setLoading(false);
      return;
    }
    if (!/^[a-z][a-z0-9-]*$/.test(formData.name)) {
      setError('Function name must start with a letter and contain only lowercase letters, numbers, and hyphens');
      setLoading(false);
      return;
    }

    try {
      await onSubmit({
        projectSlug: formData.projectSlug,
        name: formData.name,
        config: {
          runtime: formData.runtime,
          handler: formData.handler,
          memory: formData.memory,
          timeout: formData.timeout,
          min_replicas: formData.minReplicas,
          max_replicas: formData.maxReplicas,
        },
      });
      // Reset form on success
      setFormData({
        projectSlug: '',
        name: '',
        runtime: 'go',
        handler: DEFAULT_HANDLERS['go'],
        memory: '256Mi',
        timeout: 30,
        minReplicas: 0,
        maxReplicas: 10,
      });
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create function');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle>Create Function</DialogTitle>
          <DialogDescription>
            Create a new serverless function with scale-to-zero support.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          {error && (
            <div className="p-3 text-sm text-destructive bg-destructive/10 rounded-md">
              {error}
            </div>
          )}

          {/* Project Selection */}
          <div className="space-y-2">
            <Label htmlFor="project">Project</Label>
            <Select
              value={formData.projectSlug}
              onValueChange={(value) => setFormData(prev => ({ ...prev, projectSlug: value }))}
            >
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

          {/* Function Name */}
          <div className="space-y-2">
            <Label htmlFor="name">Function Name</Label>
            <Input
              id="name"
              placeholder="my-function"
              value={formData.name}
              onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value.toLowerCase() }))}
            />
            <p className="text-xs text-muted-foreground">
              Lowercase letters, numbers, and hyphens only
            </p>
          </div>

          {/* Runtime Selection */}
          <div className="space-y-2">
            <Label>Runtime</Label>
            <div className="grid grid-cols-2 gap-2">
              {RUNTIMES.map((runtime) => (
                <button
                  key={runtime.value}
                  type="button"
                  className={`p-3 text-left rounded-md border transition-colors ${
                    formData.runtime === runtime.value
                      ? 'border-primary bg-primary/5'
                      : 'border-border hover:border-primary/50'
                  }`}
                  onClick={() => handleRuntimeChange(runtime.value)}
                >
                  <div className="font-medium">{runtime.label}</div>
                  <div className="text-xs text-muted-foreground">{runtime.description}</div>
                </button>
              ))}
            </div>
          </div>

          {/* Handler */}
          <div className="space-y-2">
            <Label htmlFor="handler">Handler</Label>
            <Input
              id="handler"
              placeholder="main.Handler"
              value={formData.handler}
              onChange={(e) => setFormData(prev => ({ ...prev, handler: e.target.value }))}
            />
            <p className="text-xs text-muted-foreground">
              The entry point for your function
            </p>
          </div>

          {/* Memory */}
          <div className="space-y-2">
            <Label>Memory</Label>
            <Select
              value={formData.memory}
              onValueChange={(value) => setFormData(prev => ({ ...prev, memory: value }))}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {MEMORY_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Timeout */}
          <div className="space-y-2">
            <Label htmlFor="timeout">Timeout (seconds)</Label>
            <Input
              id="timeout"
              type="number"
              min={1}
              max={900}
              value={formData.timeout}
              onChange={(e) => setFormData(prev => ({ ...prev, timeout: parseInt(e.target.value) || 30 }))}
            />
          </div>

          {/* Scaling */}
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="minReplicas">Min Replicas</Label>
              <Input
                id="minReplicas"
                type="number"
                min={0}
                max={10}
                value={formData.minReplicas}
                onChange={(e) => setFormData(prev => ({ ...prev, minReplicas: parseInt(e.target.value) || 0 }))}
              />
              <p className="text-xs text-muted-foreground">
                0 enables scale-to-zero
              </p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="maxReplicas">Max Replicas</Label>
              <Input
                id="maxReplicas"
                type="number"
                min={1}
                max={100}
                value={formData.maxReplicas}
                onChange={(e) => setFormData(prev => ({ ...prev, maxReplicas: parseInt(e.target.value) || 10 }))}
              />
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={loading}>
              {loading ? 'Creating...' : 'Create Function'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
