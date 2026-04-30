'use client';

import { useTheme } from 'next-themes';
import { useEffect, useState } from 'react';
import { Sun, Moon, Monitor, Leaf, Building2 } from 'lucide-react';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
  DropdownMenuLabel,
} from "@enclii/ui-components/dropdown-menu";
import { Button } from "@enclii/ui-components/button";
import { useThemeSkin } from './theme-provider';

// =============================================================================
// THEME TOGGLE - Combined Light/Dark Mode + Skin Selection
// =============================================================================

export function ThemeToggle() {
  const { theme, setTheme, resolvedTheme } = useTheme();
  const [mounted, setMounted] = useState(false);

  // Avoid hydration mismatch
  useEffect(() => {
    setMounted(true);
  }, []);

  if (!mounted) {
    return (
      <Button variant="ghost" size="icon" className="h-9 w-9">
        <span className="sr-only">Toggle theme</span>
      </Button>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="h-9 w-9">
          {resolvedTheme === 'dark' ? (
            <Moon className="h-4 w-4" />
          ) : (
            <Sun className="h-4 w-4" />
          )}
          <span className="sr-only">Toggle theme</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel className="text-xs text-muted-foreground">
          Appearance
        </DropdownMenuLabel>
        <DropdownMenuItem
          onClick={() => setTheme('light')}
          className={theme === 'light' ? 'bg-accent' : ''}
        >
          <Sun className="mr-2 h-4 w-4" />
          Light
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => setTheme('dark')}
          className={theme === 'dark' ? 'bg-accent' : ''}
        >
          <Moon className="mr-2 h-4 w-4" />
          Dark
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => setTheme('system')}
          className={theme === 'system' ? 'bg-accent' : ''}
        >
          <Monitor className="mr-2 h-4 w-4" />
          System
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// =============================================================================
// COMBINED THEME TOGGLE - Light/Dark + Skin in one menu
// =============================================================================

export function CombinedThemeToggle() {
  const { theme, setTheme, resolvedTheme } = useTheme();
  const { skin, setSkin, isSolarpunk } = useThemeSkin();
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  if (!mounted) {
    return (
      <Button variant="ghost" size="icon" className="h-9 w-9">
        <span className="sr-only">Toggle theme</span>
      </Button>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="h-9 w-9">
          {isSolarpunk ? (
            <Leaf className="h-4 w-4 text-solarpunk-chlorophyll" />
          ) : resolvedTheme === 'dark' ? (
            <Moon className="h-4 w-4" />
          ) : (
            <Sun className="h-4 w-4" />
          )}
          <span className="sr-only">Toggle theme</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        {/* Skin Selection */}
        <DropdownMenuLabel className="text-xs text-muted-foreground">
          Theme Skin
        </DropdownMenuLabel>
        <DropdownMenuItem
          onClick={() => setSkin('enterprise')}
          className={skin === 'enterprise' ? 'bg-accent' : ''}
        >
          <Building2 className="mr-2 h-4 w-4" />
          Enterprise
          <span className="ml-auto text-[10px] text-muted-foreground">Default</span>
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => setSkin('solarpunk')}
          className={skin === 'solarpunk' ? 'bg-accent' : ''}
        >
          <Leaf className="mr-2 h-4 w-4 text-solarpunk-chlorophyll" />
          Solarpunk
          <span className="ml-auto text-[10px] text-muted-foreground">🌿</span>
        </DropdownMenuItem>

        <DropdownMenuSeparator />

        {/* Light/Dark Mode (only for Enterprise skin) */}
        {!isSolarpunk && (
          <>
            <DropdownMenuLabel className="text-xs text-muted-foreground">
              Appearance
            </DropdownMenuLabel>
            <DropdownMenuItem
              onClick={() => setTheme('light')}
              className={theme === 'light' ? 'bg-accent' : ''}
            >
              <Sun className="mr-2 h-4 w-4" />
              Light
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setTheme('dark')}
              className={theme === 'dark' ? 'bg-accent' : ''}
            >
              <Moon className="mr-2 h-4 w-4" />
              Dark
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setTheme('system')}
              className={theme === 'system' ? 'bg-accent' : ''}
            >
              <Monitor className="mr-2 h-4 w-4" />
              System
            </DropdownMenuItem>
          </>
        )}

        {/* Solarpunk is always "dark organic" */}
        {isSolarpunk && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            Solarpunk mode uses organic dark theme
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
