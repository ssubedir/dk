import { flattenPrices } from './oddsMovement'
import type { Game, Price } from './oddsMovement'

export type OddsPoint = {
  at: number
  payout: number
  american: string
}

export type OddsHistory = Record<string, OddsPoint[]>

const HISTORY_WINDOW_MS = 5 * 60 * 1000
const MAX_POINTS = 60

export function isWaitingForOddsUpdate(hasData: boolean, gameCount: number, history: OddsHistory) {
  if (!hasData) return true
  if (gameCount === 0) return false
  return !Object.values(history).some((points) => points.length >= 2)
}

export function appendOddsHistory(
  history: OddsHistory,
  games: Game[],
  fetchedAt: string,
): OddsHistory {
  const at = Date.parse(fetchedAt)
  if (!Number.isFinite(at)) return history

  const next: OddsHistory = {}
  const cutoff = at - HISTORY_WINDOW_MS
  for (const [key, price] of Object.entries(flattenPrices(games))) {
    const payout = decimalPayout(price)
    if (payout === null) continue

    const previous = history[key] ?? []
    const last = previous.at(-1)
    if (last && at <= last.at) {
      next[key] = previous.filter((point) => point.at >= cutoff)
      continue
    }

    next[key] = [
      ...previous.filter((point) => point.at >= cutoff),
      { at, payout, american: price.american },
    ].slice(-MAX_POINTS)
  }
  return next
}

function decimalPayout(price: Price): number | null {
  const decimal = Number(price.decimal)
  if (price.decimal && Number.isFinite(decimal) && decimal > 1) return decimal

  const american = price.american.trim().replace('−', '-').toUpperCase()
  if (american === 'EVEN' || american === 'EVS') return 2
  if (!/^[+-]?\d+$/.test(american)) return null
  const value = Number(american)
  if (value >= 100) return 1 + value / 100
  if (value <= -100) return 1 + 100 / -value
  return null
}
