import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import { Button, Form, Select, Space, Typography } from 'antd'
import { useId, useState } from 'react'
import type { PluginRepositoryRefsRequest } from '../plugin-model'
import { pluginQueries } from '../queries'

interface RepositoryRefsProps {
  repositoryUrl: string
  kind: PluginRepositoryRefsRequest['kind']
  disabled?: boolean
  selectedBranch?: string
  onSelect: (ref: string) => void
}

export function RepositoryRefs({
  repositoryUrl,
  kind,
  disabled,
  selectedBranch,
  onSelect,
}: RepositoryRefsProps) {
  const fieldId = useId()
  const [page, setPage] = useState(1)
  const isBranch = kind === 'branch'
  const label = isBranch ? '仓库分支' : '发布历史'
  const query = useQuery(pluginQueries.repositoryRefs({ repositoryUrl, kind, page }))
  const data = readableQueryData(query)

  return (
    <Space orientation="vertical" className="soha-plugin-repository-review">
      <ReadFeedback query={query} label={label} />
      {query.isLoading ? <span role="status">正在加载{label}…</span> : null}
      {data && !data.refs.length ? (
        <Typography.Text type="secondary">
          {isBranch ? '没有可选分支' : '暂无已发布版本'}
        </Typography.Text>
      ) : null}
      {isBranch ? (
        <>
          <ContextFeedback
            type="info"
            showIcon
            title="此仓库暂无可用 Release，请选择分支后预览。"
          />
          {data?.refs.length || selectedBranch ? (
            <Form.Item label={label} htmlFor={fieldId}>
              <Select
                id={fieldId}
                disabled={disabled || !data}
                value={selectedBranch}
                placeholder="选择分支"
                options={data?.refs.map(({ name }) => ({
                  label: name,
                  value: `refs/heads/${name}`,
                }))}
                onChange={onSelect}
              />
            </Form.Item>
          ) : null}
        </>
      ) : data?.refs.length ? (
        <ul>
          {data.refs.map(({ name, publishedAt, releaseUrl }) => (
            <li key={name}>
              <Space wrap>
                <Typography.Text className="soha-plugin-release-name" strong>
                  {name}
                </Typography.Text>
                {publishedAt ? (
                  <Typography.Text type="secondary">
                    <time dateTime={publishedAt}>{formatDateTime(publishedAt)}</time>
                  </Typography.Text>
                ) : null}
                {releaseUrl?.startsWith('https://github.com/') ? (
                  <Typography.Link href={releaseUrl} target="_blank" rel="noreferrer">
                    GitHub Release
                  </Typography.Link>
                ) : null}
                {!disabled ? (
                  <Button
                    size="small"
                    aria-label={`预览安装 ${name}`}
                    onClick={() => onSelect(`refs/tags/${name}`)}
                  >
                    预览安装
                  </Button>
                ) : null}
              </Space>
            </li>
          ))}
        </ul>
      ) : null}
      {page > 1 || data?.hasMore ? (
        <Space wrap>
          <Button
            size="small"
            disabled={page === 1 || query.isFetching}
            onClick={() => setPage(page - 1)}
          >
            上一页
          </Button>
          <Button
            size="small"
            disabled={!data?.hasMore || query.isFetching}
            onClick={() => setPage(page + 1)}
          >
            下一页
          </Button>
        </Space>
      ) : null}
    </Space>
  )
}
