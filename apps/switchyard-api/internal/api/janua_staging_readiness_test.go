package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func readinessFixture(t *testing.T) (januaReadinessIO, januastage.Authority) {
	t.Helper()
	a := newJanuaSourceFixture(t, "").Held.Authority
	return januaReadinessIO{
		Held: func(context.Context, uuid.UUID) (januastage.Authority, error) {
			a.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return a, nil
		},
		Catalog: func(context.Context, string) (*db.ManagedDBPlan, error) {
			return &db.ManagedDBPlan{Code: "standard-0", Engine: "postgres", StorageGB: 1, ReplicaCount: 1, MaxConnections: 10, CPURequest: "100m", MemoryRequest: "256Mi", Available: true, PriceCentsMonth: 0}, nil
		},
		Native: func(context.Context, januastage.Authority) (januaReadinessNative, error) {
			return januaReadinessNative{StorageUID: "storage-fixture", StorageVersion: "1", PolicySHA256: strings.Repeat("a", 64)}, nil
		},
		Budget: func(context.Context, uuid.UUID) (januaReadinessBudget, error) {
			return januaReadinessBudget{Present: true, AmountCents: 10000, Currency: "USD"}, nil
		},
	}, a
}

func TestJanuaPrerequisiteDiagnosticCannotClearRuntimeGates(t *testing.T) {
	io, a := readinessFixture(t)
	result, err := io.observe(t.Context(), a.ID, "standard-0")
	require.NoError(t, err)
	require.True(t, result.ReadOnly)
	require.False(t, result.ApplySupported)
	require.False(t, result.Ready)
	require.True(t, result.NativePrerequisitesObserved)
	require.NotNil(t, result.MonthlyBudgetPresent)
	require.True(t, *result.MonthlyBudgetPresent)
	require.Len(t, result.MetadataSHA256, 64)
	require.Contains(t, result.Gates, "NATIVE_ALLOW_DENY_PACKET_PROBE_MISSING")
	require.Contains(t, result.Gates, "OPERATION_SPECIFIC_COST_APPROVAL_AND_FRESH_METERED_SPEND_MISSING")
	require.Len(t, result.Gates, 4) // Zero catalog price plus generic budget is no approval.
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "AmountCents")
	require.NotContains(t, string(encoded), "storage-fixture")
}

func TestJanuaPrerequisiteDiagnosticRefusesUnknownsAndBookendDrift(t *testing.T) {
	for _, scenario := range []string{"missing-reader", "stale-authority", "foreign-authority", "bad-catalog", "native-read-error", "budget-read-error", "no-budget", "throttled", "policy-drift", "budget-drift", "authority-drift", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			io, a := readinessFixture(t)
			calls := 0
			switch scenario {
			case "missing-reader":
				io.Held = nil
			case "stale-authority":
				io.Held = func(context.Context, uuid.UUID) (januastage.Authority, error) {
					a.ObservedAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
					return a, nil
				}
			case "foreign-authority":
				io.Held = func(context.Context, uuid.UUID) (januastage.Authority, error) { a.ID = uuid.New(); return a, nil }
			case "bad-catalog":
				io.Catalog = func(context.Context, string) (*db.ManagedDBPlan, error) { return nil, errors.New("unavailable") }
			case "native-read-error":
				io.Native = func(context.Context, januastage.Authority) (januaReadinessNative, error) {
					return januaReadinessNative{}, januaReadinessNativeRefusal("COMPLETE_STABLE_POLICY_INVENTORY_MISSING")
				}
			case "budget-read-error":
				io.Budget = func(context.Context, uuid.UUID) (januaReadinessBudget, error) {
					return januaReadinessBudget{}, errors.New("unavailable")
				}
			case "no-budget":
				io.Budget = func(context.Context, uuid.UUID) (januaReadinessBudget, error) { return januaReadinessBudget{}, nil }
			case "throttled":
				io.Budget = func(context.Context, uuid.UUID) (januaReadinessBudget, error) {
					return januaReadinessBudget{Present: true, Throttled: true}, nil
				}
			case "policy-drift":
				io.Native = func(context.Context, januastage.Authority) (januaReadinessNative, error) {
					calls++
					return januaReadinessNative{StorageUID: "storage-fixture", PolicySHA256: strings.Repeat(string(rune('a'+calls)), 64)}, nil
				}
			case "budget-drift":
				io.Budget = func(context.Context, uuid.UUID) (januaReadinessBudget, error) {
					calls++
					return januaReadinessBudget{Present: true, AmountCents: int64(calls)}, nil
				}
			case "authority-drift":
				io.Held = func(context.Context, uuid.UUID) (januastage.Authority, error) {
					calls++
					a.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
					if calls > 1 {
						a.DeploymentUID = "replacement-fixture"
					}
					return a, nil
				}
			}
			ctx := t.Context()
			if scenario == "cancelled" {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			result, err := io.observe(ctx, a.ID, "standard-0")
			require.False(t, result.Ready)
			require.False(t, result.ApplySupported)
			if scenario == "missing-reader" || scenario == "stale-authority" || scenario == "foreign-authority" || scenario == "bad-catalog" || scenario == "cancelled" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if strings.HasSuffix(scenario, "drift") {
				require.Empty(t, result.MetadataSHA256)
				require.Contains(t, result.Gates, "PREREQUISITES_CHANGED_DURING_READ")
			}
			if scenario == "no-budget" {
				require.NotNil(t, result.MonthlyBudgetPresent)
				require.False(t, *result.MonthlyBudgetPresent)
				require.Contains(t, result.Gates, "PROJECT_MONTHLY_BUDGET_MISSING")
			}
			if scenario == "throttled" {
				require.NotNil(t, result.NonProductionThrottled)
				require.True(t, *result.NonProductionThrottled)
				require.Contains(t, result.Gates, "PROJECT_NON_PRODUCTION_THROTTLED")
			}
			if scenario == "budget-read-error" {
				require.Contains(t, result.Gates, "PROJECT_BUDGET_READ_UNAVAILABLE")
				require.Nil(t, result.NonProductionThrottled)
				require.Nil(t, result.MonthlyBudgetPresent)
			}
			if scenario == "native-read-error" {
				require.False(t, result.NativePrerequisitesObserved)
				require.Contains(t, result.Gates, "COMPLETE_STABLE_POLICY_INVENTORY_MISSING")
			}
		})
	}
}

func TestJanuaReadinessNativeReadsWithoutSecretsOrWrites(t *testing.T) {
	f := newJanuaAdmissionFixture(t)
	source, err := f.h.readJanuaStageAdmissionSource(t.Context(), f.service, "standard-0")
	require.NoError(t, err)
	a := januastage.Authority{ProjectID: f.project}
	for _, ns := range source.Namespaces {
		if ns.Name == januastage.Namespace {
			a.NamespaceUID = ns.UID
		}
	}
	f.client.ClearActions()
	result, err := f.h.readJanuaReadinessNative(t.Context(), a)
	require.NoError(t, err)
	require.NotEmpty(t, result.PolicySHA256)
	require.Len(t, result.Namespaces, 2)
	for _, action := range f.client.Actions() {
		require.Contains(t, []string{"get", "list"}, action.GetVerb())
		require.NotEqual(t, "secrets", action.GetResource().Resource)
		require.NotEqual(t, "nodes", action.GetResource().Resource)
		require.NotEqual(t, "pods", action.GetResource().Resource)
	}
	_, err = f.client.NetworkingV1().NetworkPolicies(januastage.Namespace).Create(t.Context(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "unreviewed-policy", Namespace: januastage.Namespace, UID: "extra-policy-fixture", ResourceVersion: "1"}}, metav1.CreateOptions{})
	require.NoError(t, err)
	after, err := f.h.readJanuaReadinessNative(t.Context(), a)
	require.NoError(t, err)
	require.NotEqual(t, result.PolicySHA256, after.PolicySHA256)
	require.Equal(t, 1, after.Namespaces[0].OtherPoliciesRead)
}

func TestJanuaReadinessBudgetReadsOnlyProjectMetadata(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "present"}[present], func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			project := uuid.New()
			rows := sqlmock.NewRows([]string{"id", "amount_cents", "currency", "updated_at", "throttled", "last_evaluator"})
			if present {
				rows.AddRow(uuid.NewString(), 10000, "USD", time.Now(), true, time.Now())
			} else {
				rows.AddRow(nil, nil, nil, nil, false, nil)
			}
			mock.ExpectQuery("SELECT b.id,b.amount_cents,b.currency,b.updated_at").WithArgs(project).WillReturnRows(rows)
			h := &Handler{repos: db.NewRepositories(database)}
			budget, err := h.readJanuaReadinessBudget(t.Context(), project)
			require.NoError(t, err)
			require.Equal(t, present, budget.Present)
			require.Equal(t, present, budget.Throttled)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestJanuaReadinessConstructorHasNoUnconfiguredFallback(t *testing.T) {
	var missing *Handler
	_, err := missing.observeJanuaStagingPrerequisites(t.Context(), uuid.New(), "standard-0")
	require.Error(t, err)
	_, err = (&Handler{}).observeJanuaStagingPrerequisites(t.Context(), uuid.New(), "standard-0")
	require.Error(t, err)
}
