//go:build integration

package argocdbackup

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// This test never falls back to ambient KUBECONFIG, DATABASE_URL, TCP, a user
// database or a service stack. The owned fixture launcher supplies this exact
// socket-only profile. Missing qualification fails; it is not a skipped proof.
func journalPostgresFixture(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	socket, nonce := os.Getenv("ARGOCD_BACKUP_JOURNAL_FIXTURE_SOCKET"), os.Getenv("ARGOCD_BACKUP_JOURNAL_FIXTURE_NONCE")
	require.True(t, strings.HasPrefix(socket, "/private/tmp/aj-pg-") && filepath.Clean(socket) == socket && uuid.Validate(nonce) == nil, "owned fixture profile required")
	dsn := "host=" + socket + " port=55442 user=backup_fixture dbname=argocd_backup_fixture sslmode=disable connect_timeout=3"
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	var user, db, listen, actualNonce string
	var super, noTCP bool
	require.NoError(t, database.QueryRow(`SELECT current_user,current_database(),current_setting('listen_addresses'),current_setting('enclii.fixture_nonce'),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user),inet_server_addr() IS NULL`).Scan(&user, &db, &listen, &actualNonce, &super, &noTCP))
	require.Equal(t, "backup_fixture", user)
	require.Equal(t, "argocd_backup_fixture", db)
	require.Empty(t, listen)
	require.Equal(t, nonce, actualNonce)
	require.False(t, super)
	require.True(t, noTCP)
	_, err = database.Exec(`CREATE TABLE users(id uuid PRIMARY KEY,is_platform_admin boolean NOT NULL,active boolean NOT NULL)`)
	require.NoError(t, err)
	up, err := os.ReadFile("../db/migrations/046_argocd_protected_backup_journal.up.sql")
	require.NoError(t, err)
	tx, err := database.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(string(up))
	if err != nil {
		_ = tx.Rollback()
	}
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	actor := uuid.NewString()
	_, err = database.Exec(`INSERT INTO users VALUES($1,true,true)`, actor)
	require.NoError(t, err)
	return database, dsn, actor
}

func journalPostgresPlan(t *testing.T, actor string) (Plan, Sealed) {
	t.Helper()
	c := fixtureContext()
	c.OperationID = uuid.NewString()
	c.BackupID = uuid.NewString()
	sealed, err := Seal(bytes.Repeat([]byte{0x5a}, 32), []byte("OWNED_SYNTHETIC_BACKUP_PAYLOAD"), c)
	require.NoError(t, err)
	now := time.Now().UTC()
	p, err := NewPlan(sealed, uuid.NewString(), actor, uuid.NewString(), 3, CaptureCounts{Fixed: 60, Secrets: 2, ConfigMaps: 7, Applications: 1, ApplicationSets: 1, AppProjects: 1, Total: 63}, "Qualify owned synthetic journal", now, now.Add(time.Hour))
	require.NoError(t, err)
	return p, sealed
}

func journalPostgresAuthenticate(t *testing.T, j SQLJournal, p Plan) {
	t.Helper()
	stored, err := j.Get(context.Background(), p.Context.OperationID, p.ActorID)
	require.NoError(t, err)
	// The provider is deliberately simulated; this control qualifies SQL/AEAD
	// recovery only and makes no real R2 custody or restore assertion.
	authenticated, err := Authenticate(bytes.Repeat([]byte{0x5a}, 32), stored.Plan.Ciphertext(), stored.Plan.Context)
	require.NoError(t, err)
	r, err := j.AppendOutcome(context.Background(), p.Context.OperationID, p.ActorID, Outcome{Kind: EventReadbackAuthenticated, Authenticated: &authenticated})
	require.NoError(t, err)
	require.True(t, r.ReadbackAuthenticated())
}

type journalCommitFaultDriver struct {
	sequence    atomic.Int64
	target      int64
	afterCommit bool
}
type journalCommitFaultConn struct {
	driver.Conn
	owner *journalCommitFaultDriver
}
type journalCommitFaultTx struct {
	driver.Tx
	owner *journalCommitFaultDriver
}

func (d *journalCommitFaultDriver) Open(dsn string) (driver.Conn, error) {
	c, err := (&pq.Driver{}).Open(dsn)
	if err != nil {
		return nil, err
	}
	return &journalCommitFaultConn{c, d}, nil
}
func (c *journalCommitFaultConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return &journalCommitFaultTx{tx, c.owner}, nil
}
func (t *journalCommitFaultTx) Commit() error {
	if t.owner.sequence.Add(1) == t.owner.target {
		if t.owner.afterCommit {
			if err := t.Tx.Commit(); err != nil {
				return err
			}
		} else {
			if err := t.Tx.Rollback(); err != nil {
				return err
			}
		}
		return errors.New("synthetic lost commit acknowledgement")
	}
	return t.Tx.Commit()
}

func TestJournalPostgresMigrationConcurrencyRecoveryAndRetirement(t *testing.T) {
	database, dsn, actor := journalPostgresFixture(t)
	j := SQLJournal{DB: database}
	ctx := context.Background()
	t.Run("immutable-plan-reload-and-global-namespace-fence", func(t *testing.T) {
		p, _ := journalPostgresPlan(t, actor)
		r, err := j.CreatePlan(ctx, p)
		require.NoError(t, err)
		require.Equal(t, p.SHA, r.Plan.SHA)
		require.Equal(t, p.Ciphertext(), r.Plan.Ciphertext())
		r, err = j.Get(ctx, p.Context.OperationID, actor)
		require.NoError(t, err)
		require.Equal(t, p.SHA, r.Plan.SHA)
		open, err := j.GetOpen(ctx, actor)
		require.NoError(t, err)
		require.Equal(t, p.Context.OperationID, open.Plan.Context.OperationID)
		other, _ := journalPostgresPlan(t, actor)
		otherContext := other.Context
		otherContext.NamespaceUID = uuid.NewString()
		otherSeal, err := Seal(bytes.Repeat([]byte{0x5a}, 32), []byte("OTHER_SYNTHETIC_PAYLOAD"), otherContext)
		require.NoError(t, err)
		other, err = NewPlan(otherSeal, other.PlanID, actor, other.KeyOriginID, 3, other.Counts, other.Reason, other.CreatedAt, other.ExpiresAt)
		require.NoError(t, err)
		_, err = j.CreatePlan(ctx, other)
		require.Equal(t, ErrJournalConflict, err, "Namespace recreation must not bypass active operation")
		_, err = j.RetirePrepared(ctx, p.Context.OperationID, p.PlanID, actor, RetireOperatorWithdrew)
		require.NoError(t, err)
		_, err = j.CreatePlan(ctx, other)
		require.NoError(t, err)
		_, err = j.RetirePrepared(ctx, other.Context.OperationID, other.PlanID, actor, RetireOperatorWithdrew)
		require.NoError(t, err)
	})
	t.Run("independent-connections-one-dispatch", func(t *testing.T) {
		p, _ := journalPostgresPlan(t, actor)
		_, err := j.CreatePlan(ctx, p)
		require.NoError(t, err)
		a, err := sql.Open("postgres", dsn)
		require.NoError(t, err)
		defer a.Close()
		a.SetMaxOpenConns(1)
		b, err := sql.Open("postgres", dsn)
		require.NoError(t, err)
		defer b.Close()
		b.SetMaxOpenConns(1)
		var one, two int
		require.NoError(t, a.QueryRow(`SELECT pg_backend_pid()`).Scan(&one))
		require.NoError(t, b.QueryRow(`SELECT pg_backend_pid()`).Scan(&two))
		require.NotEqual(t, one, two)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var allowed, readOnly atomic.Int64
		for _, db := range []*sql.DB{a, b} {
			wg.Add(1)
			go func(db *sql.DB) {
				defer wg.Done()
				<-start
				_, disposition, e := (SQLJournal{DB: db}).ClaimDispatch(ctx, p.Context.OperationID, p.PlanID, actor)
				if e != nil {
					t.Errorf("claim refused: %v", e)
					return
				}
				if disposition == DispatchOnce {
					allowed.Add(1)
				} else {
					readOnly.Add(1)
				}
			}(db)
		}
		close(start)
		wg.Wait()
		require.EqualValues(t, 1, allowed.Load())
		require.EqualValues(t, 1, readOnly.Load())
		r, err := j.Get(ctx, p.Context.OperationID, actor)
		require.NoError(t, err)
		require.Len(t, r.Events, 3)
		_, err = j.RetirePrepared(ctx, p.Context.OperationID, p.PlanID, actor, RetireSourceDrift)
		require.Equal(t, ErrJournalHeld, err)
		journalPostgresAuthenticate(t, j, p)
	})
	t.Run("apply-versus-retire-is-exclusive", func(t *testing.T) {
		for n := 0; n < 6; n++ {
			p, _ := journalPostgresPlan(t, actor)
			_, err := j.CreatePlan(ctx, p)
			require.NoError(t, err)
			a, err := sql.Open("postgres", dsn)
			require.NoError(t, err)
			b, err := sql.Open("postgres", dsn)
			require.NoError(t, err)
			start := make(chan struct{})
			var wg sync.WaitGroup
			var dispatched, retired atomic.Int64
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, d, e := (SQLJournal{DB: a}).ClaimDispatch(ctx, p.Context.OperationID, p.PlanID, actor)
				if e == nil && d == DispatchOnce {
					dispatched.Add(1)
				}
			}()
			go func() {
				defer wg.Done()
				<-start
				r, e := (SQLJournal{DB: b}).RetirePrepared(ctx, p.Context.OperationID, p.PlanID, actor, RetireOperatorWithdrew)
				if e == nil && r.Retired() {
					retired.Add(1)
				}
			}()
			close(start)
			wg.Wait()
			_ = a.Close()
			_ = b.Close()
			require.EqualValues(t, 1, dispatched.Load()+retired.Load())
			r, err := j.Get(ctx, p.Context.OperationID, actor)
			require.NoError(t, err)
			require.Equal(t, p.Ciphertext(), r.Plan.Ciphertext())
			if retired.Load() == 1 {
				require.True(t, r.Retired())
				require.False(t, r.DispatchAttempted())
				_, d, e := j.ClaimDispatch(ctx, p.Context.OperationID, p.PlanID, actor)
				require.Equal(t, DispatchReadOnly, d)
				require.Error(t, e)
			} else {
				require.True(t, r.DispatchAttempted())
				require.False(t, r.Retired())
				journalPostgresAuthenticate(t, j, p)
			}
		}
	})
	t.Run("actual-commit-uncertainty-keeps-reservation-read-only", func(t *testing.T) {
		for _, afterCommit := range []bool{false, true} {
			p, _ := journalPostgresPlan(t, actor)
			_, err := j.CreatePlan(ctx, p)
			require.NoError(t, err)
			name := "journal_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			sql.Register(name, &journalCommitFaultDriver{target: 2, afterCommit: afterCommit})
			fault, err := sql.Open(name, dsn)
			require.NoError(t, err)
			fault.SetMaxOpenConns(1)
			_, d, err := (SQLJournal{DB: fault}).ClaimDispatch(ctx, p.Context.OperationID, p.PlanID, actor)
			_ = fault.Close()
			require.Equal(t, ErrJournalUnknown, err)
			require.Equal(t, DispatchReadOnly, d)
			r, err := j.Get(ctx, p.Context.OperationID, actor)
			require.NoError(t, err)
			require.True(t, r.DispatchAttempted())
			require.Equal(t, afterCommit, hasJournalEvent(r, EventClaimed))
			_, d, err = j.ClaimDispatch(ctx, p.Context.OperationID, p.PlanID, actor)
			require.NoError(t, err)
			require.Equal(t, DispatchReadOnly, d)
			_, err = j.RetirePrepared(ctx, p.Context.OperationID, p.PlanID, actor, RetireOperatorWithdrew)
			require.Equal(t, ErrJournalHeld, err)
			journalPostgresAuthenticate(t, j, p)
		}
	})
	t.Run("active-actor-check-event-bounds-and-append-only", func(t *testing.T) {
		p, _ := journalPostgresPlan(t, actor)
		_, err := j.CreatePlan(ctx, p)
		require.NoError(t, err)
		_, err = database.Exec(`UPDATE users SET active=false WHERE id=$1`, actor)
		require.NoError(t, err)
		_, _, err = j.ClaimDispatch(ctx, p.Context.OperationID, p.PlanID, actor)
		require.Equal(t, ErrJournalOperator, err)
		_, err = database.Exec(`UPDATE users SET active=true WHERE id=$1`, actor)
		require.NoError(t, err)
		_, d, err := j.ClaimDispatch(ctx, p.Context.OperationID, p.PlanID, actor)
		require.NoError(t, err)
		require.Equal(t, DispatchOnce, d)
		for n := 0; n < 12; n++ {
			_, err = j.AppendOutcome(ctx, p.Context.OperationID, actor, Outcome{Kind: EventReadbackUnqualified})
			require.NoError(t, err)
		}
		r, err := j.Get(ctx, p.Context.OperationID, actor)
		require.NoError(t, err)
		require.Len(t, r.Events, 4)
		for _, query := range []string{`UPDATE argocd_backup_operations SET reason='changed'`, `DELETE FROM argocd_backup_operations`, `TRUNCATE argocd_backup_events`, `UPDATE argocd_backup_events SET kind='PUT_UNKNOWN'`, `DELETE FROM argocd_backup_events`} {
			_, err = database.Exec(query)
			require.Error(t, err)
		}
		_, err = database.Exec(`INSERT INTO argocd_backup_events(operation_id,sequence,kind,actor_id) VALUES($1,5,'READBACK_AUTHENTICATED',$2)`, p.Context.OperationID, actor)
		require.Error(t, err, "NULL proof must not close the fence")
		journalPostgresAuthenticate(t, j, p)
		r, err = j.Get(ctx, p.Context.OperationID, actor)
		require.NoError(t, err)
		require.Len(t, r.Events, 5)
		require.Equal(t, p.Ciphertext(), r.Plan.Ciphertext())
		var before int
		require.NoError(t, database.QueryRow(`SELECT count(*) FROM argocd_backup_operations`).Scan(&before))
		down, err := os.ReadFile("../db/migrations/046_argocd_protected_backup_journal.down.sql")
		require.NoError(t, err)
		_, err = database.Exec(string(down))
		require.Error(t, err)
		var after int
		require.NoError(t, database.QueryRow(`SELECT count(*) FROM argocd_backup_operations`).Scan(&after))
		require.Equal(t, before, after)
	})
	t.Run("lost-preparation-response-is-discoverable-without-new-operation", func(t *testing.T) {
		p, _ := journalPostgresPlan(t, actor)
		name := "journal_prepare_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		sql.Register(name, &journalCommitFaultDriver{target: 1, afterCommit: true})
		fault, err := sql.Open(name, dsn)
		require.NoError(t, err)
		_, err = (SQLJournal{DB: fault}).CreatePlan(ctx, p)
		_ = fault.Close()
		require.Equal(t, ErrJournalUnknown, err)
		current, err := j.GetOpen(ctx, actor)
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, p.Context.OperationID, current.Plan.Context.OperationID)
		require.Equal(t, p.Ciphertext(), current.Plan.Ciphertext())
		require.Len(t, current.Events, 1)
		require.False(t, current.DispatchAttempted())
		_, err = j.RetirePrepared(ctx, p.Context.OperationID, p.PlanID, actor, RetireOperatorWithdrew)
		require.NoError(t, err)
	})
	t.Run("retirement-commit-uncertainty-preserves-plan-and-requires-status-read", func(t *testing.T) {
		for _, afterCommit := range []bool{false, true} {
			p, _ := journalPostgresPlan(t, actor)
			_, err := j.CreatePlan(ctx, p)
			require.NoError(t, err)
			name := "journal_retire_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			sql.Register(name, &journalCommitFaultDriver{target: 1, afterCommit: afterCommit})
			fault, err := sql.Open(name, dsn)
			require.NoError(t, err)
			_, err = (SQLJournal{DB: fault}).RetirePrepared(ctx, p.Context.OperationID, p.PlanID, actor, RetireOperatorWithdrew)
			_ = fault.Close()
			require.Equal(t, ErrJournalUnknown, err)
			retained, err := j.Get(ctx, p.Context.OperationID, actor)
			require.NoError(t, err)
			require.Equal(t, p.Ciphertext(), retained.Plan.Ciphertext())
			require.Equal(t, afterCommit, retained.Retired())
			current, err := j.GetOpen(ctx, actor)
			require.NoError(t, err)
			if afterCommit {
				require.Nil(t, current)
				_, d, e := j.ClaimDispatch(ctx, p.Context.OperationID, p.PlanID, actor)
				require.Equal(t, DispatchReadOnly, d)
				require.Error(t, e)
			} else {
				require.NotNil(t, current)
				require.Equal(t, p.Context.OperationID, current.Plan.Context.OperationID)
				_, err = j.RetirePrepared(ctx, p.Context.OperationID, p.PlanID, actor, RetireOperatorWithdrew)
				require.NoError(t, err)
			}
		}
	})
	open, err := j.GetOpen(ctx, actor)
	require.NoError(t, err)
	require.Nil(t, open)
}
