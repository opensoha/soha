import { forwardRef } from 'react'
import type { ReactNode } from 'react'
import { Alert, Button, Card, Empty, Form, Space, Spin, Tooltip, Typography } from 'antd'
import type { AlertProps, ButtonProps, FormProps } from 'antd'
import { ColumnHeightOutlined, ReloadOutlined } from '@ant-design/icons'

const { Text } = Typography

interface ManagementQueryPanelProps extends Pick<FormProps, 'onFinish'> {
  actions: ReactNode
  children: ReactNode
  expanded?: boolean
}

interface ManagementQueryGridProps {
  actions: ReactNode
  children: ReactNode
  expanded?: boolean
}

interface ManagementTableToolbarProps {
  batchBar?: ReactNode
  children?: ReactNode
}

interface ManagementBatchBarProps {
  children: ReactNode
  selectedCount: number
  selectedLabel?: ReactNode
}

interface ManagementIconButtonProps extends Omit<ButtonProps, 'children' | 'type'> {
  tooltip: ReactNode
}

interface ManagementDetailHeaderProps {
  actions?: ReactNode
  className?: string
  description?: ReactNode
  meta?: ReactNode
  title: ReactNode
}

type ManagementStateKind =
  | 'empty'
  | 'error'
  | 'loading'
  | 'no-permission'
  | 'not-configured'
  | 'not-found'
  | 'select-scope'
  | 'unsupported'

interface ManagementStateProps {
  actions?: ReactNode
  bordered?: boolean
  className?: string
  compact?: boolean
  description?: ReactNode
  kind?: ManagementStateKind
  title?: ReactNode
}

const managementStatePresets: Record<ManagementStateKind, { description: ReactNode; title: ReactNode; type: AlertProps['type'] }> = {
  empty: {
    title: '暂无数据',
    description: '当前筛选条件下没有可展示的记录。',
    type: 'info',
  },
  error: {
    title: '加载失败',
    description: '请求失败，请稍后重试。',
    type: 'error',
  },
  loading: {
    title: '正在加载',
    description: '正在读取最新数据。',
    type: 'info',
  },
  'no-permission': {
    title: '无访问权限',
    description: '当前账号没有访问此页面的权限。',
    type: 'warning',
  },
  'not-configured': {
    title: '尚未配置',
    description: '完成必要配置后这里会显示运行数据。',
    type: 'info',
  },
  'not-found': {
    title: '未找到资源',
    description: '目标资源不存在或已不可用。',
    type: 'warning',
  },
  'select-scope': {
    title: '请选择作用域',
    description: '选择集群或命名空间后查看数据。',
    type: 'info',
  },
  unsupported: {
    title: '暂不支持',
    description: '当前运行模式暂不支持该能力。',
    type: 'warning',
  },
}

function classNames(...items: Array<string | false | null | undefined>) {
  return items.filter(Boolean).join(' ')
}

export function ManagementQueryPanel({ actions, children, expanded = false, onFinish }: ManagementQueryPanelProps) {
  return (
    <Card className="soha-management-query-card" variant="outlined">
      <Form className="soha-management-query-form" layout="horizontal" onFinish={onFinish}>
        <ManagementQueryGrid actions={actions} expanded={expanded}>
          {children}
        </ManagementQueryGrid>
      </Form>
    </Card>
  )
}

export function ManagementQueryGrid({ actions, children, expanded = false }: ManagementQueryGridProps) {
  return (
    <div className={classNames('soha-management-query-grid', expanded ? 'is-expanded' : 'is-collapsed')}>
      <div className="soha-management-query-fields">{children}</div>
      <div className="soha-management-query-actions">{actions}</div>
    </div>
  )
}

export function ManagementQueryField(props: React.ComponentProps<typeof Form.Item>) {
  return <Form.Item {...props} className={classNames('soha-management-query-field', props.className)} />
}

export function ManagementTableToolbar({ batchBar, children }: ManagementTableToolbarProps) {
  return (
    <Space wrap size={8} className="soha-management-table-toolbar-actions">
      {batchBar}
      {children}
    </Space>
  )
}

export function ManagementBatchBar({ children, selectedCount, selectedLabel }: ManagementBatchBarProps) {
  return (
    <div className="soha-management-batchbar">
      <Text type="secondary">{selectedLabel ?? `已选 ${selectedCount} 项`}</Text>
      {children}
    </div>
  )
}

export const ManagementIconButton = forwardRef<HTMLButtonElement, ManagementIconButtonProps>(function ManagementIconButton(
  { tooltip, ...buttonProps },
  ref,
) {
  return (
    <Tooltip title={tooltip}>
      <Button {...buttonProps} ref={ref} className={classNames('soha-management-icon-action', buttonProps.className)} type="text" />
    </Tooltip>
  )
})

export function ManagementRefreshButton({ tooltip, ...buttonProps }: ManagementIconButtonProps) {
  return <ManagementIconButton {...buttonProps} icon={buttonProps.icon ?? <ReloadOutlined />} tooltip={tooltip} />
}

export function ManagementDensityButton({ tooltip, ...buttonProps }: ManagementIconButtonProps) {
  return <ManagementIconButton {...buttonProps} icon={buttonProps.icon ?? <ColumnHeightOutlined />} tooltip={tooltip} />
}

export function ManagementState({
  actions,
  bordered = true,
  className,
  compact = false,
  description,
  kind = 'empty',
  title,
}: ManagementStateProps) {
  const preset = managementStatePresets[kind]
  const resolvedTitle = title ?? preset.title
  const resolvedDescription = description ?? preset.description
  const stateClassName = classNames(
    'soha-management-state',
    `is-${kind}`,
    compact && 'is-compact',
    !bordered && 'is-borderless',
    className,
  )

  if (kind === 'loading') {
    return (
      <div className={stateClassName}>
        <Spin size="small" />
        <Space orientation="vertical" size={2} className="soha-management-state-loading-copy">
          <Text strong>{resolvedTitle}</Text>
          <Text type="secondary">{resolvedDescription}</Text>
        </Space>
      </div>
    )
  }

  if (kind === 'empty') {
    const emptyDescription = resolvedDescription ? (
      <Space orientation="vertical" size={2} className="soha-management-state-empty-copy">
        <Text strong>{resolvedTitle}</Text>
        <Text type="secondary">{resolvedDescription}</Text>
      </Space>
    ) : resolvedTitle

    return (
      <div className={stateClassName}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyDescription}>
          {actions}
        </Empty>
      </div>
    )
  }

  return (
    <div className={stateClassName}>
      <Alert
        action={actions}
        description={resolvedDescription}
        message={resolvedTitle}
        showIcon
        type={preset.type}
      />
    </div>
  )
}

export function ManagementDetailHeader({ actions, className, description, meta, title }: ManagementDetailHeaderProps) {
  return (
    <div className={classNames('soha-management-detail-header', className)}>
      <div className="soha-management-detail-header-main">
        <Text strong className="soha-management-detail-header-title">{title}</Text>
        {description ? <Text type="secondary" className="soha-management-detail-header-description">{description}</Text> : null}
        {meta ? <div className="soha-management-detail-header-meta">{meta}</div> : null}
      </div>
      {actions ? <div className="soha-management-detail-header-actions">{actions}</div> : null}
    </div>
  )
}
