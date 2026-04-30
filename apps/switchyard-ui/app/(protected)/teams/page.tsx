'use client';

import { useState, useEffect } from 'react';
import Link from 'next/link';
import { apiGet, apiPost } from '@/lib/api';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@enclii/ui-components/dialog";

interface Team {
  id: string;
  name: string;
  slug: string;
  description: string | null;
  avatar_url: string | null;
  billing_email: string | null;
  member_count: number;
  user_role: string;
  created_at: string;
  updated_at: string;
}

interface Invitation {
  id: string;
  email: string;
  role: string;
  status: string;
  team_name: string;
  team_slug: string;
  inviter_name: string | null;
  expires_at: string;
  created_at: string;
}

export default function TeamsPage() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [newTeam, setNewTeam] = useState({
    name: '',
    slug: '',
    description: '',
    billing_email: ''
  });

  const fetchTeams = async () => {
    try {
      const data = await apiGet<{ teams: Team[] }>('/v1/teams');
      setTeams(data.teams || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load teams');
    }
  };

  const fetchInvitations = async () => {
    try {
      const data = await apiGet<{ invitations: Invitation[] }>('/v1/invitations');
      setInvitations(data.invitations || []);
    } catch (err) {
      console.error('Failed to fetch invitations:', err);
    }
  };

  const createTeam = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await apiPost('/v1/teams', {
        name: newTeam.name,
        slug: newTeam.slug,
        description: newTeam.description || undefined,
        billing_email: newTeam.billing_email || undefined
      });
      setNewTeam({ name: '', slug: '', description: '', billing_email: '' });
      setShowCreateForm(false);
      fetchTeams();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create team');
    }
  };

  const acceptInvitation = async (token: string) => {
    try {
      await apiPost(`/v1/invitations/${token}/accept`, {});
      fetchTeams();
      fetchInvitations();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to accept invitation');
    }
  };

  const declineInvitation = async (token: string) => {
    try {
      await apiPost(`/v1/invitations/${token}/decline`, {});
      fetchInvitations();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to decline invitation');
    }
  };

  useEffect(() => {
    Promise.all([fetchTeams(), fetchInvitations()]).finally(() => setLoading(false));
  }, []);

  const getRoleBadgeColor = (role: string) => {
    switch (role) {
      case 'owner': return 'bg-purple-100 dark:bg-purple-900/30 text-purple-800 dark:text-purple-300';
      case 'admin': return 'bg-primary/10 text-primary';
      case 'member': return 'bg-status-success-muted text-status-success-foreground';
      case 'viewer': return 'bg-muted text-foreground';
      default: return 'bg-muted text-foreground';
    }
  };

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="animate-pulse">
          <div className="h-8 bg-muted rounded w-1/4 mb-6"></div>
          <div className="space-y-4">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-24 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
      <div className="px-4 py-6 sm:px-0">
        {/* Header */}
        <div className="flex items-center justify-between mb-8">
          <h1 className="text-3xl font-bold text-foreground">Teams</h1>
          <button
            onClick={() => setShowCreateForm(true)}
            className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-enclii-blue hover:bg-enclii-blue-dark focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-enclii-blue"
          >
            <svg aria-hidden="true" className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
            </svg>
            Create Team
          </button>
        </div>

        {/* Error Alert */}
        {error && (
          <div className="mb-6 bg-status-error-muted border border-status-error/30 rounded-md p-4">
            <div className="flex">
              <div className="text-status-error-foreground">
                <h3 className="text-sm font-medium">Error</h3>
                <div className="mt-2 text-sm">{error}</div>
              </div>
              <button onClick={() => setError(null)} className="ml-auto text-status-error hover:text-status-error-foreground" aria-label="Dismiss error">
                <svg aria-hidden="true" className="w-5 h-5" fill="currentColor" viewBox="0 0 20 20">
                  <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
                </svg>
              </button>
            </div>
          </div>
        )}

        {/* Pending Invitations */}
        {invitations.length > 0 && (
          <div className="mb-8">
            <h2 className="text-lg font-medium text-foreground mb-4">Pending Invitations</h2>
            <div className="space-y-4">
              {invitations.map((invitation) => (
                <div key={invitation.id} className="bg-status-warning-muted border border-status-warning/30 rounded-lg p-4">
                  <div className="flex items-center justify-between">
                    <div>
                      <h3 className="font-medium text-foreground">{invitation.team_name}</h3>
                      <p className="text-sm text-muted-foreground">
                        Invited by {invitation.inviter_name || 'a team member'} as {invitation.role}
                      </p>
                      <p className="text-xs text-muted-foreground mt-1">
                        Expires {new Date(invitation.expires_at).toLocaleDateString()}
                      </p>
                    </div>
                    <div className="flex space-x-2">
                      <button
                        onClick={() => acceptInvitation(invitation.id)}
                        className="px-4 py-2 text-sm font-medium text-white bg-status-success rounded-md hover:bg-status-success-foreground"
                      >
                        Accept
                      </button>
                      <button
                        onClick={() => declineInvitation(invitation.id)}
                        className="px-4 py-2 text-sm font-medium text-foreground bg-card border border-input rounded-md hover:bg-accent"
                      >
                        Decline
                      </button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Create Team Modal */}
        <Dialog open={showCreateForm} onOpenChange={setShowCreateForm}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Create New Team</DialogTitle>
              <DialogDescription>
                Create a team to collaborate with others.
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={createTeam}>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">
                  Team Name
                </label>
                <input
                  type="text"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={newTeam.name}
                  onChange={(e) => setNewTeam({ ...newTeam, name: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">
                  Slug
                </label>
                <input
                  type="text"
                  required
                  pattern="^[a-z0-9][a-z0-9-]*[a-z0-9]$"
                  title="Lowercase alphanumeric with hyphens, not starting or ending with hyphen"
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={newTeam.slug}
                  onChange={(e) => setNewTeam({ ...newTeam, slug: e.target.value.toLowerCase() })}
                />
                <p className="mt-1 text-xs text-muted-foreground">URL-friendly identifier (e.g., my-team)</p>
              </div>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">
                  Description (optional)
                </label>
                <textarea
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  rows={3}
                  value={newTeam.description}
                  onChange={(e) => setNewTeam({ ...newTeam, description: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">
                  Billing Email (optional)
                </label>
                <input
                  type="email"
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={newTeam.billing_email}
                  onChange={(e) => setNewTeam({ ...newTeam, billing_email: e.target.value })}
                />
              </div>
              <DialogFooter>
                <button
                  type="button"
                  onClick={() => setShowCreateForm(false)}
                  className="px-4 py-2 text-sm font-medium text-foreground bg-muted rounded-md hover:bg-accent"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 text-sm font-medium text-white bg-enclii-blue rounded-md hover:bg-enclii-blue-dark"
                >
                  Create
                </button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        {/* Teams List */}
        <div className="space-y-6">
          {teams.length === 0 ? (
            <div className="text-center py-12">
              <div className="text-muted-foreground mb-4">
                <svg aria-hidden="true" className="mx-auto h-12 w-12" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z" />
                </svg>
              </div>
              <h3 className="text-lg font-medium text-foreground">No teams yet</h3>
              <p className="text-muted-foreground mt-1">Create a team to collaborate with others.</p>
            </div>
          ) : (
            teams.map((team) => (
              <Link
                key={team.id}
                href={`/teams/${team.slug}`}
                className="block bg-card shadow overflow-hidden sm:rounded-lg hover:shadow-md transition-shadow"
              >
                <div className="px-4 py-5 sm:p-6">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center">
                      {team.avatar_url ? (
                        <img
                          src={team.avatar_url}
                          alt={team.name}
                          className="h-12 w-12 rounded-lg object-cover"
                        />
                      ) : (
                        <div className="h-12 w-12 rounded-lg bg-enclii-blue flex items-center justify-center">
                          <span className="text-white font-semibold text-lg">
                            {team.name.charAt(0).toUpperCase()}
                          </span>
                        </div>
                      )}
                      <div className="ml-4">
                        <h3 className="text-lg font-medium text-foreground">{team.name}</h3>
                        {team.description && (
                          <p className="text-sm text-muted-foreground">{team.description}</p>
                        )}
                        <div className="flex items-center mt-1 space-x-4 text-xs text-muted-foreground">
                          <span>/{team.slug}</span>
                          <span>{team.member_count} member{team.member_count !== 1 ? 's' : ''}</span>
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center space-x-3">
                      <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${getRoleBadgeColor(team.user_role)}`}>
                        {team.user_role}
                      </span>
                      <svg aria-hidden="true" className="w-5 h-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                      </svg>
                    </div>
                  </div>
                </div>
              </Link>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
