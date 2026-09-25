package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ssubedir/dk/backend/internal/analytics"
)

func TestAnalyticsHandlersReadInMemoryHistory(t *testing.T) {
	store := analytics.NewStore(10)
	now := time.Now().UTC()
	baseline := analytics.Observation{
		ObservedAt: now, League: "NFL", GameID: "game-1",
		QuoteKey: "game-1:away:moneyline", American: "-110",
	}
	changed := baseline
	changed.ObservedAt = now.Add(time.Millisecond)
	changed.American = "-115"
	publishedAt := now.Add(-time.Millisecond)
	sourceToGoMs := 2.0
	changed.SourcePublishedAt = &publishedAt
	changed.SourceToGoMs = &sourceToGoMs
	changed.GoObservedAt = now.Add(time.Millisecond)
	store.Record([]analytics.Observation{baseline, changed})
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) { return nil, nil }), "NFL")
	handler := newHandler(cache, NewOddsHub(cache, time.Second), store)

	summaryResponse := httptest.NewRecorder()
	handler.ServeHTTP(summaryResponse, httptest.NewRequest(http.MethodGet, "/api/analysis/summary?hours=24", nil))
	if summaryResponse.Code != http.StatusOK {
		t.Fatalf("summary status = %d: %s", summaryResponse.Code, summaryResponse.Body.String())
	}
	var summary analytics.Summary
	if err := json.Unmarshal(summaryResponse.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Observations != 2 || summary.PriceChanges != 1 || summary.History.Capacity != 10 {
		t.Fatalf("unexpected API summary: %#v", summary)
	}

	movesResponse := httptest.NewRecorder()
	handler.ServeHTTP(movesResponse, httptest.NewRequest(http.MethodGet, "/api/analysis/moves?gameId=game-1", nil))
	if movesResponse.Code != http.StatusOK {
		t.Fatalf("moves status = %d: %s", movesResponse.Code, movesResponse.Body.String())
	}
	var moves []analytics.Move
	if err := json.Unmarshal(movesResponse.Body.Bytes(), &moves); err != nil {
		t.Fatal(err)
	}
	if len(moves) != 1 || moves[0].PreviousAmerican == nil || *moves[0].PreviousAmerican != "-110" {
		t.Fatalf("unexpected API moves: %#v", moves)
	}

	dumpResponse := httptest.NewRecorder()
	handler.ServeHTTP(dumpResponse, httptest.NewRequest(http.MethodGet, "/api/analysis/dump?limit=1", nil))
	if dumpResponse.Code != http.StatusOK || dumpResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("dump status = %d, cache = %q: %s", dumpResponse.Code,
			dumpResponse.Header().Get("Cache-Control"), dumpResponse.Body.String())
	}
	var firstPage analytics.DumpPage
	if err := json.Unmarshal(dumpResponse.Body.Bytes(), &firstPage); err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Observations) != 1 || firstPage.Observations[0].Sequence != 2 ||
		firstPage.NextBefore != 2 || firstPage.Observations[0].PreviousAmerican == nil ||
		*firstPage.Observations[0].PreviousAmerican != "-110" ||
		firstPage.Observations[0].SourcePublishedAt == nil ||
		!firstPage.Observations[0].SourcePublishedAt.Equal(publishedAt) ||
		firstPage.Observations[0].SourceToGoMs == nil ||
		*firstPage.Observations[0].SourceToGoMs != sourceToGoMs {
		t.Fatalf("unexpected API dump: %#v", firstPage)
	}
	olderResponse := httptest.NewRecorder()
	handler.ServeHTTP(olderResponse, httptest.NewRequest(http.MethodGet, "/api/analysis/dump?before=2&limit=1&gameId=game-1", nil))
	var olderPage analytics.DumpPage
	if err := json.Unmarshal(olderResponse.Body.Bytes(), &olderPage); err != nil {
		t.Fatal(err)
	}
	if olderResponse.Code != http.StatusOK || len(olderPage.Observations) != 1 ||
		olderPage.Observations[0].Sequence != 1 || olderPage.NextBefore != 0 {
		t.Fatalf("unexpected older dump page: %#v", olderPage)
	}
	for _, url := range []string{
		"/api/analysis/dump?limit=0", "/api/analysis/dump?limit=1001",
		"/api/analysis/dump?before=bad", "/api/analysis/dump?before=0",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d: %s", url, response.Code, response.Body.String())
		}
	}
}
