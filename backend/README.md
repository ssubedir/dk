# Go backend

Go reads DraftKings NFL game-line odds and sends them to React. It starts with a REST snapshot, applies partial MessagePack WebSocket updates to an in-memory cache, and pushes changes to browsers with SSE. In production, Go also serves the built React files.

## Run locally

Requires Go 1.25.5+. From this directory:

```bash
go run ./cmd/server
```

The API starts at `http://localhost:8080`. `/healthz` says whether Go is running; `/api/odds` shows the cached odds. If the first DraftKings fetch fails, Go retries and the API reports that odds are unavailable.

Go loads `.env` from this directory if it exists. To use a named profile:

```bash
go run ./cmd/server -env-file .env.nfl
```

The default `.env` is optional, but a file named with `-env-file` must exist. Existing environment variables win over file values. `.env` is ignored by Git. [`.env.nfl`](.env.nfl) is tracked but is not loaded automatically; Go has NFL defaults even without it.

## Configuration

| Variable | Default | Use |
| --- | --- | --- |
| `PORT` | `8080` | Go HTTP port; Compose uses `10000` |
| `DRAFTKINGS_LEAGUE_NAME` | `NFL` | League label shown in the API and page |
| `DRAFTKINGS_LEAGUE_ID` | `88808` | League to request from DraftKings |
| `DRAFTKINGS_SUBCATEGORY_ID` | `4518` | Main game-line category |
| `DRAFTKINGS_TEMPLATE_VARS` | league ID | REST URL template values, if different |
| `DRAFTKINGS_PAGE_URL` | DraftKings NFL page | `Referer` sent upstream |
| `DRAFTKINGS_API_URL` | unset | Full REST URL override, if needed |
| `DRAFTKINGS_WS_ENABLED` | `true` | Set `false` to poll REST instead |
| `ODDS_REFRESH_INTERVAL` | `5s` | REST-only poll interval and retry spacing; it does **not** change the one-minute REST check in WebSocket mode |
| `FRONTEND_DIST` | unset | Built React directory for production serving |
| `FRONTEND_ORIGIN` | unset | React origin when hosted separately |

With WebSocket on, Go fetches the starting snapshot and then listens for changes. It fetches REST again after a reconnect, for a change it cannot safely apply, and once a minute while connected. These fetches run separately from the WebSocket reader.

The first recovery can start right away; repeated attempts are spaced by `ODDS_REFRESH_INTERVAL` (30 seconds in the checked-in profiles). With WebSocket off, that setting is the normal REST poll interval.

For an NFL Docker run, the root [Compose file](../compose.yaml) already loads `./backend/.env.nfl`. Compose passes the file at runtime; [`.dockerignore`](../.dockerignore) keeps it out of the image. Render and other hosts need their own runtime settings—Compose does not configure them.

## API

| Route | Purpose |
| --- | --- |
| `GET /healthz` | Process health; it does **not** prove DraftKings is reachable |
| `GET /api/odds` | Current normalized cache; does not make a new upstream request after bootstrap |
| `GET /api/stream` | SSE odds snapshots, upstream status/unavailability, per-move flush timing, and pushed analytics summaries |
| `GET /api/metrics` | Connection, update, resync, and REST-to-SSE metrics |
| `GET /api/analysis/summary?hours=24` | In-memory price-change and timing summary; `hours` accepts 1–168 |
| `GET /api/analysis/moves?gameId=ID&limit=100` | Retained moves for a game; `limit` accepts 1–500 |
| `GET /api/analysis/dump?limit=100` | Paginated normalized observation history; optional `gameId` and `before` |
| `POST /api/refresh` | Rate-limited diagnostic REST refresh; the current UI does not call it |

SSE sends odds, connection status, errors, timing, and analytics summaries. On connect, a browser gets the latest available snapshot and summary; later summaries are pushed when they change. `GET /api/odds` and SSE use the same snapshot shape.

## Recovery and timing

Go retries a failed first REST fetch. Once it has prices, it keeps them through later failures and marks them stale. WebSocket reconnect delays grow up to 30 seconds. After reconnecting, Go fetches REST to fill gaps. Unknown IDs and market changes also trigger a refresh, but repeated requests are grouped. Go uses DraftKings' main-market tags when present; without clear tags, it shows only unambiguous lines rather than guessing.

Logs show why a WebSocket attempt failed: handshake status, subscription error, close reason, timeout, or ping/read failure. They also show connection length and next retry delay. Known token patterns are redacted; raw frames and cookies are not logged. A disconnect is not called “token expiry” unless DraftKings gives that reason. REST refresh failures are logged too.

Go keeps up to 50,000 quote observations in memory, replacing the oldest when full. A restart clears them. Server timing runs from Go receiving a WebSocket change (or finishing a REST fetch) to flushing the odds SSE event. It excludes network travel to the browser and rendering. Some DraftKings timestamps were ahead of Go's clock, so negative DraftKings → Go samples are excluded. See [timing in the main README](../README.md#freshness-and-timing).

## Code and checks

`cmd/server/main.go` loads the env file. `internal/config/` validates settings. `internal/draftkings/` handles the upstream REST and MessagePack WebSocket formats. `internal/domain/` holds normalized odds, and `internal/oddsstore/` keeps the latest snapshot. `internal/app/` coordinates recovery, `internal/stream/` fans out updates, `internal/analytics/` stores recent observations, and `internal/httpapi/` serves the API and built React files.

From this directory:

```bash
go test ./...
go vet ./...
go build ./...
```

`go build ./...` checks compilation; success prints nothing and does not make a named binary. To make one, run `go build -o server.exe ./cmd/server` on Windows or `go build -o server ./cmd/server` on macOS/Linux.

See the [main README](../README.md) for the overall design and the [frontend README](../frontend/README.md) for browser behavior.
