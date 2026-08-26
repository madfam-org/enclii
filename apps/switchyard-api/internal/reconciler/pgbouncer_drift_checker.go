package reconciler

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
)

// defaultPgbouncerDriftCheckInterval is how often the checker re-runs the
// userlist reconcile detector. 5 minutes matches the task's paging budget:
// the alert rule fires on missing_users > 0 for 10m, so a 5-minute check
// interval gives at least one confirming sample before that window closes.
const defaultPgbouncerDriftCheckInterval = 5 * time.Minute

// userlistDetector is the narrow slice of *provisioning.PgBouncerUpdater this
// checker depends on. Defining it as an interface (rather than depending on
// the concrete type directly) lets unit tests inject a fake detector without
// a live k8s clientset or Postgres connection — the same pattern
// NamespaceDiscoverer uses for workloadLister.
type userlistDetector interface {
	ReconcileUserlist(ctx context.Context, adminURL string) (provisioning.UserlistDrift, error)
}

// PgbouncerDriftChecker periodically runs the pgbouncer userlist reconcile
// detector (enclii#436, internal/provisioning/userlist_reconcile.go) and
// exports its result as Prometheus metrics so an alert rule can page on it.
//
// The detector itself was shipped DETECT-ONLY: its own doc comment says
// "wire it into a periodic check that pages when HasMissing() is true." Prior
// to this checker nothing ever called it outside the manual admin API
// endpoint (GET /v1/admin/provision/pgbouncer/reconcile) — the 2026-08-24
// outage (four login roles silently dropped from the userlist, taking
// fortuna/bloom-scroll/ceq hard-down for days behind a 502) is the class of
// incident this closes: from "nobody looked" to "a page fires within the
// check interval + alert `for:` window."
//
// This checker performs NO mutation of its own — it only calls
// ReconcileUserlist (itself read-only, see userlist_reconcile.go) and records
// gauges/counters. Repair stays the deliberate, per-service operator action
// documented there.
type PgbouncerDriftChecker struct {
	detector userlistDetector
	adminURL string
	logger   *logrus.Logger

	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewPgbouncerDriftChecker constructs a checker with the default 5-minute
// interval. detector is typically *provisioning.PgBouncerUpdater (nil when
// the k8s client was unavailable at startup, mirroring how main.go wires the
// admin API's own pgbouncerUpdater); adminURL is the Postgres superuser
// connection string (config.PostgresAdminURL) the detector needs to query
// pg_authid.
//
// The nil check here matters more than it looks: detector is a concrete
// pointer type, and assigning a nil *provisioning.PgBouncerUpdater directly
// into the userlistDetector interface field would produce a non-nil interface
// wrapping a nil pointer (the classic Go typed-nil gotcha) — check()'s
// `c.detector == nil` guard would then never fire, and the first tick would
// panic on a nil-receiver call instead of cleanly logging "not configured".
// Converting to a true nil interface value here is what keeps that guard
// meaningful.
func NewPgbouncerDriftChecker(detector *provisioning.PgBouncerUpdater, adminURL string, logger *logrus.Logger) *PgbouncerDriftChecker {
	c := &PgbouncerDriftChecker{
		adminURL: adminURL,
		logger:   logger,
		interval: defaultPgbouncerDriftCheckInterval,
		stopCh:   make(chan struct{}),
	}
	if detector != nil {
		c.detector = detector
	}
	return c
}

// Start launches the check loop in a goroutine and returns immediately. It
// runs an immediate pass on startup (unlike NamespaceDiscoverer, which waits
// a full interval) because a drift condition present at boot — e.g. this
// process restarting shortly after a hand-applied userlist edit — should
// surface on the very next scrape, not up to 5 minutes later.
func (c *PgbouncerDriftChecker) Start(ctx context.Context) {
	c.logger.WithField("interval", c.interval).Info("Starting pgbouncer userlist drift checker")

	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		c.safeCheck(ctx)

		for {
			select {
			case <-ticker.C:
				c.safeCheck(ctx)
			case <-c.stopCh:
				c.logger.Info("Pgbouncer userlist drift checker stopped")
				return
			case <-ctx.Done():
				c.logger.Info("Pgbouncer userlist drift checker context cancelled")
				return
			}
		}
	}()
}

// Stop gracefully shuts down the check loop. Safe to call multiple times.
func (c *PgbouncerDriftChecker) Stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
}

// safeCheck runs one check pass with panic recovery, so a bug in the
// underlying detector logs and is retried on the next tick rather than
// killing the checker goroutine — which would silently turn the dead-man
// timestamp gauge stale exactly when nobody is watching the checker itself,
// the failure mode the timestamp metric exists to catch, but only if the
// goroutine survives to eventually go stale rather than panicking the whole
// process.
func (c *PgbouncerDriftChecker) safeCheck(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			c.logger.WithField("panic", rec).Error("Pgbouncer userlist drift check panicked; will retry next tick")
			monitoring.RecordPgbouncerUserlistCheckError()
		}
	}()
	c.check(ctx)
}

// check is the testable core: it drives the injected detector and records
// the result as metrics. Errors are logged and counted, never returned or
// panicked — the loop must keep ticking even if a single pass fails (e.g. a
// transient Postgres connection flake).
func (c *PgbouncerDriftChecker) check(ctx context.Context) {
	if c.detector == nil {
		c.logger.Warn("Pgbouncer userlist drift checker: detector not configured, skipping pass")
		monitoring.RecordPgbouncerUserlistCheckError()
		return
	}
	if c.adminURL == "" {
		c.logger.Warn("Pgbouncer userlist drift checker: Postgres admin URL not configured, skipping pass")
		monitoring.RecordPgbouncerUserlistCheckError()
		return
	}

	drift, err := c.detector.ReconcileUserlist(ctx, c.adminURL)
	if err != nil {
		c.logger.WithError(err).Error("Pgbouncer userlist drift check failed")
		monitoring.RecordPgbouncerUserlistCheckError()
		return
	}

	missing := len(drift.MissingFromUserlist)
	monitoring.RecordPgbouncerUserlistCheckSuccess(missing)

	if missing > 0 {
		c.logger.WithField("missing_count", missing).Warn("Pgbouncer userlist drift checker: login roles missing from userlist — pooled auth will fail for them")
	}
}
