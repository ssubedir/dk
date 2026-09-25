import { memo } from 'react'
import type { BrowserTimingSummary } from '../domain/browserTiming'
import type { AnalyticsSummary } from '../types/analytics'

function formatCount(value: number): string {
  return new Intl.NumberFormat('en-US').format(value)
}

function formatLatency(value?: number): string {
  if (value === undefined || !Number.isFinite(value)) return '—'
  if (value < 1) return `${value.toFixed(2)}ms`
  if (value < 10) return `${value.toFixed(1)}ms`
  if (value < 1000) return `${Math.round(value)}ms`
  return `${(value / 1000).toFixed(1)}s`
}

export const AnalyticsPanel = memo(function AnalyticsPanel({
  summary,
  status,
  browserTiming,
}: {
  summary: AnalyticsSummary | null
  status: 'loading' | 'ready' | 'unavailable' | 'error'
  browserTiming: BrowserTimingSummary
}) {
  const changes = browserTiming.foreground.samples
  const headline = `Browser P99 ${formatLatency(browserTiming.foreground.p99Ms)} · Server P99 ${formatLatency(summary?.p99GoToSseFlushMs)} · ${formatCount(changes)} ${changes === 1 ? 'change' : 'changes'}`

  return (
    <details className="analytics-panel">
      <summary className="analytics-heading">
        <span>
          <strong>Live update timing</strong>
          <small>Server and browser · separate stages</small>
        </span>
        <span className="analytics-headline">{headline}</span>
      </summary>

      <div className="analytics-body">
          {status === 'error' && summary && <p className="analytics-message warning-text" role="status">Could not refresh server timing. Showing the last results.</p>}
          <div className="analytics-timing browser-timing">
            <div className="analytics-timing-title">
              <strong>Browser rendering</strong>
              <span>{formatCount(browserTiming.foreground.samples)} foreground changes · this tab, last 1,000 samples</span>
            </div>
            <div className="analytics-timing-grid">
              <div><span>Average</span><strong>{formatLatency(browserTiming.foreground.averageMs)}</strong></div>
              <div><span>Median (P50)</span><strong>{formatLatency(browserTiming.foreground.p50Ms)}</strong></div>
              <div><span>P99</span><strong>{formatLatency(browserTiming.foreground.p99Ms)}</strong></div>
            </div>
            <p>
              {browserTiming.foreground.samples === 0
                ? 'Waiting for a live price change in this tab.'
                : 'From this tab handling an odds update to its next chance to paint the new price. Travel time from Go to the browser is not included.'}
            </p>
            {browserTiming.background.samples > 0 && (
              <p>Background-tab changes are counted separately: {formatCount(browserTiming.background.samples)} (median {formatLatency(browserTiming.background.p50Ms)}, P99 {formatLatency(browserTiming.background.p99Ms)}).</p>
            )}
          </div>
          {summary ? (
          <>
          <div className="analytics-timing">
            <div className="analytics-timing-title">
              <strong>Server publication</strong>
              <span>{formatCount(summary.sseFlushSamples ?? 0)} retained changes · past 24 hours</span>
            </div>
            <div className="analytics-timing-grid">
              <div><span>Average</span><strong>{formatLatency(summary.averageGoToSseFlushMs)}</strong></div>
              <div><span>Median (P50)</span><strong>{formatLatency(summary.p50GoToSseFlushMs)}</strong></div>
              <div><span>P99</span><strong>{formatLatency(summary.p99GoToSseFlushMs)}</strong></div>
            </div>
            <p>
              {summary.sseFlushSamples === 0
                ? 'Waiting for a live price change to be sent to a browser stream.'
                : 'From Go receiving a price change to flushing it to the live stream. Network delivery and browser rendering are not included.'}
            </p>
          </div>
          </>
          ) : <p className="analytics-message" role="status">
          {status === 'loading' && 'Loading server timing…'}
          {status === 'unavailable' && 'Server timing is unavailable right now. Browser timing in this tab still works.'}
          {status === 'error' && 'Could not load server timing.'}
        </p>}
      </div>
    </details>
  )
})
