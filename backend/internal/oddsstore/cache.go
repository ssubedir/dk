package oddsstore

import (
	"context"
	"sync"
	"time"

	"github.com/ssubedir/dk/backend/internal/domain"
)

type Game = domain.Game
type Price = domain.Price
type OddsSnapshot = domain.OddsSnapshot
type MoveTiming = domain.MoveTiming
type liveSelectionUpdate = domain.SelectionUpdate

var ErrUnknownSelection = domain.ErrUnknownSelection

type selectionSlot struct {
	game int
	slot int
}

type OddsProvider = domain.Provider

type OddsCache struct {
	mu                sync.Mutex
	provider          OddsProvider
	league            string
	games             []Game
	fetched           time.Time
	updated           time.Time
	completed         time.Time
	fetchDuration     time.Duration
	inFlight          chan struct{}
	lastError         string
	lastFetchErr      error
	selectionIndex    map[string]selectionSlot
	knownAlternatives map[string]struct{}
	revision          uint64
	updateSource      string
	move              *MoveTiming
	moves             map[string]MoveTiming
}

func NewOddsCache(provider OddsProvider, league string) *OddsCache {
	return &OddsCache{provider: provider, league: league}
}

func (c *OddsCache) League() string { return c.league }

func (c *OddsCache) Get(ctx context.Context) (OddsSnapshot, error) {
	return c.get(ctx, false)
}

// Refresh explicitly checks the source. Get never refetches an existing snapshot.
func (c *OddsCache) Refresh(ctx context.Context) (OddsSnapshot, error) {
	return c.get(ctx, true)
}

func (c *OddsCache) get(ctx context.Context, force bool) (OddsSnapshot, error) {
	c.mu.Lock()
	if !force && !c.fetched.IsZero() {
		snapshot := c.snapshot(c.lastError != "", c.lastError)
		c.mu.Unlock()
		return snapshot, nil
	}
	if c.inFlight != nil {
		if !force && !c.fetched.IsZero() {
			snapshot := c.snapshot(c.lastError != "", c.lastError)
			c.mu.Unlock()
			return snapshot, nil
		}
		done := c.inFlight
		c.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return OddsSnapshot{}, ctx.Err()
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.fetched.IsZero() {
			return c.snapshot(c.lastError != "", c.lastError), nil
		}
		return OddsSnapshot{}, c.lastFetchErr
	}
	c.inFlight = make(chan struct{})
	startRevision := c.revision
	c.mu.Unlock()

	started := time.Now()
	games, err := c.provider.Fetch(ctx)
	completed := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastFetchErr = err
	if err != nil {
		c.lastError = err.Error()
		close(c.inFlight)
		c.inFlight = nil
		if !c.fetched.IsZero() {
			return c.snapshot(true, c.lastError), nil
		}
		return OddsSnapshot{}, err
	}
	if c.revision != startRevision && !c.fetched.IsZero() {
		// Keep live prices observed during the fetch, but still accept REST's
		// new games, markets, and selection-ID registry. Discarding the whole
		// response can starve structural recovery on a busy live slate.
		overlayInFlightLivePrices(games, c.games, c.moves, startRevision)
	}

	c.moves = reconcileRestMoves(c.games, games, c.moves, c.revision+1, completed)
	c.games = games
	c.selectionIndex = indexSelections(games)
	c.knownAlternatives = indexKnownAlternatives(games, c.selectionIndex)
	c.revision++
	c.fetched = completed.UTC()
	c.updated = completed.UTC()
	c.completed = completed
	c.fetchDuration = completed.Sub(started)
	c.updateSource = "rest"
	c.move = nil
	c.lastError = ""
	close(c.inFlight)
	c.inFlight = nil
	return c.snapshot(false, ""), nil
}

func (c *OddsCache) snapshot(stale bool, lastError string) OddsSnapshot {
	snapshot := OddsSnapshot{
		Source:          "DraftKings",
		League:          c.league,
		FetchedAt:       c.fetched,
		UpdatedAt:       c.updated,
		FetchDurationMs: c.fetchDuration.Milliseconds(),
		Stale:           stale,
		LastError:       lastError,
		Games:           append([]Game{}, c.games...),
		UpdateSource:    c.updateSource,
		Move:            c.move,
		Revision:        c.revision,
	}
	if len(c.moves) > 0 {
		snapshot.Moves = make(map[string]MoveTiming, len(c.moves))
		for key, move := range c.moves {
			snapshot.Moves[key] = move
		}
	}
	if c.updateSource == "rest" {
		snapshot.FetchCompleted = c.completed
	}
	return snapshot
}

func indexSelections(games []Game) map[string]selectionSlot {
	index := make(map[string]selectionSlot, len(games)*6)
	for gameIndex := range games {
		game := &games[gameIndex]
		prices := [...]*Price{game.Away.Moneyline, game.Home.Moneyline, game.Away.Spread, game.Home.Spread, game.Total.Over, game.Total.Under}
		for slot, price := range prices {
			if price != nil && price.SelectionID != "" {
				index[price.SelectionID] = selectionSlot{game: gameIndex, slot: slot}
			}
		}
	}
	return index
}

func indexKnownAlternatives(games []Game, primary map[string]selectionSlot) map[string]struct{} {
	alternatives := make(map[string]struct{})
	for _, game := range games {
		for _, id := range game.KnownSelectionIDs {
			if id == "" {
				continue
			}
			if _, displayed := primary[id]; !displayed {
				alternatives[id] = struct{}{}
			}
		}
	}
	return alternatives
}

func overlayInFlightLivePrices(fetched, current []Game, moves map[string]MoveTiming, startRevision uint64) {
	currentByID := make(map[string]*Game, len(current))
	for index := range current {
		currentByID[current[index].ID] = &current[index]
	}
	for index := range fetched {
		old := currentByID[fetched[index].ID]
		if old == nil {
			continue
		}
		oldPrices := [...]*Price{old.Away.Moneyline, old.Home.Moneyline, old.Away.Spread, old.Home.Spread, old.Total.Over, old.Total.Under}
		newPrices := [...]**Price{&fetched[index].Away.Moneyline, &fetched[index].Home.Moneyline, &fetched[index].Away.Spread, &fetched[index].Home.Spread, &fetched[index].Total.Over, &fetched[index].Total.Under}
		for slot, currentPrice := range oldPrices {
			move, changed := moves[selectionPriceKey(fetched[index].ID, slot)]
			if changed && move.Revision > startRevision && currentPrice != nil && *newPrices[slot] != nil &&
				(currentPrice.SelectionID == (*newPrices[slot]).SelectionID || move.ReplacedSelection) {
				*newPrices[slot] = currentPrice
			}
		}
	}
}

// ApplySelection changes a selection already joined to an event by REST.
func (c *OddsCache) ApplySelection(id, label string, price Price, receivedAt time.Time) (OddsSnapshot, bool, error) {
	return c.ApplyLiveSelection(liveSelectionUpdate{ID: id, Label: label, Price: price}, receivedAt)
}

// ApplyLiveSelection also handles a new main-line ID that names the old ID it replaces.
func (c *OddsCache) ApplyLiveSelection(update liveSelectionUpdate, receivedAt time.Time) (OddsSnapshot, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ref, ok := c.selectionIndex[update.ID]
	if !ok && update.ReplacesID != "" {
		ref, ok = c.selectionIndex[update.ReplacesID]
	}
	if !ok || ref.game >= len(c.games) {
		return OddsSnapshot{}, false, ErrUnknownSelection
	}
	if replaced, exists := c.selectionIndex[update.ReplacesID]; exists && replaced != ref {
		return OddsSnapshot{}, false, ErrUnknownSelection
	}
	game := c.games[ref.game]
	slots := [...]*Price{game.Away.Moneyline, game.Home.Moneyline, game.Away.Spread, game.Home.Spread, game.Total.Over, game.Total.Under}
	previous := slots[ref.slot]
	if previous == nil || (previous.SelectionID != update.ID && previous.SelectionID != update.ReplacesID) || (previous.SelectionLabel != "" && previous.SelectionLabel != update.Label) || (previous.Line == nil) != (update.Price.Line == nil) {
		return OddsSnapshot{}, false, ErrUnknownSelection
	}
	price := update.Price
	id := update.ID
	label := update.Label
	price.SelectionID = id
	price.SelectionLabel = label
	if previous.SelectionID == id && pricesEqual(previous, &price) {
		return c.snapshot(false, ""), false, nil
	}
	next := append([]Game(nil), c.games...)
	switch ref.slot {
	case 0:
		next[ref.game].Away.Moneyline = &price
	case 1:
		next[ref.game].Home.Moneyline = &price
	case 2:
		next[ref.game].Away.Spread = &price
	case 3:
		next[ref.game].Home.Spread = &price
	case 4:
		next[ref.game].Total.Over = &price
	case 5:
		next[ref.game].Total.Under = &price
	}
	c.games = next
	delete(c.selectionIndex, previous.SelectionID)
	c.selectionIndex[id] = ref
	if previous.SelectionID != id {
		c.knownAlternatives[previous.SelectionID] = struct{}{}
		delete(c.knownAlternatives, id)
	}
	c.updated = receivedAt.UTC()
	c.updateSource = "websocket"
	c.move = &MoveTiming{
		Revision:          c.revision + 1,
		GameID:            game.ID,
		Label:             label,
		Market:            [...]string{"Moneyline", "Moneyline", "Spread", "Spread", "Total over", "Total under"}[ref.slot],
		GoObservedAt:      receivedAt, // Retain Go's monotonic clock for SSE flush timing.
		ReplacedSelection: previous.SelectionID != id,
	}
	if !update.SourcePublishedAt.IsZero() {
		published := update.SourcePublishedAt.UTC()
		elapsed := receivedAt.Sub(published).Seconds() * 1000
		c.move.DraftKingsPublished = &published
		c.move.DraftKingsToGoMs = &elapsed
	}
	if c.moves == nil {
		c.moves = make(map[string]MoveTiming)
	}
	c.moves[selectionPriceKey(game.ID, ref.slot)] = *c.move
	c.lastError = ""
	c.revision++
	return c.snapshot(false, ""), true, nil
}

func selectionPriceKey(gameID string, slot int) string {
	return gameID + [...]string{":away:moneyline", ":home:moneyline", ":away:spread", ":home:spread", ":total:over", ":total:under"}[slot]
}

// A REST recovery can change displayed prices too. Time those changes from
// fetch completion while retaining earlier per-price timings across snapshots.
func reconcileRestMoves(previous, next []Game, previousMoves map[string]MoveTiming, revision uint64, observedAt time.Time) map[string]MoveTiming {
	before := make(map[string]Game, len(previous))
	for _, game := range previous {
		before[game.ID] = game
	}
	moves := make(map[string]MoveTiming)
	for _, game := range next {
		old, found := before[game.ID]
		if !found {
			continue
		}
		oldPrices := [...]*Price{old.Away.Moneyline, old.Home.Moneyline, old.Away.Spread, old.Home.Spread, old.Total.Over, old.Total.Under}
		newPrices := [...]*Price{game.Away.Moneyline, game.Home.Moneyline, game.Away.Spread, game.Home.Spread, game.Total.Over, game.Total.Under}
		for slot, price := range newPrices {
			if price == nil || oldPrices[slot] == nil {
				continue
			}
			key := selectionPriceKey(game.ID, slot)
			if pricesEqual(oldPrices[slot], price) {
				if move, found := previousMoves[key]; found {
					moves[key] = move
				}
				continue
			}
			label := game.Away.Name
			if slot == 1 || slot == 3 {
				label = game.Home.Name
			} else if slot == 4 {
				label = "Over"
			} else if slot == 5 {
				label = "Under"
			}
			moves[key] = MoveTiming{
				Revision:     revision,
				GameID:       game.ID,
				Label:        label,
				Market:       [...]string{"Moneyline", "Moneyline", "Spread", "Spread", "Total over", "Total under"}[slot],
				GoObservedAt: observedAt,
			}
		}
	}
	return moves
}

func (c *OddsCache) HasSelection(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.selectionIndex[id]
	return ok
}

// A selection-only WebSocket update has no market or event ID. Only a REST-
// recognized, non-displayed ID can safely be ignored without reconciliation.
func (c *OddsCache) IsKnownAlternativeUpdate(update liveSelectionUpdate) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, primary := c.selectionIndex[update.ID]; primary {
		return false
	}
	if _, known := c.knownAlternatives[update.ID]; !known {
		return false
	}
	if update.ReplacesID == "" {
		return true
	}
	_, known := c.knownAlternatives[update.ReplacesID]
	return known
}

func pricesEqual(left, right *Price) bool {
	if left.American != right.American || left.Decimal != right.Decimal {
		return false
	}
	if left.Line == nil || right.Line == nil {
		return left.Line == nil && right.Line == nil
	}
	return *left.Line == *right.Line
}
