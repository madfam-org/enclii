//go:build workload_target_postgres

package workloadtargets

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
	"github.com/stretchr/testify/require"
)

// Real049 DDL/SQL methods; synthetic completed source/native records only.
// Included by the existing required PG18/TLS TestNativeTarget filter.
func TestNativeTargetJanuaMigrationJournalPreventsRestartAfterAbortAndPreservesHistory(t *testing.T) {
	db, database, a := nativeJanuaDatabaseFixture(t)
	ctx := context.Background()
	dbJournal := januastagedb.SQLJournal{DB: db}
	require.NoError(t, dbJournal.Claim(ctx, database))
	for _, phase := range []struct{ kind, uid string }{{"RUNTIME_SECRET_CREATED", "runtime-secret"}, {"CLUSTER_CREATED", "cluster"}, {"CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED", "cluster"}} {
		require.NoError(t, dbJournal.Record(ctx, database, phase.kind, phase.uid))
	}
	body, e := os.ReadFile("../db/migrations/049_janua_staging_migration_operations.up.sql")
	require.NoError(t, e)
	_, e = db.ExecContext(ctx, string(body))
	require.NoError(t, e)
	var files []januastagemigrate.SourceFile
	for _, path := range []string{"alembic.ini", "alembic/env.py", "app/config.py", "app/core/database.py", "app/core/database_ssl.py", "app/models/__init__.py"} {
		files = append(files, januastagemigrate.SourceFile{Path: path, Content: []byte("# synthetic source only\n")})
	}
	files = append(files, januastagemigrate.SourceFile{Path: "alembic/versions/000.py", Content: []byte("revision='000'\ndown_revision=None\n")})
	descriptor, e := januastagemigrate.BuildDescriptor(januastagemigrate.ImageRepository+"@sha256:"+strings.Repeat("a", 64), strings.Repeat("b", 40), files)
	require.NoError(t, e)
	source := januastagemigrate.SourceObservation{AuthorityID: a.ID, ManifestSourceRevision: a.SourceRevision, ManifestSourceSHA256: a.SourceSHA256, Descriptor: descriptor, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	p, e := januastagemigrate.BuildPlan(database, uuid.New(), source, januastagedb.NativeReceipt{ClusterUID: "cluster", RuntimeSecretUID: "runtime-secret"}, "registry-fixture", "1", strings.Repeat("c", 64), "synthetic fixed Alembic intent")
	require.NoError(t, e)
	journal := januastagemigrate.SQLJournal{DB: db}
	require.NoError(t, journal.Check(ctx, p))
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; outcomes <- journal.Claim(ctx, p) }()
	}
	close(start)
	wg.Wait()
	close(outcomes)
	successes := 0
	for e := range outcomes {
		if e == nil {
			successes++
		} else {
			require.ErrorIs(t, e, januastagemigrate.ErrRefused)
		}
	}
	require.Equal(t, 1, successes)
	ownerSHA := strings.Repeat("d", 64)
	require.ErrorIs(t, journal.Record(ctx, p, "OWNER_SECRET_CREATED", "owner-secret", ownerSHA), januastagemigrate.ErrRefused)
	require.ErrorIs(t, journal.Record(ctx, p, "SUSPENDED_JOB_CREATED", "job", strings.Repeat("e", 64)), januastagemigrate.ErrRefused)
	require.NoError(t, journal.Record(ctx, p, "SUSPENDED_JOB_CREATED", "job", p.SHA256))
	require.NoError(t, journal.Record(ctx, p, "OWNER_SECRET_CREATED", "owner-secret", ownerSHA))
	native, e := journal.Receipt(ctx, p)
	require.NoError(t, e)
	require.False(t, native.Started)
	require.Equal(t, ownerSHA, native.OwnerSecretSHA256)
	wrong := januastagemigrate.Outcome{State: "ABORTED_BEFORE_START", JobUID: "foreign-job", OwnerSecretUID: "owner-secret", ExitCode: -1}
	require.ErrorIs(t, journal.Outcome(ctx, p, wrong), januastagemigrate.ErrRefused)
	aborted := wrong
	aborted.JobUID = "job"
	require.NoError(t, journal.Outcome(ctx, p, aborted))
	require.NoError(t, journal.Outcome(ctx, p, aborted))
	require.ErrorIs(t, journal.Record(ctx, p, "JOB_STARTED", "job", p.SHA256), januastagemigrate.ErrRefused)
	require.ErrorIs(t, journal.Record(ctx, p, "OWNER_SECRET_REMOVED", "owner-secret", strings.Repeat("e", 64)), januastagemigrate.ErrRefused)
	require.NoError(t, journal.Record(ctx, p, "OWNER_SECRET_REMOVED", "owner-secret", ownerSHA))
	native, e = journal.Receipt(ctx, p)
	require.NoError(t, e)
	require.True(t, native.OwnerRemoved)
	retained, e := journal.ReadOutcome(ctx, p)
	require.NoError(t, e)
	require.Equal(t, &aborted, retained)
	_, e = db.ExecContext(ctx, `UPDATE users SET is_platform_admin=false WHERE id=$1`, p.Database.ActorID)
	require.NoError(t, e)
	require.ErrorIs(t, journal.CheckCleanup(ctx, p), januastagemigrate.ErrRefused)
	for _, statement := range []string{`UPDATE janua_staging_migration_operations SET reason='rewrite'`, `DELETE FROM janua_staging_migration_outcomes`, `TRUNCATE janua_staging_migration_operations CASCADE`} {
		_, e = db.ExecContext(ctx, statement)
		nativeState(t, e, "P0001")
	}
	down, e := os.ReadFile("../db/migrations/049_janua_staging_migration_operations.down.sql")
	require.NoError(t, e)
	_, e = db.ExecContext(ctx, string(down))
	nativeState(t, e, "P0001")
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM janua_staging_migration_operations`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM services`).Scan(&count))
	require.Zero(t, count)
}
