package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	k8scorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// fakePostgres is an in-memory role table standing in for the shared cluster.
type fakePostgres struct {
	roles        map[string]string // role -> password last set ("" = unknown)
	ownerCalls   []string          // passwords passed to ProvisionGeneratedOwner
	appRoleCalls []string
}

func (f *fakePostgres) Provision(_ context.Context, spec *types.PostgresProvisionSpec) error {
	f.roles[ownerRoleName(spec)] = spec.RolePassword
	return nil
}

func (f *fakePostgres) ProvisionGeneratedOwner(_ context.Context, spec *types.PostgresProvisionSpec, pw string) error {
	f.ownerCalls = append(f.ownerCalls, pw)
	if pw != "" {
		f.roles[ownerRoleName(spec)] = pw
	}
	return nil
}

func (f *fakePostgres) RoleExists(_ context.Context, role string) (bool, error) {
	_, ok := f.roles[role]
	return ok, nil
}

func (f *fakePostgres) EnsureAppRole(_ context.Context, role provisioning.AppRole, pw string) (provisioning.AppRoleAction, error) {
	f.appRoleCalls = append(f.appRoleCalls, pw)
	_, exists := f.roles[role.RoleName]
	switch {
	case !exists:
		f.roles[role.RoleName] = pw
		return provisioning.AppRoleCreated, nil
	case pw != "":
		f.roles[role.RoleName] = pw
		return provisioning.AppRoleRotated, nil
	}
	return provisioning.AppRoleKept, nil
}

const testNS = "fabrication-prep"

func newCredentialFixture(t *testing.T) (credentialDeps, *fakePostgres, *fake.Clientset, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "switchyard.log")
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{Level: "debug", Format: "json", Output: logPath})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	cs := fake.NewSimpleClientset(
		&k8scorev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "pgbouncer-config", Namespace: "data"},
			Data:       map[string]string{"pgbouncer.ini": "[pgbouncer]\nauth_type = plain\n\n[databases]\nenclii = host=x dbname=enclii\n"},
		},
		&k8scorev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pgbouncer-userlist", Namespace: "data"},
			Data:       map[string][]byte{"userlist.txt": []byte("\"enclii\" \"placeholder-other\"\n")},
		},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pgbouncer", Namespace: "data"}},
	)
	pg := &fakePostgres{roles: map[string]string{}}
	counter := 0
	deps := credentialDeps{
		pg:      pg,
		pooler:  provisioning.NewPgBouncerUpdater(cs, logger),
		secrets: provisioning.NewSecretsProvisioner(cs, logger),
		logger:  logger,
		// Recognisable, unique, blocklist-clean values so the leak check can
		// search for each one.
		generate: func(n int) (string, error) {
			counter++
			return "GENVALUE" + strings.Repeat("Q", 8) + string(rune('a'+counter)), nil
		},
	}
	return deps, pg, cs, logPath
}

func fabricationPrepRequest() *types.OnboardingRequest {
	return &types.OnboardingRequest{
		RepoFullName: "madfam-org/fabrication-prep",
		ProjectName:  "fabrication-prep",
		ProvisionPostgres: &types.PostgresProvisionSpec{
			DatabaseName: "fabrication_prep", GeneratePassword: true,
		},
		ProvisionAppRole: &types.AppRoleSpec{RoleName: "fabrication_prep_app", ConnectionLimit: 8},
		GenerateSecrets:  []types.GeneratedSecretSpec{{Key: "ARTIFACT_URL_KEYS"}},
	}
}

func secretData(t *testing.T, cs *fake.Clientset, ns, name string) map[string][]byte {
	t.Helper()
	s, err := cs.CoreV1().Secrets(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret %s/%s: %v", ns, name, err)
	}
	return s.Data
}

func TestGeneratedCredentials_FirstRunWritesEverythingAndLeaksNothing(t *testing.T) {
	deps, pg, cs, logPath := newCredentialFixture(t)
	req := fabricationPrepRequest()

	results, steps := runCredentialSteps(context.Background(), deps, req, testNS)
	for _, s := range steps {
		if s.Err != nil {
			t.Fatalf("step %s failed: %v", s.Name, s.Err)
		}
	}

	data := secretData(t, cs, testNS, "fabrication-prep-credentials")
	owner, app, key := string(data["DATABASE_URL"]), string(data["APP_DATABASE_URL"]), string(data["ARTIFACT_URL_KEYS"])
	if !strings.HasPrefix(owner, "postgresql://fabrication_prep:GENVALUE") ||
		!strings.HasSuffix(owner, "@pgbouncer.data.svc.cluster.local:6432/fabrication_prep") {
		t.Errorf("owner URL has the wrong shape (value withheld)")
	}
	if !strings.HasPrefix(app, "postgresql://fabrication_prep_app:GENVALUE") {
		t.Errorf("app URL has the wrong shape (value withheld)")
	}
	if !strings.HasPrefix(key, "GENVALUE") {
		t.Errorf("generated secret not written")
	}
	if pg.roles["fabrication_prep_app"] == "" || pg.roles["fabrication_prep"] == "" {
		t.Errorf("roles not created with generated passwords")
	}

	userlist := string(secretData(t, cs, "data", "pgbouncer-userlist")["userlist.txt"])
	for _, role := range []string{"fabrication_prep", "fabrication_prep_app"} {
		if !strings.Contains(userlist, "\""+role+"\" \""+pg.roles[role]+"\"") {
			t.Errorf("userlist lacks the current line for %s", role)
		}
	}
	if !strings.Contains(userlist, "\"enclii\" \"placeholder-other\"") {
		t.Errorf("an unrelated userlist line was changed")
	}

	// Nothing generated may appear in the response JSON or in any log line.
	body, _ := json.Marshal(map[string]interface{}{"generated_credentials": results, "step_results": serializeStepResults(steps)})
	logs, _ := os.ReadFile(logPath)
	for _, v := range []string{pg.roles["fabrication_prep"], pg.roles["fabrication_prep_app"], key} {
		if strings.Contains(string(body), v) {
			t.Errorf("response JSON carries a generated value")
		}
		if strings.Contains(string(logs), v) {
			t.Errorf("log output carries a generated value")
		}
	}
	if len(logs) == 0 {
		t.Errorf("expected log lines to be captured")
	}
	actions := map[string]string{}
	for _, r := range results {
		actions[r.Name] = r.Action
	}
	if actions["fabrication_prep"] != "created" || actions["fabrication_prep_app"] != "created" || actions["ARTIFACT_URL_KEYS"] != "created" {
		t.Errorf("unexpected actions %v", actions)
	}
}

func TestGeneratedCredentials_RerunKeepsAndRotateReplaces(t *testing.T) {
	deps, pg, cs, _ := newCredentialFixture(t)
	req := fabricationPrepRequest()
	runCredentialSteps(context.Background(), deps, req, testNS)
	before := secretData(t, cs, testNS, "fabrication-prep-credentials")

	results, steps := runCredentialSteps(context.Background(), deps, req, testNS)
	for _, s := range steps {
		if s.Err != nil {
			t.Fatalf("re-run step %s failed: %v", s.Name, s.Err)
		}
		if s.Name == "pgbouncer_restart" {
			t.Errorf("re-run restarted the pooler although nothing changed")
		}
	}
	after := secretData(t, cs, testNS, "fabrication-prep-credentials")
	for k, v := range before {
		if string(after[k]) != string(v) {
			t.Errorf("re-run changed %s", k)
		}
	}
	for _, r := range results {
		if r.Action != "kept" {
			t.Errorf("%s: want kept, got %s", r.Name, r.Action)
		}
	}
	if last := pg.ownerCalls[len(pg.ownerCalls)-1]; last != "" {
		t.Errorf("re-run set an owner password")
	}

	req.ProvisionAppRole.RotatePassword = true
	runCredentialSteps(context.Background(), deps, req, testNS)
	rotated := secretData(t, cs, testNS, "fabrication-prep-credentials")
	if string(rotated["APP_DATABASE_URL"]) == string(before["APP_DATABASE_URL"]) {
		t.Errorf("rotate did not replace the app URL")
	}
	if string(rotated["DATABASE_URL"]) != string(before["DATABASE_URL"]) {
		t.Errorf("rotating the app role changed the owner URL")
	}
	userlist := string(secretData(t, cs, "data", "pgbouncer-userlist")["userlist.txt"])
	if strings.Count(userlist, "\"fabrication_prep_app\"") != 1 ||
		!strings.Contains(userlist, "\"fabrication_prep_app\" \""+pg.roles["fabrication_prep_app"]+"\"") {
		t.Errorf("rotation did not rewrite the userlist line in place")
	}
}

func TestGeneratedCredentials_ExistingRoleWithoutSecretKeyFailsVisibly(t *testing.T) {
	deps, pg, cs, _ := newCredentialFixture(t)
	pg.roles["fabrication_prep_app"] = "unknown"
	req := fabricationPrepRequest()
	req.ProvisionPostgres = nil
	req.GenerateSecrets = nil
	req.ProvisionAppRole.DatabaseName = "fabrication_prep"

	_, steps := runCredentialSteps(context.Background(), deps, req, testNS)
	if len(steps) != 1 || steps[0].Err == nil || !strings.Contains(steps[0].Detail, "refusing to rotate implicitly") {
		t.Fatalf("want a visible refusal, got %+v", steps)
	}
	if len(pg.appRoleCalls) != 0 {
		t.Errorf("the role was touched despite the refusal")
	}
	if _, err := cs.CoreV1().Secrets(testNS).Get(context.Background(), "fabrication-prep-credentials", metav1.GetOptions{}); err == nil {
		t.Errorf("a Secret was written despite the refusal")
	}
}

func TestGeneratedCredentials_UnconfiguredProvisionersFail(t *testing.T) {
	req := fabricationPrepRequest()
	_, steps := runCredentialSteps(context.Background(), credentialDeps{}, req, testNS)
	for _, s := range steps {
		if s.Err == nil {
			t.Errorf("step %s passed without provisioners", s.Name)
		}
	}
}

func TestValidateGeneratedCredentialRequest(t *testing.T) {
	base := func() *types.OnboardingRequest { return fabricationPrepRequest() }
	tests := []struct {
		name   string
		mutate func(r *types.OnboardingRequest)
		want   string
	}{
		{"valid", func(r *types.OnboardingRequest) {}, ""},
		{"both password modes", func(r *types.OnboardingRequest) { r.ProvisionPostgres.RolePassword = "pw-typed" }, "mutually exclusive"},
		{"neither password mode", func(r *types.OnboardingRequest) { r.ProvisionPostgres.GeneratePassword = false }, "is required"},
		{"rotate without generate", func(r *types.OnboardingRequest) {
			r.ProvisionPostgres.GeneratePassword = false
			r.ProvisionPostgres.RolePassword = "pw-typed"
			r.ProvisionPostgres.RotatePassword = true
		}, "need generate_password"},
		{"app role of another project", func(r *types.OnboardingRequest) { r.ProvisionAppRole.RoleName = "janua" }, "must start with"},
		{"app role equals owner", func(r *types.OnboardingRequest) {
			r.ProvisionPostgres.RoleName = "fabrication_prep_owner"
			r.ProvisionAppRole.RoleName = "fabrication_prep_owner"
		}, "owner role"},
		{"reserved role", func(r *types.OnboardingRequest) {
			r.ProvisionPostgres.DatabaseName = "pg"
			r.ProvisionAppRole.RoleName = "pg_app"
		}, "reserved"},
		{"connection limit too high", func(r *types.OnboardingRequest) { r.ProvisionAppRole.ConnectionLimit = 50 }, "out of range"},
		{"app role without database", func(r *types.OnboardingRequest) { r.ProvisionPostgres = nil }, "database_name is required"},
		{"bytes too small", func(r *types.OnboardingRequest) { r.GenerateSecrets[0].Bytes = 8 }, "out of range"},
		{"lower-case key", func(r *types.OnboardingRequest) { r.GenerateSecrets[0].Key = "artifact" }, "invalid"},
		{"duplicate key", func(r *types.OnboardingRequest) { r.GenerateSecrets[0].Key = "APP_DATABASE_URL" }, "written by both"},
		{"R2 key", func(r *types.OnboardingRequest) { r.GenerateSecrets[0].Key = "R2_ACCESS_KEY_ID" }, "written by both"},
		{"secrets file shadows generated", func(r *types.OnboardingRequest) {
			r.ProvisionSecrets = []types.SecretEntry{{Key: "DATABASE_URL", Value: "x"}}
		}, "collides"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base()
			tt.mutate(r)
			err := validateGeneratedCredentialRequest(r)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}
