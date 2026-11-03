package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
)

func TestJanuaBatchBRemainsOffBeforeAnyNativeOrCredentialRead(t *testing.T) {
	require.False(t, januaBatchAApplySupported)
	require.False(t, januaBatchBApplySupported)
	var h *Handler
	_, e := h.runJanuaStageMigration(context.Background(), uuid.New(), uuid.New(), "fixture-plan", "fixture reason", false)
	require.ErrorContains(t, e, "disabled")
}

func TestJanuaProducerIsExactMainRootAPIAndHasNoHistoryFallback(t *testing.T) {
	p, e := isolatedBootstrapProfileFor(k8s.JanuaSchemaProfile)
	require.NoError(t, e)
	require.Equal(t, "madfam-org/janua", p.Producer)
	require.Equal(t, "docker-publish.yml", p.Workflow)
	require.Equal(t, "k8s/overlays/staging/kustomization.yaml", p.PinPath)
	require.Equal(t, "ghcr.io/madfam-org/janua-api", p.Image)
	require.False(t, p.PinHistory.enabled())
	require.Contains(t, p.RequiredChecks, "API Tests (PostgreSQL)")
	require.Len(t, p.SourceRequirements, 26)
	for _, file := range p.SourceRequirements {
		if file.Path != "Dockerfile.api" && file.Path != "apps/api/alembic/env.py" && file.Path != "apps/api/app/core/database_ssl.py" && file.Path != "apps/api/app/core/isolated_staging_alembic.py" {
			require.Len(t, file.SHA256, 64)
		}
	}
}

func TestJanuaProducerRequiresActualExactMainNativeWorkflow(t *testing.T) {
	for _, mode := range []string{"good", "cancelled", "foreign", "required-skipped", "required-missing", "duplicate", "truncated", "pending"} {
		t.Run(mode, func(t *testing.T) {
			p := januaStageMigrationProfile()
			source := strings.Repeat("a", 40)
			run := map[string]any{"id": int64(123), "head_sha": source, "head_branch": "main", "event": "push", "status": "completed", "conclusion": "success", "run_attempt": 1}
			if mode == "cancelled" {
				run["conclusion"] = "cancelled"
			}
			if mode == "foreign" {
				run["head_branch"] = "fixture"
			}
			if mode == "pending" {
				run["status"] = "in_progress"
			}
			jobs := []any{}
			for _, name := range p.RequiredChecks {
				jobs = append(jobs, map[string]any{"name": name, "run_id": 123, "head_sha": source, "status": "completed", "conclusion": "success"})
			}
			if mode == "required-skipped" {
				jobs[0].(map[string]any)["conclusion"] = "skipped"
			}
			if mode == "required-missing" {
				jobs = jobs[1:]
			}
			if mode == "duplicate" {
				jobs = append(jobs, jobs[0])
			}
			adapter := bootstrapProducerIO{Client: &http.Client{Transport: bootstrapProofTransport(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "api.github.com", r.URL.Host)
				var body string
				if strings.Contains(r.URL.Path, "/workflows/tests.yml/runs") {
					count := 1
					if mode == "truncated" {
						count = 2
					}
					body = bootstrapJSON(map[string]any{"total_count": count, "workflow_runs": []any{run}})
				} else {
					require.Contains(t, r.URL.Path, "/actions/runs/123/jobs")
					body = bootstrapJSON(map[string]any{"total_count": len(jobs), "jobs": jobs})
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}
			e := adapter.verifyJanuaStageMainCI(context.Background(), p, source)
			if mode == "good" {
				require.NoError(t, e)
			} else {
				require.Error(t, e)
			}
		})
	}
}

func TestJanuaBatchBIsNotAdmittedThroughGenericBootstrap(t *testing.T) {
	for _, dry := range []bool{true, false} {
		t.Run(fmt.Sprint(dry), func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			var h *Handler
			router.POST("/projects/:slug/addons/:id/initial", h.InitialBootstrapAddon)
			body := fmt.Sprintf(`{"profile":"%s","service_id":"%s","idempotency_key":"fixture","reason":"fixture reason","dry_run":%t}`, k8s.JanuaSchemaProfile, uuid.New(), dry)
			req := httptest.NewRequest(http.MethodPost, "/projects/janua/addons/"+uuid.NewString()+"/initial", strings.NewReader(body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}
