import { Select } from 'antd'
import { buildSameOriginStreamURL } from '@/features/auth'
import { localeText } from '@/i18n'
import type { LocaleCode } from '@/i18n'

export interface DockerRuntimeServiceOption {
  label: string
  value: string
}

export interface DockerRuntimePanelProps {
  enabled: boolean
  projectId: string
  projectName?: string
  serviceName?: string
  serviceOptions: DockerRuntimeServiceOption[]
  servicesLoading?: boolean
  servicesError?: boolean
  onServiceChange: (serviceName: string) => void
}

export interface TerminalMessage {
  type: string
  data?: string
  message?: string
}

export function dockerRuntimeWebSocketURL(
  projectId: string,
  path: 'logs/stream' | 'terminal',
  params: Record<string, string | number | undefined>,
) {
  const base = buildSameOriginStreamURL(
    `/api/v1/docker/projects/${encodeURIComponent(projectId)}/runtime/${path}`,
    'ws',
  )
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') {
      base.searchParams.set(key, String(value))
    }
  })
  return base.toString()
}

export function splitLogContent(content?: string) {
  return (content || '').split(/\r?\n/).filter((line) => line.trim() !== '')
}

export function appendLogLine(items: string[], line: string) {
  const next = [...items, line]
  return next.length > 2000 ? next.slice(next.length - 2000) : next
}

export function runtimeServiceSelector({
  disabled,
  loading,
  onChange,
  options,
  serviceName,
  localeCode,
}: {
  disabled?: boolean
  loading?: boolean
  onChange: (serviceName: string) => void
  options: DockerRuntimeServiceOption[]
  serviceName?: string
  localeCode: LocaleCode
}) {
  return (
    <Select
      aria-label={localeText(localeCode, '服务', 'Service')}
      disabled={disabled}
      loading={loading}
      options={options}
      placeholder={localeText(localeCode, '选择服务', 'Select service')}
      popupMatchSelectWidth={false}
      size="small"
      style={{ minWidth: 180 }}
      value={serviceName || undefined}
      onChange={onChange}
    />
  )
}
