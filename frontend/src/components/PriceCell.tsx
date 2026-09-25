import { memo } from 'react'
import { describeBrowserLatency, describeLatency } from '../domain/latency'
import type { OddsPoint } from '../domain/oddsHistory'
import type { Movement, Price } from '../domain/oddsMovement'
import type { CellLatency } from '../types/odds'
import { formatLine } from '../utils/formatting'
import { MovementBadge } from './MovementBadge'
import { PriceSparkline } from './PriceSparkline'

export const PriceCell = memo(function PriceCell({
  price,
  prefix,
  changed,
  movement,
  history,
  latency,
}: {
  price?: Price
  prefix?: string
  changed: boolean
  movement?: Movement
  history?: OddsPoint[]
  latency?: CellLatency
}) {
  if (!price) {
    return <td className="price unavailable">—</td>
  }

  const hasLine = price.line !== undefined
  const serverTiming = latency ? describeLatency(latency.ms, latency.replay) : null
  const browserTiming = latency ? describeBrowserLatency(latency.browserMs, latency.browserBackground, latency.replay) : null
  return (
    <td className={`price${changed ? ' changed' : ''}${movement ? ` trend-${movement.direction}` : ''}`}>
      <div className="price-main">
        <div className="price-quote">
          {hasLine && (
            <strong>
              {prefix ? `${prefix} ` : ''}
              {prefix ? price.line : formatLine(price.line as number)}
            </strong>
          )}
          <span className={`current-odds${hasLine ? '' : ' standalone'}`}>{price.american || '—'}</span>
        </div>
        <div className="price-chart">
          <PriceSparkline points={history} />
          {serverTiming && browserTiming && (
            <span className="price-latencies">
              <span className="price-latency" title={serverTiming.title}>SSE {serverTiming.label}</span>
              <span className="price-latency" title={browserTiming.title}>UI {browserTiming.label}</span>
            </span>
          )}
        </div>
      </div>
      {movement && <MovementBadge movement={movement} prefix={prefix} />}
    </td>
  )
})
