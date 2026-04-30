'use client';

import { useState, useCallback } from 'react';
import { Input } from "@enclii/ui-components/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuCheckboxItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@enclii/ui-components/dropdown-menu";
import { Button } from "@enclii/ui-components/button";
import { Search, Filter, X, SortAsc, SortDesc } from 'lucide-react';

export type ServiceStatus = 'healthy' | 'unhealthy' | 'unknown';
export type SortField = 'name' | 'status' | 'environment' | 'project';
export type SortOrder = 'asc' | 'desc';

export interface FilterState {
  search: string;
  statuses: ServiceStatus[];
  environments: string[];
}

export interface SortState {
  field: SortField;
  order: SortOrder;
}

interface ProjectSearchProps {
  onFilterChange: (filters: FilterState) => void;
  onSortChange: (sort: SortState) => void;
  filters: FilterState;
  sort: SortState;
  availableEnvironments: string[];
}

export function ProjectSearch({
  onFilterChange,
  onSortChange,
  filters,
  sort,
  availableEnvironments,
}: ProjectSearchProps) {
  const [isOpen, setIsOpen] = useState(false);

  const handleSearchChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      onFilterChange({ ...filters, search: e.target.value });
    },
    [filters, onFilterChange]
  );

  const handleStatusToggle = useCallback(
    (status: ServiceStatus) => {
      const newStatuses = filters.statuses.includes(status)
        ? filters.statuses.filter((s) => s !== status)
        : [...filters.statuses, status];
      onFilterChange({ ...filters, statuses: newStatuses });
    },
    [filters, onFilterChange]
  );

  const handleEnvironmentToggle = useCallback(
    (env: string) => {
      const newEnvs = filters.environments.includes(env)
        ? filters.environments.filter((e) => e !== env)
        : [...filters.environments, env];
      onFilterChange({ ...filters, environments: newEnvs });
    },
    [filters, onFilterChange]
  );

  const handleSortChange = useCallback(
    (field: SortField) => {
      if (sort.field === field) {
        onSortChange({ field, order: sort.order === 'asc' ? 'desc' : 'asc' });
      } else {
        onSortChange({ field, order: 'asc' });
      }
    },
    [sort, onSortChange]
  );

  const clearFilters = useCallback(() => {
    onFilterChange({ search: '', statuses: [], environments: [] });
  }, [onFilterChange]);

  const hasActiveFilters =
    filters.search || filters.statuses.length > 0 || filters.environments.length > 0;

  return (
    <div className="flex items-center gap-3 mb-6">
      {/* Search Input */}
      <div className="relative flex-1 max-w-md">
        <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground h-4 w-4" />
        <Input
          type="text"
          placeholder="Search services..."
          value={filters.search}
          onChange={handleSearchChange}
          className="pl-10 pr-10"
        />
        {filters.search && (
          <button
            onClick={() => onFilterChange({ ...filters, search: '' })}
            className="absolute right-3 top-1/2 transform -translate-y-1/2 text-muted-foreground hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* Filter Dropdown */}
      <DropdownMenu open={isOpen} onOpenChange={setIsOpen}>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" className="gap-2">
            <Filter className="h-4 w-4" />
            Filter
            {hasActiveFilters && (
              <span className="ml-1 bg-primary/10 text-primary text-xs px-1.5 py-0.5 rounded-full">
                {filters.statuses.length + filters.environments.length + (filters.search ? 1 : 0)}
              </span>
            )}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-56">
          <DropdownMenuLabel>Status</DropdownMenuLabel>
          <DropdownMenuCheckboxItem
            checked={filters.statuses.includes('healthy')}
            onCheckedChange={() => handleStatusToggle('healthy')}
          >
            <span className="flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-status-success" />
              Healthy
            </span>
          </DropdownMenuCheckboxItem>
          <DropdownMenuCheckboxItem
            checked={filters.statuses.includes('unhealthy')}
            onCheckedChange={() => handleStatusToggle('unhealthy')}
          >
            <span className="flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-status-error" />
              Unhealthy
            </span>
          </DropdownMenuCheckboxItem>
          <DropdownMenuCheckboxItem
            checked={filters.statuses.includes('unknown')}
            onCheckedChange={() => handleStatusToggle('unknown')}
          >
            <span className="flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-muted-foreground" />
              Unknown
            </span>
          </DropdownMenuCheckboxItem>

          {availableEnvironments.length > 0 && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuLabel>Environment</DropdownMenuLabel>
              {availableEnvironments.map((env) => (
                <DropdownMenuCheckboxItem
                  key={env}
                  checked={filters.environments.includes(env)}
                  onCheckedChange={() => handleEnvironmentToggle(env)}
                >
                  {env.charAt(0).toUpperCase() + env.slice(1)}
                </DropdownMenuCheckboxItem>
              ))}
            </>
          )}

          {hasActiveFilters && (
            <>
              <DropdownMenuSeparator />
              <button
                onClick={clearFilters}
                className="w-full px-2 py-1.5 text-sm text-destructive hover:bg-destructive/10 text-left"
              >
                Clear all filters
              </button>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Sort Dropdown */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" className="gap-2">
            {sort.order === 'asc' ? (
              <SortAsc className="h-4 w-4" />
            ) : (
              <SortDesc className="h-4 w-4" />
            )}
            Sort
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          <DropdownMenuLabel>Sort by</DropdownMenuLabel>
          <DropdownMenuCheckboxItem
            checked={sort.field === 'name'}
            onCheckedChange={() => handleSortChange('name')}
          >
            Name {sort.field === 'name' && (sort.order === 'asc' ? '↑' : '↓')}
          </DropdownMenuCheckboxItem>
          <DropdownMenuCheckboxItem
            checked={sort.field === 'status'}
            onCheckedChange={() => handleSortChange('status')}
          >
            Status {sort.field === 'status' && (sort.order === 'asc' ? '↑' : '↓')}
          </DropdownMenuCheckboxItem>
          <DropdownMenuCheckboxItem
            checked={sort.field === 'environment'}
            onCheckedChange={() => handleSortChange('environment')}
          >
            Environment {sort.field === 'environment' && (sort.order === 'asc' ? '↑' : '↓')}
          </DropdownMenuCheckboxItem>
          <DropdownMenuCheckboxItem
            checked={sort.field === 'project'}
            onCheckedChange={() => handleSortChange('project')}
          >
            Project {sort.field === 'project' && (sort.order === 'asc' ? '↑' : '↓')}
          </DropdownMenuCheckboxItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Active filter badges */}
      {hasActiveFilters && (
        <div className="flex items-center gap-2 ml-2">
          {filters.statuses.map((status) => (
            <span
              key={status}
              className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs bg-muted text-muted-foreground"
            >
              {status}
              <button
                onClick={() => handleStatusToggle(status)}
                className="hover:text-destructive"
              >
                <X className="h-3 w-3" />
              </button>
            </span>
          ))}
          {filters.environments.map((env) => (
            <span
              key={env}
              className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs bg-primary/10 text-primary"
            >
              {env}
              <button
                onClick={() => handleEnvironmentToggle(env)}
                className="hover:text-destructive"
              >
                <X className="h-3 w-3" />
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
