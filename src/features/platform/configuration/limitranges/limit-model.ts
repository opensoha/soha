import type { LimitRangeRule } from './types'

export const limitFields = [
  'min',
  'max',
  'defaultRequest',
  'default',
  'maxLimitRequestRatio',
] as const
export type LimitField = (typeof limitFields)[number]
export interface LimitRow {
  key: string
  resource: string
  index: number
  rule: LimitRangeRule
}

export function limitGroups(rules: readonly LimitRangeRule[]) {
  const groups = new Map<string, { type: string; rules: LimitRangeRule[]; rows: LimitRow[] }>()
  rules.forEach((rule, index) => {
    const type = rule.type || 'Unknown'
    const group = groups.get(type) || { type, rules: [], rows: [] }
    group.rules.push(rule)
    const resources = [
      ...new Set(limitFields.flatMap((field) => Object.keys(rule[field] || {}))),
    ].sort()
    group.rows.push(
      ...resources.map((resource) => ({
        key: index + '/' + resource,
        index: index + 1,
        resource,
        rule,
      })),
    )
    groups.set(type, group)
  })
  const order = ['Container', 'Pod', 'PersistentVolumeClaim']
  return [...groups.values()].sort((a, b) => {
    const rank = (type: string) => (order.includes(type) ? order.indexOf(type) : order.length)
    return rank(a.type) - rank(b.type) || a.type.localeCompare(b.type)
  })
}
