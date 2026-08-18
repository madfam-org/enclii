package realtime

import (
	"context"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
)

// pqDialer is the production Dialer. It opens a dedicated pq.Listener per addon
// DSN, issues LISTEN on Channel, and adapts pq's callback-based notification
// delivery into the Listener.Notify() channel the hub reads.
type pqDialer struct {
	logger logrus.FieldLogger

	// minReconnect/maxReconnect bound pq.Listener's internal backoff. pq
	// handles reconnection transparently; on reconnect it re-issues the LISTEN.
	minReconnect time.Duration
	maxReconnect time.Duration
}

// NewPQDialer builds the production Dialer. logger may be nil.
func NewPQDialer(logger logrus.FieldLogger) Dialer {
	return &pqDialer{
		logger:       logger,
		minReconnect: 2 * time.Second,
		maxReconnect: 30 * time.Second,
	}
}

// Dial opens a LISTEN connection to the addon database named by connInfo (a
// Postgres DSN / connection URI) and starts LISTENing on Channel.
func (d *pqDialer) Dial(ctx context.Context, addonID, connInfo string) (Listener, error) {
	if connInfo == "" {
		return nil, fmt.Errorf("realtime: empty connection info for addon %s", addonID)
	}

	logger := d.logger
	l := &pqListener{
		out:    make(chan string, SendBuffer),
		done:   make(chan struct{}),
		logger: logger,
		addon:  addonID,
	}

	// pq.NewListener takes an event callback; we log connection transitions but
	// let pq drive reconnection. A nil logger is tolerated.
	eventCB := func(ev pq.ListenerEventType, err error) {
		if logger == nil {
			return
		}
		switch ev {
		case pq.ListenerEventConnectionAttemptFailed:
			logger.WithField("addon_id", addonID).WithError(err).Warn("realtime: LISTEN connection attempt failed")
		case pq.ListenerEventDisconnected:
			logger.WithField("addon_id", addonID).WithError(err).Warn("realtime: LISTEN disconnected (pq will retry)")
		case pq.ListenerEventReconnected:
			logger.WithField("addon_id", addonID).Info("realtime: LISTEN reconnected")
		}
	}

	listener := pq.NewListener(connInfo, d.minReconnect, d.maxReconnect, eventCB)
	if err := listener.Listen(Channel); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("realtime: LISTEN %s failed for addon %s: %w", Channel, addonID, err)
	}
	l.pq = listener

	go l.forward()
	return l, nil
}

// pqListener adapts a *pq.Listener into the hub's Listener interface. pq
// delivers notifications on listener.Notify (a <-chan *pq.Notification); we
// forward the payload string onto our own bounded channel so the hub sees the
// same shape as the fake.
type pqListener struct {
	pq     *pq.Listener
	out    chan string
	done   chan struct{}
	logger logrus.FieldLogger
	addon  string
}

// forward pumps pq notifications onto out until Close. On a full out buffer it
// drops the notification (the hub's per-subscriber buffer is the primary
// backpressure point; this second drop only bites if the whole hub is wedged).
func (l *pqListener) forward() {
	defer close(l.out)
	for {
		select {
		case <-l.done:
			return
		case n, ok := <-l.pq.NotificationChannel():
			if !ok {
				return
			}
			if n == nil {
				// pq sends a nil on reconnect to signal "you may have missed
				// notifications". Nothing to forward; clients cold-start with a
				// SELECT per the protocol contract.
				continue
			}
			select {
			case l.out <- n.Extra:
			case <-l.done:
				return
			default:
				if l.logger != nil {
					l.logger.WithField("addon_id", l.addon).Warn("realtime: dropped notification (forward buffer full)")
				}
			}
		}
	}
}

// Notify implements Listener.
func (l *pqListener) Notify() <-chan string { return l.out }

// Close implements Listener. Idempotent.
func (l *pqListener) Close() error {
	select {
	case <-l.done:
		return nil
	default:
		close(l.done)
	}
	if l.pq != nil {
		return l.pq.Close()
	}
	return nil
}
