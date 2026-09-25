export function WaitingForOddsNotice({ hasInitialOdds }: { hasInitialOdds: boolean }) {
  return (
    <div className="waiting-notice" role="status" aria-live="polite">
      <div className="waiting-notice-content">
        <span className="waiting-indicator" aria-hidden="true" />
        <div>
          <strong>{hasInitialOdds ? 'Waiting for another price update' : 'Waiting for live odds'}</strong>
          <p>
            {hasInitialOdds
              ? 'Current prices are loaded. Linewatch will update automatically when the next odds snapshot arrives.'
              : 'Linewatch is listening for the first odds snapshot and will update automatically.'}
          </p>
        </div>
      </div>
    </div>
  )
}
