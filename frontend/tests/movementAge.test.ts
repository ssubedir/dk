import { expect, test } from 'bun:test'
import { formatMovementAge } from '../src/domain/movementAge'

test('formats time since the browser received a price change', () => {
  const now = 100_000_000
  expect(formatMovementAge(now, now)).toBe('0s ago')
  expect(formatMovementAge(now - 12_000, now)).toBe('12s ago')
  expect(formatMovementAge(now - 65_000, now)).toBe('1m ago')
  expect(formatMovementAge(now - 3_600_000, now)).toBe('1h ago')
  expect(formatMovementAge(now - 86_400_000, now)).toBe('1d ago')
  expect(formatMovementAge(now + 1_000, now)).toBe('0s ago')
})
