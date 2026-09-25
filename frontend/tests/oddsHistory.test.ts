import { expect, test } from 'bun:test'
import { appendOddsHistory } from '../src/domain/oddsHistory'
import type { Game, Price } from '../src/domain/oddsMovement'

function game(price: Price): Game {
  return {
    id: 'game-1',
    startsAt: '2026-09-24T00:15:00Z',
    away: { name: 'Away', moneyline: price },
    home: { name: 'Home' },
    total: {},
  }
}

test('records repeated checks as a flat payout trend and captures a real change', () => {
  const first = appendOddsHistory({}, [game({ american: '-110' })], '2026-09-22T12:00:00Z')
  const flat = appendOddsHistory(first, [game({ american: '-110' })], '2026-09-22T12:00:05Z')
  const changed = appendOddsHistory(flat, [game({ american: '+100' })], '2026-09-22T12:00:10Z')
  const points = changed['game-1:away:moneyline']

  expect(points).toHaveLength(3)
  expect(points[0].payout).toBeCloseTo(1.909, 3)
  expect(points[1].payout).toBeCloseTo(points[0].payout, 6)
  expect(points[2].payout).toBe(2)
})

test('ignores repeated timestamps and trims old checks', () => {
  const first = appendOddsHistory({}, [game({ american: '+120', decimal: '2.20' })], '2026-09-22T12:00:00Z')
  const duplicate = appendOddsHistory(first, [game({ american: '+130' })], '2026-09-22T12:00:00Z')
  expect(duplicate['game-1:away:moneyline']).toHaveLength(1)

  const later = appendOddsHistory(duplicate, [game({ american: '+130' })], '2026-09-22T12:06:00Z')
  expect(later['game-1:away:moneyline']).toEqual([{
    at: Date.parse('2026-09-22T12:06:00Z'),
    payout: 2.3,
    american: '+130',
  }])
})
