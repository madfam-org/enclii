package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestOpsAddonsHibernateIsAnAdvertisedPlatformOperation(t *testing.T) {
	require.True(t, operationSupported("addons", "hibernate", opsCapabilities))
	require.False(t, operationSupported("addons", "delete", opsCapabilities), "hibernation never deletes")
}

func TestOpsAddonsHibernateClosedRequest(t *testing.T) {
	h := &Handler{}
	addon := uuid.NewString()
	for name, req := range map[string]operatorOperationRequest{
		"no project":      {DryRun: true, Args: map[string]string{"target": addon}},
		"no target":       {DryRun: true, Scope: map[string]string{"project": "fixture"}},
		"target not uuid": {DryRun: true, Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": "pg-kernel09b"}},
		"apply no plan":   {Reason: "r", IdempotencyKey: "k", Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": addon}},
		"apply no key":    {Reason: "r", Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": addon, "expected_plan": "h"}},
		"apply no reason": {IdempotencyKey: "k", Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": addon, "expected_plan": "h"}},
	} {
		resp, status := h.handleOpsAddonsHibernate(context.Background(), req, opsActor{})
		require.Equal(t, http.StatusBadRequest, status, name)
		require.Equal(t, "failed", resp.Status, name)
		require.Equal(t, opsAddonsHibernateOperation, resp.Operation)
	}
	// A well-formed request without its adapters is unavailable, not silently planned.
	_, status := h.handleOpsAddonsHibernate(context.Background(), operatorOperationRequest{DryRun: true, Scope: map[string]string{"project": "fixture"}, Args: map[string]string{"target": addon}}, opsActor{})
	require.Equal(t, http.StatusServiceUnavailable, status)
}

func TestHibernationRefusalKeepsBoundForeignAndUnreadyAddonsRunning(t *testing.T) {
	project := uuid.New()
	ok := func() *types.DatabaseAddonWithBindings {
		return &types.DatabaseAddonWithBindings{DatabaseAddon: types.DatabaseAddon{ProjectID: project, Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusReady},
			Bindings: []types.DatabaseAddonBinding{{Status: types.DatabaseAddonBindingStatusDeleted}}}
	}
	status, _ := hibernationRefusal(ok(), project)
	require.Zero(t, status, "ready, same project, only deleted bindings")

	for name, tc := range map[string]struct {
		mutate func(*types.DatabaseAddonWithBindings)
		status int
	}{
		"other project": {func(a *types.DatabaseAddonWithBindings) { a.ProjectID = uuid.New() }, http.StatusNotFound},
		"redis":         {func(a *types.DatabaseAddonWithBindings) { a.Type = types.DatabaseAddonTypeRedis }, http.StatusUnprocessableEntity},
		"provisioning":  {func(a *types.DatabaseAddonWithBindings) { a.Status = types.DatabaseAddonStatusProvisioning }, http.StatusConflict},
		"retention":     {func(a *types.DatabaseAddonWithBindings) { a.Status = types.DatabaseAddonStatusPendingDeletion }, http.StatusConflict},
		"active": {func(a *types.DatabaseAddonWithBindings) {
			a.Bindings = append(a.Bindings, types.DatabaseAddonBinding{Status: types.DatabaseAddonBindingStatusActive})
		}, http.StatusConflict},
		"suspended": {func(a *types.DatabaseAddonWithBindings) {
			a.Bindings = append(a.Bindings, types.DatabaseAddonBinding{Status: types.DatabaseAddonBindingStatusSuspended})
		}, http.StatusConflict},
	} {
		a := ok()
		tc.mutate(a)
		got, why := hibernationRefusal(a, project)
		require.Equal(t, tc.status, got, name)
		require.NotEmpty(t, why, name)
	}
	got, _ := hibernationRefusal(nil, project)
	require.Equal(t, http.StatusNotFound, got)
}
