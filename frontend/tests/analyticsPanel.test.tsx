import { expect, test } from 'bun:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { AnalyticsPanel } from '../src/components/AnalyticsPanel'
import type { BrowserTimingSummary } from '../src/domain/browserTiming'
import type { AnalyticsSummary } from '../src/types/analytics'

const noBrowserTiming: BrowserTimingSummary = { foreground: { samples: 0 }, background: { samples: 0 } }

test('timing panel starts collapsed', () => {
  const html = renderToStaticMarkup(<AnalyticsPanel summary={null} status="loading" browserTiming={noBrowserTiming} />)
  expect(html).toContain('<details class="analytics-panel">')
  expect(html).not.toContain('<details class="analytics-panel" open')
  expect(html).toContain('Browser P99 — · Server P99 — · 0 changes')
})

test('shows server flush percentiles without DraftKings clock timing', () => {
  const summary: AnalyticsSummary = {
    hours: 24,
    observations: 116,
    priceChanges: 20,
    games: 16,
    timedObservations: 0,
    clockSkewedObservations: 20,
    sseFlushSamples: 20,
    averageGoToSseFlushMs: 0.4,
    p50GoToSseFlushMs: 0.3,
    p99GoToSseFlushMs: 0.8,
    history: { storedObservations: 50_000, capacity: 50_000, evictedObservations: 3 },
  }
  const html = renderToStaticMarkup(<AnalyticsPanel summary={summary} status="ready" browserTiming={noBrowserTiming} />)
  expect(html).toContain('Server publication')
  expect(html).toContain('P99')
  expect(html).toContain('0.80ms')
  expect(html).toContain('Browser P99 — · Server P99 0.80ms · 0 changes')
  expect(html).not.toContain('DraftKings→Go timing')
  expect(html).not.toContain('Changed displayed quotes')
  expect(html).not.toContain('Distinct games observed')
  expect(html).not.toContain('Waiting for a price move with a usable DraftKings timestamp')
  expect(html).not.toContain('Stored price observations')
  expect(html).not.toContain('Oldest removed')
  expect(html).not.toContain('Older observations were removed')
})

test('keeps browser processing separate from backend delivery', () => {
  const summary: AnalyticsSummary = {
    hours: 24,
    observations: 10,
    priceChanges: 4,
    games: 1,
    timedObservations: 0,
    clockSkewedObservations: 0,
    sseFlushSamples: 4,
    averageGoToSseFlushMs: 0.3,
    p50GoToSseFlushMs: 0.2,
    p99GoToSseFlushMs: 0.5,
    history: { storedObservations: 10, capacity: 50_000, evictedObservations: 0 },
  }
  const browserTiming: BrowserTimingSummary = {
    foreground: { samples: 3, averageMs: 20, p50Ms: 18, p99Ms: 27 },
    background: { samples: 1, averageMs: 2000, p50Ms: 2000, p99Ms: 2000 },
  }
  const html = renderToStaticMarkup(<AnalyticsPanel summary={summary} status="ready" browserTiming={browserTiming} />)
  expect(html).toContain('class="analytics-headline">Browser P99 27ms · Server P99 0.50ms · 3 changes</span>')
  expect(html).toContain('Browser rendering')
  expect(html).toContain('Background-tab changes are counted separately: 1')
  expect(html).toContain('Server publication')
  expect(html).not.toContain('DraftKings → page')
  expect(html).toContain('Travel time from Go to the browser is not included')
})

test('shows only the two measured stages when source timing is available', () => {
  const summary: AnalyticsSummary = {
    hours: 24,
    observations: 10,
    priceChanges: 4,
    games: 1,
    timedObservations: 3,
    clockSkewedObservations: 1,
    sseFlushSamples: 4,
    averageSourceToGoMs: 72,
    averageGoToSseFlushMs: 0.5,
    history: { storedObservations: 10, capacity: 50_000, evictedObservations: 0 },
  }
  const browserTiming: BrowserTimingSummary = {
    foreground: { samples: 2, averageMs: 27, p50Ms: 27, p99Ms: 30 },
    background: { samples: 0 },
  }
  const html = renderToStaticMarkup(<AnalyticsPanel summary={summary} status="ready" browserTiming={browserTiming} />)
  expect(html).toContain('Browser rendering')
  expect(html).toContain('Server publication')
  expect(html).toContain('27ms')
  expect(html).toContain('0.50ms')
  expect(html).not.toContain('DraftKings → page')
  expect(html).not.toContain('~100ms')
})

test('shows browser timing even when backend analytics are unavailable', () => {
  const browserTiming: BrowserTimingSummary = {
    foreground: { samples: 1, averageMs: 18, p50Ms: 18, p99Ms: 18 },
    background: { samples: 0 },
  }
  const html = renderToStaticMarkup(<AnalyticsPanel summary={null} status="unavailable" browserTiming={browserTiming} />)
  expect(html).toContain('class="analytics-headline">Browser P99 18ms · Server P99 — · 1 change</span>')
  expect(html).toContain('Server timing is unavailable right now')
})
