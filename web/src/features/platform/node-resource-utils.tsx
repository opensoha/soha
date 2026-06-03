import { Progress, Typography } from 'antd'
import type { Node, NodeTaint } from '@/types'
import './platform-pages.css'

const { Text } = Typography

export function stringifyMap(value?: Record<string, string>) {
  return JSON.stringify(value ?? {}, null, 2)
}

export function parseStringMap(raw: unknown, field: string) {
  const value = typeof raw === 'string' ? raw.trim() : ''
  if (!value) return {}
  try {
    const parsed = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('invalid')
    }
    return Object.fromEntries(
      Object.entries(parsed as Record<string, unknown>).map(([key, item]) => [key, String(item ?? '')]),
    )
  } catch {
    throw new Error(`${field} 需要是合法 JSON 对象`)
  }
}

export function stringifyTaints(value?: NodeTaint[]) {
  return JSON.stringify(value ?? [], null, 2)
}

export function parseTaints(raw: unknown) {
  const value = typeof raw === 'string' ? raw.trim() : ''
  if (!value) return []
  try {
    const parsed = JSON.parse(value)
    if (!Array.isArray(parsed)) {
      throw new Error('invalid')
    }
    return parsed
      .filter((item) => item && typeof item === 'object')
      .map((item) => ({
        key: String((item as Record<string, unknown>).key ?? '').trim(),
        value: String((item as Record<string, unknown>).value ?? '').trim(),
        effect: String((item as Record<string, unknown>).effect ?? '').trim(),
      }))
      .filter((item) => item.key && item.effect)
  } catch {
    throw new Error('污点需要是合法 JSON 数组')
  }
}

function clampPercent(value?: number) {
  if (value == null || Number.isNaN(value)) return 0
  return Math.max(0, Math.min(100, Number(value.toFixed(1))))
}

function parseCpuCores(value?: string) {
  if (!value) return null
  const normalized = value.trim()
  if (!normalized) return null
  if (normalized.endsWith('m')) {
    const parsed = Number.parseFloat(normalized.slice(0, -1))
    return Number.isNaN(parsed) ? null : parsed / 1000
  }
  const parsed = Number.parseFloat(normalized)
  return Number.isNaN(parsed) ? null : parsed
}

export function formatCpu(value?: string) {
  const cores = parseCpuCores(value)
  if (cores == null) return '-'
  if (cores >= 10) return `${cores.toFixed(0)} Core`
  if (cores >= 1) return `${cores.toFixed(1)} Core`
  return `${cores.toFixed(2)} Core`
}

function parseBytes(value?: string) {
  if (!value) return null
  const normalized = value.trim()
  if (!normalized) return null
  const match = normalized.match(/^([\d.]+)(Ki|Mi|Gi|Ti|Pi|Ei)?$/)
  if (!match) return null
  const amount = Number.parseFloat(match[1])
  if (Number.isNaN(amount)) return null
  const unit = match[2] ?? ''
  const factors: Record<string, number> = {
    '': 1,
    Ki: 1024,
    Mi: 1024 ** 2,
    Gi: 1024 ** 3,
    Ti: 1024 ** 4,
    Pi: 1024 ** 5,
    Ei: 1024 ** 6,
  }
  return amount * factors[unit]
}

export function formatBytesAsG(value?: string) {
  const bytes = parseBytes(value)
  if (bytes == null) return '-'
  const gib = bytes / 1024 ** 3
  if (gib >= 10) return `${gib.toFixed(0)}G`
  if (gib >= 1) return `${gib.toFixed(1)}G`
  return `${gib.toFixed(2)}G`
}

function resolveProgressStroke(percent: number) {
  if (percent >= 85) return '#d84c45'
  if (percent >= 60) return '#d97706'
  return '#22a36a'
}

export function ResourceProgressCell({
  primary,
  secondary,
  percent,
  ariaLabel,
  compact = false,
  className,
}: {
  primary: string
  secondary: string
  percent: number
  ariaLabel: string
  compact?: boolean
  className?: string
}) {
  const value = clampPercent(percent)
  return (
    <div className={['soha-resource-cell', compact ? 'is-compact' : '', className].filter(Boolean).join(' ')}>
      <div className="soha-resource-cell-copy">
        <Text strong>{primary}</Text>
        <Text type="secondary" className="text-xs">{secondary}</Text>
      </div>
      <Progress
        percent={value}
        showInfo
        size={compact ? 'small' : 'default'}
        strokeColor={resolveProgressStroke(value)}
        format={(current) => `${current}%`}
        aria-label={ariaLabel}
      />
    </div>
  )
}

export function NodeResourcePanel({ node }: { node: Node }) {
  return (
    <div className="soha-node-expand-grid">
      <ResourceProgressCell
        primary={`${formatCpu(node.resources?.requests?.cpu)} / ${formatCpu(node.resources?.allocatable?.cpu)}`}
        secondary="CPU 已分配 / 总量"
        percent={node.resources?.requestPercentages?.cpu ?? 0}
        ariaLabel={`cpu allocation for ${node.name}`}
      />
      <ResourceProgressCell
        primary={`${formatBytesAsG(node.resources?.requests?.memory)} / ${formatBytesAsG(node.resources?.allocatable?.memory)}`}
        secondary="内存已分配 / 总量"
        percent={node.resources?.requestPercentages?.memory ?? 0}
        ariaLabel={`memory allocation for ${node.name}`}
      />
      <ResourceProgressCell
        primary={`${formatBytesAsG(node.resources?.requests?.ephemeralStorage)} / ${formatBytesAsG(node.resources?.allocatable?.ephemeralStorage)}`}
        secondary="磁盘已分配 / 总量"
        percent={node.resources?.requestPercentages?.ephemeralStorage ?? 0}
        ariaLabel={`disk allocation for ${node.name}`}
      />
      <ResourceProgressCell
        primary={`${node.podCount} / ${node.resources?.allocatable?.pods || '-'}`}
        secondary="Pods 已用 / 总量"
        percent={node.resources?.usagePercentages?.pods ?? 0}
        ariaLabel={`pod allocation for ${node.name}`}
      />
    </div>
  )
}
