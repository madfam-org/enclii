package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

var (
	fileFallbackTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "enclii_audit_logs_file_fallback_total",
		Help: "Total number of audit logs written to file fallback",
	})
	droppedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "enclii_audit_logs_dropped_total",
		Help: "Total number of audit logs dropped (compliance violation)",
	})
)

func init() {
	prometheus.MustRegister(fileFallbackTotal)
	prometheus.MustRegister(droppedTotal)
}

// AsyncLogger handles asynchronous audit log writing to avoid blocking requests
type AsyncLogger struct {
	repos          *db.Repositories
	logChan        chan *types.AuditLog
	fallbackChan   chan *types.AuditLog // SECURITY FIX: Fallback channel for overflow
	batchSize      int
	flushTime      time.Duration
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
	errorCount     int
	droppedCount   int // SECURITY FIX: Track dropped logs
	fallbackCount  int // SECURITY FIX: Track logs sent to fallback
	mu             sync.Mutex
	lastDropWarned time.Time // SECURITY FIX: Rate-limit drop warnings
	fileFallback   *os.File  // SOC 2: File-based fallback when both channels overflow
	fileFallbackMu sync.Mutex
	fallbackPath   string
}

// NewAsyncLogger creates a new async audit logger
// SECURITY FIX: Increased buffer size and added fallback channel to prevent log loss
func NewAsyncLogger(repos *db.Repositories, bufferSize int, fallbackPath string) *AsyncLogger {
	ctx, cancel := context.WithCancel(context.Background())

	// SECURITY FIX: Ensure minimum buffer size to prevent log loss under load
	if bufferSize < 1000 {
		bufferSize = 1000
		logrus.Warn("Audit log buffer size increased to minimum of 1000 to prevent log loss")
	}

	logger := &AsyncLogger{
		repos:        repos,
		logChan:      make(chan *types.AuditLog, bufferSize),
		fallbackChan: make(chan *types.AuditLog, bufferSize/2), // SECURITY FIX: Fallback buffer
		batchSize:    10,                                       // Write in batches of 10
		flushTime:    5 * time.Second,                          // Flush every 5 seconds
		ctx:          ctx,
		cancel:       cancel,
		fallbackPath: fallbackPath,
	}

	// SOC 2: Open file-based fallback for persistent audit log storage
	if fallbackPath == "" {
		fallbackPath = "/var/log/enclii/audit-fallback.jsonl"
		logger.fallbackPath = fallbackPath
	}
	if err := os.MkdirAll(filepath.Dir(fallbackPath), 0750); err != nil {
		logrus.WithError(err).Warn("Failed to create audit fallback directory; file fallback disabled")
	} else {
		f, err := os.OpenFile(fallbackPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			logrus.WithError(err).Warn("Failed to open audit fallback file; file fallback disabled")
		} else {
			logger.fileFallback = f
			logger.fallbackPath = fallbackPath
			logrus.WithField("path", fallbackPath).Info("Audit log file fallback enabled")
		}
	}

	// Start background workers
	logger.wg.Add(3) // Main + fallback channel + recovery worker
	go logger.worker()
	go logger.fallbackWorker()
	go logger.recoveryWorker()

	return logger
}

// Log enqueues an audit log for async writing
// SECURITY FIX: Improved handling with fallback channel and warnings to prevent silent log loss
func (l *AsyncLogger) Log(log *types.AuditLog) {
	select {
	case l.logChan <- log:
		// Successfully enqueued to primary channel
		return

	default:
		// Primary buffer full - try fallback channel
		select {
		case l.fallbackChan <- log:
			// Successfully enqueued to fallback channel
			l.mu.Lock()
			l.fallbackCount++
			// SECURITY FIX: Warn when fallback is used (rate-limited to once per minute)
			if time.Since(l.lastDropWarned) > time.Minute {
				logrus.WithFields(logrus.Fields{
					"fallback_count": l.fallbackCount,
					"dropped_count":  l.droppedCount,
				}).Warn("COMPLIANCE WARNING: Audit log primary buffer full, using fallback channel")
				l.lastDropWarned = time.Now()
			}
			l.mu.Unlock()
			return

		default:
			// Both buffers full - attempt file-based fallback (SOC 2 compliance)
			if l.writeToFileFallback(log) {
				fileFallbackTotal.Inc()
				l.mu.Lock()
				l.fallbackCount++
				if time.Since(l.lastDropWarned) > time.Minute {
					logrus.WithFields(logrus.Fields{
						"fallback_count": l.fallbackCount,
						"action":         log.Action,
					}).Warn("COMPLIANCE WARNING: Audit log written to file fallback - both memory buffers full")
					l.lastDropWarned = time.Now()
				}
				l.mu.Unlock()
				return
			}

			// File fallback also failed - log is truly dropped (compliance violation)
			droppedTotal.Inc()
			l.mu.Lock()
			l.droppedCount++
			droppedCount := l.droppedCount

			// Rate-limited critical warnings (once per minute)
			if time.Since(l.lastDropWarned) > time.Minute {
				logrus.WithFields(logrus.Fields{
					"dropped_count":  droppedCount,
					"fallback_count": l.fallbackCount,
					"action":         log.Action,
					"actor_id":       log.ActorID,
					"resource_type":  log.ResourceType,
					"resource_id":    log.ResourceID,
				}).Error("CRITICAL COMPLIANCE VIOLATION: Audit log dropped - all fallbacks exhausted!")
				logrus.Errorf("CRITICAL: Audit log dropped! Total dropped: %d", droppedCount)
				l.lastDropWarned = time.Now()
			}
			l.mu.Unlock()
		}
	}
}

// worker is the background goroutine that processes audit logs from primary channel
func (l *AsyncLogger) worker() {
	defer l.wg.Done()

	batch := make([]*types.AuditLog, 0, l.batchSize)
	ticker := time.NewTicker(l.flushTime)
	defer ticker.Stop()

	for {
		select {
		case <-l.ctx.Done():
			// Shutdown signal received - flush remaining logs
			l.flushBatch(batch, "primary")
			return

		case log := <-l.logChan:
			// Add to batch
			batch = append(batch, log)

			// Flush if batch is full
			if len(batch) >= l.batchSize {
				l.flushBatch(batch, "primary")
				batch = make([]*types.AuditLog, 0, l.batchSize)
			}

		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				l.flushBatch(batch, "primary")
				batch = make([]*types.AuditLog, 0, l.batchSize)
			}
		}
	}
}

// fallbackWorker is the background goroutine that processes audit logs from fallback channel
// SECURITY FIX: Separate worker for fallback channel to prevent log loss
func (l *AsyncLogger) fallbackWorker() {
	defer l.wg.Done()

	batch := make([]*types.AuditLog, 0, l.batchSize)
	ticker := time.NewTicker(l.flushTime)
	defer ticker.Stop()

	for {
		select {
		case <-l.ctx.Done():
			// Shutdown signal received - flush remaining logs
			l.flushBatch(batch, "fallback")
			return

		case log := <-l.fallbackChan:
			// Add to batch
			batch = append(batch, log)

			// Flush if batch is full
			if len(batch) >= l.batchSize {
				l.flushBatch(batch, "fallback")
				batch = make([]*types.AuditLog, 0, l.batchSize)
			}

		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				l.flushBatch(batch, "fallback")
				batch = make([]*types.AuditLog, 0, l.batchSize)
			}
		}
	}
}

// writeToFileFallback serializes an audit log entry to the fallback JSONL file.
// Returns true if the write succeeded.
func (l *AsyncLogger) writeToFileFallback(log *types.AuditLog) bool {
	l.fileFallbackMu.Lock()
	defer l.fileFallbackMu.Unlock()

	if l.fileFallback == nil {
		return false
	}

	data, err := json.Marshal(log)
	if err != nil {
		logrus.WithError(err).Error("Failed to marshal audit log for file fallback")
		return false
	}

	data = append(data, '\n')
	if _, err := l.fileFallback.Write(data); err != nil {
		logrus.WithError(err).Error("Failed to write audit log to file fallback")
		return false
	}

	return true
}

// recoveryWorker periodically reads the fallback file, replays entries to the DB,
// and truncates the file on success. Runs every 30 seconds.
func (l *AsyncLogger) recoveryWorker() {
	defer l.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			l.replayFallbackFile()
		}
	}
}

// replayFallbackFile reads all entries from the fallback file, writes them to DB,
// and truncates the file if all entries were replayed successfully.
func (l *AsyncLogger) replayFallbackFile() {
	l.fileFallbackMu.Lock()
	defer l.fileFallbackMu.Unlock()

	if l.fileFallback == nil || l.fallbackPath == "" {
		return
	}

	// Close current handle so we can read the file
	l.fileFallback.Close()

	f, err := os.Open(l.fallbackPath)
	if err != nil {
		logrus.WithError(err).Warn("Recovery worker: failed to open fallback file for replay")
		l.reopenFallbackFile()
		return
	}

	var entries []*types.AuditLog
	scanner := bufio.NewScanner(f)
	// Allow up to 1MB per line for large audit entries
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var entry types.AuditLog
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			logrus.WithError(err).Warn("Recovery worker: skipping malformed fallback entry")
			continue
		}
		entries = append(entries, &entry)
	}
	f.Close()

	if len(entries) == 0 {
		l.reopenFallbackFile()
		return
	}

	logrus.WithField("count", len(entries)).Info("Recovery worker: replaying audit logs from file fallback")

	// Replay all entries to the database
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	allOK := true
	for _, entry := range entries {
		if err := l.repos.AuditLogs.Log(ctx, entry); err != nil {
			logrus.WithError(err).Warn("Recovery worker: failed to replay audit log entry, will retry next cycle")
			allOK = false
			break
		}
	}

	if allOK {
		// Truncate the file since all entries were replayed
		if err := os.Truncate(l.fallbackPath, 0); err != nil {
			logrus.WithError(err).Warn("Recovery worker: failed to truncate fallback file")
		} else {
			logrus.WithField("replayed", len(entries)).Info("Recovery worker: successfully replayed and cleared fallback file")
		}
	}

	l.reopenFallbackFile()
}

// reopenFallbackFile reopens the fallback file for append writing.
// Must be called with fileFallbackMu held.
func (l *AsyncLogger) reopenFallbackFile() {
	f, err := os.OpenFile(l.fallbackPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		logrus.WithError(err).Error("Recovery worker: failed to reopen fallback file")
		l.fileFallback = nil
		return
	}
	l.fileFallback = f
}

// flushBatch writes a batch of audit logs to the database
// SECURITY FIX: Added channel parameter for better logging and monitoring
func (l *AsyncLogger) flushBatch(batch []*types.AuditLog, channel string) {
	if len(batch) == 0 {
		return
	}

	// Create a context with timeout for database operations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Write logs individually (could be optimized with batch insert)
	for _, log := range batch {
		if err := l.repos.AuditLogs.Log(ctx, log); err != nil {
			// Log write failed - in production, should:
			// 1. Increment error metric
			// 2. Write to fallback storage (file, S3, etc.)
			// 3. Alert if error rate is high
			l.mu.Lock()
			l.errorCount++
			errorCount := l.errorCount
			l.mu.Unlock()

			// SECURITY FIX: Log database write failures for compliance monitoring
			logrus.WithFields(logrus.Fields{
				"channel":       channel,
				"error_count":   errorCount,
				"action":        log.Action,
				"actor_id":      log.ActorID,
				"resource_type": log.ResourceType,
				"resource_id":   log.ResourceID,
				"error":         err.Error(),
			}).Error("Failed to write audit log to database")

			// For now, just continue - we don't want to crash on audit log failure
			// TODO: Write to persistent fallback storage (file, S3, etc.)
			continue
		}
	}
}

// Close gracefully shuts down the async logger
// Blocks until all pending logs are written
// SECURITY FIX: Improved shutdown with fallback channel handling
func (l *AsyncLogger) Close() error {
	// Signal workers to stop
	l.cancel()

	// Wait for workers to finish flushing
	l.wg.Wait()

	// Close channels
	close(l.logChan)
	close(l.fallbackChan)

	// SOC 2: Flush and close file fallback
	l.fileFallbackMu.Lock()
	if l.fileFallback != nil {
		if err := l.fileFallback.Sync(); err != nil {
			logrus.WithError(err).Warn("Failed to sync audit fallback file during shutdown")
		}
		if err := l.fileFallback.Close(); err != nil {
			logrus.WithError(err).Warn("Failed to close audit fallback file during shutdown")
		}
		l.fileFallback = nil
	}
	l.fileFallbackMu.Unlock()

	// Check if there were any errors or dropped logs
	l.mu.Lock()
	errorCount := l.errorCount
	droppedCount := l.droppedCount
	fallbackCount := l.fallbackCount
	l.mu.Unlock()

	// SECURITY FIX: Log final statistics for compliance auditing
	logrus.WithFields(logrus.Fields{
		"error_count":    errorCount,
		"dropped_count":  droppedCount,
		"fallback_count": fallbackCount,
	}).Info("Audit logger shutdown complete")

	if droppedCount > 0 {
		return fmt.Errorf("async logger dropped %d audit logs (COMPLIANCE VIOLATION)", droppedCount)
	}

	if errorCount > 0 {
		return fmt.Errorf("async logger encountered %d database write errors", errorCount)
	}

	return nil
}

// Stats returns statistics about the async logger
// SECURITY FIX: Added fallback channel statistics for monitoring
func (l *AsyncLogger) Stats() map[string]interface{} {
	l.mu.Lock()
	defer l.mu.Unlock()

	return map[string]interface{}{
		"primary_buffer_size":     cap(l.logChan),
		"primary_buffer_pending":  len(l.logChan),
		"fallback_buffer_size":    cap(l.fallbackChan),
		"fallback_buffer_pending": len(l.fallbackChan),
		"error_count":             l.errorCount,
		"dropped_count":           l.droppedCount,
		"fallback_count":          l.fallbackCount,
		"batch_size":              l.batchSize,
		"flush_interval":          l.flushTime.String(),
	}
}
