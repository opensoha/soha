import type { ResourceQuotaResource } from './types'
import {
  parseResourceQuantity as quotaQuantity,
  formatResourceQuantity as formatQuotaAmount,
} from '../shared/resource-quantity'
export { quotaQuantity, formatQuotaAmount }

const number = (value: number) =>
  new Intl.NumberFormat('en-US', { maximumSignificantDigits: 4 }).format(value)

export function quotaRows(record: ResourceQuotaResource, configuredHard?: Record<string, string>) {
  return [
    ...new Set([
      ...Object.keys(record.hard || {}),
      ...Object.keys(record.used || {}),
      ...Object.keys(configuredHard || {}),
    ]),
  ]
    .sort()
    .map((resource) => {
      const hardRaw = record.hard?.[resource]
      const usedRaw = record.used?.[resource]
      const hard = quotaQuantity(hardRaw)
      const used = quotaQuantity(usedRaw)
      const percent =
        hard !== null && used !== null
          ? hard > 0
            ? (used / hard) * 100
            : used === 0
              ? 0
              : null
          : null
      return {
        resource,
        hardRaw,
        usedRaw,
        hard,
        used,
        configuredRaw: configuredHard?.[resource],
        percent: percent !== null && Number.isFinite(percent) ? percent : null,
        remaining: hard !== null && used !== null ? hard - used : null,
        over: hard !== null && used !== null && used > hard,
      }
    })
}

export type QuotaRow = ReturnType<typeof quotaRows>[number]

export function quotaPeak(record: ResourceQuotaResource) {
  let peak: QuotaRow | undefined
  let score = -1
  for (const row of quotaRows(record)) {
    const next = row.percent ?? (row.over ? Infinity : -1)
    if (next > score) {
      peak = row
      score = next
    }
  }
  return { row: peak, score }
}

export function quotaPercentLabel(row: QuotaRow, zh: boolean) {
  return row.percent === null
    ? row.over
      ? zh
        ? '超出配额'
        : 'Over quota'
      : '—'
    : number(row.percent) + '%'
}
