package argocdbackup

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"
)

// SQLJournal never holds a transaction across a provider operation. All paths
// repeat the active SQL platform-user check; the namespace fence is constant
// even if the Namespace is recreated. No generic repository selector is exposed.
type SQLJournal struct{ DB *sql.DB }

var _ Journal = SQLJournal{}

const backupJournalLock = `SELECT pg_advisory_xact_lock(hashtextextended('enclii:argocd:protected-backup:v1',0))`
const backupOperationColumns = `operation_id,plan_id,actor_id,namespace_uid,profile,topology,backup_id,key_origin_id,key_version,
 fixed_count,secret_count,configmap_count,application_count,applicationset_count,appproject_count,auxiliary_count,total_count,
 reason,created_at,expires_at,ciphertext,ciphertext_sha256,plan_sha256`

func (s SQLJournal) begin(ctx context.Context, actor string) (*sql.Tx, error) {
	if s.DB == nil || !validUUID(actor) || ctx.Err() != nil {
		return nil, ErrJournalHeld
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, ErrJournalUnknown
	}
	var allowed bool
	if tx.QueryRowContext(ctx, `SELECT is_platform_admin AND active FROM users WHERE id=$1 FOR SHARE`, actor).Scan(&allowed) != nil || !allowed {
		_ = tx.Rollback()
		return nil, ErrJournalOperator
	}
	if _, err := tx.ExecContext(ctx, backupJournalLock); err != nil {
		_ = tx.Rollback()
		return nil, ErrJournalUnknown
	}
	return tx, nil
}

func journalCommit(tx *sql.Tx) error {
	if tx.Commit() != nil {
		return ErrJournalUnknown
	}
	return nil
}

func readJournalRecord(ctx context.Context, tx *sql.Tx, id, actor string) (Record, error) {
	var p Plan
	var raw []byte
	var digest string
	err := tx.QueryRowContext(ctx, `SELECT `+backupOperationColumns+` FROM argocd_backup_operations WHERE operation_id=$1`, id).Scan(
		&p.Context.OperationID, &p.PlanID, &p.ActorID, &p.Context.NamespaceUID, &p.Context.Profile, &p.Context.Topology, &p.Context.BackupID, &p.KeyOriginID, &p.KeyVersion,
		&p.Counts.Fixed, &p.Counts.Secrets, &p.Counts.ConfigMaps, &p.Counts.Applications, &p.Counts.ApplicationSets, &p.Counts.AppProjects, &p.Counts.Auxiliary, &p.Counts.Total,
		&p.Reason, &p.CreatedAt, &p.ExpiresAt, &raw, &digest, &p.SHA)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrJournalNotFound
	}
	if err != nil {
		return Record{}, ErrJournalUnknown
	}
	if p.ActorID != actor {
		return Record{}, ErrJournalConflict
	}
	p.CreatedAt, p.ExpiresAt = p.CreatedAt.UTC(), p.ExpiresAt.UTC()
	p.Receipt = PublicReceipt{"enclii-argocd-backup-preparation-v1", "PREPARATION_ONLY", p.Context.OperationID, p.Context.BackupID, digest, len(raw), false}
	p.envelope = Sealed{bytes.Clone(raw), p.Context}
	if p.Validate() != nil {
		return Record{}, ErrJournalHeld
	}
	record := Record{Plan: p, Events: []Event{}}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,kind,actor_id,recorded_at,ciphertext_sha256,retirement_reason FROM argocd_backup_events WHERE operation_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return Record{}, ErrJournalUnknown
	}
	defer func() { _ = rows.Close() }()
	seen := map[EventKind]bool{}
	provider := false
	for rows.Next() {
		var e Event
		var proof sql.NullString
		var retirement sql.NullString
		if rows.Scan(&e.Sequence, &e.Kind, &e.ActorID, &e.RecordedAt, &proof, &retirement) != nil || len(record.Events) >= 11 || e.Sequence != int64(len(record.Events)+1) || e.ActorID != actor || seen[e.Kind] || seen[EventRetired] || !knownJournalEvent(e.Kind) {
			return Record{}, ErrJournalHeld
		}
		if e.Kind == EventPrepared && len(record.Events) != 0 || e.Kind != EventPrepared && !seen[EventPrepared] || e.Kind == EventClaimed && !seen[EventReserved] {
			return Record{}, ErrJournalHeld
		}
		if providerOutcome(e.Kind) {
			if provider || !seen[EventClaimed] {
				return Record{}, ErrJournalHeld
			}
			provider = true
		}
		if e.Kind == EventReadbackAuthenticated {
			if !proof.Valid || proof.String != digest {
				return Record{}, ErrJournalHeld
			}
		} else if proof.Valid {
			return Record{}, ErrJournalHeld
		}
		if e.Kind == EventRetired {
			e.RetirementReason = RetirementReason(retirement.String)
			if !retirement.Valid || !e.RetirementReason.valid() || len(record.Events) != 1 {
				return Record{}, ErrJournalHeld
			}
		} else if retirement.Valid {
			return Record{}, ErrJournalHeld
		}
		e.RecordedAt = e.RecordedAt.UTC()
		record.Events = append(record.Events, e)
		seen[e.Kind] = true
	}
	if rows.Err() != nil {
		return Record{}, ErrJournalUnknown
	}
	if !seen[EventPrepared] {
		return Record{}, ErrJournalHeld
	}
	return record, nil
}

func knownJournalEvent(kind EventKind) bool {
	return kind == EventPrepared || kind == EventReserved || kind == EventClaimed || kind == EventReadbackAuthenticated || kind == EventReadbackUnqualified || kind == EventRetired || providerOutcome(kind)
}
func providerOutcome(kind EventKind) bool {
	return kind == EventPutAcknowledged || kind == EventPutExists || kind == EventPutConflict || kind == EventPutRefused || kind == EventPutUnknown
}
func hasJournalEvent(r Record, kind EventKind) bool {
	for _, e := range r.Events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}
func insertJournalEvent(ctx context.Context, tx *sql.Tx, r Record, kind EventKind, proof any) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO argocd_backup_events(operation_id,sequence,kind,actor_id,ciphertext_sha256) VALUES($1,$2,$3,$4,$5)`, r.Plan.Context.OperationID, len(r.Events)+1, string(kind), r.Plan.ActorID, proof); err != nil {
		return ErrJournalUnknown
	}
	return nil
}

func (s SQLJournal) CreatePlan(ctx context.Context, p Plan) (Record, error) {
	if p.Validate() != nil || !p.ExpiresAt.After(time.Now()) || p.CreatedAt.After(time.Now().Add(time.Minute)) {
		return Record{}, ErrJournalHeld
	}
	tx, err := s.begin(ctx, p.ActorID)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback() }()
	previous, err := readJournalRecord(ctx, tx, p.Context.OperationID, p.ActorID)
	if err == nil {
		if previous.Plan.SHA != p.SHA || !bytes.Equal(previous.Plan.envelope.blob, p.envelope.blob) {
			return Record{}, ErrJournalConflict
		}
		if err = journalCommit(tx); err != nil {
			return Record{}, err
		}
		return previous, nil
	}
	if err != ErrJournalNotFound {
		return Record{}, err
	}
	var occupied bool
	if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM argocd_backup_operations o WHERE NOT EXISTS(SELECT 1 FROM argocd_backup_events e WHERE e.operation_id=o.operation_id AND e.kind IN ('READBACK_AUTHENTICATED','PREPARATION_RETIRED')) )`).Scan(&occupied) != nil {
		return Record{}, ErrJournalUnknown
	}
	if occupied {
		return Record{}, ErrJournalConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO argocd_backup_operations(`+backupOperationColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
		p.Context.OperationID, p.PlanID, p.ActorID, p.Context.NamespaceUID, p.Context.Profile, p.Context.Topology, p.Context.BackupID, p.KeyOriginID, p.KeyVersion,
		p.Counts.Fixed, p.Counts.Secrets, p.Counts.ConfigMaps, p.Counts.Applications, p.Counts.ApplicationSets, p.Counts.AppProjects, p.Counts.Auxiliary, p.Counts.Total,
		p.Reason, p.CreatedAt, p.ExpiresAt, p.envelope.blob, p.Receipt.CiphertextSHA256, p.SHA)
	if err != nil {
		return Record{}, ErrJournalUnknown
	}
	if err = insertJournalEvent(ctx, tx, Record{Plan: p}, EventPrepared, nil); err != nil {
		return Record{}, err
	}
	record, err := readJournalRecord(ctx, tx, p.Context.OperationID, p.ActorID)
	if err != nil {
		return Record{}, err
	}
	if err = journalCommit(tx); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s SQLJournal) Get(ctx context.Context, id, actor string) (Record, error) {
	if !validUUID(id) {
		return Record{}, ErrJournalHeld
	}
	tx, err := s.begin(ctx, actor)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback() }()
	r, err := readJournalRecord(ctx, tx, id, actor)
	if err != nil {
		return Record{}, err
	}
	if err = journalCommit(tx); err != nil {
		return Record{}, err
	}
	return r, nil
}

func (s SQLJournal) GetOpen(ctx context.Context, actor string) (*Record, error) {
	tx, err := s.begin(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT operation_id FROM argocd_backup_operations o WHERE NOT EXISTS(SELECT 1 FROM argocd_backup_events e WHERE e.operation_id=o.operation_id AND e.kind IN ('READBACK_AUTHENTICATED','PREPARATION_RETIRED')) ORDER BY created_at,operation_id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if err = journalCommit(tx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, ErrJournalUnknown
	}
	r, err := readJournalRecord(ctx, tx, id, actor)
	if err != nil {
		return nil, err
	}
	if err = journalCommit(tx); err != nil {
		return nil, err
	}
	return &r, nil
}

// A new call first reserves and ACKs a durable event. A second transaction
// claims and ACKs dispatch. Only that same invocation can return DispatchOnce.
// Retained reservations (including a rolled-back/unknown final claim) are never
// resumed. If the very first reservation truly rolls back, no durable evidence
// can record its ambiguity; that failed invocation never reaches a provider.
func (s SQLJournal) ClaimDispatch(ctx context.Context, id, planID, actor string) (Record, DispatchDisposition, error) {
	if !validUUID(id) || !validUUID(planID) {
		return Record{}, DispatchReadOnly, ErrJournalHeld
	}
	tx, err := s.begin(ctx, actor)
	if err != nil {
		return Record{}, DispatchReadOnly, err
	}
	defer func() { _ = tx.Rollback() }()
	r, err := readJournalRecord(ctx, tx, id, actor)
	if err != nil {
		return Record{}, DispatchReadOnly, err
	}
	if r.Plan.PlanID != planID {
		return Record{}, DispatchReadOnly, ErrJournalConflict
	}
	if r.Retired() {
		return Record{}, DispatchReadOnly, ErrJournalHeld
	}
	if r.DispatchAttempted() || r.ReadbackAuthenticated() {
		if err = journalCommit(tx); err != nil {
			return Record{}, DispatchReadOnly, err
		}
		return r, DispatchReadOnly, nil
	}
	if !r.Plan.ExpiresAt.After(time.Now()) {
		return Record{}, DispatchReadOnly, ErrJournalHeld
	}
	if err = insertJournalEvent(ctx, tx, r, EventReserved, nil); err != nil {
		return Record{}, DispatchReadOnly, err
	}
	if err = journalCommit(tx); err != nil {
		return Record{}, DispatchReadOnly, err
	}
	claim, err := s.begin(ctx, actor)
	if err != nil {
		return Record{}, DispatchReadOnly, err
	}
	defer func() { _ = claim.Rollback() }()
	r, err = readJournalRecord(ctx, claim, id, actor)
	if err != nil {
		return Record{}, DispatchReadOnly, err
	}
	if !hasJournalEvent(r, EventReserved) || hasJournalEvent(r, EventClaimed) || r.ReadbackAuthenticated() || r.Retired() {
		return Record{}, DispatchReadOnly, ErrJournalHeld
	}
	if err = insertJournalEvent(ctx, claim, r, EventClaimed, nil); err != nil {
		return Record{}, DispatchReadOnly, err
	}
	r, err = readJournalRecord(ctx, claim, id, actor)
	if err != nil {
		return Record{}, DispatchReadOnly, err
	}
	if err = journalCommit(claim); err != nil {
		return Record{}, DispatchReadOnly, err
	}
	return r, DispatchOnce, nil
}

// Retirement is a reviewed SQL-only transition for an exact, still PREPARED
// operation. Any reservation, claim, outcome or unknown history forbids it.
// Ciphertext and original reason/identity remain immutable; no replacement is
// generated here and no provider absence is inferred.
func (s SQLJournal) RetirePrepared(ctx context.Context, id, planID, actor string, reason RetirementReason) (Record, error) {
	if !validUUID(id) || !validUUID(planID) || !reason.valid() {
		return Record{}, ErrJournalHeld
	}
	tx, err := s.begin(ctx, actor)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback() }()
	r, err := readJournalRecord(ctx, tx, id, actor)
	if err != nil {
		return Record{}, err
	}
	if r.Plan.PlanID != planID {
		return Record{}, ErrJournalConflict
	}
	if r.Retired() {
		last := r.Events[len(r.Events)-1]
		if last.RetirementReason != reason {
			return Record{}, ErrJournalConflict
		}
		if err = journalCommit(tx); err != nil {
			return Record{}, err
		}
		return r, nil
	}
	if len(r.Events) != 1 || r.Events[0].Kind != EventPrepared || r.DispatchAttempted() || r.ReadbackAuthenticated() {
		return Record{}, ErrJournalHeld
	}
	if reason == RetireExpired && r.Plan.ExpiresAt.After(time.Now()) {
		return Record{}, ErrJournalHeld
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO argocd_backup_events(operation_id,sequence,kind,actor_id,retirement_reason) VALUES($1,$2,$3,$4,$5)`, id, 2, string(EventRetired), actor, string(reason))
	if err != nil {
		return Record{}, ErrJournalUnknown
	}
	r, err = readJournalRecord(ctx, tx, id, actor)
	if err != nil {
		return Record{}, err
	}
	if err = journalCommit(tx); err != nil {
		return Record{}, err
	}
	return r, nil
}

func (s SQLJournal) AppendOutcome(ctx context.Context, id, actor string, outcome Outcome) (Record, error) {
	if !validUUID(id) || (!providerOutcome(outcome.Kind) && outcome.Kind != EventReadbackAuthenticated && outcome.Kind != EventReadbackUnqualified) {
		return Record{}, ErrJournalHeld
	}
	tx, err := s.begin(ctx, actor)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback() }()
	r, err := readJournalRecord(ctx, tx, id, actor)
	if err != nil {
		return Record{}, err
	}
	if r.Retired() {
		return Record{}, ErrJournalHeld
	}
	var proof any
	if outcome.Kind == EventReadbackAuthenticated {
		if outcome.Authenticated == nil || outcome.Authenticated.context != r.Plan.Context || !bytes.Equal(outcome.Authenticated.blob, r.Plan.envelope.blob) {
			return Record{}, ErrJournalHeld
		}
		proof = r.Plan.Receipt.CiphertextSHA256
	} else if outcome.Authenticated != nil {
		return Record{}, ErrJournalHeld
	}
	if hasJournalEvent(r, outcome.Kind) {
		if err = journalCommit(tx); err != nil {
			return Record{}, err
		}
		return r, nil
	}
	if providerOutcome(outcome.Kind) {
		if !hasJournalEvent(r, EventClaimed) {
			return Record{}, ErrJournalHeld
		}
		for _, e := range r.Events {
			if providerOutcome(e.Kind) {
				return Record{}, ErrJournalConflict
			}
		}
	}
	if err = insertJournalEvent(ctx, tx, r, outcome.Kind, proof); err != nil {
		return Record{}, err
	}
	r, err = readJournalRecord(ctx, tx, id, actor)
	if err != nil {
		return Record{}, err
	}
	if err = journalCommit(tx); err != nil {
		return Record{}, err
	}
	return r, nil
}
