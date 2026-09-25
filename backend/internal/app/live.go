package app

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/ssubedir/dk/backend/internal/domain"
	"github.com/ssubedir/dk/backend/internal/draftkings"
	"github.com/ssubedir/dk/backend/internal/oddsstore"
	"github.com/ssubedir/dk/backend/internal/stream"
)

const connectedReconcileInterval = time.Minute

// liveFeed is the application policy around a source subscription: it joins
// deltas to the current REST snapshot and serializes all recovery fetches.
type liveFeed struct {
	config   draftkings.DraftKingsConfig
	interval time.Duration
	cache    *oddsstore.OddsCache
	hub      *stream.OddsHub
	resync   chan struct{}
}

var _ draftkings.SocketSink = (*liveFeed)(nil)

func newLiveFeed(config draftkings.DraftKingsConfig, interval time.Duration, cache *oddsstore.OddsCache, hub *stream.OddsHub) *liveFeed {
	return &liveFeed{config: config, interval: interval, cache: cache, hub: hub, resync: make(chan struct{}, 1)}
}

func (feed *liveFeed) Run(ctx context.Context) {
	go feed.runRecovery(ctx)
	go feed.reconcileWhileConnected(ctx)
	(&draftkings.SocketClient{Config: feed.config, Sink: feed}).Run(ctx)
}

func (feed *liveFeed) RequestResync() {
	select {
	case feed.resync <- struct{}{}:
	default:
	}
}

func (feed *liveFeed) SetConnected(connected bool) {
	feed.hub.SetWebSocketConnected(connected)
}

func (feed *liveFeed) OnSelection(update domain.SelectionUpdate, observedAt time.Time) {
	snapshot, changed, err := feed.cache.ApplyLiveSelection(update, observedAt)
	if err != nil {
		if errors.Is(err, domain.ErrUnknownSelection) && feed.cache.IsKnownAlternativeUpdate(update) {
			feed.hub.RecordWebSocketIgnoredUpdate()
			return
		}
		log.Printf("DraftKings WebSocket selection %s requires REST resync: %v", update.ID, err)
		feed.RequestResync()
		return
	}
	if changed {
		feed.hub.RecordWebSocketUpdate()
		feed.hub.PublishSnapshot(snapshot)
	}
}

func (feed *liveFeed) OnSelectionRemoved(id string) {
	if feed.cache.HasSelection(id) {
		feed.RequestResync()
	}
}

func (feed *liveFeed) reconcileWhileConnected(ctx context.Context) {
	ticker := time.NewTicker(connectedReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			feed.reconcileIfConnected()
		}
	}
}

func (feed *liveFeed) reconcileIfConnected() {
	_, connected := feed.hub.WebSocketStatus()
	if connected {
		feed.RequestResync()
	}
}

func (feed *liveFeed) runRecovery(ctx context.Context) {
	lastResync := time.Time{}
	minimumGap := max(time.Second, feed.interval)
	retryDelay := minimumGap
	for {
		select {
		case <-ctx.Done():
			return
		case <-feed.resync:
			if wait := retryDelay - time.Since(lastResync); !lastResync.IsZero() && wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
			feed.hub.RecordWebSocketResync()
			snapshot, err := feed.hub.RefreshSnapshot(ctx)
			lastResync = time.Now()
			if err != nil || snapshot.Stale || snapshot.UpdateSource == "websocket" {
				// Retry failures and REST responses overtaken by a live change.
				if err != nil || snapshot.Stale {
					retryDelay = stream.NextRetryDelay(retryDelay, minimumGap)
					reason := snapshot.LastError
					if err != nil {
						reason = err.Error()
					}
					log.Printf("DraftKings REST reconciliation failed (league=%s): %s; retrying in %s", feed.config.LeagueName, draftkings.SafeSocketReason(reason), retryDelay)
				} else {
					retryDelay = minimumGap
				}
				feed.RequestResync()
			} else {
				retryDelay = minimumGap
			}
		}
	}
}
