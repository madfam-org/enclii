package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
)

func TestKarafielNormalCIProofRequiresExactLatestCompleteFiveJobs(t *testing.T) {
	p, err := isolatedBootstrapProfileFor(k8s.KarafielSchemaProfile)
	require.NoError(t, err)
	source := strings.Repeat("a", 40)
	for _, mode := range []string{"accepted", "latest-failed", "attempt-two", "foreign-source", "missing-job", "duplicate-job", "unknown-job", "skipped-job", "truncated-runs", "truncated-jobs"} {
		t.Run(mode, func(t *testing.T) {
			run := map[string]any{"id": 123, "head_sha": source, "head_branch": "main", "run_attempt": 1, "status": "completed", "conclusion": "success"}
			jobs := []any{}
			for _, name := range p.RequiredChecks {
				jobs = append(jobs, map[string]any{"name": name, "run_id": 123, "head_sha": source, "run_attempt": 1, "status": "completed", "conclusion": "success"})
			}
			switch mode {
			case "latest-failed":
				run["conclusion"] = "failure"
			case "attempt-two":
				run["run_attempt"] = 2
			case "foreign-source":
				run["head_sha"] = strings.Repeat("b", 40)
			case "missing-job":
				jobs = jobs[1:]
			case "duplicate-job":
				jobs[1] = jobs[0]
			case "unknown-job":
				jobs[0].(map[string]any)["name"] = "unreviewed"
			case "skipped-job":
				jobs[0].(map[string]any)["conclusion"] = "skipped"
			}
			adapter := bootstrapProducerIO{Client: &http.Client{Transport: bootstrapProofTransport(func(r *http.Request) (*http.Response, error) {
				var body string
				if strings.HasSuffix(r.URL.Path, "/runs") {
					count := 1
					if mode == "truncated-runs" {
						count = 101
					}
					body = bootstrapJSON(map[string]any{"total_count": count, "workflow_runs": []any{run}})
				} else {
					require.True(t, strings.HasSuffix(r.URL.Path, "/jobs"))
					count := len(jobs)
					if mode == "truncated-jobs" {
						count = 101
					}
					body = bootstrapJSON(map[string]any{"total_count": count, "jobs": jobs})
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			count, err := adapter.verifyClosedBootstrapCI(context.Background(), p, source)
			if mode == "accepted" {
				require.NoError(t, err)
				require.Equal(t, 5, count)
			} else {
				require.Error(t, err)
				require.Zero(t, count)
			}
		})
	}
}

func TestKarafielProducerSignatureAndSourceRequirementsCannotUseLegacyEntryPoint(t *testing.T) {
	p, err := isolatedBootstrapProfileFor(k8s.KarafielSchemaProfile)
	require.NoError(t, err)
	require.Equal(t, "karafiel-api-staging", p.Service)
	require.Equal(t, "release-build-candidate.yml", p.Workflow)
	require.Len(t, p.SourceRequirements, 2)
	for _, source := range p.SourceRequirements {
		require.True(t, bootstrapHexKey.MatchString(source.SHA256))
	}
	p.CandidateRun = 123
	source := strings.Repeat("a", 40)
	args := strings.Join(bootstrapSignatureArgs(p, p.Image+"@sha256:"+strings.Repeat("b", 64), source, "workflow_dispatch"), " ")
	require.Contains(t, args, "--annotations source_sha="+source)
	require.Contains(t, args, "--annotations candidate_run_id=123")
	require.NotContains(t, args, "ignore-tlog")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &Handler{}
	router.POST("/", h.InitialBootstrapAddon)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"profile":"karafiel-staging-migrate","service_id":"22000000-0000-4000-8000-000000000001"}`)))
	require.Equal(t, http.StatusBadRequest, response.Code)
}
