package httpapi

import (
	"net/http"

	"github.com/ssubedir/dk/backend/internal/analytics"
	"github.com/ssubedir/dk/backend/internal/oddsstore"
	"github.com/ssubedir/dk/backend/internal/stream"
)

func NewHandler(cache *oddsstore.OddsCache, hub *stream.OddsHub, stores ...*analytics.Store) http.Handler {
	mux := http.NewServeMux()
	var store *analytics.Store
	if len(stores) > 0 {
		store = stores[0]
	}
	mux.HandleFunc("GET /api/odds", oddsHandler(cache))
	mux.HandleFunc("POST /api/refresh", refreshHandler(hub))
	mux.HandleFunc("GET /api/stream", streamHandler(hub, store))
	mux.HandleFunc("GET /api/metrics", metricsHandler(hub))
	registerAnalysisRoutes(mux, store)
	mux.HandleFunc("GET /healthz", healthHandler)
	return mux
}
