import { describe, expect, it } from 'vitest'
import { playersOf, poolTotals } from './totals'

describe('poolTotals', () => {
  it('counts pools, maintenance and serving instances', () => {
    const totals = poolTotals(
      [{ maintenance: false }, { maintenance: true }],
      [
        { state: 'Ready', players: 3 },
        { state: 'Allocated', players: 5 },
        { state: 'Starting', players: 0 },
      ],
    )
    expect(totals).toEqual({ pools: 2, maintenance: 1, instancesReady: 2, instancesTotal: 3, players: 8 })
  })

  it('is all zeros for an empty controller', () => {
    expect(poolTotals([], [])).toEqual({ pools: 0, maintenance: 0, instancesReady: 0, instancesTotal: 0, players: 0 })
  })

  it('ignores a player count that is not a number', () => {
    expect(poolTotals([], [{ state: 'Ready', players: Number.NaN }]).players).toBe(0)
  })
})

describe('playersOf', () => {
  it('sums the players of one pool only', () => {
    const instances = [
      { pool: 'lobby', players: 4 },
      { pool: 'bedwars', players: 10 },
      { pool: 'lobby', players: 1 },
    ]
    expect(playersOf('lobby', instances)).toBe(5)
    expect(playersOf('skyblock', instances)).toBe(0)
  })
})
