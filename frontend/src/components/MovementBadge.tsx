import { useSyncExternalStore } from 'react'
import { formatMovementAge } from '../domain/movementAge'
import type { Movement } from '../domain/oddsMovement'
import { formatPrice, movementArrow, movementLabel } from '../utils/formatting'

let clockNow = Date.now()
let timer: number | undefined
const listeners = new Set<() => void>()

function tick() {
  clockNow = Date.now()
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  if (listeners.size === 1) {
    timer = window.setInterval(tick, 1000)
    document.addEventListener('visibilitychange', tick)
    tick()
  }
  return () => {
    listeners.delete(listener)
    if (listeners.size === 0) {
      window.clearInterval(timer)
      timer = undefined
      document.removeEventListener('visibilitychange', tick)
    }
  }
}

function getClock() {
  return clockNow
}

export function MovementBadge({ movement, prefix }: { movement: Movement; prefix?: string }) {
  const now = useSyncExternalStore(subscribe, getClock, getClock)
  const age = formatMovementAge(movement.changedAt, now)
  const previousPrice = formatPrice(movement.previous, prefix)
  return (
    <span
      className={`price-movement ${movement.direction}`}
      aria-label={`${movementLabel(movement.direction)}; previous price ${previousPrice}; ${age}`}
      title={movementLabel(movement.direction)}
    >
      <span className="movement-arrow" aria-hidden="true">{movementArrow(movement.direction)}</span>
      <span aria-hidden="true">was {previousPrice} · {age}</span>
    </span>
  )
}
