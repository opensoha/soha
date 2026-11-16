import { ContextFeedback } from '@/components/context-feedback'
import { useState } from 'react'
import { Button, Descriptions, Modal, Space, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { MetadataTag } from '@/components/status-tag'
import { deliveryQueries } from '../queries'
import type { DeliveryDocumentKind } from '../types'
import { TemplateSourcesButton } from './entry'

export function DocumentSourcePanel({
  kind,
  id,
  version,
}: {
  kind: DeliveryDocumentKind
  id: string
  version?: number
}) {
  const [repositoryOpen, setRepositoryOpen] = useState(false)
  const query = useQuery(deliveryQueries.documents.source(kind, id, version))
  if (!id) return null
  if (query.isLoading) return <ManagementState compact kind="loading" />
  const data = readableQueryData(query)
  if (!data && query.isError) return <ReadFeedback query={query} label="来源" />
  const { association, provenance, repository } = data ?? {}
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <ReadFeedback query={query} label="来源" />
      <Space wrap>
        <MetadataTag label={association ? 'Git 管理' : 'Soha 管理'} />
        {repository ? (
          <Button disabled={false} onClick={() => setRepositoryOpen(true)}>
            查看仓库
          </Button>
        ) : null}
        {association ? <TemplateSourcesButton sourceId={association.sourceId} /> : null}
      </Space>
      {repository ? (
        <Modal
          open={repositoryOpen}
          title={repository.name}
          footer={null}
          onCancel={() => setRepositoryOpen(false)}
        >
          <Typography.Paragraph>克隆地址</Typography.Paragraph>
          <Typography.Paragraph code copyable>
            {repository.url}
          </Typography.Paragraph>
          <Typography.Text type="secondary">使用仓库连接读取；凭据由 Soha 管理。</Typography.Text>
        </Modal>
      ) : null}
      {association ? (
        <ContextFeedback
          type={association.removed ? 'warning' : 'info'}
          showIcon
          title={
            association.removed
              ? '来源文件已移除，现有定义和历史版本仍保留'
              : '从 Git 同步定义；在 Soha 中可发布版本、复制或解除关联'
          }
        />
      ) : null}
      {provenance || association ? (
        <Descriptions
          size="small"
          column={1}
          items={[
            {
              key: 'source',
              label: provenance ? `v${version} 版本来源` : '当前来源',
              children: provenance?.sourceId ?? association?.sourceId,
            },
            ...(repository || provenance
              ? [
                  {
                    key: 'repo',
                    label: '仓库',
                    children: repository?.name ?? provenance?.repositoryId,
                  },
                ]
              : []),
            { key: 'path', label: '文件', children: provenance?.path ?? association?.path },
            {
              key: 'commit',
              label: 'Commit',
              children: (
                <Typography.Text code copyable>
                  {provenance?.resolvedCommit ?? association?.resolvedCommit}
                </Typography.Text>
              ),
            },
            {
              key: 'run',
              label: '同步记录',
              children: provenance?.syncRunId ?? association?.syncRunId,
            },
          ]}
        />
      ) : null}
      {version && !provenance ? (
        <Typography.Text type="secondary">此版本没有 Git 来源记录。</Typography.Text>
      ) : null}
    </Space>
  )
}
