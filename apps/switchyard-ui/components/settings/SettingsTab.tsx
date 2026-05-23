'use client';

import { useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from "@enclii/ui-components/button";
import { Input } from "@enclii/ui-components/input";
import { Label } from "@enclii/ui-components/label";
import { apiGet, apiPatch, apiDelete } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import { ServiceVolumesEditor } from './ServiceVolumesEditor';

interface ServiceSettings {
  id: string;
  name: string;
  project_id: string;
  project_name: string;
  git_repo: string;
  app_path: string;
  auto_deploy: boolean;
  auto_deploy_branch: string;
  auto_deploy_env: string;
  build_config?: {
    builder?: string;
    dockerfile?: string;
    build_command?: string;
    output_dir?: string;
  };
  created_at: string;
  updated_at: string;
}

interface SettingsResponse {
  settings: ServiceSettings;
}

interface SettingsTabProps {
  serviceId: string;
  serviceName: string;
}

export function SettingsTab({ serviceId, serviceName }: SettingsTabProps) {
  const router = useRouter();
  const [settings, setSettings] = useState<ServiceSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [deleteConfirmName, setDeleteConfirmName] = useState('');

  // Form state
  const [form, setForm] = useState({
    name: '',
    git_repo: '',
    app_path: '',
    auto_deploy: false,
    auto_deploy_branch: '',
    auto_deploy_env: '',
  });

  const fetchSettings = async () => {
    try {
      setError(null);
      const data = await apiGet<SettingsResponse>(`/v1/services/${serviceId}/settings`);
      setSettings(data.settings);
      setForm({
        name: data.settings.name || '',
        git_repo: data.settings.git_repo || '',
        app_path: data.settings.app_path || '',
        auto_deploy: data.settings.auto_deploy || false,
        auto_deploy_branch: data.settings.auto_deploy_branch || '',
        auto_deploy_env: data.settings.auto_deploy_env || '',
      });
    } catch (err) {
      console.error('Failed to fetch settings:', err);
      setError(err instanceof Error ? err.message : 'Failed to load settings');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchSettings();
  }, [serviceId]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await apiPatch(`/v1/services/${serviceId}`, {
        name: form.name || undefined,
        git_repo: form.git_repo || undefined,
        app_path: form.app_path || undefined,
        auto_deploy: form.auto_deploy,
        auto_deploy_branch: form.auto_deploy_branch || undefined,
        auto_deploy_env: form.auto_deploy_env || undefined,
      });
      await fetchSettings();
      toast.success('Settings saved successfully');
    } catch (err) {
      console.error('Failed to save settings:', err);
      toast.error('Failed to save settings: ' + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (deleteConfirmName !== settings?.name) {
      toast.error('Please type the service name to confirm deletion');
      return;
    }

    setDeleting(true);
    try {
      await apiDelete(`/v1/services/${serviceId}`);
      router.push('/services');
    } catch (err) {
      console.error('Failed to delete service:', err);
      toast.error('Failed to delete service: ' + (err instanceof Error ? err.message : 'Unknown error'));
      setDeleting(false);
    }
  };

  if (loading) {
    return (
      <Card>
        <CardContent className="py-8">
          <div className="flex items-center justify-center">
            <Spinner size="lg" />
            <span className="ml-3 text-muted-foreground">Loading settings...</span>
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className="border-status-error/30 bg-status-error-muted">
        <CardContent className="py-8">
          <div className="text-center">
            <p className="text-status-error font-medium mb-4">{error}</p>
            <Button onClick={fetchSettings} variant="outline">
              Try Again
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      {/* General Settings */}
      <Card>
        <CardHeader>
          <CardTitle>General Settings</CardTitle>
          <CardDescription>Configure basic service settings</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="name">Service Name</Label>
              <Input
                id="name"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="my-service"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="git_repo">Git Repository</Label>
              <Input
                id="git_repo"
                value={form.git_repo}
                onChange={(e) => setForm({ ...form, git_repo: e.target.value })}
                placeholder="owner/repo"
              />
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="app_path">Application Path</Label>
            <Input
              id="app_path"
              value={form.app_path}
              onChange={(e) => setForm({ ...form, app_path: e.target.value })}
              placeholder="apps/api (leave empty for root)"
            />
            <p className="text-sm text-muted-foreground">
              Path to the application within the repository (for monorepos)
            </p>
          </div>
        </CardContent>
      </Card>

      {/* Auto Deploy Settings */}
      <Card>
        <CardHeader>
          <CardTitle>Auto Deploy</CardTitle>
          <CardDescription>Configure automatic deployments from Git pushes</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="auto_deploy"
              checked={form.auto_deploy}
              onChange={(e) => setForm({ ...form, auto_deploy: e.target.checked })}
              className="h-4 w-4 rounded border-input"
            />
            <Label htmlFor="auto_deploy">Enable auto deploy</Label>
          </div>
          {form.auto_deploy && (
            <div className="grid gap-4 md:grid-cols-2 pt-2">
              <div className="space-y-2">
                <Label htmlFor="auto_deploy_branch">Deploy Branch</Label>
                <Input
                  id="auto_deploy_branch"
                  value={form.auto_deploy_branch}
                  onChange={(e) => setForm({ ...form, auto_deploy_branch: e.target.value })}
                  placeholder="main"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="auto_deploy_env">Target Environment</Label>
                <Input
                  id="auto_deploy_env"
                  value={form.auto_deploy_env}
                  onChange={(e) => setForm({ ...form, auto_deploy_env: e.target.value })}
                  placeholder="production"
                />
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <ServiceVolumesEditor serviceId={serviceId} />

      {/* Build Configuration (read-only for now) */}
      {settings?.build_config && (
        <Card>
          <CardHeader>
            <CardTitle>Build Configuration</CardTitle>
            <CardDescription>Current build settings (read-only)</CardDescription>
          </CardHeader>
          <CardContent>
            <dl className="space-y-3">
              {settings.build_config.builder && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Builder</dt>
                  <dd className="font-mono text-sm">{settings.build_config.builder}</dd>
                </div>
              )}
              {settings.build_config.dockerfile && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Dockerfile</dt>
                  <dd className="font-mono text-sm">{settings.build_config.dockerfile}</dd>
                </div>
              )}
              {settings.build_config.build_command && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Build Command</dt>
                  <dd className="font-mono text-sm">{settings.build_config.build_command}</dd>
                </div>
              )}
              {settings.build_config.output_dir && (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Output Directory</dt>
                  <dd className="font-mono text-sm">{settings.build_config.output_dir}</dd>
                </div>
              )}
            </dl>
          </CardContent>
        </Card>
      )}

      {/* Save Button */}
      <div className="flex justify-end">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? 'Saving...' : 'Save Changes'}
        </Button>
      </div>

      {/* Danger Zone */}
      <Card className="border-destructive/30">
        <CardHeader>
          <CardTitle className="text-destructive">Danger Zone</CardTitle>
          <CardDescription>Irreversible actions</CardDescription>
        </CardHeader>
        <CardContent>
          {!showDeleteConfirm ? (
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">Delete this service</p>
                <p className="text-sm text-muted-foreground">
                  This will permanently delete the service, all deployments, environment variables, and associated resources.
                </p>
              </div>
              <Button
                variant="destructive"
                onClick={() => setShowDeleteConfirm(true)}
              >
                Delete Service
              </Button>
            </div>
          ) : (
            <div className="space-y-4 p-4 bg-status-error-muted rounded-lg">
              <p className="text-status-error font-medium">
                Are you sure you want to delete &quot;{settings?.name}&quot;?
              </p>
              <p className="text-sm text-status-error">
                Type <strong>{settings?.name}</strong> to confirm:
              </p>
              <Input
                value={deleteConfirmName}
                onChange={(e) => setDeleteConfirmName(e.target.value)}
                placeholder={settings?.name}
                className="max-w-xs"
              />
              <div className="flex gap-2">
                <Button
                  variant="destructive"
                  onClick={handleDelete}
                  disabled={deleting || deleteConfirmName !== settings?.name}
                >
                  {deleting ? 'Deleting...' : 'Delete Service'}
                </Button>
                <Button
                  variant="outline"
                  onClick={() => {
                    setShowDeleteConfirm(false);
                    setDeleteConfirmName('');
                  }}
                >
                  Cancel
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
