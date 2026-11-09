package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/stretchr/testify/require"
)

func TestKarafielHeldRegistrationParserIsClosed(t *testing.T) {
	id := "22000000-0000-4000-8000-000000000001"
	valid := `{"operation_id":"` + id + `","dry_run":true}`
	_, _, ok := parseKarafielStageServiceRequest([]byte(valid))
	require.True(t, ok)
	apply := `{"operation_id":"` + id + `","dry_run":false,"expect_plan_sha256":"` + strings.Repeat("a", 64) + `","reason":"reviewed metadata only"}`
	_, _, ok = parseKarafielStageServiceRequest([]byte(apply))
	require.True(t, ok)
	for _, raw := range []string{
		`{}`, `{"operation_id":"` + id + `","dry_run":null}`, `{"operation_id":"` + id + `","dry_run":true,"namespace":"karafiel"}`,
		`{"operation_id":"` + id + `","dry_run":true,"service_id":"` + id + `"}`, `{"operation_id":"` + id + `","dry_run":true,"dry_run":false}`,
		`{"operation_id":"` + id + `","dry_run":true,"expect_plan_sha256":null}`, `{"operation_id":"` + id + `","dry_run":false}`,
		strings.Replace(apply, "reviewed metadata only", `private\nreason`, 1), valid + ` {}`, strings.Repeat(" ", 8193),
	} {
		_, _, ok := parseKarafielStageServiceRequest([]byte(raw))
		require.False(t, ok, raw)
	}
}

func TestKarafielHeldRegistrationHTTPReplayRequiresSameActorHashAndNoRuntimeEffects(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-intent", true: "changed-intent"}[changed], func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			h := &Handler{repos: db.NewRepositories(database), auth: &auth.JWTManager{}, logger: testLogger(t)}
			actor, op, project := uuid.New(), uuid.New(), uuid.New()
			service := workloadtargets.KarafielStagingServiceID(project, op)
			s := planSnapshot(workloadtargets.Request{Project: "fixture", ServiceID: service, Environment: "production", Namespace: "fixture", Deployment: "fixture-web", DryRun: true})
			plan, err := workloadtargets.ValidateSnapshot(s, "fixture", "fixture-web")
			require.NoError(t, err)
			i := plan.Identity
			i.ProjectID = project
			i.ServiceID = service
			i.SourceAuthority = workloadtargets.KarafielStagingHeldAuthority
			i.SourceRootUID = "root:fixture"
			i.NamespaceOwnershipPending = true
			i.Namespace = workloadtargets.KarafielStagingNamespace
			i.Deployment = "karafiel-api"
			i.Application = "karafiel-staging"
			i.Repo = "https://github.com/madfam-org/karafiel"
			i.Path = "infra/k8s/overlays/staging"
			i.Ref = "release/karafiel-staging"
			versions := plan.Versions
			versions.SourceRoot = "5"
			request := workloadtargets.Request{Project: "karafiel", ServiceID: service, Environment: "staging", Namespace: workloadtargets.KarafielStagingNamespace, Deployment: "karafiel-api", ExpectedPlan: strings.Repeat("a", 64), Reason: "held metadata only", IdempotencyID: op}
			sha, err := workloadtargets.RequestHash(request, actor)
			require.NoError(t, err)
			receipt := workloadtargets.Receipt{Version: "service-deployment-target-applied-v1", ID: op, ActorID: actor, Reason: request.Reason, RequestSHA: sha, Binding: workloadtargets.Binding{ID: op, Identity: i, PlanSHA: request.ExpectedPlan, Versions: versions}}
			raw, err := json.Marshal(receipt)
			require.NoError(t, err)
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
			mock.ExpectQuery(`SELECT is_platform_admin AND active FROM users WHERE id=\$1`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(true))
			now := time.Now()
			mock.ExpectQuery(`FROM projects WHERE slug = \$1`).WithArgs("karafiel").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(project, "karafiel", "karafiel", "auto", now, now))
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR UPDATE`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(project))
			mock.ExpectQuery(`SELECT id FROM services WHERE project_id=\$1 ORDER BY id`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id=\$1 FOR SHARE`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
			mock.ExpectQuery(`SELECT active FROM users WHERE id=\$1 FOR SHARE`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"active"}).AddRow(true))
			mock.ExpectQuery(`SELECT payload FROM service_deployment_target_events WHERE id=\$1`).WithArgs(op).WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(raw))
			mock.ExpectRollback()
			reason := request.Reason
			if changed {
				reason = "changed intent"
			}
			body := `{"operation_id":"` + op.String() + `","dry_run":false,"expect_plan_sha256":"` + request.ExpectedPlan + `","reason":"` + reason + `"}`
			router := gin.New()
			router.Use(withUserContext(actor, "admin"))
			registerKarafielStageServiceAdminRoutes(router.Group("/v1/admin"), h)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/admin/projects/karafiel/environments/staging/service-registration", strings.NewReader(body)))
			want := http.StatusOK
			if changed {
				want = http.StatusConflict
			}
			require.Equal(t, want, response.Code, response.Body.String())
			if !changed {
				require.Contains(t, response.Body.String(), `"activation_held":true`)
				require.Contains(t, response.Body.String(), `"namespace_ownership_pending":true`)
				require.Contains(t, response.Body.String(), `"idempotent":true`)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestKarafielHeldRegistrationRequiresWholeFlatQuarantineSource(t *testing.T) {
	fixture := func() []checks.ManifestFile {
		return []checks.ManifestFile{
			{Path: "kustomization.yaml", Content: "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nnamespace: enclii-karafiel-staging\nresources: [held.yaml]\n"},
			{Path: "held.yaml", Content: func() string {
				var content string
				for _, name := range []string{"api", "web", "admin", "worker", "beat"} {
					content += "---\napiVersion: apps/v1\nkind: Deployment\nmetadata: {name: karafiel-" + name + ", namespace: enclii-karafiel-staging}\nspec: {replicas: 0}\n"
				}
				return content
			}()},
		}
	}
	require.NoError(t, validateKarafielRegistrationQuarantine(fixture()))
	retained := append(fixture(), checks.ManifestFile{Path: "env-patch-api.yaml", Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: karafiel-api}\nspec: {replicas: 99}\n"}, checks.ManifestFile{Path: "old-deferred-job.yaml", Content: "apiVersion: batch/v1\nkind: Job\nmetadata: {name: not-applied}\n"})
	require.NoError(t, validateKarafielRegistrationQuarantine(retained), "real projection retains unreferenced source patches; they are not applied")
	retained[0].Content = strings.Replace(retained[0].Content, "[held.yaml]", "[held.yaml, old-deferred-job.yaml]", 1)
	require.Error(t, validateKarafielRegistrationQuarantine(retained), "a referenced executor is still forbidden")
	foreign := fixture()
	foreign[1].Content += "---\napiVersion: v1\nkind: Service\nmetadata: {name: foreign, namespace: karafiel}\n"
	require.Error(t, validateKarafielRegistrationQuarantine(foreign), "all referenced resources must stay in the stage namespace")
	for _, kind := range []string{"Job", "CronJob", "HorizontalPodAutoscaler", "PodDisruptionBudget", "Pod", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController"} {
		t.Run(kind, func(t *testing.T) {
			files := fixture()
			files[1].Content += "---\napiVersion: batch/v1\nkind: " + kind + "\nmetadata: {name: forbidden, namespace: enclii-karafiel-staging}\n"
			require.Error(t, validateKarafielRegistrationQuarantine(files))
		})
	}
	for name, change := range map[string]func([]checks.ManifestFile){
		"replicas-one": func(files []checks.ManifestFile) {
			files[1].Content = strings.Replace(files[1].Content, "replicas: 0", "replicas: 1", 1)
		},
		"missing-replicas": func(files []checks.ManifestFile) {
			files[1].Content = strings.Replace(files[1].Content, "replicas: 0", "", 1)
		},
		"duplicate-deployment": func(files []checks.ManifestFile) {
			files[1].Content = strings.Replace(files[1].Content, "karafiel-beat", "karafiel-worker", 1)
		},
		"foreign-namespace": func(files []checks.ManifestFile) {
			files[1].Content = strings.Replace(files[1].Content, "namespace: enclii-karafiel-staging", "namespace: karafiel", 1)
		},
		"normal-overlay-base": func(files []checks.ManifestFile) {
			files[0].Content = strings.Replace(files[0].Content, "held.yaml", "../../../production", 1)
		},
		"unsupported-transform": func(files []checks.ManifestFile) { files[0].Content += "patches: []\n" },
	} {
		t.Run(name, func(t *testing.T) {
			files := fixture()
			change(files)
			require.Error(t, validateKarafielRegistrationQuarantine(files))
		})
	}
}

func TestKarafielHeldRegistrationResponseAlwaysStatesHeldAndHistoricalOwnership(t *testing.T) {
	for _, pending := range []bool{false, true} {
		r := &workloadtargets.Receipt{Binding: workloadtargets.Binding{Identity: workloadtargets.Identity{NamespaceOwnershipPending: pending}}}
		wire := heldStageRegistrationResponse(r, true)
		require.Equal(t, true, wire["activation_held"])
		require.Equal(t, pending, wire["namespace_ownership_pending"])
		require.Equal(t, "HELD_METADATA_REGISTERED", wire["phase"])
		require.NotContains(t, wire, "ready")
	}
}

func TestKarafielHeldRegistrationRequiresActualPlatformOperatorRegardlessOfFlag(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	h, mock, cleanup := setupTenantScopeHandler(t)
	defer cleanup()
	h.auth = &auth.JWTManager{}
	user := uuid.New()
	mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(false))
	router := gin.New()
	router.Use(withUserContext(user, "admin"))
	registerKarafielStageServiceAdminRoutes(router.Group("/v1/admin"), h)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/projects/karafiel/environments/staging/service-registration", strings.NewReader(`{"operation_id":"22000000-0000-4000-8000-000000000001","dry_run":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
