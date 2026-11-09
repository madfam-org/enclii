package api

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"testing"
)

func backendFixture() (corev1.Service, discoveryv1.EndpointSlice) {
	s := corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "fixture-db", Namespace: "fixture", UID: "service-uid"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "fixture-db"}}}
	e := discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "slice", Namespace: s.Namespace, Labels: map[string]string{discoveryv1.LabelServiceName: s.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Service", Name: s.Name, UID: s.UID}}}, AddressType: discoveryv1.AddressTypeIPv4}
	return s, e
}
func TestBackendEvidenceSeparatesSelectorsFromRoutes(t *testing.T) {
	s, e := backendFixture()
	yes, no := true, false
	pods := []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "selected", Namespace: s.Namespace, Labels: s.Spec.Selector}, Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}}
	empty := inventoryServiceBackends(s, pods, inventoryEndpointRead{Status: "observed"})
	require.Equal(t, 1, empty.SelectedReadyPods)
	require.Equal(t, 0, *empty.Ready, "healthy selector matches cannot prove Service routes")
	port := int32(5433)
	e.Ports = []discoveryv1.EndpointPort{{Port: &port}}
	e.Endpoints = []discoveryv1.Endpoint{{Addresses: []string{"192.0.2.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &yes}}, {Addresses: []string{"192.0.2.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &yes}}, {Addresses: []string{"192.0.2.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &no}}, {Addresses: []string{"192.0.2.3"}}, {Addresses: []string{"192.0.2.4"}, Conditions: discoveryv1.EndpointConditions{Ready: &yes, Terminating: &yes}}}
	got := inventoryServiceBackends(s, pods, inventoryEndpointRead{Status: "observed", Slices: []discoveryv1.EndpointSlice{e}})
	require.Equal(t, "observed", got.Status)
	require.Equal(t, 1, *got.Slices)
	require.EqualValues(t, 5433, *got.Ports[0].Port)
	require.Equal(t, 1, *got.Ready)
	require.Equal(t, 1, *got.NotReady)
	require.Equal(t, 1, *got.UnknownReady)
	require.Equal(t, 1, *got.Terminating)
	b, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(b), "192.0.2.")
	e.Endpoints = append(e.Endpoints, discoveryv1.Endpoint{Addresses: []string{"192.0.2.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &no}})
	got = inventoryServiceBackends(s, nil, inventoryEndpointRead{Status: "observed", Slices: []discoveryv1.EndpointSlice{e}})
	require.Equal(t, 0, *got.Ready)
	require.Equal(t, 2, *got.UnknownReady)
}
func TestBackendEvidenceRejectsForeignOrMissingOwner(t *testing.T) {
	for _, v := range []string{"uid", "name", "kind", "version", "missing"} {
		t.Run(v, func(t *testing.T) {
			s, e := backendFixture()
			switch v {
			case "uid":
				e.OwnerReferences[0].UID = "recreated"
			case "name":
				e.OwnerReferences[0].Name = "other"
			case "kind":
				e.OwnerReferences[0].Kind = "Pod"
			case "version":
				e.OwnerReferences[0].APIVersion = "other/v1"
			case "missing":
				e.OwnerReferences = nil
			}
			got := inventoryServiceBackends(s, nil, inventoryEndpointRead{Status: "observed", Slices: []discoveryv1.EndpointSlice{e}})
			require.Equal(t, "unverified_owner", got.Status)
			require.Nil(t, got.Ready)
			require.Nil(t, got.NotReady)
			require.Nil(t, got.UnknownReady)
			require.Nil(t, got.Terminating)
		})
	}
	s, e := backendFixture()
	e.Namespace = "other"
	got := inventoryServiceBackends(s, nil, inventoryEndpointRead{Status: "observed", Slices: []discoveryv1.EndpointSlice{e}})
	require.Equal(t, 0, *got.Slices)
}
func TestEndpointUnavailablePreservesInventoryWithoutFalseZero(t *testing.T) {
	h, c, _ := inventoryFixture()
	c.PrependReactor("list", "endpointslices", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "discovery.k8s.io", Resource: "endpointslices"}, "fixture", nil)
	})
	got, err := h.readInventoryTopology(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, got.Nodes)
	require.NotEmpty(t, got.Pods)
	require.Equal(t, "permission_denied", got.Services[0].Backends.Status)
	require.Nil(t, got.Services[0].Backends.Ready)
	require.Nil(t, got.Services[0].Backends.Slices)
}
func TestEndpointIncompleteReadIsUnknown(t *testing.T) {
	c := fake.NewSimpleClientset()
	c.PrependReactor("list", "endpointslices", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, &discoveryv1.EndpointSliceList{ListMeta: metav1.ListMeta{Continue: "next"}}, nil
	})
	read := readInventoryEndpointSlices(context.Background(), c)
	require.Equal(t, "incomplete", read.Status)
	s, _ := backendFixture()
	got := inventoryServiceBackends(s, nil, read)
	require.Nil(t, got.Ready)
	require.Nil(t, got.Slices)
	for _, a := range c.Actions() {
		require.Equal(t, "list", a.GetVerb())
	}
}
func TestNetworkAccessLabelObservationsAreBounded(t *testing.T) {
	require.Equal(t, "true", inventoryAccessLabel(map[string]string{"k": "true"}, "k"))
	require.Equal(t, "false", inventoryAccessLabel(map[string]string{"k": "false"}, "k"))
	require.Equal(t, "other", inventoryAccessLabel(map[string]string{"k": "untrusted"}, "k"))
	require.Equal(t, "absent", inventoryAccessLabel(nil, "k"))
	h, c, _ := inventoryFixture()
	n, err := c.CoreV1().Namespaces().Get(context.Background(), "tenant-a", metav1.GetOptions{})
	require.NoError(t, err)
	n.Labels = map[string]string{"enclii.dev/data-access": "true", "unrelated": "do-not-export"}
	_, err = c.CoreV1().Namespaces().Update(context.Background(), n, metav1.UpdateOptions{})
	require.NoError(t, err)
	got, err := h.readInventoryTopology(context.Background())
	require.NoError(t, err)
	require.Equal(t, "true", got.Namespaces[0].DataAccess)
	require.Equal(t, "absent", got.Namespaces[0].PostgresConsumer)
	b, err := json.Marshal(got.Namespaces)
	require.NoError(t, err)
	require.NotContains(t, string(b), "do-not-export")
}
