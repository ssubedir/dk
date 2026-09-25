import type { CellLatency } from '../types/odds'

// Go reports SSE flush timing after the odds event. Browser render timing is
// separate and uses performance.now() entirely within the current tab.
export type FlushTimingEvent = {
  replay: boolean
  timings: Array<{
    quoteKey: string
    revision: number
    goObservedAt: string
    goToSseFlushMs?: number
  }>
}

export type LatencyPresentation = {
  label: string
  title: string
}

export function mergeFlushTimings(current: Record<string, CellLatency>, event: FlushTimingEvent): Record<string, CellLatency> {
  if (!event || !Array.isArray(event.timings)) return current
  let next: Record<string, CellLatency> | null = null
  for (const timing of event.timings) {
    if (!timing || typeof timing.quoteKey !== 'string' ||
      typeof timing.goObservedAt !== 'string' || !Number.isSafeInteger(timing.revision)) continue
    const reading = current[timing.quoteKey]
    if (!reading || reading.moveKey !== `${timing.quoteKey}:${timing.goObservedAt}:${timing.revision}`) continue
    if (reading.ms !== null || reading.replay) continue
    if (event.replay) {
      next ??= { ...current }
      next[timing.quoteKey] = { ...reading, replay: true }
    } else if (typeof timing.goToSseFlushMs === 'number' &&
      Number.isFinite(timing.goToSseFlushMs) && timing.goToSseFlushMs >= 0) {
      next ??= { ...current }
      next[timing.quoteKey] = { ...reading, ms: timing.goToSseFlushMs, replay: false }
    }
  }
  return next ?? current
}

export function mergeBrowserTimings(
  current: Record<string, CellLatency>,
  moveKeys: Record<string, string>,
  browserMs: number,
  background: boolean,
): Record<string, CellLatency> {
  if (!Number.isFinite(browserMs) || browserMs < 0) return current
  let next: Record<string, CellLatency> | null = null
  for (const [quoteKey, identity] of Object.entries(moveKeys)) {
    const reading = current[quoteKey]
    if (!reading || reading.moveKey !== identity || reading.replay || reading.browserMs != null) continue
    next ??= { ...current }
    next[quoteKey] = { ...reading, browserMs, browserBackground: background }
  }
  return next ?? current
}

export function describeLatency(goToSseFlushMs: number | null, replay = false): LatencyPresentation {
  if (replay) {
    return { label: 'Replay', title: 'This price arrived from an SSE replay after reconnecting; no live server-flush timing was measured for this connection.' }
  }
  if (goToSseFlushMs === null || !Number.isFinite(goToSseFlushMs) || goToSseFlushMs < 0) {
    return { label: 'Measuring…', title: 'Waiting for Go to report the SSE flush timing.' }
  }
  return {
    label: formatDuration(goToSseFlushMs),
    title: `Go update to SSE flush: ${formatDuration(goToSseFlushMs)}. Starts when Go receives a WebSocket move (or finishes a REST fetch) and ends when Go flushes the odds event for this connection. It does not measure network arrival or browser rendering.`,
  }
}

export function describeBrowserLatency(browserMs?: number | null, background = false, replay = false): LatencyPresentation {
  if (replay) return { label: 'Replay', title: 'This quote came from an SSE replay; no fresh browser render timing was recorded.' }
  if (browserMs === undefined) {
    return { label: 'Not measured', title: 'This quote did not arrive as a fresh live SSE move in this tab.' }
  }
  if (browserMs === null || !Number.isFinite(browserMs) || browserMs < 0) {
    return { label: 'Measuring…', title: 'Waiting for the browser to render this live quote.' }
  }
  return {
    label: `${formatDuration(browserMs)}${background ? ' BG' : ''}`,
    title: `SSE event callback to page paint opportunity: ${formatDuration(browserMs)}${background ? ' (the tab was backgrounded during this move)' : ''}. This does not include network transit before the browser callback.`,
  }
}

function formatDuration(milliseconds: number): string {
  if (milliseconds < 1) return `${milliseconds.toFixed(2)}ms`
  if (milliseconds < 10) return `${milliseconds.toFixed(1)}ms`
  if (milliseconds < 1000) return `${Math.round(milliseconds)}ms`
  return `${(milliseconds / 1000).toFixed(1)}s`
}
