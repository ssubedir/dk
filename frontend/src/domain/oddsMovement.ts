// Sportsbook-neutral quote and movement rules.
export type Price = {
  line?: number
  american: string
  decimal?: string
}

export type TeamOdds = {
  name: string
  spread?: Price
  moneyline?: Price
}

export type Game = {
  id: string
  startsAt: string
  away: TeamOdds
  home: TeamOdds
  total: {
    over?: Price
    under?: Price
  }
}

export type MovementDirection = 'better' | 'worse' | 'mixed' | 'changed'

export type Movement = {
  previous: Price
  direction: MovementDirection
  changedAt: number
}

export type MovementMap = Record<string, Movement>

export type MoveIdentity = {
  gameId: string
  label: string
  market: string
}

export type MoveTiming = MoveIdentity & {
  revision: number
  goObservedAt: string
  draftKingsPublishedAt?: string
  draftKingsToGoMs?: number
}

// A dropped SSE frame or a batched React render can leave several changed
// prices in one visible snapshot. Match each price to its own Go revision.
export function timedMovesForChanges(
  games: Game[],
  changedKeys: string[],
  moves?: Record<string, MoveTiming>,
  latestMove?: MoveTiming,
): Record<string, MoveTiming> {
  const timed: Record<string, MoveTiming> = {}
  for (const key of changedKeys) {
    const move = moves?.[key]
    if (move && movePriceKey(move, games, [key]) === key) timed[key] = move
  }
  if (latestMove) {
    const key = movePriceKey(latestMove, games, changedKeys)
    if (key && !timed[key]) timed[key] = latestMove
  }
  return timed
}

// A coalesced SSE snapshot can contain several changed quotes, but its timing
// record names only the last upstream selection. Do not assign it to another
// price merely because that price also changed in the snapshot.
export function movePriceKey(move: MoveIdentity, games: Game[], changedKeys: string[]): string | null {
  const game = games.find((entry) => entry.id === move.gameId)
  if (!game) return null

  const prefix = `${game.id}:`
  const changed = new Set(changedKeys)
  if (move.market === 'Total over' || move.market === 'Total under') {
    const side = move.market === 'Total over' ? 'over' : 'under'
    const key = `${prefix}total:${side}`
    return changed.has(key) ? key : null
  }

  const market = move.market === 'Moneyline' ? 'moneyline' : move.market === 'Spread' ? 'spread' : null
  if (!market) return null
  const label = move.label.trim().replace(/\s+/g, ' ').toLowerCase()
  const away = game.away.name.trim().replace(/\s+/g, ' ').toLowerCase()
  const home = game.home.name.trim().replace(/\s+/g, ' ').toLowerCase()
  const side = label === away ? 'away' : label === home ? 'home' : null
  if (side) {
    const key = `${prefix}${side}:${market}`
    return changed.has(key) ? key : null
  }

  const candidates = changedKeys.filter((key) => key.startsWith(prefix) && key.endsWith(`:${market}`))
  return candidates.length === 1 ? candidates[0] : null
}

export function flattenPrices(games: Game[]): Record<string, Price> {
  const prices: Record<string, Price> = {}
  for (const game of games) {
    if (game.away.spread) prices[`${game.id}:away:spread`] = game.away.spread
    if (game.home.spread) prices[`${game.id}:home:spread`] = game.home.spread
    if (game.away.moneyline) prices[`${game.id}:away:moneyline`] = game.away.moneyline
    if (game.home.moneyline) prices[`${game.id}:home:moneyline`] = game.home.moneyline
    if (game.total.over) prices[`${game.id}:total:over`] = game.total.over
    if (game.total.under) prices[`${game.id}:total:under`] = game.total.under
  }
  return prices
}

export function findMovements(previous: Game[] | null, next: Game[], changedAt = Date.now()): MovementMap {
  if (!previous) return {}

  const before = flattenPrices(previous)
  const after = flattenPrices(next)
  const movements: MovementMap = {}
  for (const [key, current] of Object.entries(after)) {
    const old = before[key]
    if (old && priceKey(old) !== priceKey(current)) {
      movements[key] = {
        previous: old,
        direction: movementDirection(key, old, current),
        changedAt,
      }
    }
  }
  return movements
}

function priceKey(price: Price): string {
  return `${price.line ?? ''}|${price.american}|${price.decimal ?? ''}`
}

function movementDirection(key: string, old: Price, current: Price): MovementDirection {
  const payoutChange = comparePayout(old, current)

  if (old.line !== current.line) {
    if (old.line === undefined || current.line === undefined) return 'changed'
    const lineChange = Math.sign(current.line - old.line) * (key.endsWith(':total:over') ? -1 : 1)
    if (payoutChange !== 0 && payoutChange !== null && payoutChange !== lineChange) {
      return 'mixed'
    }
    return lineChange > 0 ? 'better' : 'worse'
  }

  if (payoutChange === null || payoutChange === 0) return 'changed'
  return payoutChange > 0 ? 'better' : 'worse'
}

function comparePayout(old: Price, current: Price): number | null {
  const oldAmerican = parseAmerican(old.american)
  const currentAmerican = parseAmerican(current.american)
  if (oldAmerican !== null && currentAmerican !== null) {
    return Math.sign(currentAmerican - oldAmerican)
  }

  const oldDecimal = Number(old.decimal)
  const currentDecimal = Number(current.decimal)
  if (old.decimal && current.decimal && Number.isFinite(oldDecimal) && Number.isFinite(currentDecimal)) {
    return Math.sign(currentDecimal - oldDecimal)
  }
  return null
}

function parseAmerican(value: string): number | null {
  const normalized = value.trim().replace('−', '-').toUpperCase()
  if (normalized === 'EVEN' || normalized === 'EVS') return 100
  if (!/^[+-]?\d+$/.test(normalized)) return null
  return Number(normalized)
}
