package draftkings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/ssubedir/dk/backend/internal/domain"
)

type Game = domain.Game
type Price = domain.Price
type TeamOdds = domain.TeamOdds
type TotalOdds = domain.TotalOdds

const (
	draftKingsErrorBodyLimit = 8 << 10
	draftKingsPreviewLimit   = 2 << 10
)

// responsePreview retains only the beginning of a response so an unexpected
// HTTP 200 payload can be diagnosed without buffering the entire odds feed.
type responsePreview struct {
	data      []byte
	limit     int
	truncated bool
}

func (preview *responsePreview) Write(value []byte) (int, error) {
	remaining := preview.limit - len(preview.data)
	if remaining > 0 {
		preview.data = append(preview.data, value[:min(len(value), remaining)]...)
	}
	if len(value) > remaining {
		preview.truncated = true
	}
	return len(value), nil
}

func (preview responsePreview) String() string {
	text := fmt.Sprintf("%q", preview.data)
	if preview.truncated {
		return text + fmt.Sprintf(" (truncated after %d bytes)", preview.limit)
	}
	return text
}

type DraftKingsProvider struct {
	config   DraftKingsConfig
	endpoint string
	client   *http.Client
}

func NewDraftKingsProvider(config DraftKingsConfig, client *http.Client) (*DraftKingsProvider, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &DraftKingsProvider{config: config, endpoint: config.EndpointURL(), client: client}, nil
}

func (p *DraftKingsProvider) Fetch(ctx context.Context) ([]Game, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create DraftKings request: %w", err)
	}

	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Language", "en-CA,en-US;q=0.9,en;q=0.8")
	request.Header.Set("Origin", "https://sportsbook.draftkings.com")
	request.Header.Set("Referer", p.config.PageURL)
	request.Header.Set("Sec-Fetch-Dest", "empty")
	request.Header.Set("Sec-Fetch-Mode", "cors")
	request.Header.Set("Sec-Fetch-Site", "same-site")
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	request.Header.Set("X-Client-Feature", "league-page")
	request.Header.Set("X-Client-Name", "web")
	request.Header.Set("X-Client-Page", "League")

	response, err := p.client.Do(request)
	if err != nil {
		// http.Client wraps transport failures in url.Error, whose Error method
		// prints the full URL (including any configured query credentials).
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("fetch DraftKings odds from %s: %w", request.URL.Host, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, draftKingsErrorBodyLimit+1))
		preview := responsePreview{data: body, limit: draftKingsErrorBodyLimit}
		if len(body) > draftKingsErrorBodyLimit {
			preview.data = body[:draftKingsErrorBodyLimit]
			preview.truncated = true
		}
		if readErr != nil {
			return nil, fmt.Errorf("DraftKings HTTP %s; Content-Type=%q; body=%s; body read error: %w",
				response.Status, response.Header.Get("Content-Type"), preview.String(), readErr)
		}
		return nil, fmt.Errorf("DraftKings HTTP %s; Content-Type=%q; body=%s",
			response.Status, response.Header.Get("Content-Type"), preview.String())
	}

	var payload draftKingsResponse
	preview := responsePreview{limit: draftKingsPreviewLimit}
	decoder := json.NewDecoder(io.TeeReader(io.LimitReader(response.Body, 20<<20), &preview))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode DraftKings HTTP 200 response (Content-Type=%q, body prefix=%s): %w",
			response.Header.Get("Content-Type"), preview.String(), err)
	}

	games := normalizeDraftKings(payload)
	if len(games) == 0 {
		// A complete, explicitly empty slate is valid (for example, between
		// seasons). Missing fields or unrecognized populated markets are not.
		if payload.Events != nil && payload.Markets != nil && payload.Selections != nil &&
			len(payload.Events) == 0 && len(payload.Markets) == 0 && len(payload.Selections) == 0 {
			return []Game{}, nil
		}
		return nil, fmt.Errorf("DraftKings HTTP 200 contained no upcoming %s game-line markets (events=%d, markets=%d, selections=%d); body prefix=%s",
			p.config.LeagueName, len(payload.Events), len(payload.Markets), len(payload.Selections), preview.String())
	}
	return games, nil
}
