import { parseCpuValue, parseMemoryValue } from '@/features/platform/workloads-model'
import { Progress, Tooltip } from 'antd'
import type { Pod } from './types'

export function podResourceColumnWidth(resource: 'cpu' | 'memory', pods: readonly Pod[]) {
  const characters = pods.reduce(
    (longest, pod) => Math.max(longest, formatPodResourceValue(resource, pod[resource]).length),
    6,
  )
  // Fixed 80px rail + 8px gap + content allowance for the 12px supporting font + cell padding.
  return 80 + 8 + characters * 8 + 24
}

function formatPodResourceValue(resource: 'cpu' | 'memory', value?: string) {
  const amount = getPodResourceAmount(resource, value)
  if (!Number.isFinite(amount) || amount < 0) return '—'
  if (resource === 'cpu') return `${Number((amount * 1000).toFixed(2))}m`
  const [unit, divisor] =
    amount >= 1024 ** 3
      ? ['Gi', 1024 ** 3]
      : amount >= 1024 ** 2
        ? ['Mi', 1024 ** 2]
        : amount >= 1024
          ? ['Ki', 1024]
          : ['B', 1]
  return `${Number((amount / Number(divisor)).toFixed(2))}${unit}`
}

export function getPodResourceAmount(resource: 'cpu' | 'memory', value?: string) {
  return resource === 'cpu' ? parseCpuValue(value) : parseMemoryValue(value)
}

export function PodResourceCell({
  pod: record,
  resource,
  localeCode,
}: {
  pod: Pod
  resource: 'cpu' | 'memory'
  localeCode: 'zh_CN' | 'en_US'
}) {
  const usageValue = resource === 'cpu' ? record.cpu : record.memory
  const requestValue = record.requests?.[resource]
  const limitValue = record.limits?.[resource]
  const usageAmount = getPodResourceAmount(resource, usageValue)
  const requestAmount = getPodResourceAmount(resource, requestValue)
  const limitAmount = getPodResourceAmount(resource, limitValue)
  const hasUsage = Number.isFinite(usageAmount) && usageAmount >= 0
  const hasRequest = Number.isFinite(requestAmount) && requestAmount >= 0
  const hasLimit = Number.isFinite(limitAmount) && limitAmount > 0
  const baselineAmount = hasLimit ? limitAmount : hasRequest ? requestAmount : 0
  const hasBaseline = baselineAmount > 0
  const percent = hasUsage && hasBaseline ? Math.min(100, (usageAmount / baselineAmount) * 100) : 0
  const label = resource === 'cpu' ? 'CPU' : localeCode === 'zh_CN' ? '内存' : 'Memory'
  const usageLabel = formatPodResourceValue(resource, usageValue)
  const unsetLabel = localeCode === 'zh_CN' ? '未设置' : 'Not set'
  const missingLabel = localeCode === 'zh_CN' ? '暂无指标' : 'No metrics'
  const requestLabel = hasRequest ? formatPodResourceValue(resource, requestValue) : unsetLabel
  const limitLabel = hasLimit ? formatPodResourceValue(resource, limitValue) : unsetLabel
  const usageText = localeCode === 'zh_CN' ? '使用' : 'Usage'
  const requestText = localeCode === 'zh_CN' ? '请求' : 'Request'
  const limitText = localeCode === 'zh_CN' ? '限制' : 'Limit'
  const tone =
    !hasUsage || !hasBaseline
      ? 'unknown'
      : hasLimit && usageAmount / limitAmount >= 0.9
        ? 'danger'
        : hasRequest && usageAmount > requestAmount
          ? 'warning'
          : 'success'
  const statusText =
    tone === 'danger'
      ? localeCode === 'zh_CN'
        ? '接近或达到限制'
        : 'Near or above limit'
      : tone === 'warning'
        ? localeCode === 'zh_CN'
          ? '超过请求'
          : 'Above request'
        : tone === 'success'
          ? localeCode === 'zh_CN'
            ? hasRequest
              ? '请求以内'
              : '低于限制的 90%'
            : hasRequest
              ? 'Within request'
              : 'Below 90% of limit'
          : ''
  const baselineText = hasLimit
    ? ''
    : hasBaseline
      ? localeCode === 'zh_CN'
        ? '以请求为比例基准'
        : 'Relative to request'
      : localeCode === 'zh_CN'
        ? '无比例基准'
        : 'No comparison baseline'
  const tooltipTitle = [
    label,
    `${usageText} ${hasUsage ? usageLabel : missingLabel}`,
    `${requestText} ${requestLabel}`,
    `${limitText} ${limitLabel}`,
    baselineText,
    statusText,
  ]
    .filter(Boolean)
    .join(' · ')
  const rail = (
    <div className="soha-pod-resource-progress-wrap">
      <Progress
        aria-hidden
        percent={percent}
        showInfo={false}
        size={['100%', 4]}
        strokeColor="var(--soha-pod-resource-fill)"
        strokeLinecap="round"
        railColor="var(--soha-fill-medium)"
        styles={{
          root: { display: 'flex', margin: 0, lineHeight: 0 },
          rail: { borderRadius: 'calc(var(--soha-radius-control) / 2)' },
          track: { borderRadius: 'calc(var(--soha-radius-control) / 2)', transition: 'none' },
        }}
      />
    </div>
  )

  return (
    <Tooltip title={tooltipTitle} placement="top" trigger={['hover', 'focus', 'click']}>
      <button
        type="button"
        className={`soha-pod-resource-limit-cell is-${tone}`}
        aria-label={`${record.name} · ${tooltipTitle}`}
      >
        {rail}
        <span className="soha-pod-resource-value" aria-hidden>
          {usageLabel}
        </span>
      </button>
    </Tooltip>
  )
}
