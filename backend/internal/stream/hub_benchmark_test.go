package stream

import (
	"strconv"
	"testing"
	"time"

	"github.com/ssubedir/dk/backend/internal/analytics"
	"github.com/ssubedir/dk/backend/internal/domain"
)

type Game = domain.Game
type Price = domain.Price
type TeamOdds = domain.TeamOdds
type TotalOdds = domain.TotalOdds
type OddsSnapshot = domain.OddsSnapshot
type MoveTiming = domain.MoveTiming

type discardAnalytics struct{}

func (discardAnalytics) Record([]analytics.Observation)                 {}
func (discardAnalytics) RecordFlush(string, uint64, time.Time, float64) {}

func BenchmarkHubPublishWithAnalytics(b *testing.B) {
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
	snapshot.Move = &MoveTiming{Revision: 1, GameID: "game-0"}
	snapshot.Moves = map[string]MoveTiming{
		"game-0:away:moneyline": {Revision: 1, GameID: "game-0", GoObservedAt: now},
	}
	for _, test := range []struct {
		name     string
		recorder Recorder
	}{
		{name: "SSE_only"},
		{name: "SSE_plus_analytics_conversion", recorder: discardAnalytics{}},
	} {
		b.Run(test.name, func(b *testing.B) {
			hub := NewOddsHub(nil, time.Second)
			hub.SetRecorder(test.recorder)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				hub.PublishSnapshot(snapshot)
			}
		})
	}
}
