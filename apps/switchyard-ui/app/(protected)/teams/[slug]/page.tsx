'use client';

import { useState, useEffect, use } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { apiGet, apiPost, apiPatch, apiDelete } from '@/lib/api';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

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

interface TeamMember {
  id: string;
  user_id: string;
  email: string;
  name: string | null;
  role: string;
  joined_at: string;
}

interface TeamInvitation {
  id: string;
  email: string;
  role: string;
  status: string;
  expires_at: string;
  inviter_id: string;
  created_at: string;
}

export default function TeamDetailPage({ params }: { params: Promise<{ slug: string }> }) {
  const resolvedParams = use(params);
  const router = useRouter();
  const [team, setTeam] = useState<Team | null>(null);
  const [members, setMembers] = useState<TeamMember[]>([]);
  const [invitations, setInvitations] = useState<TeamInvitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<'members' | 'invitations' | 'settings'>('members');

  // Modals
  const [showInviteModal, setShowInviteModal] = useState(false);
  const [showEditModal, setShowEditModal] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [showRoleModal, setShowRoleModal] = useState<TeamMember | null>(null);
  const [removeMemberId, setRemoveMemberId] = useState<string | null>(null);

  // Form states
  const [inviteForm, setInviteForm] = useState({ email: '', role: 'member' });
  const [editForm, setEditForm] = useState({ name: '', description: '', billing_email: '' });
  const [selectedRole, setSelectedRole] = useState('member');

  const isAdmin = team?.user_role === 'owner' || team?.user_role === 'admin';
  const isOwner = team?.user_role === 'owner';

  const fetchTeam = async () => {
    try {
      const data = await apiGet<Team>(`/v1/teams/${resolvedParams.slug}`);
      setTeam(data);
      setEditForm({
        name: data.name,
        description: data.description || '',
        billing_email: data.billing_email || ''
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load team');
    }
  };

  const fetchMembers = async () => {
    try {
      const data = await apiGet<{ members: TeamMember[] }>(`/v1/teams/${resolvedParams.slug}/members`);
      setMembers(data.members || []);
    } catch (err) {
      console.error('Failed to fetch members:', err);
    }
  };

  const fetchInvitations = async () => {
    if (!isAdmin) return;
    try {
      const data = await apiGet<{ invitations: TeamInvitation[] }>(`/v1/teams/${resolvedParams.slug}/invitations`);
      setInvitations(data.invitations || []);
    } catch (err) {
      console.error('Failed to fetch invitations:', err);
    }
  };

  const inviteMember = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await apiPost(`/v1/teams/${resolvedParams.slug}/invitations`, inviteForm);
      setShowInviteModal(false);
      setInviteForm({ email: '', role: 'member' });
      fetchInvitations();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send invitation');
    }
  };

  const cancelInvitation = async (invitationId: string) => {
    try {
      await apiDelete(`/v1/teams/${resolvedParams.slug}/invitations/${invitationId}`);
      fetchInvitations();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to cancel invitation');
    }
  };

  const updateMemberRole = async (memberId: string, newRole: string) => {
    try {
      await apiPatch(`/v1/teams/${resolvedParams.slug}/members/${memberId}`, { role: newRole });
      setShowRoleModal(null);
      fetchMembers();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update member role');
    }
  };

  const confirmRemoveMember = async () => {
    if (!removeMemberId) return;
    try {
      await apiDelete(`/v1/teams/${resolvedParams.slug}/members/${removeMemberId}`);
      fetchMembers();
      fetchTeam();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove member');
    } finally {
      setRemoveMemberId(null);
    }
  };

  const updateTeam = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await apiPatch(`/v1/teams/${resolvedParams.slug}`, {
        name: editForm.name,
        description: editForm.description || null,
        billing_email: editForm.billing_email || null
      });
      setShowEditModal(false);
      fetchTeam();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update team');
    }
  };

  const deleteTeam = async () => {
    try {
      await apiDelete(`/v1/teams/${resolvedParams.slug}`);
      router.push('/teams');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete team');
    }
  };

  useEffect(() => {
    Promise.all([fetchTeam(), fetchMembers()]).finally(() => setLoading(false));
  }, [resolvedParams.slug]);

  useEffect(() => {
    if (isAdmin) {
      fetchInvitations();
    }
  }, [isAdmin]);

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
              <div key={i} className="h-16 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (!team) {
    return (
      <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        <div className="text-center py-12">
          <h3 className="text-lg font-medium text-foreground">Team not found</h3>
          <Link href="/teams" className="text-enclii-blue hover:underline mt-2 inline-block">
            Back to teams
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
      <div className="px-4 py-6 sm:px-0">
        {/* Header */}
        <div className="flex items-center justify-between mb-8">
          <div className="flex items-center">
            <Link href="/teams" className="text-muted-foreground hover:text-foreground mr-4" aria-label="Back to teams">
              <svg aria-hidden="true" className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
              </svg>
            </Link>
            {team.avatar_url ? (
              <img src={team.avatar_url} alt={team.name} className="h-12 w-12 rounded-lg object-cover" />
            ) : (
              <div className="h-12 w-12 rounded-lg bg-enclii-blue flex items-center justify-center">
                <span className="text-white font-semibold text-lg">{team.name.charAt(0).toUpperCase()}</span>
              </div>
            )}
            <div className="ml-4">
              <h1 className="text-2xl font-bold text-foreground">{team.name}</h1>
              {team.description && <p className="text-sm text-muted-foreground">{team.description}</p>}
            </div>
          </div>
          {isAdmin && (
            <button
              onClick={() => setShowInviteModal(true)}
              className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-enclii-blue hover:bg-enclii-blue-dark"
            >
              <svg aria-hidden="true" className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M18 9v3m0 0v3m0-3h3m-3 0h-3m-2-5a4 4 0 11-8 0 4 4 0 018 0zM3 20a6 6 0 0112 0v1H3v-1z" />
              </svg>
              Invite Member
            </button>
          )}
        </div>

        {/* Error Alert */}
        {error && (
          <div className="mb-6 bg-status-error-muted border border-status-error/30 rounded-md p-4">
            <div className="flex">
              <div className="text-status-error-foreground">
                <div className="text-sm">{error}</div>
              </div>
              <button onClick={() => setError(null)} className="ml-auto text-status-error hover:text-status-error-foreground" aria-label="Dismiss error">
                <svg aria-hidden="true" className="w-5 h-5" fill="currentColor" viewBox="0 0 20 20">
                  <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
                </svg>
              </button>
            </div>
          </div>
        )}

        {/* Tabs */}
        <div className="border-b border-border mb-6">
          <nav className="-mb-px flex space-x-8">
            <button
              onClick={() => setActiveTab('members')}
              className={`py-4 px-1 border-b-2 font-medium text-sm ${
                activeTab === 'members'
                  ? 'border-enclii-blue text-enclii-blue'
                  : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
              }`}
            >
              Members ({members.length})
            </button>
            {isAdmin && (
              <button
                onClick={() => setActiveTab('invitations')}
                className={`py-4 px-1 border-b-2 font-medium text-sm ${
                  activeTab === 'invitations'
                    ? 'border-enclii-blue text-enclii-blue'
                    : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
                }`}
              >
                Invitations ({invitations.length})
              </button>
            )}
            {isAdmin && (
              <button
                onClick={() => setActiveTab('settings')}
                className={`py-4 px-1 border-b-2 font-medium text-sm ${
                  activeTab === 'settings'
                    ? 'border-enclii-blue text-enclii-blue'
                    : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
                }`}
              >
                Settings
              </button>
            )}
          </nav>
        </div>

        {/* Members Tab */}
        {activeTab === 'members' && (
          <div className="bg-card shadow overflow-hidden sm:rounded-lg">
            <ul className="divide-y divide-border">
              {members.map((member) => (
                <li key={member.id} className="px-4 py-4 sm:px-6">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center">
                      <div className="h-10 w-10 rounded-full bg-muted flex items-center justify-center">
                        <span className="text-muted-foreground font-medium">
                          {(member.name || member.email).charAt(0).toUpperCase()}
                        </span>
                      </div>
                      <div className="ml-4">
                        <p className="text-sm font-medium text-foreground">{member.name || member.email}</p>
                        {member.name && <p className="text-sm text-muted-foreground">{member.email}</p>}
                        <p className="text-xs text-muted-foreground">
                          Joined {new Date(member.joined_at).toLocaleDateString()}
                        </p>
                      </div>
                    </div>
                    <div className="flex items-center space-x-3">
                      <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${getRoleBadgeColor(member.role)}`}>
                        {member.role}
                      </span>
                      {isAdmin && member.role !== 'owner' && (
                        <div className="flex space-x-2">
                          <button
                            onClick={() => {
                              setSelectedRole(member.role);
                              setShowRoleModal(member);
                            }}
                            className="text-muted-foreground hover:text-foreground"
                            title="Change role"
                            aria-label="Change role"
                          >
                            <svg aria-hidden="true" className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                            </svg>
                          </button>
                          <button
                            onClick={() => setRemoveMemberId(member.id)}
                            className="text-status-error/60 hover:text-status-error"
                            title="Remove member"
                            aria-label="Remove member"
                          >
                            <svg aria-hidden="true" className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                            </svg>
                          </button>
                        </div>
                      )}
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          </div>
        )}

        {/* Invitations Tab */}
        {activeTab === 'invitations' && isAdmin && (
          <div className="bg-card shadow overflow-hidden sm:rounded-lg">
            {invitations.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                No pending invitations
              </div>
            ) : (
              <ul className="divide-y divide-border">
                {invitations.map((invitation) => (
                  <li key={invitation.id} className="px-4 py-4 sm:px-6">
                    <div className="flex items-center justify-between">
                      <div>
                        <p className="text-sm font-medium text-foreground">{invitation.email}</p>
                        <p className="text-sm text-muted-foreground">
                          Invited as {invitation.role} - Expires {new Date(invitation.expires_at).toLocaleDateString()}
                        </p>
                      </div>
                      <button
                        onClick={() => cancelInvitation(invitation.id)}
                        className="text-status-error hover:text-status-error-foreground text-sm font-medium"
                      >
                        Cancel
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}

        {/* Settings Tab */}
        {activeTab === 'settings' && isAdmin && (
          <div className="space-y-6">
            <div className="bg-card shadow sm:rounded-lg">
              <div className="px-4 py-5 sm:p-6">
                <h3 className="text-lg font-medium text-foreground">Team Settings</h3>
                <div className="mt-4 space-y-4">
                  <button
                    onClick={() => setShowEditModal(true)}
                    className="inline-flex items-center px-4 py-2 border border-input text-sm font-medium rounded-md text-foreground bg-card hover:bg-accent"
                  >
                    Edit Team Details
                  </button>
                </div>
              </div>
            </div>

            {isOwner && (
              <div className="bg-status-error-muted shadow sm:rounded-lg">
                <div className="px-4 py-5 sm:p-6">
                  <h3 className="text-lg font-medium text-status-error-foreground">Danger Zone</h3>
                  <p className="mt-2 text-sm text-status-error">
                    Once you delete a team, there is no going back. Please be certain.
                  </p>
                  <div className="mt-4">
                    <button
                      onClick={() => setShowDeleteModal(true)}
                      className="inline-flex items-center px-4 py-2 border border-status-error/30 text-sm font-medium rounded-md text-status-error bg-card hover:bg-status-error-muted"
                    >
                      Delete Team
                    </button>
                  </div>
                </div>
              </div>
            )}
          </div>
        )}

        {/* Invite Modal */}
        <Dialog open={showInviteModal} onOpenChange={setShowInviteModal}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Invite Team Member</DialogTitle>
              <DialogDescription>
                Send an invitation to join this team.
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={inviteMember}>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">Email</label>
                <input
                  type="email"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={inviteForm.email}
                  onChange={(e) => setInviteForm({ ...inviteForm, email: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">Role</label>
                <select
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={inviteForm.role}
                  onChange={(e) => setInviteForm({ ...inviteForm, role: e.target.value })}
                >
                  <option value="viewer">Viewer - Read-only access</option>
                  <option value="member">Member - Can manage services</option>
                  <option value="admin">Admin - Can manage team settings</option>
                </select>
              </div>
              <DialogFooter>
                <button
                  type="button"
                  onClick={() => setShowInviteModal(false)}
                  className="px-4 py-2 text-sm font-medium text-foreground bg-muted rounded-md hover:bg-accent"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 text-sm font-medium text-white bg-enclii-blue rounded-md hover:bg-enclii-blue-dark"
                >
                  Send Invitation
                </button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        {/* Edit Team Modal */}
        <Dialog open={showEditModal} onOpenChange={setShowEditModal}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Edit Team</DialogTitle>
              <DialogDescription>
                Update your team details.
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={updateTeam}>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">Name</label>
                <input
                  type="text"
                  required
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={editForm.name}
                  onChange={(e) => setEditForm({ ...editForm, name: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">Description</label>
                <textarea
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  rows={3}
                  value={editForm.description}
                  onChange={(e) => setEditForm({ ...editForm, description: e.target.value })}
                />
              </div>
              <div className="mb-4">
                <label className="block text-sm font-medium text-foreground mb-2">Billing Email</label>
                <input
                  type="email"
                  className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                  value={editForm.billing_email}
                  onChange={(e) => setEditForm({ ...editForm, billing_email: e.target.value })}
                />
              </div>
              <DialogFooter>
                <button
                  type="button"
                  onClick={() => setShowEditModal(false)}
                  className="px-4 py-2 text-sm font-medium text-foreground bg-muted rounded-md hover:bg-accent"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 text-sm font-medium text-white bg-enclii-blue rounded-md hover:bg-enclii-blue-dark"
                >
                  Save Changes
                </button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        {/* Change Role Modal */}
        <Dialog open={!!showRoleModal} onOpenChange={(open) => { if (!open) setShowRoleModal(null); }}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>
                Change Role for {showRoleModal?.name || showRoleModal?.email}
              </DialogTitle>
              <DialogDescription>
                Select a new role for this team member.
              </DialogDescription>
            </DialogHeader>
            <div className="mb-4">
              <select
                className="w-full px-3 py-2 border border-input rounded-md focus:outline-none focus:ring-2 focus:ring-enclii-blue"
                value={selectedRole}
                onChange={(e) => setSelectedRole(e.target.value)}
              >
                <option value="viewer">Viewer</option>
                <option value="member">Member</option>
                <option value="admin">Admin</option>
                {isOwner && <option value="owner">Owner</option>}
              </select>
            </div>
            <DialogFooter>
              <button
                onClick={() => setShowRoleModal(null)}
                className="px-4 py-2 text-sm font-medium text-foreground bg-muted rounded-md hover:bg-accent"
              >
                Cancel
              </button>
              <button
                onClick={() => showRoleModal && updateMemberRole(showRoleModal.id, selectedRole)}
                className="px-4 py-2 text-sm font-medium text-white bg-enclii-blue rounded-md hover:bg-enclii-blue-dark"
              >
                Update Role
              </button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Delete Confirmation Modal */}
        <Dialog open={showDeleteModal} onOpenChange={setShowDeleteModal}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle className="text-status-error-foreground">Delete Team</DialogTitle>
              <DialogDescription>
                Are you sure you want to delete &quot;{team.name}&quot;? This action cannot be undone.
                All team members will lose access.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <button
                onClick={() => setShowDeleteModal(false)}
                className="px-4 py-2 text-sm font-medium text-foreground bg-muted rounded-md hover:bg-accent"
              >
                Cancel
              </button>
              <button
                onClick={deleteTeam}
                className="px-4 py-2 text-sm font-medium text-white bg-status-error rounded-md hover:bg-status-error-foreground"
              >
                Delete Team
              </button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <ConfirmDialog
          open={removeMemberId !== null}
          title="Remove Member"
          description="Are you sure you want to remove this member from the team?"
          variant="destructive"
          confirmLabel="Remove"
          onConfirm={confirmRemoveMember}
          onCancel={() => setRemoveMemberId(null)}
        />
      </div>
    </div>
  );
}
