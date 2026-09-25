export function LoadingState() {
  return (
    <div className="loading-panel" aria-label="Loading odds">
      <div className="skeleton title" />
      {[0, 1, 2, 3].map((item) => (
        <div className="skeleton row" key={item} />
      ))}
    </div>
  )
}
