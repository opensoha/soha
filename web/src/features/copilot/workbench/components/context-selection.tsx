import { useEffect, useRef, useState } from 'react'
import { FolderOpenOutlined } from '@ant-design/icons'
import { Alert, Button, Input, Popover, Select, Space, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import type {
  WorkbenchContextReference,
  WorkbenchContextSelection,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { clusterQueries, platformOverviewQueries } from '@/features/platform'
import { knowledgeQueries } from '../../knowledge/queries'
import { readTextAttachment, textAttachmentAccept } from '../context-selection'

const resourceKinds = [
  { value: 'pods', kind: 'pod', label: 'Pod', permission: 'platform.pods.view' },
  {
    value: 'deployments',
    kind: 'deployment',
    label: 'Deployment',
    permission: 'platform.deployment.view',
  },
  {
    value: 'services',
    kind: 'service',
    label: 'Service',
    permission: 'platform.network.services.view',
  },
  { value: 'nodes', kind: 'node', label: 'Node', permission: 'platform.nodes.view' },
] as const

export function ChatContextSelection({
  selection,
  onChange,
  knowledgeBaseIds,
  onKnowledgeChange,
  onSession,
  onParsingChange,
  disabled,
  initialClusterId,
  showSelection = true,
}: {
  selection: WorkbenchContextSelection
  onChange: (value: WorkbenchContextSelection) => void
  knowledgeBaseIds: string[]
  onKnowledgeChange: (ids: string[]) => void
  onSession: () => void
  onParsingChange: (parsing: boolean) => void
  disabled: boolean
  initialClusterId?: string
  showSelection?: boolean
}) {
  const permissions = usePermissionSnapshot()
  const can = (permission: string) => hasPermission(permissions.data?.data, permission)
  const [open, setOpen] = useState(false)
  const [clusterId, setClusterId] = useState(initialClusterId || '')
  const [kind, setKind] = useState<(typeof resourceKinds)[number]['value']>('pods')
  const [query, setQuery] = useState('')
  const [parsing, setParsing] = useState(false)
  const [error, setError] = useState('')
  const generation = useRef(0)
  useEffect(
    () => () => {
      generation.current++
    },
    [],
  )
  const bases = useQuery(knowledgeQueries.bases(open && can('ai.knowledge.view')))
  const clusters = useQuery(clusterQueries.list(open && can('platform.clusters.view')))
  const chosenKind = resourceKinds.find((item) => item.value === kind)!
  const resources = useQuery(
    platformOverviewQueries.resourceSearch(
      clusterId,
      kind,
      query,
      open && can(chosenKind.permission),
    ),
  )
  const references = selection.references ?? []
  const attachments = selection.attachments ?? []
  const addReference = (ref: WorkbenchContextReference) => {
    if (references.length >= 20) {
      setError('最多添加 20 个引用。')
      return
    }
    onChange({ ...selection, references: [...references, ref] })
  }

  return (
    <>
      <Popover
        trigger="click"
        open={open}
        onOpenChange={setOpen}
        title="添加背景"
        content={
          <Space orientation="vertical" size={12} style={{ width: 300, maxWidth: '75vw' }}>
            <Select
              aria-label="引用知识库"
              mode="multiple"
              maxCount={20}
              style={{ width: '100%' }}
              placeholder={can('ai.knowledge.view') ? '选择知识库' : '无知识库读取权限'}
              disabled={disabled || !can('ai.knowledge.view')}
              value={knowledgeBaseIds}
              onChange={onKnowledgeChange}
              loading={bases.isFetching}
              options={(bases.data?.data ?? []).map((base) => ({
                label: base.name,
                value: base.id,
              }))}
            />
            {bases.isError ? <Alert type="error" title="知识库加载失败" /> : null}
            <Select
              aria-label="引用资源集群"
              style={{ width: '100%' }}
              placeholder="选择资源所在集群"
              value={clusterId || undefined}
              onChange={setClusterId}
              options={(clusters.data ?? []).map((cluster) => ({
                label: cluster.name,
                value: cluster.id,
              }))}
            />
            <Select
              aria-label="引用资源类型"
              style={{ width: '100%' }}
              value={kind}
              onChange={(value) => {
                setKind(value)
                setQuery('')
              }}
              options={resourceKinds.map((item) => ({
                label: item.label,
                value: item.value,
                disabled: !can(item.permission),
              }))}
            />
            <Input.Search
              aria-label="搜索引用资源"
              placeholder="输入资源名称并搜索"
              onSearch={setQuery}
              disabled={!clusterId || !can(chosenKind.permission)}
              loading={resources.isFetching}
            />
            {resources.data?.items.map((item) => (
              <Button
                key={item.key}
                type="text"
                disabled={disabled}
                onClick={() =>
                  addReference({
                    kind: chosenKind.kind,
                    clusterId,
                    namespace: item.namespace,
                    name: item.name,
                  })
                }
              >
                {item.namespace ? `${item.namespace}/` : ''}
                {item.name}
              </Button>
            ))}
            {query && !resources.isFetching && !resources.data?.items.length ? (
              <Typography.Text type="secondary">没有可访问的匹配资源。</Typography.Text>
            ) : null}
            {resources.isError || clusters.isError ? (
              <Alert type="error" title="资源读取失败，请重试" />
            ) : null}
            <Button onClick={onSession} disabled={disabled}>
              引用其他会话
            </Button>
            <label>
              文本附件（每个最多 64 KiB）
              <input
                aria-label="添加文本附件"
                type="file"
                multiple
                accept={textAttachmentAccept}
                disabled={disabled || parsing}
                onChange={async (event) => {
                  const files = Array.from(event.target.files ?? [])
                  event.target.value = ''
                  if (!files.length) return
                  if (files.length + attachments.length > 8) {
                    setError('最多添加 8 个附件。')
                    return
                  }
                  const revision = generation.current
                  setParsing(true)
                  onParsingChange(true)
                  setError('')
                  try {
                    const parsed = await Promise.all(files.map(readTextAttachment))
                    if (revision === generation.current)
                      onChange({ ...selection, attachments: [...attachments, ...parsed] })
                  } catch (cause) {
                    if (revision === generation.current)
                      setError(
                        cause instanceof Error
                          ? cause.message
                          : '文件解析失败，请使用 UTF-8 文本。',
                      )
                  } finally {
                    if (revision === generation.current) {
                      setParsing(false)
                      onParsingChange(false)
                    }
                  }
                }}
              />
            </label>
          </Space>
        }
      >
        <Button
          size="small"
          type="text"
          icon={<FolderOpenOutlined />}
          aria-label="添加背景"
          disabled={disabled}
        >
          添加背景
        </Button>
      </Popover>
      {showSelection && (
        <ChatContextTags
          selection={selection}
          onChange={onChange}
          knowledgeBaseIds={knowledgeBaseIds}
          onKnowledgeChange={onKnowledgeChange}
          disabled={disabled}
          bases={bases.data?.data}
        />
      )}
      {parsing ? <Typography.Text role="status">正在解析文件…</Typography.Text> : null}
      {error ? (
        <Alert
          className="soha-ai-workbench__context-alert"
          type="error"
          title={error}
          closable
          onClose={() => setError('')}
        />
      ) : null}
    </>
  )
}

export function ChatContextTags({
  selection,
  onChange,
  knowledgeBaseIds,
  onKnowledgeChange,
  disabled,
  bases,
}: {
  selection: WorkbenchContextSelection
  onChange: (value: WorkbenchContextSelection) => void
  knowledgeBaseIds: string[]
  onKnowledgeChange: (ids: string[]) => void
  disabled: boolean
  bases?: { id: string; name: string }[]
}) {
  const cachedBases = useQuery(knowledgeQueries.bases(false))
  const knownBases = bases ?? cachedBases.data?.data
  const references = selection.references ?? []
  const attachments = selection.attachments ?? []
  return (
    <>
      {knowledgeBaseIds.map((id) => (
        <Tag
          key={id}
          closable={!disabled}
          onClose={() => onKnowledgeChange(knowledgeBaseIds.filter((item) => item !== id))}
        >
          知识库 · {knownBases?.find((item) => item.id === id)?.name || id}
        </Tag>
      ))}
      {references.map((ref, index) => (
        <Tag
          key={`${ref.kind}:${index}`}
          closable={!disabled}
          onClose={() =>
            onChange({ ...selection, references: references.filter((_, i) => i !== index) })
          }
        >
          {ref.kind} · {ref.name}
        </Tag>
      ))}
      {attachments.map((file) => (
        <Tag
          key={file.id}
          closable={!disabled}
          onClose={() =>
            onChange({
              ...selection,
              attachments: attachments.filter((item) => item.id !== file.id),
            })
          }
        >
          {file.name} · 已解析
        </Tag>
      ))}
      {attachments.reduce((size, file) => size + Array.from(file.content).length, 0) > 24_000 ? (
        <Alert
          className="soha-ai-workbench__context-alert"
          type="warning"
          title="附件超过背景预算，发送时将裁剪；实际使用范围会记录在快照中。"
        />
      ) : null}
    </>
  )
}
