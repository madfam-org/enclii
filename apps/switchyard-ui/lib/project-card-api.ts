import { apiGet } from "@/lib/api";
import type { CompactProject } from "@/components/dashboard/project-card-compact";
import {
  projectCardAggregateToCompactProject,
  type ApiProjectCardAggregate,
} from "@/lib/project-card-transform";

interface ProjectCardsResponse {
  generated_at: string;
  projects: ApiProjectCardAggregate[];
  count: number;
}

export interface ProjectCardsResult {
  generatedAt: string;
  projects: CompactProject[];
}

export async function fetchProjectCards(): Promise<ProjectCardsResult> {
  const data = await apiGet<ProjectCardsResponse>("/v1/projects/cards");

  return {
    generatedAt: data.generated_at,
    projects: (data.projects || []).map(projectCardAggregateToCompactProject),
  };
}
