package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/stretchr/testify/require"
)

func TestTargetReceiptLookupIsFreshOperatorAndReadOnly(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, scenario := range []string{"recorded", "absent", "unavailable", "cached-operator-revoked", "query", "body", "malformed", "noncanonical"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			actor, id := uuid.New(), uuid.New()
			h := &Handler{repos: db.NewRepositories(database), auth: &auth.JWTManager{}}
			h.workloadTargetSnapshotReader = func(context.Context, workloadtargets.Request) (workloadtargets.Snapshot, error) {
				t.Fatal("receipt lookup reached native/source reader")
				return workloadtargets.Snapshot{}, nil
			}
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id = \$1`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(scenario != "cached-operator-revoked"))
			path := "/v1/ops/apps/bind-target/" + id.String()
			body := ""
			if scenario == "query" {
				path += "?selector=invalid"
			}
			if scenario == "body" {
				body = `{"args":{}}`
			}
			if scenario == "malformed" {
				path = "/v1/ops/apps/bind-target/not-a-uuid"
			}
			if scenario == "noncanonical" {
				path = "/v1/ops/apps/bind-target/" + strings.ToUpper(id.String())
			}
			if scenario == "recorded" || scenario == "absent" || scenario == "unavailable" {
				q := mock.ExpectQuery(`SELECT payload FROM service_deployment_target_events WHERE id=\$1`).WithArgs(id)
				if scenario == "unavailable" {
					q.WillReturnError(context.DeadlineExceeded)
				} else {
					rows := sqlmock.NewRows([]string{"payload"})
					if scenario == "recorded" {
						s := planSnapshot(workloadtargets.Request{Project: "fixture", ServiceID: uuid.New(), Environment: "production", Namespace: "fixture", Deployment: "fixture-web", DryRun: true})
						p, err := workloadtargets.ValidateSnapshot(s, "fixture", "fixture-web")
						require.NoError(t, err)
						r := workloadtargets.Receipt{Version: "service-deployment-target-applied-v1", ID: id, Binding: workloadtargets.Binding{ID: id, Identity: p.Identity, PlanSHA: p.SHA, Versions: p.Versions}, ActorID: actor, Reason: "private-audit@fixture.invalid", RequestSHA: strings.Repeat("c", 64)}
						raw, _ := json.Marshal(r)
						rows.AddRow(raw)
					}
					q.WillReturnRows(rows)
				}
			}
			engine := gin.New()
			engine.Use(withPlatformAdminContext(actor))
			registerOperatorRoutes(engine.Group("/v1"), h)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, strings.NewReader(body)))
			expected := 200
			if scenario == "cached-operator-revoked" {
				expected = 403
			} else if scenario == "unavailable" {
				expected = 503
			} else if scenario == "query" || scenario == "body" || scenario == "malformed" || scenario == "noncanonical" {
				expected = 400
			}
			require.Equal(t, expected, response.Code, response.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
			require.NotContains(t, response.Body.String(), "private-audit@fixture.invalid")
			if scenario == "absent" {
				require.Contains(t, response.Body.String(), `"status":"not_recorded"`)
			}
			if scenario == "unavailable" {
				require.NotContains(t, response.Body.String(), `"status":"not_recorded"`)
			}
		})
	}
}
