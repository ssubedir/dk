package oddsstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ssubedir/dk/backend/internal/domain"
	"github.com/ssubedir/dk/backend/internal/stream"
)

type TeamOdds = domain.TeamOdds
type TotalOdds = domain.TotalOdds

var NewOddsHub = stream.NewOddsHub

type providerFunc func(context.Context) ([]Game, error)

func (provider providerFunc) Fetch(ctx context.Context) ([]Game, error) { return provider(ctx) }

func TestOddsCacheReturnsLastKnownGoodData(t *testing.T) {
	calls := 0
	provider := providerFunc(func(context.Context) ([]Game, error) {
		calls++
		if calls == 1 {
			time.Sleep(10 * time.Millisecond)
			return []Game{{ID: "event-1"}}, nil
		}
		return nil, errors.New("upstream unavailable")
	})

	cache := NewOddsCache(provider, "NFL")
	first, err := cache.Get(context.Background())
	if err != nil || first.Stale {
		t.Fatalf("unexpected first result: snapshot=%#v err=%v", first, err)
	}
	if first.FetchDurationMs < 10 {
		t.Fatalf("expected successful fetch duration, got %dms", first.FetchDurationMs)
	}

	// Ordinary reads never refetch. Only an explicit recovery refresh can fail.
	if cached, err := cache.Get(context.Background()); err != nil || cached.Stale || calls != 1 {
		t.Fatalf("cached read unexpectedly fetched: snapshot=%#v err=%v calls=%d", cached, err, calls)
	}
	if _, err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("expected last-known-good snapshot after failed refresh: %v", err)
	}
	second, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("expected cached response, got %v", err)
	}
	if !second.Stale || second.LastError == "" || len(second.Games) != 1 {
		t.Fatalf("expected stale last-known-good data, got %#v", second)
	}
	if second.FetchDurationMs != first.FetchDurationMs {
		t.Fatalf("stale result changed last successful fetch duration: %dms to %dms", first.FetchDurationMs, second.FetchDurationMs)
	}
}

func TestOddsCacheReadsCachedSnapshotWhileRefreshIsInFlight(t *testing.T) {
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	defer func() {
		select {
		case <-releaseRefresh:
		default:
			close(releaseRefresh)
		}
	}()
	calls := 0
	provider := providerFunc(func(context.Context) ([]Game, error) {
		calls++
		if calls == 2 {
			close(refreshStarted)
			<-releaseRefresh
		}
		return []Game{{ID: "event-1"}}, nil
	})
	cache := NewOddsCache(provider, "NFL")
	if _, err := cache.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	refreshDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		_, _ = cache.Refresh(context.Background())
	}()
	<-refreshStarted

	readDone := make(chan OddsSnapshot, 1)
	go func() {
		snapshot, _ := cache.Get(context.Background())
		readDone <- snapshot
	}()
	select {
	case snapshot := <-readDone:
		if len(snapshot.Games) != 1 || snapshot.Games[0].ID != "event-1" {
			t.Fatalf("expected cached game during refresh, got %#v", snapshot)
		}
	case <-time.After(time.Second):
		t.Fatal("cached read waited for the upstream refresh")
	}
	close(releaseRefresh)
	<-refreshDone
}

func TestOddsCachePersistsStaleStateAfterFailedRefresh(t *testing.T) {
	calls := 0
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		calls++
		if calls == 1 {
			return []Game{{ID: "event-1"}}, nil
		}
		return nil, errors.New("upstream unavailable")
	}), "NFL")
	if _, err := cache.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := cache.Get(context.Background())
	if err != nil || !snapshot.Stale || snapshot.LastError == "" || calls != 2 {
		t.Fatalf("cached failure state was lost: snapshot=%#v err=%v calls=%d", snapshot, err, calls)
	}
}

func TestFailedRecoveryRemainsStaleWhenLivePriceArrivesDuringFetch(t *testing.T) {
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
			return nil, errors.New("upstream unavailable")
		}
		return []Game{{ID: "game", Away: TeamOdds{Name: "Away", Moneyline: &Price{American: "-110", SelectionID: "away-id", SelectionLabel: "Away"}}}}, nil
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
	if _, changed, err := cache.ApplySelection("away-id", "Away", Price{American: "-115"}, time.Now()); err != nil || !changed {
		t.Fatalf("live price update: changed=%t err=%v", changed, err)
	}
	close(release)
	snapshot := <-refreshed
	if !snapshot.Stale || snapshot.LastError != "upstream unavailable" || snapshot.Games[0].Away.Moneyline.American != "-115" {
		t.Fatalf("failed recovery hid the error or lost the live quote: %#v", snapshot)
	}
}

func TestOddsCacheUsesConfiguredLeague(t *testing.T) {
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		return []Game{{ID: "event-1"}}, nil
	}), "NFL")
	snapshot, err := cache.Get(context.Background())
	if err != nil || snapshot.League != "NFL" {
		t.Fatalf("expected NFL snapshot, got snapshot=%#v err=%v", snapshot, err)
	}
}

func TestRestRecoveryTimesNFLUnderTotalChange(t *testing.T) {
	calls := 0
	cache := NewOddsCache(providerFunc(func(context.Context) ([]Game, error) {
		calls++
		line := 6.5
		american := "-150"
		if calls > 1 {
			line = 5.5
			american = "+105"
		}
		return []Game{{ID: "nfl-game", Home: TeamOdds{Name: "GB Packers"}, Total: TotalOdds{Under: &Price{Line: &line, American: american}}}}, nil
	}), "NFL")
	initial, err := cache.Refresh(context.Background())
	if err != nil || len(initial.Moves) != 0 {
		t.Fatalf("initial snapshot should not claim a price move: %#v err=%v", initial.Moves, err)
	}
	hub := NewOddsHub(cache, time.Second)
	changed, err := hub.RefreshSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	move, ok := changed.Moves["nfl-game:total:under"]
	if !ok || changed.UpdateSource != "rest" || changed.Move != nil || move.Revision != changed.Revision || move.Market != "Total under" || move.GoObservedAt.IsZero() {
		t.Fatalf("REST under-total change lacks timing: %#v", changed)
	}
}
