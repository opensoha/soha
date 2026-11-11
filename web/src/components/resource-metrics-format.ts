export function formatBytes(value: number) {
  if (!Number.isFinite(value)) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let current = value
  let index = 0
  while (current >= 1024 && index < units.length - 1) {
    current /= 1024
    index += 1
  }
  return `${current >= 10 ? current.toFixed(0) : current.toFixed(1)} ${units[index]}`
}

export function formatMetricValue(value: number, unit: string) {
  if (!Number.isFinite(value)) return '-'
  switch (unit) {
    case 'bytes':
      return formatBytes(value)
    case 'bytes/s':
      return `${formatBytes(value)}/s`
    case 'cores':
      return value >= 1 ? `${value.toFixed(2)} cores` : `${(value * 1000).toFixed(0)} mCPU`
    case 'count':
      return `${value.toFixed(0)}`
    default:
      return `${value.toFixed(2)} ${unit}`.trim()
  }
}
