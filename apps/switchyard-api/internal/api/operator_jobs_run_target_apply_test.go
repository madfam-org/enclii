package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestRunTargetApplyBoundsEffectsAndUnknownOutcomes(t *testing.T) {
	for _, scenario := range []string{"submitted", "stale plan", "unbound", "SQL operator refused", "job create unknown", "receipt create unknown", "audit unknown", "commit unknown", "replay"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			r, target, err := parseRunTarget([]byte(targetRunBody))
			require.NoError(t, err)
			s := boundRunSnapshot(t, target)
			_, _, sha, err := runTargetPlan(r, target, s)
			require.NoError(t, err)
			actor := uuid.New()
			r.DryRun = boolPointer(false)
			r.ExpectedPlan, r.IdempotencyKey, r.Reason = sha, "11000000-0000-4000-8000-000000000003", "reviewed fixture observer"
			if scenario == "stale plan" {
				r.ExpectedPlan = strings.Repeat("c", 64)
			}
			if scenario == "unbound" {
				s.Incumbent = nil
			}
			name := "target-job-" + r.IdempotencyKey
			hash := runTargetHash(struct {
				Actor   uuid.UUID
				Request runTargetRequest
			}{actor, r})
			client := fake.NewSimpleClientset()
			if scenario == "replay" {
				intent := targetRunReceipt(name+"-intent", target.Namespace, hash, nil)
				intent.UID = "intent-uid"
				receipt := targetRunReceipt(name+"-receipt", target.Namespace, hash, map[string]string{"job_uid": "job-uid", "intent_uid": "intent-uid"})
				job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: target.Namespace, UID: "job-uid", Annotations: map[string]string{"enclii.dev/request-sha256": hash}, OwnerReferences: []metav1.OwnerReference{{Kind: "ConfigMap", Name: intent.Name, UID: intent.UID}}}}
				client = fake.NewSimpleClientset(intent, receipt, job)
			}
			client.PrependReactor("create", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
				cm := a.(ktesting.CreateAction).GetObject().(*corev1.ConfigMap)
				if strings.HasSuffix(cm.Name, "-receipt") && scenario == "receipt create unknown" {
					return true, nil, errors.New("uncertain create")
				}
				cm.UID = ktypes.UID(cm.Name + "-uid")
				return false, nil, nil
			})
			client.PrependReactor("create", "jobs", func(a ktesting.Action) (bool, runtime.Object, error) {
				if scenario == "job create unknown" {
					return true, nil, errors.New("uncertain create")
				}
				job := a.(ktesting.CreateAction).GetObject().(*batchv1.Job)
				job.UID = "job-uid"
				return false, nil, nil
			})
			h := &Handler{repos: db.NewRepositories(database), logger: newNopLogger(), k8sClient: &k8s.Client{KubeClient: client}}
			reads := 0
			h.workloadTargetSnapshotReader = func(context.Context, workloadtargets.Request) (workloadtargets.Snapshot, error) {
				reads++
				return s, nil
			}
			expectTargetLock(mock, target.ServiceID, s.Project.ID)
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id=\$1 FOR SHARE`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(scenario != "SQL operator refused"))
			if scenario != "SQL operator refused" && scenario != "replay" {
				mock.ExpectQuery(`SELECT project_id FROM environments WHERE id=\$1 FOR SHARE`).WithArgs(s.Environment.ID).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(s.Project.ID))
				mock.ExpectQuery(`SELECT project_id FROM onboarding_registrations WHERE id=\$1 FOR SHARE`).WithArgs(s.Registration.ID).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(s.Project.ID))
			}
			if scenario == "submitted" || scenario == "audit unknown" || scenario == "commit unknown" || scenario == "replay" {
				audit := mock.ExpectExec(`INSERT INTO audit_logs`)
				if scenario == "audit unknown" {
					audit.WillReturnError(errors.New("audit unavailable"))
				} else {
					audit.WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}
			status := http.StatusServiceUnavailable
			switch scenario {
			case "submitted":
				status = http.StatusAccepted
				mock.ExpectCommit()
			case "replay":
				status = http.StatusOK
				mock.ExpectCommit()
			case "stale plan", "unbound":
				status = http.StatusConflict
				mock.ExpectRollback()
			case "SQL operator refused":
				status = http.StatusForbidden
				mock.ExpectRollback()
			case "commit unknown":
				mock.ExpectCommit().WillReturnError(errors.New("commit unknown"))
			default:
				mock.ExpectRollback()
			}
			router := gin.New()
			router.Use(withUserContext(actor, "admin"))
			router.POST("/run", h.RunJobForVerifiedTarget)
			body, err := json.Marshal(r)
			require.NoError(t, err)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/run", strings.NewReader(string(body))))
			require.Equal(t, status, response.Code, response.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
			creates := 0
			for _, a := range client.Actions() {
				require.NotContains(t, []string{"update", "patch", "delete"}, a.GetVerb())
				if a.GetVerb() == "create" && a.GetResource().Resource == "jobs" {
					creates++
				}
			}
			if scenario == "stale plan" || scenario == "unbound" || scenario == "SQL operator refused" || scenario == "replay" {
				require.Zero(t, creates)
			} else {
				require.Equal(t, 1, creates)
			}
			if scenario == "replay" || scenario == "SQL operator refused" {
				require.Zero(t, reads)
			} else {
				require.Equal(t, 2, reads)
			}
			if creates > 0 {
				// Whether the native reply is known or not, an intent survives to
				// prohibit resubmission. The source Deployment is never an owner.
				intent, err := client.CoreV1().ConfigMaps(target.Namespace).Get(t.Context(), name+"-intent", metav1.GetOptions{})
				require.NoError(t, err)
				require.True(t, *intent.Immutable)
				require.NotEmpty(t, intent.UID)
				require.NotContains(t, intent.Data, "command")
			}
		})
	}
}
