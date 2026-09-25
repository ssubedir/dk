import type { Game, MoveTiming } from '../domain/oddsMovement'

export type OddsResponse = {
  source: string
  league: string
  fetchedAt: string
  updatedAt?: string
  fetchDurationMs?: number
  stale: boolean
  lastError?: string
  updateSource?: 'rest' | 'websocket'
  move?: MoveTiming
  moves?: Record<string, MoveTiming>
  games: Game[]
}

export type CellLatency = {
  moveKey: string
  ms: number | null
  // null = waiting for a live paint; undefined = this move was not measured.
  browserMs?: number | null
  browserBackground?: boolean
  replay?: boolean
}
