package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"
)

func TestNFLDotenvProfileMatchesSuppliedDraftKingsURL(t *testing.T) {
	profiles := []struct {
		file, name, leagueID, subcategoryID, templateVars, pageURL string
	}{
		{".env.nfl", "NFL", "88808", "4518", "88808", "https://sportsbook.draftkings.com/leagues/football/nfl"},
	}
	for _, profile := range profiles {
		t.Run(profile.name, func(t *testing.T) {
			for _, key := range []string{
				"DRAFTKINGS_LEAGUE_NAME", "DRAFTKINGS_LEAGUE_ID", "DRAFTKINGS_SUBCATEGORY_ID",
				"DRAFTKINGS_TEMPLATE_VARS", "DRAFTKINGS_PAGE_URL", "DRAFTKINGS_API_URL",
				"DRAFTKINGS_WS_ENABLED", "ODDS_REFRESH_INTERVAL",
			} {
				t.Setenv(key, "")
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			if err := godotenv.Load(filepath.Join("..", "..", profile.file)); err != nil {
				t.Fatal(err)
			}
			config := DraftKingsFromEnv()
			if err := config.Validate(); err != nil {
				t.Fatal(err)
			}
			if config.LeagueName != profile.name || config.LeagueID != profile.leagueID || config.SubcategoryID != profile.subcategoryID || config.TemplateVars != profile.templateVars || config.PageURL != profile.pageURL || config.APIURL != "" {
				t.Fatalf("unexpected %s profile: %#v", profile.name, config)
			}
			endpoint, err := url.Parse(config.EndpointURL())
			if err != nil {
				t.Fatal(err)
			}
			query := endpoint.Query()
			if query.Get("templateVars") != profile.templateVars || query.Get("include") != "Events" || query.Get("entity") != "events" || query.Get("isBatchable") != "false" {
				t.Fatalf("unexpected %s URL parameters: %v", profile.name, query)
			}
			wantEvents := fmt.Sprintf("$filter=leagueId eq '%s' AND clientMetadata/Subcategories/any(s: s/Id eq '%s')", profile.leagueID, profile.subcategoryID)
			wantMarkets := fmt.Sprintf("$filter=clientMetadata/subCategoryId eq '%s' AND tags/all(t: t ne 'SportcastBetBuilder')", profile.subcategoryID)
			if query.Get("eventsQuery") != wantEvents || query.Get("marketsQuery") != wantMarkets {
				t.Fatalf("unexpected %s filters: %v", profile.name, query)
			}
			if os.Getenv("DRAFTKINGS_WS_ENABLED") != "true" || os.Getenv("ODDS_REFRESH_INTERVAL") != "30s" {
				t.Fatalf("%s profile did not enable WebSocket and recovery spacing", profile.name)
			}
		})
	}
}
