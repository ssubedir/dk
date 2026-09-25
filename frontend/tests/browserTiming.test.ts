import { expect, test } from 'bun:test'
import { appendBrowserTiming, combinePaintEvents, shouldCancelPaintJob, summarizeBrowserTiming } from '../src/domain/browserTiming'

test('summarizes browser processing without mixing background tabs', () => {
  let samples = appendBrowserTiming([], 10, 1, 'foreground')
  samples = appendBrowserTiming(samples, 30, 1, 'foreground')
  samples = appendBrowserTiming(samples, 5000, 1, 'background')
  const summary = summarizeBrowserTiming(samples)
  expect(summary.foreground).toEqual({ samples: 2, averageMs: 20, p50Ms: 20, p99Ms: 29.8 })
  expect(summary.background).toEqual({ samples: 1, averageMs: 5000, p50Ms: 5000, p99Ms: 5000 })
})

test('keeps only the last 1,000 valid quote samples', () => {
  const samples = appendBrowserTiming([], 5, 1100, 'foreground')
  expect(samples).toHaveLength(1000)
  expect(appendBrowserTiming(samples, -1, 1, 'foreground')).toBe(samples)
  expect(summarizeBrowserTiming(samples).foreground.p50Ms).toBe(5)
})

test('unrelated updates keep a quote paint job alive', () => {
  const active = new Set(['game:away:moneyline', 'game:home:moneyline'])
  expect(shouldCancelPaintJob('game:away:moneyline', new Set(['game:home:moneyline']), active)).toBe(false)
  expect(shouldCancelPaintJob('game:away:moneyline', new Set(['game:away:moneyline']), active)).toBe(true)
  expect(shouldCancelPaintJob('game:away:moneyline', new Set(), new Set(['game:home:moneyline']))).toBe(true)
})

test('batched SSE moves retain a paint candidate for every changed quote', () => {
  const first = 'game:away:moneyline'
  const second = 'game:home:moneyline'
  const batch = combinePaintEvents([
    { receivedAt: 10, moveKeys: { [first]: 'first:7' }, changedKeys: [first], activeKeys: [first, second], receivedHidden: false, live: true },
    { receivedAt: 15, moveKeys: { [second]: 'second:8' }, changedKeys: [second], activeKeys: [first, second], receivedHidden: false, live: true },
  ])
  expect(batch.candidates[first]).toEqual({ identity: 'first:7', receivedAt: 10, receivedHidden: false })
  expect(batch.candidates[second]).toEqual({ identity: 'second:8', receivedAt: 15, receivedHidden: false })
  const superseded = combinePaintEvents([
    { receivedAt: 10, moveKeys: { [first]: 'first:7' }, changedKeys: [first], activeKeys: [first], receivedHidden: false, live: true },
    { receivedAt: 15, moveKeys: { [first]: 'first:8' }, changedKeys: [first], activeKeys: [first], receivedHidden: false, live: true },
  ])
  expect(Object.keys(superseded.candidates)).toEqual([first])
  expect(superseded.candidates[first].identity).toBe('first:8')
})
