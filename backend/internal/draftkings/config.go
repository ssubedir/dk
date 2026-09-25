package draftkings

import (
	"fmt"
	"net/url"
	"strings"
)

const draftKingsBaseURL = "https://sportsbook-nash.draftkings.com/sites/CA-ON-SB/api/sportscontent/controldata/league/leagueSubcategory/v1/markets"

type DraftKingsConfig struct {
	LeagueName    string
	LeagueID      string
	SubcategoryID string
	TemplateVars  string // Optional comma-separated template variables; defaults to LeagueID.
	PageURL       string
	APIURL        string // Optional full endpoint override.
}

func DefaultConfig() DraftKingsConfig {
	return DraftKingsConfig{
		LeagueName:    "NFL",
		LeagueID:      "88808",
		SubcategoryID: "4518",
		PageURL:       "https://sportsbook.draftkings.com/leagues/football/nfl",
	}
}

func (config DraftKingsConfig) Validate() error {
	if strings.TrimSpace(config.LeagueName) == "" {
		return fmt.Errorf("DRAFTKINGS_LEAGUE_NAME must not be empty")
	}
	if !digitsOnly(config.LeagueID) || !digitsOnly(config.SubcategoryID) {
		return fmt.Errorf("DRAFTKINGS_LEAGUE_ID and DRAFTKINGS_SUBCATEGORY_ID must be numeric")
	}
	if config.TemplateVars != "" {
		for _, value := range strings.Split(config.TemplateVars, ",") {
			if !digitsOnly(value) {
				return fmt.Errorf("DRAFTKINGS_TEMPLATE_VARS must be comma-separated numeric IDs")
			}
		}
	}
	page, err := url.Parse(config.PageURL)
	if err != nil || page.Scheme != "https" || page.Hostname() != "sportsbook.draftkings.com" {
		return fmt.Errorf("DRAFTKINGS_PAGE_URL must be an https://sportsbook.draftkings.com URL")
	}
	if config.APIURL != "" {
		endpoint, err := url.Parse(config.APIURL)
		if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.Host == "" {
			return fmt.Errorf("DRAFTKINGS_API_URL must be an absolute HTTP URL")
		}
	}
	return nil
}

func digitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func (config DraftKingsConfig) EndpointURL() string {
	if config.APIURL != "" {
		return config.APIURL
	}
	query := url.Values{}
	query.Set("isBatchable", "false")
	templateVars := config.TemplateVars
	if templateVars == "" {
		templateVars = config.LeagueID
	}
	query.Set("templateVars", templateVars)
	query.Set("eventsQuery", fmt.Sprintf("$filter=leagueId eq '%s' AND clientMetadata/Subcategories/any(s: s/Id eq '%s')", config.LeagueID, config.SubcategoryID))
	query.Set("marketsQuery", fmt.Sprintf("$filter=clientMetadata/subCategoryId eq '%s' AND tags/all(t: t ne 'SportcastBetBuilder')", config.SubcategoryID))
	query.Set("include", "Events")
	query.Set("entity", "events")
	return draftKingsBaseURL + "?" + query.Encode()
}
