import { Fragment, memo } from 'react'
import type { OddsHistory } from '../domain/oddsHistory'
import type { Game, MovementMap } from '../domain/oddsMovement'
import type { CellLatency } from '../types/odds'
import { formatGameDate, formatGameTime } from '../utils/formatting'
import { PriceCell } from './PriceCell'
import { TeamCell } from './TeamCell'

export const OddsTable = memo(function OddsTable({
  league,
  games,
  changedPrices,
  movements,
  history,
  priceLatencies,
}: {
  league: string
  games: Game[]
  changedPrices: Set<string>
  movements: MovementMap
  history: OddsHistory
  priceLatencies: Record<string, CellLatency>
}) {
  return (
    <section className="odds-panel" aria-label={`Upcoming ${league} odds`}>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th scope="col">Kickoff</th>
              <th scope="col">Matchup</th>
              <th scope="col">Spread</th>
              <th scope="col">Total</th>
              <th scope="col">Moneyline</th>
            </tr>
          </thead>
          <tbody>
            {games.map((game) => (
              <Fragment key={game.id}>
                <tr className="away-row">
                  <td className="matchup" rowSpan={2}>
                    <time dateTime={game.startsAt}>{formatGameTime(game.startsAt)}</time>
                    <span>{formatGameDate(game.startsAt)}</span>
                  </td>
                  <TeamCell name={game.away.name} designation="AWAY" />
                  <PriceCell
                    price={game.away.spread}
                    changed={changedPrices.has(`${game.id}:away:spread`)}
                    movement={movements[`${game.id}:away:spread`]}
                    history={history[`${game.id}:away:spread`]}
                    latency={priceLatencies[`${game.id}:away:spread`]}
                  />
                  <PriceCell
                    price={game.total.over}
                    prefix="O"
                    changed={changedPrices.has(`${game.id}:total:over`)}
                    movement={movements[`${game.id}:total:over`]}
                    history={history[`${game.id}:total:over`]}
                    latency={priceLatencies[`${game.id}:total:over`]}
                  />
                  <PriceCell
                    price={game.away.moneyline}
                    changed={changedPrices.has(`${game.id}:away:moneyline`)}
                    movement={movements[`${game.id}:away:moneyline`]}
                    history={history[`${game.id}:away:moneyline`]}
                    latency={priceLatencies[`${game.id}:away:moneyline`]}
                  />
                </tr>
                <tr className="home-row">
                  <TeamCell name={game.home.name} designation="HOME" />
                  <PriceCell
                    price={game.home.spread}
                    changed={changedPrices.has(`${game.id}:home:spread`)}
                    movement={movements[`${game.id}:home:spread`]}
                    history={history[`${game.id}:home:spread`]}
                    latency={priceLatencies[`${game.id}:home:spread`]}
                  />
                  <PriceCell
                    price={game.total.under}
                    prefix="U"
                    changed={changedPrices.has(`${game.id}:total:under`)}
                    movement={movements[`${game.id}:total:under`]}
                    history={history[`${game.id}:total:under`]}
                    latency={priceLatencies[`${game.id}:total:under`]}
                  />
                  <PriceCell
                    price={game.home.moneyline}
                    changed={changedPrices.has(`${game.id}:home:moneyline`)}
                    movement={movements[`${game.id}:home:moneyline`]}
                    history={history[`${game.id}:home:moneyline`]}
                    latency={priceLatencies[`${game.id}:home:moneyline`]}
                  />
                </tr>
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
      <div className="table-note">
        <span className="pulse-dot" aria-hidden="true" />
        ↑ Better · ↓ Worse · ↔ Mixed/changed for this pick. Mini charts show recent payout odds; line changes appear in the quote. SSE measures Go observing an update → stream flush. UI measures the browser’s SSE callback → paint opportunity. Network transit between them is not measured.
      </div>
    </section>
  )
})
