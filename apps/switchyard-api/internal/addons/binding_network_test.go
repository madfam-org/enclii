package addons

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type bindingURIProbe struct {
	AddonProvisioner
	calls int
}

func (p *bindingURIProbe) GetConnectionURI(context.Context, *types.DatabaseAddon) (string, error) {
	p.calls++
	return "synthetic-runtime-uri", nil
}

func bindingNetworkFixture(t *testing.T) (*AddonService, sqlmock.Sqlmock, *fake.Clientset, *types.DatabaseAddon, *types.Service, *bindingURIProbe) {
	t.Helper()
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	repos := db.NewRepositories(raw)
	repos.ManagedDBAddonEvents = nil
	project, addonID := uuid.New(), uuid.New()
	ns := "synthetic-training"
	addon := &types.DatabaseAddon{ID: addonID, ProjectID: project, Type: types.DatabaseAddonTypePostgres, Name: "kernel", Plan: "standard-1", Status: types.DatabaseAddonStatusReady, Config: types.DatabaseAddonConfig{DatabaseName: "kernel_sim", RuntimeRole: "kernel_runtime"}, K8sNamespace: "project-" + project.String()[:8], K8sResourceName: "pg-kernel-" + addonID.String()[:8]}
	service := &types.Service{ID: uuid.New(), ProjectID: project, Name: "kernel", K8sNamespace: &ns}
	kube := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{k8s.LabelProjectNamespace: project.String()}}}, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: addon.K8sNamespace, Labels: map[string]string{k8s.LabelProjectNamespace: project.String()}}})
	probe := &bindingURIProbe{}
	svc := &AddonService{repos: repos, k8sClient: &k8s.Client{KubeClient: kube}, logger: testLogger(), provisioners: map[types.DatabaseAddonType]AddonProvisioner{types.DatabaseAddonTypePostgres: probe}}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	return svc, mock, kube, addon, service, probe
}

func expectBindingAddon(mock sqlmock.Sqlmock, a *types.DatabaseAddon) {
	cfg, _ := json.Marshal(a.Config)
	now := time.Now()
	mock.ExpectQuery(`FROM database_addons WHERE id = \$1`).WithArgs(a.ID).WillReturnRows(sqlmock.NewRows(addonColsSvc).AddRow(a.ID, a.ProjectID, nil, string(a.Type), a.Name, a.Plan, string(a.Status), "", cfg, a.K8sNamespace, a.K8sResourceName, a.ConnectionSecret, "", nil, "", "", 0, 0, nil, nil, "", now, now, nil, nil, nil))
}

func expectBindingService(mock sqlmock.Sqlmock, s *types.Service) {
	now := time.Now()
	mock.ExpectQuery(`FROM services WHERE id = \$1`).WithArgs(s.ID).WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "git_repo", "app_path", "build_config", "volumes", "auto_deploy", "auto_deploy_branch", "auto_deploy_env", "created_at", "updated_at", "jobs", "type", "region", "health_check", "k8s_namespace"}).AddRow(s.ID, s.ProjectID, s.Name, "example/synthetic-training", "", []byte(`{}`), []byte(`[]`), false, "main", "production", now, now, []byte(`[]`), "web", "default", nil, *s.K8sNamespace))
}

func expectBindingRows(mock sqlmock.Sqlmock, s *types.Service, b ...*types.DatabaseAddonBinding) {
	rows := sqlmock.NewRows([]string{"id", "addon_id", "service_id", "env_var_name", "status", "created_at", "updated_at"})
	now := time.Now()
	for _, r := range b {
		rows.AddRow(r.ID, r.AddonID, r.ServiceID, r.EnvVarName, r.Status, now, now)
	}
	mock.ExpectQuery(`FROM database_addon_bindings\s+WHERE service_id = \$1`).WithArgs(s.ID).WillReturnRows(rows)
}

func activeNetworkBinding(a *types.DatabaseAddon, s *types.Service) *types.DatabaseAddonBinding {
	return &types.DatabaseAddonBinding{ID: uuid.New(), AddonID: a.ID, ServiceID: s.ID, EnvVarName: "DATABASE_URL", Status: types.DatabaseAddonBindingStatusActive}
}

func TestBindingNetworkCrossProjectRejectedBeforeWrite(t *testing.T) {
	for _, zero := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign", true: "zero"}[zero], func(t *testing.T) {
			svc, mock, kube, a, s, probe := bindingNetworkFixture(t)
			if zero {
				a.ProjectID = uuid.Nil
				s.ProjectID = uuid.Nil
			} else {
				s.ProjectID = uuid.New()
			}
			expectBindingAddon(mock, a)
			expectBindingService(mock, s)
			b, err := svc.CreateBinding(context.Background(), a.ID, s.ID, "")
			if !errors.Is(err, ErrCrossProjectBinding) || b != nil || probe.calls != 0 || len(kube.Actions()) != 0 {
				t.Fatal("cross-project binding reached a writer")
			}
		})
	}
}

func TestBindingNetworkCreateFailureDeactivatesLedger(t *testing.T) {
	svc, mock, kube, a, s, probe := bindingNetworkFixture(t)
	ns, _ := kube.CoreV1().Namespaces().Get(context.Background(), *s.K8sNamespace, metav1.GetOptions{})
	ns.Labels[k8s.LabelProjectNamespace] = uuid.New().String()
	_, _ = kube.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})
	expectBindingAddon(mock, a)
	expectBindingService(mock, s)
	mock.ExpectExec(`INSERT INTO database_addon_bindings`).WithArgs(sqlmock.AnyArg(), a.ID, s.ID, sqlmock.AnyArg(), types.DatabaseAddonBindingStatusActive, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE database_addon_bindings`).WithArgs(types.DatabaseAddonBindingStatusDeleted, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	b, err := svc.CreateBinding(context.Background(), a.ID, s.ID, "")
	if err == nil || b != nil || probe.calls != 0 {
		t.Fatal("failed network creation exposed credentials")
	}
	policies, _ := kube.NetworkingV1().NetworkPolicies(*s.K8sNamespace).List(context.Background(), metav1.ListOptions{})
	if len(policies.Items) != 0 {
		t.Fatal("failed creation left an allow policy")
	}
}

func TestBindingNetworkEnvRequiresReadyAddon(t *testing.T) {
	svc, mock, kube, a, s, probe := bindingNetworkFixture(t)
	a.Status = types.DatabaseAddonStatusProvisioning
	b := activeNetworkBinding(a, s)
	expectBindingRows(mock, s, b)
	expectBindingService(mock, s)
	expectBindingAddon(mock, a)
	env, err := svc.GetEnvVarsForService(context.Background(), s.ID)
	if err != nil || len(env) != 0 || probe.calls != 0 || len(kube.Actions()) != 0 {
		t.Fatal("not-ready runtime secret injected")
	}
}

func TestBindingNetworkEnvUsesOnlyStillActiveBinding(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(map[bool]string{true: "active", false: "concurrently-deleted"}[active], func(t *testing.T) {
			svc, mock, kube, a, s, probe := bindingNetworkFixture(t)
			b := activeNetworkBinding(a, s)
			expectBindingRows(mock, s, b)
			expectBindingService(mock, s)
			expectBindingAddon(mock, a)
			if active {
				expectBindingRows(mock, s, b)
			} else {
				expectBindingRows(mock, s)
			}
			env, err := svc.GetEnvVarsForService(context.Background(), s.ID)
			if active {
				if err != nil || env["DATABASE_URL"] != "synthetic-runtime-uri" || probe.calls != 1 {
					t.Fatalf("active binding failed: %v", err)
				}
			} else {
				if err == nil || len(env) != 0 || probe.calls != 0 {
					t.Fatal("deleted binding exposed a runtime URI")
				}
			}
			policies, _ := kube.NetworkingV1().NetworkPolicies(*s.K8sNamespace).List(context.Background(), metav1.ListOptions{})
			if active && len(policies.Items) != 1 || !active && len(policies.Items) != 0 {
				t.Fatal("binding policy not reconciled to active ledger")
			}
		})
	}
}

func TestBindingNetworkEnvFailureDeactivatesLedger(t *testing.T) {
	svc, mock, kube, a, s, probe := bindingNetworkFixture(t)
	b := activeNetworkBinding(a, s)
	ns, _ := kube.CoreV1().Namespaces().Get(context.Background(), *s.K8sNamespace, metav1.GetOptions{})
	delete(ns.Labels, k8s.LabelProjectNamespace)
	_, _ = kube.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})
	expectBindingRows(mock, s, b)
	expectBindingService(mock, s)
	expectBindingAddon(mock, a)
	mock.ExpectExec(`UPDATE database_addon_bindings`).WithArgs(types.DatabaseAddonBindingStatusDeleted, sqlmock.AnyArg(), b.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	env, err := svc.GetEnvVarsForService(context.Background(), s.ID)
	if err == nil || len(env) != 0 || probe.calls != 0 {
		t.Fatal("unverified network emitted credentials")
	}
}

// A live (active or suspended) binding for the same pair is a clean conflict,
// never an opaque failure, and nothing reaches the network or credentials.
func TestBindingNetworkDuplicateLiveBindingIsConflict(t *testing.T) {
	svc, mock, kube, a, s, probe := bindingNetworkFixture(t)
	expectBindingAddon(mock, a)
	expectBindingService(mock, s)
	mock.ExpectExec(`INSERT INTO database_addon_bindings`).WithArgs(sqlmock.AnyArg(), a.ID, s.ID, sqlmock.AnyArg(), types.DatabaseAddonBindingStatusActive, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(&pq.Error{Code: "23505", Constraint: "unique_live_binding_per_service"})
	b, err := svc.CreateBinding(context.Background(), a.ID, s.ID, "")
	if !errors.Is(err, ErrBindingExists) || b != nil || probe.calls != 0 || len(kube.Actions()) != 0 {
		t.Fatalf("duplicate live binding was not a clean conflict: %v", err)
	}
}
