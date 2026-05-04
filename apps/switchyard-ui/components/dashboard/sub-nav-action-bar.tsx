"use client";

import Link from "next/link";
import { Plus, Boxes, Database, Layers } from "lucide-react";
import { Button } from "@enclii/ui-components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@enclii/ui-components/dropdown-menu";
import {
  ProjectSearchFilter,
  type SortOption,
} from "@/components/dashboard/project-search-filter";
import { ViewToggle, type ViewMode } from "@/components/dashboard/view-toggle";

interface SubNavActionBarProps {
  search: string;
  onSearchChange: (value: string) => void;
  sort: SortOption;
  onSortChange: (value: SortOption) => void;
  viewMode: ViewMode;
  onViewModeChange: (mode: ViewMode) => void;
  onCreateProject: () => void;
}

export function SubNavActionBar({
  search,
  onSearchChange,
  sort,
  onSortChange,
  viewMode,
  onViewModeChange,
  onCreateProject,
}: SubNavActionBarProps) {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      {/* Left: Search + Sort */}
      <div className="flex-1">
        <ProjectSearchFilter
          search={search}
          onSearchChange={onSearchChange}
          sort={sort}
          onSortChange={onSortChange}
        />
      </div>

      {/* Right: View Toggle + Add New
          The dropdown disambiguates the "+ Add New…" CTA (audit D-5).
          Items route to existing creation surfaces: project (modal on
          /projects), service (dedicated /services/new page), database
          (modal on /databases via ?create=true; page reads the param). */}
      <div className="flex items-center gap-3">
        <ViewToggle
          value={viewMode}
          onChange={onViewModeChange}
          modes={["grid", "list"]}
          size="sm"
        />

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              size="sm"
              data-tour="create-project"
              aria-label="Create a project, service, or database"
            >
              <Plus className="mr-1.5 h-4 w-4" />
              Add New...
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel>Create new</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onCreateProject}>
              <Boxes className="mr-2 h-4 w-4" />
              <span>New project</span>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link href="/services/new">
                <Layers className="mr-2 h-4 w-4" />
                <span>New service</span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link href="/databases?create=true">
                <Database className="mr-2 h-4 w-4" />
                <span>New database</span>
              </Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
