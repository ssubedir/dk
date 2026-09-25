package analytics

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func testObservation(now time.Time, key, american string) Observation {
	return Observation{
		ObservedAt: now, League: "NFL", GameID: "game-1", StartsAt: now.Add(time.Hour),
		AwayTeam: "Away", HomeTeam: "Home", QuoteKey: key, Market: "moneyline",
		Selection: "Away", American: american, Decimal: "1.91",
		UpdateSource: "websocket", GoObservedAt: now,
	}
}

func TestStoreSignalsOnlyNewAnalyticsData(t *testing.T) {
	store := NewStore(10)
	changes, unsubscribe := store.SubscribeChanges()
	defer unsubscribe()
	now := time.Now()
	row := testObservation(now, "game-1:away:moneyline", "-110")
	store.Record([]Observation{row})
	select {
	case <-changes:
	default:
		t.Fatal("new quote did not signal a summary change")
	}
	store.Record([]Observation{row})
	select {
	case <-changes:
		t.Fatal("unchanged quote signaled a summary change")
	default:
	}
	store.RecordFlush(row.QuoteKey, 1, now, 0.5)
	select {
	case <-changes:
	default:
		t.Fatal("new flush timing did not signal a summary change")
	}
	store.RecordFlush(row.QuoteKey, 1, now, 0.5)
	select {
	case <-changes:
		t.Fatal("duplicate flush timing signaled a summary change")
	default:
	}
}

func TestStoreFansOutChangesToSubscribers(t *testing.T) {
	store := NewStore(10)
	first, unsubscribeFirst := store.SubscribeChanges()
	second, unsubscribeSecond := store.SubscribeChanges()
	defer unsubscribeSecond()
	row := testObservation(time.Now(), "game-1:away:moneyline", "-110")
	store.Record([]Observation{row})
	for _, listener := range []<-chan struct{}{first, second} {
		select {
		case <-listener:
		default:
			t.Fatal("one subscriber missed a summary change")
		}
	}
	unsubscribeFirst()
	row.American = "-115"
	store.Record([]Observation{row})
	select {
	case <-first:
		t.Fatal("unsubscribed listener received another change")
	default:
	}
	select {
	case <-second:
	default:
		t.Fatal("remaining listener missed a change")
	}
}

func TestPriceSignatureKeepsLineAndPayoutDistinct(t *testing.T) {
	line := 2.5
	base := Observation{Line: &line, American: "-110", Decimal: "1.91"}
	if priceSignature(base) == priceSignature(Observation{American: "-110", Decimal: "1.91"}) {
		t.Fatal("a missing line must not equal a spread line")
	}
	if priceSignature(base) == priceSignature(Observation{Line: &line, American: "-105", Decimal: "1.91"}) {
		t.Fatal("an American price move was ignored")
	}
}

func TestSummaryAndMoves(t *testing.T) {
	store := NewStore(10)
	now := time.Now().UTC()
	key := "game-1:away:moneyline"
	baseline := testObservation(now.Add(-time.Second), key, "-110")
	first := testObservation(now, key, "-115")
	second := testObservation(now.Add(time.Millisecond), key, "-120")
	firstLatency, secondLatency := 10.0, 30.0
	first.SourceToGoMs = &firstLatency
	second.SourceToGoMs = &secondLatency
	store.Record([]Observation{baseline, first, second, second})
	summary, err := store.Summary(context.Background(), 24)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Observations != 3 || summary.PriceChanges != 2 || summary.Games != 1 ||
		summary.TimedObservations != 2 || summary.AverageSourceToGo == nil ||
		*summary.AverageSourceToGo != 20 || summary.P50SourceToGo == nil ||
		*summary.P50SourceToGo != 20 || summary.P99SourceToGo == nil ||
		*summary.P99SourceToGo != 29.8 {
		t.Fatalf("unexpected in-memory summary: %#v", summary)
	}
	if summary.FirstObservedAt == nil || !summary.FirstObservedAt.Equal(baseline.ObservedAt) ||
		summary.LastObservedAt == nil || !summary.LastObservedAt.Equal(second.ObservedAt) {
		t.Fatalf("unexpected observed time range: %#v", summary)
	}
	moves, err := store.Moves(context.Background(), "game-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 1 || moves[0].PreviousAmerican == nil ||
		*moves[0].PreviousAmerican != "-115" || moves[0].American != "-120" {
		t.Fatalf("unexpected move history: %#v", moves)
	}
}

func TestHistoryEvictsOldestAndBoundsDeduplication(t *testing.T) {
	store := NewStore(2)
	now := time.Now().UTC()
	for _, key := range []string{"a", "b", "c", "a"} {
		store.Record([]Observation{testObservation(now, key, "-110")})
	}
	if stats := store.Stats(); stats.StoredObservations != 2 || stats.Capacity != 2 || stats.EvictedObservations != 2 {
		t.Fatalf("unexpected bounded history: %#v", stats)
	}
	summary, err := store.Summary(context.Background(), 24)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Observations != 2 || summary.PriceChanges != 0 {
		t.Fatalf("evicted quote should become a new baseline: %#v", summary)
	}
	store.Record([]Observation{testObservation(now.Add(time.Millisecond), "a", "-120")})
	moves, err := store.Moves(context.Background(), "game-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 1 || moves[0].PreviousAmerican == nil || *moves[0].PreviousAmerican != "-110" {
		t.Fatalf("move lost its previous quote after eviction: %#v", moves)
	}
}

func TestSummaryFiltersOldAndClockSkewedSamples(t *testing.T) {
	store := NewStore(10)
	now := time.Now().UTC()
	old := testObservation(now.Add(-48*time.Hour), "old", "-110")
	negative := testObservation(now, "negative", "-110")
	large := testObservation(now, "large", "-110")
	valid := testObservation(now, "valid", "-110")
	negativeMs, largeMs, validMs := -1.0, 60_001.0, 5.0
	negative.SourceToGoMs = &negativeMs
	large.SourceToGoMs = &largeMs
	valid.SourceToGoMs = &validMs
	store.Record([]Observation{old, negative, large, valid})
	summary, err := store.Summary(context.Background(), 24)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Observations != 3 || summary.TimedObservations != 1 || summary.ClockSkewedObservations != 1 ||
		summary.P99SourceToGo == nil || *summary.P99SourceToGo != 5 {
		t.Fatalf("time window or clock-skew filter failed: %#v", summary)
	}
}

func TestFlushSummaryUsesSameClockAndDeduplicatesQuoteRevisions(t *testing.T) {
	store := NewStore(3)
	now := time.Now()
	store.RecordFlush("old", 1, now.Add(-48*time.Hour), 100)
	store.RecordFlush("game:away:spread", 2, now, 10)
	store.RecordFlush("game:away:spread", 2, now, 900) // Another client does not double-count.
	store.RecordFlush("game:home:spread", 2, now, 30)
	store.RecordFlush("game:home:spread", 3, now, -1)
	summary, err := store.Summary(context.Background(), 24)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SSEFlushSamples != 2 || summary.AverageGoToSSEFlush == nil || *summary.AverageGoToSSEFlush != 20 ||
		summary.P50GoToSSEFlush == nil || *summary.P50GoToSSEFlush != 20 ||
		summary.P99GoToSSEFlush == nil || *summary.P99GoToSSEFlush != 29.8 {
		t.Fatalf("unexpected flush summary: %#v", summary)
	}
	store.RecordFlush("game:total:over", 3, now, 40)
	store.RecordFlush("game:total:under", 3, now, 50) // Evicts the old key.
	store.RecordFlush("old", 1, now, 60)              // A key can be recorded again after eviction.
	if len(store.flushSamples) != 3 || len(store.flushSeen) != 3 {
		t.Fatalf("flush history did not stay bounded: samples=%d seen=%d", len(store.flushSamples), len(store.flushSeen))
	}
}

func TestDumpPaginatesRetainedObservationsNewestFirst(t *testing.T) {
	store := NewStore(3)
	now := time.Now().UTC()
	rows := []Observation{
		testObservation(now, "quote-a", "-110"),
		testObservation(now.Add(time.Millisecond), "quote-a", "-115"),
		testObservation(now.Add(2*time.Millisecond), "quote-b", "+100"),
		testObservation(now.Add(3*time.Millisecond), "quote-a", "-120"),
		testObservation(now.Add(4*time.Millisecond), "quote-c", "+110"),
	}
	rows[2].GameID = "game-2"
	rows[4].GameID = "game-2"
	store.Record(rows)

	first, err := store.Dump(context.Background(), 0, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Observations) != 2 || first.Observations[0].Sequence != 5 ||
		first.Observations[1].Sequence != 4 || first.NextBefore != 4 ||
		first.History.StoredObservations != 3 || first.History.EvictedObservations != 2 {
		t.Fatalf("unexpected first dump page: %#v", first)
	}
	changed := first.Observations[1]
	if changed.IsBaseline || changed.PreviousAmerican == nil || *changed.PreviousAmerican != "-115" ||
		changed.American != "-120" {
		t.Fatalf("dump lost previous price: %#v", changed)
	}

	second, err := store.Dump(context.Background(), first.NextBefore, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Observations) != 1 || second.Observations[0].Sequence != 3 ||
		second.NextBefore != 0 || !second.Observations[0].IsBaseline {
		t.Fatalf("unexpected second dump page: %#v", second)
	}
	filtered, err := store.Dump(context.Background(), 0, "game-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Observations) != 1 || filtered.Observations[0].Sequence != 4 || filtered.NextBefore != 0 {
		t.Fatalf("unexpected game-filtered dump: %#v", filtered)
	}
}

func BenchmarkStoreRecord(b *testing.B) {
	store := NewStore(10_000)
	now := time.Now().UTC()
	key := "game-1:away:moneyline"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		store.Record([]Observation{testObservation(now, key, strconv.Itoa(i))})
	}
}

func BenchmarkStoreSummary(b *testing.B) {
	store := NewStore(10_000)
	now := time.Now().UTC()
	for i := 0; i < 10_000; i++ {
		store.Record([]Observation{testObservation(now,
			"game-1:quote-"+strconv.Itoa(i%64), strconv.Itoa(i))})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Summary(context.Background(), 24); err != nil {
			b.Fatal(err)
		}
	}
}
