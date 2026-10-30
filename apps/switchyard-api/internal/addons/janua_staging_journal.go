package addons

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// These opaque references must be resolved from current persisted ownership by
// the platform adapter, never accepted wholesale from HTTP input or a preview.
// No credential, certificate private key or human email belongs in the journal.
type JanuaStagingOperation struct {
	OperationID    uuid.UUID `json:"operation_id"`
	ActorID        uuid.UUID `json:"actor_id"`
	ProjectID      uuid.UUID `json:"project_id"`
	EnvironmentID  uuid.UUID `json:"environment_id"`
	RegistrationID uuid.UUID `json:"registration_id"`
	ServiceID      uuid.UUID `json:"service_id"`
	AddonID        uuid.UUID `json:"addon_id"`
	PlanSHA256     string    `json:"plan_sha256"`
}

func (o JanuaStagingOperation) valid() bool {
	seen := map[uuid.UUID]bool{}
	for _, id := range []uuid.UUID{o.OperationID, o.ActorID, o.ProjectID, o.EnvironmentID, o.RegistrationID, o.ServiceID, o.AddonID} {
		if id == uuid.Nil || seen[id] {
			return false
		}
		seen[id] = true
	}
	decoded, err := hex.DecodeString(o.PlanSHA256)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == o.PlanSHA256
}

// Retained in the control namespace rather than owned by the disposable stage
// namespace. A recreated namespace cannot remove or reuse this operation claim.
func (o JanuaStagingOperation) journalName() string {
	return "janua-staging-batch-a-" + o.ProjectID.String()
}

func (o JanuaStagingOperation) claimObject() *corev1.ConfigMap {
	body, _ := json.Marshal(o)
	yes := true
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: o.journalName(), Namespace: "enclii", Labels: map[string]string{"enclii.dev/staging-profile": JanuaStagingProfile, LabelProjectID: o.ProjectID.String(), LabelAddonID: o.AddonID.String()}}, Immutable: &yes, Data: map[string]string{"operation.json": string(body)}}
}

// ClaimJanuaStagingOperation returns created=false on an exact replay. Such a
// replay is READ-ONLY; it must not rerun a provision or custody effect. A lost
// create response therefore holds, rather than adopting the discovered object.
func ClaimJanuaStagingOperation(ctx context.Context, client kubernetes.Interface, o JanuaStagingOperation) (created bool, journalUID k8stypes.UID, err error) {
	if client == nil || !o.valid() {
		return false, "", fmt.Errorf("closed Janua operation identity refused")
	}
	api, desired := client.CoreV1().ConfigMaps("enclii"), o.claimObject()
	actual, readErr := api.Get(ctx, desired.Name, metav1.GetOptions{})
	if readErr == nil {
		if !januaJournalExact(actual, desired) {
			return false, "", fmt.Errorf("Janua staging operation conflict; no adoption")
		}
		return false, actual.UID, nil
	}
	if !k8serrors.IsNotFound(readErr) {
		return false, "", fmt.Errorf("Janua staging operation unavailable")
	}
	actual, err = api.Create(ctx, desired, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: JanuaStagingProfile})
	if err != nil || !januaJournalExact(actual, desired) {
		return false, "", fmt.Errorf("Janua staging operation claim uncertain; inspect same operation")
	}
	return true, actual.UID, nil
}

func januaJournalExact(actual, desired *corev1.ConfigMap) bool {
	return actual != nil && desired != nil && actual.Name == desired.Name && actual.Namespace == desired.Namespace && actual.UID != "" && actual.ResourceVersion != "" && actual.DeletionTimestamp == nil && actual.Immutable != nil && *actual.Immutable && len(actual.OwnerReferences) == 0 && len(actual.BinaryData) == 0 && reflect.DeepEqual(actual.Labels, desired.Labels) && reflect.DeepEqual(actual.Data, desired.Data)
}

type JanuaStagingNamespaceBinding struct {
	ApplicationUID k8stypes.UID `json:"application_namespace_uid"`
	AddonUID       k8stypes.UID `json:"addon_namespace_uid"`
}

type JanuaStagingBirthReceipt struct {
	JanuaStagingNamespaceBinding
	ClusterUID             k8stypes.UID   `json:"cluster_uid"`
	RuntimeSecretUID       k8stypes.UID   `json:"runtime_secret_uid"`
	RuntimeResourceVersion string         `json:"runtime_resource_version"`
	PolicyUIDs             []k8stypes.UID `json:"policy_uids"`
}

// A birth receipt contains identity only. Secret resourceVersion is retained
// after owner binding and must match again before any custody projection.
func (r JanuaStagingBirthReceipt) valid() bool {
	if r.ApplicationUID == "" || r.AddonUID == "" || r.ApplicationUID == r.AddonUID || r.ClusterUID == "" || r.RuntimeSecretUID == "" || r.ClusterUID == r.RuntimeSecretUID || r.RuntimeResourceVersion == "" || len(r.PolicyUIDs) != 3 {
		return false
	}
	seen := map[k8stypes.UID]bool{}
	for _, uid := range r.PolicyUIDs {
		if uid == "" || seen[uid] {
			return false
		}
		seen[uid] = true
	}
	return true
}

func WriteJanuaStagingBirthReceipt(ctx context.Context, client kubernetes.Interface, o JanuaStagingOperation, journalUID k8stypes.UID, receipt JanuaStagingBirthReceipt) error {
	if client == nil || !o.valid() || journalUID == "" || !receipt.valid() {
		return fmt.Errorf("Janua staging birth identity refused")
	}
	api := client.CoreV1().ConfigMaps("enclii")
	claim, err := api.Get(ctx, o.journalName(), metav1.GetOptions{})
	if err != nil || !januaJournalExact(claim, o.claimObject()) || claim.UID != journalUID {
		return fmt.Errorf("Janua staging operation changed")
	}
	body, _ := json.Marshal(receipt)
	desired := o.claimObject()
	desired.Name += "-birth"
	desired.Data["journal_uid"] = string(journalUID)
	desired.Data["birth.json"] = string(body)
	created, err := api.Create(ctx, desired, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: JanuaStagingProfile})
	if err != nil || !januaJournalExact(created, desired) {
		return fmt.Errorf("Janua staging birth persistence uncertain; no effect retry")
	}
	return nil
}

func ReadJanuaStagingBirthReceipt(ctx context.Context, client kubernetes.Interface, o JanuaStagingOperation, journalUID k8stypes.UID) (JanuaStagingBirthReceipt, error) {
	var receipt JanuaStagingBirthReceipt
	if client == nil || !o.valid() || journalUID == "" {
		return receipt, fmt.Errorf("Janua staging operation identity refused")
	}
	api := client.CoreV1().ConfigMaps("enclii")
	claim, err := api.Get(ctx, o.journalName(), metav1.GetOptions{})
	if err != nil || !januaJournalExact(claim, o.claimObject()) || claim.UID != journalUID {
		return receipt, fmt.Errorf("Janua staging operation changed")
	}
	actual, err := api.Get(ctx, o.journalName()+"-birth", metav1.GetOptions{})
	if err != nil || actual == nil || json.Unmarshal([]byte(actual.Data["birth.json"]), &receipt) != nil || !receipt.valid() {
		return JanuaStagingBirthReceipt{}, fmt.Errorf("Janua staging birth proof unavailable")
	}
	desired := o.claimObject()
	desired.Name += "-birth"
	desired.Data["journal_uid"] = string(journalUID)
	body, _ := json.Marshal(receipt)
	desired.Data["birth.json"] = string(body)
	if !januaJournalExact(actual, desired) {
		return JanuaStagingBirthReceipt{}, fmt.Errorf("Janua staging birth proof changed")
	}
	return receipt, nil
}

// Metadata-only proof of the one atomic database+CA initialization. A missing
// receipt following an uncertain Vault write remains held; never overwrite it.
func WriteJanuaStagingCustodyReceipt(ctx context.Context, client kubernetes.Interface, o JanuaStagingOperation, journalUID k8stypes.UID, custody *JanuaStagingCustody, vaultVersion int) error {
	if custody == nil || vaultVersion <= 0 {
		return fmt.Errorf("Janua staging custody persistence refused")
	}
	decoded, err := hex.DecodeString(custody.PublicCASHA256)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("Janua staging public CA metadata refused")
	}
	birth, err := ReadJanuaStagingBirthReceipt(ctx, client, o, journalUID)
	if err != nil || birth.ClusterUID != custody.ClusterUID || birth.RuntimeSecretUID != custody.RuntimeUID {
		return fmt.Errorf("Janua staging custody birth identity changed")
	}
	desired := o.claimObject()
	desired.Name += "-custody"
	desired.Data["journal_uid"] = string(journalUID)
	desired.Data["vault_path"] = "secret/janua-staging"
	desired.Data["vault_version"] = fmt.Sprint(vaultVersion)
	desired.Data["cluster_uid"] = string(custody.ClusterUID)
	desired.Data["runtime_uid"] = string(custody.RuntimeUID)
	desired.Data["public_ca_sha256"] = custody.PublicCASHA256
	created, err := client.CoreV1().ConfigMaps("enclii").Create(ctx, desired, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: JanuaStagingProfile})
	if err != nil || !januaJournalExact(created, desired) {
		return fmt.Errorf("Janua staging custody receipt uncertain; operation held")
	}
	return nil
}
