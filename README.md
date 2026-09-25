# DraftKings live odds

A Go backend reads DraftKings NFL odds and pushes them to a React page. It shows only the main moneyline, spread, and total markets—not props or alternate lines.

- **Live app:** [dk-rtdw.onrender.com](https://dk-rtdw.onrender.com)
- **Source code:** [github.com/ssubedir/dk](https://github.com/ssubedir/dk)

The app runs one league at a time. The Docker image builds React and serves it alongside the Go API.

## At a glance

- **Scope:** NFL game lines only—moneyline, spread, and total.
- **Upstream:** REST builds the initial slate; DraftKings WebSocket deltas keep it current.
- **Delivery:** One Go process maintains the shared snapshot and pushes updates to browsers with SSE.
- **Freshness:** Known WebSocket moves are published immediately; REST reconciles the full slate once a minute while connected.
- **Resilience:** The last good prices remain visible and are marked stale while upstream recovery runs.

## Run locally

### Docker (quickest)

With Docker installed:

```bash
docker compose up --build -d
```

Open `http://localhost:10000`. The checked-in Compose profile selects NFL and contains no credentials.

Use `docker compose logs -f odds` to follow the server logs and `docker compose down` to stop the app.

### Native development

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

Open `http://localhost:5173`. Vite proxies API requests to Go on port `8080`.

## Why this approach and how it works

Inspection of the DraftKings page's network traffic revealed a REST feed plus binary MessagePack WebSocket updates. That led to four deliberate choices:

1. **REST for bootstrap and reconciliation.** It supplies the complete event, market, selection, and ID relationships needed to build a trustworthy snapshot.
2. **DraftKings WebSocket deltas for low latency.** Waiting for the next REST poll would make visible prices unnecessarily old.
3. **A shared Go cache.** DraftKings sends partial changes, so the server joins them to the REST snapshot once instead of making every browser reconstruct state or open its own upstream connection.
4. **SSE from Go to React.** Browser traffic is one-way—server to page—so SSE is simpler than another WebSocket protocol, works through normal HTTP infrastructure, and gives the browser automatic reconnection through `EventSource`.

The resulting flow is:

```text
DraftKings REST snapshot -> Go normalized cache -> SSE -> React
DraftKings WebSocket deltas -> same cache -> SSE -> React
```

One DraftKings subscription feeds every browser. Go serves normalized data, not DraftKings HTML, and the UI never needs to understand the upstream MessagePack format.

Go maps WebSocket changes to the REST snapshot by DraftKings selection ID. The normalized model represents each quote as a game, market, side, line, and American/decimal odds, independent of DraftKings' upstream payload shape. It prefers `PrimaryMarket` and `MainPointLine` tags and leaves an ambiguous quote blank rather than guessing. Unknown IDs or structural changes trigger a REST refresh; newer WebSocket prices are preserved if they arrive while that refresh is running.

`GET /api/odds` and the SSE stream read the same cache. `fetchedAt` records the last REST fetch, while `updatedAt` also advances for live changes.

## Freshness and timing

Known WebSocket changes are pushed as soon as Go handles them—there is no application polling interval between receipt and publication. A one-minute REST reconciliation catches structural changes or silent misses that the WebSocket consumer could not identify. The page highlights each observed move, shows its previous price, and updates a small chart.

The UI reports two separate timing stages beside changed quotes and in a collapsible panel:

| Display | Measured interval | Excludes |
| --- | --- | --- |
| `SSE` | Go receives a WebSocket change (or finishes a REST fetch) → flushes its odds event | Upstream delivery, network travel to the browser, rendering |
| `UI` | Browser SSE callback → frame opportunity after React commits the quote | Network travel before the callback and exact physical pixel paint |

The panel shows average, median (P50), and P99. In one observed Render session, server publication averaged `0.31 ms` with a `0.75 ms` P99; foreground browser processing averaged `9.9 ms` with a `15 ms` P99. Those samples came from different changes and do not form a DraftKings-to-screen total. They exclude Go-to-browser network transit and exact pixel paint.

DraftKings' optional `publishedTime` sometimes appeared ahead of Go's clock, so negative source-to-Go samples are excluded rather than reported as valid latency. The displayed `SSE` measurement uses only Go's clock.

## When something fails

- If the first REST fetch fails, Go retries with increasing delays. The page shows an unavailable message until it has odds. In WebSocket mode, subscription starts only after this REST snapshot succeeds.
- If a later fetch fails, Go keeps the last good prices and marks them stale. It does not replace them with an empty list.
- If the DraftKings WebSocket disconnects, Go retries with exponential backoff up to 30 seconds. After reconnecting, it uses REST to fill gaps. Unknown or structural updates also request a REST refresh.
- Browser SSE reconnects automatically. While the upstream socket is disconnected, the page warns that prices may be stale.
- Unexpected or incomplete HTTP 200 responses are errors; a confirmed empty slate is shown as empty.

REST requests time out after 12 seconds. If WebSocket delivery stops but REST remains available, `DRAFTKINGS_WS_ENABLED=false` enables regular polling. A connected socket still cannot prove that every upstream update arrived, which is why the full REST reconciliation remains in place.

No login or browser cookie is required. The WebSocket uses the literal `jwt: "default-token"` observed in DraftKings' request. It has not expired in testing; rejection would trigger retry and a stale-data warning. The  [CA-ON markets endpoint](https://sportsbook-nash.draftkings.com/sites/CA-ON-SB/api/sportscontent/controldata/league/leagueSubcategory/v1/markets) worked locally and from the Virginia deployment but returned HTTP 403 from a Frankfurt deployment. No bypass was used, so hosting region remains an operational constraint.

## Extending the approach

- **Another league:** add its IDs and page URL, then give it a filtered snapshot, subscription, and cache. The market mapping still needs validation for that sport.
- **Another sportsbook:** implement an adapter that maps its events and selections into the existing `Game`/`Price` model, with a sportsbook identifier added to the domain.
- **AI-assisted exploration:** tooling can help inspect unfamiliar payloads, correlate selection IDs, and draft mapping tests. Market meaning and recovery behavior still require human verification. Direct feeds remain preferable to browser automation for latency and reliability.

## Configuration and deployment

Go defaults to NFL. The [backend guide](backend/README.md#configuration) documents every setting and API route; the [frontend guide](frontend/README.md) covers browser behavior and separate hosting.

The bundled Docker app is same-origin and needs no CORS configuration. For separate hosting, set `VITE_API_BASE_URL` to the Go origin at React build time and `FRONTEND_ORIGIN` to the React origin at Go startup. Runtime environment variables must also be configured on the deployment host; Compose does not configure Render or another provider.

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

The backend tests cover mapping, captured WebSocket frames, recovery, SSE, and analytics. Frontend tests cover movement, history, timing, and components. Local frontend commands use Bun; Docker uses `npm ci`, so dependency changes must update both lockfiles.
