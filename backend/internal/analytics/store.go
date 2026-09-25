package analytics

import (
	"context"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"
)

const DefaultCapacity = 50_000

// Observation is a displayed quote, not a raw upstream market. Only its first
// appearance and later line or payout changes are kept in memory.
type Observation struct {
	ObservedAt        time.Time
	SourcePublishedAt *time.Time
	League            string
	GameID            string
	StartsAt          time.Time
	AwayTeam          string
	HomeTeam          string
	QuoteKey          string
	Market            string
	Selection         string
	Line              *float64
	American          string
	Decimal           string
	UpdateSource      string
	GoObservedAt      time.Time
	SourceToGoMs      *float64
}

type HistoryStats struct {
	StoredObservations  int    `json:"storedObservations"`
	Capacity            int    `json:"capacity"`
	EvictedObservations uint64 `json:"evictedObservations"`
}

type Summary struct {
	Hours                   int          `json:"hours"`
	Observations            int64        `json:"observations"`
	PriceChanges            int64        `json:"priceChanges"`
	Games                   int64        `json:"games"`
	TimedObservations       int64        `json:"timedObservations"`
	ClockSkewedObservations int64        `json:"clockSkewedObservations"`
	SSEFlushSamples         int64        `json:"sseFlushSamples"`
	AverageGoToSSEFlush     *float64     `json:"averageGoToSseFlushMs,omitempty"`
	P50GoToSSEFlush         *float64     `json:"p50GoToSseFlushMs,omitempty"`
	P99GoToSSEFlush         *float64     `json:"p99GoToSseFlushMs,omitempty"`
	FirstObservedAt         *time.Time   `json:"firstObservedAt,omitempty"`
	LastObservedAt          *time.Time   `json:"lastObservedAt,omitempty"`
	AverageSourceToGo       *float64     `json:"averageSourceToGoMs,omitempty"`
	P50SourceToGo           *float64     `json:"p50SourceToGoMs,omitempty"`
	P99SourceToGo           *float64     `json:"p99SourceToGoMs,omitempty"`
	History                 HistoryStats `json:"history"`
}

type Move struct {
	ObservedAt        time.Time  `json:"observedAt"`
	SourcePublishedAt *time.Time `json:"sourcePublishedAt,omitempty"`
	GameID            string     `json:"gameId"`
	QuoteKey          string     `json:"quoteKey"`
	Market            string     `json:"market"`
	Selection         string     `json:"selection"`
	PreviousAmerican  *string    `json:"previousAmerican,omitempty"`
	American          string     `json:"american"`
	PreviousLine      *float64   `json:"previousLine,omitempty"`
	Line              *float64   `json:"line,omitempty"`
	UpdateSource      string     `json:"updateSource"`
	SourceToGoMs      *float64   `json:"sourceToGoMs,omitempty"`
}

// DumpObservation exposes one retained, normalized quote for diagnostics.
// It deliberately contains no upstream response bodies, headers, or secrets.
type DumpObservation struct {
	Sequence          uint64     `json:"sequence"`
	ObservedAt        time.Time  `json:"observedAt"`
	SourcePublishedAt *time.Time `json:"sourcePublishedAt,omitempty"`
	League            string     `json:"league"`
	GameID            string     `json:"gameId"`
	StartsAt          time.Time  `json:"startsAt"`
	AwayTeam          string     `json:"awayTeam"`
	HomeTeam          string     `json:"homeTeam"`
	QuoteKey          string     `json:"quoteKey"`
	Market            string     `json:"market"`
	Selection         string     `json:"selection"`
	PreviousLine      *float64   `json:"previousLine,omitempty"`
	Line              *float64   `json:"line,omitempty"`
	PreviousAmerican  *string    `json:"previousAmerican,omitempty"`
	American          string     `json:"american"`
	Decimal           string     `json:"decimal"`
	UpdateSource      string     `json:"updateSource"`
	GoObservedAt      time.Time  `json:"goObservedAt"`
	SourceToGoMs      *float64   `json:"sourceToGoMs,omitempty"`
	IsBaseline        bool       `json:"isBaseline"`
}

type DumpPage struct {
	Observations []DumpObservation `json:"observations"`
	NextBefore   uint64            `json:"nextBefore,omitempty"`
	History      HistoryStats      `json:"history"`
}

type storedObservation struct {
	Observation
	sequence         uint64
	key              string
	isBaseline       bool
	previousAmerican *string
	previousLine     *float64
}

type latestQuote struct {
	signature string
	american  string
	line      *float64
	sequence  uint64
}

type flushKey struct {
	quoteKey string
	revision uint64
}

type flushSample struct {
	key        flushKey
	observedAt time.Time
	durationMs float64
}

// Store keeps a bounded per-process history. The oldest quote is evicted when
// capacity is reached; no files, native libraries, or background writer exist.
type Store struct {
	mu           sync.RWMutex
	rows         []storedObservation
	head         int
	capacity     int
	nextSeq      uint64
	evicted      uint64
	latest       map[string]latestQuote
	flushSamples []flushSample
	flushHead    int
	flushSeen    map[flushKey]struct{}
	listeners    map[chan struct{}]struct{}
}

func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Store{capacity: capacity, latest: make(map[string]latestQuote), flushSeen: make(map[flushKey]struct{}), listeners: make(map[chan struct{}]struct{})}
}

// SubscribeChanges signals retained quote or flush-timing changes. Signals are
// coalesced; callers read a fresh summary instead of consuming individual rows.
func (s *Store) SubscribeChanges() (<-chan struct{}, func()) {
	changes := make(chan struct{}, 1)
	s.mu.Lock()
	if s.listeners == nil {
		s.listeners = make(map[chan struct{}]struct{})
	}
	s.listeners[changes] = struct{}{}
	s.mu.Unlock()
	return changes, func() {
		s.mu.Lock()
		delete(s.listeners, changes)
		s.mu.Unlock()
	}
}

func (s *Store) notifyLocked() {
	for listener := range s.listeners {
		select {
		case listener <- struct{}{}:
		default:
		}
	}
}

// RecordFlush keeps the first successful live SSE flush for each changed quote.
// It is independent of quote-history recording, which intentionally happens
// after fan-out and may race with the stream goroutine.
func (s *Store) RecordFlush(quoteKey string, revision uint64, observedAt time.Time, durationMs float64) {
	if s == nil || quoteKey == "" || revision == 0 || observedAt.IsZero() ||
		durationMs < 0 || math.IsNaN(durationMs) || math.IsInf(durationMs, 0) {
		return
	}
	key := flushKey{quoteKey: quoteKey, revision: revision}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.flushSeen[key]; exists {
		return
	}
	sample := flushSample{key: key, observedAt: observedAt, durationMs: durationMs}
	if len(s.flushSamples) < s.capacity {
		s.flushSamples = append(s.flushSamples, sample)
	} else {
		delete(s.flushSeen, s.flushSamples[s.flushHead].key)
		s.flushSamples[s.flushHead] = sample
		s.flushHead = (s.flushHead + 1) % s.capacity
	}
	s.flushSeen[key] = struct{}{}
	s.notifyLocked()
}

func copyFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func priceSignature(row Observation) string {
	line := "nil"
	if row.Line != nil {
		line = strconv.FormatFloat(*row.Line, 'g', -1, 64)
	}
	return line + "|" + row.American + "|" + row.Decimal
}

// Record runs after SSE fan-out. Unchanged prices are ignored, so the work for
// a live WebSocket move is one map lookup and one bounded append.
func (s *Store) Record(rows []Observation) {
	if s == nil || len(rows) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, row := range rows {
		key := row.League + ":" + row.QuoteKey
		signature := priceSignature(row)
		previous, existed := s.latest[key]
		if existed && previous.signature == signature {
			continue
		}
		s.nextSeq++
		changed = true
		row.Line = copyFloat(row.Line)
		row.SourcePublishedAt = copyTime(row.SourcePublishedAt)
		row.SourceToGoMs = copyFloat(row.SourceToGoMs)
		entry := storedObservation{
			Observation: row,
			sequence:    s.nextSeq,
			key:         key,
			isBaseline:  !existed,
		}
		if existed {
			entry.previousAmerican = &previous.american
			entry.previousLine = copyFloat(previous.line)
		}
		if len(s.rows) < s.capacity {
			s.rows = append(s.rows, entry)
		} else {
			evicted := s.rows[s.head]
			if latest, ok := s.latest[evicted.key]; ok && latest.sequence == evicted.sequence {
				delete(s.latest, evicted.key)
			}
			s.rows[s.head] = entry
			s.head = (s.head + 1) % s.capacity
			s.evicted++
		}
		s.latest[key] = latestQuote{signature: signature, american: row.American,
			line: copyFloat(row.Line), sequence: entry.sequence}
	}
	if changed {
		s.notifyLocked()
	}
}

func (s *Store) statsLocked() HistoryStats {
	return HistoryStats{
		StoredObservations:  len(s.rows),
		Capacity:            s.capacity,
		EvictedObservations: s.evicted,
	}
}

func (s *Store) Stats() HistoryStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statsLocked()
}

func percentile(sorted []float64, p float64) float64 {
	position := p * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	return sorted[lower] + (sorted[upper]-sorted[lower])*(position-float64(lower))
}

func (s *Store) Summary(ctx context.Context, hours int) (Summary, error) {
	result := Summary{Hours: hours}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	seenGames := make(map[[2]string]struct{})
	var timings []float64
	var flushTimings []float64
	s.mu.RLock()
	result.History = s.statsLocked()
	for _, row := range s.rows {
		if err := ctx.Err(); err != nil {
			s.mu.RUnlock()
			return Summary{}, err
		}
		if row.ObservedAt.Before(cutoff) {
			continue
		}
		result.Observations++
		if !row.isBaseline {
			result.PriceChanges++
		}
		seenGames[[2]string{row.League, row.GameID}] = struct{}{}
		if result.FirstObservedAt == nil || row.ObservedAt.Before(*result.FirstObservedAt) {
			observed := row.ObservedAt
			result.FirstObservedAt = &observed
		}
		if result.LastObservedAt == nil || row.ObservedAt.After(*result.LastObservedAt) {
			observed := row.ObservedAt
			result.LastObservedAt = &observed
		}
		if row.SourceToGoMs != nil && *row.SourceToGoMs >= 0 && *row.SourceToGoMs <= 60_000 {
			timings = append(timings, *row.SourceToGoMs)
		} else if row.SourceToGoMs != nil && *row.SourceToGoMs < 0 {
			result.ClockSkewedObservations++
		}
	}
	for _, sample := range s.flushSamples {
		if err := ctx.Err(); err != nil {
			s.mu.RUnlock()
			return Summary{}, err
		}
		if !sample.observedAt.Before(cutoff) {
			flushTimings = append(flushTimings, sample.durationMs)
		}
	}
	s.mu.RUnlock()
	result.Games = int64(len(seenGames))
	result.TimedObservations = int64(len(timings))
	result.SSEFlushSamples = int64(len(flushTimings))
	if len(flushTimings) > 0 {
		sort.Float64s(flushTimings)
		var total float64
		for _, timing := range flushTimings {
			total += timing
		}
		average := total / float64(len(flushTimings))
		p50 := percentile(flushTimings, 0.5)
		p99 := percentile(flushTimings, 0.99)
		result.AverageGoToSSEFlush = &average
		result.P50GoToSSEFlush = &p50
		result.P99GoToSSEFlush = &p99
	}
	if len(timings) > 0 {
		sort.Float64s(timings)
		var total float64
		for _, timing := range timings {
			total += timing
		}
		average := total / float64(len(timings))
		p50 := percentile(timings, 0.5)
		p99 := percentile(timings, 0.99)
		result.AverageSourceToGo = &average
		result.P50SourceToGo = &p50
		result.P99SourceToGo = &p99
	}
	return result, nil
}

func (s *Store) Moves(ctx context.Context, gameID string, limit int) ([]Move, error) {
	type sequencedMove struct {
		Move
		sequence uint64
	}
	var matching []sequencedMove
	s.mu.RLock()
	for _, row := range s.rows {
		if err := ctx.Err(); err != nil {
			s.mu.RUnlock()
			return nil, err
		}
		if row.GameID != gameID || row.isBaseline {
			continue
		}
		matching = append(matching, sequencedMove{Move: Move{
			ObservedAt: row.ObservedAt, SourcePublishedAt: copyTime(row.SourcePublishedAt),
			GameID: row.GameID, QuoteKey: row.QuoteKey, Market: row.Market,
			Selection: row.Selection, PreviousAmerican: copyString(row.previousAmerican),
			American: row.American, PreviousLine: copyFloat(row.previousLine),
			Line: copyFloat(row.Line), UpdateSource: row.UpdateSource,
			SourceToGoMs: copyFloat(row.SourceToGoMs),
		}, sequence: row.sequence})
	}
	s.mu.RUnlock()
	sort.Slice(matching, func(i, j int) bool {
		if matching[i].ObservedAt.Equal(matching[j].ObservedAt) {
			return matching[i].sequence > matching[j].sequence
		}
		return matching[i].ObservedAt.After(matching[j].ObservedAt)
	})
	if len(matching) > limit {
		matching = matching[:limit]
	}
	moves := make([]Move, len(matching))
	for i, move := range matching {
		moves[i] = move.Move
	}
	return moves, nil
}

// Dump reads the retained ring from newest to oldest. A before cursor is an
// exclusive sequence number; later observations do not renumber earlier ones,
// though old observations can still be evicted between requests.
func (s *Store) Dump(ctx context.Context, before uint64, gameID string, limit int) (DumpPage, error) {
	page := DumpPage{Observations: make([]DumpObservation, 0, limit)}
	s.mu.RLock()
	defer s.mu.RUnlock()
	page.History = s.statsLocked()
	count := len(s.rows)
	for logical := count - 1; logical >= 0; logical-- {
		if err := ctx.Err(); err != nil {
			return DumpPage{}, err
		}
		row := s.rows[(s.head+logical)%count]
		if (before != 0 && row.sequence >= before) || (gameID != "" && row.GameID != gameID) {
			continue
		}
		if len(page.Observations) == limit {
			page.NextBefore = page.Observations[len(page.Observations)-1].Sequence
			break
		}
		page.Observations = append(page.Observations, DumpObservation{
			Sequence: row.sequence, ObservedAt: row.ObservedAt,
			SourcePublishedAt: copyTime(row.SourcePublishedAt),
			League:            row.League, GameID: row.GameID, StartsAt: row.StartsAt,
			AwayTeam: row.AwayTeam, HomeTeam: row.HomeTeam, QuoteKey: row.QuoteKey,
			Market: row.Market, Selection: row.Selection,
			PreviousLine: copyFloat(row.previousLine), Line: copyFloat(row.Line),
			PreviousAmerican: copyString(row.previousAmerican), American: row.American,
			Decimal: row.Decimal, UpdateSource: row.UpdateSource,
			GoObservedAt: row.GoObservedAt, SourceToGoMs: copyFloat(row.SourceToGoMs),
			IsBaseline: row.isBaseline,
		})
	}
	return page, nil
}
