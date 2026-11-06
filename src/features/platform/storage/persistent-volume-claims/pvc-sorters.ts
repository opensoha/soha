import { parseResourceQuantity } from '../../configuration/shared/resource-quantity'

export function compareRequestedStorage(a: string | undefined, b: string | undefined): number {
  const left = parseResourceQuantity(a)
  const right = parseResourceQuantity(b)
  // Keep unknown quantities separate from real zero; compare their original labels consistently.
  if (left === null && right === null) return (a ?? '').localeCompare(b ?? '')
  if (left === null) return 1
  if (right === null) return -1
  return left - right
}
