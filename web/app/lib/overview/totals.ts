import { isServing } from '~/lib/states'

export interface PoolTotals {
  pools: number
  maintenance: number
  /** Instances serving capacity (Ready or Allocated). */
  instancesReady: number
  instancesTotal: number
  players: number
}

/** Totals of the overview: pools, serving instances and players online (summed over all instances). */
export function poolTotals(
  pools: readonly { maintenance: boolean }[],
  instances: readonly { state: string, players: number }[],
): PoolTotals {
  return {
    pools: pools.length,
    maintenance: pools.filter(p => p.maintenance).length,
    instancesReady: instances.filter(i => isServing(i.state)).length,
    instancesTotal: instances.length,
    players: instances.reduce((sum, i) => sum + (Number.isFinite(i.players) ? i.players : 0), 0),
  }
}

/** Players online of one pool, summed from its instances. */
export function playersOf(pool: string, instances: readonly { pool: string, players: number }[]): number {
  return instances.reduce((sum, i) => (i.pool === pool && Number.isFinite(i.players) ? sum + i.players : sum), 0)
}
