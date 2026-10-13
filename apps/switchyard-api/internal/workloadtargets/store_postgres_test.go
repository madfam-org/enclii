//go:build workload_target_postgres

package workloadtargets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// This explicit proof tag has no skip: an owned loopback *_sim fixture and
// strict verified TLS are mandatory. It never reads platform/addon credentials.
func nativeConnection(t *testing.T, key, role string) *sql.DB {
	t.Helper()
	raw := os.Getenv(key)
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.User == nil || u.Hostname() != "127.0.0.1" || u.User.Username() != role ||
		!strings.HasSuffix(u.Path, "_sim") || u.Query().Get("sslmode") != "verify-full" || u.Query().Get("sslrootcert") == "" {
		t.Fatal("explicit owned loopback synthetic PostgreSQL fixture with verified TLS required")
	}
	connection, err := sql.Open("postgres", raw)
	if err != nil {
		t.Fatal("owned native connection unavailable")
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

func nativeState(t *testing.T, err error, expected string) {
	t.Helper()
	var failure *pq.Error
	if !errors.As(err, &failure) || string(failure.Code) != expected {
		observed := "not-a-postgres-error"
		if failure != nil {
			observed = string(failure.Code)
		}
		if err == nil {
			observed = "statement-succeeded"
		}
		t.Fatalf("expected native SQLSTATE %s; observed %s", expected, observed)
	}
}

func TestNativeTargetStoreMigrationAndConstraints(t *testing.T) {
	owner := nativeConnection(t, "ENCLII_TARGET_OWNER_PROOF_URL", "binding_owner")
	runtime := nativeConnection(t, "ENCLII_TARGET_RUNTIME_PROOF_URL", "binding_runtime")
	for index, connection := range []*sql.DB{owner, runtime} {
		var secure, privileged bool
		var ownsDatabase bool
		var memberships int
		err := connection.QueryRow(`SELECT (SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()),
 r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication,
 (SELECT count(*) FROM pg_auth_members WHERE member=r.oid),
 (SELECT datdba=r.oid FROM pg_database WHERE datname=current_database())
 FROM pg_roles r WHERE rolname=current_user`).Scan(&secure, &privileged, &memberships, &ownsDatabase)
		if err != nil || !secure || privileged || memberships != 0 || ownsDatabase != (index == 0) {
			t.Fatal("native role/TLS custody proof failed")
		}
	}
	var engineVersion int
	if owner.QueryRow(`SELECT current_setting('server_version_num')::int`).Scan(&engineVersion) != nil || engineVersion/10000 != 18 {
		t.Fatal("explicit PostgreSQL18 native fixture required")
	}
	t.Logf("native engine_version_num=%d; verified TLS and safe separate owner/runtime roles", engineVersion)
	if err := db.Migrate(owner, os.Getenv("ENCLII_TARGET_OWNER_PROOF_URL")); err != nil {
		t.Fatal("fresh full native migration chain failed")
	}
	s := targetFixture()
	p, err := ValidateSnapshot(s, "fixture", "fixture-web")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		query string
		args  []interface{}
	}{
		{`INSERT INTO projects(id,name,slug) VALUES($1,'fixture','fixture')`, []interface{}{s.Project.ID}},
		{`INSERT INTO services(id,project_id,name,git_repo) VALUES($1,$2,'fixture','https://github.com/madfam-org/fixture')`, []interface{}{s.Service.ID, s.Project.ID}},
		{`INSERT INTO environments(id,project_id,name,kube_namespace) VALUES($1,$2,'production','fixture')`, []interface{}{s.Environment.ID, s.Project.ID}},
		{`INSERT INTO onboarding_registrations(id,project_id,repo_full_name,argocd_app_name,onboard_status,config_snapshot) VALUES($1,$2,'madfam-org/fixture','fixture-services','completed','{"namespace":"fixture"}')`, []interface{}{s.Registration.ID, s.Project.ID}},
	} {
		if _, err = owner.Exec(statement.query, statement.args...); err != nil {
			t.Fatal("native FK fixture preparation failed")
		}
	}
	b := Binding{ID: uuid.New(), Identity: p.Identity, PlanSHA: p.SHA, Versions: p.Versions}
	insert := func(binding Binding, raw []byte) error {
		if raw == nil {
			raw, _ = json.Marshal(binding)
		}
		_, err := owner.Exec(`INSERT INTO service_deployment_targets(id,project_id,service_id,environment_id,registration_id,namespace_uid,deployment_uid,actor_id,reason,payload) VALUES($1,$2,$3,$4,$5,$6,$7,'fixture-operator','native source proof',$8)`,
			binding.ID, binding.Identity.ProjectID, binding.Identity.ServiceID, binding.Identity.EnvironmentID, binding.Identity.RegistrationID, binding.Identity.NamespaceUID, binding.Identity.DeploymentUID, raw)
		return err
	}
	if err = insert(b, nil); err != nil {
		t.Fatal("native immutable binding insert fixture failed")
	}
	if _, err = owner.Exec(`GRANT USAGE ON SCHEMA public TO binding_runtime; GRANT SELECT ON service_deployment_targets,environments,onboarding_registrations TO binding_runtime`); err != nil {
		t.Fatal("native read-only fixture grant failed")
	}
	t.Run("actual-owner-and-runtime-store", func(t *testing.T) {
		for _, connection := range []*sql.DB{owner, runtime} {
			got, err := (Store{DB: connection}).Get(context.Background(), s.Service.ID, s.Environment.ID)
			if err != nil || got == nil || *got != b {
				t.Fatal("actual native store differs from immutable binding")
			}
			verified, err := (Store{DB: connection}).NamespaceIntentExclusive(context.Background(), s.Project.ID, "fixture")
			if err != nil || !verified {
				t.Fatal("actual native claim read failed")
			}
		}
	})
	t.Run("immutable-update-delete-truncate", func(t *testing.T) {
		for _, statement := range []string{`UPDATE service_deployment_targets SET reason='different'`, `DELETE FROM service_deployment_targets`} {
			_, err := owner.Exec(statement)
			nativeState(t, err, "P0001")
		}
	})
	t.Run("parent-and-combined-truncate-refused", func(t *testing.T) {
		_, err := owner.Exec(`TRUNCATE service_deployment_targets`)
		nativeState(t, err, "0A000")
		_, err = owner.Exec(`TRUNCATE service_deployment_target_events,service_deployment_targets`)
		nativeState(t, err, "P0001")
	})
	t.Run("runtime-cannot-write-or-create", func(t *testing.T) {
		for _, statement := range []string{`INSERT INTO service_deployment_targets(id) VALUES(gen_random_uuid())`, `UPDATE service_deployment_targets SET reason='different'`, `DELETE FROM service_deployment_targets`, `CREATE TABLE public.unauthorized_fixture(id int)`} {
			_, err := runtime.Exec(statement)
			nativeState(t, err, "42501")
		}
	})
	t.Run("unique-service-environment", func(t *testing.T) { copy := b; copy.ID = uuid.New(); nativeState(t, insert(copy, nil), "23505") })
	t.Run("unique-actual-target-across-services", func(t *testing.T) {
		other := uuid.New()
		if _, err := owner.Exec(`INSERT INTO services(id,project_id,name,git_repo) VALUES($1,$2,'other','https://github.com/madfam-org/fixture')`, other, s.Project.ID); err != nil {
			t.Fatal("second synthetic service failed")
		}
		copy := b
		copy.ID = uuid.New()
		copy.Identity.ServiceID = other
		nativeState(t, insert(copy, nil), "23505")
	})
	t.Run("foreign-key-retirement-refused", func(t *testing.T) {
		for _, row := range []struct {
			table string
			id    uuid.UUID
		}{{"services", s.Service.ID}, {"environments", s.Environment.ID}, {"onboarding_registrations", s.Registration.ID}, {"projects", s.Project.ID}} {
			t.Run(row.table, func(t *testing.T) {
				_, err := owner.Exec(`DELETE FROM `+row.table+` WHERE id=$1`, row.id)
				var failure *pq.Error
				if !errors.As(err, &failure) || (string(failure.Code) != "23503" && string(failure.Code) != "23001") ||
					(row.table != "projects" && !strings.HasPrefix(failure.Constraint, "service_deployment_targets_")) {
					t.Fatal("retirement was not refused by an explicit target foreign key")
				}
				if row.table == "projects" {
					// Legacy project children can reject first. Independently prove
					// the new target's own project FK is present and RESTRICT.
					var action string
					if owner.QueryRow(`SELECT confdeltype FROM pg_constraint WHERE conrelid='service_deployment_targets'::regclass AND conname='service_deployment_targets_project_id_fkey'`).Scan(&action) != nil || action != "r" {
						t.Fatal("native target project RESTRICT missing")
					}
				}
			})
		}
	})
	t.Run("missing-json-identity-is-not-SQL-null-pass", func(t *testing.T) {
		copy := b
		copy.ID = uuid.New()
		raw, _ := json.Marshal(map[string]interface{}{"id": copy.ID, "identity": map[string]interface{}{"service_id": s.Service.ID}})
		nativeState(t, insert(copy, raw), "23514")
	})
	t.Run("another-persisted-namespace-claim-refused", func(t *testing.T) {
		other := uuid.New()
		if _, err := owner.Exec(`INSERT INTO projects(id,name,slug) VALUES($1,'other','other')`, other); err != nil {
			t.Fatal("second synthetic project failed")
		}
		if _, err := owner.Exec(`INSERT INTO environments(project_id,name,kube_namespace) VALUES($1,'production','fixture')`, other); err != nil {
			t.Fatal("conflicting namespace fixture failed")
		}
		verified, err := (Store{DB: runtime}).NamespaceIntentExclusive(context.Background(), s.Project.ID, "fixture")
		if err != nil || verified {
			t.Fatal("foreign native claim adopted")
		}
	})
	t.Run("down-refuses-retained-history", func(t *testing.T) {
		data, err := os.ReadFile("../db/migrations/043_service_deployment_targets.down.sql")
		if err != nil {
			t.Fatal("fixed migration source unavailable")
		}
		_, err = owner.Exec(string(data))
		nativeState(t, err, "P0001")
	})
	var count int
	if err = owner.QueryRow(`SELECT count(*) FROM service_deployment_targets WHERE service_id=$1`, s.Service.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("native refusal changed immutable history")
	}
}
