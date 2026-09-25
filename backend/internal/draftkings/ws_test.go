package draftkings

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/vmihailenco/msgpack/v5"
)

const capturedNFLMoneylineUpdate = "ldkkYmRkODNiNDAtMmJiNS00ZWVhLTkwOTQtZWYwYWZmMTc2MmNipnVwZGF0ZZOTlZCQkJCQk5CQkJOQkJGSGJitME1MODQ2OTU3MDRfM6pIT1UgVGV4YW5zlqbiiJIxNDikMS42N6UyNS8zN6U1OS42JaM1NyWlMS42N3jLP/rPkU1E10rAzgDkqPqSo1NHUKNPU0LAwIOrY3JlYXRlZFRpbWW4MjAyNi0wOS0yM1QwMjozNjo0Mi43MzFarHJlY2VpdmVkVGltZbgyMDI2LTA5LTIzVDAyOjM2OjQyLjczOFqtcHVibGlzaGVkVGltZbgyMDI2LTA5LTIzVDAyOjM2OjQzLjI3NlrAktf/RbnSsGqzOzsA"

const capturedNFLSpreadUpdate = "ldkkYmRkODNiNDAtMmJiNS00ZWVhLTkwOTQtZWYwYWZmMTc2MmNipnVwZGF0ZZOTlZCQkJCQk5CQkJOQkJGSGJixMEhDODQ2OTU3MDROMzAwXzOqSE9VIFRleGFuc5am4oiSMTAypDEuOTilNTAvNTGlNTAuNCWjNTAlpTEuOTh4yz//r6+wh0bcy8AIAAAAAAAA0fRHk61NYWluUG9pbnRMaW5lo1NHUKNPU0LAwIOrY3JlYXRlZFRpbWW4MjAyNi0wOS0yM1QwMjozNjo0Mi45MDFarHJlY2VpdmVkVGltZbgyMDI2LTA5LTIzVDAyOjM2OjQyLjkwOVqtcHVibGlzaGVkVGltZbgyMDI2LTA5LTIzVDAyOjM2OjQzLjQ1MVrAktf/dyqkAGqzOzsA"

func TestWebSocketFailureDetailsExposeCauseWithoutSecrets(t *testing.T) {
	frameBytes, err := msgpack.Marshal([]any{"subscription-id", "error", map[string]any{
		"code": 401, "message": "session expired; token=private-value",
	}})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := decodeDraftKingsSocketFrame(frameBytes)
	if err != nil || frame.kind != "error" || !strings.Contains(frame.failureDetail, "401") ||
		!strings.Contains(frame.failureDetail, "session expired") || strings.Contains(frame.failureDetail, "private-value") {
		t.Fatalf("subscription rejection detail was lost or leaked a secret: frame=%#v err=%v", frame, err)
	}
	unsubscribedBytes, err := msgpack.Marshal([]any{"subscription-id", "unsubscribed", nil, map[string]any{"reason": "session expired"}})
	if err != nil {
		t.Fatal(err)
	}
	unsubscribed, err := decodeDraftKingsSocketFrame(unsubscribedBytes)
	if err != nil || !strings.Contains(unsubscribed.failureDetail, "session expired") {
		t.Fatalf("unsubscription reason was lost: frame=%#v err=%v", unsubscribed, err)
	}
	globalErrorBytes, err := msgpack.Marshal([]any{nil, "error", map[string]any{"message": "authentication expired"}})
	if err != nil {
		t.Fatal(err)
	}
	globalError, err := decodeDraftKingsSocketFrame(globalErrorBytes)
	if err != nil || globalError.id != "" || !strings.Contains(globalError.failureDetail, "authentication expired") {
		t.Fatalf("global subscription error was lost: frame=%#v err=%v", globalError, err)
	}
	closeErr := fmt.Errorf("read upstream frame: %w", websocket.CloseError{Code: websocket.StatusPolicyViolation, Reason: "session expired\nBearer private-value access_token=another-secret"})
	detail := socketErrorDescription(closeErr)
	if !strings.Contains(detail, "close_code=1008") || !strings.Contains(detail, "session expired") || strings.Contains(detail, "private-value") || strings.Contains(detail, "another-secret") || strings.Contains(detail, "\n") {
		t.Fatalf("close detail was lost or leaked a secret: %q", detail)
	}
}

func TestWebSocketHandshakeFailureIncludesHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"session expired","token":"private-value"}`)
	}))
	defer server.Close()
	hub := NewOddsHub(nil, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := hub.draftKingsSocketSession(ctx, DefaultConfig(), "ws"+strings.TrimPrefix(server.URL, "http"), func() {}, false)
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "session expired") || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("WebSocket handshake status missing: %v", err)
	}
}

func TestDecodeCapturedNFLSelectionUpdates(t *testing.T) {
	for _, test := range []struct {
		name     string
		encoded  string
		id       string
		american string
		line     *float64
	}{
		{name: "moneyline", encoded: capturedNFLMoneylineUpdate, id: "0ML84695704_3", american: "-148"},
		{name: "spread", encoded: capturedNFLSpreadUpdate, id: "0HC84695704N300_3", american: "-102", line: floatPtr(-3)},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := base64.StdEncoding.DecodeString(test.encoded)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := decodeDraftKingsSocketFrame(raw)
			if err != nil {
				t.Fatal(err)
			}
			if frame.kind != "update" || len(frame.selections) != 1 {
				t.Fatalf("unexpected frame: %#v", frame)
			}
			selection := frame.selections[0]
			if selection.ID != test.id || selection.Label != "HOU Texans" || selection.Price.American != test.american {
				t.Fatalf("unexpected selection: %#v", selection)
			}
			if (selection.Price.Line == nil) != (test.line == nil) || (test.line != nil && *selection.Price.Line != *test.line) {
				t.Fatalf("unexpected line: %#v", selection.Price.Line)
			}
			if selection.SourcePublishedAt.IsZero() {
				t.Fatalf("DraftKings publishedTime was not decoded: %#v", selection)
			}
		})
	}
}

func TestSubscriptionRequestMatchesNFLPartials(t *testing.T) {
	request := DefaultConfig().subscriptionRequest("request-id")
	encoded, err := msgpack.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := msgpack.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["method"] != "subscribe" || decoded["id"] != "request-id" {
		t.Fatalf("unexpected request: %#v", decoded)
	}
	params := decoded["params"].(map[string]any)
	query := params["queryParams"].(map[string]any)
	wantEvents := "$filter=leagueId eq '88808' AND clientMetadata/Subcategories/any(s: s/Id eq '4518') and tags/any(t: t eq 'OSB')"
	wantMarkets := "$filter=clientMetadata/subCategoryId eq '4518' AND tags/all(t: t ne 'SportcastBetBuilder') and tags/any(t: t eq 'OSB')"
	if query["initialData"] != false || query["query"] != wantEvents || query["includeMarkets"] != wantMarkets {
		t.Fatalf("unexpected NFL subscription filters: %#v", query)
	}
}

func TestKnownLiveSelectionUpdatesCacheWithoutMutatingEarlierSnapshot(t *testing.T) {
	games := normalizeDraftKings(draftKingsResponse{
		Events:  []draftKingsEvent{{ID: "34118261", Name: "HOU Texans @ IND Colts"}},
		Markets: []draftKingsMarket{{ID: "1_84695704", EventID: "34118261", Name: "Moneyline"}},
		Selections: []draftKingsSelection{
			{ID: "0ML84695704_3", MarketID: "1_84695704", Label: "HOU Texans", DisplayOdds: draftKingsDisplayOdds{American: "−148", Decimal: "1.67"}},
			{ID: "0ML84695704_1", MarketID: "1_84695704", Label: "IND Colts", DisplayOdds: draftKingsDisplayOdds{American: "+124", Decimal: "2.24"}},
		},
	})
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) { return games, nil }), "NFL")
	before, err := cache.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if before.Games[0].Away.Moneyline.SelectionID != "0ML84695704_3" {
		t.Fatalf("REST selection ID was not retained: %#v", before.Games[0].Away.Moneyline)
	}
	updated, changed, err := cache.ApplySelection("0ML84695704_3", "HOU Texans", Price{American: "-150", Decimal: "1.67"}, before.UpdatedAt.Add(time.Second))
	if err != nil || !changed {
		t.Fatalf("live update failed: changed=%t err=%v", changed, err)
	}
	if before.Games[0].Away.Moneyline.American != "-148" || updated.Games[0].Away.Moneyline.American != "-150" || updated.Games[0].Home.Moneyline.American != "+124" {
		t.Fatalf("unexpected before/after odds: before=%#v after=%#v", before.Games, updated.Games)
	}
	if updated.UpdateSource != "websocket" || !updated.FetchCompleted.IsZero() {
		t.Fatalf("unexpected live snapshot metadata: %#v", updated)
	}
	if !updated.FetchedAt.Equal(before.FetchedAt) || !updated.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("REST fetchedAt should remain stable while updatedAt advances: before=%#v after=%#v", before, updated)
	}
	publicJSON, err := json.Marshal(updated)
	if err != nil || strings.Contains(string(publicJSON), "0ML84695704_3") {
		t.Fatalf("internal selection ID leaked into public snapshot: %s err=%v", publicJSON, err)
	}
	if _, changed, err := cache.ApplySelection("0ML84695704_3", "HOU Texans", Price{American: "-150", Decimal: "1.67"}, time.Now()); err != nil || changed {
		t.Fatalf("identical price should not publish: changed=%t err=%v", changed, err)
	}
	if _, _, err := cache.ApplySelection("new-selection", "HOU Texans", Price{American: "-150"}, time.Now()); !errors.Is(err, ErrUnknownSelection) {
		t.Fatalf("new selection should require REST resync: %v", err)
	}
	if _, _, err := cache.ApplySelection("0ML84695704_3", "HOU Texans", Price{American: "-150", Line: floatPtr(-3)}, time.Now()); !errors.Is(err, ErrUnknownSelection) {
		t.Fatalf("unexpected line structure should require REST resync: %v", err)
	}
}

func TestInFlightRESTFetchDoesNotOverwriteWebSocketPrice(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	initial := []Game{{ID: "34118261", Away: TeamOdds{Name: "HOU Texans", Moneyline: &Price{American: "-145", SelectionID: "0ML84695704_3", SelectionLabel: "HOU Texans"}}, Home: TeamOdds{Name: "IND Colts", Moneyline: &Price{American: "+105", SelectionID: "home-id", SelectionLabel: "IND Colts"}}}}
	count := 0
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		count++
		if count == 2 {
			close(started)
			<-release
			return []Game{
				{ID: "34118261", Away: TeamOdds{Name: "HOU Texans", Moneyline: &Price{American: "-145", SelectionID: "0ML84695704_3", SelectionLabel: "HOU Texans"}}, Home: TeamOdds{Name: "IND Colts", Moneyline: &Price{American: "+110", SelectionID: "home-id", SelectionLabel: "IND Colts"}}},
				{ID: "new-game", Away: TeamOdds{Name: "New Away", Moneyline: &Price{American: "+120", SelectionID: "new-id", SelectionLabel: "New Away"}}},
			}, nil
		}
		return initial, nil
	}), "NFL")
	if _, err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	refreshDone := make(chan OddsSnapshot, 1)
	go func() {
		snapshot, _ := cache.Refresh(context.Background())
		refreshDone <- snapshot
	}()
	<-started
	if _, changed, err := cache.ApplySelection("0ML84695704_3", "HOU Texans", Price{American: "-148"}, time.Now()); err != nil || !changed {
		t.Fatalf("live update failed during REST fetch: changed=%t err=%v", changed, err)
	}
	close(release)
	snapshot := <-refreshDone
	if len(snapshot.Games) != 2 || snapshot.Games[0].Away.Moneyline.American != "-148" || snapshot.Games[0].Home.Moneyline.American != "+110" || snapshot.Games[1].ID != "new-game" || snapshot.UpdateSource != "rest" {
		t.Fatalf("REST reconciliation lost a live price or new data: %#v", snapshot)
	}
	if snapshot.Moves["34118261:away:moneyline"].Revision == snapshot.Revision || snapshot.Moves["34118261:home:moneyline"].Revision != snapshot.Revision || !cache.HasSelection("new-id") {
		t.Fatalf("reconciled selection IDs or move timings are wrong: %#v", snapshot)
	}
}

func TestRestReconciliationHandlesSelectionIdentityChanges(t *testing.T) {
	for _, test := range []struct {
		name         string
		restID       string
		liveID       string
		replacesID   string
		wantID       string
		wantAmerican string
	}{
		{name: "REST has newer unlinked main ID", restID: "rest-new", liveID: "old", wantID: "rest-new", wantAmerican: "-120"},
		{name: "WebSocket linked replacement is newer", restID: "old", liveID: "live-new", replacesID: "old", wantID: "live-new", wantAmerican: "-115"},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			calls := 0
			cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
				calls++
				if calls == 2 {
					close(started)
					<-release
					return []Game{{ID: "game", Away: TeamOdds{Name: "Away", Moneyline: &Price{American: "-120", SelectionID: test.restID, SelectionLabel: "Away"}}}}, nil
				}
				return []Game{{ID: "game", Away: TeamOdds{Name: "Away", Moneyline: &Price{American: "-110", SelectionID: "old", SelectionLabel: "Away"}}}}, nil
			}), "NFL")
			if _, err := cache.Get(context.Background()); err != nil {
				t.Fatal(err)
			}
			refreshed := make(chan OddsSnapshot, 1)
			go func() {
				snapshot, _ := cache.Refresh(context.Background())
				refreshed <- snapshot
			}()
			<-started
			if _, changed, err := cache.ApplyLiveSelection(liveSelectionUpdate{ID: test.liveID, ReplacesID: test.replacesID, Label: "Away", Price: Price{American: "-115"}}, time.Now()); err != nil || !changed {
				t.Fatalf("live update: changed=%t err=%v", changed, err)
			}
			close(release)
			snapshot := <-refreshed
			price := snapshot.Games[0].Away.Moneyline
			if price.SelectionID != test.wantID || price.American != test.wantAmerican || !cache.HasSelection(test.wantID) || snapshot.UpdateSource != "rest" {
				t.Fatalf("wrong quote after reconciliation: price=%#v snapshot=%#v", price, snapshot)
			}
		})
	}
}

func TestUnknownSocketChangeRequiresResync(t *testing.T) {
	message := []any{"request-id", "update", []any{[]any{[]any{[]any{1}}, []any{[]any{}, []any{}, []any{}}, []any{[]any{}, []any{}, []any{}}}, nil, map[string]any{}}, nil, []any{time.Now(), 0}}
	raw, err := msgpack.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := decodeDraftKingsSocketFrame(raw)
	if err != nil || !frame.requiresResync {
		t.Fatalf("event addition must force resync: frame=%#v err=%v", frame, err)
	}
}

func TestDecodeLiveLineReplacementAlongsideScoreAndMarketUpdates(t *testing.T) {
	newID := "0HC86420797N250_3"
	oldID := "0HC86420797N150_3"
	message := []any{"request-id", "update", []any{
		[]any{
			[]any{[]any{}, []any{}, []any{[]any{int64(35), []any{newID, "2_86420797", "HOU Texans"}}}},
			[]any{[]any{}, []any{}, []any{oldID}},
			[]any{
				[]any{[]any{int64(22), []any{"34704699", "score change"}}},
				[]any{[]any{int64(23), []any{"2_86420797", "Spread"}}},
				[]any{[]any{int64(24), []any{newID, "HOU Texans", []any{"−105", "1.95"}, 1.95, -2.5, 0, []any{"OSB"}, oldID}}},
			},
		}, nil, map[string]any{"publishedTime": "2026-09-23T02:17:31.910Z"},
	}, nil, []any{time.Now(), 0}}
	raw, err := msgpack.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := decodeDraftKingsSocketFrame(raw)
	if err != nil || frame.requiresResync || len(frame.selections) != 1 || len(frame.removedSelectionIDs) != 1 {
		t.Fatalf("unexpected live frame: frame=%#v err=%v", frame, err)
	}
	selection := frame.selections[0]
	if selection.ID != newID || selection.ReplacesID != oldID || selection.Price.Line == nil || *selection.Price.Line != -2.5 || selection.Price.American != "-105" {
		t.Fatalf("line replacement was not decoded: %#v", selection)
	}
	if selection.SourcePublishedAt.Format(time.RFC3339Nano) != "2026-09-23T02:17:31.91Z" {
		t.Fatalf("published time was not decoded: %#v", selection.SourcePublishedAt)
	}
}

func TestLiveLineReplacementRemapsSelectionWithoutREST(t *testing.T) {
	oldID := "0HC86420797N150_3"
	newID := "0HC86420797N250_3"
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		return []Game{{ID: "34704699", Away: TeamOdds{Name: "HOU Texans", Spread: &Price{Line: floatPtr(-1.5), American: "-110", Decimal: "1.91", SelectionID: oldID, SelectionLabel: "HOU Texans"}}}}, nil
	}), "NFL")
	before, err := cache.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now()
	publishedAt := observedAt.Add(-125 * time.Millisecond)
	after, changed, err := cache.ApplyLiveSelection(liveSelectionUpdate{ID: newID, ReplacesID: oldID, Label: "HOU Texans", Price: Price{Line: floatPtr(-2.5), American: "-105", Decimal: "1.95"}, SourcePublishedAt: publishedAt}, observedAt)
	if err != nil || !changed || after.Games[0].Away.Spread.Line == nil || *after.Games[0].Away.Spread.Line != -2.5 {
		t.Fatalf("replacement was not applied: after=%#v changed=%t err=%v", after, changed, err)
	}
	if after.Move == nil || after.Move.Revision != after.Revision || after.Move.GameID != "34704699" || after.Move.Market != "Spread" || after.Move.DraftKingsToGoMs == nil || *after.Move.DraftKingsToGoMs != 125 {
		t.Fatalf("live move timing was not retained: %#v", after.Move)
	}
	if !after.Move.GoObservedAt.Equal(observedAt) || after.Move.DraftKingsPublished == nil || !after.Move.DraftKingsPublished.Equal(publishedAt) {
		t.Fatalf("live move timestamps are wrong: %#v", after.Move)
	}
	if cache.HasSelection(oldID) || !cache.HasSelection(newID) || *before.Games[0].Away.Spread.Line != -1.5 {
		t.Fatalf("selection index or prior snapshot was corrupted: before=%#v after=%#v", before, after)
	}
	if !cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: oldID}) || cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: newID}) {
		t.Fatal("replacement did not demote the old ID and promote the new ID")
	}
	if _, changed, err := cache.ApplyLiveSelection(liveSelectionUpdate{ID: newID, Label: "HOU Texans", Price: Price{Line: floatPtr(-2.5), American: "-110", Decimal: "1.91"}}, time.Now()); err != nil || !changed {
		t.Fatalf("follow-up price update for new ID failed: changed=%t err=%v", changed, err)
	}
}

func TestWebSocketUnknownSelectionsRequestRecoveryButKnownAlternativeDoesNot(t *testing.T) {
	games := normalizeDraftKings(draftKingsResponse{
		Events: []draftKingsEvent{{ID: "game", Name: "Away @ Home"}},
		Markets: []draftKingsMarket{
			{ID: "main", EventID: "game", MarketType: draftKingsMarketType{Name: "Moneyline"}},
			{ID: "prop", EventID: "game", MarketType: draftKingsMarketType{Name: "Anytime scorer"}},
		},
		Selections: []draftKingsSelection{
			{ID: "displayed", MarketID: "main", Label: "Away", DisplayOdds: draftKingsDisplayOdds{American: "+100"}},
			{ID: "alternative", MarketID: "prop", Label: "Player", DisplayOdds: draftKingsDisplayOdds{American: "+200"}},
		},
	})
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) { return games, nil }), "NFL")
	if _, err := cache.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	hub := NewOddsHub(cache, time.Second)
	var resyncs atomic.Int32
	serverErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, requestBytes, err := conn.Read(ctx)
		if err != nil {
			serverErrors <- err
			return
		}
		var request map[string]any
		if err := msgpack.Unmarshal(requestBytes, &request); err != nil {
			serverErrors <- err
			return
		}
		id, ok := request["id"].(string)
		if !ok {
			serverErrors <- errors.New("missing subscription ID")
			return
		}
		for index, message := range []any{
			[]any{id, "subscribed", nil},
			socketSelectionFrame(id, "alternative"),
			socketSelectionFrame(id, "new-1"),
			socketSelectionFrame(id, "new-2"),
		} {
			raw, err := msgpack.Marshal(message)
			if err == nil {
				err = conn.Write(ctx, websocket.MessageBinary, raw)
			}
			if err != nil {
				serverErrors <- err
				return
			}
			if index == 1 {
				for hub.metrics().WebSocketIgnored == 0 && ctx.Err() == nil {
					time.Sleep(time.Millisecond)
				}
				if ctx.Err() != nil || resyncs.Load() != 0 {
					serverErrors <- errors.New("known alternative caused a REST resync")
					return
				}
			}
		}
		serverErrors <- conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	subscribed, _ := hub.draftKingsSocketSession(ctx, DefaultConfig(), "ws"+strings.TrimPrefix(server.URL, "http"), func() { resyncs.Add(1) }, false)
	if err := <-serverErrors; err != nil {
		t.Fatal(err)
	}
	if !subscribed || resyncs.Load() != 2 || hub.metrics().WebSocketIgnored != 1 {
		t.Fatalf("expected both unknown IDs to request recovery and one ignored alternative: subscribed=%t resyncs=%d metrics=%#v", subscribed, resyncs.Load(), hub.metrics())
	}
}

func socketSelectionFrame(subscriptionID, selectionID string) []any {
	selection := []any{int64(24), []any{selectionID, "Player", []any{"+210", "3.10"}, 3.1, nil, 0, []any{"OSB"}, nil}}
	groups := []any{
		[]any{[]any{}, []any{}, []any{}},
		[]any{[]any{}, []any{}, []any{}},
		[]any{[]any{}, []any{}, []any{selection}},
	}
	return []any{subscriptionID, "update", []any{groups, nil, map[string]any{}}, nil, []any{time.Now(), 0}}
}

func TestLiveSnapshotKeepsPerPriceTimingAcrossCoalescedUpdates(t *testing.T) {
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		return []Game{{
			ID:   "game-1",
			Away: TeamOdds{Name: "Away", Moneyline: &Price{American: "-110", SelectionID: "away-id", SelectionLabel: "Away"}},
			Home: TeamOdds{Name: "Home", Moneyline: &Price{American: "+100", SelectionID: "home-id", SelectionLabel: "Home"}},
		}}, nil
	}), "NFL")
	if _, err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, changed, err := cache.ApplySelection("away-id", "Away", Price{American: "-115"}, time.Now())
	if err != nil || !changed {
		t.Fatalf("first move: changed=%t err=%v", changed, err)
	}
	second, changed, err := cache.ApplySelection("home-id", "Home", Price{American: "+105"}, time.Now())
	if err != nil || !changed {
		t.Fatalf("second move: changed=%t err=%v", changed, err)
	}
	awayKey := "game-1:away:moneyline"
	homeKey := "game-1:home:moneyline"
	if len(first.Moves) != 1 || first.Moves[awayKey].Revision != first.Move.Revision {
		t.Fatalf("first snapshot timing: %#v", first.Moves)
	}
	if len(second.Moves) != 2 || second.Moves[awayKey].Revision != first.Move.Revision || second.Moves[homeKey].Revision != second.Move.Revision {
		t.Fatalf("coalesced snapshot lost a quote timing: %#v", second.Moves)
	}
	hub := NewOddsHub(cache, time.Second)
	updates, _, unsubscribe := hub.subscribe()
	defer unsubscribe()
	hub.publishSnapshot(first)
	hub.publishSnapshot(second)
	var delivered OddsSnapshot
	if err := json.Unmarshal((<-updates).Data, &delivered); err != nil || len(delivered.Moves) != 2 {
		t.Fatalf("dropped SSE frame lost a quote timing: moves=%#v err=%v", delivered.Moves, err)
	}
	refreshed, err := cache.Refresh(context.Background())
	if err != nil || refreshed.UpdateSource != "rest" || len(refreshed.Moves) != 2 || refreshed.Moves[awayKey].Revision != refreshed.Revision || refreshed.Moves[homeKey].Revision != refreshed.Revision {
		t.Fatalf("REST recovery did not time its changed quotes: %#v err=%v", refreshed.Moves, err)
	}
	hub.publishSnapshot(refreshed)
}

func TestWebSocketSessionAppliesCapturedUpdate(t *testing.T) {
	games := []Game{{ID: "34118261", Away: TeamOdds{Name: "HOU Texans", Moneyline: &Price{American: "-145", Decimal: "1.69", SelectionID: "0ML84695704_3", SelectionLabel: "HOU Texans"}}, Home: TeamOdds{Name: "IND Colts"}}}
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) { return games, nil }), "NFL")
	if _, err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	hub := NewOddsHub(cache, time.Hour)
	sseServer := httptest.NewServer(newHandler(cache, hub.OddsHub))
	defer sseServer.Close()
	streamCtx, stopStream := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopStream()
	streamRequest, err := http.NewRequestWithContext(streamCtx, http.MethodGet, sseServer.URL+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResponse, err := sseServer.Client().Do(streamRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResponse.Body.Close()
	serverErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, requestBytes, err := conn.Read(ctx)
		if err != nil {
			serverErrors <- err
			return
		}
		var request map[string]any
		if err := msgpack.Unmarshal(requestBytes, &request); err != nil {
			serverErrors <- err
			return
		}
		if request["method"] != "subscribe" {
			serverErrors <- errors.New("client did not subscribe")
			return
		}
		id := request["id"].(string)
		ack, _ := msgpack.Marshal([]any{id, "subscribed", nil, nil, []any{time.Now(), 0}})
		if err := conn.Write(ctx, websocket.MessageBinary, ack); err != nil {
			serverErrors <- err
			return
		}
		raw, _ := base64.StdEncoding.DecodeString(capturedNFLMoneylineUpdate)
		var update []any
		if err := msgpack.Unmarshal(raw, &update); err != nil {
			serverErrors <- err
			return
		}
		update[0] = id
		patched, _ := msgpack.Marshal(update)
		if err := conn.Write(ctx, websocket.MessageBinary, patched); err != nil {
			serverErrors <- err
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
		serverErrors <- nil
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resyncs := 0
	subscribed, _ := hub.draftKingsSocketSession(ctx, DefaultConfig(), "ws"+strings.TrimPrefix(server.URL, "http"), func() { resyncs++ }, false)
	if err := <-serverErrors; err != nil {
		t.Fatal(err)
	}
	if !subscribed {
		t.Fatal("WebSocket subscription was not acknowledged")
	}
	if resyncs != 0 {
		t.Fatalf("first subscription caused %d redundant REST resyncs", resyncs)
	}
	reader := bufio.NewReader(streamResponse.Body)
	eventName, eventData := readSSEEvent(t, reader)
	if eventName == "status" {
		eventName, eventData = readSSEEvent(t, reader)
	}
	var pushed OddsSnapshot
	if err := json.Unmarshal([]byte(eventData), &pushed); err != nil || eventName != "odds" || pushed.UpdateSource != "websocket" || pushed.Games[0].Away.Moneyline.American != "-148" {
		t.Fatalf("WebSocket change was not pushed over SSE: event=%q snapshot=%#v err=%v", eventName, pushed, err)
	}
	if pushed.Move == nil || pushed.Move.DraftKingsPublished == nil || pushed.Move.GoObservedAt.IsZero() {
		t.Fatalf("move timing was not pushed over SSE: %#v", pushed.Move)
	}
	snapshot, err := cache.Get(context.Background())
	if err != nil || snapshot.Games[0].Away.Moneyline.American != "-148" || snapshot.UpdateSource != "websocket" {
		t.Fatalf("WebSocket update not applied: snapshot=%#v err=%v", snapshot, err)
	}
	metrics := hub.metrics()
	if metrics.WebSocketConnected || metrics.WebSocketUpdates != 1 {
		t.Fatalf("unexpected WebSocket metrics: %#v", metrics)
	}
}

func floatPtr(value float64) *float64 { return &value }
