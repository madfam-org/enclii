package api

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func testPollerLogger(t *testing.T) logging.Logger {
	t.Helper()
	// "error" level keeps the poller's Info/Warn lines out of test output while
	// still exercising the real structured logger.
	lg, err := logging.NewStructuredLogger(&logging.LogConfig{
		Level:       "error",
		Format:      "json",
		Output:      "stdout",
		ServiceName: "switchyard-api-test",
	})
	if err != nil {
		t.Fatalf("failed to build test logger: %v", err)
	}
	return lg
}

// newTestArgoApp builds a minimal ArgoCD Application unstructured with the
// status fields the poller reads.
func newTestArgoApp(name, syncStatus, health, revision string, images ...string) unstructured.Unstructured {
	imgs := make([]interface{}, len(images))
	for i, im := range images {
		imgs[i] = im
	}
	return unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": "argocd",
		},
		"status": map[string]interface{}{
			"sync": map[string]interface{}{
				"status":   syncStatus,
				"revision": revision,
			},
			"health": map[string]interface{}{
				"status": health,
			},
			"summary": map[string]interface{}{
				"images": imgs,
			},
		},
	}}
}

// fakeArgoLister is an in-memory argoAppLister for orchestration tests.
type fakeArgoLister struct {
	apps []unstructured.Unstructured
	err  error
}

func (f fakeArgoLister) ListApplications(ctx context.Context) ([]unstructured.Unstructured, error) {
	return f.apps, f.err
}

func TestArgocdObservationFromApplication(t *testing.T) {
	tests := []struct {
		name        string
		app         unstructured.Unstructured
		wantOK      bool
		wantTrigger string
		wantImages  int
	}{
		{
			name:        "healthy OutOfSync app is recordable (the tulana freeze shape)",
			app:         newTestArgoApp("tulana-services", "OutOfSync", "Healthy", "rev1", "ghcr.io/madfam-org/tulana/api@sha256:aaa"),
			wantOK:      true,
			wantTrigger: "sync-succeeded",
			wantImages:  1,
		},
		{
			name:        "healthy Synced app is recordable",
			app:         newTestArgoApp("core-services", "Synced", "Healthy", "rev1", "ghcr.io/madfam-org/enclii/switchyard-api@sha256:bbb"),
			wantOK:      true,
			wantTrigger: "sync-succeeded",
			wantImages:  1,
		},
		{
			name:        "degraded app records a failure",
			app:         newTestArgoApp("app-services", "Synced", "Degraded", "rev1", "ghcr.io/x/y@sha256:ccc"),
			wantOK:      true,
			wantTrigger: "sync-failed",
			wantImages:  1,
		},
		{
			name:   "progressing app is skipped (transient)",
			app:    newTestArgoApp("app-services", "Synced", "Progressing", "rev1", "ghcr.io/x/y@sha256:ddd"),
			wantOK: false,
		},
		{
			name:   "unknown health is skipped",
			app:    newTestArgoApp("app-services", "Synced", "Unknown", "rev1", "ghcr.io/x/y@sha256:eee"),
			wantOK: false,
		},
		{
			name:   "healthy but no revision is skipped",
			app:    newTestArgoApp("app-services", "Synced", "Healthy", "", "ghcr.io/x/y@sha256:fff"),
			wantOK: false,
		},
		{
			name:   "healthy but no images is skipped",
			app:    newTestArgoApp("app-services", "Synced", "Healthy", "rev1"),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs, ok := argocdObservationFromApplication(tt.app)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if obs.Trigger != tt.wantTrigger {
				t.Errorf("trigger = %q, want %q", obs.Trigger, tt.wantTrigger)
			}
			if len(obs.Images) != tt.wantImages {
				t.Errorf("images = %d, want %d", len(obs.Images), tt.wantImages)
			}
		})
	}
}

func TestArgocdObservationAlreadyTracked(t *testing.T) {
	relID := uuid.New()
	runningDep := &types.Deployment{ID: uuid.New(), ReleaseID: relID, Status: types.DeploymentStatusRunning}

	tests := []struct {
		name     string
		dep      *types.Deployment
		rel      *types.Release
		revision string
		image    string
		want     bool
	}{
		{
			name:     "no deployment yet is not tracked",
			dep:      nil,
			rel:      nil,
			revision: "rev1",
			image:    "ghcr.io/x/y@sha256:aaa",
			want:     false,
		},
		{
			name:     "same revision is tracked",
			dep:      runningDep,
			rel:      &types.Release{ID: relID, GitSHA: "rev1", ImageURI: "ghcr.io/x/y@sha256:aaa"},
			revision: "rev1",
			image:    "ghcr.io/x/y@sha256:aaa",
			want:     true,
		},
		{
			name:     "different revision is not tracked",
			dep:      runningDep,
			rel:      &types.Release{ID: relID, GitSHA: "rev0", ImageURI: "ghcr.io/x/y@sha256:old"},
			revision: "rev1",
			image:    "ghcr.io/x/y@sha256:new",
			want:     false,
		},
		{
			name:     "empty stored git sha falls back to matching digest",
			dep:      runningDep,
			rel:      &types.Release{ID: relID, GitSHA: "", ImageURI: "ghcr.io/x/y@sha256:aaa"},
			revision: "rev1",
			image:    "ghcr.io/x/y@sha256:aaa",
			want:     true,
		},
		{
			name:     "empty stored git sha with different digest is not tracked",
			dep:      runningDep,
			rel:      &types.Release{ID: relID, GitSHA: "", ImageURI: "ghcr.io/x/y@sha256:aaa"},
			revision: "rev1",
			image:    "ghcr.io/x/y@sha256:bbb",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := argocdObservationAlreadyTracked(tt.dep, tt.rel, tt.revision, tt.image)
			if got != tt.want {
				t.Errorf("argocdObservationAlreadyTracked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArgocdPollDecision(t *testing.T) {
	svc := &types.Service{ID: uuid.New(), Name: "tulana-api"}
	image := "ghcr.io/madfam-org/tulana/api@sha256:ba27878a"

	knownService := func(imageURI string) *types.Service { return svc }
	unknownService := func(imageURI string) *types.Service { return nil }

	trackedAt := func(revision, imageURI string) func(string) (*types.Deployment, *types.Release) {
		relID := uuid.New()
		return func(string) (*types.Deployment, *types.Release) {
			return &types.Deployment{ID: uuid.New(), ReleaseID: relID, Status: types.DeploymentStatusRunning},
				&types.Release{ID: relID, GitSHA: revision, ImageURI: imageURI}
		}
	}
	neverTracked := func(string) (*types.Deployment, *types.Release) { return nil, nil }

	t.Run("new revision yields exactly one changed image", func(t *testing.T) {
		obs := argocdObservation{AppName: "tulana-services", Revision: "rev-new", Images: []string{image}, Trigger: "sync-succeeded"}
		got := argocdPollDecision(obs, knownService, trackedAt("rev-old", "ghcr.io/madfam-org/tulana/api@sha256:old"))
		if len(got) != 1 || got[0] != image {
			t.Fatalf("changed = %v, want [%s]", got, image)
		}
	})

	t.Run("never-tracked service yields the image (first observation)", func(t *testing.T) {
		obs := argocdObservation{AppName: "tulana-services", Revision: "rev-new", Images: []string{image}, Trigger: "sync-succeeded"}
		got := argocdPollDecision(obs, knownService, neverTracked)
		if len(got) != 1 {
			t.Fatalf("changed = %v, want one image", got)
		}
	})

	t.Run("already-tracked revision is a no-op (idempotency)", func(t *testing.T) {
		obs := argocdObservation{AppName: "tulana-services", Revision: "rev-current", Images: []string{image}, Trigger: "sync-succeeded"}
		got := argocdPollDecision(obs, knownService, trackedAt("rev-current", image))
		if len(got) != 0 {
			t.Fatalf("changed = %v, want none", got)
		}
	})

	t.Run("unknown app is skipped", func(t *testing.T) {
		obs := argocdObservation{AppName: "mystery", Revision: "rev-new", Images: []string{image}, Trigger: "sync-succeeded"}
		got := argocdPollDecision(obs, unknownService, neverTracked)
		if len(got) != 0 {
			t.Fatalf("changed = %v, want none", got)
		}
	})

	t.Run("empty revision yields nothing", func(t *testing.T) {
		obs := argocdObservation{AppName: "tulana-services", Revision: "", Images: []string{image}}
		got := argocdPollDecision(obs, knownService, neverTracked)
		if len(got) != 0 {
			t.Fatalf("changed = %v, want none", got)
		}
	})

	t.Run("duplicate images are de-duplicated", func(t *testing.T) {
		obs := argocdObservation{AppName: "tulana-services", Revision: "rev-new", Images: []string{image, image}, Trigger: "sync-succeeded"}
		got := argocdPollDecision(obs, knownService, neverTracked)
		if len(got) != 1 {
			t.Fatalf("changed = %v, want one image", got)
		}
	})
}

// TestArgocdPollerReconcile exercises the full reconcile path (list → observe →
// decide → apply) with a fake lister, injected repo resolvers, and a spy apply.
func TestArgocdPollerReconcile(t *testing.T) {
	image := "ghcr.io/madfam-org/tulana/api@sha256:ba27878a"
	svc := &types.Service{ID: uuid.New(), Name: "tulana-api"}

	newPoller := func(
		apps []unstructured.Unstructured,
		resolve func(context.Context, string) *types.Service,
		tracked func(context.Context, string) (*types.Deployment, *types.Release),
		apply func(context.Context, ArgocdSyncRequest, string) int,
	) *ArgocdPoller {
		return &ArgocdPoller{
			lister:         fakeArgoLister{apps: apps},
			resolveService: resolve,
			latestTracked:  tracked,
			apply:          apply,
			interval:       time.Minute,
			logger:         testPollerLogger(t),
			stopCh:         make(chan struct{}),
		}
	}

	t.Run("creates exactly one record for a new untracked revision", func(t *testing.T) {
		var calls []ArgocdSyncRequest
		var sources []string
		apply := func(_ context.Context, req ArgocdSyncRequest, source string) int {
			calls = append(calls, req)
			sources = append(sources, source)
			return len(req.Images)
		}
		p := newPoller(
			[]unstructured.Unstructured{newTestArgoApp("tulana-services", "OutOfSync", "Healthy", "rev-new", image)},
			func(context.Context, string) *types.Service { return svc },
			func(context.Context, string) (*types.Deployment, *types.Release) { return nil, nil }, // never tracked
			apply,
		)

		p.reconcile(context.Background())

		if len(calls) != 1 {
			t.Fatalf("apply called %d times, want 1", len(calls))
		}
		if len(calls[0].Images) != 1 || calls[0].Images[0] != image {
			t.Errorf("applied images = %v, want [%s]", calls[0].Images, image)
		}
		if calls[0].Revision != "rev-new" {
			t.Errorf("applied revision = %q, want rev-new", calls[0].Revision)
		}
		if calls[0].Trigger != "sync-succeeded" {
			t.Errorf("applied trigger = %q, want sync-succeeded", calls[0].Trigger)
		}
		if len(sources) != 1 || sources[0] != argocdSyncSourcePoller {
			t.Errorf("apply source = %v, want [%s]", sources, argocdSyncSourcePoller)
		}
	})

	t.Run("no-op when the observed revision is already tracked (idempotency)", func(t *testing.T) {
		called := false
		apply := func(context.Context, ArgocdSyncRequest, string) int { called = true; return 0 }

		relID := uuid.New()
		tracked := func(context.Context, string) (*types.Deployment, *types.Release) {
			return &types.Deployment{ID: uuid.New(), ReleaseID: relID, Status: types.DeploymentStatusRunning},
				&types.Release{ID: relID, GitSHA: "rev-current", ImageURI: image}
		}
		p := newPoller(
			[]unstructured.Unstructured{newTestArgoApp("tulana-services", "OutOfSync", "Healthy", "rev-current", image)},
			func(context.Context, string) *types.Service { return svc },
			tracked,
			apply,
		)

		p.reconcile(context.Background())

		if called {
			t.Fatal("apply must not be called when the revision is already tracked")
		}
	})

	t.Run("unknown application is skipped", func(t *testing.T) {
		called := false
		apply := func(context.Context, ArgocdSyncRequest, string) int { called = true; return 0 }
		p := newPoller(
			[]unstructured.Unstructured{newTestArgoApp("mystery-app", "Synced", "Healthy", "rev-new", "ghcr.io/other/thing@sha256:zzz")},
			func(context.Context, string) *types.Service { return nil }, // unknown
			func(context.Context, string) (*types.Deployment, *types.Release) { return nil, nil },
			apply,
		)

		p.reconcile(context.Background())

		if called {
			t.Fatal("apply must not be called for an unknown application")
		}
	})

	t.Run("transient (Progressing) application is skipped", func(t *testing.T) {
		called := false
		apply := func(context.Context, ArgocdSyncRequest, string) int { called = true; return 0 }
		p := newPoller(
			[]unstructured.Unstructured{newTestArgoApp("tulana-services", "Synced", "Progressing", "rev-new", image)},
			func(context.Context, string) *types.Service { return svc },
			func(context.Context, string) (*types.Deployment, *types.Release) { return nil, nil },
			apply,
		)

		p.reconcile(context.Background())

		if called {
			t.Fatal("apply must not be called for a Progressing application")
		}
	})
}

func TestParseArgocdPollInterval(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "valid duration", raw: "2m", want: 2 * time.Minute},
		{name: "valid seconds", raw: "90s", want: 90 * time.Second},
		{name: "empty falls back to default", raw: "", want: DefaultArgocdPollInterval},
		{name: "garbage falls back to default", raw: "not-a-duration", want: DefaultArgocdPollInterval},
		{name: "zero falls back to default", raw: "0s", want: DefaultArgocdPollInterval},
		{name: "below floor is clamped", raw: "5s", want: MinArgocdPollInterval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseArgocdPollInterval(tt.raw); got != tt.want {
				t.Errorf("ParseArgocdPollInterval(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestImageDigest(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{image: "ghcr.io/madfam-org/tulana/api@sha256:ba27878a", want: "sha256:ba27878a"},
		{image: "ghcr.io/madfam-org/tulana/api:main", want: ""},
		{image: "ghcr.io/madfam-org/tulana/api", want: ""},
	}
	for _, tt := range tests {
		if got := imageDigest(tt.image); got != tt.want {
			t.Errorf("imageDigest(%q) = %q, want %q", tt.image, got, tt.want)
		}
	}
}
