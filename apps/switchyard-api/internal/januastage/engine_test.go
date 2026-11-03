package januastage

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type nativeFixture struct {
	objects map[string]*unstructured.Unstructured
	creates int
	guards  int
	failAt  int
	foreign bool
}

func (n *nativeFixture) Guard(context.Context, Plan) error {
	n.guards++
	if n.foreign {
		return ErrRefused
	}
	return nil
}
func (n *nativeFixture) Get(_ context.Context, o Object) (*unstructured.Unstructured, error) {
	if v := n.objects[o.Kind+o.Name]; v != nil {
		return v.DeepCopy(), nil
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: o.Kind}, o.Name)
}
func (n *nativeFixture) Create(_ context.Context, o Object) (*unstructured.Unstructured, error) {
	n.creates++
	if n.creates == n.failAt {
		return nil, fmt.Errorf("unknown native result")
	}
	u := o.Body.DeepCopy()
	u.SetUID(types.UID(uuid.NewString()))
	u.SetResourceVersion("1")
	n.objects[o.Kind+o.Name] = u
	return u.DeepCopy(), nil
}

type journalFixture struct {
	objects   int
	completed int
	retained  map[string][2]string
}

func (j *journalFixture) RetainedObject(_ context.Context, object Object) (string, string, error) {
	value := j.retained[object.Kind+object.Name]
	return value[0], value[1], nil
}

func (j *journalFixture) RecordObject(_ context.Context, object Object, live *unstructured.Unstructured) error {
	j.objects++
	if j.retained == nil {
		j.retained = map[string][2]string{}
	}
	j.retained[object.Kind+object.Name] = [2]string{string(live.GetUID()), ObjectDigest(live)}
	return nil
}
func (j *journalFixture) Complete(_ context.Context, a Authority) error {
	if ValidateAuthorityShape(a) != nil {
		return ErrRefused
	}
	j.completed++
	return nil
}

func TestNativeEngineCreatesOnlySixInertObjectsAndRetriesWithoutOverwrite(t *testing.T) {
	p, err := BuildPlan(uuid.New(), intentFixture(), strings.Repeat("b", 40), "ns", "baseline", sourceFixture(t))
	require.NoError(t, err)
	n := &nativeFixture{objects: map[string]*unstructured.Unstructured{}}
	j := &journalFixture{}
	a, err := Materialize(t.Context(), p, n, j)
	require.NoError(t, err)
	require.NoError(t, ValidateAuthorityShape(a))
	require.Equal(t, 6, n.creates)
	require.Equal(t, 6, j.objects)
	require.Equal(t, 1, j.completed)
	require.GreaterOrEqual(t, n.guards, 7)
	require.True(t, a.ActivationHeld)
	require.False(t, a.ActiveServiceRegistered)
	_, err = Materialize(t.Context(), p, n, j)
	require.NoError(t, err)
	require.Equal(t, 6, n.creates, "retry must only read exact owned native objects")
}

func TestForeignObjectsAndDriftAreNeverAdoptedOrOverwritten(t *testing.T) {
	p, err := BuildPlan(uuid.New(), intentFixture(), strings.Repeat("b", 40), "ns", "baseline", sourceFixture(t))
	require.NoError(t, err)
	for _, change := range []func(*unstructured.Unstructured){
		func(u *unstructured.Unstructured) { u.SetLabels(map[string]string{"enclii.dev/managed-by": "foreign"}) },
		func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, true, "spec", "syncPolicy", "automated", "enabled")
		},
	} {
		n := &nativeFixture{objects: map[string]*unstructured.Unstructured{}}
		object := p.Objects[0]
		u, err := n.Create(t.Context(), object)
		require.NoError(t, err)
		change(u)
		n.objects[object.Kind+object.Name] = u
		j := &journalFixture{}
		_, err = Materialize(t.Context(), p, n, j)
		require.Error(t, err)
		require.Equal(t, 1, n.creates)
		require.Zero(t, j.completed)
	}
}

func TestUnknownCreateOutcomeRetainsOnlyInertPartialObjects(t *testing.T) {
	p, err := BuildPlan(uuid.New(), intentFixture(), strings.Repeat("b", 40), "ns", "baseline", sourceFixture(t))
	require.NoError(t, err)
	n := &nativeFixture{objects: map[string]*unstructured.Unstructured{}, failAt: 3}
	j := &journalFixture{}
	_, err = Materialize(t.Context(), p, n, j)
	require.ErrorIs(t, err, ErrUnknown)
	require.Len(t, n.objects, 2)
	require.Zero(t, j.completed)
	n.failAt = 0
	_, err = Materialize(t.Context(), p, n, &journalFixture{})
	require.ErrorIs(t, err, ErrUnknown, "partial transaction rollback cannot establish durable native UID lineage")
	require.Len(t, n.objects, 2)
}

func TestNamespaceGuardFailurePreventsEveryNativeWrite(t *testing.T) {
	p, err := BuildPlan(uuid.New(), intentFixture(), strings.Repeat("b", 40), "ns", "baseline", sourceFixture(t))
	require.NoError(t, err)
	n := &nativeFixture{objects: map[string]*unstructured.Unstructured{}, foreign: true}
	_, err = Materialize(t.Context(), p, n, &journalFixture{})
	require.Error(t, err)
	require.Zero(t, n.creates)
}

func TestReadbackRejectsLatentPodAndServiceAuthorityInjection(t *testing.T) {
	p, err := BuildPlan(uuid.New(), intentFixture(), strings.Repeat("b", 40), "ns", "baseline", sourceFixture(t))
	require.NoError(t, err)
	for _, change := range []struct {
		kind, name string
		mutate     func(*unstructured.Unstructured)
	}{
		{"Deployment", "janua-staging-redis", func(u *unstructured.Unstructured) {
			containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
			container := containers[0].(map[string]any)
			container["securityContext"].(map[string]any)["privileged"] = true
			_ = unstructured.SetNestedSlice(u.Object, containers, "spec", "template", "spec", "containers")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
			container := containers[0].(map[string]any)
			container["securityContext"].(map[string]any)["capabilities"].(map[string]any)["add"] = []any{"SYS_ADMIN"}
			_ = unstructured.SetNestedSlice(u.Object, containers, "spec", "template", "spec", "containers")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, "privileged-account", "spec", "template", "spec", "serviceAccountName")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, "node-fixture", "spec", "template", "spec", "nodeName")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, "unreviewed-class", "spec", "template", "spec", "runtimeClassName")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, true, "spec", "template", "spec", "hostNetwork")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, true, "spec", "template", "spec", "hostPID")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, true, "spec", "template", "spec", "hostIPC")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(u.Object, []any{map[string]any{"name": "injected"}}, "spec", "template", "spec", "initContainers")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(u.Object, []any{map[string]any{"name": "injected"}}, "spec", "template", "spec", "ephemeralContainers")
		}},
		{"Deployment", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(u.Object, []any{map[string]any{"name": "kernel.msgmax", "value": "999999"}}, "spec", "template", "spec", "securityContext", "sysctls")
		}},
		{"Service", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, "foreign", "spec", "selector", "extra")
		}},
		{"Service", Deployment, func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, "None", "spec", "clusterIP")
		}},
	} {
		for _, object := range p.Objects {
			if object.Kind != change.kind || object.Name != change.name {
				continue
			}
			actual := object.Body.DeepCopy()
			actual.SetUID(types.UID(uuid.NewString()))
			change.mutate(actual)
			require.False(t, ProjectionMatches(object, actual), change.kind+"/"+change.name)
		}
	}
}

func TestReadbackAllowsOnlyKnownBenignAPIDefaultsAndEmptyEnvOmission(t *testing.T) {
	p, err := BuildPlan(uuid.New(), intentFixture(), strings.Repeat("b", 40), "ns", "baseline", sourceFixture(t))
	require.NoError(t, err)
	for _, object := range p.Objects {
		actual := object.Body.DeepCopy()
		actual.SetUID(types.UID(uuid.NewString()))
		if object.Kind == "Deployment" {
			_ = unstructured.SetNestedField(actual.Object, "ClusterFirst", "spec", "template", "spec", "dnsPolicy")
			_ = unstructured.SetNestedField(actual.Object, "default-scheduler", "spec", "template", "spec", "schedulerName")
			_ = unstructured.SetNestedField(actual.Object, "default", "spec", "template", "spec", "serviceAccountName")
			containers, _, _ := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
			for _, raw := range containers {
				container := raw.(map[string]any)
				container["terminationMessagePolicy"] = "File"
				container["terminationMessagePath"] = "/dev/termination-log"
				for _, v := range container["env"].([]any) {
					env := v.(map[string]any)
					if env["value"] == "" {
						delete(env, "value")
					}
				}
			}
			_ = unstructured.SetNestedSlice(actual.Object, containers, "spec", "template", "spec", "containers")
		}
		require.True(t, ProjectionMatches(object, actual), object.Kind+"/"+object.Name)
	}
}
