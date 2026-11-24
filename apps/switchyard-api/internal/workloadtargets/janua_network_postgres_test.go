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
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagenetwork"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func nativeJanuaNetworkFixture(t *testing.T) (januastagenetwork.Store, januastagenetwork.Plan) {
	t.Helper()
	db, database, a := nativeJanuaDatabaseFixture(t)
	body, e := os.ReadFile("../db/migrations/052_janua_staging_network_preparation.up.sql")
	require.NoError(t, e)
	_, e = db.ExecContext(context.Background(), string(body))
	require.NoError(t, e)
	p, e := januastagenetwork.BuildPlan(uuid.New(), database.ActorID, a, januastagenetwork.Inputs{Peers: []januastagenetwork.Peer{{HostRoute: "192.0.2.1/32", Port: 443}}, Operator: metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "cloudnative-pg"}}, ContextSHA256: strings.Repeat("e", 64)})
	require.NoError(t, e)
	return januastagenetwork.Store{DB: db}, p
}
func nativeNetworkRecord(t *testing.T, s januastagenetwork.Store, p januastagenetwork.Plan, i int) {
	t.Helper()
	ctx := context.Background()
	o := p.Objects()[i]
	require.NoError(t, s.Intent(ctx, p, i, o))
	require.NoError(t, s.Record(ctx, p, januastagenetwork.ObjectReceipt{Index: i, Key: o.Key, UID: "synthetic-network-" + uuid.NewString(), ResourceVersion: "1", SHA256: p.ObjectSHA256[i]}))
}

// Real PostgreSQL/TLS, source-only fixture. Native UID strings below are
// explicitly synthetic; these tests establish journal behavior, not live CNI.
func TestNativeTargetJanuaNetworkPartialReceiptsSurviveAndCannotBeRewritten(t *testing.T) {
	s, p := nativeJanuaNetworkFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	close, e := s.Lock(ctx, p)
	require.NoError(t, e)
	defer close()
	require.NoError(t, s.Claim(ctx, p, "synthetic fixed preparation"))
	nativeNetworkRecord(t, s, p, 0) // Independent commit while access SHARE locks remain held.
	close()
	r, e := s.Read(ctx, p.ID, p.ActorID)
	require.NoError(t, e)
	require.Len(t, r.Objects, 1)
	require.False(t, r.Complete)
	require.ErrorIs(t, s.Claim(ctx, p, "synthetic fixed preparation"), januastagenetwork.ErrUnknown)
	replacement := r.Objects[0]
	replacement.UID = "recreated-namespace-fixture"
	require.Error(t, s.Record(ctx, p, replacement))
	reread, e := s.Read(ctx, p.ID, p.ActorID)
	require.NoError(t, e)
	require.Equal(t, r.Objects, reread.Objects)
	require.Error(t, s.Complete(ctx, p))
	for _, query := range []string{`DELETE FROM janua_staging_network_operations`, `DELETE FROM janua_staging_network_attempts`, `DELETE FROM janua_staging_network_receipts`, `TRUNCATE janua_staging_network_receipts`, `UPDATE janua_staging_network_receipts SET uid='replacement-fixture'`} {
		_, e = s.DB.ExecContext(ctx, query)
		require.Error(t, e)
	}
	body, e := os.ReadFile("../db/migrations/052_janua_staging_network_preparation.down.sql")
	require.NoError(t, e)
	_, e = s.DB.ExecContext(ctx, string(body))
	require.Error(t, e)
}
func TestNativeTargetJanuaNetworkClosedOrderedCompletion(t *testing.T) {
	s, p := nativeJanuaNetworkFixture(t)
	ctx := context.Background()
	require.NoError(t, s.Claim(ctx, p, "synthetic ordered preparation"))
	_, e := s.DB.ExecContext(ctx, `INSERT INTO janua_staging_network_attempts(operation_id,ordinal,object_key,desired_sha256) VALUES($1,0,'Namespace:/foreign',$2)`, p.ID, p.ObjectSHA256[0])
	require.Error(t, e)
	_, e = s.DB.ExecContext(ctx, `INSERT INTO janua_staging_network_attempts(operation_id,ordinal,object_key,desired_sha256) VALUES($1,0,$2,$3)`, p.ID, p.Objects()[0].Key, strings.Repeat("f", 64))
	require.Error(t, e)
	require.Error(t, s.Intent(ctx, p, 1, p.Objects()[1]))
	require.Error(t, s.Complete(ctx, p))
	for i := range p.Objects() {
		nativeNetworkRecord(t, s, p, i)
	}
	require.NoError(t, s.Complete(ctx, p))
	r, e := s.Read(ctx, p.ID, p.ActorID)
	require.NoError(t, e)
	require.True(t, r.Complete)
	require.Len(t, r.Objects, 7)
	_, e = s.Read(ctx, p.ID, uuid.New())
	require.Error(t, e)
	_, e = s.DB.ExecContext(ctx, `UPDATE users SET active=false WHERE id=$1`, p.ActorID)
	require.NoError(t, e)
	require.Error(t, s.Check(ctx, p))
	_, e = s.Read(ctx, p.ID, p.ActorID)
	require.Error(t, e)
}
func TestNativeTargetJanuaNetworkConcurrentClaimHasOneAuthoritySlot(t *testing.T) {
	s, p := nativeJanuaNetworkFixture(t)
	a := p.Authority
	a.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	other, e := januastagenetwork.BuildPlan(uuid.New(), p.ActorID, a, p.Inputs)
	require.NoError(t, e)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []januastagenetwork.Plan{p, other} {
		wg.Add(1)
		go func(p januastagenetwork.Plan) {
			defer wg.Done()
			<-start
			results <- s.Claim(context.Background(), p, "synthetic concurrent preparation")
		}(candidate)
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		}
	}
	require.Equal(t, 1, wins)
	var count int
	require.NoError(t, s.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM janua_staging_network_operations WHERE authority_id=$1`, p.Authority.ID).Scan(&count))
	require.Equal(t, 1, count)
}
func TestNativeTargetJanuaNetworkAccessLockFencesRevocationAndCompetingEffects(t *testing.T) {
	s, p := nativeJanuaNetworkFixture(t)
	ctx := context.Background()
	release, e := s.Lock(ctx, p)
	require.NoError(t, e)
	defer release()
	_, e = s.Lock(ctx, p)
	require.Error(t, e)
	conn, e := s.DB.Conn(ctx)
	require.NoError(t, e)
	defer func() { release(); _ = conn.Close() }()
	var pid int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	done := make(chan error, 1)
	go func() {
		_, e := conn.ExecContext(ctx, `UPDATE users SET active=false WHERE id=$1`, p.ActorID)
		done <- e
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		e := s.DB.QueryRowContext(ctx, `SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&blocked)
		return e == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	release()
	require.NoError(t, <-done)
	require.Error(t, s.Check(ctx, p))
}
