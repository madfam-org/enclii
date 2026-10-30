package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// A separate immutable claim bounds each closed phase to one execution. The
// phase is server-selected; replay returns metadata only, never effect authority.
func ClaimJanuaStagingPhase(ctx context.Context, client kubernetes.Interface, o JanuaStagingOperation, journalUID k8stypes.UID, phase, reason string) (bool, error) {
	if !januaStageReasonValid(reason) {
		return false, fmt.Errorf("bounded Janua staging phase reason required")
	}
	if phase != "reserve" && phase != "namespaces" && phase != "provision" && phase != "custody" {
		return false, fmt.Errorf("closed Janua staging phase refused")
	}
	if err := verifyJanuaClaim(ctx, client, o, journalUID); err != nil {
		return false, err
	}
	desired := o.claimObject()
	desired.Name += "-" + phase + "-claim"
	desired.Data["journal_uid"] = string(journalUID)
	desired.Data["reason"] = strings.TrimSpace(reason)
	api := client.CoreV1().ConfigMaps("enclii")
	actual, err := api.Get(ctx, desired.Name, metav1.GetOptions{})
	if err == nil {
		if !januaJournalExact(actual, desired) {
			return false, fmt.Errorf("Janua staging phase conflict")
		}
		return false, nil
	}
	if !k8serrors.IsNotFound(err) {
		return false, fmt.Errorf("Janua staging phase unavailable")
	}
	actual, err = api.Create(ctx, desired, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: JanuaStagingProfile})
	if err != nil || !januaJournalExact(actual, desired) {
		return false, fmt.Errorf("Janua staging phase claim uncertain; no effect retry")
	}
	return true, nil
}

func verifyJanuaClaim(ctx context.Context, client kubernetes.Interface, o JanuaStagingOperation, journalUID k8stypes.UID) error {
	if client == nil || !o.valid() || journalUID == "" {
		return fmt.Errorf("Janua staging operation identity refused")
	}
	actual, err := client.CoreV1().ConfigMaps("enclii").Get(ctx, o.journalName(), metav1.GetOptions{})
	if err != nil || !januaJournalExact(actual, o.claimObject()) || actual.UID != journalUID {
		return fmt.Errorf("Janua staging operation changed")
	}
	return nil
}

func ReadJanuaStagingPhase(ctx context.Context, client kubernetes.Interface, o JanuaStagingOperation, journalUID k8stypes.UID, phase, reason string) (bool, error) {
	if !januaStageReasonValid(reason) {
		return false, fmt.Errorf("bounded Janua staging phase reason required")
	}
	if phase != "reserve" && phase != "namespaces" && phase != "provision" && phase != "custody" {
		return false, fmt.Errorf("closed Janua staging phase refused")
	}
	if err := verifyJanuaClaim(ctx, client, o, journalUID); err != nil {
		return false, err
	}
	desired := o.claimObject()
	desired.Name += "-" + phase + "-claim"
	desired.Data["journal_uid"] = string(journalUID)
	desired.Data["reason"] = strings.TrimSpace(reason)
	actual, err := client.CoreV1().ConfigMaps("enclii").Get(ctx, desired.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil || !januaJournalExact(actual, desired) {
		return false, fmt.Errorf("Janua staging phase identity unavailable")
	}
	return true, nil
}

func WriteJanuaStagingNamespaces(ctx context.Context, client kubernetes.Interface, o JanuaStagingOperation, journalUID k8stypes.UID, binding JanuaStagingNamespaceBinding) error {
	if binding.ApplicationUID == "" || binding.AddonUID == "" || binding.ApplicationUID == binding.AddonUID {
		return fmt.Errorf("Janua staging namespace birth identity refused")
	}
	if err := verifyJanuaClaim(ctx, client, o, journalUID); err != nil {
		return err
	}
	body, _ := json.Marshal(binding)
	desired := o.claimObject()
	desired.Name += "-namespaces"
	desired.Data["journal_uid"] = string(journalUID)
	desired.Data["namespaces.json"] = string(body)
	actual, err := client.CoreV1().ConfigMaps("enclii").Create(ctx, desired, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: JanuaStagingProfile})
	if err != nil || !januaJournalExact(actual, desired) {
		return fmt.Errorf("Janua staging namespace receipt uncertain; no adoption")
	}
	return nil
}

func ReadJanuaStagingNamespaces(ctx context.Context, client kubernetes.Interface, o JanuaStagingOperation, journalUID k8stypes.UID) (JanuaStagingNamespaceBinding, error) {
	var binding JanuaStagingNamespaceBinding
	if err := verifyJanuaClaim(ctx, client, o, journalUID); err != nil {
		return binding, err
	}
	actual, err := client.CoreV1().ConfigMaps("enclii").Get(ctx, o.journalName()+"-namespaces", metav1.GetOptions{})
	if err != nil || actual == nil || json.Unmarshal([]byte(actual.Data["namespaces.json"]), &binding) != nil || binding.ApplicationUID == "" || binding.AddonUID == "" || binding.ApplicationUID == binding.AddonUID {
		return JanuaStagingNamespaceBinding{}, fmt.Errorf("Janua staging namespace birth proof unavailable")
	}
	body, _ := json.Marshal(binding)
	desired := o.claimObject()
	desired.Name += "-namespaces"
	desired.Data["journal_uid"] = string(journalUID)
	desired.Data["namespaces.json"] = string(body)
	if !januaJournalExact(actual, desired) {
		return JanuaStagingNamespaceBinding{}, fmt.Errorf("Janua staging namespace birth proof changed")
	}
	return binding, nil
}

func januaStageReasonValid(reason string) bool {
	return len(strings.TrimSpace(reason)) >= 8 && len(reason) <= 256 && !strings.ContainsAny(reason, "\r\n\x00")
}
