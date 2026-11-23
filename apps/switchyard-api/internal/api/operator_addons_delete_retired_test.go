package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestOpsAddonsDeleteRetiredIsAnAdvertisedPlatformOperation(t *testing.T) {
	require.True(t, operationSupported("addons", "delete-retired", opsCapabilities))
	require.False(t, operationSupported("addons", "delete", opsCapabilities), "only the closed, retired-only delete is offered")
	require.True(t, operationSupported("addons", "hibernate", opsCapabilities), "the existing addon actions stay")
}

func TestOpsAddonsDeleteRetiredClosedRequest(t *testing.T) {
	h := &Handler{}
	addon := uuid.NewString()
	for name, req := range map[string]operatorOperationRequest{
		"no project":      {DryRun: true, Args: map[string]string{"target": addon}},
		"no target":       {DryRun: true, Scope: map[string]string{"project": "fixture"}},
		"target not uuid": {DryRun: true, Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": "kernel09b"}},
		"apply no plan":   {Reason: "r", IdempotencyKey: "k", Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": addon}},
		"apply no key":    {Reason: "r", Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": addon, "expected_plan": "h"}},
		"apply no reason": {IdempotencyKey: "k", Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": addon, "expected_plan": "h"}},
	} {
		resp, status := h.handleOpsAddonsDeleteRetired(context.Background(), req, opsActor{})
		require.Equal(t, http.StatusBadRequest, status, name)
		require.Equal(t, "failed", resp.Status, name)
		require.Equal(t, opsAddonsDeleteRetiredOperation, resp.Operation)
	}
	// A well-formed request without its adapters is unavailable, not silently planned.
	_, status := h.handleOpsAddonsDeleteRetired(context.Background(), operatorOperationRequest{DryRun: true, Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": addon}}, opsActor{})
	require.Equal(t, http.StatusServiceUnavailable, status)
}

// retiredFixture is an in-memory addon, cluster and audit store.
type retiredFixture struct {
	project   uuid.UUID
	addon     *types.DatabaseAddonWithBindings
	state     *addons.HibernationState
	stateErr  error
	auditErr  error
	deleteErr error
	noAudit   bool
	deletes   int
	audits    []string // outcomes, in order
	events    []string // "audit:<outcome>" and "delete", in order
}

func newRetiredFixture() *retiredFixture {
	project := uuid.New()
	return &retiredFixture{
		project: project,
		addon: &types.DatabaseAddonWithBindings{
			DatabaseAddon: types.DatabaseAddon{ID: uuid.New(), ProjectID: project, Name: "kernel09b", Type: types.DatabaseAddonTypePostgres,
				Status: types.DatabaseAddonStatusReady, K8sNamespace: "project-fixture", K8sResourceName: "pg-kernel09b-fixture"},
			Bindings: []types.DatabaseAddonBinding{{ID: uuid.New(), ServiceID: uuid.New(), EnvVarName: "DATABASE_URL", Status: types.DatabaseAddonBindingStatusDeleted}},
		},
		state: &addons.HibernationState{Namespace: "project-fixture", Cluster: "pg-kernel09b-fixture", ClusterUID: "uid-cluster", Annotated: true, Confirmed: true},
	}
}

func (f *retiredFixture) deps() deleteRetiredDeps {
	d := deleteRetiredDeps{
		projectID: func(slug string) (uuid.UUID, bool) { return f.project, slug == "fixture" },
		addon: func(_ context.Context, id uuid.UUID) (*types.DatabaseAddonWithBindings, error) {
			if id != f.addon.ID {
				return nil, errors.New("addon not found")
			}
			cp := *f.addon
			return &cp, nil
		},
		hibernation: func(context.Context, *types.DatabaseAddon) (*addons.HibernationState, error) {
			if f.stateErr != nil {
				return nil, f.stateErr
			}
			cp := *f.state
			return &cp, nil
		},
		scheduleDeletion: func(_ context.Context, id uuid.UUID) error {
			f.events = append(f.events, "delete")
			if f.deleteErr != nil {
				return f.deleteErr
			}
			f.deletes++
			at := time.Date(2026, 12, 12, 0, 0, 0, 0, time.UTC)
			f.addon.Status, f.addon.DeletionScheduledAt = types.DatabaseAddonStatusPendingDeletion, &at
			return nil
		},
	}
	if !f.noAudit {
		d.audit = func(_ context.Context, outcome, _, _ string, details map[string]any) (string, error) {
			f.events = append(f.events, "audit:"+outcome)
			if f.auditErr != nil {
				return "", f.auditErr
			}
			f.audits = append(f.audits, outcome)
			return uuid.NewString(), nil
		}
	}
	return d
}

func (f *retiredFixture) dryRun() operatorOperationRequest {
	return operatorOperationRequest{DryRun: true, Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": f.addon.ID.String()}}
}

func (f *retiredFixture) apply(plan string) operatorOperationRequest {
	return operatorOperationRequest{Reason: "crea-sim flota: retired 14 days after hibernation", IdempotencyKey: "borrar-kernel09b",
		Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": f.addon.ID.String(), "expected_plan": plan}}
}

func (f *retiredFixture) planHash(t *testing.T) string {
	t.Helper()
	resp, status := runOpsAddonsDeleteRetired(context.Background(), f.dryRun(), f.deps())
	require.Equal(t, http.StatusOK, status, resp.Summary)
	require.Equal(t, "ready_to_apply", resp.Status)
	return resp.Data.(retiredAddonDeletionPlan).PlanHash
}

func TestDeleteRetiredDryRunPlanIsStableAndSecretFree(t *testing.T) {
	f := newRetiredFixture()
	resp, status := runOpsAddonsDeleteRetired(context.Background(), f.dryRun(), f.deps())
	require.Equal(t, http.StatusOK, status)
	plan := resp.Data.(retiredAddonDeletionPlan)
	require.Equal(t, f.addon.ID.String(), plan.AddonID)
	require.Equal(t, "kernel09b", plan.Name)
	require.True(t, plan.Hibernated)
	require.Equal(t, 7*24, plan.RetentionHours)
	require.Len(t, plan.Bindings, 1)
	require.Regexp(t, "^[a-f0-9]{64}$", plan.PlanHash)
	require.Equal(t, plan.PlanHash, f.planHash(t), "same facts, same hash")
	require.Zero(t, f.deletes)
	require.Empty(t, f.audits, "a dry-run writes nothing")

	// CNPG's condition is informational: it does not move the hash.
	f.state.Confirmed = false
	require.Equal(t, plan.PlanHash, f.planHash(t))
	// A replaced cluster does.
	f.state.ClusterUID = "uid-other"
	require.NotEqual(t, plan.PlanHash, f.planHash(t))
}

func TestDeleteRetiredRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*retiredFixture)
		status int
		state  string
	}{
		"other project": {func(f *retiredFixture) { f.addon.ProjectID = uuid.New() }, http.StatusNotFound, "failed"},
		"unknown addon": {func(f *retiredFixture) { f.addon.ID = uuid.New() }, http.StatusNotFound, "failed"},
		"redis":         {func(f *retiredFixture) { f.addon.Type = types.DatabaseAddonTypeRedis }, http.StatusUnprocessableEntity, "failed"},
		"no cluster":    {func(f *retiredFixture) { f.addon.K8sResourceName = "" }, http.StatusUnprocessableEntity, "failed"},
		"shared":        {func(f *retiredFixture) { f.addon.Plan = "shared-discovered" }, http.StatusUnprocessableEntity, "failed"},
		"active binding": {func(f *retiredFixture) {
			f.addon.Bindings = append(f.addon.Bindings, types.DatabaseAddonBinding{Status: types.DatabaseAddonBindingStatusActive})
		}, http.StatusConflict, "failed"},
		"suspended binding": {func(f *retiredFixture) {
			f.addon.Bindings = append(f.addon.Bindings, types.DatabaseAddonBinding{Status: types.DatabaseAddonBindingStatusSuspended})
		}, http.StatusConflict, "failed"},
		"provisioning":   {func(f *retiredFixture) { f.addon.Status = types.DatabaseAddonStatusProvisioning }, http.StatusConflict, "failed"},
		"failed":         {func(f *retiredFixture) { f.addon.Status = types.DatabaseAddonStatusFailed }, http.StatusConflict, "failed"},
		"not hibernated": {func(f *retiredFixture) { f.state.Annotated = false }, http.StatusConflict, "blocked"},
		"cluster unread": {func(f *retiredFixture) { f.stateErr = errors.New("read CNPG cluster: not found") }, http.StatusBadGateway, "failed"},
	} {
		for _, dry := range []bool{true, false} {
			f := newRetiredFixture()
			req := f.apply(f.planHash(t))
			if dry {
				req = f.dryRun()
			}
			tc.mutate(f)
			resp, status := runOpsAddonsDeleteRetired(context.Background(), req, f.deps())
			require.Equal(t, tc.status, status, "%s dry=%v: %s", name, dry, resp.Summary)
			require.Equal(t, tc.state, resp.Status, name)
			require.NotEmpty(t, resp.Summary, name)
			require.Zero(t, f.deletes, name)
			require.Empty(t, f.audits, "%s: a refusal writes nothing", name)
		}
	}
}

func TestDeleteRetiredApplyWithoutAuditStoreChangesNothing(t *testing.T) {
	f := newRetiredFixture()
	hash := f.planHash(t)
	f.noAudit = true
	resp, status := runOpsAddonsDeleteRetired(context.Background(), f.apply(hash), f.deps())
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "failed", resp.Status)
	require.Zero(t, f.deletes)

	// An audit store that refuses the request row: no delete either.
	f = newRetiredFixture()
	hash = f.planHash(t)
	f.auditErr = errors.New("audit write failed")
	_, status = runOpsAddonsDeleteRetired(context.Background(), f.apply(hash), f.deps())
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, []string{"audit:requested"}, f.events)
	require.Zero(t, f.deletes)
}

func TestDeleteRetiredApplyRefusesAChangedPlan(t *testing.T) {
	f := newRetiredFixture()
	hash := f.planHash(t)
	f.state.ClusterUID = "uid-replaced"
	resp, status := runOpsAddonsDeleteRetired(context.Background(), f.apply(hash), f.deps())
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, "failed", resp.Status)
	require.Zero(t, f.deletes)
	require.Empty(t, f.events)
}

func TestDeleteRetiredApplyAuditsThenSchedulesTheHoldOnce(t *testing.T) {
	f := newRetiredFixture()
	hash := f.planHash(t)
	resp, status := runOpsAddonsDeleteRetired(context.Background(), f.apply(hash), f.deps())
	require.Equal(t, http.StatusOK, status, resp.Summary)
	require.Equal(t, "succeeded", resp.Status)
	require.NotEmpty(t, resp.AuditID)
	require.Equal(t, []string{"audit:requested", "delete", "audit:success"}, f.events, "the request row precedes the delete")
	require.Equal(t, 1, f.deletes)
	data := resp.Data.(map[string]any)
	require.Equal(t, hash, data["plan_hash"])
	require.Equal(t, resp.AuditID, data["request_audit_id"])
	require.Equal(t, 7*24, data["retention_hours"])
	require.NotNil(t, data["deletion_scheduled_at"])

	// A retry (same key) on the scheduled addon answers succeeded without a second delete.
	again, status := runOpsAddonsDeleteRetired(context.Background(), f.apply(hash), f.deps())
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "succeeded", again.Status)
	require.Equal(t, true, again.Data.(map[string]any)["already_scheduled"])
	require.Equal(t, 1, f.deletes)
	// And so does a dry-run.
	dry, status := runOpsAddonsDeleteRetired(context.Background(), f.dryRun(), f.deps())
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "succeeded", dry.Status)
	require.Equal(t, []string{"audit:requested", "delete", "audit:success"}, f.events)
}

func TestDeleteRetiredApplyRecordsAFailedDelete(t *testing.T) {
	f := newRetiredFixture()
	hash := f.planHash(t)
	f.deleteErr = errors.New("failed to schedule addon deletion")
	resp, status := runOpsAddonsDeleteRetired(context.Background(), f.apply(hash), f.deps())
	require.Equal(t, http.StatusBadGateway, status)
	require.Equal(t, "failed", resp.Status)
	require.Equal(t, []string{"audit:requested", "delete", "audit:failure"}, f.events)
}

func TestOpsEventActorAttributesTheOperator(t *testing.T) {
	id := uuid.New()
	ev := opsEventActor(opsActor{ID: id.String(), Email: "operator@example.test"})
	require.Equal(t, &id, ev.UserID)
	require.Equal(t, "operator@example.test", ev.UserEmail)
	require.Nil(t, opsEventActor(opsActor{ID: "not-a-uuid"}).UserID)
}
