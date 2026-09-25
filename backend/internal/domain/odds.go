package domain

import (
	"context"
	"errors"
	"time"
)

type Price struct {
	Line           *float64 `json:"line,omitempty"`
	American       string   `json:"american"`
	Decimal        string   `json:"decimal,omitempty"`
	SelectionID    string   `json:"-"`
	SelectionLabel string   `json:"-"`
}

type TeamOdds struct {
	Name      string `json:"name"`
	Spread    *Price `json:"spread,omitempty"`
	Moneyline *Price `json:"moneyline,omitempty"`
}

type TotalOdds struct {
	Over  *Price `json:"over,omitempty"`
	Under *Price `json:"under,omitempty"`
}

type Game struct {
	ID       string    `json:"id"`
	StartsAt time.Time `json:"startsAt"`
	Away     TeamOdds  `json:"away"`
	Home     TeamOdds  `json:"home"`
	Total    TotalOdds `json:"total"`
	// REST knows more selections than the six quotes displayed by the page.
	KnownSelectionIDs []string `json:"-"`
}

type OddsSnapshot struct {
	Source          string                `json:"source"`
	League          string                `json:"league"`
	FetchedAt       time.Time             `json:"fetchedAt"`
	UpdatedAt       time.Time             `json:"updatedAt"`
	FetchDurationMs int64                 `json:"fetchDurationMs"`
	Stale           bool                  `json:"stale"`
	LastError       string                `json:"lastError,omitempty"`
	Games           []Game                `json:"games"`
	UpdateSource    string                `json:"updateSource"`
	Move            *MoveTiming           `json:"move,omitempty"`
	Moves           map[string]MoveTiming `json:"moves,omitempty"`
	FetchCompleted  time.Time             `json:"-"`
	Revision        uint64                `json:"-"`
}

// MoveTiming describes one displayed quote change. The DraftKings timestamp
// exists for WebSocket updates; GoObservedAt uses this process's clock and is
// set at fetch completion for REST recovery changes.
type MoveTiming struct {
	Revision            uint64     `json:"revision"`
	GameID              string     `json:"gameId"`
	Label               string     `json:"label"`
	Market              string     `json:"market"`
	DraftKingsPublished *time.Time `json:"draftKingsPublishedAt,omitempty"`
	GoObservedAt        time.Time  `json:"goObservedAt"`
	DraftKingsToGoMs    *float64   `json:"draftKingsToGoMs,omitempty"`
	ReplacedSelection   bool       `json:"-"`
}

var ErrUnknownSelection = errors.New("selection is not in the current REST snapshot")

// Provider supplies a normalized snapshot without exposing a sportsbook's wire format.
type Provider interface {
	Fetch(context.Context) ([]Game, error)
}

// SelectionUpdate is a sportsbook-neutral live quote delta. Source adapters
// decode their transport and pass only these fields to the cache.
type SelectionUpdate struct {
	ID                string
	ReplacesID        string
	Label             string
	Price             Price
	SourcePublishedAt time.Time
}
