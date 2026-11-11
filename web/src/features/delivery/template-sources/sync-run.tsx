import { useState } from 'react'
import { Alert, App, Button, Card, Descriptions, Select, Space, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import { deliveryApi } from '../api'
import { deliveryKeys } from '../keys'
import { deliveryQueries } from '../queries'
import { DeliveryDocumentPreviewView } from '../documents/import-dialog'
import type {
  DeliveryTemplateSource,
  DeliveryTemplateSyncInput,
  DeliveryTemplateSyncRun,
} from '../types'

const runLabels = {
  running: '读取中',
  ready: '待导入',
  invalid: '校验失败',
  failed: '读取失败',
  applied: '已导入',
  stale: '已失效',
}

export function TemplateSourceSync({
  source,
  canSync,
}: {
  source: DeliveryTemplateSource
  canSync: boolean
}) {
  const client = useQueryClient()
  const { message } = App.useApp()
  const [offset, setOffset] = useState(0)
  const [selectedId, setSelectedId] = useState('')
  const [attempt, setAttempt] = useState<DeliveryTemplateSyncInput>()
  const runs = useQuery(deliveryQueries.templateSources.runs(source.id, offset))
  const runId = selectedId || source.lastSyncRunId || runs.data?.[0]?.id || ''
  const run = useQuery(deliveryQueries.templateSources.run(source.id, runId))
  const saveResult = async (result: DeliveryTemplateSyncRun) => {
    client.setQueryData(deliveryKeys.templateSources.run(source.id, result.id), result)
    setSelectedId(result.id)
    await client.invalidateQueries({ queryKey: deliveryKeys.all })
  }
  const sync = useMutation({
    mutationFn: (input: DeliveryTemplateSyncInput) =>
      deliveryApi.templateSources.sync(source.id, input),
    onSuccess: async (result) => {
      setAttempt(undefined)
      await saveResult(result)
    },
  })
  const apply = useMutation({
    mutationFn: () => {
      const candidate = run.data
      if (!candidate?.preview?.valid || !candidate.preview.candidateDigest)
        throw new Error('请先读取并校验来源。')
      return deliveryApi.templateSources.apply(source.id, candidate.id, {
        expectedGeneration: candidate.sourceGeneration,
        candidateDigest: candidate.preview.candidateDigest,
        // Stable across retries and reopening this run, including a lost response.
        idempotencyKey: `web-apply-${candidate.id}`,
      })
    },
    onSuccess: async (result) => {
      await saveResult(result)
      message.success('已导入，模板保存为草稿')
    },
  })
  const busy = sync.isPending || apply.isPending || run.data?.status === 'running'
  const expired = Boolean(
    run.data?.preview?.expiresAt && Date.parse(run.data.preview.expiresAt) <= Date.now(),
  )
  const valid =
    run.data?.status === 'ready' &&
    run.data.preview?.valid &&
    !expired &&
    source.generation === run.data.sourceGeneration
  const start = () => {
    const input =
      attempt?.expectedGeneration === source.generation
        ? attempt
        : { expectedGeneration: source.generation, idempotencyKey: crypto.randomUUID() }
    setAttempt(input)
    apply.reset()
    sync.mutate(input)
  }
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Space wrap>
        <Button
          type="primary"
          disabled={!canSync || !source.enabled || busy}
          loading={sync.isPending}
          onClick={start}
        >
          {attempt && sync.isError ? '恢复本次读取结果' : '读取并预览'}
        </Button>
        <Button
          onClick={() => {
            void runs.refetch()
            if (runId) void run.refetch()
          }}
          disabled={sync.isPending || apply.isPending}
        >
          刷新记录
        </Button>
      </Space>
      {sync.error || apply.error ? (
        <Alert
          type="error"
          showIcon
          title={(sync.error || apply.error)?.message}
          description="可刷新记录或恢复本次操作的结果。已失败的记录恢复后，可以再次读取 Git。"
        />
      ) : null}
      {runs.isError ? <Alert type="error" title="同步历史读取失败" /> : null}
      {runs.data?.length ? (
        <Space wrap>
          <Select
            aria-label="同步记录"
            style={{ minWidth: 320 }}
            value={runId}
            onChange={(value) => {
              setSelectedId(value)
              apply.reset()
            }}
            options={runs.data.map((item) => ({
              value: item.id,
              label: `${formatDateTime(item.createdAt)} · ${runLabels[item.status]} · ${item.resolvedCommit?.slice(0, 12) || item.id}`,
            }))}
          />
          <Button disabled={offset === 0 || runs.isFetching} onClick={() => setOffset(offset - 50)}>
            上一页
          </Button>
          <Button
            disabled={runs.data.length < 50 || runs.isFetching}
            onClick={() => setOffset(offset + 50)}
          >
            下一页
          </Button>
        </Space>
      ) : null}
      {run.isLoading ? (
        <ManagementState compact kind="loading" />
      ) : run.isError ? (
        <Alert type="error" title="同步记录读取失败" description={run.error.message} />
      ) : run.data ? (
        <>
          <Descriptions
            size="small"
            column={1}
            items={[
              {
                key: 'status',
                label: '状态',
                children: (
                  <StatusTag
                    value={
                      run.data.status === 'invalid'
                        ? 'failed'
                        : run.data.status === 'applied'
                          ? 'succeeded'
                          : run.data.status
                    }
                    label={runLabels[run.data.status]}
                  />
                ),
              },
              {
                key: 'commit',
                label: 'Commit',
                children: (
                  <Typography.Text code copyable>
                    {run.data.resolvedCommit || '尚未解析'}
                  </Typography.Text>
                ),
              },
              { key: 'actor', label: '发起人', children: run.data.actorId },
              { key: 'generation', label: '来源配置版本', children: run.data.sourceGeneration },
            ]}
          />
          {run.data.errorMessage ? (
            <Alert type="error" title={run.data.errorMessage} description={run.data.errorCode} />
          ) : null}
          {run.data.preview ? (
            <>
              <DeliveryDocumentPreviewView
                preview={run.data.preview}
                showImportHint={run.data.status === 'ready'}
              />
              {run.data.status === 'ready' && run.data.preview.expiresAt ? (
                <Typography.Text type={expired ? 'danger' : 'secondary'}>
                  候选有效至 {formatDateTime(run.data.preview.expiresAt)}
                  {expired ? '，请重新读取' : ''}
                </Typography.Text>
              ) : null}
            </>
          ) : run.data.status === 'ready' ? (
            <Alert type="info" title="仅发起本次同步且仍有对象权限的用户可以查看候选和导入" />
          ) : null}
          {run.data.removed?.length ? (
            <Card size="small" title="来源中已移除的文件">
              <Typography.Paragraph>
                {run.data.status === 'applied' ? '已标记为移除' : '确认导入后标记为已移除'}
                ，保留现有定义、版本和绑定。可在“关联对象”中单独解除管理或废弃。
              </Typography.Paragraph>
              <ul>
                {run.data.removed.map((item) => (
                  <li key={`${item.kind}:${item.objectId}`}>
                    {item.path} · {item.key}
                  </li>
                ))}
              </ul>
            </Card>
          ) : null}
          {run.data.status === 'ready' ? (
            <Button
              type="primary"
              disabled={!canSync || !source.enabled || !valid || busy}
              loading={apply.isPending}
              onClick={() => apply.mutate()}
            >
              确认导入此提交
            </Button>
          ) : null}
          {run.data.result ? (
            <Alert
              type="success"
              showIcon
              title={`已导入 ${run.data.result.objects.length} 个对象`}
              description={
                run.data.result.objects.some((item) => item.kind === 'Workflow')
                  ? '模板仍需发布后供新绑定使用；工作流配置已保存。'
                  : '模板仍需发布后供新绑定使用。'
              }
            />
          ) : null}
        </>
      ) : (
        <ManagementState
          compact
          kind="empty"
          title="尚未同步"
          description="读取 Git 后检查差异，再确认导入。"
        />
      )}
    </Space>
  )
}
