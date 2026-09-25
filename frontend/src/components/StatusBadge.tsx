export function StatusBadge({ stale, loading, unavailable }: { stale: boolean; loading: boolean; unavailable: boolean }) {
  const label = loading ? 'Connecting' : unavailable ? 'Unavailable' : stale ? 'Data stale' : 'Live updates'
  return (
    <span className={`status-badge${stale || unavailable ? ' stale' : ''}`}>
      <span aria-hidden="true" />
      {label}
    </span>
  )
}
