import type { MoveTiming } from '../domain/oddsMovement'

export function moveKey(move: MoveTiming, priceKey: string): string {
  return `${priceKey}:${move.goObservedAt}:${move.revision}`
}
