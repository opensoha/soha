const suffixes: Record<string, number> = {
  '': 1,
  n: 1e-9,
  u: 1e-6,
  m: 1e-3,
  k: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
  P: 1e15,
  E: 1e18,
  Ki: 1024,
  Mi: 1024 ** 2,
  Gi: 1024 ** 3,
  Ti: 1024 ** 4,
  Pi: 1024 ** 5,
  Ei: 1024 ** 6,
}

export function parseResourceQuantity(raw: string | undefined): number | null {
  if (raw === undefined) return null
  const match = raw
    .trim()
    .match(/^([+]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+))([eE][+-]?[0-9]+|[numkMGTPE]|[KMGTPE]i)?$/)
  if (!match) return null
  const suffix = match[2] || ''
  const factor = /^[eE][+-]?[0-9]+$/.test(suffix) ? 10 ** Number(suffix.slice(1)) : suffixes[suffix]
  if (factor === undefined) return null
  const value = Number(match[1]) * factor
  return Number.isFinite(value) && value >= 0 ? value : null
}

const number = (value: number) =>
  new Intl.NumberFormat('en-US', { maximumSignificantDigits: 4 }).format(value)

export function formatResourceQuantity(resource: string, value: number, zh = true): string {
  if (/(^|\.)cpu$/.test(resource))
    return value > 0 && value < 1
      ? number(value * 1000) + 'm'
      : number(value) + (zh ? ' 核' : ' cores')
  if (/(^|\.)(memory|storage|ephemeral-storage)$/.test(resource)) {
    const units = ['B', 'Ki', 'Mi', 'Gi', 'Ti', 'Pi', 'Ei']
    const index =
      value > 0 ? Math.min(6, Math.max(0, Math.floor(Math.log(value) / Math.log(1024)))) : 0
    return number(value / 1024 ** index) + ' ' + units[index]
  }
  return number(value)
}
