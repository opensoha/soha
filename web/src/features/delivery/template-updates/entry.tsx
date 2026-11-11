import { lazy, Suspense, useState } from 'react'
import { Button } from 'antd'
import { DiffOutlined } from '@ant-design/icons'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { ManagementIconButton, ManagementState } from '@/components/management-list'
import type { TemplateUpdateScope } from './api'

const Drawer = lazy(() =>
  import('./drawer').then((module) => ({ default: module.TemplateAdoptionDrawer })),
)

export function TemplateUpdateEntry({
  kind,
  templateId,
  applicationId,
  serviceId,
  environmentId,
  initialVersion,
  initialTargetIds,
  iconOnly = false,
  label = '检查模板更新',
}: TemplateUpdateScope & {
  serviceId?: string
  environmentId?: string
  initialVersion?: number
  initialTargetIds?: string[]
  iconOnly?: boolean
  label?: string
}) {
  const [open, setOpen] = useState(false)
  const permissions = usePermissionSnapshot()
  const group =
    kind === 'build'
      ? 'build-templates'
      : kind === 'workflow'
        ? 'workflow-templates'
        : 'deployment-templates'
  if (!templateId || !hasPermission(permissions.data?.data, `delivery.${group}.view`)) return null
  return (
    <>
      {iconOnly ? (
        <ManagementIconButton
          aria-label={label}
          tooltip={label}
          size="small"
          icon={<DiffOutlined />}
          onClick={() => setOpen(true)}
        />
      ) : (
        <Button icon={<DiffOutlined />} onClick={() => setOpen(true)}>
          {label}
        </Button>
      )}
      {open ? (
        <Suspense fallback={<ManagementState kind="loading" title="正在打开模板采用" />}>
          <Drawer
            key={`${kind}:${templateId}:${applicationId ?? ''}:${serviceId ?? ''}:${environmentId ?? ''}`}
            scope={{ kind, templateId, applicationId }}
            serviceId={serviceId}
            environmentId={environmentId}
            initialVersion={initialVersion}
            initialTargetIds={initialTargetIds}
            onClose={() => setOpen(false)}
          />
        </Suspense>
      ) : null}
    </>
  )
}
