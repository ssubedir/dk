import { memo } from 'react'

export const TeamCell = memo(function TeamCell({ name, designation }: { name: string; designation: string }) {
  const abbreviation = name
    .split(' ')
    .map((part) => part[0])
    .join('')
    .slice(0, 3)
    .toUpperCase()

  return (
    <th className="team" scope="row">
      <span className="team-logo" aria-hidden="true">{abbreviation}</span>
      <span>
        <strong>{name}</strong>
        <small>{designation}</small>
      </span>
    </th>
  )
})
