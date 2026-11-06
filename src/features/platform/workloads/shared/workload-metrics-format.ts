import { formatMetricValue } from '@/components/resource-metrics-format'

export function formatWorkloadMetricValue(value: number, unit: string) {
  if (unit === 'cores' && Number.isFinite(value) && value !== 0 && Math.abs(value) < 0.001)
    return `${Number((value * 1000).toPrecision(3))} mCPU`
  return formatMetricValue(value, unit)
}
