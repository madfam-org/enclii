package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ManagedDBAddonEventType is a whitelisted event type matching the CHECK
// constraint on managed_db_addon_events.event_type.
type ManagedDBAddonEventType string

const (
	EventAddonCreateRequested     ManagedDBAddonEventType = "addon.create.requested"
	EventAddonProvisioningStarted ManagedDBAddonEventType = "addon.provisioning.started"
	EventAddonReady               ManagedDBAddonEventType = "addon.ready"
	EventAddonFailed              ManagedDBAddonEventType = "addon.failed"
	EventAddonDestroyRequested    ManagedDBAddonEventType = "addon.destroy.requested"
	EventAddonDestroyed           ManagedDBAddonEventType = "addon.destroyed"
	EventAddonBindingCreated      ManagedDBAddonEventType = "addon.binding.created"
	EventAddonBindingDeleted      ManagedDBAddonEventType = "addon.binding.deleted"
	EventAddonCredentialsRotated  ManagedDBAddonEventType = "addon.credentials.rotated" // #nosec G101 -- event name, not a credential
	EventAddonPlanChanged         ManagedDBAddonEventType = "addon.plan.changed"
)

// ManagedDBAddonEvent is one row in the append-only lifecycle ledger.
// Used by operations (timeline UI), compliance (forensic trail), and Sprint 3
// billing (the `addon.ready` / `addon.destroyed` events bracket billable spans).
type ManagedDBAddonEvent struct {
	ID             uuid.UUID               `json:"id"`
	AddonID        uuid.UUID               `json:"addon_id"`
	ProjectID      uuid.UUID               `json:"project_id"`
	EventType      ManagedDBAddonEventType `json:"event_type"`
	ActorUserSub   string                  `json:"actor_user_sub,omitempty"`
	ActorUserEmail string                  `json:"actor_user_email,omitempty"`
	Details        json.RawMessage         `json:"details"`
	CreatedAt      time.Time               `json:"created_at"`
}

// ManagedDBAddonEventRepository handles append-only writes and reads of the
// lifecycle event ledger. There is no update or delete surface by design —
// events are immutable once written.
type ManagedDBAddonEventRepository struct {
	db DBTX
}

// NewManagedDBAddonEventRepository creates an event ledger writer/reader.
func NewManagedDBAddonEventRepository(db DBTX) *ManagedDBAddonEventRepository {
	return &ManagedDBAddonEventRepository{db: db}
}

// NewManagedDBAddonEventRepositoryWithTx creates a transaction-scoped one.
func NewManagedDBAddonEventRepositoryWithTx(tx DBTX) *ManagedDBAddonEventRepository {
	return &ManagedDBAddonEventRepository{db: tx}
}

// InsertEventParams carries the fields needed to record a lifecycle event.
// Details must be a JSON-serializable value (or nil for no payload).
type InsertEventParams struct {
	AddonID        uuid.UUID
	ProjectID      uuid.UUID
	EventType      ManagedDBAddonEventType
	ActorUserSub   string
	ActorUserEmail string
	Details        map[string]interface{}
}

// Insert writes a new event. Returns the event's assigned ID.
func (r *ManagedDBAddonEventRepository) Insert(ctx context.Context, params InsertEventParams) (uuid.UUID, error) {
	if params.AddonID == uuid.Nil {
		return uuid.Nil, errors.New("addon_id is required")
	}
	if params.ProjectID == uuid.Nil {
		return uuid.Nil, errors.New("project_id is required")
	}
	if params.EventType == "" {
		return uuid.Nil, errors.New("event_type is required")
	}

	var detailsJSON []byte
	if params.Details == nil {
		detailsJSON = []byte(`{}`)
	} else {
		b, err := json.Marshal(params.Details)
		if err != nil {
			return uuid.Nil, err
		}
		detailsJSON = b
	}

	id := uuid.New()
	query := `
		INSERT INTO managed_db_addon_events
			(id, addon_id, project_id, event_type, actor_user_sub, actor_user_email, details, created_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, NOW())
	`
	if _, err := r.db.ExecContext(ctx, query,
		id, params.AddonID, params.ProjectID, params.EventType,
		params.ActorUserSub, params.ActorUserEmail, detailsJSON,
	); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// ListByAddon returns events for a single addon in reverse-chron order.
// `limit` caps result size (0 = default 100).
func (r *ManagedDBAddonEventRepository) ListByAddon(ctx context.Context, addonID uuid.UUID, limit int) ([]*ManagedDBAddonEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, addon_id, project_id, event_type, actor_user_sub, actor_user_email, details, created_at
		FROM managed_db_addon_events
		WHERE addon_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, addonID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanEvents(rows)
}

// ListByProject returns events across all addons in a project in reverse-chron
// order. Useful for the project audit UI.
func (r *ManagedDBAddonEventRepository) ListByProject(ctx context.Context, projectID uuid.UUID, limit int) ([]*ManagedDBAddonEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, addon_id, project_id, event_type, actor_user_sub, actor_user_email, details, created_at
		FROM managed_db_addon_events
		WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]*ManagedDBAddonEvent, error) {
	events := []*ManagedDBAddonEvent{}
	for rows.Next() {
		ev := &ManagedDBAddonEvent{}
		var sub, email sql.NullString
		var details []byte
		if err := rows.Scan(
			&ev.ID, &ev.AddonID, &ev.ProjectID, &ev.EventType,
			&sub, &email, &details, &ev.CreatedAt,
		); err != nil {
			return nil, err
		}
		if sub.Valid {
			ev.ActorUserSub = sub.String
		}
		if email.Valid {
			ev.ActorUserEmail = email.String
		}
		if len(details) > 0 {
			ev.Details = json.RawMessage(details)
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}
