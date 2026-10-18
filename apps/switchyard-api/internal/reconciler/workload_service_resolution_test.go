package reconciler

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type fakeNameLister struct {
	byName map[string][]*types.Service
	err    error
}

func (f fakeNameLister) ListByName(name string) ([]*types.Service, error) {
	return f.byName[name], f.err
}

// TestResolveWorkloadService: two projects that both run a service called
// "web" (enclii#404) must each get their own record from the K8s→DB sync.
func TestResolveWorkloadService(t *testing.T) {
	projectA, projectB := uuid.New(), uuid.New()
	custom := "shared-infra"
	webA := &types.Service{ID: uuid.New(), ProjectID: projectA, Name: "web"}
	webB := &types.Service{ID: uuid.New(), ProjectID: projectB, Name: "web"}
	webBCustomNS := &types.Service{ID: uuid.New(), ProjectID: projectB, Name: "web", K8sNamespace: &custom}
	apiA := &types.Service{ID: uuid.New(), ProjectID: projectA, Name: "api"}

	ns := namespaceProjects{}
	ns.add("ns-a", projectA)
	ns.add("ns-b", projectB)
	ns.add("ns-shared", projectA)
	ns.add("ns-shared", projectB)

	tests := []struct {
		name      string
		byName    map[string][]*types.Service
		listErr   error
		namespace string
		workload  string
		want      *types.Service
		wantErrIs error
	}{
		{name: "project A's namespace gets A's web", byName: map[string][]*types.Service{"web": {webA, webB}}, namespace: "ns-a", workload: "web", want: webA},
		{name: "project B's namespace gets B's web", byName: map[string][]*types.Service{"web": {webA, webB}}, namespace: "ns-b", workload: "web", want: webB},
		{name: "a namespace shared by both projects is ambiguous", byName: map[string][]*types.Service{"web": {webA, webB}}, namespace: "ns-shared", workload: "web", wantErrIs: db.ErrAmbiguousServiceName},
		{name: "another project's same-named service is not this workload", byName: map[string][]*types.Service{"api": {apiA}}, namespace: "ns-b", workload: "api", want: nil},
		{name: "service k8s_namespace override claims the namespace", byName: map[string][]*types.Service{"web": {webA, webBCustomNS}}, namespace: custom, workload: "web", want: webBCustomNS},
		{name: "core namespace with a platform-unique name", byName: map[string][]*types.Service{"api": {apiA}}, namespace: "monitoring", workload: "api", want: apiA},
		{name: "core namespace with a colliding name is ambiguous", byName: map[string][]*types.Service{"web": {webA, webB}}, namespace: "monitoring", workload: "web", wantErrIs: db.ErrAmbiguousServiceName},
		{name: "no service at all", byName: map[string][]*types.Service{}, namespace: "ns-a", workload: "redis", want: nil},
		{name: "lookup error is returned", listErr: errors.New("db down"), namespace: "ns-a", workload: "web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveWorkloadService(fakeNameLister{byName: tt.byName, err: tt.listErr}, ns, tt.namespace, tt.workload)
			switch {
			case tt.listErr != nil:
				if err == nil {
					t.Fatalf("want the lookup error, got service %v", got)
				}
			case tt.wantErrIs != nil:
				if !errors.Is(err, tt.wantErrIs) || got != nil {
					t.Fatalf("got (%v, %v), want error %v", got, err, tt.wantErrIs)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}
