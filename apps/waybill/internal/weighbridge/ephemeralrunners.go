package weighbridge

import (
	"context"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

// EphemeralRunnerGVR is ARC's runner custom resource.
var EphemeralRunnerGVR = schema.GroupVersionResource{
	Group:    "actions.github.com",
	Version:  "v1alpha1",
	Resource: "ephemeralrunners",
}

// EphemeralRunnerStore answers "which job was this runner running" from a
// cached view of the EphemeralRunner custom resources.
//
// READ AS UNSTRUCTURED, ON PURPOSE. Taking ARC's Go types as a dependency
// would tie this module's build to a controller that ships its own
// Kubernetes-version-locked client, in exchange for three strings. Three
// strings do not justify that, and an unstructured read degrades to "field
// absent" when ARC renames something, where a typed read would fail to
// compile against a cluster it no longer matches.
//
// BEST EFFORT BY DESIGN. ARC deletes an EphemeralRunner at roughly the same
// moment as its pod, so this lookup races the delete and sometimes loses. It
// losing means repo/workflow/job are absent from that event — never that the
// event is dropped, and never that a value is invented. The minutes are the
// meter; the job name is context.
type EphemeralRunnerStore struct {
	informer cache.SharedIndexInformer
	logger   *zap.Logger
}

// NewEphemeralRunnerStore builds a namespace-scoped informer over
// EphemeralRunners. Returns nil when no dynamic client is configured, which
// makes "no CR access" a supported deployment rather than a crash.
func NewEphemeralRunnerStore(client dynamic.Interface, namespace string, logger *zap.Logger) *EphemeralRunnerStore {
	if client == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(client, resyncPeriod, namespace, nil)
	return &EphemeralRunnerStore{
		informer: factory.ForResource(EphemeralRunnerGVR).Informer(),
		logger:   logger,
	}
}

// Run starts the informer and blocks until ctx is cancelled.
func (s *EphemeralRunnerStore) Run(ctx context.Context) {
	if s == nil {
		<-ctx.Done()
		return
	}
	s.informer.Run(ctx.Done())
}

// WaitForSync blocks until the store has listed once, or the context ends.
// A lookup against an unsynced store returns "not found", which would silently
// strip the job metadata off every event for the first few seconds of a
// rollout.
func (s *EphemeralRunnerStore) WaitForSync(ctx context.Context) bool {
	if s == nil {
		return false
	}
	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return cache.WaitForCacheSync(syncCtx.Done(), s.informer.HasSynced)
}

// LookupJob implements MetadataSource. ARC names the runner pod after its
// EphemeralRunner, so the pod's own name is the key.
func (s *EphemeralRunnerStore) LookupJob(namespace, name string) (JobRef, bool) {
	if s == nil {
		return JobRef{}, false
	}
	obj, exists, err := s.informer.GetStore().GetByKey(namespace + "/" + name)
	if err != nil || !exists {
		return JobRef{}, false
	}
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return JobRef{}, false
	}
	ref := JobRef{
		Repo:     nestedString(u, "status", "jobRepositoryName"),
		Workflow: nestedString(u, "status", "jobWorkflowRef"),
		Job:      nestedString(u, "status", "jobDisplayName"),
	}
	if ref.Repo == "" && ref.Workflow == "" && ref.Job == "" {
		// An EphemeralRunner that never picked up a job. Reporting "found"
		// here would overwrite nothing with nothing, but reporting "not
		// found" keeps the distinction visible to the caller.
		return JobRef{}, false
	}
	return ref, true
}

func nestedString(u *unstructured.Unstructured, fields ...string) string {
	v, found, err := unstructured.NestedString(u.Object, fields...)
	if err != nil || !found {
		return ""
	}
	return v
}
