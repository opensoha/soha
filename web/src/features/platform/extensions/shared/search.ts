export type SearchableValue = string | number | boolean | null | undefined

export function normalizeSearchKeyword(value: string) {
  return value.trim().toLowerCase()
}

export function includesSearch(values: SearchableValue[], keyword: string) {
  if (!keyword) return true
  return values.some((value) =>
    String(value ?? '')
      .toLowerCase()
      .includes(keyword),
  )
}
