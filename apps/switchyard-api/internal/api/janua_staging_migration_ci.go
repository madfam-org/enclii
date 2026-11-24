package api

import (
	"context"
	"strconv"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
)

// These are the existing Janua workflows, not a new attestation pipeline.
// A check with the right display name is insufficient: qualify jobs from the
// actual exact-SHA main workflow runs. No skipped mandatory API/PG job qualifies.
func verifyJanuaMigrationCI(ctx context.Context, a bootstrapProducerIO, revision string) error {
	if !bootstrapSHA.MatchString(revision) {
		return januastagemigrate.ErrRefused
	}
	workflows := map[string]map[string]bool{
		"ci.yml":    {"NetworkPolicy port consistency": true, "Lint & Typecheck": true, "CLI Tests (@janua/cli)": true, "Build": true, "API Tests": false, "E2E Tests": false},
		"tests.yml": {"API Tests (Python)": true, "API Tests (PostgreSQL)": true, "MCP schema evidence": true, "API Diagnostics (advisory)": true, "E2E Tests (Playwright)": true, "Coverage Quality Gate": true},
	}
	for workflow, expected := range workflows {
		var runs struct {
			Total int `json:"total_count"`
			Runs  []struct {
				ID                        int64  `json:"id"`
				SHA                       string `json:"head_sha"`
				Branch                    string `json:"head_branch"`
				Attempt                   int    `json:"run_attempt"`
				Path                      string `json:"path"`
				Status, Conclusion, Event string
				Repository                struct {
					FullName string `json:"full_name"`
				}
			} `json:"workflow_runs"`
		}
		base := "https://api.github.com/repos/madfam-org/janua"
		if a.readJSON(ctx, base+"/actions/workflows/"+workflow+"/runs?head_sha="+revision+"&branch=main&per_page=100", a.GitHubToken, &runs) != nil || len(runs.Runs) == 0 || runs.Total != len(runs.Runs) {
			return januastagemigrate.ErrRefused
		}
		latest := 0
		for i, run := range runs.Runs {
			if run.SHA != revision || run.Branch != "main" || run.Path != ".github/workflows/"+workflow || run.Repository.FullName != "madfam-org/janua" || run.ID <= 0 {
				return januastagemigrate.ErrRefused
			}
			if run.ID > runs.Runs[latest].ID {
				latest = i
			}
		}
		run := runs.Runs[latest]
		if run.Attempt <= 0 || run.Status != "completed" || run.Conclusion != "success" || run.Event != "push" {
			return januastagemigrate.ErrRefused
		}
		var jobs struct {
			Total int `json:"total_count"`
			Jobs  []struct {
				Name, Status, Conclusion string
				RunID                    int64  `json:"run_id"`
				SHA                      string `json:"head_sha"`
				Attempt                  int    `json:"run_attempt"`
			} `json:"jobs"`
		}
		if a.readJSON(ctx, base+"/actions/runs/"+strconv.FormatInt(run.ID, 10)+"/jobs?filter=latest&per_page=100", a.GitHubToken, &jobs) != nil || jobs.Total != len(jobs.Jobs) || len(jobs.Jobs) != len(expected) {
			return januastagemigrate.ErrRefused
		}
		seen := map[string]bool{}
		for _, job := range jobs.Jobs {
			mandatory, exists := expected[job.Name]
			if !exists || seen[job.Name] || job.Status != "completed" || (job.Conclusion != "success" && (mandatory || job.Conclusion != "skipped")) || job.RunID != run.ID || job.SHA != revision || job.Attempt != run.Attempt {
				return januastagemigrate.ErrRefused
			}
			seen[job.Name] = true
		}
	}
	// Retain the existing producer verifier's complete commit-check/status
	// closure as well: qualifying API workflows must not hide a failed security
	// or other check on the same source revision.
	var checks struct {
		Total int `json:"total_count"`
		Runs  []struct {
			Status, Conclusion string
			SHA                string `json:"head_sha"`
		} `json:"check_runs"`
	}
	base := "https://api.github.com/repos/madfam-org/janua/commits/" + revision
	if a.readJSON(ctx, base+"/check-runs?per_page=100", a.GitHubToken, &checks) != nil || checks.Total != len(checks.Runs) || len(checks.Runs) == 0 || len(checks.Runs) > 100 {
		return januastagemigrate.ErrRefused
	}
	for _, check := range checks.Runs {
		if check.SHA != revision || check.Status != "completed" || (check.Conclusion != "success" && check.Conclusion != "skipped") {
			return januastagemigrate.ErrRefused
		}
	}
	var status struct {
		State string
		Total int `json:"total_count"`
	}
	if a.readJSON(ctx, base+"/status", a.GitHubToken, &status) != nil || status.Total < 0 || (status.Total > 0 && status.State != "success") {
		return januastagemigrate.ErrRefused
	}
	return nil
}
