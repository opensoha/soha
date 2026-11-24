import type { ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AdminTable, type AdminTableProps } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { NamespaceScopeTag } from './namespace-scope-tag'
import { useResourceListViewport } from './use-resource-list-viewport'
import '../platform-shared.css'

type Column = AdminTableProps['columns'][number]
const columnId = (column: Column, index: number) =>
  String(
    column.key ??
      (Array.isArray(column.dataIndex) ? column.dataIndex.join('.') : column.dataIndex) ??
      index,
  )
const isAction = (column: Column) =>
  ['actions', '__actions'].includes(String(column.key ?? column.dataIndex))

export function resourceColumnWidths(columns: Column[], rows: Record<string, unknown>[]) {
  const primary = columns.findIndex((column) => !isAction(column))
  return Object.fromEntries(
    columns.map((column, index) => {
      const field = column.dataIndex
      let width = typeof column.width === 'number' ? column.width : 168
      if (typeof field === 'string' && (index === primary || !column.minWidth)) {
        const longest = rows.reduce((length, row) => {
          const value = row[field]
          return typeof value === 'string' || typeof value === 'number'
            ? Math.max(length, String(value).length)
            : length
        }, 0)
        if (index === primary) width = Math.max(220, Math.min(320, longest * 7 + 32))
        else if (field === 'namespace') width = Math.max(108, Math.min(164, longest * 7 + 28))
        else if (/ip$/i.test(field)) width = Math.max(104, Math.min(288, longest * 7 + 32))
        else if (column.width === undefined) width = Math.max(88, Math.min(220, longest * 7 + 28))
      }
      return [columnId(column, index), width]
    }),
  )
}

export type KubernetesResourceTableProps = Omit<
  AdminTableProps,
  | 'pagination'
  | 'pageSize'
  | 'virtual'
  | 'viewportScroll'
  | 'columnLayoutKey'
  | 'tableRef'
  | 'selectCurrentPageOnly'
  | 'currentPageSelectionLabel'
  | 'defaultHiddenColumnKeys'
> & {
  resourceKey: string
  selectionToolbar?: ReactNode
  resourceLabel: string
  /** Complete readable scope, before client filtering; never a server page. */
  allRows: Record<string, any>[]
  filterKey?: string
  scopeMode?: 'namespace' | 'cluster' | 'global'
  hideNamespaceWhenScoped?: boolean
}

/** K8s full-collection browsing; request and mutation ownership stays with each resource. */
export function KubernetesResourceTable({
  resourceKey,
  selectionToolbar,
  resourceLabel,
  allRows,
  filterKey = '',
  scopeMode = 'namespace',
  hideNamespaceWhenScoped = false,
  columns,
  dataSource,
  rowKey,
  title,
  onChange,
  onScroll,
  onFitColumns,
  onResetColumns,
  rowSelection,
  className,
  scroll,
  ...props
}: KubernetesResourceTableProps) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const userId = useAuthStore((state) => state.user?.userId)
  const { clusterId, namespace } = usePlatformScopeStore()
  const scope = JSON.stringify([
    resourceKey,
    scopeMode === 'global' ? null : clusterId,
    scopeMode === 'namespace' ? namespace : null,
    localeCode,
  ])
  const rows = allRows ?? dataSource
  const [fitted, setFitted] = useState(() => ({
    scope,
    initialized: rows.length > 0,
    widths: resourceColumnWidths(columns, rows),
    contentFit: false,
  }))
  const measured = useMemo(
    () =>
      fitted.scope !== scope || (!fitted.initialized && rows.length)
        ? resourceColumnWidths(columns, rows)
        : fitted.widths,
    [columns, rows, scope, fitted],
  )
  useEffect(() => {
    if (fitted.scope !== scope || (!fitted.initialized && rows.length))
      setFitted({ scope, initialized: rows.length > 0, widths: measured, contentFit: false })
  }, [scope, measured, rows.length, fitted.scope, fitted.initialized])
  const widths = fitted.scope === scope ? fitted.widths : measured
  const shellRef = useRef<HTMLDivElement>(null)
  const [available, setAvailable] = useState(1100)
  useEffect(() => {
    const element = shellRef.current
    if (!element) return
    const measure = () => setAvailable(element.clientWidth - 32)
    measure()
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure)
    observer?.observe(element)
    return () => observer?.disconnect()
  }, [])
  const activeColumns = useMemo(() => {
    const primary = columns.findIndex((column) => !isAction(column))
    return columns.map((column, index) => ({
      ...column,
      render:
        column.render ??
        (column.dataIndex === 'namespace'
          ? (value: string) => <NamespaceScopeTag namespace={value} />
          : undefined),
      key: columnId(column, index),
      ellipsis: column.ellipsis ?? (index === primary ? true : undefined),
      width:
        index === primary && !(fitted.scope === scope && fitted.contentFit)
          ? Math.max(220, Math.min(320, Math.round(available * 0.3)))
          : widths[columnId(column, index)],
      minWidth: column.minWidth ?? (index === primary ? 200 : 64),
      fixed:
        index === primary
          ? available >= 640
            ? 'left'
            : undefined
          : isAction(column) && available < 640 && widths[columnId(column, index)] > 64
            ? false
            : column.fixed,
    }))
  }, [columns, available, widths, fitted.scope, fitted.contentFit, scope])
  const viewport = useResourceListViewport(dataSource, rowKey, `${scope}/${filterKey}`)
  const count = dataSource.length
  const scopeReady = scopeMode === 'global' || Boolean(clusterId)
  const countLabel = !scopeReady
    ? zh
      ? '请选择集群'
      : 'Select a cluster'
    : props.loading && !rows.length
      ? zh
        ? `正在加载 ${resourceLabel}`
        : `Loading ${resourceLabel}`
      : props.error && !rows.length
        ? zh
          ? `${resourceLabel} 未加载`
          : `${resourceLabel} unavailable`
        : count === rows.length
          ? zh
            ? `${count} 个 ${resourceLabel}`
            : `${count} ${resourceLabel}`
          : zh
            ? `匹配 ${count} / 共 ${rows.length} 个 ${resourceLabel}`
            : `${count} matches / ${rows.length} ${resourceLabel}`
  const selectionLabel = zh
    ? `选择全部匹配的 ${count} 个 ${resourceLabel}`
    : `Select all ${count} matching ${resourceLabel}`
  const defaultHiddenColumns = useMemo(
    () =>
      hideNamespaceWhenScoped && scopeMode === 'namespace' && namespace
        ? columns.flatMap((column, index) =>
            column.dataIndex === 'namespace' ? [columnId(column, index)] : [],
          )
        : [],
    [hideNamespaceWhenScoped, scopeMode, namespace, columns],
  )
  const tableSelection = useMemo(
    () =>
      rowSelection
        ? {
            ...rowSelection,
            getTitleCheckboxProps: () => ({
              ...rowSelection.getTitleCheckboxProps?.(),
              'aria-label': selectionLabel,
              title: selectionLabel,
            }),
          }
        : undefined,
    [rowSelection, selectionLabel],
  )
  const handleScroll = useCallback<NonNullable<AdminTableProps['onScroll']>>(
    (event) => {
      viewport.onScroll(event)
      onScroll?.(event)
    },
    [viewport.onScroll, onScroll],
  )
  const handleChange = useCallback<NonNullable<AdminTableProps['onChange']>>(
    (pagination, filters, sorter, extra) => {
      if (extra.action === 'sort' || extra.action === 'filter') viewport.resetPosition()
      onChange?.(pagination, filters, sorter, extra)
    },
    [viewport.resetPosition, onChange],
  )
  return (
    <div ref={shellRef} className="soha-kubernetes-resource-list">
      <AdminTable
        {...props}
        className={['soha-kubernetes-resource-table', className].filter(Boolean).join(' ')}
        title={
          <>
            {title ?? (
              <span className="soha-kubernetes-resource-count" role="status" aria-live="polite">
                {countLabel}
              </span>
            )}
            {selectionToolbar}
          </>
        }
        columns={activeColumns}
        dataSource={dataSource}
        rowKey={rowKey}
        pagination={false}
        virtual
        viewportScroll
        columnLayoutKey={`${resourceKey}:${userId || 'session'}:${localeCode}`}
        defaultHiddenColumnKeys={defaultHiddenColumns}
        onFitColumns={() => {
          onFitColumns?.()
          setFitted({
            scope,
            initialized: rows.length > 0,
            widths: resourceColumnWidths(columns, rows),
            contentFit: true,
          })
        }}
        onResetColumns={() => {
          onResetColumns?.()
          setFitted({
            scope,
            initialized: rows.length > 0,
            widths: resourceColumnWidths(columns, rows),
            contentFit: false,
          })
        }}
        tableRef={viewport.tableRef}
        scroll={{ ...scroll, x: undefined, scrollToFirstRowOnChange: false }}
        onScroll={handleScroll}
        onChange={handleChange}
        rowSelection={tableSelection}
      />
    </div>
  )
}
