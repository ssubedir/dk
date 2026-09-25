package draftkings

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDraftKingsNetworkErrorDoesNotExposeURLSecrets(t *testing.T) {
	config := DefaultConfig()
	config.APIURL = "https://example.com/markets?token=secret-value"
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}
	provider, err := NewDraftKingsProvider(config, client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "connection refused") || !strings.Contains(err.Error(), "example.com") {
		t.Fatalf("missing network failure details: %v", err)
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("network failure leaked URL query: %v", err)
	}
}

func TestDraftKingsHTTPErrorIncludesUpstreamStatusAndBody(t *testing.T) {
	body := "{\n  \"error\": \"region blocked\"\n}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.APIURL = server.URL
	provider, err := NewDraftKingsProvider(config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected DraftKings HTTP error")
	}
	for _, want := range []string{"403 Forbidden", `Content-Type="application/json"`, strconv.Quote(body)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("upstream error missing %q: %v", want, err)
		}
	}
}

func TestDraftKingsHTTPErrorBodyIsExplicitlyTruncated(t *testing.T) {
	body := strings.Repeat("x", draftKingsErrorBodyLimit+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.APIURL = server.URL
	provider, err := NewDraftKingsProvider(config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "429 Too Many Requests") ||
		!strings.Contains(err.Error(), "truncated after 8192 bytes") || strings.Contains(err.Error(), body) {
		t.Fatalf("expected bounded, clearly marked upstream response: %v", err)
	}
}

func TestDraftKingsInvalidJSONIncludesUpstreamBodyPrefix(t *testing.T) {
	body := "<html>DraftKings upstream error</html>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.APIURL = server.URL
	provider, err := NewDraftKingsProvider(config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode DraftKings HTTP 200 response") ||
		!strings.Contains(err.Error(), `Content-Type="text/html"`) || !strings.Contains(err.Error(), strconv.Quote(body)) {
		t.Fatalf("expected upstream payload in decode failure: %v", err)
	}
}

func TestDraftKingsEndpointUsesNFLLeagueAndSubcategory(t *testing.T) {
	config := DefaultConfig()

	provider, err := NewDraftKingsProvider(config, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(provider.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	query := endpoint.Query()
	if query.Get("templateVars") != "88808" {
		t.Fatalf("unexpected templateVars: %q", query.Get("templateVars"))
	}
	if want := "$filter=leagueId eq '88808' AND clientMetadata/Subcategories/any(s: s/Id eq '4518')"; query.Get("eventsQuery") != want {
		t.Fatalf("unexpected eventsQuery: %q", query.Get("eventsQuery"))
	}
	if want := "$filter=clientMetadata/subCategoryId eq '4518' AND tags/all(t: t ne 'SportcastBetBuilder')"; query.Get("marketsQuery") != want {
		t.Fatalf("unexpected marketsQuery: %q", query.Get("marketsQuery"))
	}
}

func TestDraftKingsEndpointUsesExplicitTemplateVars(t *testing.T) {
	config := DefaultConfig()
	config.TemplateVars = "88808,4518"
	provider, err := NewDraftKingsProvider(config, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(provider.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	query := endpoint.Query()
	if query.Get("templateVars") != "88808,4518" || query.Get("include") != "Events" || query.Get("entity") != "events" {
		t.Fatalf("unexpected URL query: %v", query)
	}
	if want := "$filter=leagueId eq '88808' AND clientMetadata/Subcategories/any(s: s/Id eq '4518')"; query.Get("eventsQuery") != want {
		t.Fatalf("unexpected events filter: %q", query.Get("eventsQuery"))
	}
	if want := "$filter=clientMetadata/subCategoryId eq '4518' AND tags/all(t: t ne 'SportcastBetBuilder')"; query.Get("marketsQuery") != want {
		t.Fatalf("unexpected markets filter: %q", query.Get("marketsQuery"))
	}
}

func TestDraftKingsProviderUsesConfiguredPageURLAndLeagueName(t *testing.T) {
	pageURL := "https://sportsbook.draftkings.com/leagues/football/nfl"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != pageURL {
			t.Errorf("unexpected Referer: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[{"id":"event-1","name":"Away @ Home"}],"markets":[],"selections":[]}`))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.LeagueName = "NFL"
	config.PageURL = pageURL
	config.APIURL = server.URL
	provider, err := NewDraftKingsProvider(config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if provider.endpoint != server.URL {
		t.Fatalf("endpoint override ignored: %q", provider.endpoint)
	}
	_, err = provider.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "NFL") || !strings.Contains(err.Error(), "events=1") ||
		!strings.Contains(err.Error(), `body prefix="{\"events\":[{\"id\":\"event-1\"`) {
		t.Fatalf("expected configured league and upstream response in empty-market error, got %v", err)
	}
}

func TestDraftKingsProviderAcceptsExplicitEmptySlate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[],"markets":[],"selections":[]}`))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.APIURL = server.URL
	provider, err := NewDraftKingsProvider(config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	games, err := provider.Fetch(context.Background())
	if err != nil || games == nil || len(games) != 0 {
		t.Fatalf("expected a valid empty slate, got games=%#v err=%v", games, err)
	}
}

func TestDraftKingsProviderRejectsMissingSlateFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.APIURL = server.URL
	provider, err := NewDraftKingsProvider(config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no upcoming NFL game-line markets") {
		t.Fatalf("missing slate fields should not look like a valid empty schedule: %v", err)
	}
}

func TestDraftKingsConfigRejectsUnsafeIDs(t *testing.T) {
	config := DefaultConfig()
	config.LeagueID = "88808' OR true"
	if _, err := NewDraftKingsProvider(config, http.DefaultClient); err == nil {
		t.Fatal("expected invalid league ID to be rejected")
	}
}

func TestDraftKingsConfigRejectsUnsafeTemplateVars(t *testing.T) {
	config := DefaultConfig()
	config.TemplateVars = "88808,4518' OR true"
	if _, err := NewDraftKingsProvider(config, http.DefaultClient); err == nil {
		t.Fatal("expected invalid template variables to be rejected")
	}
}
