package weighbridge

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// seenCapacity bounds the emitted-UID set.
//
// The set exists only to stop an informer resync from re-POSTing an event
// Waybill would refuse anyway; correctness across restarts comes from the
// idempotency key, not from this map. So it can be forgotten freely, and a
// process that has run for a month must not be holding every pod UID it ever
// saw. At ARC's observed churn (thousands of runners in a bad day) this holds
// well over a day of history.
const seenCapacity = 20000

// resyncPeriod is how often the informer re-delivers its whole store.
//
// It is a SECOND CHANCE, not the primary path: the watch delivers a pod's
// terminal update within milliseconds, and the resync only matters for a pod
// whose terminal update was missed while the watch was reconnecting. Every
// resynced pod that was already emitted is suppressed by the seen set, so the
// cost of a short period is a map lookup per pod.
const resyncPeriod = 5 * time.Minute

// Controller watches runner pods and emits one build.completed per completed
// pod.
//
// WHAT IT CANNOT SEE. A pod that finishes AND is deleted while this process is
// down is never observed and its minutes are never metered. There is no
// replay: the pod is gone and Kubernetes keeps no history of it. That gap is
// the reason the roundhouse stream and the reusable-workflow post-step exist
// as CROSS-CHECKS — comparing three counts is how the gap gets measured. It is
// also why this deploys as a single always-on Deployment rather than a
// CronJob: a CronJob's whole design is to be absent between runs.
type Controller struct {
	client   kubernetes.Interface
	cfg      Config
	emitter  Emitter
	metadata MetadataSource
	metrics  *Metrics
	logger   *zap.Logger

	mu       sync.Mutex
	seen     map[string]struct{}
	seenRing []string
	seenNext int
}

// NewController wires a controller. `metadata` may be nil, in which case
// repo/workflow/job are simply absent from every event.
func NewController(
	client kubernetes.Interface,
	cfg Config,
	emitter Emitter,
	metadata MetadataSource,
	metrics *Metrics,
	logger *zap.Logger,
) *Controller {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Controller{
		client:   client,
		cfg:      cfg,
		emitter:  emitter,
		metadata: metadata,
		metrics:  metrics,
		logger:   logger,
		seen:     make(map[string]struct{}, seenCapacity),
		seenRing: make([]string, seenCapacity),
	}
}

// Run starts the pod informer and blocks until ctx is cancelled.
func (c *Controller) Run(ctx context.Context) error {
	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.LabelSelector = c.cfg.RunnerLabelSelector
			return c.client.CoreV1().Pods(c.cfg.Namespace).List(ctx, opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.LabelSelector = c.cfg.RunnerLabelSelector
			return c.client.CoreV1().Pods(c.cfg.Namespace).Watch(ctx, opts)
		},
		DisableChunking: false,
	}

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { c.handleObject(ctx, obj) },
		UpdateFunc: func(_, obj interface{}) {
			c.handleObject(ctx, obj)
		},
		// A pod can go terminal and be deleted before any update is
		// delivered. The delete event still carries the last known state, so
		// it is a real chance to meter the pod rather than a cleanup hook.
		DeleteFunc: func(obj interface{}) {
			if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = tombstone.Obj
			}
			c.handleObject(ctx, obj)
		},
	}

	_, informer := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: lw,
		ObjectType:    &corev1.Pod{},
		ResyncPeriod:  resyncPeriod,
		Handler:       handler,
	})

	c.logger.Info("weighbridge watching runner pods",
		zap.String("namespace", c.cfg.Namespace),
		zap.String("selector", c.cfg.RunnerLabelSelector),
	)
	informer.Run(ctx.Done())
	return ctx.Err()
}

// handleObject is the informer's entry point; Handle is the testable core.
func (c *Controller) handleObject(ctx context.Context, obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	c.Handle(ctx, pod)
}

// Handle observes one pod and emits at most one event for it, ever.
//
// The ordering is deliberate: dedup happens BEFORE attribution and BEFORE the
// HTTP call, so a resync storm costs a map lookup rather than N POSTs, and a
// pod that was already metered cannot be re-counted as unattributed if its
// labels changed.
func (c *Controller) Handle(ctx context.Context, pod *corev1.Pod) {
	obs, err := Observe(pod, c.cfg)
	switch {
	case errors.Is(err, ErrNotTerminal):
		// The common case in a healthy pool. Not logged, not counted: a
		// running pod is not an event.
		return
	case errors.Is(err, ErrUnattributed):
		c.count(func(m *Metrics) { m.Observed.Inc(); m.Unattributed.Inc() })
		c.logger.Warn("runner pod has no project attribution; minutes dropped",
			zap.String("pod", pod.Name),
			zap.String("namespace", pod.Namespace),
		)
		return
	case err != nil:
		c.logger.Warn("could not observe runner pod", zap.String("pod", pod.Name), zap.Error(err))
		return
	}

	c.count(func(m *Metrics) { m.Observed.Inc() })

	if c.markSeen(obs.UID) {
		c.count(func(m *Metrics) { m.Duplicate.Inc() })
		return
	}

	if c.metadata != nil {
		if ref, ok := c.metadata.LookupJob(pod.Namespace, pod.Name); ok {
			obs.Enrich(ref)
		}
	}

	if c.emitter == nil {
		return
	}

	if err := c.emitter.Emit(ctx, obs.BuildEvent()); err != nil {
		c.count(func(m *Metrics) { m.Rejected.Inc() })
		// Log the pod, not the payload: the payload carries a repository name
		// and a tenant, and this line goes to a shared log store.
		c.logger.Error("waybill refused a build.completed event; these minutes are lost",
			zap.String("pod", pod.Name),
			zap.Error(err),
		)
		return
	}

	c.count(func(m *Metrics) { m.Emitted.Inc() })
	c.logger.Debug("emitted build.completed",
		zap.String("pod", pod.Name),
		zap.Float64("slot_seconds", obs.SlotSeconds),
		zap.Float64("duration_seconds", obs.DurationSeconds),
	)
}

// markSeen records a UID and reports whether it was already there.
//
// Bounded by construction: the ring overwrites the oldest entry, so the map
// and the slice stay the same size forever. Forgetting a UID is safe — the
// idempotency key makes the re-emission a no-op at Waybill — whereas an
// unbounded map is an OOM with a several-week fuse, which is exactly how the
// stuck-runner watchdog died.
func (c *Controller) markSeen(uid string) bool {
	if uid == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.seen[uid]; ok {
		return true
	}
	if old := c.seenRing[c.seenNext]; old != "" {
		delete(c.seen, old)
	}
	c.seenRing[c.seenNext] = uid
	c.seenNext = (c.seenNext + 1) % len(c.seenRing)
	c.seen[uid] = struct{}{}
	return false
}

func (c *Controller) count(f func(*Metrics)) {
	if c.metrics != nil {
		f(c.metrics)
	}
}
