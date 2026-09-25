import { expect, test } from 'bun:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { WaitingForOddsNotice } from '../src/components/WaitingForOddsNotice'
import { isWaitingForOddsUpdate } from '../src/domain/oddsHistory'

test('waits through the first odds snapshot and stops after another price check', () => {
  expect(isWaitingForOddsUpdate(false, 0, {})).toBe(true)
  expect(isWaitingForOddsUpdate(true, 1, { quote: [{ at: 1, payout: 2, american: '+100' }] })).toBe(true)
  expect(isWaitingForOddsUpdate(true, 1, {
    quote: [
      { at: 1, payout: 2, american: '+100' },
      { at: 2, payout: 2.1, american: '+110' },
    ],
  })).toBe(false)
  expect(isWaitingForOddsUpdate(true, 0, {})).toBe(false)
})

test('explains that the app is waiting for its first odds snapshot', () => {
  const html = renderToStaticMarkup(<WaitingForOddsNotice hasInitialOdds={false} />)

  expect(html).toContain('role="status"')
  expect(html).toContain('aria-live="polite"')
  expect(html).toContain('Waiting for live odds')
  expect(html).toContain('listening for the first odds snapshot')
})

test('uses the same waiting state as an empty price sparkline after initial odds load', () => {
  const html = renderToStaticMarkup(<WaitingForOddsNotice hasInitialOdds />)

  expect(html).toContain('Waiting for another price update')
  expect(html).toContain('Current prices are loaded')
  expect(html).toContain('when the next odds snapshot arrives')
})
