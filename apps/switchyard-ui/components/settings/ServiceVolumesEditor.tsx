'use client';

import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@enclii/ui-components/button';
import { Input } from '@enclii/ui-components/input';
import { Label } from '@enclii/ui-components/label';
import { apiGet, apiPatch } from '@/lib/api';
import type { ServiceVolume } from '@madfam/enclii-sdk';
import { Spinner } from '@/components/ui/spinner';

interface SettingsResponse {
  settings: {
    volumes?: ServiceVolume[];
  };
}

interface ServiceVolumesEditorProps {
  serviceId: string;
}

const emptyVolume = (): ServiceVolume => ({
  name: '',
  mount_path: '/data',
  size: '10Gi',
  storage_class_name: 'longhorn',
  access_mode: 'ReadWriteOnce',
});

export function ServiceVolumesEditor({ serviceId }: ServiceVolumesEditorProps) {
  const [volumes, setVolumes] = useState<ServiceVolume[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet<SettingsResponse>(`/v1/services/${serviceId}/settings`);
      setVolumes(data.settings.volumes ?? []);
    } catch (err) {
      console.error('Failed to load volumes:', err);
      toast.error('Failed to load persistent volumes');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, [serviceId]);

  const save = async () => {
    setSaving(true);
    try {
      await apiPatch(`/v1/services/${serviceId}`, { volumes });
      toast.success('Persistent volumes saved');
    } catch (err) {
      console.error('Failed to save volumes:', err);
      toast.error(err instanceof Error ? err.message : 'Failed to save volumes');
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center py-8">
        <Spinner className="h-5 w-5" />
        <span className="ml-3 text-muted-foreground">Loading volumes...</span>
      </div>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Persistent volumes</CardTitle>
        <CardDescription>
          PVCs are created on deploy via the reconciler. Changes apply on the next deployment.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {volumes.length === 0 && (
          <p className="text-sm text-muted-foreground">No volumes configured for this service.</p>
        )}

        {volumes.map((vol, index) => (
          <div key={index} className="grid gap-3 rounded-lg border p-4 md:grid-cols-2">
            <div>
              <Label htmlFor={`vol-name-${index}`}>Name</Label>
              <Input
                id={`vol-name-${index}`}
                value={vol.name}
                onChange={(e) => {
                  const next = [...volumes];
                  next[index] = { ...vol, name: e.target.value };
                  setVolumes(next);
                }}
                placeholder="data"
              />
            </div>
            <div>
              <Label htmlFor={`vol-mount-${index}`}>Mount path</Label>
              <Input
                id={`vol-mount-${index}`}
                value={vol.mount_path}
                onChange={(e) => {
                  const next = [...volumes];
                  next[index] = { ...vol, mount_path: e.target.value };
                  setVolumes(next);
                }}
                placeholder="/data"
              />
            </div>
            <div>
              <Label htmlFor={`vol-size-${index}`}>Size</Label>
              <Input
                id={`vol-size-${index}`}
                value={vol.size}
                onChange={(e) => {
                  const next = [...volumes];
                  next[index] = { ...vol, size: e.target.value };
                  setVolumes(next);
                }}
                placeholder="10Gi"
              />
            </div>
            <div>
              <Label htmlFor={`vol-sc-${index}`}>Storage class</Label>
              <Input
                id={`vol-sc-${index}`}
                value={vol.storage_class_name ?? ''}
                onChange={(e) => {
                  const next = [...volumes];
                  next[index] = { ...vol, storage_class_name: e.target.value };
                  setVolumes(next);
                }}
                placeholder="longhorn"
              />
            </div>
            <div className="md:col-span-2 flex justify-end">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setVolumes(volumes.filter((_, i) => i !== index))}
              >
                Remove
              </Button>
            </div>
          </div>
        ))}

        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" onClick={() => setVolumes([...volumes, emptyVolume()])}>
            Add volume
          </Button>
          <Button type="button" onClick={save} disabled={saving}>
            {saving ? 'Saving...' : 'Save volumes'}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
