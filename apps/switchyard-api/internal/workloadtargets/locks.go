package workloadtargets

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Session owns a transaction-pooled-safe advisory lock plus the exact service
// row. It must stay open through the complete ordinary mutation, not only GET.
type Session struct {
	tx                   *sql.Tx
	serviceID, projectID uuid.UUID
	cancel               context.CancelFunc
	services             map[uuid.UUID]bool
}

func (s *Session) Tx() *sql.Tx  { return s.tx }
func (s *Session) Store() Store { return Store{Reader: s.tx} }
func (s *Session) Close()       { _ = s.tx.Rollback(); s.cancel() }
func (s *Session) Commit() error {
	if s.tx.Commit() != nil {
		return ErrCommitUnknown
	}
	return nil
}

func (s Store) LockService(ctx context.Context, serviceID uuid.UUID) (*Session, error) {
	if s.DB == nil || serviceID == uuid.Nil {
		return nil, ErrUnavailable
	}
	// Only Close/Commit end the transaction lifetime. Caller cancellation still
	// bounds every query/native callback, but cannot silently release this lock
	// while that callback is classifying an in-flight transport result.
	lifetime, cancel := context.WithCancel(context.WithoutCancel(ctx))
	tx, err := s.DB.BeginTx(lifetime, nil)
	if err != nil {
		cancel()
		return nil, ErrUnavailable
	}
	session := &Session{tx: tx, serviceID: serviceID, cancel: cancel}
	fail := func(e error) (*Session, error) { session.Close(); return nil, e }
	// Parent-first locks serialize project deletion without inversion against
	// apply. A later service reparent cannot substitute another project.
	var parent, lockedParent uuid.UUID
	if tx.QueryRowContext(ctx, `SELECT project_id FROM services WHERE id=$1`, serviceID).Scan(&parent) != nil {
		return fail(ErrIntent)
	}
	if tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE id=$1 FOR SHARE`, parent).Scan(&session.projectID) != nil {
		return fail(ErrIntent)
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "enclii.service-target:"+serviceID.String()); err != nil {
		return fail(ErrUnavailable)
	}
	if tx.QueryRowContext(ctx, `SELECT project_id FROM services WHERE id=$1 FOR NO KEY UPDATE`, serviceID).Scan(&lockedParent) != nil || lockedParent != parent {
		return fail(ErrConflict)
	}
	return session, nil
}

// LockProject locks its parent before all children in a fixed order. A service
// insertion needs the parent FK lock and cannot race a successful deletion.
func (s Store) LockProject(ctx context.Context, projectID uuid.UUID) (*Session, error) {
	if s.DB == nil || projectID == uuid.Nil {
		return nil, ErrUnavailable
	}
	// Only Close/Commit end the transaction lifetime. Caller cancellation still
	// bounds every query/native callback, but cannot silently release this lock
	// while that callback is classifying an in-flight transport result.
	lifetime, cancel := context.WithCancel(context.WithoutCancel(ctx))
	tx, err := s.DB.BeginTx(lifetime, nil)
	if err != nil {
		cancel()
		return nil, ErrUnavailable
	}
	session := &Session{tx: tx, projectID: projectID, cancel: cancel}
	fail := func(e error) (*Session, error) { session.Close(); return nil, e }
	var id uuid.UUID
	if tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE id=$1 FOR UPDATE`, projectID).Scan(&id) != nil {
		return fail(ErrIntent)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM services WHERE project_id=$1 ORDER BY id`, projectID)
	if err != nil {
		return fail(ErrUnavailable)
	}
	var ids []uuid.UUID
	for rows.Next() {
		if rows.Scan(&id) != nil {
			_ = rows.Close()
			return fail(ErrUnavailable)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return fail(ErrUnavailable)
	}
	for _, id = range ids {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "enclii.service-target:"+id.String()); err != nil {
			return fail(ErrUnavailable)
		}
		if tx.QueryRowContext(ctx, `SELECT id FROM services WHERE id=$1 FOR UPDATE`, id).Scan(&id) != nil {
			return fail(ErrConflict)
		}
	}
	return session, nil
}

func (s *Session) RefuseBound(ctx context.Context) error {
	var bound bool
	var err error
	if len(s.services) > 0 {
		ids := []string{}
		for id := range s.services {
			ids = append(ids, id.String())
		}
		err = s.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM service_deployment_targets WHERE service_id=ANY($1::uuid[]))`, pq.Array(ids)).Scan(&bound)
	} else if s.serviceID != uuid.Nil {
		err = s.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM service_deployment_targets WHERE service_id=$1)`, s.serviceID).Scan(&bound)
	} else {
		err = s.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM service_deployment_targets WHERE project_id=$1)`, s.projectID).Scan(&bound)
	}
	if err != nil {
		return ErrUnavailable
	}
	if bound {
		return ErrConflict
	}
	return nil
}

// LockIntent prevents registration/environment edits after the fresh read.
// The service and project already belong to this locked session.
func (s *Session) LockIntent(ctx context.Context, environmentID, registrationID uuid.UUID) error {
	for _, q := range []struct {
		sql string
		id  uuid.UUID
	}{
		{`SELECT project_id FROM environments WHERE id=$1 FOR SHARE`, environmentID},
		{`SELECT project_id FROM onboarding_registrations WHERE id=$1 FOR SHARE`, registrationID},
	} {
		var parent uuid.UUID
		if s.tx.QueryRowContext(ctx, q.sql, q.id).Scan(&parent) != nil || parent != s.projectID {
			return ErrIntent
		}
	}
	return nil
}

func (s *Session) RequireOperator(ctx context.Context, actor uuid.UUID) error {
	var operator bool
	if actor == uuid.Nil || s.tx.QueryRowContext(ctx, `SELECT is_platform_admin FROM users WHERE id=$1 FOR SHARE`, actor).Scan(&operator) != nil || !operator {
		return ErrOperator
	}
	return nil
}

var ErrOperator = errors.New("actual platform operator required")

// WithUnbound fences legacy native/status writers against binding creation.
func (s Store) WithUnbound(ctx context.Context, id uuid.UUID, fn func(*sql.Tx) error) error {
	session, err := s.LockService(ctx, id)
	if err != nil {
		return err
	}
	defer session.Close()
	if err = session.RefuseBound(ctx); err != nil {
		return err
	}
	outcome := fn(session.Tx())
	if err = session.Commit(); err != nil {
		return err
	}
	return outcome
}

// LockServices locks only a server-resolved affected set, in stable UUID order.
// Every member must belong to the same already locked project. Nested child
// writers consume this session instead of acquiring a second same-service lock.
func (s Store) LockServices(ctx context.Context, ids []uuid.UUID) (*Session, error) {
	ordered := append([]uuid.UUID(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	if len(ordered) == 0 {
		return nil, ErrIntent
	}
	for i, id := range ordered {
		if id == uuid.Nil || i > 0 && id == ordered[i-1] {
			return nil, ErrIntent
		}
	}
	session, err := s.LockService(ctx, ordered[0])
	if err != nil {
		return nil, err
	}
	session.services = map[uuid.UUID]bool{ordered[0]: true}
	for _, id := range ordered[1:] {
		if _, err = session.tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "enclii.service-target:"+id.String()); err != nil {
			session.Close()
			return nil, ErrUnavailable
		}
		var parent uuid.UUID
		if session.tx.QueryRowContext(ctx, `SELECT project_id FROM services WHERE id=$1 FOR NO KEY UPDATE`, id).Scan(&parent) != nil || parent != session.projectID {
			session.Close()
			return nil, ErrIntent
		}
		session.services[id] = true
	}
	return session, nil
}
func (s *Session) OwnsService(id uuid.UUID) bool {
	return id != uuid.Nil && (s.serviceID == id || s.services[id])
}
func (s *Session) ProjectID() uuid.UUID { return s.projectID }

// SelectServiceForBinding selects one verified child in a locked project
// transaction. It changes no service metadata. New rows and their immutable
// binding commit together, so no writer observes an unbound new service.
func (s *Session) SelectServiceForBinding(ctx context.Context, id uuid.UUID) error {
	if s == nil || s.tx == nil || s.projectID == uuid.Nil || s.serviceID != uuid.Nil || id == uuid.Nil || len(s.services) != 0 {
		return ErrConflict
	}
	if _, err := s.tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "enclii.service-target:"+id.String()); err != nil {
		return ErrUnavailable
	}
	var parent uuid.UUID
	if s.tx.QueryRowContext(ctx, `SELECT project_id FROM services WHERE id=$1 FOR NO KEY UPDATE`, id).Scan(&parent) != nil || parent != s.projectID {
		return ErrIntent
	}
	s.serviceID = id
	return nil
}
