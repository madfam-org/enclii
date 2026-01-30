'use client';

import { useState, useEffect, useCallback } from 'react';
import { Bell, Check, X, AlertCircle, CheckCircle2, Clock, Rocket, RefreshCw } from 'lucide-react';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import Link from 'next/link';
import { apiGet } from '@/lib/api';
import { useAuth } from '@/contexts/AuthContext';

export interface Notification {
  id: string;
  type: 'deployment' | 'build' | 'error' | 'info';
  title: string;
  message: string;
  timestamp: Date;
  read: boolean;
  link?: string;
}

// API response types
interface AuditLog {
  id: string;
  timestamp: string;
  actor_email: string;
  action: string;
  resource_type: string;
  resource_id: string;
  resource_name: string;
  outcome: string;
  context?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

interface ActivityResponse {
  activities: AuditLog[];
  count: number;
  limit: number;
  offset: number;
}

// Map audit log action to notification type
function mapActionToType(action: string, outcome: string): Notification['type'] {
  if (outcome === 'failure' || outcome === 'denied') return 'error';
  if (action === 'deploy' || action === 'rollback') return 'deployment';
  if (action === 'build') return 'build';
  return 'info';
}

// Generate notification title from audit log
function generateTitle(action: string, resourceType: string, outcome: string): string {
  const actionMap: Record<string, string> = {
    deploy: 'Deployment',
    rollback: 'Rollback',
    build: 'Build',
    create: 'Created',
    update: 'Updated',
    delete: 'Deleted',
    login: 'Login',
    logout: 'Logout',
    invite: 'Invitation',
    join: 'Joined',
    leave: 'Left',
  };

  const actionLabel = actionMap[action] || action.charAt(0).toUpperCase() + action.slice(1);
  const outcomeLabel = outcome === 'success' ? 'completed' : outcome === 'failure' ? 'failed' : outcome;

  return `${actionLabel} ${outcomeLabel}`;
}

// Generate resource link from audit log
function generateLink(resourceType: string, resourceName: string): string | undefined {
  switch (resourceType) {
    case 'service':
      return `/services/${resourceName}`;
    case 'project':
      return `/projects/${resourceName}`;
    case 'deployment':
    case 'release':
      return '/deployments';
    case 'domain':
      return '/domains';
    default:
      return '/activity';
  }
}

// Convert audit log to notification
function auditLogToNotification(log: AuditLog, readIds: Set<string>): Notification {
  return {
    id: log.id,
    type: mapActionToType(log.action, log.outcome),
    title: generateTitle(log.action, log.resource_type, log.outcome),
    message: `${log.resource_type}: ${log.resource_name}${log.actor_email ? ` by ${log.actor_email}` : ''}`,
    timestamp: new Date(log.timestamp),
    read: readIds.has(log.id) || log.outcome === 'success',
    link: generateLink(log.resource_type, log.resource_name),
  };
}

// Local storage key for read notification IDs
const READ_NOTIFICATIONS_KEY = 'enclii_read_notifications';

function getNotificationIcon(type: Notification['type']) {
  switch (type) {
    case 'deployment':
      return <Rocket className="h-4 w-4 text-status-success" />;
    case 'build':
      return <Clock className="h-4 w-4 text-status-info" />;
    case 'error':
      return <AlertCircle className="h-4 w-4 text-status-error" />;
    case 'info':
      return <CheckCircle2 className="h-4 w-4 text-status-info" />;
  }
}

function formatTimestamp(date: Date): string {
  const now = new Date();
  const diff = now.getTime() - date.getTime();

  const minutes = Math.floor(diff / (1000 * 60));
  const hours = Math.floor(diff / (1000 * 60 * 60));
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));

  if (minutes < 1) return 'Just now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days < 7) return `${days}d ago`;
  return date.toLocaleDateString();
}

export function NotificationBell() {
  const { isAuthenticated, isLoading: authLoading } = useAuth();
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [isOpen, setIsOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [readIds, setReadIds] = useState<Set<string>>(new Set());

  // Load read notification IDs from localStorage
  useEffect(() => {
    if (typeof window !== 'undefined') {
      const stored = localStorage.getItem(READ_NOTIFICATIONS_KEY);
      if (stored) {
        try {
          setReadIds(new Set(JSON.parse(stored)));
        } catch {
          // Invalid JSON, ignore
        }
      }
    }
  }, []);

  // Persist read IDs to localStorage
  const persistReadIds = useCallback((ids: Set<string>) => {
    if (typeof window !== 'undefined') {
      // Keep only the last 100 IDs to prevent unbounded growth
      const idsArray = Array.from(ids).slice(-100);
      localStorage.setItem(READ_NOTIFICATIONS_KEY, JSON.stringify(idsArray));
    }
  }, []);

  // Fetch notifications from API
  const fetchNotifications = useCallback(async () => {
    try {
      setError(null);
      const data = await apiGet<ActivityResponse>('/v1/activity?limit=10');
      const mappedNotifications = (data.activities || []).map((log) =>
        auditLogToNotification(log, readIds)
      );
      setNotifications(mappedNotifications);
    } catch (err) {
      console.error('Failed to fetch notifications:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch notifications');
    } finally {
      setLoading(false);
    }
  }, [readIds]);

  // Initial fetch and polling — only after auth is ready
  useEffect(() => {
    if (authLoading || !isAuthenticated) return;

    fetchNotifications();

    // Poll every 30 seconds for updates
    const interval = setInterval(fetchNotifications, 30000);
    return () => clearInterval(interval);
  }, [fetchNotifications, authLoading, isAuthenticated]);

  const unreadCount = notifications.filter((n) => !n.read).length;

  const markAsRead = (id: string) => {
    const newReadIds = new Set(readIds);
    newReadIds.add(id);
    setReadIds(newReadIds);
    persistReadIds(newReadIds);
    setNotifications((prev) =>
      prev.map((n) => (n.id === id ? { ...n, read: true } : n))
    );
  };

  const markAllAsRead = () => {
    const newReadIds = new Set(readIds);
    notifications.forEach((n) => newReadIds.add(n.id));
    setReadIds(newReadIds);
    persistReadIds(newReadIds);
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
  };

  const dismissNotification = (id: string, e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setNotifications((prev) => prev.filter((n) => n.id !== id));
  };

  return (
    <DropdownMenu open={isOpen} onOpenChange={setIsOpen}>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="h-9 w-9 relative">
          <Bell className="h-4 w-4" />
          {unreadCount > 0 && (
            <span className="absolute -top-1 -right-1 h-5 w-5 flex items-center justify-center rounded-full bg-status-error text-white text-xs font-medium">
              {unreadCount > 9 ? '9+' : unreadCount}
            </span>
          )}
          <span className="sr-only">
            {unreadCount > 0 ? `${unreadCount} unread notifications` : 'Notifications'}
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80">
        <div className="flex items-center justify-between px-4 py-2">
          <DropdownMenuLabel className="p-0">Notifications</DropdownMenuLabel>
          {unreadCount > 0 && (
            <button
              onClick={markAllAsRead}
              className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1"
            >
              <Check className="h-3 w-3" />
              Mark all read
            </button>
          )}
        </div>
        <DropdownMenuSeparator />
        <div className="max-h-96 overflow-y-auto">
          {loading ? (
            <div className="py-8 text-center text-muted-foreground">
              <RefreshCw className="h-6 w-6 mx-auto mb-2 animate-spin opacity-50" />
              <p className="text-sm">Loading...</p>
            </div>
          ) : error ? (
            <div className="py-8 text-center text-muted-foreground">
              <AlertCircle className="h-8 w-8 mx-auto mb-2 text-status-error opacity-70" />
              <p className="text-sm text-status-error">{error}</p>
              <button
                onClick={() => fetchNotifications()}
                className="mt-2 text-xs text-blue-600 hover:underline"
              >
                Retry
              </button>
            </div>
          ) : notifications.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              <Bell className="h-8 w-8 mx-auto mb-2 opacity-50" />
              <p className="text-sm">No notifications</p>
            </div>
          ) : (
            notifications.map((notification) => {
              const content = (
                <div
                  className={`flex items-start gap-3 px-4 py-3 hover:bg-accent cursor-pointer transition-colors ${
                    !notification.read ? 'bg-accent/50' : ''
                  }`}
                  onClick={() => markAsRead(notification.id)}
                >
                  <div className="flex-shrink-0 mt-0.5">
                    {getNotificationIcon(notification.type)}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-start justify-between gap-2">
                      <p className={`text-sm ${!notification.read ? 'font-medium' : ''}`}>
                        {notification.title}
                      </p>
                      <button
                        onClick={(e) => dismissNotification(notification.id, e)}
                        className="flex-shrink-0 text-muted-foreground hover:text-foreground opacity-0 group-hover:opacity-100 transition-opacity"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </div>
                    <p className="text-xs text-muted-foreground truncate">
                      {notification.message}
                    </p>
                    <p className="text-xs text-muted-foreground mt-1">
                      {formatTimestamp(notification.timestamp)}
                    </p>
                  </div>
                  {!notification.read && (
                    <div className="flex-shrink-0">
                      <span className="h-2 w-2 rounded-full bg-status-info block" />
                    </div>
                  )}
                </div>
              );

              if (notification.link) {
                return (
                  <Link
                    key={notification.id}
                    href={notification.link}
                    className="block group"
                    onClick={() => {
                      markAsRead(notification.id);
                      setIsOpen(false);
                    }}
                  >
                    {content}
                  </Link>
                );
              }

              return (
                <div key={notification.id} className="group">
                  {content}
                </div>
              );
            })
          )}
        </div>
        {notifications.length > 0 && (
          <>
            <DropdownMenuSeparator />
            <Link
              href="/activity"
              className="block px-4 py-2 text-sm text-center text-muted-foreground hover:text-foreground hover:bg-accent"
              onClick={() => setIsOpen(false)}
            >
              View all activity
            </Link>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
