"use client";

import { Plus, Globe, UserPlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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

      {/* Right: View Toggle + Add New */}
      <div className="flex items-center gap-3">
        <ViewToggle
          value={viewMode}
          onChange={onViewModeChange}
          modes={["grid", "list"]}
          size="sm"
        />

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" data-tour="create-project">
              <Plus className="mr-1.5 h-4 w-4" />
              Add New...
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuItem onClick={onCreateProject}>
              <Plus className="mr-2 h-4 w-4" />
              New Project
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <a href="/domains?create=true">
                <Globe className="mr-2 h-4 w-4" />
                New Domain
              </a>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <a href="/settings/teams/new">
                <UserPlus className="mr-2 h-4 w-4" />
                New Team Member
              </a>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
