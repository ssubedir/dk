import { expect, test } from 'bun:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { PriceCell } from '../src/components/PriceCell'

test('shows distinct SSE and browser timing beside the quote chart', () => {
  const html = renderToStaticMarkup(<PriceCell
    price={{ american: '-110' }}
    changed={false}
    latency={{ moveKey: 'game:away:moneyline:observed:2', ms: 0.2, browserMs: 18 }}
  />)
  expect(html).toContain('SSE 0.20ms')
  expect(html).toContain('UI 18ms')
  expect(html).toContain('SSE event callback to page paint opportunity')
})

test('separates the previous line and odds while leaving moneyline alone', () => {
  const changedAt = Date.now() - 12_000
  const spread = renderToStaticMarkup(<PriceCell
    price={{ line: 2.5, american: '-110' }}
    changed={true}
    movement={{ previous: { line: 3.5, american: '+105' }, direction: 'worse', changedAt }}
  />)
  expect(spread).toMatch(/was \+3\.5, \+105 · \d+s ago/)
  expect(spread).toContain('previous price +3.5, +105')

  const total = renderToStaticMarkup(<PriceCell
    price={{ line: 44.5, american: '-110' }}
    prefix="O"
    changed={true}
    movement={{ previous: { line: 45.5, american: '+100' }, direction: 'worse', changedAt }}
  />)
  expect(total).toMatch(/was O 45\.5, \+100 · \d+s ago/)

  const moneyline = renderToStaticMarkup(<PriceCell
    price={{ american: '-120' }}
    changed={true}
    movement={{ previous: { american: '-110' }, direction: 'worse', changedAt }}
  />)
  expect(moneyline).toMatch(/was -110 · \d+s ago/)
})
