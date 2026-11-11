import type { ReactNode } from 'react'
import { Select, Tabs, Tooltip } from 'antd'
import './workspace.css'
import { useI18n, localeText } from '@/i18n'

export function ServiceWorkspaceControls({
  environmentId,
  environments,
  onEnvironmentChange,
  status,
  actions,
}: {
  environmentId?: string
  environments: Array<{ value: string; label: string; production?: boolean }>
  onEnvironmentChange: (id: string) => void
  status?: ReactNode
  actions?: ReactNode
}) {
  const { localeCode } = useI18n()
  return (
    <div className="soha-application-context-select">
      <span>{localeText(localeCode, '环境', 'Environment')}</span>
      <Select
        aria-label={localeText(localeCode, '服务环境', 'Service environment')}
        placeholder={localeText(localeCode, '选择环境', 'Select environment')}
        value={environmentId || undefined}
        options={[...environments]
          .sort((a, b) => Number(a.production === true) - Number(b.production === true))
          .map((environment) => ({
            ...environment,
            label: environment.production ? `${environment.label} · PROD` : environment.label,
          }))}
        onChange={onEnvironmentChange}
        disabled={!environments.length}
      />
      {status}
      {actions}
    </div>
  )
}

export function ServiceWorkspaceNavigation({
  activeTab,
  onChange,
  hasRuntime,
  hasService = true,
  extra,
  podCount,
  resourceCount,
}: {
  activeTab: string
  onChange: (key: string) => void
  hasRuntime: boolean
  hasService?: boolean
  extra?: ReactNode
  podCount?: number
  resourceCount?: number
}) {
  const { localeCode } = useI18n()
  const label = (zh: string, en: string) => localeText(localeCode, zh, en)
  const runtimeItem = (key: string, itemLabel: string) => ({
    key,
    label: hasRuntime ? (
      itemLabel
    ) : (
      <Tooltip
        title={label('当前环境暂无可用运行实例', 'No runtime available in this environment')}
      >
        {itemLabel}
      </Tooltip>
    ),
    disabled: !hasRuntime,
  })
  return (
    <div className="soha-service-toolbar">
      {extra}
      <Tabs
        className="soha-resource-tabs is-header-only"
        activeKey={activeTab}
        onChange={onChange}
        items={[
          {
            key: 'pods',
            label: `${label('实例', 'Instances')}${podCount === undefined ? '' : ` ${podCount}`}`,
          },
          runtimeItem(
            'related-resources',
            `${label('关联资源', 'Related resources')}${resourceCount === undefined ? '' : ` ${resourceCount}`}`,
          ),
          ...(hasService
            ? [
                { key: 'build', label: label('构建', 'Build') },
                { key: 'resources', label: label('资源清单', 'Resource manifests') },
              ]
            : []),
        ]}
      />
    </div>
  )
}
