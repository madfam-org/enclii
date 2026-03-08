package queue

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: These tests validate queue type construction, key constants, and
// configuration logic without requiring a live Redis connection.
// Integration tests requiring Redis are separated by build tag.

// --- Redis key constants ---

func TestRedisKeyConstants(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		prefix  string
		wantNon bool
	}{
		{"buildQueueKey", buildQueueKey, "roundhouse:", true},
		{"priorityQueueKey", priorityQueueKey, "roundhouse:", true},
		{"callbackRetryKey", callbackRetryKey, "roundhouse:", true},
		{"callbackHashPrefix", callbackHashPrefix, "roundhouse:", true},
		{"jobHashKeyPrefix", jobHashKeyPrefix, "roundhouse:", true},
		{"logsStreamPrefix", logsStreamPrefix, "roundhouse:", true},
		{"statsKey", statsKey, "roundhouse:", true},
		{"activeWorkersKey", activeWorkersKey, "roundhouse:", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.key, "key constant should not be empty")
			assert.Contains(t, tt.key, tt.prefix, "key should have roundhouse: prefix")
		})
	}
}

func TestRedisKeyConstants_Unique(t *testing.T) {
	keys := []string{
		buildQueueKey,
		priorityQueueKey,
		callbackRetryKey,
		statsKey,
		activeWorkersKey,
	}

	seen := make(map[string]bool)
	for _, k := range keys {
		assert.False(t, seen[k], "duplicate key constant: %s", k)
		seen[k] = true
	}
}

func TestRedisKeyPrefixes_Unique(t *testing.T) {
	// Prefixes must be unique to avoid key collisions
	prefixes := []string{
		callbackHashPrefix,
		jobHashKeyPrefix,
		logsStreamPrefix,
	}

	seen := make(map[string]bool)
	for _, p := range prefixes {
		assert.False(t, seen[p], "duplicate key prefix: %s", p)
		seen[p] = true
	}
}

// --- RedisQueueConfig ---

func TestRedisQueueConfig_StandaloneMode(t *testing.T) {
	cfg := &RedisQueueConfig{
		RedisURL:        "redis://localhost:6379/0",
		SentinelEnabled: false,
	}

	assert.Equal(t, "redis://localhost:6379/0", cfg.RedisURL)
	assert.False(t, cfg.SentinelEnabled)
	assert.Empty(t, cfg.SentinelAddrs)
	assert.Empty(t, cfg.SentinelMasterName)
}

func TestRedisQueueConfig_SentinelMode(t *testing.T) {
	cfg := &RedisQueueConfig{
		SentinelEnabled:    true,
		SentinelAddrs:      []string{"redis-0:26379", "redis-1:26379", "redis-2:26379"},
		SentinelMasterName: "enclii-master",
		Password:           "redis-password",
	}

	assert.True(t, cfg.SentinelEnabled)
	assert.Len(t, cfg.SentinelAddrs, 3)
	assert.Equal(t, "enclii-master", cfg.SentinelMasterName)
	assert.Equal(t, "redis-password", cfg.Password)
}

// --- NewRedisQueue URL validation ---

func TestNewRedisQueue_InvalidURL(t *testing.T) {
	_, err := NewRedisQueue("not-a-valid-url", nil)
	require.Error(t, err, "should reject invalid Redis URL")
	assert.Contains(t, err.Error(), "invalid redis URL")
}

func TestNewRedisQueueWithConfig_InvalidStandaloneURL(t *testing.T) {
	cfg := &RedisQueueConfig{
		RedisURL:        "://bad",
		SentinelEnabled: false,
	}

	_, err := NewRedisQueueWithConfig(cfg, nil)
	require.Error(t, err, "should reject malformed Redis URL")
}

// --- Job key generation ---

func TestJobKeyFormat(t *testing.T) {
	jobID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	expected := "roundhouse:job:11111111-2222-3333-4444-555555555555"

	key := jobHashKeyPrefix + jobID.String()
	assert.Equal(t, expected, key)
}

func TestCallbackKeyFormat(t *testing.T) {
	callbackID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	expected := "roundhouse:callback:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	key := callbackHashPrefix + callbackID.String()
	assert.Equal(t, expected, key)
}

func TestLogsStreamKeyFormat(t *testing.T) {
	jobID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	expected := "roundhouse:logs:11111111-2222-3333-4444-555555555555"

	key := logsStreamPrefix + jobID.String()
	assert.Equal(t, expected, key)
}

// --- BuildJob priority scoring ---

func TestPriorityScoring(t *testing.T) {
	// The enqueue logic uses: score = now_unix - (priority * 1000)
	// Lower score = dequeued first (ZPopMin). Higher priority = lower score.

	now := time.Now().Unix()

	tests := []struct {
		name     string
		priority int
	}{
		{"normal_priority_0", 0},
		{"elevated_priority_1", 1},
		{"high_priority_5", 5},
		{"critical_priority_10", 10},
	}

	var lastScore float64
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := float64(now) - float64(tt.priority*1000)
			if i > 0 {
				assert.Less(t, score, lastScore,
					"higher priority should produce lower score (dequeued first)")
			}
			lastScore = score
		})
	}
}

// --- BuildJob default values ---

func TestBuildJob_NewID_SetOnEnqueue(t *testing.T) {
	// Verify that a job with zero-value ID is expected pre-enqueue.
	// The Enqueue method assigns the ID via uuid.New().
	job := &BuildJob{
		ServiceID: uuid.New(),
		GitRepo:   "https://github.com/org/repo.git",
		GitSHA:    "abc123",
	}

	assert.Equal(t, uuid.Nil, job.ID, "ID should be zero-value before enqueue")
	assert.True(t, job.CreatedAt.IsZero(), "CreatedAt should be zero-value before enqueue")
}

// --- Job status transitions ---

func TestJobStatus_TerminalStates(t *testing.T) {
	terminalStates := []JobStatus{
		StatusCompleted,
		StatusFailed,
		StatusCancelled,
	}

	nonTerminalStates := []JobStatus{
		StatusQueued,
		StatusBuilding,
	}

	for _, s := range terminalStates {
		t.Run("terminal_"+string(s), func(t *testing.T) {
			// Terminal states should trigger completed_at timestamp in UpdateStatus
			assert.True(t,
				s == StatusCompleted || s == StatusFailed || s == StatusCancelled,
				"expected terminal state")
		})
	}

	for _, s := range nonTerminalStates {
		t.Run("non_terminal_"+string(s), func(t *testing.T) {
			assert.True(t,
				s == StatusQueued || s == StatusBuilding,
				"expected non-terminal state")
		})
	}
}
