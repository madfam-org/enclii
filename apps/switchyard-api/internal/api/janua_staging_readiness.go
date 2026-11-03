package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
)

// Deliberately not a provisioning/admission receipt or an HTTP input. These
// concrete prerequisite reads cannot establish packet isolation, placement or
// operation-specific cost approval. No kernel gate accepts this diagnostic.
type januaReadinessDiagnostic struct {
	Schema                      string    `json:"schema"`
	AuthorityID                 uuid.UUID `json:"authority_id"`
	ReadOnly                    bool      `json:"read_only"`
	ApplySupported              bool      `json:"apply_supported"`
	Ready                       bool      `json:"ready"`
	CatalogPlan                 string    `json:"catalog_plan"`
	MonthlyBudgetPresent        *bool     `json:"monthly_budget_present"`
	NonProductionThrottled      *bool     `json:"non_production_throttled"`
	NativePrerequisitesObserved bool      `json:"native_prerequisites_observed"`
	MetadataSHA256              string    `json:"metadata_sha256,omitempty"`
	ObservedAt                  string    `json:"observed_at"`
	Gates                       []string  `json:"unresolved_gates"`
}

type januaReadinessNative struct {
	Namespaces                 []januaStageNamespaceRead
	PolicySHA256               string
	StorageUID, StorageVersion string
	Peers                      []januaStageAPIPeer
	EndpointVersions           []string
	OperatorSHA256             string
}

type januaReadinessBudget struct {
	Present, Throttled       bool
	ID, Currency             string
	AmountCents              int64
	UpdatedAt, LastEvaluator time.Time
}

type januaReadinessIO struct {
	Held    func(context.Context, uuid.UUID) (januastage.Authority, error)
	Catalog func(context.Context, string) (*db.ManagedDBPlan, error)
	Native  func(context.Context, januastage.Authority) (januaReadinessNative, error)
	Budget  func(context.Context, uuid.UUID) (januaReadinessBudget, error)
}

type januaReadinessFrame struct {
	Authority   januastage.Authority
	Catalog     *db.ManagedDBPlan
	Native      januaReadinessNative
	Budget      januaReadinessBudget
	BudgetKnown bool
}

func januaReadinessHash(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (a januaReadinessIO) read(ctx context.Context, id uuid.UUID, code string) (januaReadinessFrame, []string, error) {
	var frame januaReadinessFrame
	authority, err := a.Held(ctx, id)
	stamp, stampErr := time.Parse(time.RFC3339Nano, authority.ObservedAt)
	if err != nil || januastage.ValidateAuthorityShape(authority) != nil || authority.ID != id || stampErr != nil || time.Since(stamp) < 0 || time.Since(stamp) > 30*time.Second {
		return frame, nil, januastage.ErrRefused
	}
	frame.Authority = authority
	frame.Authority.ObservedAt = ""
	var gates []string
	frame.Catalog, err = a.Catalog(ctx, code)
	if err != nil || !januaStageCatalogPlanValid(frame.Catalog) || frame.Catalog.Code != code || frame.Catalog.MaxConnections <= 0 {
		return frame, nil, januastage.ErrRefused
	}
	frame.Native, err = a.Native(ctx, authority)
	if err != nil {
		// The reader returns only a fixed code, never raw Kubernetes error bodies.
		var refused januaReadinessNativeRefusal
		if errors.As(err, &refused) {
			gates = append(gates, string(refused))
		} else {
			gates = append(gates, "NATIVE_PREREQUISITE_READ_REFUSED")
		}
		frame.Native = januaReadinessNative{}
	}
	frame.Budget, err = a.Budget(ctx, authority.ProjectID)
	frame.BudgetKnown = err == nil
	if err != nil {
		frame.Budget = januaReadinessBudget{}
		gates = append(gates, "PROJECT_BUDGET_READ_UNAVAILABLE")
	} else {
		if !frame.Budget.Present {
			gates = append(gates, "PROJECT_MONTHLY_BUDGET_MISSING")
		}
		if frame.Budget.Throttled {
			gates = append(gates, "PROJECT_NON_PRODUCTION_THROTTLED")
		}
	}
	return frame, gates, nil
}

// observe uses only server-owned live readers, then repeats them. There is no
// uploaded-JSON parser, caller boolean, catalog-zero shortcut or execution path.
func (a januaReadinessIO) observe(ctx context.Context, id uuid.UUID, code string) (januaReadinessDiagnostic, error) {
	result := januaReadinessDiagnostic{Schema: "enclii-janua-staging-prerequisites-v1", AuthorityID: id, ReadOnly: true, CatalogPlan: code,
		Gates: []string{"JOURNALLED_CLOSED_NETWORK_OPERATION_MISSING", "NATIVE_ALLOW_DENY_PACKET_PROBE_MISSING", "EXACT_ALLOCATION_SCHEDULER_AND_RETAINED_STORAGE_PLACEMENT_MISSING", "OPERATION_SPECIFIC_COST_APPROVAL_AND_FRESH_METERED_SPEND_MISSING"}}
	if id == uuid.Nil || code == "" || a.Held == nil || a.Catalog == nil || a.Native == nil || a.Budget == nil {
		return result, januastage.ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	before, gates, err := a.read(ctx, id, code)
	if err != nil {
		return result, err
	}
	after, currentGates, err := a.read(ctx, id, code)
	if err != nil || ctx.Err() != nil {
		return result, januastage.ErrRefused
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(gates, currentGates) {
		result.Gates = append(result.Gates, "PREREQUISITES_CHANGED_DURING_READ")
	} else {
		result.Gates = append(result.Gates, gates...)
		result.MetadataSHA256 = januaReadinessHash(after)
		if after.BudgetKnown {
			result.MonthlyBudgetPresent = &after.Budget.Present
			result.NonProductionThrottled = &after.Budget.Throttled
		}
		result.NativePrerequisitesObserved = after.Native.StorageUID != ""
		for _, ns := range after.Native.Namespaces {
			if ns.OtherPoliciesRead > 0 {
				result.Gates = append(result.Gates, "ADDITIVE_POLICY_SET_REQUIRES_JOURNALLED_NETWORK_REVIEW")
				break
			}
		}
	}
	sort.Strings(result.Gates)
	result.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return result, nil
}

// Internal constructor only. Missing authority/custody/execution adapters stay
// disconnected; this adds no route or automatic staging action.
func (h *Handler) observeJanuaStagingPrerequisites(ctx context.Context, id uuid.UUID, code string) (januaReadinessDiagnostic, error) {
	if h == nil || h.repos == nil || h.repos.DB() == nil || h.repos.ManagedDBPlans == nil || h.k8sClient == nil || h.k8sClient.Kube() == nil {
		return januaReadinessDiagnostic{}, januastage.ErrRefused
	}
	return (januaReadinessIO{Held: h.readJanuaInertAuthority, Catalog: h.repos.ManagedDBPlans.GetByCode, Native: h.readJanuaReadinessNative, Budget: h.readJanuaReadinessBudget}).observe(ctx, id, code)
}
