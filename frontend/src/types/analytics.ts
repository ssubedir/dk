export type AnalyticsSummary = {
  hours: number
  observations: number
  priceChanges: number
  games: number
  timedObservations: number
  clockSkewedObservations: number
  sseFlushSamples: number
  averageGoToSseFlushMs?: number
  p50GoToSseFlushMs?: number
  p99GoToSseFlushMs?: number
  firstObservedAt?: string
  lastObservedAt?: string
  averageSourceToGoMs?: number
  p50SourceToGoMs?: number
  p99SourceToGoMs?: number
  history: {
    storedObservations: number
    capacity: number
    evictedObservations: number
  }
}
