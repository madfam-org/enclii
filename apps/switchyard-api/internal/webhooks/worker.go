package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// WorkerConfig tunes the goroutine pool.  All fields have sensible
// defaults; only override when you really need to.
type WorkerConfig struct {
	// PoolSize is the number of concurrent delivery goroutines.
	// Defaults to 5.
	PoolSize int
	// PollInterval is how often to check for newly eligible
	// deliveries when the last batch was empty. Defaults to 3s.
	PollInterval time.Duration
	// ClaimBatchSize limits how many rows each poll transitions
	// from pending → delivering. Defaults to 20.
	ClaimBatchSize int
	// HTTPTimeout caps the subscriber-response wait. Defaults to 10s.
	HTTPTimeout time.Duration
	// UserAgent is sent as the outgoing User-Agent header.
	UserAgent string
}

// Worker is the background delivery loop. Start it once at boot via
// `go worker.Run(ctx)`. Cancelling the context shuts it down cleanly.
type Worker struct {
	cfg       WorkerConfig
	repos     *db.Repositories
	encryptor *Encryptor
	client    *http.Client
	log       Logger
	wg        sync.WaitGroup
}

// NewWorker wires the delivery loop. `encryptor` is required — the
// worker needs to decrypt the stored signing secret to sign outgoing
// requests.
func NewWorker(cfg WorkerConfig, repos *db.Repositories, enc *Encryptor, log Logger) *Worker {
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 5
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 3 * time.Second
	}
	if cfg.ClaimBatchSize <= 0 {
		cfg.ClaimBatchSize = 20
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Enclii-Webhook/1.0"
	}
	if log == nil {
		log = noopLogger{}
	}

	// One dedicated transport per worker keeps idle conns per host
	// bounded.  We disable redirects so 3xx surfaces as a terminal
	// error (prevents subscribers from tricking us into POSTing to
	// internal hostnames).
	tr := &http.Transport{
		MaxIdleConns:        cfg.PoolSize * 2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   cfg.HTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Worker{
		cfg:       cfg,
		repos:     repos,
		encryptor: enc,
		client:    client,
		log:       log,
	}
}

// Run blocks until ctx is cancelled. Spawns PoolSize goroutines, each
// independently polling the deliveries queue.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info(ctx, "outbound webhook worker starting",
		"pool_size", w.cfg.PoolSize,
		"poll_interval", w.cfg.PollInterval.String(),
	)
	for i := 0; i < w.cfg.PoolSize; i++ {
		w.wg.Add(1)
		go w.loop(ctx, i)
	}
	w.wg.Wait()
	w.log.Info(ctx, "outbound webhook worker stopped")
}

func (w *Worker) loop(ctx context.Context, id int) {
	defer w.wg.Done()

	// Each worker keeps its own timer so they don't all wake together
	// after a quiet period.
	t := time.NewTimer(w.cfg.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n := w.tick(ctx)
			// If we processed anything, poll again immediately;
			// otherwise wait the full PollInterval. This keeps
			// latency low under load without hammering Postgres
			// when idle.
			delay := w.cfg.PollInterval
			if n > 0 {
				delay = 50 * time.Millisecond
			}
			t.Reset(delay)
		}
	}
}

// tick claims and processes a batch. Returns the number of deliveries
// actually attempted.
func (w *Worker) tick(ctx context.Context) int {
	deliveries, err := w.repos.OutboundWebhooks.ClaimPendingDeliveries(ctx, w.cfg.ClaimBatchSize/w.cfg.PoolSize+1)
	if err != nil {
		w.log.Error(ctx, "claim deliveries failed", "error", err.Error())
		return 0
	}
	for _, d := range deliveries {
		w.process(ctx, d)
	}
	return len(deliveries)
}

// process performs one HTTP attempt. All paths update the delivery row
// to a terminal (delivered/dlq) or retryable (failed) state.
func (w *Worker) process(ctx context.Context, d *types.OutboundWebhookDelivery) {
	sub, err := w.repos.OutboundWebhooks.GetSubscription(ctx, d.SubscriptionID)
	if err != nil {
		w.log.Error(ctx, "subscription missing for delivery",
			"delivery_id", d.ID.String(), "error", err.Error())
		_ = w.repos.OutboundWebhooks.MarkDLQ(ctx, d.ID, nil, "subscription not found", "", 0)
		return
	}
	if !sub.Active || sub.AutoDisabledAt != nil {
		// The subscription was disabled between enqueue and delivery;
		// don't waste an attempt.
		_ = w.repos.OutboundWebhooks.MarkDLQ(ctx, d.ID, nil, "subscription disabled", "", 0)
		return
	}

	secretBlob, err := w.repos.OutboundWebhooks.GetSubscriptionSecret(ctx, d.SubscriptionID)
	if err != nil {
		w.log.Error(ctx, "fetch subscription secret failed",
			"delivery_id", d.ID.String(), "error", err.Error())
		w.recordFailure(ctx, d, nil, "secret fetch failed", "", 0)
		return
	}
	secret, err := w.encryptor.DecryptString(secretBlob)
	if err != nil {
		w.log.Error(ctx, "decrypt secret failed",
			"subscription_id", d.SubscriptionID.String(), "error", err.Error())
		w.recordFailure(ctx, d, nil, "secret decrypt failed", "", 0)
		return
	}

	// Rebuild the canonical body from the stored envelope pieces. The
	// stored payload is the `data` field only, so we reconstruct the
	// full envelope here (this guarantees byte-for-byte identical
	// signing even across worker restarts).
	envelope := &types.OutboundWebhookEnvelope{
		ID:         d.EventID,
		Type:       d.EventType,
		CreatedAt:  d.CreatedAt,
		APIVersion: types.OutboundWebhookAPIVersion,
		Data:       d.Payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		w.recordFailure(ctx, d, nil, "marshal envelope: "+err.Error(), "", 0)
		return
	}

	sig := Sign(secret, time.Now().UTC(), body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(body))
	if err != nil {
		w.recordFailure(ctx, d, nil, "build request: "+err.Error(), "", 0)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", w.cfg.UserAgent)
	req.Header.Set(types.OutboundWebhookSignatureHeader, sig)
	req.Header.Set(types.OutboundWebhookEventHeader, string(d.EventType))
	req.Header.Set(types.OutboundWebhookDeliveryIDHeader, d.ID.String())

	start := time.Now()
	resp, httpErr := w.client.Do(req)
	durationMs := int(time.Since(start) / time.Millisecond)

	var (
		httpStatus *int
		snippet    string
		respErr    error
		retryAfter time.Duration
	)

	if httpErr != nil {
		respErr = httpErr
	} else {
		code := resp.StatusCode
		httpStatus = &code
		snippet = readSnippet(resp.Body)
		// Honor Retry-After on 429/503 (seconds form only; RFC date
		// form is uncommon in practice).
		if h := resp.Header.Get("Retry-After"); h != "" {
			if n, perr := strconv.Atoi(h); perr == nil && n > 0 && n < 3600 {
				retryAfter = time.Duration(n) * time.Second
			}
		}
		_ = resp.Body.Close()
	}

	// 2xx → terminal success
	if httpStatus != nil && *httpStatus >= 200 && *httpStatus < 300 {
		if err := w.repos.OutboundWebhooks.MarkDelivered(ctx, d.ID, *httpStatus, snippet, durationMs); err != nil {
			w.log.Error(ctx, "mark delivered failed",
				"delivery_id", d.ID.String(), "error", err.Error())
		}
		_ = w.repos.OutboundWebhooks.RecordDeliverySuccess(ctx, d.SubscriptionID)
		return
	}

	// Decide retry vs terminal
	if ShouldRetry(safeCode(httpStatus), respErr) {
		errMsg := errToString(respErr)
		delay, ok := NextRetryDelay(d.AttemptNumber)
		if !ok {
			w.recordDLQ(ctx, d, httpStatus, errMsg, snippet, durationMs)
			return
		}
		if retryAfter > 0 && retryAfter > delay {
			delay = retryAfter
		}
		nextAttempt := time.Now().Add(delay).UTC()
		// Mark current attempt as failed with its retry time, then
		// enqueue a brand-new row for attempt N+1.  This preserves
		// history per the append-only spec.
		_ = w.repos.OutboundWebhooks.MarkFailed(ctx, d.ID, httpStatus, errMsg, snippet, durationMs, nextAttempt)

		next := &types.OutboundWebhookDelivery{
			SubscriptionID:   d.SubscriptionID,
			LifecycleEventID: d.LifecycleEventID,
			EventID:          d.EventID,
			EventType:        d.EventType,
			Payload:          d.Payload,
			PayloadSHA256:    d.PayloadSHA256,
			AttemptNumber:    d.AttemptNumber + 1,
			Status:           types.OutboundDeliveryPending,
			NextRetryAt:      &nextAttempt,
		}
		if err := w.repos.OutboundWebhooks.CreateDelivery(ctx, next); err != nil {
			w.log.Error(ctx, "enqueue retry failed",
				"delivery_id", d.ID.String(), "error", err.Error())
		}
		_, _ = w.repos.OutboundWebhooks.RecordDeliveryFailure(ctx, d.SubscriptionID, types.OutboundWebhookAutoDisableThreshold)
		return
	}

	// Terminal failure (e.g. 4xx other than 408/429)
	w.recordDLQ(ctx, d, httpStatus, errToString(respErr), snippet, durationMs)
}

func (w *Worker) recordDLQ(ctx context.Context, d *types.OutboundWebhookDelivery, httpStatus *int, errMsg, snippet string, durationMs int) {
	if err := w.repos.OutboundWebhooks.MarkDLQ(ctx, d.ID, httpStatus, errMsg, snippet, durationMs); err != nil {
		w.log.Error(ctx, "mark dlq failed",
			"delivery_id", d.ID.String(), "error", err.Error())
	}
	_, _ = w.repos.OutboundWebhooks.RecordDeliveryFailure(ctx, d.SubscriptionID, types.OutboundWebhookAutoDisableThreshold)
}

func (w *Worker) recordFailure(ctx context.Context, d *types.OutboundWebhookDelivery, httpStatus *int, errMsg, snippet string, durationMs int) {
	delay, ok := NextRetryDelay(d.AttemptNumber)
	if !ok {
		w.recordDLQ(ctx, d, httpStatus, errMsg, snippet, durationMs)
		return
	}
	nextAttempt := time.Now().Add(delay).UTC()
	_ = w.repos.OutboundWebhooks.MarkFailed(ctx, d.ID, httpStatus, errMsg, snippet, durationMs, nextAttempt)
	next := &types.OutboundWebhookDelivery{
		SubscriptionID:   d.SubscriptionID,
		LifecycleEventID: d.LifecycleEventID,
		EventID:          d.EventID,
		EventType:        d.EventType,
		Payload:          d.Payload,
		PayloadSHA256:    d.PayloadSHA256,
		AttemptNumber:    d.AttemptNumber + 1,
		Status:           types.OutboundDeliveryPending,
		NextRetryAt:      &nextAttempt,
	}
	_ = w.repos.OutboundWebhooks.CreateDelivery(ctx, next)
	_, _ = w.repos.OutboundWebhooks.RecordDeliveryFailure(ctx, d.SubscriptionID, types.OutboundWebhookAutoDisableThreshold)
}

// ---------------------------------------------------------------------------
// Redeliver (manual replay from the UI)
// ---------------------------------------------------------------------------

// Redeliver enqueues a fresh delivery row that clones the payload of an
// existing row but starts at attempt 1 again. Used by the admin
// redeliver endpoint.
func Redeliver(ctx context.Context, repos *db.Repositories, deliveryID uuid.UUID) (*types.OutboundWebhookDelivery, error) {
	orig, err := repos.OutboundWebhooks.GetDelivery(ctx, deliveryID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	clone := &types.OutboundWebhookDelivery{
		SubscriptionID:   orig.SubscriptionID,
		LifecycleEventID: orig.LifecycleEventID,
		EventID:          orig.EventID,
		EventType:        orig.EventType,
		Payload:          orig.Payload,
		PayloadSHA256:    orig.PayloadSHA256,
		AttemptNumber:    1,
		Status:           types.OutboundDeliveryPending,
		NextRetryAt:      &now,
	}
	if err := repos.OutboundWebhooks.CreateDelivery(ctx, clone); err != nil {
		return nil, err
	}
	return clone, nil
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func readSnippet(body io.Reader) string {
	if body == nil {
		return ""
	}
	buf := make([]byte, types.OutboundWebhookMaxResponseSnippetBytes)
	n, _ := io.ReadFull(body, buf)
	if n <= 0 {
		return ""
	}
	// Drain remainder so the connection can be reused
	_, _ = io.Copy(io.Discard, body)
	return string(buf[:n])
}

func safeCode(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "context canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return fmt.Sprintf("%v", err)
}
