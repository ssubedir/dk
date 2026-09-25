package analytics

import (
	"time"

	"github.com/ssubedir/dk/backend/internal/domain"
)

// snapshotObservations converts only the six displayed game-line quotes into
// sportsbook-neutral analytical rows. The in-memory store deduplicates unchanged rows.
func SnapshotObservations(snapshot domain.OddsSnapshot) []Observation {
	if snapshot.Stale {
		return nil
	}
	liveMove := snapshot.UpdateSource == "websocket" && snapshot.Move != nil
	capacity := len(snapshot.Games) * 6
	if liveMove {
		capacity = 1
	}
	rows := make([]Observation, 0, capacity)
	for _, game := range snapshot.Games {
		if liveMove && game.ID != snapshot.Move.GameID {
			continue
		}
		add := func(key, market, selection string, price *domain.Price) {
			if price == nil {
				return
			}
			if liveMove {
				move, ok := snapshot.Moves[key]
				if !ok || move.Revision != snapshot.Move.Revision {
					return
				}
			}
			row := Observation{
				ObservedAt:   snapshot.UpdatedAt,
				League:       snapshot.League,
				GameID:       game.ID,
				StartsAt:     game.StartsAt,
				AwayTeam:     game.Away.Name,
				HomeTeam:     game.Home.Name,
				QuoteKey:     key,
				Market:       market,
				Selection:    selection,
				Line:         price.Line,
				American:     price.American,
				Decimal:      price.Decimal,
				UpdateSource: snapshot.UpdateSource,
				GoObservedAt: snapshot.UpdatedAt,
			}
			if move, ok := snapshot.Moves[key]; ok {
				row.GoObservedAt = move.GoObservedAt
				row.SourcePublishedAt = move.DraftKingsPublished
				row.SourceToGoMs = move.DraftKingsToGoMs
			}
			if row.GoObservedAt.IsZero() {
				row.GoObservedAt = time.Now()
			}
			rows = append(rows, row)
		}
		prefix := game.ID + ":"
		add(prefix+"away:spread", "spread", game.Away.Name, game.Away.Spread)
		add(prefix+"home:spread", "spread", game.Home.Name, game.Home.Spread)
		add(prefix+"total:over", "total", "Over", game.Total.Over)
		add(prefix+"total:under", "total", "Under", game.Total.Under)
		add(prefix+"away:moneyline", "moneyline", game.Away.Name, game.Away.Moneyline)
		add(prefix+"home:moneyline", "moneyline", game.Home.Name, game.Home.Moneyline)
	}
	return rows
}
