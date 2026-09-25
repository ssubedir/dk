package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ssubedir/dk/backend/internal/analytics"
	"github.com/ssubedir/dk/backend/internal/stream"
)

const summaryPushDelay = 200 * time.Millisecond

// Flush timings are sent as a second SSE event because the duration is not
// known until after the odds event has been flushed. They measure this server
// connection only, not network arrival or browser rendering.
type quoteFlushTiming struct {
	QuoteKey       string    `json:"quoteKey"`
	Revision       uint64    `json:"revision"`
	GoObservedAt   time.Time `json:"goObservedAt"`
	GoToSSEFlushMs *float64  `json:"goToSseFlushMs,omitempty"`
}

type flushTimingEvent struct {
	Replay  bool               `json:"replay"`
	Timings []quoteFlushTiming `json:"timings"`
}

func streamHandler(hub *stream.OddsHub, store *analytics.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming is unavailable", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("X-Accel-Buffering", "no")

		updates, latest, unsubscribe := hub.Subscribe()
		defer unsubscribe()
		var changes <-chan struct{}
		if store != nil {
			var unsubscribeChanges func()
			changes, unsubscribeChanges = store.SubscribeChanges()
			defer unsubscribeChanges()
		}

		if _, err := io.WriteString(w, ": connected\nretry: 2000\n\n"); err != nil {
			return
		}
		flusher.Flush()

		send := func(update stream.Update, replay bool) bool {
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", update.Event, update.Data); err != nil {
				return false
			}
			flusher.Flush()
			flushedAt := time.Now()
			if !replay && !update.FetchCompleted.IsZero() {
				// Flush has no delivery acknowledgement; this measures server-side time only.
				hub.RecordFlush(flushedAt.Sub(update.FetchCompleted))
			}
			if update.Event == "odds" && len(update.Moves) > 0 {
				payload := flushTimingEvent{Replay: replay, Timings: make([]quoteFlushTiming, 0, len(update.Moves))}
				for key, move := range update.Moves {
					if move.Revision == 0 || move.GoObservedAt.IsZero() {
						continue
					}
					entry := quoteFlushTiming{QuoteKey: key, Revision: move.Revision, GoObservedAt: move.GoObservedAt}
					if !replay {
						elapsed := flushedAt.Sub(move.GoObservedAt).Seconds() * 1000
						if elapsed < 0 {
							continue
						}
						entry.GoToSSEFlushMs = &elapsed
						if move.Revision == update.Revision && store != nil {
							store.RecordFlush(key, move.Revision, move.GoObservedAt, elapsed)
						}
					}
					payload.Timings = append(payload.Timings, entry)
				}
				if len(payload.Timings) > 0 {
					encoded, err := json.Marshal(payload)
					if err != nil {
						return false
					}
					if _, err := fmt.Fprintf(w, "event: flush-timing\ndata: %s\n\n", encoded); err != nil {
						return false
					}
					flusher.Flush()
				}
			}
			return true
		}
		sendSummary := func() bool {
			if store == nil {
				return true
			}
			summary, err := store.Summary(r.Context(), 24)
			if err != nil {
				if r.Context().Err() != nil {
					return false
				}
				return send(stream.Update{Event: "analysis-error", Data: []byte(`{}`)}, true)
			}
			data, err := json.Marshal(summary)
			if err != nil {
				return send(stream.Update{Event: "analysis-error", Data: []byte(`{}`)}, true)
			}
			return send(stream.Update{Event: "analysis-summary", Data: data}, true)
		}
		if latest != nil && !send(*latest, true) {
			return
		}
		if enabled, connected := hub.WebSocketStatus(); enabled {
			data, _ := json.Marshal(map[string]bool{"websocketConnected": connected})
			if !send(stream.Update{Event: "status", Data: data}, true) {
				return
			}
		}
		if !sendSummary() {
			return
		}

		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		var summaryTimer *time.Timer
		var summaryDue <-chan time.Time
		defer func() {
			if summaryTimer != nil {
				summaryTimer.Stop()
			}
		}()
		for {
			select {
			case <-r.Context().Done():
				return
			case update := <-updates:
				if !send(update, false) {
					return
				}
			case <-changes:
				if summaryDue == nil {
					summaryTimer = time.NewTimer(summaryPushDelay)
					summaryDue = summaryTimer.C
				}
			case <-summaryDue:
				summaryDue = nil
				// A single summary includes all changes currently retained.
				select {
				case <-changes:
				default:
				}
				if !sendSummary() {
					return
				}
			case <-heartbeat.C:
				if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
