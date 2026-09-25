package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssubedir/dk/backend/internal/domain"
	"github.com/ssubedir/dk/backend/internal/draftkings"
	"github.com/ssubedir/dk/backend/internal/oddsstore"
	"github.com/ssubedir/dk/backend/internal/stream"
)

type providerFunc func(context.Context) ([]domain.Game, error)

func (provider providerFunc) Fetch(ctx context.Context) ([]domain.Game, error) { return provider(ctx) }

func liveTestFeed(t *testing.T, fetch providerFunc) (*liveFeed, *oddsstore.OddsCache, *stream.OddsHub) {
	t.Helper()
	cache := oddsstore.NewOddsCache(fetch, "NFL")
	if _, err := cache.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	hub := stream.NewOddsHub(cache, time.Second)
	hub.EnableWebSocket()
	return newLiveFeed(draftkings.DefaultConfig(), time.Second, cache, hub), cache, hub
}

func TestLiveFeedAppliesKnownSelectionAndPublishes(t *testing.T) {
	feed, cache, hub := liveTestFeed(t, func(context.Context) ([]domain.Game, error) {
		return []domain.Game{{ID: "game", Away: domain.TeamOdds{Name: "Away", Moneyline: &domain.Price{American: "+100", SelectionID: "away-id", SelectionLabel: "Away"}}}}, nil
	})
	updates, _, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	feed.SetConnected(true)
	if !hub.Metrics().WebSocketConnected {
		t.Fatal("connection status was not published")
	}
	feed.OnSelection(domain.SelectionUpdate{ID: "away-id", Label: "Away", Price: domain.Price{American: "+110"}}, time.Now())
	snapshot, err := cache.Get(context.Background())
	if err != nil || snapshot.Games[0].Away.Moneyline.American != "+110" || snapshot.UpdateSource != "websocket" {
		t.Fatalf("live update not applied: snapshot=%#v err=%v", snapshot, err)
	}
	if hub.Metrics().WebSocketUpdates != 1 {
		t.Fatalf("live update not counted: %#v", hub.Metrics())
	}
	select {
	case update := <-updates:
		if update.Event != "odds" {
			t.Fatalf("expected odds publication, got %q", update.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("live update was not published")
	}
}

func TestLiveFeedQueuesRecoveryForUnknownAndRemovedSelections(t *testing.T) {
	feed, _, _ := liveTestFeed(t, func(context.Context) ([]domain.Game, error) {
		return []domain.Game{{ID: "game", Away: domain.TeamOdds{Name: "Away", Moneyline: &domain.Price{American: "+100", SelectionID: "away-id", SelectionLabel: "Away"}}}}, nil
	})
	feed.OnSelection(domain.SelectionUpdate{ID: "unknown", Label: "Away", Price: domain.Price{American: "+110"}}, time.Now())
	select {
	case <-feed.resync:
	default:
		t.Fatal("unknown selection did not request REST recovery")
	}
	feed.OnSelectionRemoved("other")
	select {
	case <-feed.resync:
		t.Fatal("unrelated removal requested recovery")
	default:
	}
	feed.OnSelectionRemoved("away-id")
	select {
	case <-feed.resync:
	default:
		t.Fatal("displayed selection removal did not request recovery")
	}
}

func TestLiveFeedRecoveryRefreshesWithoutSocketReadLoop(t *testing.T) {
	var calls atomic.Int32
	feed, _, hub := liveTestFeed(t, func(context.Context) ([]domain.Game, error) {
		calls.Add(1)
		return []domain.Game{{ID: "game"}}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go feed.runRecovery(ctx)
	feed.RequestResync()
	deadline := time.After(2 * time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("recovery did not issue another REST fetch")
		case <-time.After(time.Millisecond):
		}
	}
	if hub.Metrics().WebSocketResyncs != 1 {
		t.Fatalf("recovery count mismatch: %#v", hub.Metrics())
	}
}

func TestLiveFeedReconcilesOnlyWhileConnected(t *testing.T) {
	feed, _, _ := liveTestFeed(t, func(context.Context) ([]domain.Game, error) {
		return []domain.Game{}, nil
	})
	feed.reconcileIfConnected()
	select {
	case <-feed.resync:
		t.Fatal("disconnected socket requested reconciliation")
	default:
	}
	feed.SetConnected(true)
	feed.reconcileIfConnected()
	select {
	case <-feed.resync:
	default:
		t.Fatal("connected socket did not request reconciliation")
	}
	feed.SetConnected(false)
	feed.reconcileIfConnected()
	select {
	case <-feed.resync:
		t.Fatal("disconnected socket requested another reconciliation")
	default:
	}
}
