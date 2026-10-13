package workloadtargets

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

type Receipt struct {
	Version    string    `json:"version"`
	ID         uuid.UUID `json:"id"`
	Binding    Binding   `json:"binding"`
	ActorID    uuid.UUID `json:"actor_id"`
	Reason     string    `json:"reason"`
	RequestSHA string    `json:"request_sha256"`
}

func RequestHash(r Request, actor uuid.UUID) (string, error) {
	return hash(struct {
		Project, Environment, Namespace, Deployment, ExpectedPlan, Reason string
		ServiceID, ActorID, IdempotencyID                                 uuid.UUID
	}{r.Project, r.Environment, r.Namespace, r.Deployment, r.ExpectedPlan, r.Reason, r.ServiceID, actor, r.IdempotencyID})
}
func (s Store) Receipt(ctx context.Context, id uuid.UUID) (*Receipt, error) {
	if s.queryer() == nil || id == uuid.Nil {
		return nil, ErrUnavailable
	}
	var raw []byte
	err := s.queryer().QueryRowContext(ctx, `SELECT payload FROM service_deployment_target_events WHERE id=$1`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	var receipt Receipt
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 65536 || d.Decode(&receipt) != nil || receipt.ID != id || receipt.Binding.ID != id || receipt.Binding.Validate() != nil || receipt.ActorID == uuid.Nil || !sha64.MatchString(receipt.RequestSHA) || receipt.Version != "service-deployment-target-applied-v1" {
		return nil, ErrConflict
	}
	return &receipt, nil
}

// Persist is one fixed immutable binding plus its immutable actor/reason audit.
// No Kubernetes writer runs, no upsert/rebind and no automatic SQL retry exists.
func (s *Session) Persist(ctx context.Context, r Request, actor uuid.UUID, plan Plan) (*Receipt, error) {
	if r.DryRun || r.ServiceID != s.serviceID || r.IdempotencyID == uuid.Nil || plan.Identity.ServiceID != s.serviceID || plan.Identity.ProjectID != s.projectID || plan.SHA != r.ExpectedPlan || plan.Identity.Validate() != nil {
		return nil, ErrConflict
	}
	fingerprint, err := plan.Fingerprint()
	if err != nil || fingerprint != r.ExpectedPlan {
		return nil, ErrConflict
	}
	if err = s.RequireOperator(ctx, actor); err != nil {
		return nil, err
	}
	requestSHA, err := RequestHash(r, actor)
	if err != nil {
		return nil, err
	}
	old, err := s.Store().Receipt(ctx, r.IdempotencyID)
	if err != nil {
		return nil, err
	}
	if old != nil {
		if old.RequestSHA != requestSHA {
			return nil, ErrConflict
		}
		return old, nil
	}
	if plan.Incumbent != nil {
		return nil, ErrConflict
	}
	binding := Binding{ID: r.IdempotencyID, Identity: plan.Identity, PlanSHA: plan.SHA, Versions: plan.Versions}
	raw, err := json.Marshal(binding)
	if err != nil {
		return nil, ErrIntent
	}
	_, err = s.tx.ExecContext(ctx, `INSERT INTO service_deployment_targets (id,project_id,service_id,environment_id,registration_id,namespace_uid,deployment_uid,actor_id,reason,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, binding.ID, binding.Identity.ProjectID, binding.Identity.ServiceID, binding.Identity.EnvironmentID, binding.Identity.RegistrationID, binding.Identity.NamespaceUID, binding.Identity.DeploymentUID, actor.String(), r.Reason, raw)
	if err != nil {
		return nil, ErrConflict
	}
	receipt := &Receipt{Version: "service-deployment-target-applied-v1", ID: r.IdempotencyID, Binding: binding, ActorID: actor, Reason: r.Reason, RequestSHA: requestSHA}
	raw, err = json.Marshal(receipt)
	if err != nil {
		return nil, ErrIntent
	}
	if _, err = s.tx.ExecContext(ctx, `INSERT INTO service_deployment_target_events (id,binding_id,actor_id,request_sha256,reason,payload) VALUES($1,$2,$3,$4,$5,$6)`, receipt.ID, binding.ID, actor.String(), requestSHA, r.Reason, raw); err != nil {
		return nil, ErrConflict
	}
	return receipt, nil
}
