import { expect, test } from 'bun:test'
import { describeBrowserLatency, describeLatency, mergeBrowserTimings, mergeFlushTimings } from '../src/domain/latency'

test('shows only the server-side Go-to-SSE-flush measurement', () => {
  const reading = describeLatency(1.25)
  expect(reading.label).toBe('1.3ms')
  expect(reading.title).toContain('Go update to SSE flush')
  expect(reading.title).toContain('does not measure network arrival or browser rendering')
})

test('distinguishes a replay from a pending live flush', () => {
  expect(describeLatency(null).label).toBe('Measuring…')
  expect(describeLatency(null, true).label).toBe('Replay')
})

test('matches server timing to the exact quote and move revision', () => {
  const key = 'game-1:away:spread'
  const observedAt = '2026-09-23T17:06:37.558Z'
  const current = { [key]: { moveKey: `${key}:${observedAt}:7`, ms: null } }
  const wrong = mergeFlushTimings(current, {
    replay: false,
    timings: [{ quoteKey: key, goObservedAt: observedAt, revision: 6, goToSseFlushMs: 2 }],
  })
  expect(wrong).toBe(current)
  const matched = mergeFlushTimings(current, {
    replay: false,
    timings: [{ quoteKey: key, goObservedAt: observedAt, revision: 7, goToSseFlushMs: 2 }],
  })
  expect(matched[key].ms).toBe(2)
  expect(current[key].ms).toBeNull()
  expect(mergeFlushTimings(matched, {
    replay: false,
    timings: [{ quoteKey: key, goObservedAt: observedAt, revision: 7, goToSseFlushMs: 999 }],
  })[key].ms).toBe(2)
  const replayed = mergeFlushTimings(current, {
    replay: true,
    timings: [{ quoteKey: key, goObservedAt: observedAt, revision: 7 }],
  })
  expect(replayed[key].replay).toBe(true)
})

test('matches browser timing to the same quote and move without overwriting a newer move', () => {
  const key = 'game-1:away:spread'
  const current = { [key]: { moveKey: `${key}:observed:7`, ms: 0.2, browserMs: null } }
  const wrong = mergeBrowserTimings(current, { [key]: `${key}:observed:6` }, 18, false)
  expect(wrong).toBe(current)
  const matched = mergeBrowserTimings(current, { [key]: `${key}:observed:7` }, 18, false)
  expect(matched[key].browserMs).toBe(18)
  expect(matched[key].ms).toBe(0.2)
  expect(current[key].browserMs).toBeNull()
  expect(mergeBrowserTimings(matched, { [key]: `${key}:observed:7` }, 100, true)[key].browserMs).toBe(18)
  expect(describeBrowserLatency(18).label).toBe('18ms')
  expect(describeBrowserLatency(5000, true).label).toBe('5.0s BG')
  expect(describeBrowserLatency(undefined).label).toBe('Not measured')
  expect(describeBrowserLatency(null).label).toBe('Measuring…')
})
