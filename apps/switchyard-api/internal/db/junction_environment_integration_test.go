//go:build integration

package db

// Migration 041 (junctions.environment_id) and JunctionRepository.Rebind,
// against a real Postgres.
//
// Run with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/db/...

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestMigration041JunctionEnvironmentAndRebind(t *testing.T) {
	conn, _ := freshDatabase(t)
	if err := migrateTo(t, conn, 41); err != nil {
		t.Fatalf("migrate to 41: %v", err)
	}
	ctx := context.Background()

	projectID, _, webID := seedService(t, conn, "rebind")
	apiID := uuid.New()
	mustExec(t, conn, `INSERT INTO services (id, project_id, name, git_repo) VALUES ($1, $2, 'rebind-api', 'example/rebind')`,
		apiID, projectID)
	stagingID := uuid.New()
	mustExec(t, conn, `INSERT INTO environments (id, project_id, name, kube_namespace)
		VALUES ($1, $2, 'staging', 'enclii-rebind-staging')`, stagingID, projectID)

	repo := NewJunctionRepository(conn)
	junction := &types.Junction{ProjectID: projectID, ServiceID: webID, Domain: "staging-api.example.com"}
	if err := repo.Create(ctx, junction); err != nil {
		t.Fatalf("create junction: %v", err)
	}

	got, err := repo.GetByID(ctx, junction.ID)
	if err != nil {
		t.Fatalf("read junction: %v", err)
	}
	if got.EnvironmentID != nil {
		t.Fatalf("a junction created without an environment must read back NULL, got %v", *got.EnvironmentID)
	}

	if err := repo.Rebind(ctx, junction.ID, projectID, apiID, &stagingID); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	// Idempotent: the same rebind again succeeds and changes nothing.
	if err := repo.Rebind(ctx, junction.ID, projectID, apiID, &stagingID); err != nil {
		t.Fatalf("repeat rebind: %v", err)
	}
	got, err = repo.GetByID(ctx, junction.ID)
	if err != nil {
		t.Fatalf("read rebound junction: %v", err)
	}
	if got.ServiceID != apiID || got.EnvironmentID == nil || *got.EnvironmentID != stagingID {
		t.Fatalf("rebind did not stick: service=%v environment=%v", got.ServiceID, got.EnvironmentID)
	}

	// Scoped to the project: another project's id matches nothing.
	if err := repo.Rebind(ctx, junction.ID, uuid.New(), webID, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rebind under another project = %v, want sql.ErrNoRows", err)
	}

	// Deleting the environment keeps the junction and clears its environment.
	mustExec(t, conn, `DELETE FROM environments WHERE id = $1`, stagingID)
	got, err = repo.GetByID(ctx, junction.ID)
	if err != nil {
		t.Fatalf("junction must survive its environment's deletion: %v", err)
	}
	if got.EnvironmentID != nil {
		t.Fatalf("environment_id = %v after the environment was deleted, want NULL", *got.EnvironmentID)
	}

	// Down and up again: both directions are idempotent.
	if err := migrateTo(t, conn, 40); err != nil {
		t.Fatalf("migrate down to 40: %v", err)
	}
	if err := migrateTo(t, conn, 41); err != nil {
		t.Fatalf("migrate back up to 41: %v", err)
	}
}
