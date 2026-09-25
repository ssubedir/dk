package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssubedir/dk/backend/internal/analytics"
)

func TestBootstrapLogsInitialFailureCause(t *testing.T) {
	var output bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previousOutput)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		cancel()
		return nil, errors.New("DraftKings returned 403 Forbidden: access denied")
	}), "NFL")
	if NewOddsHub(cache, 5*time.Second).RunBootstrap(ctx) {
		t.Fatal("bootstrap unexpectedly succeeded")
	}
	for _, want := range []string{
		"DraftKings initial snapshot failed",
		"league=NFL",
		"attempt=1",
		"elapsed=",
		"403 Forbidden: access denied",
		"retrying in 5s",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("bootstrap log missing %q: %s", want, output.String())
		}
	}
}

func TestStreamPushesLatestSnapshotWithoutFetchingPerClient(t *testing.T) {
	var fetches atomic.Int32
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		fetches.Add(1)
		return []Game{{ID: "event-1"}}, nil
	}), "NFL")
	hub := NewOddsHub(cache, 5*time.Second)
	hub.refresh(context.Background())

	server := httptest.NewServer(withCORS(newHandler(cache, hub), "https://linewatch.example.com"))
	defer server.Close()

	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/stream", nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		request.Header.Set("Origin", "https://linewatch.example.com")
		response, err := server.Client().Do(request)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("expected SSE response, got status=%d type=%q", response.StatusCode, response.Header.Get("Content-Type"))
		}
		if response.Header.Get("Access-Control-Allow-Origin") != "https://linewatch.example.com" {
			t.Fatalf("missing stream CORS header: %q", response.Header.Get("Access-Control-Allow-Origin"))
		}

		eventName, data := readSSEEvent(t, bufio.NewReader(response.Body))
		if eventName != "odds" {
			t.Fatalf("expected odds event, got %q", eventName)
		}
		var snapshot OddsSnapshot
		if err := json.Unmarshal([]byte(data), &snapshot); err != nil || len(snapshot.Games) != 1 || snapshot.Games[0].ID != "event-1" {
			t.Fatalf("unexpected stream snapshot: value=%#v err=%v", snapshot, err)
		}
		response.Body.Close()
		cancel()
	}

	if got := fetches.Load(); got != 1 {
		t.Fatalf("subscribers triggered upstream fetches: got %d, want 1", got)
	}
}

func TestStreamPushesChangesToAnOpenConnection(t *testing.T) {
	var fetches atomic.Int32
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		id := fetches.Add(1)
		return []Game{{ID: fmt.Sprintf("event-%d", id)}}, nil
	}), "NFL")
	hub := NewOddsHub(cache, time.Second)
	server := httptest.NewServer(newHandler(cache, hub))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)

	for want := 1; want <= 2; want++ {
		hub.refresh(context.Background())
		eventName, data := readSSEEvent(t, reader)
		var snapshot OddsSnapshot
		if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
			t.Fatal(err)
		}
		if eventName != "odds" || len(snapshot.Games) != 1 || snapshot.Games[0].ID != fmt.Sprintf("event-%d", want) {
			t.Fatalf("push %d: event=%q snapshot=%#v", want, eventName, snapshot)
		}
	}
}

func TestLiveLineReplacementReachesSSE(t *testing.T) {
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		line := -1.5
		return []Game{{ID: "nfl-live", Away: TeamOdds{Name: "HOU Texans", Spread: &Price{Line: &line, American: "-110", SelectionID: "old-line", SelectionLabel: "HOU Texans"}}}}, nil
	}), "NFL")
	hub := NewOddsHub(cache, time.Second)
	store := analytics.NewStore(10)
	hub.setRecorder(store)
	hub.refresh(context.Background())
	server := httptest.NewServer(newHandler(cache, hub, store))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readSSEEvent(t, reader); event != "odds" {
		t.Fatalf("initial event = %q", event)
	}
	var initialSummary analytics.Summary
	if event, data := readSSEEvent(t, reader); event != "analysis-summary" || json.Unmarshal([]byte(data), &initialSummary) != nil || initialSummary.Observations != 1 {
		t.Fatalf("initial analytics summary missing: event=%q summary=%#v", event, initialSummary)
	}
	observedAt := time.Now().Add(-25 * time.Millisecond)
	snapshot, changed, err := cache.ApplyLiveSelection(liveSelectionUpdate{ID: "new-line", ReplacesID: "old-line", Label: "HOU Texans", Price: Price{Line: floatPtr(-2.5), American: "-105"}}, observedAt)
	if err != nil || !changed {
		t.Fatalf("line replacement failed: changed=%t err=%v", changed, err)
	}
	hub.publishSnapshot(snapshot)
	event, data := readSSEEvent(t, reader)
	var pushed OddsSnapshot
	if err := json.Unmarshal([]byte(data), &pushed); err != nil || event != "odds" || pushed.UpdateSource != "websocket" || pushed.Games[0].Away.Spread.Line == nil || *pushed.Games[0].Away.Spread.Line != -2.5 {
		t.Fatalf("replacement not pushed: event=%q snapshot=%#v err=%v", event, pushed, err)
	}
	event, data = readSSEEvent(t, reader)
	var timing flushTimingEvent
	if err := json.Unmarshal([]byte(data), &timing); err != nil || event != "flush-timing" || timing.Replay ||
		len(timing.Timings) != 1 || timing.Timings[0].QuoteKey != "nfl-live:away:spread" ||
		pushed.Move == nil || timing.Timings[0].Revision != pushed.Move.Revision || timing.Timings[0].GoToSSEFlushMs == nil ||
		*timing.Timings[0].GoToSSEFlushMs < 25 || !timing.Timings[0].GoObservedAt.Equal(observedAt) {
		t.Fatalf("missing server-measured live flush timing: event=%q timing=%#v err=%v", event, timing, err)
	}
	event, data = readSSEEvent(t, reader)
	var pushedSummary analytics.Summary
	if err := json.Unmarshal([]byte(data), &pushedSummary); err != nil || event != "analysis-summary" ||
		pushedSummary.PriceChanges != 1 || pushedSummary.SSEFlushSamples != 1 {
		t.Fatalf("live analytics summary missing: event=%q summary=%#v err=%v", event, pushedSummary, err)
	}
	summary, err := store.Summary(context.Background(), 24)
	if err != nil || summary.SSEFlushSamples != 1 || summary.P50GoToSSEFlush == nil ||
		*summary.P50GoToSSEFlush < 25 || summary.PriceChanges != 1 {
		t.Fatalf("live flush was not summarized: summary=%#v err=%v", summary, err)
	}

	replayResponse, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer replayResponse.Body.Close()
	replayReader := bufio.NewReader(replayResponse.Body)
	if event, _ := readSSEEvent(t, replayReader); event != "odds" {
		t.Fatalf("replay event = %q", event)
	}
	event, data = readSSEEvent(t, replayReader)
	timing = flushTimingEvent{}
	if err := json.Unmarshal([]byte(data), &timing); err != nil || event != "flush-timing" || !timing.Replay ||
		len(timing.Timings) != 1 || timing.Timings[0].GoToSSEFlushMs != nil {
		t.Fatalf("replay incorrectly measured as a live flush: event=%q timing=%#v err=%v", event, timing, err)
	}
	summary, err = store.Summary(context.Background(), 24)
	if err != nil || summary.SSEFlushSamples != 1 {
		t.Fatalf("replay added another timing sample: summary=%#v err=%v", summary, err)
	}
}

func TestStreamMetricsCountLiveFlushButNotInitialReplay(t *testing.T) {
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		return []Game{{ID: "event-1"}}, nil
	}), "NFL")
	hub := NewOddsHub(cache, time.Second)
	hub.refresh(context.Background())

	server := httptest.NewServer(newHandler(cache, hub))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readSSEEvent(t, reader); event != "odds" {
		t.Fatalf("initial replay event = %q, want odds", event)
	}
	if metrics := readStreamMetrics(t, server); metrics.Samples != 0 {
		t.Fatalf("initial replay should not count as a live flush: %#v", metrics)
	}

	hub.refresh(context.Background())
	if event, data := readSSEEvent(t, reader); event != "odds" || strings.Contains(data, "fetchCompleted") {
		t.Fatalf("live event = %q, hidden timestamp exposed = %t", event, strings.Contains(data, "fetchCompleted"))
	}
	deadline := time.Now().Add(time.Second)
	for {
		metrics := readStreamMetrics(t, server)
		if metrics.Samples == 1 {
			if metrics.LastFetchToFlushMs < 0 || metrics.MaxFetchToFlushMs < metrics.LastFetchToFlushMs {
				t.Fatalf("invalid live flush metrics: %#v", metrics)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected one live SSE flush, got %#v", metrics)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStreamMetricsAverageAndMax(t *testing.T) {
	hub := NewOddsHub(nil, time.Second)
	hub.recordFlush(500 * time.Microsecond)
	hub.recordFlush(1500 * time.Microsecond)
	metrics := hub.metrics()
	if metrics.Samples != 2 || metrics.LastFetchToFlushMs != 1.5 || metrics.AverageFetchToFlushMs != 1 || metrics.MaxFetchToFlushMs != 1.5 {
		t.Fatalf("unexpected flush metrics: %#v", metrics)
	}
}

func TestHubDoesNotPublishOlderConcurrentSnapshot(t *testing.T) {
	hub := NewOddsHub(nil, time.Second)
	hub.publish(streamUpdate{Event: "odds", Data: []byte(`{"price":"new"}`), Revision: 2})
	hub.publish(streamUpdate{Event: "odds", Data: []byte(`{"price":"old"}`), Revision: 1})
	_, latest, unsubscribe := hub.subscribe()
	defer unsubscribe()
	if latest == nil || string(latest.Data) != `{"price":"new"}` {
		t.Fatalf("older snapshot replaced a newer update: %#v", latest)
	}
}

func readStreamMetrics(t *testing.T, server *httptest.Server) StreamMetrics {
	t.Helper()
	response, err := server.Client().Get(server.URL + "/api/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("metrics status=%d cache-control=%q", response.StatusCode, response.Header.Get("Cache-Control"))
	}
	var metrics StreamMetrics
	if err := json.NewDecoder(response.Body).Decode(&metrics); err != nil {
		t.Fatal(err)
	}
	return metrics
}

func TestScheduledRefreshFetchesInsideCacheTTL(t *testing.T) {
	var fetches atomic.Int32
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		id := fetches.Add(1)
		return []Game{{ID: fmt.Sprintf("event-%d", id)}}, nil
	}), "NFL")
	hub := NewOddsHub(cache, 5*time.Second)

	hub.refresh(context.Background())
	hub.refresh(context.Background())

	if got := fetches.Load(); got != 2 {
		t.Fatalf("scheduled refreshes fetched %d times, want 2", got)
	}
}

func TestWebSocketBootstrapFetchesOnceAndCachedReadsDoNotPoll(t *testing.T) {
	var fetches atomic.Int32
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		fetches.Add(1)
		return []Game{{ID: "event-1"}}, nil
	}), "NFL")
	hub := NewOddsHub(cache, time.Second)
	if !hub.RunBootstrap(context.Background()) {
		t.Fatal("bootstrap failed")
	}
	for range 3 {
		if _, err := cache.Get(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("bootstrap and cached reads made %d REST requests, want 1", got)
	}
}

func TestBootstrapJoinsColdAPIRequest(t *testing.T) {
	var fetches atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		fetches.Add(1)
		close(started)
		<-release
		return []Game{{ID: "event-1"}}, nil
	}), "NFL")
	hub := NewOddsHub(cache, time.Second)
	apiDone := make(chan struct{})
	go func() {
		defer close(apiDone)
		_, _ = cache.Get(context.Background())
	}()
	<-started
	bootstrapDone := make(chan bool, 1)
	go func() { bootstrapDone <- hub.RunBootstrap(context.Background()) }()
	close(release)
	<-apiDone
	if !<-bootstrapDone || fetches.Load() != 1 {
		t.Fatalf("bootstrap did not share cold request: fetches=%d", fetches.Load())
	}
}

func TestManualRefreshIsTheOnlySubsequentRESTRequest(t *testing.T) {
	var fetches atomic.Int32
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		id := fetches.Add(1)
		return []Game{{ID: fmt.Sprintf("event-%d", id)}}, nil
	}), "NFL")
	hub := NewOddsHub(cache, time.Second)
	if !hub.RunBootstrap(context.Background()) {
		t.Fatal("bootstrap failed")
	}
	server := httptest.NewServer(newHandler(cache, hub))
	defer server.Close()
	for range 2 {
		response, err := server.Client().Get(server.URL + "/api/odds")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("GET /api/odds fetched %d times, want 1", got)
	}
	response, err := server.Client().Post(server.URL+"/api/refresh", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var snapshot OddsSnapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || fetches.Load() != 2 || snapshot.Games[0].ID != "event-2" {
		t.Fatalf("manual refresh failed: status=%d fetches=%d snapshot=%#v", response.StatusCode, fetches.Load(), snapshot)
	}
}

func TestStreamReportsWebSocketConnectionChanges(t *testing.T) {
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		return []Game{{ID: "event-1"}}, nil
	}), "NFL")
	hub := NewOddsHub(cache, time.Second)
	hub.enableWebSocket()
	if !hub.RunBootstrap(context.Background()) {
		t.Fatal("bootstrap failed")
	}
	server := httptest.NewServer(newHandler(cache, hub))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readSSEEvent(t, reader); event != "odds" {
		t.Fatalf("first event = %q, want odds", event)
	}
	for _, want := range []bool{false, true, false} {
		if want {
			hub.setWebSocketConnected(true)
		} else if _, connected := hub.webSocketStatus(); connected {
			hub.setWebSocketConnected(false)
		}
		event, data := readSSEEvent(t, reader)
		if event != "status" || !strings.Contains(data, fmt.Sprintf("\"websocketConnected\":%t", want)) {
			t.Fatalf("status event = %q %q, want connection=%t", event, data, want)
		}
	}
}

func TestRetryDelayBacksOffAndCapsAtThirtySeconds(t *testing.T) {
	base := 2 * time.Second
	if got := nextRetryDelay(base, base); got != 4*time.Second {
		t.Fatalf("first failure delay = %s, want 4s", got)
	}
	if got := nextRetryDelay(16*time.Second, base); got != 30*time.Second {
		t.Fatalf("later failure delay = %s, want 30s", got)
	}
	if got := nextRetryDelay(time.Minute, time.Minute); got != time.Minute {
		t.Fatalf("configured interval should not shrink: got %s", got)
	}
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	var eventName, data string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE event: %v", err)
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
		if line == "" && data != "" {
			return eventName, data
		}
	}
}
