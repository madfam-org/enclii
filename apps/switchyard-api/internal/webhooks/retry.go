package webhooks

import (
	"time"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// retryBackoff is the fixed exponential schedule:
//
//	attempt 1 → 30s
//	attempt 2 → 2m
//	attempt 3 → 10m
//	attempt 4 → 30m
//	attempt 5 → 2h
//
// After the 5th failure the delivery transitions to DLQ and no further
// retries are scheduled.
var retryBackoff = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

// NextRetryDelay returns the delay to apply before attempt N+1 given
// that attempt N just failed. If attemptJustDone has reached the
// ceiling, ok=false means the caller should DLQ the delivery instead.
func NextRetryDelay(attemptJustDone int) (delay time.Duration, ok bool) {
	if attemptJustDone >= types.OutboundWebhookMaxAttempts {
		return 0, false
	}
	if attemptJustDone < 1 {
		attemptJustDone = 1
	}
	// attempt 1 just done → index 0 in backoff is the wait before attempt 2
	idx := attemptJustDone - 1
	if idx >= len(retryBackoff) {
		idx = len(retryBackoff) - 1
	}
	return retryBackoff[idx], true
}

// ShouldRetry classifies an HTTP response + error pair into retriable
// (true) or terminal (false). Standard policy per the spec:
//   - 2xx   → not retried (success)
//   - 4xx   → terminal except 408/429
//   - 5xx   → retry
//   - timeout / connection error → retry
func ShouldRetry(httpStatus int, transportErr error) bool {
	if transportErr != nil {
		// Network errors / timeouts / TLS issues — always retry.
		return true
	}
	if httpStatus >= 200 && httpStatus < 300 {
		return false
	}
	if httpStatus == 408 || httpStatus == 429 {
		return true
	}
	if httpStatus >= 500 && httpStatus < 600 {
		return true
	}
	// 3xx shouldn't happen (we disable redirects in the client); treat
	// as terminal so customers fix their endpoint.
	return false
}
