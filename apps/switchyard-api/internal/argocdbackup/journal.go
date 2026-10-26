package argocdbackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

var (
	ErrJournalHeld     = errors.New("argocd backup journal held")
	ErrJournalNotFound = errors.New("argocd backup operation not found")
	ErrJournalConflict = errors.New("argocd backup immutable operation conflict")
	ErrJournalUnknown  = errors.New("argocd backup journal outcome unknown; same-operation read recovery only")
	ErrJournalOperator = errors.New("argocd backup active platform operator required")
)

// Counts contain only bounded structural counts, never protected object names,
// values or a digest of those values. Capture separately qualifies the fixed
// auxiliary allowlist and full object payloads before constructing a plan.
type CaptureCounts struct {
	Fixed, Secrets, ConfigMaps, Applications, ApplicationSets, AppProjects, Auxiliary, Total int
}

func (c CaptureCounts) valid() bool {
	return c.Fixed == 60 && c.Secrets >= 2 && c.Secrets <= 128 && c.ConfigMaps >= 7 && c.ConfigMaps <= 128 &&
		c.Applications > 0 && c.Applications <= 512 && c.ApplicationSets > 0 && c.ApplicationSets <= 128 && c.AppProjects > 0 && c.AppProjects <= 128 &&
		c.Auxiliary >= 0 && c.Auxiliary <= 9 && c.Total == 60+(c.Secrets-2)+(c.ConfigMaps-7)+c.Applications+c.ApplicationSets+c.AppProjects+c.Auxiliary
}

// Plan is immutable once persisted. Only a package-minted sealed envelope may
// supply ciphertext. JSON cannot serialize or populate its envelope bytes.
// SHA binds public identity/count metadata and a ciphertext digest, never a key,
// plaintext snapshot, credential or other private-value hash.
type Plan struct {
	Context              Context
	PlanID               string
	ActorID              string
	KeyOriginID          string
	KeyVersion           int
	Counts               CaptureCounts
	Reason               string
	CreatedAt, ExpiresAt time.Time
	Receipt              PublicReceipt
	SHA                  string
	envelope             Sealed
}

func NewPlan(sealed Sealed, planID, actorID, keyOriginID string, keyVersion int, counts CaptureCounts, reason string, createdAt, expiresAt time.Time) (Plan, error) {
	r, err := Receipt(sealed)
	if err != nil {
		return Plan{}, ErrJournalHeld
	}
	p := Plan{Context: sealed.context, PlanID: planID, ActorID: actorID, KeyOriginID: keyOriginID, KeyVersion: keyVersion, Counts: counts,
		Reason: reason, CreatedAt: createdAt.UTC().Truncate(time.Microsecond), ExpiresAt: expiresAt.UTC().Truncate(time.Microsecond), Receipt: r, envelope: Sealed{bytes.Clone(sealed.blob), sealed.context}}
	p.SHA, err = p.fingerprint()
	if err != nil || p.Validate() != nil {
		return Plan{}, ErrJournalHeld
	}
	return p, nil
}

func (p Plan) Ciphertext() []byte            { return p.envelope.Bytes() }
func (p Plan) Reference() (Reference, error) { return ReferenceFromReceipt(p.Context, p.Receipt) }

func (p Plan) fingerprint() (string, error) {
	// The unexported envelope is deliberately absent from this projection.
	public := struct {
		Context                      Context
		PlanID, ActorID, KeyOriginID string
		KeyVersion                   int
		Counts                       CaptureCounts
		Reason                       string
		CreatedAt, ExpiresAt         time.Time
		Receipt                      PublicReceipt
	}{p.Context, p.PlanID, p.ActorID, p.KeyOriginID, p.KeyVersion, p.Counts, p.Reason, p.CreatedAt.UTC(), p.ExpiresAt.UTC(), p.Receipt}
	raw, err := json.Marshal(public)
	if err != nil {
		return "", ErrJournalHeld
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (p Plan) Validate() error {
	if _, err := p.Context.aad(); err != nil || p.Context.Topology != "NON_HA_LAYOUT_OBSERVED" || !validUUID(p.PlanID) || !validUUID(p.ActorID) ||
		!secretsintake.ValidArgocdBackupOriginID(p.KeyOriginID) || p.KeyVersion <= 0 || p.KeyVersion > 2147483647 || !p.Counts.valid() ||
		len(p.Reason) > 1000 || strings.TrimSpace(p.Reason) == "" || !utf8.ValidString(p.Reason) || strings.IndexFunc(p.Reason, unicode.IsControl) != -1 ||
		p.CreatedAt.IsZero() || !p.ExpiresAt.After(p.CreatedAt) || !p.CreatedAt.Equal(p.CreatedAt.Truncate(time.Microsecond)) || !p.ExpiresAt.Equal(p.ExpiresAt.Truncate(time.Microsecond)) || p.envelope.context != p.Context {
		return ErrJournalHeld
	}
	ref, err := p.Reference()
	if err != nil || !ref.Matches(p.envelope.blob) {
		return ErrJournalHeld
	}
	want, err := p.fingerprint()
	if err != nil || p.SHA != want {
		return ErrJournalHeld
	}
	return nil
}

type EventKind string

const (
	EventPrepared              EventKind = "PREPARED"
	EventReserved              EventKind = "DISPATCH_RESERVED"
	EventClaimed               EventKind = "DISPATCH_CLAIMED"
	EventPutAcknowledged       EventKind = "PUT_ACKNOWLEDGED"
	EventPutExists             EventKind = "PUT_EXISTS"
	EventPutConflict           EventKind = "PUT_CONFLICT"
	EventPutRefused            EventKind = "PUT_REFUSED"
	EventPutUnknown            EventKind = "PUT_UNKNOWN"
	EventReadbackUnqualified   EventKind = "READBACK_UNQUALIFIED"
	EventReadbackAuthenticated EventKind = "READBACK_AUTHENTICATED"
	EventRetired               EventKind = "PREPARATION_RETIRED"
)

type RetirementReason string

const (
	RetireExpired          RetirementReason = "expired"
	RetireSourceDrift      RetirementReason = "source_drift"
	RetireOperatorWithdrew RetirementReason = "operator_withdrew_before_dispatch"
)

func (r RetirementReason) valid() bool {
	return r == RetireExpired || r == RetireSourceDrift || r == RetireOperatorWithdrew
}

type Event struct {
	Sequence         int64
	Kind             EventKind
	ActorID          string
	RecordedAt       time.Time
	RetirementReason RetirementReason
}
type Record struct {
	Plan   Plan
	Events []Event
}

func (r Record) ReadbackAuthenticated() bool {
	for _, event := range r.Events {
		if event.Kind == EventReadbackAuthenticated {
			return true
		}
	}
	return false
}
func (r Record) DispatchAttempted() bool {
	for _, event := range r.Events {
		if event.Kind == EventReserved || event.Kind == EventClaimed {
			return true
		}
	}
	return false
}

func (r Record) Retired() bool {
	for _, event := range r.Events {
		if event.Kind == EventRetired {
			return true
		}
	}
	return false
}

type DispatchDisposition string

const (
	DispatchReadOnly DispatchDisposition = "SAME_OPERATION_READ_AUTHENTICATION_ONLY"
	DispatchOnce     DispatchDisposition = "NEW_ACKNOWLEDGED_CLAIM_ONE_CONDITIONAL_PUT"
)

// Only successful authenticated readback may close the namespace fence.
// Sealed proof must exactly match the durable envelope; API integration must
// obtain it via the bounded same-object GET followed by Authenticate.
type Outcome struct {
	Kind          EventKind
	Authenticated *Sealed
}

type Journal interface {
	CreatePlan(context.Context, Plan) (Record, error)
	Get(context.Context, string, string) (Record, error)                                        // operationID, actual actorID
	GetOpen(context.Context, string) (*Record, error)                                           // fixed namespace/profile; actual actorID
	ClaimDispatch(context.Context, string, string, string) (Record, DispatchDisposition, error) // operationID, PlanID, actual actorID
	AppendOutcome(context.Context, string, string, Outcome) (Record, error)
	RetirePrepared(context.Context, string, string, string, RetirementReason) (Record, error)
}
