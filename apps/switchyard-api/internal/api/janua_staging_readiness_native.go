package api

import (
	"context"
	"database/sql"
	"reflect"
	"sort"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type januaReadinessNativeRefusal string

func (e januaReadinessNativeRefusal) Error() string { return string(e) }

func (h *Handler) readJanuaReadinessNative(ctx context.Context, a januastage.Authority) (januaReadinessNative, error) {
	var result januaReadinessNative
	var policyProjection []struct {
		Namespace, Name, UID, Version string
		Spec                          networkingv1.NetworkPolicySpec
	}
	for _, name := range []string{januastage.Namespace, januastagedb.Namespace(a.ProjectID)} {
		policies, e := h.k8sClient.Kube().NetworkingV1().NetworkPolicies(name).List(ctx, metav1.ListOptions{Limit: 64})
		if e != nil || policies.Continue != "" || policies.RemainingItemCount != nil && *policies.RemainingItemCount > 0 || len(policies.Items) > 64 {
			return result, januaReadinessNativeRefusal("COMPLETE_STABLE_POLICY_INVENTORY_MISSING")
		}
		ns, err := h.readJanuaStageNamespace(ctx, name, a.ProjectID)
		if err != nil || ns.State != "OWNED" || ns.ResourceVersion == "" || !ns.SignatureAdmissionLabel || ns.DNSOnlyBaseline != "EXACT_OWNED" || ns.BaselineResourceVersion == "" || ns.LegacySharedDataAccessLabel || ns.LegacySharedPostgresConsumerLabel || (name == januastage.Namespace && ns.UID != a.NamespaceUID) || (name != januastage.Namespace && ns.UID == a.NamespaceUID) {
			return result, januaReadinessNativeRefusal("OWNED_NAMESPACE_DNS_AND_SIGNATURE_BASELINE_MISSING")
		}
		if len(policies.Items) != ns.PoliciesRead {
			return result, januaReadinessNativeRefusal("COMPLETE_STABLE_POLICY_INVENTORY_MISSING")
		}
		baseline := false
		for _, p := range policies.Items {
			if p.UID == "" || p.ResourceVersion == "" || p.DeletionTimestamp != nil || p.Namespace != name {
				return result, januaReadinessNativeRefusal("COMPLETE_STABLE_POLICY_INVENTORY_MISSING")
			}
			if p.Name == "default-deny" {
				if string(p.UID) != ns.BaselineUID || p.ResourceVersion != ns.BaselineResourceVersion || !reflect.DeepEqual(p.Spec, januaStageDNSOnlyBaseline()) {
					return result, januaReadinessNativeRefusal("COMPLETE_STABLE_POLICY_INVENTORY_MISSING")
				}
				baseline = true
			}
			policyProjection = append(policyProjection, struct {
				Namespace, Name, UID, Version string
				Spec                          networkingv1.NetworkPolicySpec
			}{name, p.Name, string(p.UID), p.ResourceVersion, p.Spec})
		}
		if !baseline {
			return result, januaReadinessNativeRefusal("COMPLETE_STABLE_POLICY_INVENTORY_MISSING")
		}
		result.Namespaces = append(result.Namespaces, ns)
	}
	sort.Slice(policyProjection, func(i, j int) bool {
		if policyProjection[i].Namespace == policyProjection[j].Namespace {
			return policyProjection[i].Name < policyProjection[j].Name
		}
		return policyProjection[i].Namespace < policyProjection[j].Namespace
	})
	result.PolicySHA256 = januaReadinessHash(policyProjection)
	peers, versions, err := h.readJanuaStageAPIPeers(ctx)
	if err != nil {
		return result, januaReadinessNativeRefusal("CANONICAL_KUBERNETES_API_PEERS_MISSING")
	}
	result.Peers, result.EndpointVersions = peers, versions
	selector, refs, err := h.readJanuaStageOperator(ctx)
	if err != nil {
		return result, januaReadinessNativeRefusal("CURRENT_AVAILABLE_CNPG_OPERATOR_SELECTOR_MISSING")
	}
	result.OperatorSHA256 = januaReadinessHash(struct {
		Selector metav1.LabelSelector
		Versions []string
	}{selector, refs})
	storage, err := h.k8sClient.Kube().StorageV1().StorageClasses().Get(ctx, "longhorn-replicated", metav1.GetOptions{})
	if err != nil || storage.UID == "" || storage.ResourceVersion == "" || storage.DeletionTimestamp != nil || storage.ReclaimPolicy == nil || *storage.ReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
		return result, januaReadinessNativeRefusal("CURRENT_RETAIN_STORAGE_CLASS_IDENTITY_MISSING")
	}
	result.StorageUID, result.StorageVersion = string(storage.UID), storage.ResourceVersion
	return result, nil
}

// Existing Waybill records only. A project ceiling/evaluator timestamp is not
// an approval/reservation for this operation or proof of current metered spend.
// One statement observes the monthly row, throttle and evaluator consistently;
// no account, usage payload, credential or external cost response is read.
func (h *Handler) readJanuaReadinessBudget(ctx context.Context, project uuid.UUID) (januaReadinessBudget, error) {
	const query = `SELECT b.id,b.amount_cents,b.currency,b.updated_at,
 EXISTS(SELECT 1 FROM waybill_throttles WHERE project_id=$1 AND env_scope='non-production' AND cleared_at IS NULL),
 (SELECT MAX(started_at) FROM budget_evaluator_runs)
 FROM (SELECT 1) AS one LEFT JOIN budgets b ON b.project_id=$1 AND b.period='monthly'`
	var id, currency sql.NullString
	var amount sql.NullInt64
	var updated, evaluator sql.NullTime
	var throttled bool
	if err := h.repos.DB().QueryRowContext(ctx, query, project).Scan(&id, &amount, &currency, &updated, &throttled, &evaluator); err != nil {
		return januaReadinessBudget{}, err
	}
	if id.Valid && (!amount.Valid || amount.Int64 <= 0 || !currency.Valid || len(currency.String) != 3 || !updated.Valid) {
		return januaReadinessBudget{}, januastage.ErrRefused
	}
	return januaReadinessBudget{Present: id.Valid, ID: id.String, AmountCents: amount.Int64, Currency: currency.String, UpdatedAt: updated.Time, LastEvaluator: evaluator.Time, Throttled: throttled}, nil
}
