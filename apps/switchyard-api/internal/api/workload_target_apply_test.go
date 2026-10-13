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

func TestTargetApplyFreshPlanAuditAndExactIdempotency(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, scenario := range []string{"success", "stale-config-RV", "recreated-deployment", "missing-intent", "operator-revoked", "audit-failure", "same-UUID-read", "changed-same-UUID", "unknown-commit-found", "unknown-commit-absent"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			actor := uuid.New()
			r := workloadtargets.Request{Project: "fixture", Environment: "production", Namespace: "fixture", Deployment: "fixture-web", ServiceID: uuid.New(), IdempotencyID: uuid.New(), Reason: "synthetic apply proof"}
			snapshot := planSnapshot(r)
			plan, err := workloadtargets.ValidateSnapshot(snapshot, r.Namespace, r.Deployment)
			require.NoError(t, err)
			r.ExpectedPlan = plan.SHA
			requestSHA, err := workloadtargets.RequestHash(r, actor)
			require.NoError(t, err)
			receipt := workloadtargets.Receipt{Version: "service-deployment-target-applied-v1", ID: r.IdempotencyID, Binding: workloadtargets.Binding{ID: r.IdempotencyID, Identity: plan.Identity, PlanSHA: plan.SHA, Versions: plan.Versions}, ActorID: actor, Reason: r.Reason, RequestSHA: requestSHA}
			raw, _ := json.Marshal(receipt)
			h := &Handler{auth: &auth.JWTManager{}, repos: db.NewRepositories(database), logger: newNopLogger()}
			reads := 0
			h.workloadTargetSnapshotReader = func(context.Context, workloadtargets.Request) (workloadtargets.Snapshot, error) {
				reads++
				if reads == 2 {
					if scenario == "stale-config-RV" {
						snapshot.ArgoConfig.ResourceVersion = "changed"
					}
					if scenario == "recreated-deployment" {
						snapshot.Deployment.UID = "recreated"
					}
				}
				if scenario == "missing-intent" {
					snapshot.Registration = nil
				}
				return snapshot, nil
			}
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id = \$1`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
			expectTargetLock(mock, r.ServiceID, snapshot.Project.ID)
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id=\$1 FOR SHARE`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(scenario != "operator-revoked"))
			if scenario != "operator-revoked" {
				receiptQuery := mock.ExpectQuery(`SELECT payload FROM service_deployment_target_events WHERE id=\$1`).WithArgs(r.IdempotencyID)
				if strings.Contains(scenario, "same-UUID") {
					if scenario == "changed-same-UUID" {
						changed := receipt
						changed.RequestSHA = strings.Repeat("c", 64)
						raw, _ = json.Marshal(changed)
					}
					receiptQuery.WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(raw))
				} else {
					receiptQuery.WillReturnRows(sqlmock.NewRows([]string{"payload"}))
					if scenario != "missing-intent" {
						mock.ExpectQuery(`SELECT project_id FROM environments WHERE id=\$1 FOR SHARE`).WithArgs(snapshot.Environment.ID).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(snapshot.Project.ID))
						mock.ExpectQuery(`SELECT project_id FROM onboarding_registrations WHERE id=\$1 FOR SHARE`).WithArgs(snapshot.Registration.ID).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(snapshot.Project.ID))
						if scenario != "stale-config-RV" && scenario != "recreated-deployment" {
							mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id=\$1 FOR SHARE`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
							mock.ExpectQuery(`SELECT payload FROM service_deployment_target_events WHERE id=\$1`).WithArgs(r.IdempotencyID).WillReturnRows(sqlmock.NewRows([]string{"payload"}))
							mock.ExpectExec(`INSERT INTO service_deployment_targets`).WithArgs(r.IdempotencyID, snapshot.Project.ID, r.ServiceID, snapshot.Environment.ID, snapshot.Registration.ID, string(snapshot.Namespace.UID), string(snapshot.Deployment.UID), actor.String(), r.Reason, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
							audit := mock.ExpectExec(`INSERT INTO service_deployment_target_events`).WithArgs(r.IdempotencyID, r.IdempotencyID, actor.String(), requestSHA, r.Reason, sqlmock.AnyArg())
							if scenario == "audit-failure" {
								audit.WillReturnError(context.DeadlineExceeded)
							} else {
								audit.WillReturnResult(sqlmock.NewResult(1, 1))
							}
						}
					}
				}
			}
			status := http.StatusOK
			switch scenario {
			case "stale-config-RV", "recreated-deployment", "missing-intent", "audit-failure", "changed-same-UUID":
				status = http.StatusConflict
			case "operator-revoked":
				status = http.StatusForbidden
			case "unknown-commit-absent":
				status = http.StatusServiceUnavailable
			}
			if strings.HasPrefix(scenario, "unknown-commit") {
				mock.ExpectCommit().WillReturnError(context.DeadlineExceeded)
				query := mock.ExpectQuery(`SELECT payload FROM service_deployment_target_events WHERE id=\$1`).WithArgs(r.IdempotencyID)
				if scenario == "unknown-commit-found" {
					query.WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(raw))
				} else {
					query.WillReturnRows(sqlmock.NewRows([]string{"payload"}))
				}
			} else if scenario == "same-UUID-read" || status != http.StatusOK {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			engine := gin.New()
			engine.Use(withUserContext(actor, "admin"))
			registerOperatorRoutes(engine.Group("/v1"), h)
			body, _ := json.Marshal(map[string]interface{}{"dry_run": false, "expect_plan_sha256": r.ExpectedPlan, "reason": r.Reason, "idempotency_key": r.IdempotencyID.String(), "scope": map[string]string{"project": r.Project, "service": r.ServiceID.String(), "environment": r.Environment}, "args": map[string]string{"namespace": r.Namespace, "deployment": r.Deployment}})
			response := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/v1/ops/apps/bind-target", strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(response, request)
			require.Equal(t, status, response.Code, response.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
			if strings.Contains(scenario, "same-UUID") || scenario == "operator-revoked" {
				require.Zero(t, reads)
			} else if scenario == "missing-intent" {
				require.Equal(t, 1, reads)
			} else {
				require.Equal(t, 2, reads)
			}
			if status == http.StatusOK {
				require.Contains(t, response.Body.String(), `"status":"applied"`)
			}
			if scenario == "unknown-commit-absent" {
				require.Contains(t, response.Body.String(), `"status":"commit_unknown"`)
				require.NotContains(t, response.Body.String(), `"status":"applied"`)
			}
		})
	}
}
