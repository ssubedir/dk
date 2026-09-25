import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { appendBrowserTiming, combinePaintEvents, shouldCancelPaintJob, summarizeBrowserTiming } from '../domain/browserTiming'
import type { BrowserTimingSample, PaintEvent } from '../domain/browserTiming'
import { appendOddsHistory } from '../domain/oddsHistory'
import type { OddsHistory } from '../domain/oddsHistory'
import { mergeBrowserTimings, mergeFlushTimings } from '../domain/latency'
import type { FlushTimingEvent } from '../domain/latency'
import { findMovements, flattenPrices, timedMovesForChanges } from '../domain/oddsMovement'
import type { MovementMap } from '../domain/oddsMovement'
import type { CellLatency, OddsResponse } from '../types/odds'
import type { AnalyticsSummary } from '../types/analytics'
import { API_BASE_URL } from '../utils/api'
import { moveKey } from '../utils/moveKey'

type PaintJob = {
  firstFrame: number
  secondFrame: number
  onVisibilityChange: () => void
}

function cancelPaintJob(job: PaintJob) {
  window.cancelAnimationFrame(job.firstFrame)
  window.cancelAnimationFrame(job.secondFrame)
  document.removeEventListener('visibilitychange', job.onVisibilityChange)
}

export function useLiveOdds() {
  const [data, setData] = useState<OddsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [connecting, setConnecting] = useState(true)
  const [webSocketConnected, setWebSocketConnected] = useState<boolean | null>(null)
  const [changedPrices, setChangedPrices] = useState<Set<string>>(new Set())
  const [movements, setMovements] = useState<MovementMap>({})
  const [history, setHistory] = useState<OddsHistory>({})
  const [priceLatencies, setPriceLatencies] = useState<Record<string, CellLatency>>({})
  const [browserTimings, setBrowserTimings] = useState<BrowserTimingSample[]>([])
  const [analyticsSummary, setAnalyticsSummary] = useState<AnalyticsSummary | null>(null)
  const [analyticsStatus, setAnalyticsStatus] = useState<'loading' | 'ready' | 'unavailable' | 'error'>('loading')
  const [paintEpoch, setPaintEpoch] = useState(0)
  const pendingPaintEvents = useRef<PaintEvent[]>([])
  const previous = useRef<OddsResponse | null>(null)
  const clearHighlightsTimer = useRef<number | undefined>(undefined)
  const paintJobs = useRef<Map<string, PaintJob>>(new Map())

  const applySnapshot = useCallback((next: OddsResponse, live: boolean, receivedAt: number) => {
    const changes = findMovements(previous.current?.games ?? null, next.games, Date.now())
    const activePrices = flattenPrices(next.games)
    const updatedAt = next.updatedAt ?? next.fetchedAt
    const changedKeys = Object.keys(changes)
    const timedMoves = !next.stale
      ? timedMovesForChanges(next.games, changedKeys, next.moves, next.move)
      : {}
    const moveKeys = Object.fromEntries(Object.entries(timedMoves).map(([key, move]) => [key, moveKey(move, key)]))
    // React may batch multiple SSE events into one commit. Keep every event's
    // move identity until that commit so an earlier changed quote is not lost.
    pendingPaintEvents.current.push({
      receivedAt, moveKeys, changedKeys, activeKeys: Object.keys(activePrices),
      receivedHidden: document.visibilityState !== 'visible', live,
    })
    setPaintEpoch((current) => current + 1)
    setPriceLatencies((current) => {
      const retained: Record<string, CellLatency> = {}
      for (const key of Object.keys(activePrices)) {
        if (!changes[key] && current[key]) retained[key] = current[key]
      }
      for (const [key, move] of Object.entries(timedMoves)) {
        retained[key] = { moveKey: moveKey(move, key), ms: null, browserMs: live ? null : undefined }
      }
      return retained
    })
    previous.current = next
    setData(next)
    setError(null)
    setConnecting(false)
    setMovements((current) => {
      const retained: MovementMap = {}
      for (const key of Object.keys(activePrices)) {
        if (current[key]) retained[key] = current[key]
      }
      return { ...retained, ...changes }
    })
    if (!next.stale) {
      setHistory((current) => appendOddsHistory(current, next.games, updatedAt))
    }
    if (changedKeys.length > 0) {
      setChangedPrices((current) => new Set([...current, ...changedKeys]))
      window.clearTimeout(clearHighlightsTimer.current)
      clearHighlightsTimer.current = window.setTimeout(
        () => setChangedPrices(new Set()),
        2600,
      )
    }
  }, [])

  useLayoutEffect(() => {
    const events = pendingPaintEvents.current.splice(0)
    if (events.length === 0) return
    const { candidates, changedKeys, activeKeys } = combinePaintEvents(events)
    for (const [quoteKey, job] of paintJobs.current) {
      if (shouldCancelPaintJob(quoteKey, changedKeys, activeKeys)) {
        cancelPaintJob(job)
        paintJobs.current.delete(quoteKey)
      }
    }
    for (const [quoteKey, candidate] of Object.entries(candidates)) {
      const { identity, receivedAt, receivedHidden } = candidate
      const previousJob = paintJobs.current.get(quoteKey)
      if (previousJob) cancelPaintJob(previousJob)
      let becameHidden = receivedHidden || document.visibilityState !== 'visible'
      const job: PaintJob = {
        firstFrame: 0,
        secondFrame: 0,
        onVisibilityChange: () => {
          if (document.visibilityState !== 'visible') becameHidden = true
        },
      }
      paintJobs.current.set(quoteKey, job)
      document.addEventListener('visibilitychange', job.onVisibilityChange)
      // React has committed this price. The first frame gets a paint
      // opportunity; the next frame records the browser-side duration.
      job.firstFrame = window.requestAnimationFrame(() => {
        job.secondFrame = window.requestAnimationFrame(() => {
          if (paintJobs.current.get(quoteKey) !== job) return
          paintJobs.current.delete(quoteKey)
          document.removeEventListener('visibilitychange', job.onVisibilityChange)
          const visibility = becameHidden || document.visibilityState !== 'visible' ? 'background' : 'foreground'
          const elapsed = performance.now() - receivedAt
          setPriceLatencies((current) => mergeBrowserTimings(current, { [quoteKey]: identity }, elapsed, visibility === 'background'))
          setBrowserTimings((current) => appendBrowserTiming(current, elapsed, 1, visibility))
        })
      })
    }
  }, [paintEpoch])

  useLayoutEffect(() => () => {
    for (const job of paintJobs.current.values()) cancelPaintJob(job)
    paintJobs.current.clear()
  }, [])

  useEffect(() => {
    const source = new EventSource(`${API_BASE_URL}/api/stream`)
    let firstOdds = true
    source.addEventListener('odds', (event) => {
      const receivedAt = performance.now()
      try {
        applySnapshot(JSON.parse(event.data) as OddsResponse, !firstOdds, receivedAt)
        firstOdds = false
      } catch {
        setError('The live odds stream sent an invalid update.')
        setConnecting(false)
      }
    })
    source.addEventListener('flush-timing', (event) => {
      try {
        const timing = JSON.parse(event.data) as FlushTimingEvent
        setPriceLatencies((current) => mergeFlushTimings(current, timing))
      } catch {
        // A malformed diagnostic event must not interrupt the odds stream.
      }
    })
    source.addEventListener('analysis-summary', (event) => {
      try {
        setAnalyticsSummary(JSON.parse(event.data) as AnalyticsSummary)
        setAnalyticsStatus('ready')
      } catch {
        setAnalyticsStatus('error')
      }
    })
    source.addEventListener('analysis-error', () => setAnalyticsStatus('error'))
    source.addEventListener('unavailable', (event) => {
      const message = JSON.parse(event.data) as { error?: string }
      setError(message.error ?? 'DraftKings odds are temporarily unavailable')
      setConnecting(false)
    })
    source.addEventListener('status', (event) => {
      try {
        const status = JSON.parse(event.data) as { websocketConnected: boolean }
        setWebSocketConnected(status.websocketConnected)
        if (status.websocketConnected) setError(null)
      } catch {
        setError('The live connection sent an invalid status.')
      }
    })
    source.onerror = () => {
      firstOdds = true
      setError('Live connection interrupted. Reconnecting automatically.')
      setConnecting(false)
      setAnalyticsStatus('error')
    }

    return () => {
      source.close()
      window.clearTimeout(clearHighlightsTimer.current)
    }
  }, [applySnapshot])

  const browserTiming = useMemo(() => summarizeBrowserTiming(browserTimings), [browserTimings])
  return { data, error, connecting, webSocketConnected, changedPrices, movements, history, priceLatencies, browserTiming, analyticsSummary, analyticsStatus }
}
