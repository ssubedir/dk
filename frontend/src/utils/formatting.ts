import type { MovementDirection, Price } from '../domain/oddsMovement'

export function formatLine(line: number): string {
  return line > 0 ? `+${line}` : String(line)
}

export function formatPrice(price: Price, prefix?: string): string {
  const odds = price.american || '—'
  if (price.line === undefined) return odds
  const line = prefix ? `${prefix} ${price.line}` : formatLine(price.line)
  return `${line}, ${odds}`
}

export function movementLabel(direction: MovementDirection): string {
  switch (direction) {
    case 'better': return 'Better for this pick'
    case 'worse': return 'Worse for this pick'
    case 'mixed': return 'Line and payout moved in opposite directions'
    case 'changed': return 'Price changed'
  }
}

export function movementArrow(direction: MovementDirection): string {
  if (direction === 'better') return '↑'
  if (direction === 'worse') return '↓'
  return '↔'
}

export function formatGameTime(value: string): string {
  return new Intl.DateTimeFormat('en-US', {
    hour: 'numeric',
    minute: '2-digit',
  }).format(new Date(value))
}

export function formatGameDate(value: string): string {
  return new Intl.DateTimeFormat('en-US', {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
  }).format(new Date(value))
}
