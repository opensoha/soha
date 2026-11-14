import type { ReactNode } from 'react'
import { Tag } from 'antd'
import { useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'

type TagColor =
  | 'default'
  | 'success'
  | 'error'
  | 'warning'
  | 'processing'
  | 'grey'
  | 'green'
  | 'red'
  | 'orange'
  | 'blue'
  | 'gold'
  | 'lime'
  | 'cyan'
  | 'purple'
  | 'volcano'
  | 'geekblue'
  | 'magenta'

export type MetadataTagTone = 'default' | 'blue' | 'cyan' | 'purple' | 'gold' | 'orange'

function resolveAntdTagColor(
  color: TagColor,
): Exclude<TagColor, 'grey' | 'green' | 'red' | 'orange' | 'blue'> {
  switch (color) {
    case 'green':
      return 'success'
    case 'red':
      return 'error'
    case 'orange':
      return 'warning'
    case 'blue':
      return 'processing'
    case 'grey':
      return 'default'
    default:
      return color
  }
}

function pickStatusColor(value?: null | string): TagColor {
  const normalized = (value || '').trim().toLowerCase()
  if (!normalized) {
    return 'default'
  }

  if (['published', 'visible', 'resolved', 'deployed', 'available'].includes(normalized)) {
    return 'geekblue'
  }

  if (['connected', 'bound', 'normal'].includes(normalized)) {
    return 'cyan'
  }

  if (
    [
      'active',
      'enabled',
      'healthy',
      'online',
      'ready',
      'running',
      'supported',
      'succeeded',
      'completed',
      'complete',
      'synced',
      'success',
      'docker_ready',
      'true',
      'allow',
      'live',
      'accepted',
      'fresh',
      'verified',
    ].includes(normalized)
  ) {
    return 'success'
  }

  if (
    [
      'warning',
      'approval',
      'pending',
      'queued',
      'building',
      'waiting',
      'released',
      'pending-install',
      'pending-upgrade',
      'draft',
      'degraded',
      'medium',
      'maintenance',
      'target',
      'agent_registered',
      'polling',
      'partial',
      'reconnecting',
    ].includes(normalized)
  ) {
    if (normalized === 'draft') return 'magenta'
    return 'warning'
  }

  if (
    [
      'error',
      'blocked',
      'denied',
      'failure',
      'failed',
      'disconnected',
      'critical',
      'high',
      'vulnerability',
      'crashloopbackoff',
      'terminating',
      'notready',
      'lost',
      'deny',
      'expired',
      'unavailable',
      'agent_failed',
      'timeout',
      'callback_timeout',
      'stale',
    ].includes(normalized)
  ) {
    return 'error'
  }

  if (['acknowledged', 'configuration', 'info', 'low'].includes(normalized)) {
    return 'processing'
  }

  if (
    [
      'attention',
      'checking',
      'connecting',
      'defined',
      'initializing',
      'provisioning',
      'provisioned_waiting_agent',
      'agent_bootstrapping',
      'running_task',
      'syncing',
      'deleting',
      'progressing',
      'vm_ready',
    ].includes(normalized)
  ) {
    return 'processing'
  }

  return 'default'
}

export function StatusTag({ label, value }: { label?: ReactNode; value?: null | string }) {
  const { localeCode } = useI18n()
  const displayLabel = label ?? formatStatusLabel(value, localeCode)
  return (
    <Tag
      className="soha-status-tag"
      color={resolveAntdTagColor(pickStatusColor(value))}
      variant="filled"
    >
      {displayLabel}
    </Tag>
  )
}

export function MetadataTag({
  label,
  tone = 'default',
  className,
  title,
}: {
  label: ReactNode
  tone?: MetadataTagTone
  className?: string
  title?: string
}) {
  return (
    <Tag
      className={['soha-metadata-tag', className].filter(Boolean).join(' ')}
      title={title}
      color={resolveAntdTagColor(tone)}
      variant="filled"
    >
      {label}
    </Tag>
  )
}

export function BooleanTag({
  value,
  trueLabel = '是',
  falseLabel = '否',
  trueColor = 'success',
  falseColor = 'default',
}: {
  value: boolean
  trueLabel?: string
  falseLabel?: string
  trueColor?: TagColor
  falseColor?: TagColor
}) {
  return (
    <Tag
      className="soha-status-tag"
      color={resolveAntdTagColor(value ? trueColor : falseColor)}
      variant="filled"
    >
      {value ? trueLabel : falseLabel}
    </Tag>
  )
}
