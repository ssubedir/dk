package stream

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/ssubedir/dk/backend/internal/analytics"
	"github.com/ssubedir/dk/backend/internal/domain"
)

// Source is the snapshot boundary used for bootstrap and REST recovery.
// The hub never needs to know which sportsbook or cache implements it.
type Source interface {
	Get(context.Context) (domain.OddsSnapshot, error)
	Refresh(context.Context) (domain.OddsSnapshot, error)
	League() string
}

type Update struct {
	Event          string
	Data           []byte
	FetchCompleted time.Time
	Revision       uint64
	Moves          map[string]domain.MoveTiming
}

const manualRefreshMinGap = 5 * time.Second

type StreamMetrics struct {
	Samples               uint64  `json:"samples"`
	LastFetchToFlushMs    float64 `json:"lastFetchToFlushMs"`
	AverageFetchToFlushMs float64 `json:"averageFetchToFlushMs"`
	MaxFetchToFlushMs     float64 `json:"maxFetchToFlushMs"`
	WebSocketConnected    bool    `json:"websocketConnected"`
	WebSocketUpdates      uint64  `json:"websocketUpdates"`
	WebSocketIgnored      uint64  `json:"websocketIgnoredUpdates"`
	WebSocketResyncs      uint64  `json:"websocketResyncs"`
}

type Recorder interface {
	Record([]analytics.Observation)
	RecordFlush(quoteKey string, revision uint64, observedAt time.Time, durationMs float64)
}

// OddsHub fans out source changes. In WebSocket mode REST runs at bootstrap,
// for recovery, and for connected reconciliation; Run is the REST-only fallback.
type OddsHub struct {
	source             Source
	recorder           Recorder
	interval           time.Duration
	mu                 sync.Mutex
	latest             *Update
	subscribers        map[chan Update]struct{}
	flushCount         uint64
	flushLast          time.Duration
	flushMax           time.Duration
	flushTotal         time.Duration
	webSocketEnabled   bool
	webSocketConnected bool
	webSocketUpdates   uint64
	webSocketIgnored   uint64
	webSocketResyncs   uint64
	lastManualRefresh  time.Time
}

func (h *OddsHub) SetRecorder(recorder Recorder) {
	h.recorder = recorder
}

// RunBootstrap retries an initial REST snapshot until it succeeds, then stops.
func (h *OddsHub) RunBootstrap(ctx context.Context) bool {
	delay := h.interval
	attempt := 0
	for ctx.Err() == nil {
		attempt++
		started := time.Now()
		// A cold /api/odds request may already have started the first fetch.
		// Get joins it (or reads its result) instead of forcing a second REST call.
		snapshot, err := h.source.Get(ctx)
		h.publishFetchResult(snapshot, err)
		if err == nil && !snapshot.Stale {
			return true
		}
		reason := "snapshot is stale"
		if snapshot.LastError != "" {
			reason = snapshot.LastError
		}
		if err != nil {
			reason = err.Error()
		}
		log.Printf("DraftKings initial snapshot failed (league=%s, attempt=%d, elapsed=%s): %s; retrying in %s",
			h.source.League(), attempt, time.Since(started).Round(time.Millisecond), reason, delay)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(delay):
		}
		delay = NextRetryDelay(delay, h.interval)
	}
	return false
}

func NewOddsHub(source Source, interval time.Duration) *OddsHub {
	return &OddsHub{
		source:      source,
		interval:    interval,
		subscribers: make(map[chan Update]struct{}),
	}
}

func (h *OddsHub) Run(ctx context.Context) {
	delay := h.interval
	if !h.refresh(ctx) {
		delay = NextRetryDelay(delay, h.interval)
		log.Printf("DraftKings refresh failed; retrying in %s", delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			started := time.Now()
			if h.refresh(ctx) {
				delay = h.interval
				// Keep successful checks close to the configured start-to-start cadence.
				timer.Reset(max(500*time.Millisecond, delay-time.Since(started)))
			} else {
				delay = NextRetryDelay(delay, h.interval)
				// After an upstream failure, wait the full backoff from completion.
				log.Printf("DraftKings refresh failed; retrying in %s", delay)
				timer.Reset(delay)
			}
		}
	}
}

func NextRetryDelay(previous, base time.Duration) time.Duration {
	limit := max(30*time.Second, base)
	current := max(previous, base)
	if current >= limit/2 {
		return limit
	}
	return current * 2
}

func (h *OddsHub) refresh(ctx context.Context) bool {
	snapshot, err := h.RefreshSnapshot(ctx)
	return err == nil && !snapshot.Stale
}

func (h *OddsHub) RefreshSnapshot(ctx context.Context) (domain.OddsSnapshot, error) {
	snapshot, err := h.source.Refresh(ctx)
	h.publishFetchResult(snapshot, err)
	return snapshot, err
}

func (h *OddsHub) publishFetchResult(snapshot domain.OddsSnapshot, err error) {
	var update Update
	if err != nil {
		update.Event = "unavailable"
		update.Data, _ = json.Marshal(map[string]string{
			"error":   "DraftKings odds are temporarily unavailable",
			"details": err.Error(),
		})
	} else {
		update = snapshotUpdate(snapshot)
	}
	if h.Publish(update) && err == nil && h.recorder != nil {
		h.recorder.Record(analytics.SnapshotObservations(snapshot))
	}
}

func snapshotUpdate(snapshot domain.OddsSnapshot) Update {
	data, _ := json.Marshal(snapshot)
	update := Update{Event: "odds", Data: data, Revision: snapshot.Revision}
	if !snapshot.Stale {
		update.FetchCompleted = snapshot.FetchCompleted
		update.Moves = snapshot.Moves
	}
	return update
}

func (h *OddsHub) PublishSnapshot(snapshot domain.OddsSnapshot) {
	if h.Publish(snapshotUpdate(snapshot)) && h.recorder != nil {
		h.recorder.Record(analytics.SnapshotObservations(snapshot))
	}
}

// Publish fans one update out to current subscribers, dropping an older
// revision if a concurrent refresh completes after a newer live change.
func (h *OddsHub) Publish(update Update) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.latest != nil && update.Revision < h.latest.Revision {
		return false
	}
	h.latest = &update
	for subscriber := range h.subscribers {
		select {
		case subscriber <- update:
		default:
			<-subscriber
			subscriber <- update
		}
	}
	return true
}

func (h *OddsHub) RecordFlush(elapsed time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.flushCount++
	h.flushLast = elapsed
	h.flushTotal += elapsed
	h.flushMax = max(h.flushMax, elapsed)
}

func (h *OddsHub) EnableWebSocket() {
	h.mu.Lock()
	h.webSocketEnabled = true
	h.mu.Unlock()
}

func (h *OddsHub) SetWebSocketConnected(connected bool) {
	h.mu.Lock()
	if h.webSocketConnected == connected {
		h.mu.Unlock()
		return
	}
	h.webSocketConnected = connected
	data, _ := json.Marshal(map[string]bool{"websocketConnected": connected})
	update := Update{Event: "status", Data: data}
	for subscriber := range h.subscribers {
		select {
		case subscriber <- update:
		default:
			<-subscriber
			subscriber <- update
		}
	}
	h.mu.Unlock()
}

func (h *OddsHub) WebSocketStatus() (bool, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.webSocketEnabled, h.webSocketConnected
}

func (h *OddsHub) RecordWebSocketUpdate() {
	h.mu.Lock()
	h.webSocketUpdates++
	h.mu.Unlock()
}

func (h *OddsHub) RecordWebSocketIgnoredUpdate() {
	h.mu.Lock()
	h.webSocketIgnored++
	h.mu.Unlock()
}

func (h *OddsHub) RecordWebSocketResync() {
	h.mu.Lock()
	h.webSocketResyncs++
	h.mu.Unlock()
}

// Reserve a public manual refresh before starting HTTP. This bounds upstream
// requests even when many browsers press Refresh in quick succession.
func (h *OddsHub) ReserveManualRefresh() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if wait := manualRefreshMinGap - now.Sub(h.lastManualRefresh); !h.lastManualRefresh.IsZero() && wait > 0 {
		return wait
	}
	h.lastManualRefresh = now
	return 0
}

func (h *OddsHub) Metrics() StreamMetrics {
	h.mu.Lock()
	defer h.mu.Unlock()
	metrics := StreamMetrics{Samples: h.flushCount, WebSocketConnected: h.webSocketConnected, WebSocketUpdates: h.webSocketUpdates, WebSocketIgnored: h.webSocketIgnored, WebSocketResyncs: h.webSocketResyncs}
	if h.flushCount > 0 {
		metrics.LastFetchToFlushMs = h.flushLast.Seconds() * 1000
		metrics.AverageFetchToFlushMs = h.flushTotal.Seconds() * 1000 / float64(h.flushCount)
		metrics.MaxFetchToFlushMs = h.flushMax.Seconds() * 1000
	}
	return metrics
}

func (h *OddsHub) Subscribe() (<-chan Update, *Update, func()) {
	h.mu.Lock()
	updates := make(chan Update, 1)
	h.subscribers[updates] = struct{}{}
	latest := h.latest
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		delete(h.subscribers, updates)
		h.mu.Unlock()
	}
	return updates, latest, unsubscribe
}
