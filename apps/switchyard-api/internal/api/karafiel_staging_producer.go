package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

// Stage acceptance runs after the build producer, so its own check is still
// open. The new profile qualifies the exact completed normal CI workflow/run
// and complete five-job matrix; existing profiles retain their all-commit-check
// closure rules unchanged.
func (a bootstrapProducerIO) verifyClosedBootstrapCI(ctx context.Context, p isolatedBootstrapProfile, revision string) (int, error) {
	if p.Name != k8s.KarafielSchemaProfile || p.CIWorkflow != "ci.yml" || len(p.RequiredChecks) != 5 {
		return 0, fmt.Errorf("closed staging CI profile refused")
	}
	var runs struct {
		TotalCount   int `json:"total_count"`
		WorkflowRuns []struct {
			ID                 int64  `json:"id"`
			HeadSHA            string `json:"head_sha"`
			HeadBranch         string `json:"head_branch"`
			RunAttempt         int    `json:"run_attempt"`
			Status, Conclusion string
		} `json:"workflow_runs"`
	}
	if err := a.readJSON(ctx, "https://api.github.com/repos/"+p.Producer+"/actions/workflows/ci.yml/runs?head_sha="+revision+"&branch=main&status=completed&per_page=100", a.GitHubToken, &runs); err != nil {
		return 0, err
	}
	if len(runs.WorkflowRuns) == 0 || runs.TotalCount != len(runs.WorkflowRuns) {
		return 0, fmt.Errorf("complete exact main CI run inventory unavailable")
	}
	id := int64(0)
	for _, run := range runs.WorkflowRuns {
		if run.HeadSHA != revision || run.HeadBranch != "main" {
			return 0, fmt.Errorf("foreign CI source refused")
		}
		if run.ID > id {
			if run.Status != "completed" || run.Conclusion != "success" || run.RunAttempt != 1 {
				return 0, fmt.Errorf("latest first-attempt main CI is not successful")
			}
			id = run.ID
		}
	}
	if id <= 0 {
		return 0, fmt.Errorf("exact successful main CI run missing")
	}
	var matrix struct {
		TotalCount int `json:"total_count"`
		Jobs       []struct {
			Name, Status, Conclusion string
			RunID                    int64  `json:"run_id"`
			HeadSHA                  string `json:"head_sha"`
			RunAttempt               int    `json:"run_attempt"`
		} `json:"jobs"`
	}
	if err := a.readJSON(ctx, "https://api.github.com/repos/"+p.Producer+"/actions/runs/"+strconv.FormatInt(id, 10)+"/jobs?filter=latest&per_page=100", a.GitHubToken, &matrix); err != nil {
		return 0, err
	}
	if matrix.TotalCount != len(matrix.Jobs) || len(matrix.Jobs) != len(p.RequiredChecks) {
		return 0, fmt.Errorf("closed main CI matrix incomplete or broadened")
	}
	wanted := map[string]bool{}
	for _, name := range p.RequiredChecks {
		wanted[name] = true
	}
	for _, job := range matrix.Jobs {
		if !wanted[job.Name] || job.Status != "completed" || job.Conclusion != "success" || job.RunID != id || job.HeadSHA != revision || job.RunAttempt != 1 {
			return 0, fmt.Errorf("mandatory main CI job missing, ambiguous or unsuccessful")
		}
		delete(wanted, job.Name)
	}
	if len(wanted) != 0 {
		return 0, fmt.Errorf("mandatory main CI jobs missing")
	}
	return len(matrix.Jobs), nil
}
