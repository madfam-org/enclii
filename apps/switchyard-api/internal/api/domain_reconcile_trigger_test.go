package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/reconciler"
)

type fakeDomainReconciler struct {
	result      *reconciler.ReconcileResult
	calls       int
	ctxErr      error
	hasDeadline bool
	deadlineIn  time.Duration
	service     uuid.UUID
	environment uuid.UUID
}

func (f *fakeDomainReconciler) ReconcileDomains(ctx context.Context, serviceID, environmentID uuid.UUID) *reconciler.ReconcileResult {
	f.calls++
	f.ctxErr = ctx.Err()
	if d, ok := ctx.Deadline(); ok {
		f.hasDeadline = true
		f.deadlineIn = time.Until(d)
	}
	f.service, f.environment = serviceID, environmentID
	return f.result
}

// TestTriggerDomainReconciliationRunsDetached: the trigger runs after the
// HTTP handler has returned, so it must not inherit the request context
// (cancelled on return). It gets its own bounded context instead.
func TestTriggerDomainReconciliationRunsDetached(t *testing.T) {
	results := []struct {
		name   string
		result *reconciler.ReconcileResult
	}{
		{"success", &reconciler.ReconcileResult{Success: true}},
		{"skipped", &reconciler.ReconcileResult{Success: true, Message: "GitOps-bound service", Error: reconciler.ErrDomainReconcileSkipped}},
		{"failure", &reconciler.ReconcileResult{Success: false, Message: "read custom domains", Error: errors.New("db down")}},
		{"nil result", nil},
	}
	for _, tt := range results {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeDomainReconciler{result: tt.result}
			h := &Handler{domainReconcilerOverride: fake, logger: testPollerLogger(t)}
			svc, env := uuid.New(), uuid.New()

			h.triggerDomainReconciliation(svc, env)

			if fake.calls != 1 {
				t.Fatalf("ReconcileDomains calls = %d, want 1", fake.calls)
			}
			if fake.ctxErr != nil {
				t.Fatalf("reconcile ran on a finished context: %v", fake.ctxErr)
			}
			if !fake.hasDeadline || fake.deadlineIn <= 0 || fake.deadlineIn > reconciler.DomainReconcileTimeout {
				t.Fatalf("deadline in %v (set=%v), want within %v", fake.deadlineIn, fake.hasDeadline, reconciler.DomainReconcileTimeout)
			}
			if fake.service != svc || fake.environment != env {
				t.Fatalf("reconciled (%s, %s), want (%s, %s)", fake.service, fake.environment, svc, env)
			}
		})
	}
}

func TestTriggerDomainReconciliationWithoutReconcilerDoesNotPanic(t *testing.T) {
	h := &Handler{logger: testPollerLogger(t)}
	h.triggerDomainReconciliation(uuid.New(), uuid.New())
}
