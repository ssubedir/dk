package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ssubedir/dk/backend/internal/draftkings"
)

type Config struct {
	Port             string
	DraftKings       draftkings.DraftKingsConfig
	RefreshInterval  time.Duration
	WebSocketEnabled bool
	FrontendDist     string
	FrontendOrigin   string
}

func Load() (Config, error) {
	result := Config{
		Port:           envOrDefault("PORT", "8080"),
		DraftKings:     DraftKingsFromEnv(),
		FrontendDist:   strings.TrimSpace(os.Getenv("FRONTEND_DIST")),
		FrontendOrigin: strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")),
	}
	interval, err := time.ParseDuration(envOrDefault("ODDS_REFRESH_INTERVAL", "5s"))
	if err != nil || interval < time.Second {
		return Config{}, fmt.Errorf("ODDS_REFRESH_INTERVAL must be at least 1s (for example 5s)")
	}
	result.RefreshInterval = interval
	result.WebSocketEnabled, err = strconv.ParseBool(envOrDefault("DRAFTKINGS_WS_ENABLED", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("DRAFTKINGS_WS_ENABLED must be true or false")
	}
	if err := result.DraftKings.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func DraftKingsFromEnv() draftkings.DraftKingsConfig {
	result := draftkings.DefaultConfig()
	result.LeagueName = envOrDefault("DRAFTKINGS_LEAGUE_NAME", result.LeagueName)
	result.LeagueID = envOrDefault("DRAFTKINGS_LEAGUE_ID", result.LeagueID)
	result.SubcategoryID = envOrDefault("DRAFTKINGS_SUBCATEGORY_ID", result.SubcategoryID)
	result.TemplateVars = strings.TrimSpace(os.Getenv("DRAFTKINGS_TEMPLATE_VARS"))
	result.PageURL = envOrDefault("DRAFTKINGS_PAGE_URL", result.PageURL)
	result.APIURL = strings.TrimSpace(os.Getenv("DRAFTKINGS_API_URL"))
	return result
}

func Summary(config Config) []string {
	apiURL := "generated from league and subcategory IDs"
	if config.DraftKings.APIURL != "" {
		apiURL = safeURLHost(config.DraftKings.APIURL) + " (override; path and query hidden)"
	}
	frontendOrigin := "unset"
	if config.FrontendOrigin != "" {
		frontendOrigin = safeURLHost(config.FrontendOrigin)
	}
	return []string{
		fmt.Sprintf("PORT=%q", config.Port),
		fmt.Sprintf("DRAFTKINGS_LEAGUE_NAME=%q", config.DraftKings.LeagueName),
		fmt.Sprintf("DRAFTKINGS_LEAGUE_ID=%q", config.DraftKings.LeagueID),
		fmt.Sprintf("DRAFTKINGS_SUBCATEGORY_ID=%q", config.DraftKings.SubcategoryID),
		fmt.Sprintf("DRAFTKINGS_TEMPLATE_VARS=%q", config.DraftKings.TemplateVars),
		fmt.Sprintf("DRAFTKINGS_PAGE_URL=%q", safePageURL(config.DraftKings.PageURL)),
		fmt.Sprintf("DRAFTKINGS_API_URL=%q", apiURL),
		fmt.Sprintf("ODDS_REFRESH_INTERVAL=%q (REST-only cadence and recovery resync gap; WebSocket reconciliation uses a separate one-minute timer)", config.RefreshInterval.String()),
		fmt.Sprintf("FRONTEND_ORIGIN=%q", frontendOrigin),
	}
}

func safeURLHost(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "invalid URL"
	}
	return parsed.Scheme + "://" + parsed.Host
}

func safePageURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "invalid URL"
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
