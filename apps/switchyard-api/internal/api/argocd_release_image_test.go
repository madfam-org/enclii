package api

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func observerImageFixture() (*types.Service, *types.Release, *types.Deployment, ArgocdSyncRequest, string) {
	svc := &types.Service{ID: uuid.New(), ProjectID: uuid.New(), Name: "project-api"}
	revision := strings.Repeat("a", 40)
	base := "ghcr.io/fixture/project/api@sha256:"
	old, next := base+strings.Repeat("b", 64), base+strings.Repeat("c", 64)
	rel := &types.Release{ID: uuid.New(), ServiceID: svc.ID, GitSHA: revision, Version: "argocd-" + shortSHA(revision), ImageURI: old, Status: types.ReleaseStatusReady}
	dep := &types.Deployment{ID: uuid.New(), ReleaseID: rel.ID, Status: types.DeploymentStatusRunning}
	req := ArgocdSyncRequest{AppName: "fixture-services", Revision: revision, Trigger: "sync-succeeded", SyncStatus: "Synced", HealthStatus: "Healthy", Images: []string{next}}
	return svc, rel, dep, req, next
}

func settledObserverApp(req ArgocdSyncRequest) unstructured.Unstructured {
	app := newTestArgoApp(req.AppName, "Synced", "Healthy", req.Revision, req.Images...)
	app.SetUID("fixture-app-uid")
	app.SetResourceVersion("17")
	_ = unstructured.SetNestedField(app.Object, "Succeeded", "status", "operationState", "phase")
	_ = unstructured.SetNestedField(app.Object, req.Revision, "status", "operationState", "syncResult", "revision")
	return app
}

func TestArgocdImageCorrectionEligibility(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*types.Release, *types.Service, *ArgocdSyncRequest, *string)
	}{
		{"CI release", func(r *types.Release, _ *types.Service, _ *ArgocdSyncRequest, _ *string) { r.Version = "ci-v1" }},
		{"historical revision", func(r *types.Release, _ *types.Service, _ *ArgocdSyncRequest, _ *string) { r.GitSHA = "older" }},
		{"different service", func(r *types.Release, _ *types.Service, _ *ArgocdSyncRequest, _ *string) { r.ServiceID = uuid.New() }},
		{"signed image", func(r *types.Release, _ *types.Service, _ *ArgocdSyncRequest, _ *string) {
			r.ImageSignature = "signature"
		}},
		{"verified image", func(r *types.Release, _ *types.Service, _ *ArgocdSyncRequest, _ *string) {
			now := time.Now()
			r.SignatureVerifiedAt = &now
		}},
		{"SBOM", func(r *types.Release, _ *types.Service, _ *ArgocdSyncRequest, _ *string) { r.SBOM = "sbom" }},
		{"SBOM format", func(r *types.Release, _ *types.Service, _ *ArgocdSyncRequest, _ *string) { r.SBOMFormat = "spdx" }},
		{"not ready", func(r *types.Release, _ *types.Service, _ *ArgocdSyncRequest, _ *string) {
			r.Status = types.ReleaseStatusBuilding
		}},
		{"tag only", func(_ *types.Release, _ *types.Service, _ *ArgocdSyncRequest, image *string) {
			*image = "ghcr.io/fixture/project/api:main"
		}},
		{"different repository", func(_ *types.Release, _ *types.Service, _ *ArgocdSyncRequest, image *string) {
			*image = strings.Replace(*image, "/api@", "/other@", 1)
		}},
		{"unchanged", func(r *types.Release, _ *types.Service, _ *ArgocdSyncRequest, image *string) { *image = r.ImageURI }},
		{"unhealthy", func(_ *types.Release, _ *types.Service, req *ArgocdSyncRequest, _ *string) {
			req.HealthStatus = "Progressing"
		}},
		{"out of sync", func(_ *types.Release, _ *types.Service, req *ArgocdSyncRequest, _ *string) {
			req.SyncStatus = "OutOfSync"
		}},
		{"failed callback", func(_ *types.Release, _ *types.Service, req *ArgocdSyncRequest, _ *string) {
			req.Trigger = "sync-failed"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, rel, _, req, image := observerImageFixture()
			require.True(t, argocdImageCorrectionEligible(rel, svc, req, image))
			test.edit(rel, svc, &req, &image)
			require.False(t, argocdImageCorrectionEligible(rel, svc, req, image))
		})
	}
}

func TestArgocdCurrentImageObservationRefusesUnsettledMixedAndOutdated(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*unstructured.Unstructured, *types.Release)
	}{
		{"outdated callback image", func(a *unstructured.Unstructured, r *types.Release) {
			_ = unstructured.SetNestedStringSlice(a.Object, []string{r.ImageURI}, "status", "summary", "images")
		}},
		{"mixed rollout", func(a *unstructured.Unstructured, r *types.Release) {
			imgs, _, _ := unstructured.NestedStringSlice(a.Object, "status", "summary", "images")
			_ = unstructured.SetNestedStringSlice(a.Object, append(imgs, r.ImageURI), "status", "summary", "images")
		}},
		{"newer revision", func(a *unstructured.Unstructured, _ *types.Release) {
			_ = unstructured.SetNestedField(a.Object, "newer", "status", "sync", "revision")
		}},
		{"previous completed operation", func(a *unstructured.Unstructured, _ *types.Release) {
			_ = unstructured.SetNestedField(a.Object, "older", "status", "operationState", "syncResult", "revision")
		}},
		{"incomplete operation", func(a *unstructured.Unstructured, _ *types.Release) {
			_ = unstructured.SetNestedField(a.Object, "Running", "status", "operationState", "phase")
		}},
		{"progressing", func(a *unstructured.Unstructured, _ *types.Release) {
			_ = unstructured.SetNestedField(a.Object, "Progressing", "status", "health", "status")
		}},
		{"out of sync", func(a *unstructured.Unstructured, _ *types.Release) {
			_ = unstructured.SetNestedField(a.Object, "OutOfSync", "status", "sync", "status")
		}},
		{"no UID", func(a *unstructured.Unstructured, _ *types.Release) { a.SetUID("") }},
		{"no resource version", func(a *unstructured.Unstructured, _ *types.Release) { a.SetResourceVersion("") }},
		{"deleted", func(a *unstructured.Unstructured, _ *types.Release) {
			now := metav1.Now()
			a.SetDeletionTimestamp(&now)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, rel, _, req, image := observerImageFixture()
			resolve := func(string) (*types.Service, string) { return svc, svc.Name }
			app := settledObserverApp(req)
			proof, err := argocdCurrentImageObservation(app, req, svc, image, resolve)
			require.NoError(t, err)
			require.Equal(t, "17", proof.resourceVersion)
			test.edit(&app, rel)
			_, err = argocdCurrentImageObservation(app, req, svc, image, resolve)
			require.Error(t, err)
		})
	}
}

func TestArgocdMixedServiceImagesAreOrderIndependent(t *testing.T) {
	svc, rel, _, req, next := observerImageFixture()
	other := &types.Service{ID: uuid.New(), Name: "other"}
	resolve := func(image string) (*types.Service, string) {
		if image == "unknown" {
			return nil, ""
		}
		if image == "other" {
			return other, other.Name
		}
		return svc, svc.Name
	}
	for _, images := range [][]string{{rel.ImageURI, next, "other", "unknown"}, {next, "other", rel.ImageURI}} {
		got := argocdUnambiguousServiceImages(images, resolve)
		require.Len(t, got, 1)
		require.Equal(t, other.ID, got[0].service.ID)
		obs := argocdObservation{Revision: req.Revision, Images: images}
		changed := argocdPollDecision(obs, func(i string) *types.Service { s, _ := resolve(i); return s }, func(string) (*types.Deployment, *types.Release) { return nil, nil })
		require.Equal(t, []string{"other"}, changed)
	}
	require.Len(t, argocdUnambiguousServiceImages([]string{next, next}, resolve), 1)
}

type observerAuditMatch struct{ previous, image, revision string }

func (m observerAuditMatch) Match(value driver.Value) bool {
	b, ok := value.([]byte)
	if !ok {
		return false
	}
	var data map[string]interface{}
	if json.Unmarshal(b, &data) != nil {
		return false
	}
	return data["previous_image"] == m.previous && data["observed_image"] == m.image && data["revision"] == m.revision &&
		data["application_uid"] == "fixture-app-uid" && data["application_resource_version"] == "17" &&
		data["scope"] == "OBSERVED_METADATA_ONLY_NOT_RUNTIME_IDENTITY"
}

func expectObserverAudit(mock sqlmock.Sqlmock, svc *types.Service, rel *types.Release, req ArgocdSyncRequest, image string) *sqlmock.ExpectedExec {
	return mock.ExpectExec(`INSERT INTO audit_logs`).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), nil,
		"argocd@system.enclii.dev", types.RoleSystem, "release.argocd_image_reconciled", "release", rel.ID.String(), svc.Name,
		&svc.ProjectID, nil, nil, "", "success", observerAuditMatch{rel.ImageURI, image, req.Revision}, sqlmock.AnyArg())
}

func TestArgocdObservedImageAuditIsAtomicAndCASRefusesRaces(t *testing.T) {
	for _, result := range []string{"success", "audit failure", "update failure", "lost comparison", "commit failure"} {
		t.Run(result, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			svc, rel, dep, req, image := observerImageFixture()
			mock.ExpectBegin()
			update := mock.ExpectExec(`UPDATE releases AS r SET image_uri`).WithArgs(rel.ID, svc.ID, dep.ID, req.Revision, rel.ImageURI, image)
			if result == "update failure" {
				update.WillReturnError(errors.New("write failed"))
				mock.ExpectRollback()
			} else if result == "lost comparison" {
				update.WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectCommit()
			} else {
				update.WillReturnResult(sqlmock.NewResult(0, 1))
				audit := expectObserverAudit(mock, svc, rel, req, image)
				if result == "audit failure" {
					audit.WillReturnError(errors.New("audit failed"))
					mock.ExpectRollback()
				} else {
					audit.WillReturnResult(sqlmock.NewResult(0, 1))
					if result == "commit failure" {
						mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
					} else {
						mock.ExpectCommit()
					}
				}
			}
			updated, err := reconcileArgocdObservedImage(context.Background(), db.NewRepositories(database), req, argocdSyncSourcePoller, svc, rel, dep, image, argocdImageObservation{"fixture-app-uid", "17"})
			require.Equal(t, result == "success", updated)
			if result == "success" || result == "lost comparison" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, strings.Repeat("b", 64), imageDigest(rel.ImageURI)[7:], "the transaction helper must not mutate in-memory history")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func expectObserverService(mock sqlmock.Sqlmock, svc *types.Service) {
	cols := []string{"id", "project_id", "name", "git_repo", "app_path", "build_config", "auto_deploy", "auto_deploy_branch", "auto_deploy_env", "k8s_namespace", "created_at", "updated_at", "jobs", "type", "region"}
	now := time.Now()
	mock.ExpectQuery(`FROM services WHERE name = \$1`).WithArgs(svc.Name).WillReturnRows(sqlmock.NewRows(cols).AddRow(svc.ID, svc.ProjectID, svc.Name, "https://github.com/fixture/project", "", []byte(`{}`), false, "main", "production", nil, now, now, []byte(`[]`), "web", "default"))
}

func expectObserverDeployment(mock sqlmock.Sqlmock, svc *types.Service, dep *types.Deployment) {
	cols := []string{"id", "release_id", "environment_id", "replicas", "status", "health", "error_message", "service_id", "version_number", "created_at", "updated_at"}
	now := time.Now()
	mock.ExpectQuery(`FROM deployments d`).WithArgs(svc.ID.String()).WillReturnRows(sqlmock.NewRows(cols).AddRow(dep.ID, dep.ReleaseID, uuid.New(), 1, dep.Status, types.HealthStatusHealthy, nil, svc.ID, nil, now, now))
}

func TestArgocdCallbackReconcilesBeforeSameReleaseNoop(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	svc, rel, dep, req, image := observerImageFixture()
	app := settledObserverApp(req)
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), &app)
	h := &Handler{repos: db.NewRepositories(database), logger: testPollerLogger(t), k8sClient: &k8s.Client{DynamicClient: client}}
	expectObserverService(mock, svc)
	cols := []string{"id", "service_id", "version", "image_uri", "git_sha", "status", "sbom", "sbom_format", "image_signature", "signature_verified_at", "error_message", "framework_slug", "created_at", "updated_at"}
	now := time.Now()
	mock.ExpectQuery(`FROM releases WHERE service_id = \$1`).WithArgs(svc.ID).WillReturnRows(sqlmock.NewRows(cols).AddRow(rel.ID, svc.ID, rel.Version, rel.ImageURI, req.Revision, rel.Status, nil, nil, nil, nil, nil, nil, now, now))
	mock.ExpectQuery(`FROM deployment_lifecycle_events`).WithArgs(req.Revision).WillReturnError(errors.New("no metadata fixture"))
	mock.ExpectExec(`UPDATE releases SET commit_message`).WillReturnResult(sqlmock.NewResult(0, 1))
	expectObserverDeployment(mock, svc, dep)
	expectObserverService(mock, svc)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE releases AS r SET image_uri`).WithArgs(rel.ID, svc.ID, dep.ID, req.Revision, rel.ImageURI, image).WillReturnResult(sqlmock.NewResult(0, 1))
	expectObserverAudit(mock, svc, rel, req, image).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectObserverDeployment(mock, svc, dep)
	require.Zero(t, h.processArgocdSyncRequest(context.Background(), req, argocdSyncSourceWebhook), "repair metadata without another deployment or lifecycle event")
	require.NoError(t, mock.ExpectationsWereMet())
	require.Len(t, client.Actions(), 1)
	require.Equal(t, "get", client.Actions()[0].GetVerb(), "no cluster writes")
}

func TestArgocdImageReconciliationRefusesBeforeDatabaseWrites(t *testing.T) {
	for _, scenario := range []string{"stale callback", "mixed current summary", "advanced deployment", "failed deployment", "signed release", "reader unavailable", "reader failure"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			svc, rel, dep, req, image := observerImageFixture()
			app := settledObserverApp(req)
			if scenario == "stale callback" {
				old := rel.ImageURI
				rel.ImageURI = image
				image = old
				req.Images = []string{old}
			}
			if scenario == "mixed current summary" {
				_ = unstructured.SetNestedStringSlice(app.Object, []string{rel.ImageURI, image}, "status", "summary", "images")
			}
			if scenario == "advanced deployment" {
				dep.ReleaseID = uuid.New()
			}
			if scenario == "failed deployment" {
				dep.Status = types.DeploymentStatusFailed
			}
			if scenario == "signed release" {
				rel.ImageSignature = "retained-signature"
			} else {
				expectObserverDeployment(mock, svc, dep)
			}
			objects := []runtime.Object{&app}
			if scenario == "reader failure" {
				objects = nil
			}
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
			h := &Handler{repos: db.NewRepositories(database), logger: testPollerLogger(t), k8sClient: &k8s.Client{DynamicClient: client}}
			if scenario == "reader unavailable" {
				h.k8sClient = nil
			}
			if scenario == "stale callback" {
				expectObserverService(mock, svc)
			}
			if scenario == "mixed current summary" {
				expectObserverService(mock, svc)
				expectObserverService(mock, svc)
			}
			updated, err := h.reconcileArgocdReleaseImage(context.Background(), req, argocdSyncSourceWebhook, svc, rel, image)
			require.Error(t, err)
			require.False(t, updated)
			require.NoError(t, mock.ExpectationsWereMet(), "no transaction, image update or audit write")
			for _, action := range client.Actions() {
				require.Equal(t, "get", action.GetVerb())
			}
		})
	}
}
