import type { PortForwardSession } from './types'
export const validForwardPort = (value: unknown): value is number =>
  typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= 65535
export function hasKnownPortConflict(items: readonly PortForwardSession[], port: unknown) {
  return (
    validForwardPort(port) &&
    items.some(
      (item) =>
        item.localPort === port && ['active', 'starting'].includes(item.status.toLowerCase()),
    )
  )
}
export const sessionCanStop = (status: string) =>
  !['stopped', 'closed', 'completed'].includes(status.toLowerCase())
