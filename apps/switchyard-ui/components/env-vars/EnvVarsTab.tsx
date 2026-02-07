'use client';

import { useState, useEffect } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { apiGet, apiPost, apiPut, apiDelete } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';
import { EnvironmentVariable, EnvironmentVariableListResponse, RevealedValue } from './types';

interface EnvVarsTabProps {
  serviceId: string;
  serviceName: string;
}

export function EnvVarsTab({ serviceId, serviceName }: EnvVarsTabProps) {
  const [envVars, setEnvVars] = useState<EnvironmentVariable[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [revealedValues, setRevealedValues] = useState<Record<string, string>>({});
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editForm, setEditForm] = useState({ key: '', value: '', is_secret: false });
  const [isAddingNew, setIsAddingNew] = useState(false);
  const [newEnvVar, setNewEnvVar] = useState({ key: '', value: '', is_secret: false });
  const [saving, setSaving] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<EnvironmentVariable | null>(null);

  const fetchEnvVars = async () => {
    try {
      setError(null);
      const data = await apiGet<EnvironmentVariableListResponse>(`/v1/services/${serviceId}/env-vars`);
      setEnvVars(data.environment_variables || []);
    } catch (err) {
      console.error('Failed to fetch env vars:', err);
      setError(err instanceof Error ? err.message : 'Failed to load environment variables');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchEnvVars();
  }, [serviceId]);

  const handleReveal = async (envVar: EnvironmentVariable) => {
    if (revealedValues[envVar.id]) {
      // Toggle hide
      const newRevealed = { ...revealedValues };
      delete newRevealed[envVar.id];
      setRevealedValues(newRevealed);
      return;
    }

    try {
      const data = await apiPost<RevealedValue>(`/v1/services/${serviceId}/env-vars/${envVar.id}/reveal`, {});
      setRevealedValues(prev => ({ ...prev, [envVar.id]: data.value }));
    } catch (err) {
      console.error('Failed to reveal value:', err);
      toast.error('Failed to reveal value: ' + (err instanceof Error ? err.message : 'Unknown error'));
    }
  };

  const handleAddNew = async () => {
    if (!newEnvVar.key.trim() || !newEnvVar.value.trim()) {
      toast.error('Key and value are required');
      return;
    }

    setSaving(true);
    try {
      await apiPost(`/v1/services/${serviceId}/env-vars`, {
        key: newEnvVar.key.trim(),
        value: newEnvVar.value,
        is_secret: newEnvVar.is_secret,
      });
      setNewEnvVar({ key: '', value: '', is_secret: false });
      setIsAddingNew(false);
      await fetchEnvVars();
    } catch (err) {
      console.error('Failed to add env var:', err);
      toast.error('Failed to add environment variable: ' + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setSaving(false);
    }
  };

  const handleEdit = (envVar: EnvironmentVariable) => {
    setEditingId(envVar.id);
    setEditForm({
      key: envVar.key,
      value: revealedValues[envVar.id] || '',
      is_secret: envVar.is_secret,
    });
  };

  const handleSaveEdit = async () => {
    if (!editingId) return;

    setSaving(true);
    try {
      await apiPut(`/v1/services/${serviceId}/env-vars/${editingId}`, {
        key: editForm.key.trim(),
        value: editForm.value,
        is_secret: editForm.is_secret,
      });
      setEditingId(null);
      setRevealedValues(prev => {
        const newRevealed = { ...prev };
        delete newRevealed[editingId];
        return newRevealed;
      });
      await fetchEnvVars();
    } catch (err) {
      console.error('Failed to update env var:', err);
      toast.error('Failed to update environment variable: ' + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = (envVar: EnvironmentVariable) => {
    setDeleteTarget(envVar);
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;

    try {
      await apiDelete(`/v1/services/${serviceId}/env-vars/${deleteTarget.id}`);
      setDeleteTarget(null);
      await fetchEnvVars();
    } catch (err) {
      console.error('Failed to delete env var:', err);
      toast.error('Failed to delete environment variable: ' + (err instanceof Error ? err.message : 'Unknown error'));
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Spinner size="lg" />
        <span className="ml-3 text-muted-foreground">Loading environment variables...</span>
      </div>
    );
  }

  if (error) {
    return (
      <Card className="border-destructive/30 bg-destructive/10">
        <CardContent className="py-8">
          <div className="text-center">
            <p className="text-destructive font-medium mb-4">{error}</p>
            <Button variant="outline" onClick={fetchEnvVars}>
              Try Again
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle>Environment Variables</CardTitle>
            <CardDescription>
              Manage environment variables for {serviceName}. Secrets are encrypted at rest.
            </CardDescription>
          </div>
          <Button onClick={() => setIsAddingNew(true)} disabled={isAddingNew}>
            <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            Add Variable
          </Button>
        </CardHeader>
        <CardContent>
          {/* Add New Form */}
          {isAddingNew && (
            <div className="mb-6 p-4 border rounded-lg bg-muted/50">
              <h4 className="font-medium mb-3">Add New Variable</h4>
              <div className="space-y-3">
                <div className="grid grid-cols-2 gap-3">
                  <Input
                    placeholder="KEY_NAME"
                    value={newEnvVar.key}
                    onChange={(e) => setNewEnvVar(prev => ({ ...prev, key: e.target.value.toUpperCase() }))}
                    className="font-mono"
                  />
                  <Input
                    placeholder="Value"
                    value={newEnvVar.value}
                    onChange={(e) => setNewEnvVar(prev => ({ ...prev, value: e.target.value }))}
                    type={newEnvVar.is_secret ? "password" : "text"}
                    className="font-mono"
                  />
                </div>
                <div className="flex items-center justify-between">
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={newEnvVar.is_secret}
                      onChange={(e) => setNewEnvVar(prev => ({ ...prev, is_secret: e.target.checked }))}
                      className="rounded"
                    />
                    <span className="text-sm">Mark as secret (hidden by default)</span>
                  </label>
                  <div className="space-x-2">
                    <Button variant="outline" onClick={() => setIsAddingNew(false)} disabled={saving}>
                      Cancel
                    </Button>
                    <Button onClick={handleAddNew} disabled={saving}>
                      {saving ? 'Saving...' : 'Save'}
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Variable List */}
          {envVars.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              <svg aria-hidden="true" className="w-12 h-12 mx-auto mb-4 text-muted-foreground/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M19.5 14.25v-2.625a3.375 3.375 0 00-3.375-3.375h-1.5A1.125 1.125 0 0113.5 7.125v-1.5a3.375 3.375 0 00-3.375-3.375H8.25m0 12.75h7.5m-7.5 3H12M10.5 2.25H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 00-9-9z" />
              </svg>
              <p>No environment variables configured</p>
              <p className="text-sm mt-1">Add variables to configure your service</p>
            </div>
          ) : (
            <div className="space-y-2">
              {envVars.map((envVar) => (
                <div
                  key={envVar.id}
                  className="flex items-center justify-between p-3 border rounded-lg hover:bg-accent"
                >
                  {editingId === envVar.id ? (
                    // Edit Mode
                    <div className="flex-1 space-y-2">
                      <div className="grid grid-cols-2 gap-3">
                        <Input
                          value={editForm.key}
                          onChange={(e) => setEditForm(prev => ({ ...prev, key: e.target.value.toUpperCase() }))}
                          className="font-mono"
                        />
                        <Input
                          value={editForm.value}
                          onChange={(e) => setEditForm(prev => ({ ...prev, value: e.target.value }))}
                          type={editForm.is_secret ? "password" : "text"}
                          className="font-mono"
                          placeholder="Enter new value"
                        />
                      </div>
                      <div className="flex items-center justify-between">
                        <label className="flex items-center gap-2">
                          <input
                            type="checkbox"
                            checked={editForm.is_secret}
                            onChange={(e) => setEditForm(prev => ({ ...prev, is_secret: e.target.checked }))}
                            className="rounded"
                          />
                          <span className="text-sm">Secret</span>
                        </label>
                        <div className="space-x-2">
                          <Button variant="outline" size="sm" onClick={() => setEditingId(null)} disabled={saving}>
                            Cancel
                          </Button>
                          <Button size="sm" onClick={handleSaveEdit} disabled={saving}>
                            {saving ? 'Saving...' : 'Save'}
                          </Button>
                        </div>
                      </div>
                    </div>
                  ) : (
                    // View Mode
                    <>
                      <div className="flex items-center gap-3">
                        <code className="font-mono text-sm bg-muted px-2 py-1 rounded">
                          {envVar.key}
                        </code>
                        {envVar.is_secret && (
                          <Badge variant="secondary" className="text-xs">
                            Secret
                          </Badge>
                        )}
                      </div>
                      <div className="flex items-center gap-3">
                        <span className="font-mono text-sm text-muted-foreground max-w-[200px] truncate">
                          {revealedValues[envVar.id] || (envVar.is_secret ? '********' : envVar.value || '(empty)')}
                        </span>
                        <div className="flex items-center gap-1">
                          {envVar.is_secret && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleReveal(envVar)}
                              title={revealedValues[envVar.id] ? 'Hide' : 'Reveal'}
                              aria-label={revealedValues[envVar.id] ? 'Hide value' : 'Reveal value'}
                            >
                              {revealedValues[envVar.id] ? (
                                <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21" />
                                </svg>
                              ) : (
                                <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                                </svg>
                              )}
                            </Button>
                          )}
                          <Button variant="ghost" size="sm" onClick={() => handleEdit(envVar)} title="Edit" aria-label="Edit">
                            <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                            </svg>
                          </Button>
                          <Button variant="ghost" size="sm" onClick={() => handleDelete(envVar)} title="Delete" aria-label="Delete" className="text-destructive hover:text-destructive hover:bg-destructive/10">
                            <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                            </svg>
                          </Button>
                        </div>
                      </div>
                    </>
                  )}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Info Card */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">About Environment Variables</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground space-y-2">
          <p>
            Environment variables are injected into your service at runtime. Changes take effect on the next deployment.
          </p>
          <ul className="list-disc list-inside space-y-1">
            <li>Variables marked as <strong>Secret</strong> are encrypted at rest and hidden in the UI</li>
            <li>Use SCREAMING_SNAKE_CASE for variable names (e.g., DATABASE_URL)</li>
            <li>System variables (ENCLII_*) are automatically added and cannot be overridden</li>
          </ul>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Variable"
        description={`Are you sure you want to delete "${deleteTarget?.key}"? This action cannot be undone.`}
        variant="destructive"
        confirmLabel="Delete"
        onConfirm={confirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  );
}
