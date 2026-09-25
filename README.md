# DraftKings live odds

A Go backend reads DraftKings NFL odds and pushes them to a React page. It shows only the main moneyline, spread, and total markets—not props or alternate lines.

- **Live app:** [dk-rtdw.onrender.com](https://dk-rtdw.onrender.com)
- **Source code:** [github.com/ssubedir/dk](https://github.com/ssubedir/dk)

The app runs one league at a time. The Docker image builds React and serves it alongside the Go API.

## Run locally

Install Go 1.25.5+ and Bun. Start the NFL backend:

```bash
cd backend
go run ./cmd/server -env-file .env.nfl
```

In another terminal, start React:

```bash
cd frontend
bun install
bun run dev
```

For a Docker run instead, set `env_file` in `compose.yaml` to `./backend/.env.nfl`, then run:

```bash
docker compose up --build
```

Open `http://localhost:10000`.

## Configuration and deployment

Go defaults to NFL if no env file is selected. The [backend README](backend/README.md#configuration) lists every setting. In WebSocket mode, `ODDS_REFRESH_INTERVAL` controls retry spacing, not the one-minute REST reconciliation check.

The bundled Docker app needs no cross-origin settings. If you host React separately, set `VITE_API_BASE_URL` to the Go server's origin **when building React**, and set `FRONTEND_ORIGIN` to the React site's origin **when starting Go**. Do not add `/api` to either value. Set runtime variables on Render or your other host; Compose's env file does not configure that host. Go logs its effective settings at startup without URL credentials or query strings. A sleeping host can delay reconnects while it wakes.

## How the data gets to the page

Inspection of the DraftKings page's network traffic revealed binary MessagePack WebSocket messages. REST supplies the starting list of games, markets, prices, and IDs. WebSocket messages then send only what changed, so Go keeps the full current picture in memory:

```text
DraftKings REST snapshot ──→ Go normalized cache ──→ SSE ──→ React
DraftKings WebSocket deltas ──→ same cache ────────→ SSE ──→ React
```

This avoids waiting for the next poll to show a known change. One DraftKings subscription feeds every browser. Go serves data, not DraftKings HTML.

SSE carries updates from Go to the browser. They flow one way, and the browser's `EventSource` reconnects on its own. Go sends the latest snapshot when one is available, without an extra heartbeat or subscription protocol in React. `GET /api/odds` also reads this cache; opening the page does not trigger one DraftKings request per browser.

Go uses DraftKings selection IDs to update the right quote. It prefers `PrimaryMarket` and `MainPointLine` tags. If the main line is unclear, Go leaves that quote blank rather than guessing.

An unknown ID or market-structure change triggers a REST refresh. The refresh keeps newer WebSocket prices that arrived while it ran. Go also checks REST once a minute while connected. The feed is undocumented, so a silent line change without a clear signal might not be caught until that check.

`fetchedAt` is the last REST fetch; `updatedAt` changes after REST or a live update. A failed fetch does not erase the last good prices. A truly empty slate is shown as empty; a broken or incomplete response is treated as an error.

## Where to find things

- `backend/internal/domain/`: normalized games, quotes, and live changes.
- `backend/internal/draftkings/`: DraftKings REST, MessagePack WebSocket, and decoding.
- `backend/internal/oddsstore/`: current snapshot and selection-ID cache.
- `backend/internal/stream/`: SSE fan-out, bootstrap, and metrics.
- `backend/internal/app/`: recovery policy and server wiring.
- `backend/internal/httpapi/`: API, SSE handlers, and static React serving.
- `backend/internal/analytics/`: in-memory quote history.
- `frontend/src/`: React page, SSE hook, table, charts, and timing logic.

See the [backend](backend/README.md) and [frontend](frontend/README.md) guides for more detail. In production, Go serves the built React files; it does not generate the page HTML.

## Freshness and timing

Go pushes known price changes as soon as it handles a DraftKings WebSocket message. The page highlights the change, shows the old price, and updates a small chart. It shows two **separate** timing stages beside changed quotes and in a collapsible panel:

| Display | Measured interval | Excludes |
| --- | --- | --- |
| `SSE` | Go receives a WebSocket change (or finishes a REST fetch) → flushes its odds event | Upstream delivery, network travel to the browser, rendering |
| `UI` | Browser SSE callback → frame opportunity after React commits the quote | Network travel before the callback and exact physical pixel paint |

The panel shows average, median (P50), and P99. Server numbers come from recent live changes in Go; browser numbers come from this tab. Background-tab changes are counted separately. Neither metric measures Go → browser travel or exact pixel paint, so the stages are not added into a total.

One observed live session used the app deployed on Render in Virginia (US East):

| Stage | Changes measured | Average | P50 | P99 |
| --- | ---: | ---: | ---: | ---: |
| Browser rendering, foreground tab | 296 | 9.9 ms | 9.8 ms | 15 ms |
| Server publication | 270 retained changes in the 24-hour analysis window | 0.31 ms | 0.28 ms | 0.75 ms |

The browser and server rows use different changes. They are not a measured DraftKings-to-screen total. The browser's location was not recorded; Render's Virginia region does not tell us the network travel time to that browser.

DraftKings includes `publishedTime` on some messages, but its timestamps were sometimes 0.5–0.9 seconds *ahead* of Go's clock in local tests. A timezone difference does not explain that. The clocks or the meaning of that timestamp may differ. Go leaves negative DraftKings → Go samples out of those statistics. The displayed `SSE` timing uses only Go's clock.

## Live-session analysis

Go keeps up to 50,000 quote observations in memory. It saves starting prices and real changes, not repeated unchanged values. New observations replace the oldest at the limit; a restart clears the history. The dashboard and these endpoints read Go's memory, not DraftKings:

- `GET /api/analysis/summary?hours=24`: counts and timing statistics.
- `GET /api/analysis/moves?gameId=GAME_ID&limit=100`: recent old/new prices for one game.
- `GET /api/analysis/dump?limit=100`: recent normalized observations for debugging; optional `gameId` and `before` cursor. No raw DraftKings response, headers, or cookies.

Go sends the summary over the existing SSE connection on connect and when results change. The browser does not poll for it. Replayed data does not count as a new live timing sample. `GET /api/metrics` has separate connection and recovery counters.

## When something fails

- If the first REST fetch fails, Go retries with increasing delays. The page shows an unavailable message until it has odds. In WebSocket mode, subscription starts only after this REST snapshot succeeds.
- If a later fetch fails, Go keeps the last good prices and marks them stale. It does not replace them with an empty list.
- If the DraftKings WebSocket disconnects, Go retries with exponential backoff up to 30 seconds. After reconnecting, it uses REST to fill gaps. Unknown or structural updates also request a REST refresh.
- Browser SSE reconnects automatically. The one-minute REST check runs only while the DraftKings WebSocket is connected. While disconnected, the page warns that prices may be stale. A restart clears the cache and starts from REST again.

Go logs connection and subscription failures, close reasons, ping failures, and retry delays. It limits upstream error text and redacts known token patterns; it does not log raw frames or cookies. It does not call a disconnect “token expiry” unless DraftKings says so.

REST requests time out after 12 seconds. The UI has no refresh button; `POST /api/refresh` is for diagnostics and is rate-limited. If WebSocket stops working but REST still works, set `DRAFTKINGS_WS_ENABLED=false` to use regular REST polling. A quiet market alone does not make prices stale.

Go gets its REST snapshot from DraftKings' [CA-ON markets endpoint](https://sportsbook-nash.draftkings.com/sites/CA-ON-SB/api/sportscontent/controldata/league/leagueSubcategory/v1/markets), with league and market filters in the query string. No login or browser cookie is configured. Local CA-ON tests and the Virginia deployment did not encounter an access denial. A Frankfurt deployment did: its initial REST request returned HTTP 403 with an HTML `Access Denied` response. Because the WebSocket starts only after REST succeeds, that log does **not** show whether the WebSocket was blocked too. The cause could be geography or the hosting IP; the exact access rule is unknown. No bypass was used.

The WebSocket sends the literal `jwt: "default-token"` seen in DraftKings' own request; it does not use a cookie-derived token. It has not expired in observed runs. If DraftKings starts rejecting it, Go retries and marks prices stale. REST-only mode is available if REST still works. If both feeds are blocked, prices cannot refresh.

A connected socket does not prove every update arrived. The next successful one-minute REST check can catch a silent miss, but not instantly. A quiet market cannot prove that DraftKings is publishing every change. Access to the live app also depends on the host and DraftKings being reachable from the viewer's region.

## Extending the approach

DraftKings WebSocket subscriptions can be filtered by league ID and game-line category. The `query` selects events; `includeMarkets` selects their markets, including the `OSB` tag filter. Switching leagues mainly means changing those IDs, the REST template values, and the page URL, then checking that the market mapping still fits.

The app currently runs one league at a time. To show several at once, the straightforward extension is one REST snapshot, filtered WebSocket subscription, and cache per league in the same Go process. Go would send each league's data to its own UI view. A shared WebSocket carrying multiple subscriptions might reduce connections, but that remains untested with DraftKings.

A second sportsbook would need an adapter that maps its games and prices into the same `Game`/`Price` model, plus a sportsbook label. The DraftKings integration shows the process: its WebSocket frames looked opaque in DevTools because they use binary MessagePack and send partial updates. Codex/GPT-6 helped decode captured frames, match their selection IDs to the games and markets in the REST snapshot, and build the Go WebSocket consumer. The same approach could help explore another feed and draft mapping tests, but its market meanings, prices, and recovery behavior would still need manual verification.

Another possible bridge is a browser agent that observes a sportsbook's network responses or page changes and sends normalized updates to Go. That could help when no direct feed is available, but it is only a concept here: browser automation adds latency and failure points, and the captured odds would still need validation. A direct feed adapter remains the better fit for a low-latency service.

## Verify

```bash
cd backend
go test ./...
go vet ./...
go build ./...

cd ../frontend
bun test
bun run lint
bun run build
```

`go build ./...` checks compilation without making a named binary. Docker builds the server explicitly. Local frontend commands use Bun; Docker uses `npm ci`, so update both `bun.lock` and `package-lock.json` when dependencies change.

The backend tests cover data mapping, captured WebSocket frames, recovery, SSE, and analytics. Frontend tests cover movement, history, timing, and components. See the [backend README](backend/README.md) for routes and settings, or the [frontend README](frontend/README.md) for browser behavior.
