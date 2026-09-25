package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/ssubedir/dk/backend/internal/analytics"
	"github.com/ssubedir/dk/backend/internal/domain"
	"github.com/ssubedir/dk/backend/internal/oddsstore"
	"github.com/ssubedir/dk/backend/internal/stream"
)

type Game = domain.Game
type Price = domain.Price
type TeamOdds = domain.TeamOdds
type TotalOdds = domain.TotalOdds
type OddsSnapshot = domain.OddsSnapshot
type MoveTiming = domain.MoveTiming
type liveSelectionUpdate = domain.SelectionUpdate
type streamUpdate = stream.Update
type StreamMetrics = stream.StreamMetrics

var NewOddsCache = oddsstore.NewOddsCache
var withCORS = WithCORS
var snapshotObservations = analytics.SnapshotObservations
var nextRetryDelay = stream.NextRetryDelay

type providerFunc func(context.Context) ([]Game, error)

type analyticsRecorder interface {
	Record([]analytics.Observation)
	RecordFlush(string, uint64, time.Time, float64)
}

func (provider providerFunc) Fetch(ctx context.Context) ([]Game, error) { return provider(ctx) }

type testHub struct{ *stream.OddsHub }

func NewOddsHub(cache *oddsstore.OddsCache, interval time.Duration) *testHub {
	return &testHub{stream.NewOddsHub(cache, interval)}
}

func newHandler(cache *oddsstore.OddsCache, hub *testHub, stores ...*analytics.Store) http.Handler {
	return NewHandler(cache, hub.OddsHub, stores...)
}

func (hub *testHub) refresh(ctx context.Context) bool {
	snapshot, err := hub.RefreshSnapshot(ctx)
	return err == nil && !snapshot.Stale
}
func (hub *testHub) setRecorder(recorder interface {
	Record([]analytics.Observation)
	RecordFlush(string, uint64, time.Time, float64)
}) {
	hub.SetRecorder(recorder)
}
func (hub *testHub) publishSnapshot(snapshot OddsSnapshot)                   { hub.PublishSnapshot(snapshot) }
func (hub *testHub) recordFlush(elapsed time.Duration)                       { hub.RecordFlush(elapsed) }
func (hub *testHub) metrics() StreamMetrics                                  { return hub.Metrics() }
func (hub *testHub) publish(update streamUpdate) bool                        { return hub.Publish(update) }
func (hub *testHub) subscribe() (<-chan streamUpdate, *streamUpdate, func()) { return hub.Subscribe() }
func (hub *testHub) enableWebSocket()                                        { hub.EnableWebSocket() }
func (hub *testHub) setWebSocketConnected(connected bool)                    { hub.SetWebSocketConnected(connected) }
func (hub *testHub) webSocketStatus() (bool, bool)                           { return hub.WebSocketStatus() }

func floatPtr(value float64) *float64 { return &value }
