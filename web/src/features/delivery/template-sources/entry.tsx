import { lazy, Suspense, useState } from 'react'
import { Button } from 'antd'
import { BranchesOutlined } from '@ant-design/icons'
import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'

const Manager = lazy(() =>
  import('./manager').then((module) => ({ default: module.TemplateSourceManager })),
)

export function TemplateSourcesButton({
  sourceId,
  iconOnly = false,
}: {
  sourceId?: string
  iconOnly?: boolean
}) {
  const [open, setOpen] = useState(false)
  const permissions = usePermissionSnapshot()
  if (!hasPermission(permissions.data?.data, 'delivery.template-sources.view')) return null
  const label = sourceId ? '管理 Git 来源' : 'Git 来源'
  return (
    <>
      {iconOnly ? (
        <ManagementIconButton
          aria-label={label}
          tooltip={label}
          icon={<BranchesOutlined />}
          onClick={() => setOpen(true)}
        />
      ) : (
        <Button icon={<BranchesOutlined />} onClick={() => setOpen(true)}>
          {label}
        </Button>
      )}
      {open ? (
        <Suspense fallback={<ManagementState compact kind="loading" />}>
          <Manager initialId={sourceId} onClose={() => setOpen(false)} />
        </Suspense>
      ) : null}
    </>
  )
}
