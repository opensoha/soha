import { useState, type ReactNode } from 'react'
import { TableCellLink } from '@/components/table-cell-content'
import {
  ManagementDensityButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementState,
} from '@/components/management-list'
import { encodeAIContextForElement, useAIPageContext } from '@/features/copilot'
import type { WorkloadKind } from './types'

export type WorkloadLocaleCode = 'zh_CN' | 'en_US'

const WORKLOAD_ENTITY_KINDS: Record<WorkloadKind, string> = {
  deployments: 'Deployment',
  pods: 'Pod',
  replicasets: 'ReplicaSet',
  replicationcontrollers: 'ReplicationController',
  statefulsets: 'StatefulSet',
  daemonsets: 'DaemonSet',
  jobs: 'Job',
  cronjobs: 'CronJob',
}

export function useWorkloadListAIContext({
  clusterId,
  itemCount,
  kind,
  label,
  namespace,
  searchKeyword,
}: {
  clusterId?: string | null
  itemCount: number
  kind: WorkloadKind
  label: string
  namespace?: string | null
  searchKeyword: string
}) {
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: label,
    entityKind: WORKLOAD_ENTITY_KINDS[kind],
    clusterId: clusterId || undefined,
    namespace: namespace || undefined,
    visibleFilters: { keyword: searchKeyword || undefined },
    pinnedData: { itemCount },
  })
}

export function workloadRowAIContext(
  kind: WorkloadKind,
  record: { name: string; namespace: string },
  clusterId?: string | null,
) {
  return {
    'data-ai-context': encodeAIContextForElement({
      sourceWorkbench: 'platform',
      entityKind: WORKLOAD_ENTITY_KINDS[kind],
      entityName: record.name,
      clusterId: clusterId || undefined,
      namespace: record.namespace || undefined,
      workload: record.name,
    }),
  }
}

export function WorkloadRefreshButton({
  disabled,
  label,
  loading,
  onRefresh,
}: {
  disabled?: boolean
  label: string
  loading?: boolean
  onRefresh: () => void
}) {
  return (
    <ManagementRefreshButton
      aria-label={label}
      disabled={disabled}
      loading={loading}
      tooltip={label}
      onClick={onRefresh}
    />
  )
}

export function WorkloadSearchInput({
  label,
  onChange,
  placeholder,
  value,
  width,
}: {
  label: ReactNode
  onChange: (value: string) => void
  placeholder: string
  value: string
  width?: number | string
}) {
  return (
    <ManagementKeywordField
      label={label}
      value={value}
      onChange={onChange}
      placeholder={placeholder}
      width={width}
      inputProps={{
        className: 'soha-platform-compact-field soha-workload-search-input',
        size: 'small',
      }}
    />
  )
}

export function WorkloadQueryPanel({
  children,
  hasActiveFilters,
  localeCode,
  onReset,
}: {
  children: ReactNode
  hasActiveFilters: boolean
  localeCode: WorkloadLocaleCode
  onReset: () => void
}) {
  return (
    <ManagementQueryPanel
      collapsible
      lessLabel={localeCode === 'zh_CN' ? '收起' : 'Collapse'}
      moreLabel={localeCode === 'zh_CN' ? '展开' : 'Expand'}
      onFinish={() => undefined}
      actions={
        <ManagementQueryActions
          disabledReset={!hasActiveFilters}
          onReset={onReset}
          resetLabel={localeCode === 'zh_CN' ? '重置' : 'Reset'}
          submitLabel={localeCode === 'zh_CN' ? '查询' : 'Search'}
        />
      }
    >
      {children}
    </ManagementQueryPanel>
  )
}

export function WorkloadTableEmpty({
  clusterId,
  filteredCount,
  localeCode,
  resourceLabel,
  totalCount,
}: {
  clusterId?: string | null
  filteredCount: number
  localeCode: WorkloadLocaleCode
  resourceLabel: string
  totalCount: number
}) {
  const hasFilterMiss = totalCount > 0 && filteredCount === 0
  const title = !clusterId
    ? localeCode === 'zh_CN'
      ? '请选择集群'
      : 'Select a cluster'
    : hasFilterMiss
      ? localeCode === 'zh_CN'
        ? `没有匹配的 ${resourceLabel}`
        : `No matching ${resourceLabel}`
      : localeCode === 'zh_CN'
        ? `当前范围没有 ${resourceLabel}`
        : `No ${resourceLabel} in the current scope`
  const description = !clusterId
    ? localeCode === 'zh_CN'
      ? '在顶部作用域选择集群后查看工作负载资源。'
      : 'Select a cluster in the header scope controls to inspect workload resources.'
    : hasFilterMiss
      ? localeCode === 'zh_CN'
        ? '调整搜索或筛选条件后重试。'
        : 'Adjust search or filters and try again.'
      : localeCode === 'zh_CN'
        ? '当前集群和命名空间范围内没有可展示的记录。'
        : 'No records are available for the selected cluster and namespace scope.'

  return (
    <ManagementState
      bordered={false}
      compact
      description={description}
      kind={!clusterId ? 'select-scope' : 'empty'}
      title={title}
    />
  )
}

export function useWorkloadTableDensity(localeCode: WorkloadLocaleCode) {
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const densityButton = (
    <ManagementDensityButton
      aria-label={densityLabel}
      title={densityLabel}
      tooltip={densityLabel}
      onClick={() => setTableSize((current) => (current === 'middle' ? 'small' : 'middle'))}
    />
  )

  return { densityButton, tableSize }
}

export function renderWorkloadNameLink(name: string, onClick: () => void) {
  return <TableCellLink label={name} onClick={onClick} />
}
