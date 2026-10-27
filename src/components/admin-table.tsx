import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { forwardRef, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { ComponentProps, ReactNode, Ref } from 'react'
import { Button, Checkbox, Popover, Table, Typography } from 'antd'
import type { GetRef, TableProps } from 'antd'
import { ArrowDownOutlined, ArrowUpOutlined, SettingOutlined } from '@ant-design/icons'
import {
  ManagementDensityButton,
  ManagementRefreshButton,
  ManagementState,
} from '@/components/management-list'
import { localeText, useI18n } from '@/i18n'
import type { LocaleCode } from '@/i18n'
import './admin-table.css'

const DEFAULT_PAGE_SIZE_OPTIONS = [10, 15, 20, 50, 100]
const VIEWPORT_SCROLL_MIN_BODY_HEIGHT = 240
const VIEWPORT_SCROLL_MIN_CONTAINER_HEIGHT = 640
const VIEWPORT_SCROLL_MIN_WIDTH = 768
const DEFAULT_PAGINATION_SUMMARY = (
  localeCode: LocaleCode,
  total: number,
  range: [number, number],
) => {
  if (total <= 0) return localeCode === 'zh_CN' ? '当前 0 / 0 条' : '0 / 0 items'
  return localeCode === 'zh_CN'
    ? `当前 ${range[0]}-${range[1]} / ${total} 条`
    : `${range[0]}-${range[1]} / ${total} items`
}
const ACTION_COLUMN_CLASS_NAME = 'soha-table-actions-column'
const AUTO_ACTION_COLUMN_CLASS_NAME = 'soha-table-actions-column--auto'
const { Text } = Typography
const EMPTY_TABLE_RECORDS: AdminTableProps['dataSource'] = []

// Antd adds a native title to ellipsis headers even when sorter tooltips are disabled.
function TableHeaderCell(props: ComponentProps<'th'>) {
  return <th {...props} title={undefined} />
}

const VirtualTableBody = forwardRef<HTMLDivElement, ComponentProps<'div'>>((props, ref) => {
  const { localeCode } = useI18n()
  return (
    <div
      {...props}
      ref={ref}
      tabIndex={0}
      role="region"
      aria-label={localeText(localeCode, '资源列表，可连续滚动', 'Scrollable resource list')}
      onKeyDown={(event) => {
        props.onKeyDown?.(event)
        if (event.defaultPrevented || event.target !== event.currentTarget) return
        const body = event.currentTarget
        const step = body.querySelector('[data-row-key]')?.getBoundingClientRect().height ?? 24
        const positions: Record<string, number> = {
          ArrowDown: body.scrollTop + step,
          ArrowUp: body.scrollTop - step,
          PageDown: body.scrollTop + body.clientHeight,
          PageUp: body.scrollTop - body.clientHeight,
          Home: 0,
          End: body.scrollHeight,
        }
        if (positions[event.key] === undefined) return
        event.preventDefault()
        body.scrollTop = positions[event.key]
      }}
    />
  )
})
VirtualTableBody.displayName = 'VirtualTableBody'

export interface AdminTableProps {
  className?: string
  columns: any[]
  currentPageSelectionLabel?: ReactNode
  dataSource: any[]
  empty?: ReactNode
  error?: Error | null
  hasReadData?: boolean
  enableColumnSelection?: boolean
  enableDensity?: boolean
  expandable?: any
  expandedRowRender?: (record: any, index?: number) => ReactNode
  headerExtra?: ReactNode
  hideExpandedColumn?: boolean
  localSorting?: boolean
  loading?: boolean
  onChange?: TableProps<any>['onChange']
  onRow?: any
  onScroll?: TableProps['onScroll']
  onRefresh?: () => void
  onRetry?: () => void
  refreshing?: boolean
  pageSize?: number
  pagination?: any
  paginationSummary?: ReactNode | ((total: number, range: [number, number]) => ReactNode)
  rowKey: string | ((record: any) => string)
  rowClassName?: any
  rowSelection?: any
  shellClassName?: string
  tableSize?: 'large' | 'middle' | 'small'
  tableLayout?: 'auto' | 'fixed'
  tableRef?: Ref<GetRef<typeof Table>>
  columnSettingPlacement?: 'toolbar' | 'header' | 'outside' | 'hidden'
  columnSettingIconOnly?: boolean
  scroll?: {
    x?: string | number
    y?: string | number
    scrollToFirstRowOnChange?: boolean
  }
  selectCurrentPageOnly?: boolean
  title?: ReactNode
  toolbar?: ReactNode
  toolbarExtra?: ReactNode
  viewportScroll?: boolean
  virtual?: boolean
}

function getColumnId(column: any, index: number) {
  if (typeof column?.key === 'string' && column.key) return column.key
  if (typeof column?.dataIndex === 'string' && column.dataIndex)
    return `${column.dataIndex}:${index}`
  if (Array.isArray(column?.dataIndex) && column.dataIndex.length > 0)
    return `${column.dataIndex.join('.')}:${index}`
  if (typeof column?.title === 'string' && column.title) return `${column.title}:${index}`
  return `column:${index}`
}

function isActionColumn(column: any) {
  const key = String(column?.key ?? '')
    .trim()
    .toLowerCase()
  const dataIndex = Array.isArray(column?.dataIndex)
    ? column.dataIndex.join('.').toLowerCase()
    : String(column?.dataIndex ?? '')
        .trim()
        .toLowerCase()
  const title = typeof column?.title === 'string' ? column.title.trim().toLowerCase() : ''
  return (
    key === 'actions' ||
    String(column?.className ?? '')
      .split(/\s+/)
      .includes(ACTION_COLUMN_CLASS_NAME) ||
    dataIndex === '__actions' ||
    (column?.fixed === 'right' && (title === '操作' || title === 'actions'))
  )
}

function mergeClassNames(...values: unknown[]) {
  return values
    .flatMap((value) => String(value ?? '').split(/\s+/))
    .filter(Boolean)
    .filter((value, index, items) => items.indexOf(value) === index)
    .join(' ')
}

function normalizeTableColumn(column: any, path: string, iconWidths: Record<string, number>): any {
  if (Array.isArray(column?.children)) {
    return {
      ...column,
      children: column.children.map((child: any, index: number) =>
        normalizeTableColumn(child, `${path}.${index}`, iconWidths),
      ),
    }
  }
  if (!isActionColumn(column)) return column

  const originalHeaderCell = column.onHeaderCell
  const originalCell = column.onCell
  const declaredClasses = mergeClassNames(
    column.className,
    typeof originalHeaderCell === 'function' ? originalHeaderCell(column)?.className : '',
  )
  const usesExplicitActionContract = declaredClasses.includes(ACTION_COLUMN_CLASS_NAME)
  const iconWidth = iconWidths[path]
  const actionClasses = mergeClassNames(
    declaredClasses,
    ACTION_COLUMN_CLASS_NAME,
    usesExplicitActionContract || iconWidth ? '' : AUTO_ACTION_COLUMN_CLASS_NAME,
  )
  const cellStyle = (style: any) =>
    iconWidth ? { ...style, '--soha-table-actions-column-width': `${iconWidth}px` } : style

  return {
    ...column,
    title: '',
    fixed: 'right',
    align: 'center',
    sorter: false,
    sortOrder: null,
    defaultSortOrder: undefined,
    width: iconWidth ?? (usesExplicitActionContract ? column.width : undefined),
    minWidth: iconWidth ?? (usesExplicitActionContract ? column.minWidth : 52),
    className: actionClasses,
    onHeaderCell: (...args: any[]) => {
      const props = typeof originalHeaderCell === 'function' ? originalHeaderCell(...args) : {}
      return {
        ...props,
        style: cellStyle(props?.style),
        className: mergeClassNames(props?.className, actionClasses),
      }
    },
    onCell: (...args: any[]) => {
      const props = typeof originalCell === 'function' ? originalCell(...args) : {}
      return {
        ...props,
        style: cellStyle(props?.style),
        'data-soha-action-column': path,
        className: mergeClassNames(props?.className, actionClasses),
      }
    },
  }
}

function getColumnValue(record: any, dataIndex: string | number | Array<string | number>) {
  const path = Array.isArray(dataIndex) ? dataIndex : [dataIndex]
  return path.reduce((value, key) => (value == null ? undefined : value[key]), record)
}

function isSortableValue(value: unknown) {
  return (
    typeof value === 'string' ||
    typeof value === 'boolean' ||
    (typeof value === 'number' && Number.isFinite(value)) ||
    value instanceof Date
  )
}

function compareSortableValues(left: unknown, right: unknown, collator: Intl.Collator) {
  if (left == null && right == null) return 0
  if (left == null) return 1
  if (right == null) return -1
  if (typeof left === 'number' && typeof right === 'number') return left - right
  if (typeof left === 'boolean' && typeof right === 'boolean') {
    return Number(left) - Number(right)
  }
  if (left instanceof Date && right instanceof Date) return left.getTime() - right.getTime()
  return collator.compare(String(left), String(right))
}

function addLocalSorters(columns: any[], dataSource: any[], collator: Intl.Collator): any[] {
  return columns.map((column) => {
    if (Array.isArray(column?.children)) {
      return { ...column, children: addLocalSorters(column.children, dataSource, collator) }
    }
    if (
      isActionColumn(column) ||
      Object.prototype.hasOwnProperty.call(column, 'sorter') ||
      column?.dataIndex == null
    ) {
      return column
    }
    const sampleValue = dataSource
      .map((record) => getColumnValue(record, column.dataIndex))
      .find((value) => value != null)
    if (!isSortableValue(sampleValue)) return column
    return {
      ...column,
      sorter: (left: any, right: any) =>
        compareSortableValues(
          getColumnValue(left, column.dataIndex),
          getColumnValue(right, column.dataIndex),
          collator,
        ),
    }
  })
}

function getColumnLabel(column: any, index: number) {
  if (isActionColumn(column)) return '操作'
  if (typeof column?.title === 'string' && column.title) return column.title
  if (typeof column?.dataIndex === 'string' && column.dataIndex) return column.dataIndex
  if (Array.isArray(column?.dataIndex) && column.dataIndex.length > 0)
    return column.dataIndex.join('.')
  return `列 ${index + 1}`
}

function getColumnWidth(column: any) {
  if (typeof column?.width === 'number' && Number.isFinite(column.width)) {
    return column.width
  }
  if (typeof column?.width === 'string') {
    const parsed = Number.parseInt(column.width, 10)
    if (Number.isFinite(parsed)) {
      return parsed
    }
  }
  return null
}

export function AdminTable({
  className,
  columns,
  currentPageSelectionLabel,
  dataSource: sourceRecords,
  empty,
  error,
  hasReadData,
  enableColumnSelection = true,
  enableDensity = false,
  headerExtra,
  localSorting = false,
  loading,
  onRefresh,
  onRetry,
  refreshing,
  pageSize = 15,
  pagination,
  paginationSummary,
  rowKey,
  rowSelection,
  shellClassName,
  tableSize = 'small',
  tableRef,
  columnSettingPlacement = 'toolbar',
  columnSettingIconOnly = false,
  scroll,
  selectCurrentPageOnly = false,
  title,
  toolbar,
  toolbarExtra,
  viewportScroll = false,
  virtual = false,
  ...rest
}: AdminTableProps) {
  const cachedRecords = readableQueryData({
    data: (hasReadData ?? sourceRecords.length > 0) ? sourceRecords : undefined,
    error: error ?? null,
  })
  const dataSource = error ? (cachedRecords ?? EMPTY_TABLE_RECORDS) : sourceRecords
  const { localeCode, t } = useI18n()
  const [density, setDensity] = useState<AdminTableProps['tableSize']>()
  const resolvedTableSize = enableDensity ? (density ?? tableSize) : tableSize
  const [iconActionWidths, setIconActionWidths] = useState<Record<string, number>>({})
  const localSortCollator = useMemo(
    () =>
      new Intl.Collator(localeCode === 'zh_CN' ? 'zh-CN' : 'en-US', {
        numeric: true,
        sensitivity: 'base',
      }),
    [localeCode],
  )
  const normalizedColumns = useMemo(() => {
    const nextColumns = columns.map((column, index) =>
      normalizeTableColumn(column, String(index), iconActionWidths),
    )
    return localSorting ? addLocalSorters(nextColumns, dataSource, localSortCollator) : nextColumns
  }, [columns, dataSource, iconActionWidths, localSortCollator, localSorting])
  const columnOptions = useMemo(
    () =>
      normalizedColumns.map((column, index) => ({
        id: getColumnId(column, index),
        label: getColumnLabel(column, index),
        column,
      })),
    [normalizedColumns],
  )
  const selectableColumnOptions = useMemo(
    () => columnOptions.filter((option) => !isActionColumn(option.column)),
    [columnOptions],
  )
  const columnSignature = columnOptions.map((option) => option.id).join('|')
  const [visibleColumnIds, setVisibleColumnIds] = useState<string[]>([])
  const [currentPage, setCurrentPage] = useState(1)
  const [currentPageSize, setCurrentPageSize] = useState(pageSize)
  const [viewportScrollY, setViewportScrollY] = useState<number>()
  const tableShellRef = useRef<HTMLDivElement>(null)

  // Measure the rendered icon group: permissions and row state can change its size.
  // Keep explicit sizing for text actions; never infer a button count from a render callback.
  useLayoutEffect(() => {
    const shell = tableShellRef.current
    if (!shell) return
    const groups = Array.from(
      shell.querySelectorAll<HTMLElement>(
        '[data-soha-action-column] .soha-row-action-icons, [data-soha-action-column] .ant-space:has(.ant-btn-icon-only), [data-soha-action-column] > .ant-btn-icon-only',
      ),
    ).filter(
      (group) =>
        group.classList.contains('soha-row-action-icons') ||
        !group.querySelector('.ant-btn:not(.ant-btn-icon-only)'),
    )
    const measure = () => {
      const widths: Record<string, number> = {}
      groups.forEach((group) => {
        const cell = group.closest<HTMLElement>('[data-soha-action-column]')!
        const width = group.getBoundingClientRect().width
        if (!width) return
        const style = window.getComputedStyle(cell)
        const padding =
          (Number.parseFloat(style.paddingLeft) || 0) + (Number.parseFloat(style.paddingRight) || 0)
        const key = cell.dataset.sohaActionColumn!
        widths[key] = Math.max(widths[key] ?? 0, Math.ceil(width + padding + 1), 40)
      })
      setIconActionWidths((current) =>
        JSON.stringify(current) === JSON.stringify(widths) ? current : widths,
      )
    }
    measure()
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure)
    groups.forEach((group) => observer?.observe(group))
    return () => observer?.disconnect()
  }, [columns, dataSource, currentPage, currentPageSize, pagination?.current])

  useEffect(() => {
    const nextIds = selectableColumnOptions.map((option) => option.id)
    setVisibleColumnIds((current) => {
      const filtered = current.filter((id) => nextIds.includes(id))
      return filtered.length > 0 ? filtered : nextIds
    })
  }, [columnSignature, selectableColumnOptions])

  useEffect(() => {
    if (pagination && pagination !== false && typeof pagination.currentPage === 'number') {
      setCurrentPage(pagination.currentPage)
    }
  }, [pagination])

  useEffect(() => {
    const nextPageSize =
      pagination && pagination !== false && typeof pagination.pageSize === 'number'
        ? pagination.pageSize
        : pageSize
    setCurrentPageSize(nextPageSize)
  }, [pageSize, pagination])

  const activeColumnIds =
    visibleColumnIds.length > 0
      ? visibleColumnIds
      : selectableColumnOptions.map((option) => option.id)
  const activeColumns = columnOptions
    .filter((option) => isActionColumn(option.column) || activeColumnIds.includes(option.id))
    .map((option) => option.column)
  const estimatedScrollWidth = useMemo(() => {
    const columnWidth = activeColumns.reduce(
      (total, column) => total + (getColumnWidth(column) ?? 168),
      0,
    )
    const selectionWidth = rowSelection ? 56 : 0
    return Math.max(960, columnWidth + selectionWidth)
  }, [activeColumns, rowSelection])

  const getRowKeyValue = (record: any) =>
    typeof rowKey === 'function' ? rowKey(record) : record?.[rowKey]
  const activeRowSelection =
    rowSelection && typeof rowSelection === 'object' ? rowSelection : undefined
  const currentPageRows =
    !selectCurrentPageOnly || pagination === false
      ? dataSource
      : dataSource.slice((currentPage - 1) * currentPageSize, currentPage * currentPageSize)
  const pageSelectableRows = activeRowSelection
    ? currentPageRows.filter((record) => !activeRowSelection.getCheckboxProps?.(record)?.disabled)
    : []
  const pageSelectableKeys = pageSelectableRows.map((record) => getRowKeyValue(record))
  const selectedKeySet = new Set(activeRowSelection?.selectedRowKeys ?? [])
  const pageAllSelected =
    pageSelectableKeys.length > 0 && pageSelectableKeys.every((key) => selectedKeySet.has(key))
  const pageIndeterminate =
    pageSelectableKeys.some((key) => selectedKeySet.has(key)) && !pageAllSelected

  const resolvedRowSelection =
    selectCurrentPageOnly && activeRowSelection
      ? {
          ...activeRowSelection,
          columnTitle: currentPageSelectionLabel ? (
            <span className="soha-admin-table-selection-label">{currentPageSelectionLabel}</span>
          ) : (
            activeRowSelection.columnTitle
          ),
          title: currentPageSelectionLabel ? (
            <span className="soha-admin-table-selection-label">{currentPageSelectionLabel}</span>
          ) : undefined,
          getTitleCheckboxProps: () => ({
            checked: pageAllSelected,
            indeterminate: pageIndeterminate,
          }),
          onSelectAll: (selected: boolean) => {
            const selectedRowKeys = Array.isArray(activeRowSelection.selectedRowKeys)
              ? [...activeRowSelection.selectedRowKeys]
              : []
            const pageKeySet = new Set(pageSelectableKeys)
            const nextSelectedRowKeys = selected
              ? Array.from(new Set(selectedRowKeys.concat(pageSelectableKeys)))
              : selectedRowKeys.filter((key) => !pageKeySet.has(key))
            const nextSelectedKeySet = new Set(nextSelectedRowKeys)
            const nextSelectedRows = dataSource.filter((record) =>
              nextSelectedKeySet.has(getRowKeyValue(record)),
            )
            activeRowSelection.onChange?.(nextSelectedRowKeys, nextSelectedRows)
          },
        }
      : rowSelection

  const inheritedPagination = pagination && pagination !== false ? pagination : undefined
  const resolvedPagination =
    pagination === false
      ? false
      : {
          pageSize: currentPageSize,
          current: currentPage,
          size: 'small' as const,
          showLessItems: true,
          showSizeChanger: true,
          pageSizeOptions: DEFAULT_PAGE_SIZE_OPTIONS,
          ...inheritedPagination,
          showTotal: paginationSummary
            ? (total: number, range: [number, number]) =>
                typeof paginationSummary === 'function'
                  ? paginationSummary(total, range)
                  : paginationSummary
            : (inheritedPagination?.showTotal ??
              ((total: number, range: [number, number]) =>
                DEFAULT_PAGINATION_SUMMARY(localeCode, total, range))),
          onChange: (nextPage: number, nextPageSize: number) => {
            inheritedPagination?.onChange?.(nextPage, nextPageSize)
            if (nextPageSize !== currentPageSize) {
              setCurrentPage(nextPage)
              setCurrentPageSize(nextPageSize)
              inheritedPagination?.onPageSizeChange?.(nextPageSize)
              return
            }
            if (nextPage === currentPage) return
            setCurrentPage(nextPage)
            inheritedPagination?.onPageChange?.(nextPage)
          },
        }

  useLayoutEffect(() => {
    if (!viewportScroll) {
      setViewportScrollY(undefined)
      return
    }
    const shell = tableShellRef.current
    const content = shell?.closest<HTMLElement>('.soha-content')
    if (!shell || !content) return

    const updateScrollHeight = () => {
      if (
        !virtual &&
        (window.innerWidth < VIEWPORT_SCROLL_MIN_WIDTH ||
          content.clientHeight < VIEWPORT_SCROLL_MIN_CONTAINER_HEIGHT)
      ) {
        setViewportScrollY(undefined)
        return
      }
      const tableHeader = shell.querySelector<HTMLElement>('.ant-table-thead')
      if (!tableHeader) return
      const paginationElement = shell.querySelector<HTMLElement>('.ant-table-pagination')
      const contentRect = content.getBoundingClientRect()
      const headerRect = tableHeader.getBoundingClientRect()
      const paddingBottom = Number.parseFloat(window.getComputedStyle(content).paddingBottom) || 0
      const paginationHeight = paginationElement?.getBoundingClientRect().height ?? 0
      const headerBottom = headerRect.bottom - contentRect.top + content.scrollTop
      const availableHeight = Math.floor(
        content.clientHeight - headerBottom - paddingBottom - paginationHeight - 8,
      )
      const nextHeight = virtual
        ? Math.max(VIEWPORT_SCROLL_MIN_BODY_HEIGHT, availableHeight)
        : availableHeight >= VIEWPORT_SCROLL_MIN_BODY_HEIGHT
          ? availableHeight
          : undefined
      setViewportScrollY((current) => (current === nextHeight ? current : nextHeight))
    }

    updateScrollHeight()
    const resizeObserver =
      typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(updateScrollHeight)
    resizeObserver?.observe(content)
    resizeObserver?.observe(shell)
    if (shell.parentElement) resizeObserver?.observe(shell.parentElement)
    window.addEventListener('resize', updateScrollHeight)
    return () => {
      resizeObserver?.disconnect()
      window.removeEventListener('resize', updateScrollHeight)
    }
  }, [dataSource.length, viewportScroll, virtual])

  const resolvedScroll = useMemo(
    () => ({
      x:
        virtual && typeof scroll?.x !== 'number'
          ? estimatedScrollWidth
          : (scroll?.x ?? estimatedScrollWidth),
      y: scroll?.y ?? viewportScrollY ?? (virtual ? VIEWPORT_SCROLL_MIN_BODY_HEIGHT : undefined),
      scrollToFirstRowOnChange: scroll?.scrollToFirstRowOnChange,
    }),
    [
      estimatedScrollWidth,
      scroll?.x,
      scroll?.y,
      scroll?.scrollToFirstRowOnChange,
      viewportScrollY,
      virtual,
    ],
  )

  const columnSetting =
    enableColumnSelection &&
    columnSettingPlacement !== 'hidden' &&
    selectableColumnOptions.length > 1 ? (
      <Popover
        trigger="click"
        placement="bottomRight"
        content={
          <div className="soha-admin-table-column-popover">
            <div className="soha-admin-table-column-actions">
              <Button
                size="small"
                type="text"
                onClick={() =>
                  setVisibleColumnIds(selectableColumnOptions.map((option) => option.id))
                }
              >
                {t('table.columns.selectAll', '全选')}
              </Button>
            </div>
            <Checkbox.Group
              className="soha-admin-table-column-options"
              options={selectableColumnOptions.map((option) => ({
                label: option.label,
                value: option.id,
              }))}
              value={activeColumnIds}
              onChange={(value) => {
                const next = value as string[]
                if (next.length === 0) return
                setVisibleColumnIds(next)
              }}
            />
            <Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
              {t('table.columns.sessionHint', '列设置仅影响当前页面会话。')}
            </Text>
          </div>
        }
      >
        <Button
          aria-label={t('table.columns.title', '列设置')}
          className={
            columnSettingIconOnly
              ? 'soha-admin-table-column-setting-button is-icon-only'
              : 'soha-admin-table-column-setting-button'
          }
          icon={<SettingOutlined />}
          size="small"
          type={columnSettingIconOnly ? 'text' : 'default'}
        >
          {columnSettingIconOnly ? null : t('table.columns.title', '列设置')}
        </Button>
      </Popover>
    ) : null

  const toolbarColumnSetting = columnSettingPlacement === 'toolbar' ? columnSetting : null
  const headerColumnSetting = columnSettingPlacement === 'header' ? columnSetting : null
  const outsideColumnSetting = columnSettingPlacement === 'outside' ? columnSetting : null
  const listControls =
    enableDensity || onRefresh ? (
      <>
        {enableDensity ? (
          <ManagementDensityButton
            aria-label={t('table.density.toggle', '切换表格密度')}
            size="small"
            tooltip={
              resolvedTableSize === 'small'
                ? t('table.density.relaxed', '切换为宽松密度')
                : t('table.density.compact', '切换为紧凑密度')
            }
            onClick={() => setDensity(resolvedTableSize === 'small' ? 'middle' : 'small')}
          />
        ) : null}
        {onRefresh ? (
          <ManagementRefreshButton
            aria-label={t('table.refresh', '刷新列表')}
            size="small"
            tooltip={t('table.refresh', '刷新列表')}
            loading={refreshing}
            onClick={onRefresh}
          />
        ) : null}
      </>
    ) : null
  const headerControls = columnSettingPlacement === 'header' ? listControls : null
  const toolbarControls = columnSettingPlacement !== 'header' ? listControls : null
  const resolvedHeaderExtra =
    headerExtra || headerControls || headerColumnSetting ? (
      <>
        {headerExtra}
        {headerControls}
        {headerColumnSetting}
      </>
    ) : null
  const resolvedToolbarExtra =
    toolbarExtra || toolbarControls || toolbarColumnSetting ? (
      <>
        {toolbarExtra}
        {toolbarControls}
        {toolbarColumnSetting}
      </>
    ) : null

  const hasHeader = Boolean(title || resolvedHeaderExtra)
  const hasToolbar = Boolean(toolbar || resolvedToolbarExtra)
  const resolvedShellClassName = [
    'soha-admin-table-shell',
    shellClassName,
    hasHeader || hasToolbar ? 'is-panel' : '',
    viewportScroll ? 'is-viewport-scroll' : '',
  ]
    .filter(Boolean)
    .join(' ')
  const resolvedTableClassName = ['soha-admin-table', className].filter(Boolean).join(' ')

  const tableShell = (
    <div ref={tableShellRef} className={resolvedShellClassName}>
      {hasHeader ? (
        <div className="soha-admin-table-header">
          <div className="soha-admin-table-header-main">{title}</div>
          {resolvedHeaderExtra ? (
            <div className="soha-admin-table-header-extra">{resolvedHeaderExtra}</div>
          ) : null}
        </div>
      ) : null}
      {hasToolbar ? (
        <div className="soha-admin-table-toolbar">
          {toolbar ? <div className="soha-admin-table-toolbar-main">{toolbar}</div> : null}
          {resolvedToolbarExtra ? (
            <div className="soha-admin-table-toolbar-extra">{resolvedToolbarExtra}</div>
          ) : null}
        </div>
      ) : null}
      {error ? (
        <ReadFeedback
          label={localeText(localeCode, '列表', 'List')}
          query={{
            data: (hasReadData ?? sourceRecords.length > 0) ? sourceRecords : undefined,
            error,
            isError: true,
            isFetching: Boolean(refreshing),
            refetch: onRetry ?? onRefresh,
          }}
        />
      ) : null}
      {!error || cachedRecords !== undefined ? (
        <Table
          {...rest}
          ref={tableRef}
          className={resolvedTableClassName}
          components={{
            header: { cell: TableHeaderCell },
            ...(virtual ? { body: { wrapper: VirtualTableBody } } : {}),
          }}
          column={{
            sortIcon: ({ sortOrder }) =>
              sortOrder ? (
                <span aria-hidden className="soha-table-sort-icon">
                  {sortOrder === 'ascend' ? <ArrowUpOutlined /> : <ArrowDownOutlined />}
                </span>
              ) : null,
          }}
          columns={activeColumns}
          dataSource={dataSource}
          loading={loading}
          locale={{ emptyText: empty ?? <ManagementState bordered={false} compact /> }}
          pagination={resolvedPagination}
          rowKey={rowKey}
          rowSelection={resolvedRowSelection}
          scroll={resolvedScroll}
          showSorterTooltip={false}
          size={resolvedTableSize}
          virtual={virtual}
        />
      ) : null}
    </div>
  )

  if (outsideColumnSetting) {
    return (
      <div className="soha-admin-table-host">
        <div className="soha-admin-table-outside-toolbar">{outsideColumnSetting}</div>
        {tableShell}
      </div>
    )
  }

  return tableShell
}
