package k8s

import (
	"context"
	"fmt"
	"reflect"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	initialPreparedUID = "enclii.dev/bootstrap-prepared-uid"
	initialPreparedSHA = "enclii.dev/bootstrap-prepared-sha256"
)

func boundedInitialProfile(profile string) bool {
	return IsProvenanceReadProfile(profile) || profile == PortalReconstructionCloseProfile
}

// The canonical pre-UID intent commits the complete original reviewed identity
// before any bounded Job exists, without changing existing plan hashes. A raw
// Job's credential/spec fingerprint alone cannot prove its old public identity.
func (c *Client) readInitialPreparation(ctx context.Context, p IsolatedInitialJobPlan, expected initialIntent) (*corev1.ConfigMap, error) {
	cm, err := c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Get(ctx, p.jobName()+"-prepared", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var original initialIntent
	if validateInitialRecord(cm, p, "-prepared", &original) != nil || !initialUID.MatchString(string(cm.UID)) ||
		expected.JobUID != "" || expected.CreationResourceVersion != "" || !reflect.DeepEqual(original, expected) {
		return nil, fmt.Errorf("original bounded Job preparation conflicts; held")
	}
	return cm, nil
}

func bindInitialPreparation(job *batchv1.Job, cm *corev1.ConfigMap) {
	job.Annotations[initialPreparedUID] = string(cm.UID)
	job.Annotations[initialPreparedSHA] = cm.Data["sha256"]
}

// Creation is attempted once. An uncertain write is read only on a later
// same-key request; it never falls back to a replacement or a new identity.
func (c *Client) retainInitialPreparation(ctx context.Context, p IsolatedInitialJobPlan, intent initialIntent) (*corev1.ConfigMap, error) {
	cm, err := c.readInitialPreparation(ctx, p, intent)
	if err == nil {
		return cm, nil
	}
	if !k8serrors.IsNotFound(err) {
		return nil, err
	}
	record, err := initialRecord(p, "-prepared", intent)
	if err != nil {
		return nil, err
	}
	if _, err = c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Create(ctx, record, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("bounded Job preparation creation uncertain; inspect same operation")
	}
	return c.readInitialPreparation(ctx, p, intent)
}
