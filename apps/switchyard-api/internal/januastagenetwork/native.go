package januastagenetwork

import (
	"context"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type NativeAdapter struct {
	Kube    kubernetes.Interface
	Dynamic dynamic.Interface
	// Fresh must independently reread the held objects/source and exact current
	// canonical peers/operator/baseline context; caller JSON cannot implement it.
	Fresh func(context.Context, Plan) error
}

func (n NativeAdapter) Get(ctx context.Context, o Object) (runtime.Object, error) {
	switch x := o.Body.(type) {
	case *corev1.Namespace:
		return n.Kube.CoreV1().Namespaces().Get(ctx, x.Name, metav1.GetOptions{})
	case *networkingv1.NetworkPolicy:
		return n.Kube.NetworkingV1().NetworkPolicies(x.Namespace).Get(ctx, x.Name, metav1.GetOptions{})
	}
	return nil, ErrRefused
}
func (n NativeAdapter) Create(ctx context.Context, o Object) (runtime.Object, error) {
	options := metav1.CreateOptions{FieldManager: Profile, FieldValidation: metav1.FieldValidationStrict}
	switch x := o.Body.(type) {
	case *corev1.Namespace:
		return n.Kube.CoreV1().Namespaces().Create(ctx, x.DeepCopy(), options)
	case *networkingv1.NetworkPolicy:
		return n.Kube.NetworkingV1().NetworkPolicies(x.Namespace).Create(ctx, x.DeepCopy(), options)
	}
	return nil, ErrRefused
}
func (n NativeAdapter) Guard(ctx context.Context, p Plan, r Receipt) error {
	if n.Kube == nil || n.Dynamic == nil || n.Fresh == nil || p.Validate() != nil || n.Fresh(ctx, p) != nil || len(r.Objects) > len(p.Objects()) {
		return ErrRefused
	}
	expected := map[string]ObjectReceipt{}
	for i, x := range r.Objects {
		if x.Index != i || x.Key != p.Objects()[i].Key || x.UID == "" || x.ResourceVersion == "" || x.SHA256 != p.ObjectSHA256[i] {
			return ErrRefused
		}
		expected[x.Key] = x
	}
	for _, o := range p.Objects() {
		actual, e := n.Get(ctx, o)
		retained, exists := expected[o.Key]
		if !exists {
			if !apierrors.IsNotFound(e) {
				return ErrRefused
			}
			continue
		}
		if e != nil || !Matches(o.Body, actual) {
			return ErrRefused
		}
		uid, _, sha, e := Projection(actual)
		if e != nil || uid != retained.UID || sha != retained.SHA256 {
			return ErrRefused
		}
	}
	for _, ns := range []string{januastage.Namespace, namespace(p)} {
		_, present := expected["Namespace:/"+ns]
		if ns != januastage.Namespace && !present {
			continue
		}
		list, e := n.Kube.NetworkingV1().NetworkPolicies(ns).List(ctx, metav1.ListOptions{Limit: 32})
		if e != nil || list.Continue != "" || list.RemainingItemCount != nil && *list.RemainingItemCount > 0 || len(list.Items) > 32 {
			return ErrRefused
		}
		for _, x := range list.Items {
			if ns == januastage.Namespace && x.Name == "default-deny" {
				continue
			} // Fresh binds this exact original baseline.
			if _, ok := expected["NetworkPolicy:"+ns+"/"+x.Name]; !ok {
				return ErrRefused
			}
		}
	}
	{
		// The newly owned addon namespace must remain empty of executing or
		// latent workloads/volumes. This is not an inventory/qualification of
		// arbitrary Secret, ConfigMap, ServiceAccount or RBAC custody; later
		// credentials and runtime gates remain separate and unqualified.
		// Check these exact list permissions/API resources even before create,
		// so known discovery/RBAC failures do not leave a partial namespace.
		for _, key := range []string{"v1/pods", "v1/persistentvolumeclaims", "v1/replicationcontrollers", "apps/v1/deployments", "apps/v1/statefulsets", "apps/v1/daemonsets", "apps/v1/replicasets", "batch/v1/jobs", "batch/v1/cronjobs", "autoscaling/v2/horizontalpodautoscalers", "postgresql.cnpg.io/v1/clusters"} {
			parts := strings.Split(key, "/")
			gvr := schema.GroupVersionResource{Version: parts[0], Resource: parts[1]}
			if len(parts) == 3 {
				gvr = schema.GroupVersionResource{Group: parts[0], Version: parts[1], Resource: parts[2]}
			}
			list, e := n.Dynamic.Resource(gvr).Namespace(namespace(p)).List(ctx, metav1.ListOptions{Limit: 1})
			if e != nil || list.GetContinue() != "" || list.GetRemainingItemCount() != nil && *list.GetRemainingItemCount() > 0 || len(list.Items) != 0 {
				return ErrRefused
			}
		}
	}
	return nil
}
