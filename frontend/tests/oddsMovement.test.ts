import { expect, test } from 'bun:test'
import { findMovements, movePriceKey, timedMovesForChanges, type Game, type Price } from '../src/domain/oddsMovement'

function game(prices: {
  awayMoneyline?: Price
  awaySpread?: Price
  over?: Price
  under?: Price
}): Game {
  return {
    id: 'game-1',
    startsAt: '2026-09-24T00:15:00Z',
    away: { name: 'Away', moneyline: prices.awayMoneyline, spread: prices.awaySpread },
    home: { name: 'Home' },
    total: { over: prices.over, under: prices.under },
  }
}

test('does not claim a move on the first snapshot or when a quote first appears', () => {
  const current = game({ awayMoneyline: { american: '-110' } })
  expect(findMovements(null, [current])).toEqual({})
  expect(findMovements([game({})], [current])).toEqual({})
})

test('assigns a WebSocket move only to its changed quote', () => {
  const current = game({ awaySpread: { line: 1.5, american: '-110' }, under: { line: 8.5, american: '-105' } })
  const changed = ['game-1:away:spread', 'game-1:home:spread', 'game-1:total:under']
  expect(movePriceKey({ gameId: 'game-1', label: 'Away', market: 'Spread' }, [current], changed)).toBe('game-1:away:spread')
  expect(movePriceKey({ gameId: 'game-1', label: 'Under', market: 'Total under' }, [current], changed)).toBe('game-1:total:under')
  expect(movePriceKey({ gameId: 'game-1', label: 'Away', market: 'Moneyline' }, [current], changed)).toBeNull()
  expect(movePriceKey({ gameId: 'game-1', label: 'Away', market: 'Spread' }, [current], ['game-1:home:spread'])).toBeNull()
})

test('uses a unique changed market as fallback but rejects ambiguous attribution', () => {
  const current = game({ awayMoneyline: { american: '-110' } })
  expect(movePriceKey({ gameId: 'game-1', label: 'Away variant', market: 'Moneyline' }, [current], ['game-1:away:moneyline'])).toBe('game-1:away:moneyline')
  expect(movePriceKey({ gameId: 'game-1', label: 'Unknown', market: 'Spread' }, [current], ['game-1:away:spread', 'game-1:home:spread'])).toBeNull()
})

test('retains separate timings when multiple quotes arrive in one visible snapshot', () => {
  const current = game({ awayMoneyline: { american: '-115' }, under: { line: 8.5, american: '-105' } })
  const away = { revision: 2, gameId: 'game-1', label: 'Away', market: 'Moneyline', goObservedAt: '2026-09-23T02:00:00Z' }
  const under = { revision: 3, gameId: 'game-1', label: 'Under', market: 'Total under', goObservedAt: '2026-09-23T02:00:01Z' }
  const changes = ['game-1:away:moneyline', 'game-1:total:under']
  expect(timedMovesForChanges([current], changes, {
    'game-1:away:moneyline': away,
    'game-1:total:under': under,
  }, under)).toEqual({
    'game-1:away:moneyline': away,
    'game-1:total:under': under,
  })
  expect(timedMovesForChanges([current], changes, {
    'game-1:away:moneyline': under,
  })).toEqual({})
})

test('uses payout for unchanged lines', () => {
  const previous = game({ awayMoneyline: { american: '-110' } })
  const better = game({ awayMoneyline: { american: '-105' } })
  const worse = game({ awayMoneyline: { american: '-120' } })
  expect(findMovements([previous], [better])['game-1:away:moneyline'].direction).toBe('better')
  expect(findMovements([previous], [worse])['game-1:away:moneyline'].direction).toBe('worse')
  expect(findMovements([previous], [better], 1234)['game-1:away:moneyline'].changedAt).toBe(1234)
})

test('uses the selection-specific direction for spread and total lines', () => {
  const previous = game({
    awaySpread: { line: 3.5, american: '-110' },
    over: { line: 44, american: '-110' },
    under: { line: 44, american: '-110' },
  })
  const current = game({
    awaySpread: { line: 4, american: '-110' },
    over: { line: 43.5, american: '-110' },
    under: { line: 45, american: '-110' },
  })
  const moves = findMovements([previous], [current])
  expect(moves['game-1:away:spread'].direction).toBe('better')
  expect(moves['game-1:total:over'].direction).toBe('better')
  expect(moves['game-1:total:under'].direction).toBe('better')
})

test('does not call a better line with worse payout an unqualified improvement', () => {
  const previous = game({ awaySpread: { line: 3.5, american: '-110' } })
  const current = game({ awaySpread: { line: 4, american: '-120' } })
  expect(findMovements([previous], [current])['game-1:away:spread'].direction).toBe('mixed')
})
