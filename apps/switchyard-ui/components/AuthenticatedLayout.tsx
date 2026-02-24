'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useAuth } from '@/contexts/AuthContext';
import { AuthErrorBanner } from '@/components/AuthErrorBanner';
import { NotificationBell } from '@/components/notifications/notification-bell';
import { CommandPalette } from '@/components/command/command-palette';
import { SystemHealthBadge } from '@/components/dashboard/system-health';
import { Spinner } from '@/components/ui/spinner';
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from '@/components/ui/sheet';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Menu, ChevronDown, Sun, Moon, Monitor } from 'lucide-react';
import { useScrollShadow } from '@/hooks/use-scroll-shadow';
import { ScopeSwitcher } from '@/components/navigation/scope-switcher';
import { UserMenu } from '@/components/navigation/user-menu';
import { useScope } from '@/contexts/ScopeContext';
import { useTheme } from 'next-themes';

interface AuthenticatedLayoutProps {
  children: React.ReactNode;
}

interface NavItem {
  name: string;
  href: string;
  tourId?: string;
}

// Helper component for nav links
function NavLink({ item, pathname }: { item: NavItem; pathname: string }) {
  const isActive = pathname === item.href || (item.href !== '/' && pathname.startsWith(item.href));
  return (
    <Link
      href={item.href}
      data-tour={item.tourId}
      className={`px-2 lg:px-3 py-2 text-sm font-medium transition-colors duration-150 whitespace-nowrap ${
        isActive
          ? 'text-enclii-blue border-b-2 border-enclii-blue'
          : 'text-muted-foreground hover:text-enclii-blue hover:border-b-2 hover:border-border'
      }`}
    >
      {item.name}
    </Link>
  );
}

export function AuthenticatedLayout({ children }: AuthenticatedLayoutProps) {
  const pathname = usePathname();
  const router = useRouter();
  const { user, isAuthenticated, isLoading, logout } = useAuth();
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const { isScrolled, shadowClass } = useScrollShadow();
  const { currentScope, scopes, switchScope } = useScope();
  const { theme, setTheme } = useTheme();

  // All navigation items for the bottom bar
  const allNav: NavItem[] = [
    { name: 'Dashboard', href: '/', tourId: 'dashboard' },
    { name: 'Projects', href: '/projects', tourId: 'projects' },
    { name: 'Services', href: '/services', tourId: 'services' },
    { name: 'Deployments', href: '/deployments', tourId: 'deployments' },
    { name: 'Observability', href: '/observability', tourId: 'observability' },
    { name: 'Templates', href: '/templates' },
    { name: 'Databases', href: '/databases' },
    { name: 'Functions', href: '/functions' },
    { name: 'Domains', href: '/domains', tourId: 'domains' },
    { name: 'Activity', href: '/activity' },
  ];

  // At lg: first 7 inline, last 3 in "More" dropdown
  const inlineNav = allNav.slice(0, 7);
  const moreNav = allNav.slice(7);

  const secondaryNav: NavItem[] = [
    { name: 'Usage', href: '/usage' },
    { name: 'Settings', href: '/settings' },
  ];

  // Check if any "More" item is active
  const isMoreActive = moreNav.some(
    (item) => pathname === item.href || pathname.startsWith(item.href)
  );

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!isLoading && !isAuthenticated) {
      router.push('/login');
    }
  }, [isLoading, isAuthenticated, router]);

  const handleLogout = async () => {
    await logout();
    router.push('/login');
  };

  // Show loading state
  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-background">
        <div className="text-center">
          <Spinner size="lg" className="mx-auto" />
          <p className="mt-4 text-muted-foreground">Loading...</p>
        </div>
      </div>
    );
  }

  // Don't render protected content if not authenticated
  if (!isAuthenticated) {
    return null;
  }

  return (
    <div className="min-h-screen flex flex-col">
      {/* Auth Error Banner - shows session expiry, auth failures, etc. */}
      <AuthErrorBanner />

      {/* Two-Level Navbar */}
      <header className={`bg-background border-b border-border sticky top-0 z-50 transition-shadow duration-200 overflow-x-hidden ${isScrolled ? shadowClass : ''}`}>
        {/* Top bar: Logo + scope + actions */}
        <div className="w-full max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between h-14 min-w-0">
            <div className="flex items-center min-w-0 flex-1">
              <div className="flex-shrink-0 flex items-center gap-2">
                <Link href="/" className="flex items-center">
                  <span className="text-2xl font-bold text-enclii-blue">🚂 Enclii</span>
                  <span className="ml-2 text-sm text-muted-foreground font-medium hidden sm:inline">Switchyard</span>
                </Link>
                {/* Scope Switcher - Vercel-style team/personal context */}
                {currentScope && (
                  <>
                    <span className="text-muted-foreground/40 hidden md:inline">/</span>
                    <div className="hidden md:block">
                      <ScopeSwitcher
                        scopes={scopes}
                        currentScope={currentScope}
                        onScopeChange={switchScope}
                        onCreateTeam={() => router.push('/settings/teams/new')}
                      />
                    </div>
                  </>
                )}
              </div>
            </div>
            <div className="flex items-center gap-2 min-w-0 flex-shrink-0">
              {/* Command Palette */}
              <CommandPalette />

              {/* Notifications */}
              <NotificationBell />

              {/* User Menu - visible at lg+ (desktop) */}
              <div className="hidden lg:block">
                <UserMenu user={user} onLogout={handleLogout} />
              </div>

              {/* Mobile/Tablet Hamburger Menu - visible below lg */}
              <Sheet open={mobileMenuOpen} onOpenChange={setMobileMenuOpen}>
                <SheetTrigger asChild>
                  <button className="lg:hidden p-2 rounded-md hover:bg-accent">
                    <Menu className="h-6 w-6 text-foreground" />
                    <span className="sr-only">Open menu</span>
                  </button>
                </SheetTrigger>
                <SheetContent side="right" className="w-[300px] sm:w-[350px]">
                  <SheetHeader>
                    <SheetTitle>Menu</SheetTitle>
                  </SheetHeader>
                  <nav className="flex flex-col gap-4 mt-6">
                    {/* Scope Switcher - Mobile */}
                    {currentScope && (
                      <div className="px-3 py-2 border-b border-border pb-4">
                        <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                          Account
                        </p>
                        <ScopeSwitcher
                          scopes={scopes}
                          currentScope={currentScope}
                          onScopeChange={(scope) => {
                            switchScope(scope);
                            setMobileMenuOpen(false);
                          }}
                          onCreateTeam={() => {
                            setMobileMenuOpen(false);
                            router.push('/settings/teams/new');
                          }}
                        />
                      </div>
                    )}

                    {/* Navigation Links */}
                    <div className="space-y-1">
                      <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                        Navigation
                      </p>
                      {allNav.map((item) => {
                        const isActive = pathname === item.href || (item.href !== '/' && pathname.startsWith(item.href));
                        return (
                          <Link
                            key={item.name}
                            href={item.href}
                            onClick={() => setMobileMenuOpen(false)}
                            className={`block px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                              isActive
                                ? 'bg-accent text-enclii-blue'
                                : 'text-foreground hover:bg-accent'
                            }`}
                          >
                            {item.name}
                          </Link>
                        );
                      })}
                    </div>

                    {/* Secondary Navigation */}
                    <div className="space-y-1 border-t border-border pt-4">
                      <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                        Settings
                      </p>
                      {secondaryNav.map((item) => {
                        const isActive = pathname === item.href || pathname.startsWith(item.href);
                        return (
                          <Link
                            key={item.name}
                            href={item.href}
                            onClick={() => setMobileMenuOpen(false)}
                            className={`block px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                              isActive
                                ? 'bg-accent text-enclii-blue'
                                : 'text-foreground hover:bg-accent'
                            }`}
                          >
                            {item.name}
                          </Link>
                        );
                      })}
                    </div>

                    {/* Theme Toggle - Mobile */}
                    <div className="border-t border-border pt-4">
                      <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2 px-3">
                        Theme
                      </p>
                      <div className="flex gap-2 px-3">
                        <button
                          onClick={() => setTheme('light')}
                          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                            theme === 'light'
                              ? 'bg-accent text-enclii-blue'
                              : 'text-foreground hover:bg-accent'
                          }`}
                        >
                          <Sun className="h-4 w-4" />
                          Light
                        </button>
                        <button
                          onClick={() => setTheme('dark')}
                          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                            theme === 'dark'
                              ? 'bg-accent text-enclii-blue'
                              : 'text-foreground hover:bg-accent'
                          }`}
                        >
                          <Moon className="h-4 w-4" />
                          Dark
                        </button>
                        <button
                          onClick={() => setTheme('system')}
                          className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                            theme === 'system'
                              ? 'bg-accent text-enclii-blue'
                              : 'text-foreground hover:bg-accent'
                          }`}
                        >
                          <Monitor className="h-4 w-4" />
                          Auto
                        </button>
                      </div>
                    </div>

                    {/* System Status - visible in mobile menu */}
                    <div className="border-t border-border pt-4">
                      <div className="px-3 py-2 flex items-center gap-2">
                        <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                          System Status
                        </span>
                        <SystemHealthBadge />
                      </div>
                    </div>

                    {/* User Section */}
                    <div className="border-t border-border pt-4 mt-auto">
                      <div className="px-3 py-2">
                        <p className="text-sm font-medium text-foreground truncate">
                          {user?.name || 'User'}
                        </p>
                        <p className="text-xs text-muted-foreground truncate">
                          {user?.email}
                        </p>
                      </div>
                      <button
                        onClick={() => {
                          setMobileMenuOpen(false);
                          handleLogout();
                        }}
                        className="w-full mt-2 px-3 py-2 text-sm font-medium text-destructive hover:bg-destructive/10 rounded-md transition-colors text-left"
                      >
                        Sign out
                      </button>
                    </div>
                  </nav>
                </SheetContent>
              </Sheet>
            </div>
          </div>
        </div>

        {/* Bottom bar: Navigation links */}
        <div className="hidden lg:block border-t border-border/50">
          <div className="w-full max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
            <div className="flex items-center h-10 space-x-1">
              {/* At xl+: all 10 items inline */}
              <div className="hidden xl:flex items-center space-x-1">
                {allNav.map((item) => (
                  <NavLink key={item.name} item={item} pathname={pathname} />
                ))}
              </div>

              {/* At lg (below xl): first 7 inline + "More" dropdown for last 3 */}
              <div className="flex xl:hidden items-center space-x-1">
                {inlineNav.map((item) => (
                  <NavLink key={item.name} item={item} pathname={pathname} />
                ))}
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button
                      className={`px-3 py-2 text-sm font-medium transition-colors duration-150 flex items-center gap-1 ${
                        isMoreActive
                          ? 'text-enclii-blue'
                          : 'text-muted-foreground hover:text-enclii-blue'
                      }`}
                    >
                      More
                      <ChevronDown className="h-4 w-4" />
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="w-40">
                    {moreNav.map((item) => {
                      const isActive = pathname === item.href || pathname.startsWith(item.href);
                      return (
                        <DropdownMenuItem key={item.name} asChild>
                          <Link
                            href={item.href}
                            className={isActive ? 'text-enclii-blue bg-accent' : ''}
                          >
                            {item.name}
                          </Link>
                        </DropdownMenuItem>
                      );
                    })}
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </div>
          </div>
        </div>
      </header>

      <main className="flex-grow">{children}</main>

      {/* Persistent Footer */}
      <footer className="bg-background border-t border-border">
        <div className="max-w-7xl mx-auto py-4 px-4 sm:px-6 lg:px-8">
          <div className="flex flex-col sm:flex-row items-center justify-between gap-3">
            {/* Left: Links */}
            <div className="flex items-center gap-4 text-sm text-muted-foreground">
              <a href="https://docs.enclii.dev" className="hover:text-foreground transition-colors">
                Docs
              </a>
              <a href="https://docs.enclii.dev/sdks" className="hover:text-foreground transition-colors">
                SDKs
              </a>
              <a href="https://enclii.dev/contact" className="hover:text-foreground transition-colors">
                Contact
              </a>
              <a href="https://enclii.dev/legal" className="hover:text-foreground transition-colors">
                Legal
              </a>
            </div>

            {/* Right: System health + theme toggle */}
            <div className="flex items-center gap-4">
              <SystemHealthBadge />

              {/* Inline theme toggle */}
              <div className="hidden sm:flex items-center rounded-md border border-border bg-muted/50 p-0.5">
                <button
                  onClick={() => setTheme('light')}
                  className={`p-1.5 rounded transition-colors ${
                    theme === 'light'
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                  aria-label="Light theme"
                >
                  <Sun className="h-3.5 w-3.5" />
                </button>
                <button
                  onClick={() => setTheme('dark')}
                  className={`p-1.5 rounded transition-colors ${
                    theme === 'dark'
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                  aria-label="Dark theme"
                >
                  <Moon className="h-3.5 w-3.5" />
                </button>
                <button
                  onClick={() => setTheme('system')}
                  className={`p-1.5 rounded transition-colors ${
                    theme === 'system'
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                  aria-label="Auto theme"
                >
                  <Monitor className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          </div>
        </div>
      </footer>
    </div>
  );
}
