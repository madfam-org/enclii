'use client';

import { useEffect, useState, useCallback } from 'react';
import { useRouter } from 'next/navigation';
import { Command } from 'cmdk';
import {
  Home,
  FolderKanban,
  Server,
  Rocket,
  Globe,
  BarChart3,
  Activity,
  Settings,
  PlusCircle,
  Github,
  Search,
  RefreshCw,
  LogOut,
  Moon,
  Sun,
  Monitor,
} from 'lucide-react';
import { useTheme } from 'next-themes';
import { useAuth } from '@/contexts/AuthContext';

interface CommandItem {
  id: string;
  label: string;
  icon: React.ReactNode;
  shortcut?: string;
  action: () => void;
  keywords?: string[];
  group: string;
}

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const router = useRouter();
  const { setTheme, theme } = useTheme();
  const { logout } = useAuth();

  // Toggle command palette with Cmd+K or Ctrl+K
  useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((open) => !open);
      }
      // Also support Escape to close
      if (e.key === 'Escape') {
        setOpen(false);
      }
    };

    document.addEventListener('keydown', down);
    return () => document.removeEventListener('keydown', down);
  }, []);

  const runCommand = useCallback((command: () => void) => {
    setOpen(false);
    setSearch('');
    command();
  }, []);

  const commands: CommandItem[] = [
    // Navigation
    {
      id: 'dashboard',
      label: 'Go to Dashboard',
      icon: <Home className="h-4 w-4" />,
      action: () => router.push('/'),
      keywords: ['home', 'overview'],
      group: 'Navigation',
    },
    {
      id: 'projects',
      label: 'Go to Projects',
      icon: <FolderKanban className="h-4 w-4" />,
      action: () => router.push('/projects'),
      keywords: ['apps', 'folders'],
      group: 'Navigation',
    },
    {
      id: 'services',
      label: 'Go to Services',
      icon: <Server className="h-4 w-4" />,
      action: () => router.push('/services'),
      keywords: ['containers', 'deployments'],
      group: 'Navigation',
    },
    {
      id: 'deployments',
      label: 'Go to Deployments',
      icon: <Rocket className="h-4 w-4" />,
      action: () => router.push('/deployments'),
      keywords: ['releases', 'builds'],
      group: 'Navigation',
    },
    {
      id: 'domains',
      label: 'Go to Domains',
      icon: <Globe className="h-4 w-4" />,
      action: () => router.push('/domains'),
      keywords: ['dns', 'networking', 'urls'],
      group: 'Navigation',
    },
    {
      id: 'observability',
      label: 'Go to Observability',
      icon: <BarChart3 className="h-4 w-4" />,
      action: () => router.push('/observability'),
      keywords: ['logs', 'metrics', 'traces'],
      group: 'Navigation',
    },
    {
      id: 'activity',
      label: 'Go to Activity',
      icon: <Activity className="h-4 w-4" />,
      action: () => router.push('/activity'),
      keywords: ['events', 'history'],
      group: 'Navigation',
    },
    {
      id: 'usage',
      label: 'Go to Usage',
      icon: <BarChart3 className="h-4 w-4" />,
      action: () => router.push('/usage'),
      keywords: ['billing', 'costs'],
      group: 'Navigation',
    },
    {
      id: 'settings',
      label: 'Go to Settings',
      icon: <Settings className="h-4 w-4" />,
      action: () => router.push('/settings'),
      keywords: ['preferences', 'config'],
      group: 'Navigation',
    },

    // Actions
    {
      id: 'new-service',
      label: 'Create New Service',
      icon: <PlusCircle className="h-4 w-4" />,
      shortcut: '⌘ N',
      action: () => router.push('/services/new'),
      keywords: ['add', 'create'],
      group: 'Actions',
    },
    {
      id: 'import-github',
      label: 'Import from GitHub',
      icon: <Github className="h-4 w-4" />,
      action: () => router.push('/services/import'),
      keywords: ['git', 'repository'],
      group: 'Actions',
    },

    // Theme
    {
      id: 'theme-light',
      label: 'Switch to Light Mode',
      icon: <Sun className="h-4 w-4" />,
      action: () => setTheme('light'),
      keywords: ['appearance', 'bright'],
      group: 'Theme',
    },
    {
      id: 'theme-dark',
      label: 'Switch to Dark Mode',
      icon: <Moon className="h-4 w-4" />,
      action: () => setTheme('dark'),
      keywords: ['appearance', 'night'],
      group: 'Theme',
    },
    {
      id: 'theme-system',
      label: 'Use System Theme',
      icon: <Monitor className="h-4 w-4" />,
      action: () => setTheme('system'),
      keywords: ['appearance', 'auto'],
      group: 'Theme',
    },

    // Account
    {
      id: 'logout',
      label: 'Sign Out',
      icon: <LogOut className="h-4 w-4" />,
      action: () => logout(),
      keywords: ['exit', 'leave'],
      group: 'Account',
    },
  ];

  // Group commands
  const groupedCommands = commands.reduce((acc, command) => {
    if (!acc[command.group]) {
      acc[command.group] = [];
    }
    acc[command.group].push(command);
    return acc;
  }, {} as Record<string, CommandItem[]>);

  return (
    <>
      {/* Keyboard shortcut hint */}
      <button
        onClick={() => setOpen(true)}
        className="hidden md:flex items-center gap-2 px-3 py-1.5 text-sm text-muted-foreground border border-border rounded-md hover:bg-accent transition-colors"
      >
        <Search className="h-3.5 w-3.5" />
        <span className="hidden xl:inline">Search...</span>
        <kbd className="ml-2 px-1.5 py-0.5 text-xs bg-muted rounded border border-border font-mono">
          ⌘K
        </kbd>
      </button>

      {/* Command Dialog */}
      <Command.Dialog
        open={open}
        onOpenChange={setOpen}
        className="fixed inset-0 z-50"
      >
        <div
          className="fixed inset-0 bg-black/50"
          onClick={() => setOpen(false)}
        />
        <div className="fixed top-[20%] left-1/2 -translate-x-1/2 w-full max-w-lg mx-auto p-4 z-50">
          <div className="bg-background border border-border rounded-lg shadow-2xl overflow-hidden">
            <Command.Input
              value={search}
              onValueChange={setSearch}
              placeholder="Type a command or search..."
              className="w-full px-4 py-3 text-base border-b border-border bg-transparent outline-none placeholder:text-muted-foreground"
            />
            <Command.List className="max-h-80 overflow-y-auto p-2">
              <Command.Empty className="py-6 text-center text-sm text-muted-foreground">
                No results found.
              </Command.Empty>

              {Object.entries(groupedCommands).map(([group, items]) => (
                <Command.Group
                  key={group}
                  heading={group}
                  className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-muted-foreground"
                >
                  {items.map((command) => (
                    <Command.Item
                      key={command.id}
                      value={`${command.label} ${command.keywords?.join(' ') || ''}`}
                      onSelect={() => runCommand(command.action)}
                      className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm hover:bg-accent data-[selected=true]:bg-accent"
                    >
                      <span className="text-muted-foreground">{command.icon}</span>
                      <span className="flex-1">{command.label}</span>
                      {command.shortcut && (
                        <kbd className="ml-auto text-xs text-muted-foreground font-mono">
                          {command.shortcut}
                        </kbd>
                      )}
                    </Command.Item>
                  ))}
                </Command.Group>
              ))}
            </Command.List>
          </div>
        </div>
      </Command.Dialog>
    </>
  );
}
