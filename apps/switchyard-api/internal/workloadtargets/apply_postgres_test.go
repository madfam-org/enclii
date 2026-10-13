//go:build workload_target_postgres

package workloadtargets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNativeTargetWriterLockAuditAndIdempotency(t *testing.T) {
	owner := nativeConnection(t, "ENCLII_TARGET_OWNER_PROOF_URL", "binding_owner")
	defer owner.Close()
	if err := db.Migrate(owner, os.Getenv("ENCLII_TARGET_OWNER_PROOF_URL")); err != nil {
		t.Fatal("fixed migration chain failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshot := targetFixture()
	snapshot.Project.ID = uuid.New()
	snapshot.Project.Slug = "writer-fixture"
	snapshot.Namespace.Name = "writer-fixture"
	snapshot.Namespace.UID = "namespace:writer"
	snapshot.Deployment.Namespace = "writer-fixture"
	snapshot.Deployment.UID = "deployment:writer"
	snapshot.Deployment.Annotations["argocd.argoproj.io/tracking-id"] = "fixture-services:apps/Deployment:writer-fixture/fixture-web"
	snapshot.Declared = snapshot.Deployment.DeepCopy()
	snapshot.Environment.KubeNamespace = "writer-fixture"
	snapshot.Registration.ConfigSnapshot["namespace"] = "writer-fixture"
	snapshot.Service.GitRepo = "https://github.com/madfam-org/writer-fixture"
	snapshot.Registration.RepoFullName = "madfam-org/writer-fixture"
	_ = unstructured.SetNestedField(snapshot.Application.Object, "writer-fixture", "spec", "destination", "namespace")
	_ = unstructured.SetNestedField(snapshot.Application.Object, snapshot.Service.GitRepo, "spec", "source", "repoURL")
	source, _, _ := unstructured.NestedMap(snapshot.Application.Object, "spec", "source")
	_ = unstructured.SetNestedMap(snapshot.Application.Object, source, "status", "sync", "comparedTo", "source")
	rows, _, _ := unstructured.NestedSlice(snapshot.Application.Object, "status", "resources")
	rows[0].(map[string]interface{})["namespace"] = "writer-fixture"
	_ = unstructured.SetNestedSlice(snapshot.Application.Object, rows, "status", "resources")
	snapshot.Service.ProjectID = snapshot.Project.ID
	snapshot.Environment.ProjectID = snapshot.Project.ID
	snapshot.Registration.ProjectID = snapshot.Project.ID
	actor, tenant := uuid.New(), uuid.New()
	for _, q := range []struct {
		query string
		args  []interface{}
	}{
		{`INSERT INTO projects(id,name,slug) VALUES($1,'writer fixture','writer-fixture')`, []interface{}{snapshot.Project.ID}},
		{`INSERT INTO services(id,project_id,name,git_repo) VALUES($1,$2,'fixture','https://github.com/madfam-org/writer-fixture')`, []interface{}{snapshot.Service.ID, snapshot.Project.ID}},
		{`INSERT INTO environments(id,project_id,name,kube_namespace) VALUES($1,$2,'production','writer-fixture')`, []interface{}{snapshot.Environment.ID, snapshot.Project.ID}},
		{`INSERT INTO onboarding_registrations(id,project_id,repo_full_name,onboard_status,config_snapshot) VALUES($1,$2,'madfam-org/writer-fixture','completed','{"namespace":"writer-fixture","manifest_path":"infra/k8s/production","branch":"main"}')`, []interface{}{snapshot.Registration.ID, snapshot.Project.ID}},
		{`INSERT INTO users(id,name,email,role,is_platform_admin) VALUES($1,'fixture operator','operator@fixture.invalid','admin',true),($2,'fixture tenant','tenant@fixture.invalid','admin',false)`, []interface{}{actor, tenant}},
	} {
		if _, err := owner.ExecContext(ctx, q.query, q.args...); err != nil {
			t.Fatal("native writer FK fixture failed")
		}
	}
	plan, err := ValidateSnapshot(snapshot, "writer-fixture", "fixture-web")
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Project: "writer-fixture", Environment: "production", Namespace: "writer-fixture", Deployment: "fixture-web", ServiceID: snapshot.Service.ID, ExpectedPlan: plan.SHA, Reason: "native fixed apply proof", IdempotencyID: uuid.New()}
	store := Store{DB: owner}
	var receipt *Receipt
	t.Run("legacy-operation-serializes-before-new-binding", func(t *testing.T) {
		legacy, err := store.LockService(ctx, snapshot.Service.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err = legacy.RefuseBound(ctx); err != nil {
			legacy.Close()
			t.Fatal(err)
		}
		acquired := make(chan *Session, 1)
		failed := make(chan error, 1)
		go func() {
			session, err := store.LockService(ctx, snapshot.Service.ID)
			if err != nil {
				failed <- err
				return
			}
			acquired <- session
		}()
		// Observe the actual queued advisory lock, rather than infer exclusion from
		// elapsed sleep or a missing channel response.
		deadline := time.Now().Add(5 * time.Second)
		for {
			var blocked bool
			err = owner.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)`).Scan(&blocked)
			if err != nil {
				legacy.Close()
				t.Fatal("native lock metadata unavailable")
			}
			if blocked {
				break
			}
			if time.Now().After(deadline) {
				legacy.Close()
				t.Fatal("actual queued service lock not observed")
			}
			time.Sleep(10 * time.Millisecond)
		}
		select {
		case session := <-acquired:
			session.Close()
			legacy.Close()
			t.Fatal("apply crossed the legacy lock")
		default:
		}
		legacy.Close()
		var writer *Session
		select {
		case writer = <-acquired:
		case err = <-failed:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal("writer did not acquire released lock")
		}
		defer writer.Close()
		if err = writer.LockIntent(ctx, snapshot.Environment.ID, snapshot.Registration.ID); err != nil {
			t.Fatal(err)
		}
		receipt, err = writer.Persist(ctx, request, actor, plan)
		if err != nil {
			t.Fatal(err)
		}
		if err = writer.Commit(); err != nil {
			t.Fatal(err)
		}
		session, err := store.LockService(ctx, snapshot.Service.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if !errors.Is(session.RefuseBound(ctx), ErrConflict) {
			t.Fatal("formerly unbound queued writer could bypass new binding")
		}
	})
	t.Run("immutable-binding-and-actor-audit-one-transaction", func(t *testing.T) {
		stored, err := store.Get(ctx, snapshot.Service.ID, snapshot.Environment.ID)
		if err != nil || stored == nil || *stored != receipt.Binding {
			t.Fatal("native binding differs")
		}
		event, err := store.Receipt(ctx, request.IdempotencyID)
		if err != nil || event == nil || event.ActorID != actor || event.Reason != request.Reason || event.Binding != receipt.Binding {
			t.Fatal("native receipt differs")
		}
		for _, q := range []string{`UPDATE service_deployment_target_events SET reason='different'`, `DELETE FROM service_deployment_target_events`, `TRUNCATE service_deployment_target_events`} {
			_, err = owner.ExecContext(ctx, q)
			nativeState(t, err, "P0001")
		}
	})
	t.Run("same-UUID-has-exactly-one-event", func(t *testing.T) {
		session, err := store.LockService(ctx, snapshot.Service.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		repeated, err := session.Persist(ctx, request, actor, plan)
		if err != nil || repeated == nil || *repeated != *receipt {
			t.Fatal("exact idempotent receipt missing")
		}
		var count int
		if err = owner.QueryRowContext(ctx, `SELECT count(*) FROM service_deployment_target_events WHERE id=$1`, request.IdempotencyID).Scan(&count); err != nil || count != 1 {
			t.Fatal("idempotency wrote another audit")
		}
		changed := request
		changed.Reason = "different reason"
		if _, err = session.Persist(ctx, changed, actor, plan); !errors.Is(err, ErrConflict) {
			t.Fatal("same idempotency UUID accepted changed input")
		}
	})
	t.Run("tenant-role-cannot-apply", func(t *testing.T) {
		session, err := store.LockService(ctx, snapshot.Service.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if _, err = session.Persist(ctx, request, tenant, plan); !errors.Is(err, ErrOperator) {
			t.Fatal("tenant admin passed unconditional SQL writer guard")
		}
	})
	t.Run("fresh-review-mismatch-cannot-write", func(t *testing.T) {
		session, err := store.LockService(ctx, snapshot.Service.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		changed := plan
		changed.Versions.ArgoConfig = "changed"
		if _, err = session.Persist(ctx, request, actor, changed); !errors.Is(err, ErrConflict) {
			t.Fatal("changed config RV was applied")
		}
	})
	t.Run("audit-failure-rolls-back-binding", func(t *testing.T) {
		// Use a second independent service/environment, never clear immutable data.
		second := uuid.New()
		env := uuid.New()
		if _, err := owner.ExecContext(ctx, `INSERT INTO services(id,project_id,name,git_repo) VALUES($1,$2,'second','https://github.com/madfam-org/fixture');`, second, snapshot.Project.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ExecContext(ctx, `INSERT INTO environments(id,project_id,name,kube_namespace) VALUES($1,$2,'second','second')`, env, snapshot.Project.ID); err != nil {
			t.Fatal(err)
		}
		otherPlan := plan
		otherPlan.Identity.ServiceID = second
		otherPlan.Identity.EnvironmentID = env
		otherPlan.Identity.NamespaceUID = "namespace:second"
		otherPlan.Identity.DeploymentUID = "deployment:second"
		otherPlan.SHA, _ = otherPlan.Fingerprint()
		other := request
		other.ServiceID = second
		other.ExpectedPlan = otherPlan.SHA
		other.IdempotencyID = uuid.New()
		// An owner-only synthetic trigger forces the second statement to fail.
		if _, err := owner.ExecContext(ctx, `CREATE FUNCTION fixture_reject_target_event() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'fixture audit refusal'; END;$$; CREATE TRIGGER fixture_target_event_refusal BEFORE INSERT ON service_deployment_target_events FOR EACH ROW EXECUTE FUNCTION fixture_reject_target_event()`); err != nil {
			t.Fatal(err)
		}
		session, err := store.LockService(ctx, second)
		if err != nil {
			t.Fatal(err)
		}
		_, err = session.Persist(ctx, other, actor, otherPlan)
		session.Close()
		if !errors.Is(err, ErrConflict) {
			t.Fatal("audit refusal was not reported")
		}
		got, err := store.Get(ctx, second, env)
		if err != nil || got != nil {
			t.Fatal("binding survived an uncommitted audit failure")
		}
		if _, err = owner.ExecContext(ctx, `DROP TRIGGER fixture_target_event_refusal ON service_deployment_target_events; DROP FUNCTION fixture_reject_target_event()`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("row-lock-prevents-config-change-and-delete", func(t *testing.T) {
		session, err := store.LockService(ctx, snapshot.Service.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		other, err := owner.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer other.Rollback()
		if _, err = other.ExecContext(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
			t.Fatal(err)
		}
		_, err = other.ExecContext(ctx, `UPDATE services SET git_repo='https://github.com/madfam-org/other' WHERE id=$1`, snapshot.Service.ID)
		nativeState(t, err, "55P03")
	})
	t.Run("project-retirement-refuses-before-cascade", func(t *testing.T) {
		session, err := store.LockProject(ctx, snapshot.Project.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if !errors.Is(session.RefuseBound(ctx), ErrConflict) {
			t.Fatal("bound project retirement allowed")
		}
	})
	t.Run("canceled-caller-cannot-release-lock-before-native-classification", func(t *testing.T) {
		service := uuid.New()
		if _, err := owner.ExecContext(ctx, `INSERT INTO services(id,project_id,name,git_repo) VALUES($1,$2,'cancellation-fixture','https://github.com/madfam-org/fixture')`, service, snapshot.Project.ID); err != nil {
			t.Fatal(err)
		}
		caller, cancelCaller := context.WithCancel(ctx)
		defer cancelCaller()
		entered, releaseNative := make(chan struct{}), make(chan struct{})
		result := make(chan error, 1)
		go func() {
			result <- store.WithUnbound(caller, service, func(_ *sql.Tx) error { close(entered); <-releaseNative; return nil })
		}()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("native callback did not start")
		}
		cancelCaller()
		acquired, failed := make(chan *Session, 1), make(chan error, 1)
		go func() {
			session, err := store.LockService(ctx, service)
			if err != nil {
				failed <- err
			} else {
				acquired <- session
			}
		}()
		deadline := time.Now().Add(3 * time.Second)
		for {
			select {
			case session := <-acquired:
				session.Close()
				close(releaseNative)
				<-result
				t.Fatal("caller cancellation released the lock while native classification remained pending")
			default:
			}
			var blocked bool
			if owner.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)`).Scan(&blocked) != nil {
				close(releaseNative)
				t.Fatal("actual native waiting lock unavailable")
			}
			if blocked {
				break
			}
			if time.Now().After(deadline) {
				close(releaseNative)
				t.Fatal("queued apply lock not observed during cancellation")
			}
			time.Sleep(10 * time.Millisecond)
		}
		close(releaseNative)
		select {
		case err := <-result:
			if err != nil {
				t.Fatal("completed callback lost its explicitly owned transaction lifetime")
			}
		case <-ctx.Done():
			t.Fatal("callback completion missing")
		}
		select {
		case session := <-acquired:
			session.Close()
		case err := <-failed:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal("lock not released after callback classification")
		}
	})

	t.Run("group-no-key-lock-allows-FK-insert-but-blocks-binding", func(t *testing.T) {
		first, second := uuid.New(), uuid.New()
		for _, id := range []uuid.UUID{first, second} {
			if _, err := owner.ExecContext(ctx, `INSERT INTO services(id,project_id,name,git_repo) VALUES($1,$2,$3,'https://github.com/madfam-org/fixture')`, id, snapshot.Project.ID, "fixture-"+id.String()); err != nil {
				t.Fatal(err)
			}
		}
		group, err := store.LockServices(ctx, []uuid.UUID{second, first})
		if err != nil {
			t.Fatal(err)
		}
		defer group.Close()
		if group.RefuseBound(ctx) != nil || !group.OwnsService(first) || !group.OwnsService(second) || group.OwnsService(snapshot.Service.ID) {
			t.Fatal("affected set did not preserve unbound children alongside another bound project member")
		}
		metadata, err := owner.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer metadata.Rollback()
		if _, err = metadata.ExecContext(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
			t.Fatal(err)
		}
		release := uuid.New()
		if _, err = metadata.ExecContext(ctx, `INSERT INTO releases(id,service_id,version,image_uri,git_sha,status) VALUES($1,$2,'1','fixture','fixture','ready')`, release, first); err != nil {
			t.Fatal("ordinary release FK could not pass held NO KEY UPDATE")
		}
		if _, err = metadata.ExecContext(ctx, `INSERT INTO deployments(release_id,environment_id,service_id,replicas,status,health) VALUES($1,$2,$3,1,'pending','unknown')`, release, snapshot.Environment.ID, first); err != nil {
			t.Fatal("ordinary deployment FK could not pass held NO KEY UPDATE")
		}
		if err = metadata.Commit(); err != nil {
			t.Fatal(err)
		}
		acquired, failed := make(chan *Session, 1), make(chan error, 1)
		go func() {
			session, err := store.LockService(ctx, first)
			if err != nil {
				failed <- err
			} else {
				acquired <- session
			}
		}()
		deadline := time.Now().Add(3 * time.Second)
		for {
			var blocked bool
			if owner.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)`).Scan(&blocked) != nil {
				t.Fatal("waiting native lock unavailable")
			}
			if blocked {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("binding did not wait behind group callback")
			}
			time.Sleep(10 * time.Millisecond)
		}
		select {
		case session := <-acquired:
			session.Close()
			t.Fatal("binding crossed held group lock")
		default:
		}
		group.Close()
		select {
		case session := <-acquired:
			session.Close()
		case err := <-failed:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal("group completion did not release the binding lock")
		}
	})

	raw, _ := json.Marshal(receipt)
	if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "token") {
		t.Fatal("receipt contains custody")
	}
}
