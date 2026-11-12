package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
)

// Reuse the native producer's bounded content-addressed registry reader and
// revision-tag corroboration. Unlike generic bootstrap, the existing Janua
// producer does not label its OCI config: its revision has already come from
// the verified certificate and been independently reverified with exact flags.
func verifyJanuaMigrationImage(ctx context.Context, a bootstrapProducerIO, image string, hint januaMigrationCertificateHint) error {
	digest := strings.TrimPrefix(image, januastagemigrate.ImageRepository+"@")
	if image != januastagemigrate.ImageRepository+"@"+digest || !bootstrapDigest.MatchString(digest) || !bootstrapSHA.MatchString(hint.Revision) {
		return januastagemigrate.ErrRefused
	}
	token, err := a.registryToken(ctx, "madfam-org/janua-api")
	if err != nil {
		return januastagemigrate.ErrRefused
	}
	base := "https://ghcr.io/v2/madfam-org/janua-api"
	type manifest struct {
		Config    struct{ Digest string } `json:"config"`
		Manifests []struct {
			Digest   string
			Platform struct{ Architecture, OS string }
		}
	}
	var m manifest
	if a.readJSON(ctx, base+"/manifests/"+digest, token, &m) != nil {
		return januastagemigrate.ErrRefused
	}
	if len(m.Manifests) > 0 {
		if m.Config.Digest != "" || len(m.Manifests) > 16 {
			return januastagemigrate.ErrRefused
		}
		selected := ""
		for _, child := range m.Manifests {
			if child.Platform.Architecture == "amd64" && child.Platform.OS == "linux" {
				if selected != "" {
					return januastagemigrate.ErrRefused
				}
				selected = child.Digest
			}
		}
		if !bootstrapDigest.MatchString(selected) {
			return januastagemigrate.ErrRefused
		}
		m = manifest{}
		if a.readJSON(ctx, base+"/manifests/"+selected, token, &m) != nil || len(m.Manifests) > 0 {
			return januastagemigrate.ErrRefused
		}
	}
	if !bootstrapDigest.MatchString(m.Config.Digest) {
		return januastagemigrate.ErrRefused
	}
	var config struct{ Architecture, OS string }
	if a.readJSON(ctx, base+"/blobs/"+m.Config.Digest, token, &config) != nil || config.Architecture != "amd64" || config.OS != "linux" || a.verifyRevisionTag(ctx, base, token, hint.Revision, digest) != nil {
		return januastagemigrate.ErrRefused
	}
	var runs struct {
		Total int `json:"total_count"`
		Runs  []struct {
			ID                        int64  `json:"id"`
			SHA                       string `json:"head_sha"`
			Branch                    string `json:"head_branch"`
			Attempt                   int    `json:"run_attempt"`
			Path                      string `json:"path"`
			Event, Status, Conclusion string
			Repository                struct {
				FullName string `json:"full_name"`
			}
		} `json:"workflow_runs"`
	}
	gh := "https://api.github.com/repos/madfam-org/janua"
	if a.readJSON(ctx, gh+"/actions/workflows/docker-publish.yml/runs?head_sha="+hint.Revision+"&branch=main&per_page=100", a.GitHubToken, &runs) != nil || runs.Total != len(runs.Runs) || len(runs.Runs) == 0 {
		return januastagemigrate.ErrRefused
	}
	latest := 0
	for i, run := range runs.Runs {
		if run.ID <= 0 || run.SHA != hint.Revision || run.Branch != "main" || run.Path != ".github/workflows/docker-publish.yml" || run.Repository.FullName != "madfam-org/janua" {
			return januastagemigrate.ErrRefused
		}
		if run.ID > runs.Runs[latest].ID {
			latest = i
		}
	}
	run := runs.Runs[latest]
	if run.Event != hint.Event || run.Status != "completed" || run.Conclusion != "success" || run.Attempt <= 0 {
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
	if a.readJSON(ctx, gh+"/actions/runs/"+strconv.FormatInt(run.ID, 10)+"/jobs?filter=latest&per_page=100", a.GitHubToken, &jobs) != nil || jobs.Total != len(jobs.Jobs) || len(jobs.Jobs) == 0 || len(jobs.Jobs) > 16 {
		return januastagemigrate.ErrRefused
	}
	builds := 0
	for _, job := range jobs.Jobs {
		if job.RunID != run.ID || job.SHA != hint.Revision || job.Attempt != run.Attempt || job.Status != "completed" || (job.Conclusion != "success" && job.Conclusion != "skipped") {
			return januastagemigrate.ErrRefused
		}
		if job.Name == "Build API" {
			if job.Conclusion != "success" {
				return januastagemigrate.ErrRefused
			}
			builds++
		}
	}
	if builds != 1 {
		return januastagemigrate.ErrRefused
	}
	return nil
}
