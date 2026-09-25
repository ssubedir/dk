package config

import (
	"strings"
	"testing"
	"time"

	"github.com/ssubedir/dk/backend/internal/draftkings"
)

func TestDraftKingsConfigFromEnv(t *testing.T) {
	t.Setenv("DRAFTKINGS_LEAGUE_NAME", "NFL")
	t.Setenv("DRAFTKINGS_LEAGUE_ID", "88808")
	t.Setenv("DRAFTKINGS_SUBCATEGORY_ID", "4518")
	t.Setenv("DRAFTKINGS_TEMPLATE_VARS", "88808,4518")
	t.Setenv("DRAFTKINGS_PAGE_URL", "https://sportsbook.draftkings.com/leagues/football/nfl")
	t.Setenv("DRAFTKINGS_API_URL", "")

	config := DraftKingsFromEnv()
	if config.LeagueName != "NFL" || config.LeagueID != "88808" || config.SubcategoryID != "4518" || config.TemplateVars != "88808,4518" {
		t.Fatalf("environment did not configure league: %#v", config)
	}
	if config.APIURL != "" || config.PageURL != "https://sportsbook.draftkings.com/leagues/football/nfl" {
		t.Fatalf("environment did not configure endpoint headers: %#v", config)
	}
}

func TestStartupConfigSummaryShowsEffectiveValuesWithoutURLSecrets(t *testing.T) {
	config := draftkings.DraftKingsConfig{
		LeagueName:    "NFL",
		LeagueID:      "88808",
		SubcategoryID: "4518",
		TemplateVars:  "88808",
		PageURL:       "https://user:password@sportsbook.draftkings.com/leagues/football/nfl?token=secret",
		APIURL:        "https://user:password@example.com/feed?token=secret",
	}
	summary := strings.Join(Summary(Config{DraftKings: config, Port: "8080", RefreshInterval: 5 * time.Second, FrontendOrigin: "https://frontend.example.com"}), "\n")
	for _, expected := range []string{
		`PORT="8080"`,
		`DRAFTKINGS_LEAGUE_NAME="NFL"`,
		`DRAFTKINGS_LEAGUE_ID="88808"`,
		`DRAFTKINGS_SUBCATEGORY_ID="4518"`,
		`DRAFTKINGS_TEMPLATE_VARS="88808"`,
		`DRAFTKINGS_PAGE_URL="https://sportsbook.draftkings.com/leagues/football/nfl"`,
		`DRAFTKINGS_API_URL="https://example.com (override; path and query hidden)"`,
		`ODDS_REFRESH_INTERVAL="5s"`,
		`FRONTEND_ORIGIN="https://frontend.example.com"`,
	} {
		if !strings.Contains(summary, expected) {
			t.Errorf("startup summary missing %s: %s", expected, summary)
		}
	}
	if strings.Contains(summary, "password") || strings.Contains(summary, "secret") {
		t.Fatalf("startup summary leaked URL credentials or query: %s", summary)
	}
}

func TestStartupConfigSummaryMarksGeneratedEndpoint(t *testing.T) {
	summary := strings.Join(Summary(Config{DraftKings: draftkings.DefaultConfig(), Port: "8080", RefreshInterval: 5 * time.Second}), "\n")
	if !strings.Contains(summary, `DRAFTKINGS_API_URL="generated from league and subcategory IDs"`) {
		t.Fatalf("expected generated endpoint label, got %s", summary)
	}
}
