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
}

func (j *journalFixture) RecordObject(_ context.Context, _ Object, _ *unstructured.Unstructured) error {
	j.objects++
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
	_, err = Materialize(t.Context(), p, n, &journalFixture{})
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
	require.NoError(t, err)
	require.Len(t, n.objects, 6)
}

func TestNamespaceGuardFailurePreventsEveryNativeWrite(t *testing.T) {
	p, err := BuildPlan(uuid.New(), intentFixture(), strings.Repeat("b", 40), "ns", "baseline", sourceFixture(t))
	require.NoError(t, err)
	n := &nativeFixture{objects: map[string]*unstructured.Unstructured{}, foreign: true}
	_, err = Materialize(t.Context(), p, n, &journalFixture{})
	require.Error(t, err)
	require.Zero(t, n.creates)
}
