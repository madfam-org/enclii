//go:build workload_target_postgres

package workloadtargets

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/stagingintents"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Uses the existing explicit owned loopback PG18/TLS fixture, no skipped body
// and no credential grant. The CI proof runner's ^TestNativeTarget filter includes
// this test automatically, alongside the original native binding proofs.
func TestNativeTargetHeldStagingIntentIsImmutableInertAndIdempotent(t *testing.T) {
	owner := nativeConnection(t, "ENCLII_TARGET_OWNER_PROOF_URL", "binding_owner")
	if db.Migrate(owner, os.Getenv("ENCLII_TARGET_OWNER_PROOF_URL")) != nil {
		t.Fatal("fresh native migration chain failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	project, environment, registration, actor := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO projects(id,name,slug) VALUES($1,'synthetic staging fixture','janua')`, []any{project}},
		{`INSERT INTO environments(id,project_id,name,kube_namespace) VALUES($1,$2,'staging','janua-staging')`, []any{environment, project}},
		{`INSERT INTO onboarding_registrations(id,project_id,repo_full_name,argocd_app_name,onboard_status,config_snapshot) VALUES($1,$2,'madfam-org/janua','production-fixture','completed','{"namespace":"production-fixture","manifest_path":"production-fixture"}')`, []any{registration, project}},
		{`INSERT INTO users(id,name,email,role,is_platform_admin) VALUES($1,'synthetic intent operator','intent-operator@fixture.invalid','admin',true)`, []any{actor}},
		{`INSERT INTO services(id,project_id,name,git_repo,k8s_namespace) VALUES($1,$2,'janua-api','https://github.com/madfam-org/janua.git','production-fixture')`, []any{uuid.New(), project}},
	} {
		if _, err := owner.ExecContext(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal("native held-intent fixture preparation failed")
		}
	}
	store := stagingintents.Store{DB: owner, ProjectID: project}
	first, second := uuid.New(), uuid.New()
	plans := make([]types.JanuaStagingIntent, 2)
	for i, id := range []uuid.UUID{first, second} {
		var err error
		plans[i], err = store.Plan(ctx, actor, id)
		if err != nil {
			t.Fatal("native held plan unavailable")
		}
	}
	for _, mutation := range []string{"activation", "missing-gate", "source-scope"} {
		candidate := plans[0]
		recorded := true
		candidate.Recorded = &recorded
		candidate.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		candidate.ID = uuid.New()
		raw, _ := json.Marshal(candidate)
		var object map[string]any
		_ = json.Unmarshal(raw, &object)
		switch mutation {
		case "activation":
			object["activation_held"] = false
		case "missing-gate":
			object["unresolved_gates"] = candidate.Gates[1:]
		case "source-scope":
			object["repository_registration_scope"] = "STAGING_SOURCE_VERIFIED"
		}
		raw, _ = json.Marshal(object)
		_, err := owner.ExecContext(ctx, `INSERT INTO janua_staging_preregistration_intents(id,project_id,environment_id,repository_registration_id,actor_id,plan_sha256,request_sha256,reason,payload) VALUES($1,$2,$3,$4,$5,$6,$6,'native negative proof',$7)`, candidate.ID, project, environment, registration, actor, candidate.PlanSHA256, raw)
		nativeState(t, err, "23514")
	}
	if _, err := store.Record(ctx, actor, first, plans[1].PlanSHA256, "native bounded reason"); !errors.Is(err, stagingintents.ErrRefused) {
		t.Fatal("foreign plan was accepted")
	}
	type outcome struct {
		receipt types.JanuaStagingIntent
		err     error
	}
	results := make(chan outcome, 2)
	var workers sync.WaitGroup
	for _, p := range plans {
		workers.Add(1)
		go func(p types.JanuaStagingIntent) {
			defer workers.Done()
			r, e := store.Record(ctx, actor, p.ID, p.PlanSHA256, "native bounded reason")
			results <- outcome{r, e}
		}(p)
	}
	workers.Wait()
	close(results)
	var winner types.JanuaStagingIntent
	successes, refusals := 0, 0
	for result := range results {
		if result.err == nil {
			successes++
			winner = result.receipt
		} else if errors.Is(result.err, stagingintents.ErrRefused) {
			refusals++
		} else {
			t.Fatal("native record outcome unclassified")
		}
	}
	if successes != 1 || refusals != 1 {
		t.Fatal("concurrent fixed-slot uniqueness failed")
	}
	for i := 0; i < 2; i++ {
		r, err := store.Record(ctx, actor, winner.ID, winner.PlanSHA256, "native bounded reason")
		if err != nil || r.RecordedAt != winner.RecordedAt {
			t.Fatal("exact replay changed immutable history")
		}
	}
	if _, err := store.Record(ctx, actor, winner.ID, winner.PlanSHA256, "changed reason"); !errors.Is(err, stagingintents.ErrRefused) {
		t.Fatal("changed replay accepted")
	}
	got, err := store.Read(ctx, winner.ID)
	if err != nil || types.ValidateJanuaStagingIntent(got, winner.ID) != nil || *got.ServiceMaterialized || *got.NativeBindingProven || !*got.ActivationHeld {
		t.Fatal("native historical intent lost held boundary")
	}
	var intentCount, serviceCount int
	if owner.QueryRowContext(ctx, `SELECT count(*) FROM janua_staging_preregistration_intents WHERE project_id=$1`, project).Scan(&intentCount) != nil || intentCount != 1 {
		t.Fatal("native intent count differs")
	}
	if owner.QueryRowContext(ctx, `SELECT count(*) FROM services WHERE project_id=$1`, project).Scan(&serviceCount) != nil || serviceCount != 1 {
		t.Fatal("recording materialized a service")
	}
	var namespace string
	if owner.QueryRowContext(ctx, `SELECT k8s_namespace FROM services WHERE project_id=$1 AND name='janua-api'`, project).Scan(&namespace) != nil || namespace != "production-fixture" {
		t.Fatal("production registration was retargeted")
	}
	for _, statement := range []string{`UPDATE janua_staging_preregistration_intents SET reason='changed'`, `DELETE FROM janua_staging_preregistration_intents`, `TRUNCATE janua_staging_preregistration_intents`} {
		_, err := owner.ExecContext(ctx, statement)
		nativeState(t, err, "P0001")
	}
	runtime := nativeConnection(t, "ENCLII_TARGET_RUNTIME_PROOF_URL", "binding_runtime")
	_, err = runtime.ExecContext(ctx, `INSERT INTO janua_staging_preregistration_intents(id) VALUES($1)`, uuid.New())
	nativeState(t, err, "42501")
	for _, statement := range []struct {
		sql string
		id  uuid.UUID
	}{{`DELETE FROM environments WHERE id=$1`, environment}, {`DELETE FROM onboarding_registrations WHERE id=$1`, registration}, {`DELETE FROM users WHERE id=$1`, actor}} {
		_, err := owner.ExecContext(ctx, statement.sql, statement.id)
		nativeState(t, err, "23503")
	}
	down, err := os.ReadFile("../db/migrations/045_janua_staging_preregistration.down.sql")
	if err != nil {
		t.Fatal("migration rollback guard absent")
	}
	_, err = owner.ExecContext(ctx, string(down))
	nativeState(t, err, "P0001")
}
