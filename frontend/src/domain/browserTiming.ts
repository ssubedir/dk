export type BrowserTimingSample = {
  ms: number
  visibility: 'foreground' | 'background'
}

export type TimingDistribution = {
  samples: number
  averageMs?: number
  p50Ms?: number
  p99Ms?: number
}

export type BrowserTimingSummary = {
  foreground: TimingDistribution
  background: TimingDistribution
}

export type PaintEvent = {
  receivedAt: number
  moveKeys: Record<string, string>
  changedKeys: string[]
  activeKeys: string[]
  receivedHidden: boolean
  live: boolean
}

export type PaintCandidate = {
  identity: string
  receivedAt: number
  receivedHidden: boolean
}

// Multiple SSE callbacks can be batched into one React commit. Preserve each
// still-visible quote's timing candidate, not just the last callback's move.
export function combinePaintEvents(events: PaintEvent[]): {
  candidates: Record<string, PaintCandidate>
  changedKeys: Set<string>
  activeKeys: Set<string>
} {
  const candidates: Record<string, PaintCandidate> = {}
  const changedKeys = new Set<string>()
  let activeKeys = new Set<string>()
  for (const event of events) {
    activeKeys = new Set(event.activeKeys)
    for (const key of event.changedKeys) {
      changedKeys.add(key)
      delete candidates[key]
    }
    for (const key of Object.keys(candidates)) {
      if (!activeKeys.has(key)) delete candidates[key]
    }
    if (!event.live) continue
    for (const [key, identity] of Object.entries(event.moveKeys)) {
      candidates[key] = { identity, receivedAt: event.receivedAt, receivedHidden: event.receivedHidden }
    }
  }
  return { candidates, changedKeys, activeKeys }
}

// An unrelated odds snapshot must not cancel a quote still awaiting a paint.
export function shouldCancelPaintJob(quoteKey: string, changedKeys: Set<string>, activeKeys: Set<string>): boolean {
  return changedKeys.has(quoteKey) || !activeKeys.has(quoteKey)
}

const sampleLimit = 1000

export function appendBrowserTiming(
  current: BrowserTimingSample[],
  ms: number,
  count: number,
  visibility: BrowserTimingSample['visibility'],
): BrowserTimingSample[] {
  if (!Number.isFinite(ms) || ms < 0 || !Number.isSafeInteger(count) || count < 1) return current
  const added = Array.from({ length: Math.min(count, sampleLimit) }, () => ({ ms, visibility }))
  return [...current, ...added].slice(-sampleLimit)
}

function distribution(samples: number[]): TimingDistribution {
  if (samples.length === 0) return { samples: 0 }
  samples.sort((a, b) => a - b)
  const percentile = (p: number) => {
    const position = p * (samples.length - 1)
    const lower = Math.floor(position)
    const upper = Math.ceil(position)
    return samples[lower] + (samples[upper] - samples[lower]) * (position - lower)
  }
  return {
    samples: samples.length,
    averageMs: samples.reduce((sum, ms) => sum + ms, 0) / samples.length,
    p50Ms: percentile(0.5),
    p99Ms: percentile(0.99),
  }
}

export function summarizeBrowserTiming(samples: BrowserTimingSample[]): BrowserTimingSummary {
  return {
    foreground: distribution(samples.filter((sample) => sample.visibility === 'foreground').map((sample) => sample.ms)),
    background: distribution(samples.filter((sample) => sample.visibility === 'background').map((sample) => sample.ms)),
  }
}
