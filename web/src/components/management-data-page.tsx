import type { ReactNode } from 'react'
import { AdminTable } from '@/components/admin-table'
import type { AdminTableProps } from '@/components/admin-table'
import { ManagementDetailHeader, ManagementQueryPanel } from '@/components/management-list'
import type { FormProps } from 'antd'

interface ManagementDataPageQueryProps extends Pick<FormProps, 'form' | 'initialValues' | 'onFinish'> {
  actions: ReactNode
  children: ReactNode
  collapsible?: boolean
  defaultExpanded?: boolean
  expanded?: boolean
  lessLabel?: ReactNode
  moreLabel?: ReactNode
  onExpandedChange?: (expanded: boolean) => void
  wrapperClassName?: string
}

interface ManagementDataPageHeaderProps {
  actions?: ReactNode
  className?: string
  description?: ReactNode
  meta?: ReactNode
  title: ReactNode
}

interface ManagementDataPageBaseProps {
  afterTable?: ReactNode
  beforeQuery?: ReactNode
  children?: ReactNode
  className?: string
  header?: ManagementDataPageHeaderProps
  query?: ManagementDataPageQueryProps
}

type ManagementDataPageProps = ManagementDataPageBaseProps & (
  | { table: AdminTableProps; tableNode?: never }
  | { table?: never; tableNode: ReactNode }
)

function classNames(...items: Array<string | false | null | undefined>) {
  return items.filter(Boolean).join(' ')
}

export function ManagementDataPage({
  afterTable,
  beforeQuery,
  children,
  className,
  header,
  query,
  table,
  tableNode,
}: ManagementDataPageProps) {
  const renderedTable = tableNode ?? (() => {
    if (!table) return null
    const { shellClassName, ...tableProps } = table
    return (
      <AdminTable
        {...tableProps}
        shellClassName={classNames('soha-management-table-shell', shellClassName)}
      />
    )
  })()

  const renderedQuery = query ? (
    <ManagementQueryPanel
      actions={query.actions}
      collapsible={query.collapsible}
      defaultExpanded={query.defaultExpanded}
      expanded={query.expanded}
      form={query.form}
      initialValues={query.initialValues}
      lessLabel={query.lessLabel}
      moreLabel={query.moreLabel}
      onExpandedChange={query.onExpandedChange}
      onFinish={query.onFinish}
    >
      {query.children}
    </ManagementQueryPanel>
  ) : null

  return (
    <div className={classNames('soha-page', className)}>
      {header ? <ManagementDetailHeader {...header} /> : null}
      {beforeQuery}
      {renderedQuery && query?.wrapperClassName ? <div className={query.wrapperClassName}>{renderedQuery}</div> : renderedQuery}
      {children}
      {renderedTable}
      {afterTable}
    </div>
  )
}
