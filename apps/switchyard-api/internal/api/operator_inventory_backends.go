package api

import (
	"context"
	"errors"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type inventoryBackendPort struct {
	Name     string `json:"name"`
	Port     *int32 `json:"port"`
	Protocol string `json:"protocol"`
}
type inventoryBackendEvidence struct {
	Ports             []inventoryBackendPort `json:"ports"`
	Status            string                 `json:"status"`
	Slices            *int                   `json:"listed_slices"`
	Ready             *int                   `json:"ready_addresses"`
	NotReady          *int                   `json:"not_ready_addresses"`
	UnknownReady      *int                   `json:"unknown_ready_addresses"`
	Terminating       *int                   `json:"terminating_addresses"`
	SelectedPods      int                    `json:"selector_matched_pods"`
	SelectedReadyPods int                    `json:"selector_matched_ready_pods"`
}
type inventoryEndpointRead struct {
	Status string
	Slices []discoveryv1.EndpointSlice
}

func inventoryAccessLabel(values map[string]string, key string) string {
	switch value, present := values[key]; {
	case !present:
		return "absent"
	case value == "true" || value == "false":
		return value
	default:
		return "other"
	}
}
func readInventoryEndpointSlices(ctx context.Context, client kubernetes.Interface) inventoryEndpointRead {
	result, err := client.DiscoveryV1().EndpointSlices("").List(ctx, metav1.ListOptions{})
	if err != nil {
		status := "unavailable"
		if apierrors.IsForbidden(err) {
			status = "permission_denied"
		} else if errors.Is(err, context.DeadlineExceeded) || apierrors.IsTimeout(err) {
			status = "timeout"
		}
		return inventoryEndpointRead{Status: status}
	}
	if result.Continue != "" || result.RemainingItemCount != nil && *result.RemainingItemCount > 0 {
		return inventoryEndpointRead{Status: "incomplete"}
	}
	return inventoryEndpointRead{Status: "observed", Slices: result.Items}
}

// Selector matches are context, not Service routing proof. Only a complete
// EndpointSlice read with exact Service owner UIDs produces numeric counts.
// Unknown readiness, terminating addresses and unavailable reads stay distinct.
func inventoryServiceBackends(service corev1.Service, pods []corev1.Pod, read inventoryEndpointRead) inventoryBackendEvidence {
	out := inventoryBackendEvidence{Status: read.Status}
	if len(service.Spec.Selector) > 0 {
		selector := labels.SelectorFromSet(service.Spec.Selector)
		for _, pod := range pods {
			if pod.Namespace == service.Namespace && selector.Matches(labels.Set(pod.Labels)) {
				out.SelectedPods++
				if pod.DeletionTimestamp == nil {
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
							out.SelectedReadyPods++
							break
						}
					}
				}
			}
		}
	}
	if read.Status != "observed" {
		return out
	}
	states := map[string]uint8{}
	ports := map[string]inventoryBackendPort{}
	listed := 0
	out.Slices = &listed
	for _, slice := range read.Slices {
		if slice.Namespace != service.Namespace || slice.Labels[discoveryv1.LabelServiceName] != service.Name {
			continue
		}
		listed++
		owned := false
		for _, owner := range slice.OwnerReferences {
			if owner.APIVersion == "v1" && owner.Kind == "Service" && owner.Name == service.Name && owner.UID != "" && owner.UID == service.UID {
				owned = true
			}
		}
		if !owned {
			out.Status = "unverified_owner"
			continue
		}
		for _, p := range slice.Ports {
			name, proto, value := "", "TCP", "unknown"
			if p.Name != nil {
				name = *p.Name
			}
			if p.Protocol != nil {
				proto = string(*p.Protocol)
			}
			if p.Port != nil {
				value = fmt.Sprint(*p.Port)
			}
			ports[name+"/"+proto+"/"+value] = inventoryBackendPort{Name: name, Port: p.Port, Protocol: proto}
		}
		for _, endpoint := range slice.Endpoints {
			for _, address := range endpoint.Addresses {
				key := string(slice.AddressType) + ":" + address
				switch {
				case endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating:
					states[key] |= 8
				case endpoint.Conditions.Ready == nil:
					states[key] |= 4
				case *endpoint.Conditions.Ready:
					states[key] |= 1
				default:
					states[key] |= 2
				}
			}
		}
	}
	if out.Status != "observed" {
		return out
	}
	keys := make([]string, 0, len(ports))
	for key := range ports {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out.Ports = []inventoryBackendPort{}
	for _, key := range keys {
		out.Ports = append(out.Ports, ports[key])
	}
	// Duplicate addresses count once. Any conflicting state stays unknown.
	r, n, u, t := 0, 0, 0, 0
	for _, state := range states {
		switch state {
		case 1:
			r++
		case 2:
			n++
		case 8:
			t++
		default:
			u++
		}
	}
	out.Ready, out.NotReady, out.UnknownReady, out.Terminating = &r, &n, &u, &t
	return out
}
