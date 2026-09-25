# React frontend

This React app shows DraftKings NFL games and their main moneyline, spread, and total prices. React talks to Go, not directly to DraftKings. Vite serves the page locally; in Docker, Go serves the built page and API together.

## Run locally

Start the [Go backend](../backend/README.md) on port 8080. Then, from this directory:

```bash
bun install
bun run dev
```

Open `http://localhost:5173`. Vite forwards `/api` and `/healthz` to Go. To run checks or preview a build:

```bash
bun test
bun run lint
bun run build
bun run preview
```

The preview is at `http://localhost:4173`. Local development uses Bun; Docker uses Node and `npm ci`. If dependencies change, update both `bun.lock` and `package-lock.json`.

## How the page works

`useLiveOdds` opens one SSE (`EventSource`) connection to `GET /api/stream`. SSE fits this one-way flow from Go to React without a browser WebSocket. `EventSource` reconnects automatically, and Go replays its latest snapshot when available. React never polls DraftKings or calls `POST /api/refresh`.

The stream carries odds, connection status, errors, timing, and analytics summaries. The browser does not poll the summary endpoint. The timing panel starts collapsed and shows average, median (P50), and P99 for the server and browser stages.

When a price changes, the table highlights it, shows the old price with the time since this tab saw the change, and updates a small SVG chart. Charts show up to five minutes of payout odds in this tab and reset on reload. Spread and total line moves stay in the quote and old-price label; the chart tracks payout odds only.

If a connection fails, the page keeps the last prices and warns that they may be stale. Before the first valid snapshot, it shows an unavailable message. A quiet market does not become stale just because no price moved.

`SSE` beside a quote is Go receiving a WebSocket change (or finishing a REST fetch) → flushing it to the stream. `UI` is the browser's SSE callback → its next chance to paint after React updates. They are separate numbers, not a total; network travel to the browser and exact pixel paint are not measured. Background-tab changes are counted separately, and replayed odds are not treated as new live timing samples. See [freshness and timing](../README.md#freshness-and-timing).

## Build-time API URL

Leave `VITE_API_BASE_URL` unset for Docker or local Vite; React will call same-origin `/api`. If React is hosted separately, set it to Go's public origin (for example `https://api.example.com`) **before building**, and set Go's `FRONTEND_ORIGIN` to the React site's origin. Do not add `/api` to either value.

## Layout

- `src/App.tsx`: page layout and loading/error states
- `src/components/`: table, charts, status, and timing panel
- `src/hooks/useLiveOdds.ts`: SSE connection and live state
- `src/domain/`: price movement, history, and timing rules
- `src/types/` and `src/utils/`: data types and formatting
- `tests/`: component and logic tests

See the [main README](../README.md) for the full data flow and the [backend README](../backend/README.md) for API routes and recovery.
