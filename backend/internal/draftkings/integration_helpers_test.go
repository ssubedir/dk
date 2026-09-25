package draftkings

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ssubedir/dk/backend/internal/domain"
	"github.com/ssubedir/dk/backend/internal/httpapi"
	"github.com/ssubedir/dk/backend/internal/oddsstore"
	"github.com/ssubedir/dk/backend/internal/stream"
)

type testHub struct {
	*stream.OddsHub
	cache *oddsstore.OddsCache
}

type OddsSnapshot = domain.OddsSnapshot

func NewOddsHub(cache *oddsstore.OddsCache, interval time.Duration) *testHub {
	return &testHub{OddsHub: stream.NewOddsHub(cache, interval), cache: cache}
}

func (hub *testHub) draftKingsSocketSession(ctx context.Context, config DraftKingsConfig, endpoint string, requestResync func(), reconnect bool) (bool, error) {
	return (&SocketClient{Config: config, Sink: &testSocketSink{hub: hub, requestResync: requestResync}}).session(ctx, endpoint, reconnect)
}

func (hub *testHub) metrics() stream.StreamMetrics { return hub.Metrics() }
func (hub *testHub) subscribe() (<-chan stream.Update, *stream.Update, func()) {
	return hub.Subscribe()
}
func (hub *testHub) publishSnapshot(snapshot domain.OddsSnapshot) { hub.PublishSnapshot(snapshot) }

type testSocketSink struct {
	hub           *testHub
	requestResync func()
}

func (sink *testSocketSink) SetConnected(connected bool) { sink.hub.SetWebSocketConnected(connected) }
func (sink *testSocketSink) RequestResync()              { sink.requestResync() }
func (sink *testSocketSink) OnSelectionRemoved(id string) {
	if sink.hub.cache != nil && sink.hub.cache.HasSelection(id) {
		sink.requestResync()
	}
}
func (sink *testSocketSink) OnSelection(update domain.SelectionUpdate, observedAt time.Time) {
	snapshot, changed, err := sink.hub.cache.ApplyLiveSelection(update, observedAt)
	if err != nil {
		if errors.Is(err, domain.ErrUnknownSelection) && sink.hub.cache.IsKnownAlternativeUpdate(update) {
			sink.hub.RecordWebSocketIgnoredUpdate()
			return
		}
		sink.requestResync()
		return
	}
	if changed {
		sink.hub.RecordWebSocketUpdate()
		sink.hub.PublishSnapshot(snapshot)
	}
}

var newHandler = httpapi.NewHandler
var ErrUnknownSelection = domain.ErrUnknownSelection

func readSSEEvent(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	var event, data string
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
		if line == "" && event != "" {
			return event, data
		}
		if err == io.EOF {
			t.Fatalf("SSE stream ended before an event: %q", event)
		}
	}
}
