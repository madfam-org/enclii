'use client';

import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Switch } from '@/components/ui/switch';
import { apiGet, apiPost, apiDelete } from '@/lib/api';
import { Spinner } from '@/components/ui/spinner';

interface UserProfile {
  id: string;
  email: string;
  name: string;
  avatar_url?: string;
  created_at: string;
  role: string;
}

interface NotificationPrefs {
  email_deployments: boolean;
  email_builds: boolean;
  email_alerts: boolean;
  email_billing: boolean;
  browser_notifications: boolean;
}

interface APIToken {
  id: string;
  name: string;
  prefix: string;
  created_at: string;
  last_used_at?: string;
  expires_at?: string;
  scopes?: string[];
}

interface APITokenCreateResponse {
  token: string;  // Full token - only shown once
  id: string;
  name: string;
  prefix: string;
  created_at: string;
  expires_at?: string;
  scopes?: string[];
}

interface APITokenListResponse {
  tokens: APIToken[];
}

export default function SettingsPage() {
  const [activeTab, setActiveTab] = useState('profile');
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [notifications, setNotifications] = useState<NotificationPrefs>({
    email_deployments: true,
    email_builds: true,
    email_alerts: true,
    email_billing: true,
    browser_notifications: false,
  });
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [showNewToken, setShowNewToken] = useState(false);
  const [newTokenName, setNewTokenName] = useState('');
  const [loading, setLoading] = useState(true);

  // Token-specific state
  const [tokensLoading, setTokensLoading] = useState(false);
  const [tokensError, setTokensError] = useState<string | null>(null);
  const [creatingToken, setCreatingToken] = useState(false);
  const [revokingTokenId, setRevokingTokenId] = useState<string | null>(null);
  const [newlyCreatedToken, setNewlyCreatedToken] = useState<string | null>(null);
  const [tokenCopied, setTokenCopied] = useState(false);

  // Fetch tokens from API
  const fetchTokens = useCallback(async () => {
    setTokensLoading(true);
    setTokensError(null);
    try {
      const response = await apiGet<APITokenListResponse>('/v1/user/tokens');
      setTokens(response.tokens || []);
    } catch (error) {
      console.error('Failed to fetch tokens:', error);
      setTokensError(error instanceof Error ? error.message : 'Failed to load tokens');
    } finally {
      setTokensLoading(false);
    }
  }, []);

  useEffect(() => {
    // Load user profile from localStorage
    setLoading(true);
    const storedAuth = localStorage.getItem('auth');
    if (storedAuth) {
      try {
        const auth = JSON.parse(storedAuth);
        setProfile({
          id: auth.user?.id || 'unknown',
          email: auth.user?.email || 'user@example.com',
          name: auth.user?.name || auth.user?.email?.split('@')[0] || 'User',
          created_at: auth.user?.created_at || new Date().toISOString(),
          role: auth.user?.role || 'developer',
        });
      } catch (e) {
        console.error('Failed to parse auth:', e);
      }
    }
    setLoading(false);
  }, []);

  // Fetch tokens when the tokens tab becomes active
  useEffect(() => {
    if (activeTab === 'tokens') {
      fetchTokens();
    }
  }, [activeTab, fetchTokens]);

  const handleNotificationChange = (key: keyof NotificationPrefs) => {
    setNotifications(prev => ({
      ...prev,
      [key]: !prev[key],
    }));
  };

  const handleCreateToken = async () => {
    if (!newTokenName.trim()) return;

    setCreatingToken(true);
    setTokensError(null);
    setNewlyCreatedToken(null);

    try {
      const response = await apiPost<APITokenCreateResponse>('/v1/user/tokens', {
        name: newTokenName.trim(),
      });

      // Store the full token to display once
      setNewlyCreatedToken(response.token);

      // Add to tokens list (without the full token)
      setTokens(prev => [...prev, {
        id: response.id,
        name: response.name,
        prefix: response.prefix,
        created_at: response.created_at,
        expires_at: response.expires_at,
        scopes: response.scopes,
      }]);

      setNewTokenName('');
      setShowNewToken(false);
    } catch (error) {
      console.error('Failed to create token:', error);
      setTokensError(error instanceof Error ? error.message : 'Failed to create token');
    } finally {
      setCreatingToken(false);
    }
  };

  const handleRevokeToken = async (tokenId: string) => {
    setRevokingTokenId(tokenId);
    setTokensError(null);

    try {
      await apiDelete(`/v1/user/tokens/${tokenId}`);
      setTokens(prev => prev.filter(t => t.id !== tokenId));
    } catch (error) {
      console.error('Failed to revoke token:', error);
      setTokensError(error instanceof Error ? error.message : 'Failed to revoke token');
    } finally {
      setRevokingTokenId(null);
    }
  };

  const handleCopyToken = async () => {
    if (!newlyCreatedToken) return;

    try {
      await navigator.clipboard.writeText(newlyCreatedToken);
      setTokenCopied(true);
      setTimeout(() => setTokenCopied(false), 2000);
    } catch (error) {
      console.error('Failed to copy token:', error);
    }
  };

  const handleDismissNewToken = () => {
    setNewlyCreatedToken(null);
    setTokenCopied(false);
  };

  const tabs = [
    { id: 'profile', label: 'Profile', icon: (
      <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
      </svg>
    )},
    { id: 'notifications', label: 'Notifications', icon: (
      <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
      </svg>
    )},
    { id: 'security', label: 'Security', icon: (
      <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
      </svg>
    )},
    { id: 'tokens', label: 'API Tokens', icon: (
      <svg aria-hidden="true" className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
      </svg>
    )},
  ];

  if (loading) {
    return (
      <div className="flex items-center justify-center py-24">
        <Spinner size="lg" />
        <span className="ml-3 text-muted-foreground">Loading settings...</span>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
        <p className="text-muted-foreground">
          Manage your account settings and preferences
        </p>
      </div>

      <div className="flex flex-col md:flex-row gap-6">
        {/* Sidebar */}
        <div className="w-full md:w-64 space-y-1">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={`w-full flex items-center gap-3 px-4 py-2 text-sm font-medium rounded-lg transition-colors ${
                activeTab === tab.id
                  ? 'bg-primary/10 text-primary'
                  : 'text-muted-foreground hover:bg-accent'
              }`}
            >
              {tab.icon}
              {tab.label}
            </button>
          ))}
        </div>

        {/* Content */}
        <div className="flex-1 space-y-6">
          {activeTab === 'profile' && (
            <Card>
              <CardHeader>
                <CardTitle>Profile Information</CardTitle>
                <CardDescription>Update your account details</CardDescription>
              </CardHeader>
              <CardContent className="space-y-6">
                <div className="flex items-center gap-4">
                  <div className="w-20 h-20 rounded-full bg-gradient-to-br from-blue-400 to-blue-600 flex items-center justify-center text-white text-2xl font-bold">
                    {profile?.name?.charAt(0).toUpperCase() || 'U'}
                  </div>
                  <div>
                    <Button variant="outline" size="sm">Change Avatar</Button>
                  </div>
                </div>

                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <label className="text-sm font-medium">Full Name</label>
                    <Input defaultValue={profile?.name} />
                  </div>
                  <div className="space-y-2">
                    <label className="text-sm font-medium">Email</label>
                    <Input defaultValue={profile?.email} disabled />
                    <p className="text-xs text-muted-foreground">Email cannot be changed</p>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">Role:</span>
                  <Badge variant="secondary">{profile?.role}</Badge>
                </div>

                <div className="flex items-center gap-2">
                  <span className="text-sm text-muted-foreground">
                    Member since {profile?.created_at ? new Date(profile.created_at).toLocaleDateString() : 'Unknown'}
                  </span>
                </div>

                <div className="flex justify-end">
                  <Button>Save Changes</Button>
                </div>
              </CardContent>
            </Card>
          )}

          {activeTab === 'notifications' && (
            <Card>
              <CardHeader>
                <CardTitle>Notification Preferences</CardTitle>
                <CardDescription>Choose what notifications you receive</CardDescription>
              </CardHeader>
              <CardContent className="space-y-6">
                <div className="space-y-4">
                  <h3 className="text-sm font-medium">Email Notifications</h3>

                  <div className="flex items-center justify-between py-2">
                    <div>
                      <p className="font-medium">Deployments</p>
                      <p className="text-sm text-muted-foreground">Get notified when deployments succeed or fail</p>
                    </div>
                    <Switch
                      checked={notifications.email_deployments}
                      onCheckedChange={() => handleNotificationChange('email_deployments')}
                    />
                  </div>

                  <div className="flex items-center justify-between py-2">
                    <div>
                      <p className="font-medium">Builds</p>
                      <p className="text-sm text-muted-foreground">Get notified about build status changes</p>
                    </div>
                    <Switch
                      checked={notifications.email_builds}
                      onCheckedChange={() => handleNotificationChange('email_builds')}
                    />
                  </div>

                  <div className="flex items-center justify-between py-2">
                    <div>
                      <p className="font-medium">Alerts</p>
                      <p className="text-sm text-muted-foreground">Get notified about service health alerts</p>
                    </div>
                    <Switch
                      checked={notifications.email_alerts}
                      onCheckedChange={() => handleNotificationChange('email_alerts')}
                    />
                  </div>

                  <div className="flex items-center justify-between py-2">
                    <div>
                      <p className="font-medium">Billing</p>
                      <p className="text-sm text-muted-foreground">Get notified about invoices and billing changes</p>
                    </div>
                    <Switch
                      checked={notifications.email_billing}
                      onCheckedChange={() => handleNotificationChange('email_billing')}
                    />
                  </div>
                </div>

                <div className="border-t pt-6 space-y-4">
                  <h3 className="text-sm font-medium">Browser Notifications</h3>

                  <div className="flex items-center justify-between py-2">
                    <div>
                      <p className="font-medium">Push Notifications</p>
                      <p className="text-sm text-muted-foreground">Receive notifications in your browser</p>
                    </div>
                    <Switch
                      checked={notifications.browser_notifications}
                      onCheckedChange={() => handleNotificationChange('browser_notifications')}
                    />
                  </div>
                </div>

                <div className="flex justify-end">
                  <Button>Save Preferences</Button>
                </div>
              </CardContent>
            </Card>
          )}

          {activeTab === 'security' && (
            <Card>
              <CardHeader>
                <CardTitle>Security Settings</CardTitle>
                <CardDescription>Manage your account security</CardDescription>
              </CardHeader>
              <CardContent className="space-y-6">
                <div className="space-y-4">
                  <div className="flex items-center justify-between py-3 border-b">
                    <div>
                      <p className="font-medium">Password</p>
                      <p className="text-sm text-muted-foreground">Last changed: Never</p>
                    </div>
                    <Button variant="outline">Change Password</Button>
                  </div>

                  <div className="flex items-center justify-between py-3 border-b">
                    <div>
                      <p className="font-medium">Two-Factor Authentication</p>
                      <p className="text-sm text-muted-foreground">Add an extra layer of security</p>
                    </div>
                    <Button variant="outline">
                      <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
                      </svg>
                      Enable 2FA
                    </Button>
                  </div>

                  <div className="flex items-center justify-between py-3 border-b">
                    <div>
                      <p className="font-medium">Active Sessions</p>
                      <p className="text-sm text-muted-foreground">1 active session</p>
                    </div>
                    <Button variant="outline">View Sessions</Button>
                  </div>

                  <div className="flex items-center justify-between py-3 border-b">
                    <div>
                      <p className="font-medium">Connected Accounts</p>
                      <p className="text-sm text-muted-foreground">GitHub, Google</p>
                    </div>
                    <Button variant="outline">Manage</Button>
                  </div>
                </div>

                <div className="pt-4 border-t">
                  <h3 className="text-sm font-medium text-status-error mb-2">Danger Zone</h3>
                  <div className="flex items-center justify-between py-3 bg-status-error-muted px-4 rounded-lg">
                    <div>
                      <p className="font-medium text-status-error">Delete Account</p>
                      <p className="text-sm text-status-error-foreground">Permanently delete your account and all data</p>
                    </div>
                    <Button variant="destructive">Delete Account</Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          {activeTab === 'tokens' && (
            <Card>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <CardTitle>API Tokens</CardTitle>
                    <CardDescription>Manage API tokens for programmatic access</CardDescription>
                  </div>
                  <Button onClick={() => setShowNewToken(true)} disabled={showNewToken}>
                    <svg aria-hidden="true" className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
                    </svg>
                    New Token
                  </Button>
                </div>
              </CardHeader>
              <CardContent className="space-y-4">
                {/* Error message */}
                {tokensError && (
                  <div className="p-4 border border-status-error/30 rounded-lg bg-status-error-muted text-status-error-foreground">
                    <p className="text-sm">{tokensError}</p>
                  </div>
                )}

                {/* Newly created token display */}
                {newlyCreatedToken && (
                  <div className="p-4 border border-status-success/30 rounded-lg bg-status-success-muted space-y-3">
                    <div className="flex items-center gap-2">
                      <svg aria-hidden="true" className="w-5 h-5 text-status-success" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                      </svg>
                      <p className="font-medium text-status-success-foreground">Token created successfully!</p>
                    </div>
                    <p className="text-sm text-status-success">
                      Copy this token now. You won&apos;t be able to see it again.
                    </p>
                    <div className="flex items-center gap-2">
                      <code className="flex-1 p-2 bg-white border rounded text-sm font-mono break-all">
                        {newlyCreatedToken}
                      </code>
                      <Button size="sm" onClick={handleCopyToken}>
                        {tokenCopied ? (
                          <>
                            <svg aria-hidden="true" className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                            </svg>
                            Copied
                          </>
                        ) : (
                          <>
                            <svg aria-hidden="true" className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                            </svg>
                            Copy
                          </>
                        )}
                      </Button>
                    </div>
                    <Button variant="outline" size="sm" onClick={handleDismissNewToken}>
                      Done
                    </Button>
                  </div>
                )}

                {/* Create new token form */}
                {showNewToken && !newlyCreatedToken && (
                  <div className="p-4 border rounded-lg bg-muted/50 space-y-4">
                    <div className="space-y-2">
                      <label className="text-sm font-medium">Token Name</label>
                      <Input
                        placeholder="e.g., CI/CD Pipeline"
                        value={newTokenName}
                        onChange={(e) => setNewTokenName(e.target.value)}
                        disabled={creatingToken}
                      />
                    </div>
                    <div className="flex gap-2">
                      <Button onClick={handleCreateToken} disabled={creatingToken || !newTokenName.trim()}>
                        {creatingToken ? (
                          <>
                            <Spinner size="sm" className="text-white mr-2" />
                            Creating...
                          </>
                        ) : (
                          'Create Token'
                        )}
                      </Button>
                      <Button variant="outline" onClick={() => {
                        setShowNewToken(false);
                        setNewTokenName('');
                      }} disabled={creatingToken}>
                        Cancel
                      </Button>
                    </div>
                  </div>
                )}

                {/* Loading state */}
                {tokensLoading && (
                  <div className="flex items-center justify-center py-8">
                    <Spinner />
                    <span className="ml-3 text-muted-foreground">Loading tokens...</span>
                  </div>
                )}

                {/* Empty state */}
                {!tokensLoading && tokens.length === 0 && !showNewToken ? (
                  <div className="text-center py-12 text-muted-foreground">
                    <svg aria-hidden="true" className="w-12 h-12 mx-auto mb-4 text-muted-foreground/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
                    </svg>
                    <p className="text-lg font-medium">No API tokens</p>
                    <p className="text-sm mt-1">Create a token to access the API programmatically</p>
                  </div>
                ) : !tokensLoading && tokens.length > 0 && (
                  <div className="space-y-2">
                    {tokens.map((token) => (
                      <div key={token.id} className="flex items-center justify-between p-4 border rounded-lg">
                        <div>
                          <p className="font-medium">{token.name}</p>
                          <p className="text-sm text-muted-foreground font-mono">{token.prefix}...</p>
                          <p className="text-xs text-muted-foreground">
                            Created {new Date(token.created_at).toLocaleDateString()}
                            {token.last_used_at && ` • Last used ${new Date(token.last_used_at).toLocaleDateString()}`}
                            {token.expires_at && ` • Expires ${new Date(token.expires_at).toLocaleDateString()}`}
                          </p>
                        </div>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleRevokeToken(token.id)}
                          disabled={revokingTokenId === token.id}
                        >
                          {revokingTokenId === token.id ? (
                            <>
                              <Spinner size="sm" className="mr-2 text-muted-foreground" />
                              Revoking...
                            </>
                          ) : (
                            'Revoke'
                          )}
                        </Button>
                      </div>
                    ))}
                  </div>
                )}

                <div className="pt-4 border-t">
                  <h3 className="text-sm font-medium mb-2">Token Permissions</h3>
                  <p className="text-sm text-muted-foreground">
                    API tokens have full access to your account. Keep them secure and never share them publicly.
                    Use environment variables to store tokens in your CI/CD pipelines.
                  </p>
                </div>
              </CardContent>
            </Card>
          )}
        </div>
      </div>
    </div>
  );
}
