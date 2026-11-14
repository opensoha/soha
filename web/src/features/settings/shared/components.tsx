import type { ComponentProps, ReactNode } from 'react'
import { Card, Select } from 'antd'
import { AdminTable } from '@/components/admin-table'

export const WIDE_FORM_LAYOUT = {
  labelAlign: 'left' as const,
  labelCol: { flex: '160px' },
  wrapperCol: { flex: 'auto' },
}

export const DEFAULT_FORM_LAYOUT = {
  labelAlign: 'left' as const,
  labelCol: { flex: '140px' },
  wrapperCol: { flex: 'auto' },
}

export const fullWidthStyle = { width: '100%' }

export function SectionCallout({ title, description }: { title: string; description: string }) {
  return (
    <div className="mb-4 rounded border border-[var(--soha-border-color)] bg-[var(--soha-fill-weak)] p-3 text-sm">
      <div className="font-medium">{title}</div>
      <div className="mt-1 text-[var(--ant-colorTextSecondary)]">{description}</div>
    </div>
  )
}

export function SettingsCard({
  title,
  extra,
  children,
}: {
  title?: ReactNode
  extra?: ReactNode
  children: ReactNode
}) {
  return (
    <Card title={title} extra={extra}>
      {children}
    </Card>
  )
}

function classNames(...items: Array<string | false | null | undefined>) {
  return items.filter(Boolean).join(' ')
}

export function SettingsAdminTable({
  className,
  columnSettingIconOnly = true,
  columnSettingPlacement = 'header',
  scroll,
  shellClassName,
  tableSize = 'small',
  ...props
}: ComponentProps<typeof AdminTable>) {
  return (
    <AdminTable
      {...props}
      className={classNames('soha-settings-table', className)}
      columnSettingIconOnly={columnSettingIconOnly}
      columnSettingPlacement={columnSettingPlacement}
      scroll={scroll ?? { x: 'max-content' }}
      shellClassName={classNames(
        'soha-management-table-shell',
        'soha-settings-table-shell',
        shellClassName,
      )}
      tableSize={tableSize}
    />
  )
}

export function TagSelect(props: {
  placeholder?: string
  mode?: 'multiple' | 'tags'
  options?: Array<{ value: string; label: string }>
}) {
  return <Select {...props} style={fullWidthStyle} />
}
