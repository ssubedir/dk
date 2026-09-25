package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBackendServesOnlyAPI(t *testing.T) {
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		return []Game{}, nil
	}), "NFL")
	handler := withCORS(newHandler(cache, NewOddsHub(cache, time.Second)), "https://linewatch.example.com")

	root := httptest.NewRecorder()
	handler.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusNotFound {
		t.Fatalf("root should not serve a UI: got %d", root.Code)
	}

	apiRequest := httptest.NewRequest(http.MethodGet, "/api/odds", nil)
	apiRequest.Header.Set("Origin", "https://linewatch.example.com")
	api := httptest.NewRecorder()
	handler.ServeHTTP(api, apiRequest)
	if api.Code != http.StatusOK || api.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("API should return JSON: status=%d content-type=%q", api.Code, api.Header().Get("Content-Type"))
	}
	if api.Header().Get("Access-Control-Allow-Origin") != "https://linewatch.example.com" {
		t.Fatalf("expected configured frontend origin, got %q", api.Header().Get("Access-Control-Allow-Origin"))
	}
	if api.Header().Get("Access-Control-Expose-Headers") != "Retry-After" {
		t.Fatalf("frontend cannot read the refresh cooldown: %q", api.Header().Get("Access-Control-Expose-Headers"))
	}

	otherRequest := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	otherRequest.Header.Set("Origin", "https://other.example.com")
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, otherRequest)
	if other.Code != http.StatusOK || other.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected health/CORS response: status=%d origin=%q", other.Code, other.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestRemovedLatencyAcknowledgementRouteIsNotRegistered(t *testing.T) {
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) { return nil, nil }), "NFL")
	handler := newHandler(cache, NewOddsHub(cache, time.Second))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/latency-ack/99", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("removed route returned %d, want 404", response.Code)
	}
}

func TestAnalyticsUnavailableWithoutStore(t *testing.T) {
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) { return nil, nil }), "NFL")
	handler := newHandler(cache, NewOddsHub(cache, time.Second))
	for _, path := range []string{"/api/analysis/summary", "/api/analysis/moves?gameId=game"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: got %d, want 503", path, response.Code)
		}
	}
}

func TestManualRefreshIsGloballyRateLimited(t *testing.T) {
	calls := 0
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		calls++
		return []Game{}, nil
	}), "NFL")
	hub := NewOddsHub(cache, time.Second)
	handler := newHandler(cache, hub)
	refresh := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/refresh", nil))
		return response
	}
	if first := refresh(); first.Code != http.StatusOK || calls != 1 {
		t.Fatalf("first manual refresh failed: status=%d calls=%d", first.Code, calls)
	}
	if limited := refresh(); limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" || calls != 1 {
		t.Fatalf("immediate repeat was not limited: status=%d calls=%d", limited.Code, calls)
	}
}
