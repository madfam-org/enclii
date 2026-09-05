// Package weighbridge turns finished CI runner pods into build.completed
// events, from the platform's own timestamps.
//
// WHY IT EXISTS
// =============
// Until this package, nobody could say how many CI minutes a tenant had used.
// switchyard-api credited a flat 3.0 minutes per release and billed overage
// against that number; roundhouse computed a real duration and told nobody.
// Neither is a measurement, so no runner SKU and no per-product cost
// allocation had a number to stand on.
//
// WHY IT WATCHES THE CLUSTER AND NOT THE WORKFLOW
// ==============================================
// A post-step in a reusable workflow is cheaper and covers only the repos that
// choose to consume the workflow, and a tenant can delete the step. This
// watches the pods the platform itself created, so a workflow cannot opt out
// of being metered. That is the whole point of a weighbridge: the lorry does
// not get to report its own weight.
//
// WHY THE POD AND NOT THE EphemeralRunner CR
// ==========================================
// The CR is the better-looking source — it carries the repository, the
// workflow ref and the job name in its status — but it records no completion
// timestamp anywhere. Reading a finish time off a CR means using the moment
// this process happened to notice, which is observation lag wearing a
// timestamp's clothes.
//
// The pod carries the two real ones: `metadata.creationTimestamp` (when the
// slot was claimed) and the runner container's `state.terminated.startedAt` /
// `finishedAt` (when the job actually ran). So the pod is the source of every
// number, and the CR is read only to enrich the metadata that the pod does not
// carry. When the CR is already gone — ARC deletes EphemeralRunners promptly —
// those metadata fields are ABSENT. They are not guessed.
//
// WHAT IT DOES NOT OBSERVE
// ========================
// Cache bytes and egress bytes. Nothing in the runner pod's status reports
// either one; there is no cache-volume accounting and no per-pod byte counter
// in this estate. The fields are therefore omitted from the event entirely
// rather than sent as zero, because a zero is a claim ("no cache was used")
// and an absent field is the truth ("nobody measured"). Waybill's aggregator
// drops a metric that sums to zero, so an absent field and a zero field would
// have produced the same row — but not the same conversation with the next
// person who reads the payload.
package weighbridge

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"

	"github.com/madfam-org/enclii/apps/waybill/internal/events"
)

// Config is everything the observer needs that is not on the pod.
type Config struct {
	// Namespace holds the runner pods (the ARC scale-set namespace).
	Namespace string

	// RunnerLabelSelector selects runner pods. ARC stamps every runner pod
	// with its own labels; this is a selector rather than a hardcoded string
	// so a scale-set rename does not need a rebuild.
	RunnerLabelSelector string

	// RunnerContainerName is the container whose termination window is the
	// build itself. ARC names it "runner".
	RunnerContainerName string

	// ScaleSetLabel, TenantLabel and ProjectIDLabel name the pod labels each
	// value is read from. Tenant and project are NOT ARC labels — they are
	// stamped by the platform when a per-tenant scale set is provisioned. A
	// pod without them is not guessed at; see DefaultProjectID.
	ScaleSetLabel  string
	TenantLabel    string
	ProjectIDLabel string

	// DefaultProjectID attributes runners from the shared pool, which carries
	// no per-tenant label because there is only one tenant on it today.
	// Waybill's ingest requires a project id and there is no honest way to
	// invent one, so a runner that matches neither the label nor a configured
	// default is COUNTED AND DROPPED (weighbridge_runners_unattributed_total)
	// rather than filed under a placeholder project that somebody would
	// eventually get an invoice for.
	DefaultProjectID uuid.UUID
}

// DefaultConfig returns the shape ARC actually produces today.
func DefaultConfig() Config {
	return Config{
		Namespace: "arc-runners",
		// `app.kubernetes.io/component: runner` is what ARC's scale-set
		// controller puts on the runner pod and on nothing else in the
		// namespace (the listener pod carries `component: runner-scale-set-
		// listener`).
		RunnerLabelSelector: "app.kubernetes.io/component=runner",
		RunnerContainerName: "runner",
		ScaleSetLabel:       "actions.github.com/scale-set-name",
		TenantLabel:         "enclii.dev/tenant",
		ProjectIDLabel:      "enclii.dev/project-id",
	}
}

// Observation is one finished runner pod, reduced to the facts.
//
// Every field here came off the pod. A field that could not be read is left at
// its zero value and is omitted from the event, never defaulted.
type Observation struct {
	// UID is the pod's UID. It is the idempotency key: a property of the
	// artefact, minted once by the API server, stable across every restart
	// and re-list of this process. A key derived from anything this process
	// computes — a hash of the payload, a timestamp — would change when the
	// code changed and re-bill a month of history.
	UID string

	PodName   string
	ProjectID uuid.UUID

	// SlotSeconds is how long the runner slot was HELD: pod creation to the
	// last container's termination. This is the number pool capacity is sized
	// against.
	SlotSeconds float64

	// DurationSeconds is how long the job RAN: the runner container's own
	// start-to-finish. Always <= SlotSeconds; the difference is image pull,
	// scheduling and teardown, which are real costs but not the customer's
	// job time.
	DurationSeconds float64

	Outcome           string // "succeeded" | "failed"
	ScaleSet          string
	Tenant            string
	RunnerImageDigest string

	// Repo, Workflow and Job are NOT on the pod. They live in the
	// EphemeralRunner CR's status, which ARC deletes on roughly the same
	// schedule as the pod, so they are filled in by Enrich when the CR is
	// still readable and left EMPTY when it is not. Empty means "the CR was
	// already gone", never "this runner ran no job".
	Repo     string
	Workflow string
	Job      string

	// FinishedAt is the event's timestamp — when the work ended, not when
	// this process noticed. An event stamped with observation time lands in
	// the wrong hourly bucket every time the controller restarts.
	FinishedAt time.Time
}

// JobRef is the part of a runner's identity that lives only on the
// EphemeralRunner CR: which repository asked for it, which workflow ref, which
// job. Best-effort by construction — see Observation.Repo.
type JobRef struct {
	Repo     string
	Workflow string
	Job      string
}

// MetadataSource resolves a runner pod's name to its job identity. An
// interface so the controller can be tested without a CRD, a dynamic client or
// an API server, and so a future source (the ARC listener, say) can replace
// the CR read without touching the mapping.
type MetadataSource interface {
	LookupJob(namespace, name string) (JobRef, bool)
}

// Enrich applies whatever the metadata source knew. Fields it did not know
// stay empty; nothing here invents a repository name.
func (o *Observation) Enrich(ref JobRef) {
	o.Repo = ref.Repo
	o.Workflow = ref.Workflow
	o.Job = ref.Job
}

// ErrNotTerminal reports a pod that has not finished. Not an error condition:
// most pods in a healthy pool are still running.
var ErrNotTerminal = fmt.Errorf("pod has not reached a terminal phase")

// ErrUnattributed reports a pod that cannot be tied to a project.
var ErrUnattributed = fmt.Errorf("pod carries no project attribution")

// Observe reduces a runner pod to an Observation, or explains why it cannot.
//
// Pure: no clock, no client, no I/O. Every number comes from the pod, which is
// what makes the mapping testable without a cluster and auditable without
// running anything.
func Observe(pod *corev1.Pod, cfg Config) (*Observation, error) {
	if pod == nil {
		return nil, fmt.Errorf("nil pod")
	}
	if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
		return nil, ErrNotTerminal
	}

	projectID, err := projectIDFor(pod, cfg)
	if err != nil {
		return nil, err
	}

	obs := &Observation{
		UID:       string(pod.UID),
		PodName:   pod.Name,
		ProjectID: projectID,
		Outcome:   outcomeFor(pod.Status.Phase),
		ScaleSet:  pod.Labels[cfg.ScaleSetLabel],
		Tenant:    pod.Labels[cfg.TenantLabel],
	}

	// Slot window: pod creation → the last container to stop.
	//
	// Creation, not `status.startTime`: the slot is claimed the moment the pod
	// object exists, and the gap to startTime is scheduling delay, which is
	// capacity the pool spent and could not sell to anyone else.
	created := pod.CreationTimestamp.Time
	finished := lastTermination(pod)
	if !created.IsZero() && !finished.IsZero() && finished.After(created) {
		obs.SlotSeconds = finished.Sub(created).Seconds()
	}
	obs.FinishedAt = finished

	// Run window: the runner container's own terminated state.
	if cs := containerStatus(pod, cfg.RunnerContainerName); cs != nil {
		if t := cs.State.Terminated; t != nil {
			if !t.StartedAt.IsZero() && t.FinishedAt.After(t.StartedAt.Time) {
				obs.DurationSeconds = t.FinishedAt.Sub(t.StartedAt.Time).Seconds()
			}
		}
		obs.RunnerImageDigest = digestOf(cs.ImageID)
	}

	// A terminal pod with no readable termination time still has a UID and a
	// project, so it is worth recording — but it must not be stamped with
	// "now", which would file it under whichever hour the controller happened
	// to restart in. Fall back to the pod's creation time, which is at least a
	// real moment in the pod's life, and let the zero durations say plainly
	// that no time was measured.
	if obs.FinishedAt.IsZero() {
		obs.FinishedAt = created
	}

	return obs, nil
}

// projectIDFor resolves the project a runner's minutes belong to.
//
// Label first, configured default second, refuse third. The refusal is the
// important branch: Waybill's ingest requires a project id, and the tempting
// fix — a nil UUID, a "shared" sentinel, the first project in the database —
// produces a row that looks metered and is attributed to nobody. Better to
// count the drop and alert on it.
func projectIDFor(pod *corev1.Pod, cfg Config) (uuid.UUID, error) {
	if raw := strings.TrimSpace(pod.Labels[cfg.ProjectIDLabel]); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, fmt.Errorf("%w: label %s is not a UUID", ErrUnattributed, cfg.ProjectIDLabel)
		}
		return id, nil
	}
	if cfg.DefaultProjectID != uuid.Nil {
		return cfg.DefaultProjectID, nil
	}
	return uuid.Nil, ErrUnattributed
}

func outcomeFor(phase corev1.PodPhase) string {
	if phase == corev1.PodSucceeded {
		return "succeeded"
	}
	return "failed"
}

// lastTermination is the latest FinishedAt across every container, init
// containers included. The slot is not free until the last one stops.
func lastTermination(pod *corev1.Pod) time.Time {
	var latest time.Time
	consider := func(states []corev1.ContainerStatus) {
		for i := range states {
			t := states[i].State.Terminated
			if t == nil || t.FinishedAt.IsZero() {
				continue
			}
			if t.FinishedAt.After(latest) {
				latest = t.FinishedAt.Time
			}
		}
	}
	consider(pod.Status.InitContainerStatuses)
	consider(pod.Status.ContainerStatuses)
	return latest
}

func containerStatus(pod *corev1.Pod, name string) *corev1.ContainerStatus {
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == name {
			return &pod.Status.ContainerStatuses[i]
		}
	}
	return nil
}

// digestOf extracts the content digest from a container status ImageID.
//
// The runner image identity is the one piece of supply-chain evidence a
// finished pod still carries, and it is the field that would have named the
// 2026-08-10 deprecated-runner outage in the usage stream itself. A repository
// path with no digest is dropped rather than recorded: `ghcr.io/x/y:v1` is not
// an identity, it is a mutable pointer.
func digestOf(imageID string) string {
	if i := strings.Index(imageID, "@"); i >= 0 {
		return imageID[i+1:]
	}
	return ""
}

// UsageResourceType is the `resource_type` every Weighbridge event carries.
// One string, chosen once: Waybill stores it verbatim and it is a join key.
const UsageResourceType = "ci_runner"

// EventSource is the `source` metadata every Weighbridge event carries.
//
// THIS IS THE METER OF RECORD. The other two producers the roadmap describes —
// a post-step in the reusable workflow, and roundhouse reporting its own build
// durations — carry different `source` values and exist to be COMPARED against
// this stream, never summed with it. Anything that sums across sources double
// counts.
const EventSource = "weighbridge"

// BuildEvent renders an Observation as Waybill's ingest request.
//
// The struct is Waybill's own `events.EventRequest`, not a hand-copied JSON
// shape, because this package ships inside the waybill module. That is the
// argument for it living here: the addon emitter in switchyard-api had to
// duplicate this struct across a module boundary and says so in its own
// comment, and a duplicated wire type is a drift waiting for a release.
func (o *Observation) BuildEvent() events.EventRequest {
	// Only fields that were actually observed. A map literal with conditional
	// deletes would be shorter; this way the absent case is the default and
	// somebody has to write a line to make a field appear.
	metrics := map[string]float64{}
	if o.DurationSeconds > 0 {
		metrics["duration_seconds"] = o.DurationSeconds
	}
	if o.SlotSeconds > 0 {
		metrics["slot_seconds"] = o.SlotSeconds
	}
	// NO cache_bytes_read, cache_bytes_written or egress_bytes. See the
	// package comment: nothing in the pod status reports them, and a zero
	// would be a measurement claim nobody made.

	metadata := map[string]string{
		"source":  EventSource,
		"outcome": o.Outcome,
	}
	for k, v := range map[string]string{
		"scale_set":           o.ScaleSet,
		"tenant":              o.Tenant,
		"repo":                o.Repo,
		"workflow":            o.Workflow,
		"job":                 o.Job,
		"runner_image_digest": o.RunnerImageDigest,
	} {
		if v != "" {
			metadata[k] = v
		}
	}

	ts := o.FinishedAt
	return events.EventRequest{
		EventType:    events.EventBuildCompleted,
		ProjectID:    o.ProjectID,
		ResourceType: UsageResourceType,
		// The pod's UID, again: resource_id identifies the metered artefact,
		// and for a runner the artefact is the pod.
		ResourceID:     uuidOrNil(o.UID),
		ResourceName:   o.PodName,
		Metrics:        metrics,
		Metadata:       metadata,
		IdempotencyKey: o.UID,
		Timestamp:      &ts,
	}
}

// uuidOrNil parses a pod UID. Kubernetes UIDs are UUIDs, but a fake or a
// hand-written fixture may not be, and a malformed one must not stop the event:
// the idempotency key carries the original string either way.
func uuidOrNil(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}
