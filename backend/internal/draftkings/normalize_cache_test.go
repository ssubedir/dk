package draftkings

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ssubedir/dk/backend/internal/oddsstore"
)

var NewOddsCache = oddsstore.NewOddsCache

func TestEmptySlateSnapshotSerializesAsArray(t *testing.T) {
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		return []Game{}, nil
	}), "NFL")
	snapshot, err := cache.Get(context.Background())
	if err != nil || snapshot.Stale || snapshot.Games == nil || len(snapshot.Games) != 0 {
		t.Fatalf("expected a fresh empty slate: snapshot=%#v err=%v", snapshot, err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || !strings.Contains(string(encoded), `"games":[]`) {
		t.Fatalf("empty slate must be a JSON array, got %s (err=%v)", encoded, err)
	}
}

func TestNormalizeDraftKingsMainMarkets(t *testing.T) {
	start := time.Date(2026, time.September, 24, 0, 15, 0, 0, time.UTC)
	awaySpread := 6.0
	homeSpread := -6.0
	total := 43.5

	payload := draftKingsResponse{
		Events: []draftKingsEvent{{
			ID: "event-1", Name: "ATL Falcons @ GB Packers", StartEventDate: start, Status: "NOT_STARTED",
		}},
		Markets: []draftKingsMarket{
			{ID: "moneyline", EventID: "event-1", MarketType: draftKingsMarketType{Name: "Moneyline"}},
			{ID: "spread", EventID: "event-1", MarketType: draftKingsMarketType{Name: "Spread"}},
			{ID: "total", EventID: "event-1", MarketType: draftKingsMarketType{Name: "Total"}},
			{ID: "prop", EventID: "event-1", MarketType: draftKingsMarketType{Name: "Anytime Touchdown Scorer"}},
		},
		Selections: []draftKingsSelection{
			{MarketID: "moneyline", Label: "ATL Falcons", DisplayOdds: draftKingsDisplayOdds{American: "+225", Decimal: "3.25"}},
			{MarketID: "moneyline", Label: "GB Packers", DisplayOdds: draftKingsDisplayOdds{American: "−278", Decimal: "1.36"}},
			{MarketID: "spread", Label: "ATL Falcons", Points: &awaySpread, DisplayOdds: draftKingsDisplayOdds{American: "−112"}},
			{MarketID: "spread", Label: "GB Packers", Points: &homeSpread, DisplayOdds: draftKingsDisplayOdds{American: "−108"}},
			{MarketID: "total", Label: "Over", Points: &total, DisplayOdds: draftKingsDisplayOdds{American: "−115"}},
			{MarketID: "total", Label: "Under", Points: &total, DisplayOdds: draftKingsDisplayOdds{American: "−105"}},
			{MarketID: "prop", Label: "Player One", DisplayOdds: draftKingsDisplayOdds{American: "+140"}},
		},
	}

	games := normalizeDraftKings(payload)
	if len(games) != 1 {
		t.Fatalf("expected one game, got %d", len(games))
	}

	game := games[0]
	if game.Away.Name != "ATL Falcons" || game.Home.Name != "GB Packers" {
		t.Fatalf("unexpected teams: %s @ %s", game.Away.Name, game.Home.Name)
	}
	if game.Away.Spread == nil || game.Away.Spread.Line == nil || *game.Away.Spread.Line != 6 {
		t.Fatalf("unexpected away spread: %#v", game.Away.Spread)
	}
	if game.Home.Moneyline == nil || game.Home.Moneyline.American != "-278" {
		t.Fatalf("unexpected home moneyline: %#v", game.Home.Moneyline)
	}
	if game.Total.Over == nil || game.Total.Over.Line == nil || *game.Total.Over.Line != 43.5 {
		t.Fatalf("unexpected total: %#v", game.Total.Over)
	}
}

func TestNormalizeDraftKingsIncludesLiveGame(t *testing.T) {
	payload := draftKingsResponse{
		Events:  []draftKingsEvent{{ID: "live-nfl", Name: "ATL Falcons @ GB Packers", Status: "STARTED"}},
		Markets: []draftKingsMarket{{ID: "moneyline", EventID: "live-nfl", MarketType: draftKingsMarketType{Name: "Moneyline"}}},
		Selections: []draftKingsSelection{
			{ID: "away-selection", MarketID: "moneyline", Label: "ATL Falcons", DisplayOdds: draftKingsDisplayOdds{American: "+120"}},
			{ID: "home-selection", MarketID: "moneyline", Label: "GB Packers", DisplayOdds: draftKingsDisplayOdds{American: "-140"}},
		},
	}
	games := normalizeDraftKings(payload)
	if len(games) != 1 || games[0].Away.Moneyline == nil || games[0].Away.Moneyline.SelectionID != "away-selection" {
		t.Fatalf("live game was excluded from the REST snapshot: %#v", games)
	}
}

func TestRESTIndexesNonDisplayedSelectionsWithoutExposingThem(t *testing.T) {
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
	snapshot, err := cache.Get(context.Background())
	if err != nil || len(snapshot.Games) != 1 || !cache.HasSelection("displayed") {
		t.Fatalf("REST game line not indexed: snapshot=%#v err=%v", snapshot, err)
	}
	if !cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: "alternative"}) || cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: "displayed"}) || cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: "new"}) {
		t.Fatal("REST selection IDs were classified incorrectly")
	}
	if cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: "alternative", ReplacesID: "displayed"}) || cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: "alternative", ReplacesID: "new"}) {
		t.Fatal("an alternative with a primary or unknown replacement cannot be ignored")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(encoded), "alternative") {
		t.Fatalf("internal alternative ID leaked into the API: %s err=%v", encoded, err)
	}
}

func TestNormalizeDraftKingsPrefersTaggedPrimaryLinesOverLaterAlternatives(t *testing.T) {
	mainSpread := 2.5
	altSpread := 3.5
	mainTotal := 42.5
	altTotal := 44.5
	games := normalizeDraftKings(draftKingsResponse{
		Events: []draftKingsEvent{{ID: "game", Name: "Away @ Home"}},
		Markets: []draftKingsMarket{
			{ID: "main-spread", EventID: "game", MarketType: draftKingsMarketType{Name: "Spread"}, Tags: []string{"PrimaryMarket"}},
			{ID: "alt-spread", EventID: "game", MarketType: draftKingsMarketType{Name: "Spread"}},
			{ID: "total", EventID: "game", MarketType: draftKingsMarketType{Name: "Total"}, Tags: []string{"PrimaryMarket"}},
		},
		Selections: []draftKingsSelection{
			{ID: "main-spread-id", MarketID: "main-spread", Label: "Away", Points: &mainSpread, Tags: []string{"MainPointLine"}, DisplayOdds: draftKingsDisplayOdds{American: "-110"}},
			{ID: "main-total-id", MarketID: "total", Label: "Over", Points: &mainTotal, Tags: []string{"MainPointLine"}, DisplayOdds: draftKingsDisplayOdds{American: "-110"}},
			{ID: "alt-spread-id", MarketID: "alt-spread", Label: "Away", Points: &altSpread, DisplayOdds: draftKingsDisplayOdds{American: "-125"}},
			{ID: "alt-total-id", MarketID: "total", Label: "Over", Points: &altTotal, DisplayOdds: draftKingsDisplayOdds{American: "+100"}},
		},
	})
	if len(games) != 1 || games[0].Away.Spread == nil || *games[0].Away.Spread.Line != mainSpread || games[0].Total.Over == nil || *games[0].Total.Over.Line != mainTotal {
		t.Fatalf("an alternative overwrote the primary line: %#v", games)
	}
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) { return games, nil }), "NFL")
	if _, err := cache.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: "alt-spread-id"}) || !cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: "alt-total-id"}) {
		t.Fatal("filtered alternatives must still be recognized as non-displayed IDs")
	}
}

func TestNormalizeDraftKingsRejectsAmbiguousTaglessLines(t *testing.T) {
	main := 2.5
	alternate := 3.5
	games := normalizeDraftKings(draftKingsResponse{
		Events: []draftKingsEvent{{ID: "game", Name: "Away @ Home"}},
		Markets: []draftKingsMarket{
			{ID: "moneyline", EventID: "game", MarketType: draftKingsMarketType{Name: "Moneyline"}},
			{ID: "spread", EventID: "game", MarketType: draftKingsMarketType{Name: "Spread"}},
		},
		Selections: []draftKingsSelection{
			{ID: "moneyline-away", MarketID: "moneyline", Label: "Away", DisplayOdds: draftKingsDisplayOdds{American: "+100"}},
			{ID: "spread-main", MarketID: "spread", Label: "Away", Points: &main},
			{ID: "spread-alternate", MarketID: "spread", Label: "Away", Points: &alternate},
		},
	})
	if len(games) != 1 || games[0].Away.Moneyline == nil || games[0].Away.Spread != nil {
		t.Fatalf("ambiguous tagless spread was displayed: %#v", games)
	}
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) { return games, nil }), "NFL")
	if _, err := cache.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: "spread-main"}) || !cache.IsKnownAlternativeUpdate(liveSelectionUpdate{ID: "spread-alternate"}) {
		t.Fatal("ambiguous selections must remain indexed as non-displayed")
	}
}

func TestNormalizeDraftKingsRejectsAmbiguousTaglessMarkets(t *testing.T) {
	games := normalizeDraftKings(draftKingsResponse{
		Events: []draftKingsEvent{{ID: "game", Name: "Away @ Home"}},
		Markets: []draftKingsMarket{
			{ID: "moneyline", EventID: "game", MarketType: draftKingsMarketType{Name: "Moneyline"}},
			{ID: "spread-one", EventID: "game", MarketType: draftKingsMarketType{Name: "Spread"}},
			{ID: "spread-two", EventID: "game", MarketType: draftKingsMarketType{Name: "Spread"}},
		},
		Selections: []draftKingsSelection{
			{ID: "moneyline-away", MarketID: "moneyline", Label: "Away", DisplayOdds: draftKingsDisplayOdds{American: "+100"}},
			{ID: "spread-one-away", MarketID: "spread-one", Label: "Away"},
			{ID: "spread-two-away", MarketID: "spread-two", Label: "Away"},
		},
	})
	if len(games) != 1 || games[0].Away.Moneyline == nil || games[0].Away.Spread != nil {
		t.Fatalf("ambiguous tagless spread market was displayed: %#v", games)
	}
}

func TestNFLSpreadMarketNames(t *testing.T) {
	for _, name := range []string{"Spread", "Point Spread"} {
		market := draftKingsMarket{MarketType: draftKingsMarketType{Name: name}}
		if got := canonicalMarketName(market); got != "spread" {
			t.Errorf("%q mapped to %q, want spread", name, got)
		}
	}
}

func TestNormalizeDraftKingsKeepsSameKickoffGamesInStableOrder(t *testing.T) {
	start := time.Date(2026, time.September, 27, 17, 0, 0, 0, time.UTC)
	payload := draftKingsResponse{
		Events: []draftKingsEvent{
			{ID: "z", Name: "Zulu @ Home Z", StartEventDate: start},
			{ID: "a", Name: "Alpha @ Home A", StartEventDate: start},
			{ID: "e", Name: "Echo @ Home E", StartEventDate: start},
		},
		Markets: []draftKingsMarket{
			{ID: "market-z", EventID: "z", MarketType: draftKingsMarketType{Name: "Moneyline"}},
			{ID: "market-a", EventID: "a", MarketType: draftKingsMarketType{Name: "Moneyline"}},
			{ID: "market-e", EventID: "e", MarketType: draftKingsMarketType{Name: "Moneyline"}},
		},
		Selections: []draftKingsSelection{
			{MarketID: "market-z", Label: "Zulu", DisplayOdds: draftKingsDisplayOdds{American: "+100"}},
			{MarketID: "market-a", Label: "Alpha", DisplayOdds: draftKingsDisplayOdds{American: "+100"}},
			{MarketID: "market-e", Label: "Echo", DisplayOdds: draftKingsDisplayOdds{American: "+100"}},
		},
	}

	for range 20 {
		games := normalizeDraftKings(payload)
		if len(games) != 3 || games[0].ID != "a" || games[1].ID != "e" || games[2].ID != "z" {
			t.Fatalf("same-kickoff games changed order: %#v", games)
		}
	}
}

type providerFunc func(context.Context) ([]Game, error)

func (f providerFunc) Fetch(ctx context.Context) ([]Game, error) {
	return f(ctx)
}
