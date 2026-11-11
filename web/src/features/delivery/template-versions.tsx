import { lazy, Suspense, useState } from 'react'
import { Button, Modal, Select, Space, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { HistoryOutlined } from '@ant-design/icons'
import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { deliveryApi } from './api'
import { deliveryKeys } from './keys'
import type { BuildTemplate, WorkflowTemplate, ServiceDeploymentTemplate } from './types'
import { DocumentSourcePanel } from './template-sources/source-panel'

type TemplateSnapshot = BuildTemplate | WorkflowTemplate | ServiceDeploymentTemplate
const ExportView = lazy(() =>
  import('./documents/source-editor').then((module) => ({
    default: module.DeliveryDocumentExportView,
  })),
)

export function TemplatePublicationStatus({
  template,
}: {
  template?: Pick<TemplateSnapshot, 'publicationState' | 'publishedVersion'>
}) {
  const { localeCode } = useI18n()
  const state = template?.publicationState ?? 'draft'
  const labels =
    localeCode === 'zh_CN'
      ? { draft: '草稿', published: '已发布', deprecated: '已废弃' }
      : { draft: 'Draft', published: 'Published', deprecated: 'Deprecated' }
  return (
    <Space size="small">
      <StatusTag value={state} label={labels[state]} />
      {(template?.publishedVersion ?? 0) > 0 ? <span>v{template?.publishedVersion}</span> : null}
    </Space>
  )
}

export function TemplateVersionHistory({
  kind,
  templateId,
  iconOnly = false,
}: {
  kind: 'build' | 'workflow' | 'deployment'
  templateId: string
  iconOnly?: boolean
}) {
  const { localeCode } = useI18n()
  const [open, setOpen] = useState(false)
  const [version, setVersion] = useState<number>()
  const group =
    kind === 'build'
      ? 'buildTemplates'
      : kind === 'deployment'
        ? 'deploymentTemplates'
        : 'workflowTemplates'
  const query = useQuery<TemplateSnapshot[]>({
    queryKey: deliveryKeys[group].versions(templateId),
    queryFn: () => deliveryApi[group].versions(templateId),
    enabled: open && Boolean(templateId),
  })
  const selected = query.data?.find((item) => item.publishedVersion === version) ?? query.data?.[0]
  const title = localeCode === 'zh_CN' ? '已发布版本' : 'Published versions'
  return (
    <>
      {iconOnly ? (
        <ManagementIconButton
          aria-label={title}
          tooltip={title}
          icon={<HistoryOutlined />}
          size="small"
          disabled={!templateId}
          onClick={() => {
            setVersion(undefined)
            setOpen(true)
          }}
        />
      ) : (
        <Button
          disabled={!templateId}
          onClick={() => {
            setVersion(undefined)
            setOpen(true)
          }}
        >
          {title}
        </Button>
      )}
      <Modal title={title} open={open} footer={null} onCancel={() => setOpen(false)} width={800}>
        {query.isLoading ? (
          <ManagementState kind="loading" />
        ) : query.isError ? (
          <ManagementState
            kind="error"
            description={query.error.message}
            actions={<Button onClick={() => void query.refetch()}>重试</Button>}
          />
        ) : !selected ? (
          <ManagementState
            kind="empty"
            title={localeCode === 'zh_CN' ? '尚未发布' : 'No published versions'}
          />
        ) : (
          <>
            <Select
              aria-label={title}
              value={selected.publishedVersion}
              onChange={setVersion}
              options={query.data?.map((item) => ({
                value: item.publishedVersion,
                label: `v${item.publishedVersion} · ${item.name}`,
              }))}
              style={{ width: '100%' }}
            />
            <Typography.Paragraph type="secondary">{selected.contentDigest}</Typography.Paragraph>
            <DocumentSourcePanel
              kind={
                kind === 'build'
                  ? 'BuildTemplate'
                  : kind === 'deployment'
                    ? 'DeploymentTemplate'
                    : 'WorkflowTemplate'
              }
              id={templateId}
              version={selected.publishedVersion}
            />
            <Suspense fallback={<ManagementState kind="loading" />}>
              <ExportView
                kind={
                  kind === 'build'
                    ? 'BuildTemplate'
                    : kind === 'deployment'
                      ? 'DeploymentTemplate'
                      : 'WorkflowTemplate'
                }
                id={templateId}
                version={selected.publishedVersion}
                legacy={selected}
              />
            </Suspense>
          </>
        )}
      </Modal>
    </>
  )
}
