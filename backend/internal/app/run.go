package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ssubedir/dk/backend/internal/analytics"
	"github.com/ssubedir/dk/backend/internal/config"
	"github.com/ssubedir/dk/backend/internal/draftkings"
	"github.com/ssubedir/dk/backend/internal/httpapi"
	"github.com/ssubedir/dk/backend/internal/oddsstore"
	"github.com/ssubedir/dk/backend/internal/stream"
)

// Run is the composition root. Source adapters do not import the cache,
// stream, or HTTP packages; their integration lives here.
func Run() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	provider, err := draftkings.NewDraftKingsProvider(settings.DraftKings, &http.Client{Timeout: 12 * time.Second})
	if err != nil {
		return err
	}
	cache := oddsstore.NewOddsCache(provider, settings.DraftKings.LeagueName)
	hub := stream.NewOddsHub(cache, settings.RefreshInterval)
	store := analytics.NewStore(analytics.DefaultCapacity)
	hub.SetRecorder(store)
	if settings.WebSocketEnabled {
		hub.EnableWebSocket()
	}
	handler := httpapi.NewHandler(cache, hub, store)
	if settings.FrontendDist != "" {
		handler, err = httpapi.WithStaticSite(handler, settings.FrontendDist)
		if err != nil {
			return fmt.Errorf("FRONTEND_DIST: %w", err)
		}
	}
	for _, setting := range config.Summary(settings) {
		log.Printf("config %s", setting)
	}
	log.Printf("config DRAFTKINGS_WS_ENABLED=%t", settings.WebSocketEnabled)
	log.Printf("config FRONTEND_DIST=%q", settings.FrontendDist)
	log.Printf("config ANALYTICS_HISTORY_CAPACITY=%d (in-memory observations)", analytics.DefaultCapacity)
	if settings.WebSocketEnabled {
		log.Printf("REST mode: initial snapshot, recovery, and reconciliation every %s while WebSocket connected", connectedReconcileInterval)
		go func() {
			ctx := context.Background()
			if hub.RunBootstrap(ctx) {
				newLiveFeed(settings.DraftKings, settings.RefreshInterval, cache, hub).Run(ctx)
			}
		}()
	} else {
		log.Printf("REST mode: polling every %s (WebSocket disabled)", settings.RefreshInterval)
		go hub.Run(context.Background())
	}

	server := &http.Server{
		Addr:              ":" + settings.Port,
		Handler:           httpapi.WithRequestLogging(httpapi.WithCORS(handler, settings.FrontendOrigin)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      0, // SSE responses stay open; each client disconnects via its request context.
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("DraftKings %s odds server listening on http://localhost:%s", settings.DraftKings.LeagueName, settings.Port)
	return server.ListenAndServe()
}
