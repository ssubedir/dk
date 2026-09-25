package analytics

import (
	"strconv"
	"testing"
	"time"

	"github.com/ssubedir/dk/backend/internal/domain"
)

type Game = domain.Game
type Price = domain.Price
type TeamOdds = domain.TeamOdds
type TotalOdds = domain.TotalOdds
type OddsSnapshot = domain.OddsSnapshot
type MoveTiming = domain.MoveTiming

func TestSnapshotObservationsIncludeDisplayedQuotesAndTiming(t *testing.T) {
	now := time.Now().UTC()
	line := 2.5
	sourceToGo := 12.5
	published := now.Add(-10 * time.Millisecond)
	key := "game:away:spread"
	snapshot := OddsSnapshot{
		League: "NFL", UpdatedAt: now, UpdateSource: "websocket",
		Games: []Game{{
			ID: "game", StartsAt: now.Add(time.Hour),
			Away: TeamOdds{Name: "Away", Spread: &Price{Line: &line, American: "-110"}},
			Home: TeamOdds{Name: "Home", Moneyline: &Price{American: "+120"}},
		}},
		Moves: map[string]MoveTiming{key: {
			Revision: 1, GoObservedAt: now, DraftKingsPublished: &published, DraftKingsToGoMs: &sourceToGo,
		}},
		Move: &MoveTiming{Revision: 1, GameID: "game"},
	}
	rows := SnapshotObservations(snapshot)
	if len(rows) != 1 || rows[0].QuoteKey != key || rows[0].SourceToGoMs == nil ||
		*rows[0].SourceToGoMs != sourceToGo || rows[0].SourcePublishedAt == nil {
		t.Fatalf("displayed quote conversion lost values or timing: %#v", rows)
	}
	snapshot.UpdateSource = "rest"
	if rows := SnapshotObservations(snapshot); len(rows) != 2 {
		t.Fatalf("REST snapshot should include both displayed quotes: %#v", rows)
	}
	snapshot.Stale = true
	if rows := SnapshotObservations(snapshot); len(rows) != 0 {
		t.Fatalf("stale replay should not record new analytical rows: %#v", rows)
	}
}

func BenchmarkSnapshotObservations(b *testing.B) {
	now := time.Now().UTC()
	price := &Price{American: "-110", Decimal: "1.91"}
	snapshot := OddsSnapshot{League: "NFL", UpdatedAt: now, UpdateSource: "websocket"}
	for i := 0; i < 100; i++ {
		snapshot.Games = append(snapshot.Games, Game{
			ID: "game-" + strconv.Itoa(i), StartsAt: now.Add(time.Hour),
			Away:  TeamOdds{Name: "Away", Spread: price, Moneyline: price},
			Home:  TeamOdds{Name: "Home", Spread: price, Moneyline: price},
			Total: TotalOdds{Over: price, Under: price},
		})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if rows := SnapshotObservations(snapshot); len(rows) != 600 {
			b.Fatalf("got %d rows", len(rows))
		}
	}
}
