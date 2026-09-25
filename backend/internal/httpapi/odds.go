package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ssubedir/dk/backend/internal/oddsstore"
	"github.com/ssubedir/dk/backend/internal/stream"
)

func oddsHandler(cache *oddsstore.OddsCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		snapshot, err := cache.Get(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error":   "DraftKings odds are temporarily unavailable",
				"details": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	}
}

func refreshHandler(hub *stream.OddsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if wait := hub.ReserveManualRefresh(); wait > 0 {
			seconds := int((wait + time.Second - 1) / time.Second)
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":             "Manual refresh is temporarily limited; live updates continue",
				"retryAfterSeconds": seconds,
			})
			return
		}
		snapshot, err := hub.RefreshSnapshot(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error":   "DraftKings odds are temporarily unavailable",
				"details": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	}
}

func metricsHandler(hub *stream.OddsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, hub.Metrics())
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
