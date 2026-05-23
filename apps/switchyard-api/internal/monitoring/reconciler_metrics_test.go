package monitoring

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordReconcilerQueuePressure(t *testing.T) {
	RecordReconcilerQueuePressure(3, 100, 2, 5)
	assert.Equal(t, 3.0, testutil.ToFloat64(reconcilerWorkQueueSize))
	assert.Equal(t, 100.0, testutil.ToFloat64(reconcilerWorkQueueCapacity))
	assert.Equal(t, 2.0, testutil.ToFloat64(reconcilerRetryQueueSize))
	assert.Equal(t, 5.0, testutil.ToFloat64(reconcilerDroppedWorkTotal))
}

func TestRecordReconcilerWorkScheduled(t *testing.T) {
	before := testutil.ToFloat64(reconcilerWorkScheduledTotal)
	RecordReconcilerWorkScheduled()
	after := testutil.ToFloat64(reconcilerWorkScheduledTotal)
	assert.Equal(t, before+1, after)
}
