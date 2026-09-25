package httpapi

import (
	"net/http"
	"strconv"

	"github.com/ssubedir/dk/backend/internal/analytics"
)

func registerAnalysisRoutes(mux *http.ServeMux, store *analytics.Store) {
	mux.HandleFunc("GET /api/analysis/summary", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "In-memory analytics are unavailable on this backend"})
			return
		}
		hours := 24
		if value := r.URL.Query().Get("hours"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || parsed > 168 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hours must be between 1 and 168"})
				return
			}
			hours = parsed
		}
		summary, err := store.Summary(r.Context(), hours)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Analytics summary failed", "details": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, summary)
	})
	mux.HandleFunc("GET /api/analysis/moves", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "In-memory analytics are unavailable on this backend"})
			return
		}
		gameID := r.URL.Query().Get("gameId")
		if gameID == "" || len(gameID) > 128 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "gameId is required (maximum 128 characters)"})
			return
		}
		limit := 100
		if value := r.URL.Query().Get("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || parsed > 500 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be between 1 and 500"})
				return
			}
			limit = parsed
		}
		moves, err := store.Moves(r.Context(), gameID, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Analytics moves query failed", "details": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, moves)
	})
	mux.HandleFunc("GET /api/analysis/dump", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "In-memory analytics are unavailable on this backend"})
			return
		}
		query := r.URL.Query()
		gameID := query.Get("gameId")
		if len(gameID) > 128 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "gameId must be at most 128 characters"})
			return
		}
		limit := 100
		if value := query.Get("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || parsed > 1000 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be between 1 and 1000"})
				return
			}
			limit = parsed
		}
		var before uint64
		if value := query.Get("before"); value != "" {
			parsed, err := strconv.ParseUint(value, 10, 64)
			if err != nil || parsed == 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "before must be a positive sequence number"})
				return
			}
			before = parsed
		}
		page, err := store.Dump(r.Context(), before, gameID, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Analytics dump failed", "details": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
}
