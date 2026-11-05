//go:build integration

package db

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The existing CI Postgres boot-smoke job runs all integration-tagged db tests
// with LOCAL_DB=yes and a loopback service-container DSN. No live platform or
// tenant schema may be targeted. freshDatabase retains isolated-create/cleanup.
func TestJanuaReservationCurrentPostgresSchemaAndHeldNativeBirth(t *testing.T) {
	parsed, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	require.NoError(t, err)
	require.Equal(t, "yes", os.Getenv("LOCAL_DB"), "explicit disposable local PostgreSQL guard required")
	require.Contains(t, []string{"127.0.0.1", "localhost", "::1"}, parsed.Hostname(), "fixture must use loopback")
	require.Contains(t, []string{"/postgres", "/enclii_smoke"}, parsed.Path, "fixture admin database is bounded")
	conn, _ := freshDatabase(t)
	entries, err := migrationFS.ReadDir("migrations")
	require.NoError(t, err)
	var latest uint
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			var version uint
			_, e := fmt.Sscanf(entry.Name(), "%d_", &version)
			if e == nil && version > latest {
				latest = version
			}
		}
	}
	require.GreaterOrEqual(t, latest, uint(45))
	require.NoError(t, migrateTo(t, conn, latest))
	project, environment, registration, actor := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mustExec(t, conn, `INSERT INTO users(id,email,name,role,is_platform_admin) VALUES($1,'operator@fixture.invalid','Synthetic operator','admin',true)`, actor)
	mustExec(t, conn, `INSERT INTO projects(id,name,slug) VALUES($1,'Synthetic Janua fixture','janua')`, project)
	mustExec(t, conn, `INSERT INTO environments(id,project_id,name,kube_namespace) VALUES($1,$2,'staging','janua-staging')`, environment, project)
	mustExec(t, conn, `INSERT INTO onboarding_registrations(id,project_id,repo_full_name,onboard_status) VALUES($1,$2,'madfam-org/janua','completed')`, registration, project)
	r := NewRepositories(conn)
	ctx := context.Background()
	// Actor revocation is refused by the transaction, even if a caller had
	// resolved platform rank before the transaction started.
	mustExec(t, conn, `UPDATE users SET is_platform_admin=false WHERE id=$1`, actor)
	_, err = r.ReserveJanuaStaging(ctx, project, environment, registration, uuid.New(), uuid.New(), actor, uuid.New(), "standard-0", "native authority refusal fixture")
	require.Error(t, err)
	var count int
	require.NoError(t, conn.QueryRow(`SELECT COUNT(*) FROM services WHERE project_id=$1`, project).Scan(&count))
	require.Zero(t, count)
	mustExec(t, conn, `UPDATE users SET is_platform_admin=true WHERE id=$1`, actor)
	serviceID, addonID, operation := uuid.New(), uuid.New(), uuid.New()
	addon, err := r.ReserveJanuaStaging(ctx, project, environment, registration, serviceID, addonID, actor, operation, "standard-0", "native held reservation fixture")
	require.NoError(t, err)
	require.Equal(t, addonID, addon.ID)
	require.Equal(t, "janua_staging_runtime", addon.Config.RuntimeRole)
	service, err := r.Services.GetByID(serviceID)
	require.NoError(t, err)
	require.False(t, service.AutoDeploy)
	require.Equal(t, "staging", service.AutoDeployEnv)
	require.Equal(t, "janua-api-staging", service.Name)
	require.NotNil(t, service.K8sNamespace)
	require.Equal(t, "janua-staging", *service.K8sNamespace)
	require.NoError(t, r.RecordJanuaStagingNativeBirth(ctx, project, environment, registration, addonID, actor, operation, "fixture-cluster-uid", "fixture-runtime-uid", "native held birth fixture"))
	var status string
	var provisioned bool
	require.NoError(t, conn.QueryRow(`SELECT status,provisioned_at IS NOT NULL FROM database_addons WHERE id=$1`, addonID).Scan(&status, &provisioned))
	require.Equal(t, "provisioning", status)
	require.False(t, provisioned)
	require.NoError(t, conn.QueryRow(`SELECT COUNT(*) FROM managed_db_addon_events WHERE addon_id=$1 AND event_type='addon.ready'`, addonID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, conn.QueryRow(`SELECT COUNT(*) FROM managed_db_addon_events WHERE addon_id=$1 AND details->>'reason'=$2 AND details->>'held'='true'`, addonID, "native held birth fixture").Scan(&count))
	require.Equal(t, 1, count)
	// A second birth transition cannot rewrite the first event or mark Ready.
	require.Error(t, r.RecordJanuaStagingNativeBirth(ctx, project, environment, registration, addonID, actor, operation, "replacement", "replacement", "attempted metadata replacement"))
	_, err = r.ReserveJanuaStaging(ctx, project, environment, registration, uuid.New(), uuid.New(), actor, uuid.New(), "standard-0", "existing stage must remain held")
	require.Error(t, err)
	require.NoError(t, conn.QueryRow(`SELECT COUNT(*) FROM services WHERE project_id=$1`, project).Scan(&count))
	require.Equal(t, 1, count)
}
