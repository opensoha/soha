import { Tag } from '@douyinfe/semi-ui'

type TagColor = 'grey' | 'green' | 'red' | 'orange' | 'blue'

function pickStatusColor(value: string): TagColor {
  const normalized = value.trim().toLowerCase()

  if ([
    'active', 'healthy', 'ready', 'running', 'succeeded', 'complete', 'success',
    'connected', 'published', 'visible', 'resolved', 'deployed', 'available',
    'bound', 'normal', 'true', 'allow',
  ].includes(normalized)) {
    return 'green'
  }

  if ([
    'warning', 'pending', 'queued', 'building', 'waiting', 'released',
    'pending-install', 'pending-upgrade', 'draft',
  ].includes(normalized)) {
    return 'orange'
  }

  if ([
    'error', 'failed', 'disconnected', 'critical', 'crashloopbackoff',
    'terminating', 'notready', 'lost', 'deny',
  ].includes(normalized)) {
    return 'red'
  }

  if (['acknowledged', 'info'].includes(normalized)) {
    return 'blue'
  }

  return 'grey'
}

export function StatusTag({ value }: { value: string }) {
  return <Tag color={pickStatusColor(value)}>{value}</Tag>
}

export function BooleanTag({
  value,
  trueLabel = '是',
  falseLabel = '否',
  trueColor = 'green',
  falseColor = 'grey',
}: {
  value: boolean
  trueLabel?: string
  falseLabel?: string
  trueColor?: TagColor
  falseColor?: TagColor
}) {
  return <Tag color={value ? trueColor : falseColor}>{value ? trueLabel : falseLabel}</Tag>
}
