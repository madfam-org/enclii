package weighbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/waybill/internal/events"
)

var (
	testProject = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	podUID      = "22222222-2222-4222-8222-222222222222"
	baseTime    = time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
)

// completedPod builds a runner pod in the shape ARC leaves behind: created,
// scheduled a little later, ran, stopped.
func completedPod(phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "madfam-runners-blue-abcde",
			Namespace:         "arc-runners",
			UID:               k8stypes.UID(podUID),
			CreationTimestamp: metav1.NewTime(baseTime),
			Labels: map[string]string{
				"app.kubernetes.io/component":       "runner",
				"actions.github.com/scale-set-name": "madfam-runners-blue",
				"enclii.dev/tenant":                 "madfam",
				"enclii.dev/project-id":             testProject.String(),
			},
		},
		Status: corev1.PodStatus{
			Phase: phase,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "runner",
					ImageID: "ghcr.io/madfam-org/enclii/arc-runner@sha256:" +
						"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							// 30s of scheduling and image pull, then 5 minutes
							// of actual job.
							StartedAt:  metav1.NewTime(baseTime.Add(30 * time.Second)),
							FinishedAt: metav1.NewTime(baseTime.Add(330 * time.Second)),
						},
					},
				},
			},
		},
	}
}

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.DefaultProjectID = uuid.Nil // force label-based attribution in tests
	return cfg
}

// --- Observation mapping ---------------------------------------------------

func TestObserve_CompletedPod(t *testing.T) {
	obs, err := Observe(completedPod(corev1.PodSucceeded), testConfig())
	require.NoError(t, err)

	assert.Equal(t, podUID, obs.UID)
	assert.Equal(t, testProject, obs.ProjectID)
	assert.Equal(t, "succeeded", obs.Outcome)
	assert.Equal(t, "madfam-runners-blue", obs.ScaleSet)
	assert.Equal(t, "madfam", obs.Tenant)

	// The slot was held from pod creation to the last container stopping:
	// 330s. The job itself ran for 300s. The 30s difference is scheduling and
	// image pull — capacity that was spent and could not be sold twice.
	assert.InDelta(t, 330.0, obs.SlotSeconds, 1e-9)
	assert.InDelta(t, 300.0, obs.DurationSeconds, 1e-9)
	assert.Greater(t, obs.SlotSeconds, obs.DurationSeconds)

	// The event's timestamp is when the work ended, not when we looked.
	assert.True(t, obs.FinishedAt.Equal(baseTime.Add(330*time.Second)))

	assert.Equal(t,
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		obs.RunnerImageDigest)
}

func TestObserve_FailedPodIsStillMetered(t *testing.T) {
	obs, err := Observe(completedPod(corev1.PodFailed), testConfig())
	require.NoError(t, err)
	// A failed build burns exactly as much of the pool as a successful one.
	assert.Equal(t, "failed", obs.Outcome)
	assert.InDelta(t, 330.0, obs.SlotSeconds, 1e-9)
}

func TestObserve_RunningPodIsNotAnEvent(t *testing.T) {
	for _, phase := range []corev1.PodPhase{corev1.PodPending, corev1.PodRunning} {
		t.Run(string(phase), func(t *testing.T) {
			pod := completedPod(phase)
			pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(baseTime)},
			}
			_, err := Observe(pod, testConfig())
			assert.ErrorIs(t, err, ErrNotTerminal)
		})
	}
}

func TestObserve_UnattributedPodIsRefused(t *testing.T) {
	t.Run("no_label_no_default", func(t *testing.T) {
		pod := completedPod(corev1.PodSucceeded)
		delete(pod.Labels, "enclii.dev/project-id")
		_, err := Observe(pod, testConfig())
		assert.ErrorIs(t, err, ErrUnattributed)
	})

	t.Run("malformed_label_is_not_silently_replaced_by_the_default", func(t *testing.T) {
		cfg := testConfig()
		cfg.DefaultProjectID = uuid.MustParse("33333333-3333-4333-8333-333333333333")
		pod := completedPod(corev1.PodSucceeded)
		pod.Labels["enclii.dev/project-id"] = "not-a-uuid"
		// A stamped-but-broken label means somebody meant to attribute this
		// runner somewhere specific. Falling back to the pool default would
		// bill the wrong project and look completely normal.
		_, err := Observe(pod, cfg)
		assert.ErrorIs(t, err, ErrUnattributed)
	})

	t.Run("default_covers_the_unlabelled_shared_pool", func(t *testing.T) {
		cfg := testConfig()
		cfg.DefaultProjectID = uuid.MustParse("33333333-3333-4333-8333-333333333333")
		pod := completedPod(corev1.PodSucceeded)
		delete(pod.Labels, "enclii.dev/project-id")
		obs, err := Observe(pod, cfg)
		require.NoError(t, err)
		assert.Equal(t, cfg.DefaultProjectID, obs.ProjectID)
	})
}

// A terminal pod whose container never recorded a termination time still has a
// UID and a project. It must not be stamped with "now": that would file it
// under whichever hour the controller happened to restart in.
func TestObserve_NoTerminationTimestampsFallsBackToCreation(t *testing.T) {
	pod := completedPod(corev1.PodSucceeded)
	pod.Status.ContainerStatuses = nil

	obs, err := Observe(pod, testConfig())
	require.NoError(t, err)
	assert.Zero(t, obs.SlotSeconds)
	assert.Zero(t, obs.DurationSeconds)
	assert.True(t, obs.FinishedAt.Equal(baseTime))
}

func TestObserve_SlotEndsAtTheLastContainerToStop(t *testing.T) {
	pod := completedPod(corev1.PodSucceeded)
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{
		Name: "dind",
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				StartedAt:  metav1.NewTime(baseTime.Add(10 * time.Second)),
				FinishedAt: metav1.NewTime(baseTime.Add(400 * time.Second)),
			},
		},
	})

	obs, err := Observe(pod, testConfig())
	require.NoError(t, err)
	// The slot is not free until the sidecar stops too.
	assert.InDelta(t, 400.0, obs.SlotSeconds, 1e-9)
	// The job's own runtime is unchanged.
	assert.InDelta(t, 300.0, obs.DurationSeconds, 1e-9)
}

func TestDigestOf(t *testing.T) {
	assert.Equal(t, "sha256:abc", digestOf("ghcr.io/x/y@sha256:abc"))
	// A tag is a mutable pointer, not an identity, and is dropped rather than
	// recorded as one.
	assert.Empty(t, digestOf("ghcr.io/x/y:v1"))
	assert.Empty(t, digestOf(""))
}

// --- The wire envelope -----------------------------------------------------

// The event must carry the documented fields and NOTHING else. A meter that
// quietly grows a field is a meter whose consumers cannot be reviewed.
func TestBuildEvent_EnvelopeCarriesNoUndocumentedField(t *testing.T) {
	obs, err := Observe(completedPod(corev1.PodSucceeded), testConfig())
	require.NoError(t, err)
	obs.Enrich(JobRef{Repo: "madfam-org/enclii", Workflow: "ci.yml@refs/heads/main", Job: "unit-tests"})

	raw, err := json.Marshal(obs.BuildEvent())
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{
		"event_type", "project_id", "resource_type", "resource_id",
		"resource_name", "metrics", "metadata", "idempotency_key", "timestamp",
	}, keysOf(t, raw))

	var body struct {
		EventType      string             `json:"event_type"`
		ResourceType   string             `json:"resource_type"`
		IdempotencyKey string             `json:"idempotency_key"`
		Metrics        map[string]float64 `json:"metrics"`
		Metadata       map[string]string  `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))

	assert.Equal(t, string(events.EventBuildCompleted), body.EventType)
	assert.Equal(t, UsageResourceType, body.ResourceType)
	// The idempotency key is the artefact's UID, not anything this process
	// computed.
	assert.Equal(t, podUID, body.IdempotencyKey)

	// Exactly the metrics that were observed. cache_bytes_* and egress_bytes
	// are absent because nothing measures them — see the package comment.
	assert.ElementsMatch(t, []string{"duration_seconds", "slot_seconds"}, mapKeys(body.Metrics))

	assert.ElementsMatch(t, []string{
		"source", "outcome", "scale_set", "tenant", "repo", "workflow", "job",
		"runner_image_digest",
	}, mapKeys(body.Metadata))
	assert.Equal(t, EventSource, body.Metadata["source"])
}

// Unobserved metadata is absent, not blank. A blank `repo` is a claim that the
// runner ran no repository's job.
func TestBuildEvent_UnobservedFieldsAreAbsent(t *testing.T) {
	pod := completedPod(corev1.PodSucceeded)
	delete(pod.Labels, "enclii.dev/tenant")
	pod.Status.ContainerStatuses[0].ImageID = ""

	obs, err := Observe(pod, testConfig())
	require.NoError(t, err)

	ev := obs.BuildEvent()
	assert.NotContains(t, ev.Metadata, "tenant")
	assert.NotContains(t, ev.Metadata, "runner_image_digest")
	assert.NotContains(t, ev.Metadata, "repo")
	assert.NotContains(t, ev.Metrics, "cache_bytes_read")
	assert.NotContains(t, ev.Metrics, "cache_bytes_written")
	assert.NotContains(t, ev.Metrics, "egress_bytes")
}

func keysOf(t *testing.T, raw []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- Controller ------------------------------------------------------------

type recordingEmitter struct {
	mu     sync.Mutex
	events []events.EventRequest
	err    error
}

func (r *recordingEmitter) Emit(_ context.Context, ev events.EventRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, ev)
	return nil
}

func (r *recordingEmitter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

type staticMetadata struct{ ref JobRef }

func (s staticMetadata) LookupJob(string, string) (JobRef, bool) { return s.ref, true }

func newTestController(t *testing.T, em Emitter) (*Controller, *Metrics) {
	t.Helper()
	metrics := NewMetrics(prometheus.NewRegistry())
	c := NewController(fake.NewSimpleClientset(), testConfig(), em,
		staticMetadata{ref: JobRef{Repo: "madfam-org/enclii", Workflow: "ci.yml", Job: "unit-tests"}},
		metrics, nil)
	return c, metrics
}

// The property the whole meter rests on: a completed pod observed twice —
// which happens on every informer resync and every controller restart — emits
// exactly one event.
func TestHandle_CompletedPodObservedTwiceEmitsOnce(t *testing.T) {
	em := &recordingEmitter{}
	c, metrics := newTestController(t, em)

	pod := completedPod(corev1.PodSucceeded)
	c.Handle(context.Background(), pod)
	c.Handle(context.Background(), pod)

	assert.Equal(t, 1, em.count())
	assert.InDelta(t, 1, counterValue(t, metrics.Emitted), 1e-9)
	assert.InDelta(t, 1, counterValue(t, metrics.Duplicate), 1e-9)
	assert.InDelta(t, 2, counterValue(t, metrics.Observed), 1e-9)
}

func TestHandle_RunningPodEmitsNothing(t *testing.T) {
	em := &recordingEmitter{}
	c, metrics := newTestController(t, em)

	pod := completedPod(corev1.PodRunning)
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(baseTime)},
	}
	c.Handle(context.Background(), pod)

	assert.Zero(t, em.count())
	// A running pod is not an event and is not even counted as observed.
	assert.InDelta(t, 0, counterValue(t, metrics.Observed), 1e-9)
}

func TestHandle_UnattributedPodIsCountedAndDropped(t *testing.T) {
	em := &recordingEmitter{}
	c, metrics := newTestController(t, em)

	pod := completedPod(corev1.PodSucceeded)
	delete(pod.Labels, "enclii.dev/project-id")
	c.Handle(context.Background(), pod)

	assert.Zero(t, em.count())
	assert.InDelta(t, 1, counterValue(t, metrics.Unattributed), 1e-9)
}

// A refused delivery is counted as a loss, and the pod is NOT marked as
// re-emittable — the UID stays in the seen set, because Waybill may have
// recorded the event and answered late. Double-billing on a timeout is worse
// than the miss, and the miss is visible in the rejected counter.
func TestHandle_RejectedDeliveryIsCounted(t *testing.T) {
	em := &recordingEmitter{err: errors.New("HTTP 503")}
	c, metrics := newTestController(t, em)

	c.Handle(context.Background(), completedPod(corev1.PodSucceeded))

	assert.InDelta(t, 1, counterValue(t, metrics.Rejected), 1e-9)
	assert.InDelta(t, 0, counterValue(t, metrics.Emitted), 1e-9)
}

func TestHandle_EnrichmentReachesTheEvent(t *testing.T) {
	em := &recordingEmitter{}
	c, _ := newTestController(t, em)

	c.Handle(context.Background(), completedPod(corev1.PodSucceeded))

	require.Equal(t, 1, em.count())
	assert.Equal(t, "madfam-org/enclii", em.events[0].Metadata["repo"])
	assert.Equal(t, "unit-tests", em.events[0].Metadata["job"])
}

// The seen set is a bounded ring. Forgetting a UID is safe (the idempotency
// key makes the re-emission a no-op at Waybill); an unbounded map is an OOM
// with a several-week fuse.
func TestMarkSeen_IsBoundedAndForgetsOldest(t *testing.T) {
	c := NewController(nil, testConfig(), nil, nil, nil, nil)
	c.seen = make(map[string]struct{}, 2)
	c.seenRing = make([]string, 2)

	assert.False(t, c.markSeen("a"))
	assert.True(t, c.markSeen("a"))
	assert.False(t, c.markSeen("b"))
	assert.False(t, c.markSeen("c")) // evicts "a"

	assert.Len(t, c.seen, 2)
	assert.True(t, c.markSeen("c"))
	assert.False(t, c.markSeen("a"), "the oldest UID is forgotten, which is safe by design")
}

// The informer wiring, against a fake API server: a pod that is already
// terminal when the controller starts is metered by the initial LIST, without
// any watch event at all. This is the restart path.
func TestRun_MetersAPodPresentAtStartup(t *testing.T) {
	// The fake clientset's Watch cannot serve a streaming initial list, which
	// is the path client-go now prefers. Turning the gate off here exercises
	// the ordinary list-then-watch reflector; it changes nothing about how the
	// controller behaves against a real API server, which supports both.
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)

	em := &recordingEmitter{}
	client := fake.NewSimpleClientset(completedPod(corev1.PodSucceeded))
	metrics := NewMetrics(prometheus.NewRegistry())
	c := NewController(client, testConfig(), em, nil, metrics, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Run(ctx)
	}()

	require.Eventually(t, func() bool { return em.count() == 1 }, 4*time.Second, 10*time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, 1, em.count())
	assert.Equal(t, podUID, em.events[0].IdempotencyKey)
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.Write(&m))
	return m.GetCounter().GetValue()
}

// --- EphemeralRunner enrichment -------------------------------------------

func TestEphemeralRunnerStore_NilIsSafe(t *testing.T) {
	var s *EphemeralRunnerStore
	ref, ok := s.LookupJob("arc-runners", "x")
	assert.False(t, ok)
	assert.Equal(t, JobRef{}, ref)
	assert.False(t, s.WaitForSync(context.Background()))
	assert.Nil(t, NewEphemeralRunnerStore(nil, "arc-runners", nil))
}

func TestNewHTTPEmitter_UnsetBaseURLDisablesTheFeature(t *testing.T) {
	assert.Nil(t, NewHTTPEmitter("", "key"))
	assert.Nil(t, NewHTTPEmitter("   ", "key"))
	assert.NotNil(t, NewHTTPEmitter("http://waybill", ""))
}

func TestHTTPEmitter_SendsTheDocumentedRequest(t *testing.T) {
	obs, err := Observe(completedPod(corev1.PodSucceeded), testConfig())
	require.NoError(t, err)

	var (
		gotPath string
		gotKey  string
		gotUA   string
		gotBody []byte
	)
	srv := newTestServer(func(path, apiKey, ua string, body []byte) int {
		gotPath, gotKey, gotUA, gotBody = path, apiKey, ua, body
		return 201
	})
	defer srv.Close()

	em := NewHTTPEmitter(srv.URL, "test-key")
	require.NotNil(t, em)
	require.NoError(t, em.Emit(context.Background(), obs.BuildEvent()))

	assert.Equal(t, "/internal/events", gotPath)
	// Same auth header and same idempotency field the addon usage emitter
	// uses: one dialect at one endpoint, or the dedup index stops deduping.
	assert.Equal(t, "test-key", gotKey)
	assert.Equal(t, "weighbridge/1.0", gotUA)
	assert.Contains(t, string(gotBody), fmt.Sprintf("%q:%q", "idempotency_key", podUID))
}

func TestHTTPEmitter_NonSuccessIsAnErrorAndLeaksNoBody(t *testing.T) {
	obs, err := Observe(completedPod(corev1.PodSucceeded), testConfig())
	require.NoError(t, err)

	srv := newTestServer(func(string, string, string, []byte) int { return 500 })
	defer srv.Close()

	em := NewHTTPEmitter(srv.URL, "")
	err = em.Emit(context.Background(), obs.BuildEvent())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
	// The upstream body can echo the request, which carries a tenant and a
	// repository name, and this error is logged.
	assert.NotContains(t, err.Error(), "echoed-request-body")
}

// newTestServer records what arrived and answers with the status the handler
// returns, echoing the request body so a leak of it into an error message
// would be visible.
func newTestServer(fn func(path, apiKey, ua string, body []byte) int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		status := fn(r.URL.Path, r.Header.Get("X-API-Key"), r.Header.Get("User-Agent"), body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte("echoed-request-body"))
	}))
}
