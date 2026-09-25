import { memo } from 'react'
import type { OddsPoint } from '../domain/oddsHistory'

export const PriceSparkline = memo(function PriceSparkline({ points }: { points?: OddsPoint[] }) {
  const width = 86
  const height = 34
  const padding = 4

  if (!points || points.length < 2) {
    return (
      <svg className="sparkline empty" viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Collecting price history">
        <title>Waiting for another price update</title>
        <path d={`M${padding} ${height / 2}H${width - padding}`} />
      </svg>
    )
  }

  const first = points[0]
  const last = points[points.length - 1]
  const values = points.map((point) => point.payout)
  const min = Math.min(...values)
  const max = Math.max(...values)
  const margin = Math.max((max - min) * 0.25, 0.01)
  const low = min - margin
  const high = max + margin
  const x = (point: OddsPoint) => padding + ((point.at - first.at) / (last.at - first.at)) * (width - padding * 2)
  const y = (point: OddsPoint) => height - padding - ((point.payout - low) / (high - low)) * (height - padding * 2)
  const path = points.reduce((result, point, index) => {
    const currentX = x(point).toFixed(1)
    const currentY = y(point).toFixed(1)
    return index === 0
      ? `M${currentX} ${currentY}`
      : `${result} H${currentX} V${currentY}`
  }, '')
  const direction = last.payout > first.payout ? 'up' : last.payout < first.payout ? 'down' : 'flat'
  const label = `Recent payout odds: ${first.american} to ${last.american} across ${points.length} checks`

  return (
    <svg className={`sparkline ${direction}`} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label}>
      <title>{label}</title>
      <path d={path} />
      <circle cx={x(last)} cy={y(last)} r="2.5" />
    </svg>
  )
})
