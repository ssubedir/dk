import { useEffect } from 'react'
import { AnalyticsPanel } from './components/AnalyticsPanel'
import { LoadingState } from './components/LoadingState'
import { OddsTable } from './components/OddsTable'
import { StatusBadge } from './components/StatusBadge'
import { useLiveOdds } from './hooks/useLiveOdds'
import './App.css'

function App() {
  const { data, error, connecting, webSocketConnected, changedPrices, movements, history, priceLatencies, browserTiming, analyticsSummary, analyticsStatus } = useLiveOdds()

  useEffect(() => {
    document.title = data?.league
      ? `Linewatch · DraftKings ${data.league} Odds`
      : 'Linewatch · DraftKings Odds'
  }, [data?.league])

  const stale = Boolean(data && (data.stale || error || webSocketConnected === false))

  return (
    <div className="app-shell">
      <header className="topbar">
        <a className="brand" href="/" aria-label="Linewatch home">
          <span className="brand-mark" aria-hidden="true">
            LW
          </span>
          <span>
            <strong>Linewatch</strong>
            <small>DraftKings odds</small>
          </span>
        </a>
        <StatusBadge stale={stale} loading={!data && connecting} unavailable={!data && Boolean(error)} />
      </header>

      <main>
        <section className="intro" aria-labelledby="page-title">
          <div>
            <p className="eyebrow">{data?.league ?? 'LIVE'} · MAIN MARKETS</p>
            <h1 id="page-title">Live & upcoming games</h1>
            <p className="intro-copy">
              Live moneyline, spread, and total prices from DraftKings.
            </p>
          </div>
        </section>

        {(stale || (error && data)) && (
          <div className="notice warning" role="status">
            <span className="notice-icon" aria-hidden="true">!</span>
            <div>
              <strong>Showing the last successful prices</strong>
              <p>
                {error ?? (webSocketConnected === false
                  ? 'DraftKings WebSocket disconnected; reconnecting automatically.'
                  : 'DraftKings data could not be updated.')}{' '}
                Last-known prices are retained.
              </p>
            </div>
          </div>
        )}

        <AnalyticsPanel summary={analyticsSummary} status={analyticsStatus} browserTiming={browserTiming} />

        {!data && connecting && !error && <LoadingState />}

        {!data && error && (
          <div className="empty-state" role="alert">
            <span className="empty-icon" aria-hidden="true">!</span>
            <h2>Odds are temporarily unavailable</h2>
            <p>{error}</p>
            <p>The backend is retrying automatically. This page will update when odds are available.</p>
          </div>
        )}

        {data && data.games.length === 0 && (
          <div className="empty-state">
            <span className="empty-icon calendar" aria-hidden="true">–</span>
            <h2>No available {data.league} games</h2>
            <p>DraftKings is not currently offering game-line markets.</p>
          </div>
        )}

        {data && data.games.length > 0 && (
          <OddsTable league={data.league} games={data.games} changedPrices={changedPrices} movements={movements} history={history} priceLatencies={priceLatencies} />
        )}
      </main>

    </div>
  )
}

export default App
