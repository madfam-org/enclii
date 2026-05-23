package reconciler

import "time"

// Controller queue and worker defaults.
const (
	DefaultWorkQueueSize  = 100
	DefaultWorkerCount    = 5
	DefaultMaxRetries     = 10
	DefaultRetryBaseDelay = 30 * time.Second
	DefaultRetryMaxDelay  = 5 * time.Minute
)

// Kubernetes probe defaults for generated manifests.
const (
	DefaultProbeInitialDelaySeconds = 30
	DefaultProbeTimeoutSeconds      = 5
	DefaultProbePeriodSeconds       = 10
	DefaultProbeFailureThreshold    = 3
	DefaultProbeSuccessThreshold    = 1
)
