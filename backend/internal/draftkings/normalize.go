package draftkings

import (
	"sort"
	"strings"
	"time"
)

type draftKingsResponse struct {
	Events     []draftKingsEvent     `json:"events"`
	Markets    []draftKingsMarket    `json:"markets"`
	Selections []draftKingsSelection `json:"selections"`
}

type draftKingsEvent struct {
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	StartEventDate time.Time               `json:"startEventDate"`
	Status         string                  `json:"status"`
	Participants   []draftKingsParticipant `json:"participants"`
}

type draftKingsParticipant struct {
	Name string `json:"name"`
}

type draftKingsMarket struct {
	ID         string               `json:"id"`
	EventID    string               `json:"eventId"`
	Name       string               `json:"name"`
	MarketType draftKingsMarketType `json:"marketType"`
	Tags       []string             `json:"tags"`
}

type draftKingsMarketType struct {
	Name string `json:"name"`
}

type draftKingsSelection struct {
	ID           string                  `json:"id"`
	MarketID     string                  `json:"marketId"`
	Label        string                  `json:"label"`
	Points       *float64                `json:"points"`
	DisplayOdds  draftKingsDisplayOdds   `json:"displayOdds"`
	TrueOdds     float64                 `json:"trueOdds"`
	Participants []draftKingsParticipant `json:"participants"`
	Tags         []string                `json:"tags"`
}

type draftKingsDisplayOdds struct {
	American   string `json:"american"`
	Decimal    string `json:"decimal"`
	Fractional string `json:"fractional"`
}

func normalizeDraftKings(payload draftKingsResponse) []Game {
	events := make(map[string]draftKingsEvent, len(payload.Events))
	games := make(map[string]*Game, len(payload.Events))
	for _, event := range payload.Events {
		away, home := eventTeams(event)
		if away == "" || home == "" {
			continue
		}
		events[event.ID] = event
		games[event.ID] = &Game{
			ID:       event.ID,
			StartsAt: event.StartEventDate,
			Away:     TeamOdds{Name: away},
			Home:     TeamOdds{Name: home},
		}
	}

	type marketKey struct{ eventID, kind string }
	markets := make(map[string]draftKingsMarket, len(payload.Markets))
	marketGroups := make(map[marketKey][]draftKingsMarket)
	for _, market := range payload.Markets {
		if _, ok := events[market.EventID]; !ok {
			continue
		}
		markets[market.ID] = market
		if kind := canonicalMarketName(market); kind != "" {
			key := marketKey{market.EventID, kind}
			marketGroups[key] = append(marketGroups[key], market)
		}
	}
	selectedMarkets := make(map[marketKey]string)
	for key, group := range marketGroups {
		var tagged []draftKingsMarket
		for _, market := range group {
			if hasDraftKingsTag(market.Tags, "PrimaryMarket") {
				tagged = append(tagged, market)
			}
		}
		switch {
		case len(tagged) == 1:
			selectedMarkets[key] = tagged[0].ID
		case len(tagged) == 0 && len(group) == 1:
			selectedMarkets[key] = group[0].ID
		}
	}
	mainPointLines := make(map[string]bool)
	type selectionKey struct{ eventID, kind, side string }
	candidates := make(map[selectionKey][]draftKingsSelection)
	for _, selection := range payload.Selections {
		if hasDraftKingsTag(selection.Tags, "MainPointLine") {
			mainPointLines[selection.MarketID] = true
		}
	}

	for _, selection := range payload.Selections {
		market, ok := markets[selection.MarketID]
		if !ok {
			continue
		}
		game := games[market.EventID]
		if selection.ID != "" {
			game.KnownSelectionIDs = append(game.KnownSelectionIDs, selection.ID)
		}
		kind := canonicalMarketName(market)
		if kind == "" || selectedMarkets[marketKey{market.EventID, kind}] != market.ID {
			continue
		}
		if (kind == "spread" || kind == "total") && mainPointLines[market.ID] && !hasDraftKingsTag(selection.Tags, "MainPointLine") {
			continue
		}
		side := selectionSide(game, kind, selectionLabel(selection))
		if side != "" {
			key := selectionKey{market.EventID, kind, side}
			candidates[key] = append(candidates[key], selection)
		}
	}
	for key, group := range candidates {
		// A tagless alternate line (or duplicate tagged line) must not win by
		// payload order. Leave that quote absent until the REST slate is clear.
		if len(group) != 1 {
			continue
		}
		game := games[key.eventID]
		price := selectionPrice(group[0])
		switch key.kind + ":" + key.side {
		case "moneyline:away":
			game.Away.Moneyline = price
		case "moneyline:home":
			game.Home.Moneyline = price
		case "spread:away":
			game.Away.Spread = price
		case "spread:home":
			game.Home.Spread = price
		case "total:over":
			game.Total.Over = price
		case "total:under":
			game.Total.Under = price
		}
	}

	result := make([]Game, 0, len(games))
	for _, game := range games {
		if game.Away.Moneyline == nil && game.Away.Spread == nil && game.Home.Moneyline == nil && game.Home.Spread == nil && game.Total.Over == nil && game.Total.Under == nil {
			continue
		}
		result = append(result, *game)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if !left.StartsAt.Equal(right.StartsAt) {
			return left.StartsAt.Before(right.StartsAt)
		}
		if left.Away.Name != right.Away.Name {
			return left.Away.Name < right.Away.Name
		}
		if left.Home.Name != right.Home.Name {
			return left.Home.Name < right.Home.Name
		}
		return left.ID < right.ID
	})
	return result
}

func hasDraftKingsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

func canonicalMarketName(market draftKingsMarket) string {
	name := market.MarketType.Name
	if name == "" {
		name = market.Name
	}
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "moneyline", "money line":
		return "moneyline"
	case "spread", "point spread", "run line", "puck line":
		return "spread"
	case "total", "game total", "total points":
		return "total"
	default:
		return ""
	}
}

func eventTeams(event draftKingsEvent) (string, string) {
	for _, separator := range []string{" @ ", " at ", " vs ", " vs. "} {
		if parts := strings.SplitN(event.Name, separator, 2); len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
	}
	if len(event.Participants) >= 2 {
		return event.Participants[0].Name, event.Participants[1].Name
	}
	return "", ""
}

func selectionLabel(selection draftKingsSelection) string {
	if strings.TrimSpace(selection.Label) != "" {
		return strings.TrimSpace(selection.Label)
	}
	if len(selection.Participants) > 0 {
		return strings.TrimSpace(selection.Participants[0].Name)
	}
	return ""
}

func selectionPrice(selection draftKingsSelection) *Price {
	return &Price{
		Line:           selection.Points,
		American:       strings.ReplaceAll(selection.DisplayOdds.American, "−", "-"),
		Decimal:        selection.DisplayOdds.Decimal,
		SelectionID:    selection.ID,
		SelectionLabel: selectionLabel(selection),
	}
}

func selectionSide(game *Game, kind, label string) string {
	normalized := strings.ToLower(strings.TrimSpace(label))
	if normalized == "" {
		return ""
	}
	if kind == "total" {
		switch {
		case normalized == "over" || strings.HasPrefix(normalized, "over "):
			return "over"
		case normalized == "under" || strings.HasPrefix(normalized, "under "):
			return "under"
		default:
			return ""
		}
	}
	away := strings.ToLower(game.Away.Name)
	home := strings.ToLower(game.Home.Name)
	if normalized == away {
		return "away"
	}
	if normalized == home {
		return "home"
	}
	awayMatch := strings.Contains(normalized, away) || strings.Contains(away, normalized)
	homeMatch := strings.Contains(normalized, home) || strings.Contains(home, normalized)
	if awayMatch && !homeMatch {
		return "away"
	}
	if homeMatch && !awayMatch {
		return "home"
	}
	return ""
}
