package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

func januaStageMigrationProfile() isolatedBootstrapProfile {
	var graph struct {
		Migrations []struct{ Path, Checksum string } `json:"migrations"`
	}
	if json.Unmarshal([]byte(k8s.JanuaStageMigrations()), &graph) != nil {
		panic("embedded Janua source contract invalid")
	}
	requirements := []bootstrapSourceRequirement{{Path: "Dockerfile.api"}, {Path: "apps/api/app/core/isolated_staging_alembic.py", SHA256: "f889cf91063a61c7e7dce3fa870284387cfef0977200f4f19c141ebfb524cb3c"}, {Path: "apps/api/alembic/env.py", SHA256: "05af5096a4ab53e2fdceaa3644c456f9c56ba0c9decc0879cc1665efd1e10d1f"}, {Path: "apps/api/app/core/database_ssl.py", SHA256: "780440a6c4be3ccef300912e79958f4ac17930ec97fb49ddc7748a73f51a4f83"}}
	for _, file := range graph.Migrations {
		requirements = append(requirements, bootstrapSourceRequirement{Path: file.Path, SHA256: file.Checksum})
	}
	return isolatedBootstrapProfile{Name: k8s.JanuaSchemaProfile, Project: "janua", Service: "janua-api-staging", Namespace: "janua-staging", Database: "janua_staging", RuntimeRole: "janua_staging_runtime", Producer: "madfam-org/janua", Workflow: "docker-publish.yml", PinPath: "k8s/overlays/staging/kustomization.yaml", Image: "ghcr.io/madfam-org/janua-api", SignerIdentity: `^https://github\.com/madfam-org/janua/\.github/workflows/docker-publish\.yml@refs/heads/main$`, RequiredChecks: []string{"API Tests (Python)", "API Tests (PostgreSQL)", "MCP schema evidence", "Coverage Quality Gate"}, SourceRequirements: requirements}
}

// Unlike a PR-tree shortcut, the latest exact-source main Tests run must
// actually finish successfully. Its native PostgreSQL positive-count guard is
// part of the immutable workflow source, not inferred from a skipped check.
func (a bootstrapProducerIO) verifyJanuaStageMainCI(ctx context.Context, p isolatedBootstrapProfile, source string) error {
	if p.Name != k8s.JanuaSchemaProfile || p.Producer != "madfam-org/janua" {
		return fmt.Errorf("closed Janua producer refused")
	}
	var runs struct {
		TotalCount int `json:"total_count"`
		Runs       []struct {
			ID                        int64
			HeadSHA                   string `json:"head_sha"`
			HeadBranch                string `json:"head_branch"`
			Event, Status, Conclusion string
			RunAttempt                int `json:"run_attempt"`
		} `json:"workflow_runs"`
	}
	if a.readJSON(ctx, "https://api.github.com/repos/"+p.Producer+"/actions/workflows/tests.yml/runs?head_sha="+source+"&branch=main&event=push&per_page=100", a.GitHubToken, &runs) != nil || len(runs.Runs) == 0 || runs.TotalCount != len(runs.Runs) {
		return fmt.Errorf("complete Janua source main CI inventory unavailable")
	}
	var latest int64
	var selected struct {
		Status, Conclusion string
		Attempt            int
	}
	for _, run := range runs.Runs {
		if run.HeadSHA != source || run.HeadBranch != "main" || run.Event != "push" {
			return fmt.Errorf("foreign Janua source CI refused")
		}
		if run.ID > latest {
			latest = run.ID
			selected.Status = run.Status
			selected.Conclusion = run.Conclusion
			selected.Attempt = run.RunAttempt
		}
	}
	if latest <= 0 || selected.Status != "completed" || selected.Conclusion != "success" || selected.Attempt < 1 {
		return fmt.Errorf("Janua source main CI not successful")
	}

	var matrix struct {
		TotalCount int `json:"total_count"`
		Jobs       []struct {
			Name, Status, Conclusion string
			RunID                    int64  `json:"run_id"`
			HeadSHA                  string `json:"head_sha"`
		} `json:"jobs"`
	}
	if a.readJSON(ctx, "https://api.github.com/repos/"+p.Producer+"/actions/runs/"+strconv.FormatInt(latest, 10)+"/jobs?filter=latest&per_page=100", a.GitHubToken, &matrix) != nil || matrix.TotalCount != len(matrix.Jobs) {
		return fmt.Errorf("complete Janua main CI jobs unavailable")
	}
	wanted := map[string]int{}
	for _, name := range p.RequiredChecks {
		wanted[name] = 0
	}
	for _, job := range matrix.Jobs {
		if job.RunID != latest || job.HeadSHA != source || job.Status != "completed" || job.Conclusion != "success" && job.Conclusion != "skipped" {
			return fmt.Errorf("Janua main CI not closed")
		}
		if _, ok := wanted[job.Name]; ok {
			if job.Conclusion != "success" {
				return fmt.Errorf("required Janua native/source proof skipped")
			}
			wanted[job.Name]++
		}
	}
	for _, count := range wanted {
		if count != 1 {
			return fmt.Errorf("required Janua source proof absent or ambiguous")
		}
	}
	return nil
}
