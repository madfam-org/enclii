package api

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Immutable intent/result receipts outlive the Job's TTL. A missing Job after
// intent is never recreated automatically, even after an uncertain API result.
func previousTargetRun(ctx context.Context, client kubernetes.Interface, namespace, name, requestSHA string) (*batchv1.Job, error) {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, name+"-intent", metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		_, receiptErr := client.CoreV1().ConfigMaps(namespace).Get(ctx, name+"-receipt", metav1.GetOptions{})
		_, jobErr := client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if kerrors.IsNotFound(jobErr) && kerrors.IsNotFound(receiptErr) {
			return nil, nil
		}
		return nil, workloadtargets.ErrConflict
	}
	if err != nil {
		return nil, workloadtargets.ErrUnavailable
	}
	if cm.UID == "" || cm.Immutable == nil || !*cm.Immutable || cm.Data["request_sha256"] != requestSHA {
		return nil, workloadtargets.ErrConflict
	}
	job, err := client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, workloadtargets.ErrCommitUnknown
	}
	receipt, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, name+"-receipt", metav1.GetOptions{})
	if err != nil {
		return nil, workloadtargets.ErrCommitUnknown
	}
	if receipt.Immutable == nil || !*receipt.Immutable || receipt.Data["request_sha256"] != requestSHA || job.UID == "" || receipt.Data["job_uid"] != string(job.UID) || receipt.Data["intent_uid"] != string(cm.UID) ||
		job.Annotations["enclii.dev/request-sha256"] != requestSHA {
		return nil, workloadtargets.ErrConflict
	}
	owned := false
	for _, owner := range job.OwnerReferences {
		if owner.Kind == "ConfigMap" && owner.Name == cm.Name && owner.UID == cm.UID {
			owned = true
		}
	}
	if !owned {
		return nil, workloadtargets.ErrConflict
	}
	return job, nil
}

func targetRunReceipt(name, namespace, requestSHA string, data map[string]string) *corev1.ConfigMap {
	yes := true
	values := map[string]string{"request_sha256": requestSHA}
	for k, v := range data {
		values[k] = v
	}
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace,
		Labels: map[string]string{"app.kubernetes.io/managed-by": "enclii-ops"}}, Immutable: &yes, Data: values}
}

func targetRunOutcomeError(name string) error {
	return fmt.Errorf("%w: inspect Job %s and immutable intent/result receipts; never rerun with another key before reconciliation", workloadtargets.ErrCommitUnknown, name)
}
