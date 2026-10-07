//go:build integration

package jobholds

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// This suite intentionally accepts only an explicitly authorized loopback test
// database. It creates its own schema and never uses a production/default URL.
func TestJobHoldMigrationAndSerialization(t *testing.T) {
	raw := os.Getenv("JOB_HOLD_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("JOB_HOLD_TEST_DATABASE_URL absent: use a disposable local PostgreSQL instance")
	}
	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, "yes", os.Getenv("LOCAL_DB"))
	require.Contains(t, []string{"127.0.0.1", "localhost", "::1"}, u.Hostname())
	database, err := sql.Open("postgres", raw)
	require.NoError(t, err)
	defer database.Close()
	schema := "job_holds_" + uuid.New().String()[:8]
	_, err = database.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer func() { _, _ = database.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) }()
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	scoped, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer scoped.Close()
	scoped.SetMaxOpenConns(5)
	_, err = scoped.Exec(`CREATE TABLE projects (id uuid PRIMARY KEY, slug text);
 CREATE TABLE services (id uuid PRIMARY KEY, project_id uuid REFERENCES projects(id), name text);
 CREATE TABLE environments (id uuid PRIMARY KEY, project_id uuid REFERENCES projects(id), kube_namespace text)`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../db/migrations/042_service_job_holds.up.sql")
	require.NoError(t, err)
	_, err = scoped.Exec(string(migration))
	require.NoError(t, err)
	b := Binding{ProjectID: uuid.New(), ServiceID: uuid.New(), EnvironmentID: uuid.New(), ServiceName: "fixture-api"}
	_, err = scoped.Exec(`INSERT INTO projects VALUES ($1,'fixture')`, b.ProjectID)
	require.NoError(t, err)
	_, err = scoped.Exec(`INSERT INTO services VALUES ($1,$2,$3)`, b.ServiceID, b.ProjectID, b.ServiceName)
	require.NoError(t, err)
	_, err = scoped.Exec(`INSERT INTO environments VALUES ($1,$2,'fixture-dev')`, b.EnvironmentID, b.ProjectID)
	require.NoError(t, err)
	store := &Store{DB: scoped}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, store.WithLock(ctx, "fixture-dev", "nightly", func(ctx context.Context, s *Session) error {
		resolved, err := s.Resolve(ctx, "fixture", b.ServiceID, "fixture-dev")
		require.NoError(t, err)
		require.Equal(t, b, resolved)
		_, err = s.Resolve(ctx, "fixture", b.ServiceID, "fixture-prod")
		require.ErrorIs(t, err, sql.ErrNoRows)
		return s.Put(ctx, "fixture-dev", "nightly", Hold{Binding: b, Reason: "fixture containment", Actor: "fixture-operator", UID: "uid-1", ResourceVersion: "42"})
	}))
	// The committed hold remains visible in another transaction before any K8s call.
	require.NoError(t, store.WithLock(ctx, "fixture-dev", "nightly", func(ctx context.Context, s *Session) error {
		hold, err := s.Get(ctx, "fixture-dev", "nightly")
		require.NoError(t, err)
		require.NotNil(t, hold)
		require.Equal(t, b, hold.Binding)
		return nil
	}))
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- store.WithLock(ctx, "fixture-dev", "nightly", func(context.Context, *Session) error { close(entered); <-release; return nil })
	}()
	<-entered
	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- store.WithLock(ctx, "fixture-dev", "nightly", func(context.Context, *Session) error { close(secondEntered); return nil })
	}()
	select {
	case <-secondEntered:
		t.Fatal("same namespace/job bypassed transaction lock")
	case <-time.After(150 * time.Millisecond):
	}
	// Another namespace can proceed while the first environment is locked.
	require.NoError(t, store.WithLock(ctx, "fixture-prod", "nightly", func(context.Context, *Session) error { return nil }))
	close(release)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	down, err := os.ReadFile("../db/migrations/042_service_job_holds.down.sql")
	require.NoError(t, err)
	_, err = scoped.Exec(string(down))
	require.ErrorContains(t, err, "operational holds exist")
	var count int
	require.NoError(t, scoped.QueryRow(`SELECT count(*) FROM service_job_holds`).Scan(&count))
	require.Equal(t, 1, count)
}
