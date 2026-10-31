import { useEffect, useRef } from 'react'
import { Alert, Button, Input, Popconfirm, Space, Typography } from 'antd'
import { DownloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { DockerVolumeFileReadInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { ReadFeedback } from '@/components/request-feedback'
import { DecisionEvidence } from '@/components/content-dlp-policy-tools'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { downloadText } from '@/utils/download'
import { DockerDLPActivity } from '../content-dlp-activity'
import { authorizeDLP, dlpWorkflowQueries, readVolumeTransfer } from '../content-dlp-workflow-api'

// Parent keys this component by authenticated viewer and the complete file identity.
export function VolumeContentPreview({
  projectId,
  viewer,
  input,
  enabled,
}: {
  projectId: string
  viewer: string
  input: DockerVolumeFileReadInput
  enabled: boolean
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const client = useQueryClient()
  const options = dlpWorkflowQueries.file(projectId, viewer, input, enabled)
  const query = useQuery(options)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const retry = useOperationMutation(
    {
      retry: false,
      gcTime: 0,
      mutationFn: async ({
        event,
        action,
      }: {
        event: string
        action: 'acknowledge' | 'approved_appeal'
      }) => {
        const state = client.getQueryState(options.queryKey)
        const stamp = state?.dataUpdateCount
        const grant = await authorizeDLP(projectId, event, action)
        const result = await readVolumeTransfer(projectId, { ...input, authorization: grant.token })
        // Authorization is local to this call, never a query key, URL or persisted state.
        return { result, stamp }
      },
      onSuccess: ({ result, stamp }) => {
        const state = client.getQueryState(options.queryKey)
        if (
          mounted.current &&
          state?.fetchStatus === 'idle' &&
          !state.isInvalidated &&
          state.dataUpdateCount === stamp
        )
          client.setQueryData(options.queryKey, result)
      },
    },
    {
      label: ['确认并重新检查内容', 'Confirm and inspect content again'],
      target: ({ event }) => event,
      receipt: ({ result }) => ({
        message: result.file
          ? text(
              '文件重新检查允许返回，授权使用情况见事件',
              'File recheck allowed; authorization use is recorded in the event',
            )
          : text('重新检查未放行，请查看新事件', 'Recheck denied; review the new event'),
        evidence: result.decision?.eventId ?? result.file?.dlp?.eventId,
      }),
    },
  )
  const result =
    enabled &&
    query.isSuccess &&
    !query.isFetching &&
    !retry.isPending &&
    !retry.isError &&
    !retry.outcomeUnknown
      ? query.data
      : undefined
  const candidate = result?.file
  const file =
    candidate?.projectId === projectId &&
    candidate.serviceName === input.serviceName &&
    candidate.target === input.target &&
    candidate.path === input.path
      ? candidate
      : undefined
  const decision = result?.decision ?? file?.dlp
  const act = (event: string, action: 'acknowledge' | 'approved_appeal') =>
    retry.mutate({ event, action })
  return (
    <Space orientation="vertical" size={8} style={{ width: '100%' }}>
      <DockerDLPActivity
        projectId={projectId}
        eventId={decision?.eventId}
        onRetry={enabled && Boolean(input.path) ? act : undefined}
        busy={retry.blocked || query.isFetching}
      />
      {input.path ? <ReadFeedback query={query} label={text('文件预览', 'File preview')} /> : null}
      {retry.feedback}
      {decision ? (
        <Typography.Text type="secondary">
          {decision.mode} · {decision.status} · v{decision.policyVersion} · {decision.eventId}
        </Typography.Text>
      ) : null}
      {decision && !decision.allowed ? <DecisionEvidence decision={decision} /> : null}
      {decision && !decision.allowed ? (
        <Alert
          type={decision.requiredAction === 'acknowledge' ? 'warning' : 'error'}
          showIcon
          title={
            decision.requiredAction === 'acknowledge'
              ? text(
                  '命中内容规则，需要明确确认',
                  'Content rule matched; explicit acknowledgement required',
                )
              : text('内容传输已阻断', 'Content transfer blocked')
          }
          description={text(
            '文件正文尚未返回。完整命中事件可在记录中申诉；无法完整检查的内容不可确认放行。',
            'No file body has been returned. Fully inspected matched events may be appealed in the records. Incompletely inspected content cannot be acknowledged.',
          )}
          action={
            decision.requiredAction === 'acknowledge' ? (
              <Popconfirm
                title={text(
                  '确认本次警告并重新读取同一文件？',
                  'Acknowledge this warning and reread the same file?',
                )}
                description={text(
                  '仅允许本次精确内容的一次重试，60 秒内有效；内容或策略变化会重新阻断。',
                  'One retry for this exact content within 60 seconds. Changed content or policy is blocked again.',
                )}
                onConfirm={() => act(decision.eventId, 'acknowledge')}
                disabled={retry.blocked || query.isFetching}
              >
                <Button disabled={retry.blocked || query.isFetching}>
                  {text('确认本次警告', 'Acknowledge warning')}
                </Button>
              </Popconfirm>
            ) : null
          }
        />
      ) : null}
      <div className="soha-docker-volume-preview-toolbar">
        <Typography.Text type="secondary">
          {query.isFetching || retry.isPending
            ? text('正在检查文件…', 'Inspecting file…')
            : input.path || text('选择文件预览', 'Select a file to preview')}
        </Typography.Text>
        <Button
          size="small"
          disabled={!file || retry.blocked}
          icon={<DownloadOutlined />}
          onClick={() =>
            file && downloadText(file.path.split('/').pop() || 'volume-file.txt', file.content)
          }
        >
          {text('下载', 'Download')}
        </Button>
      </div>
      <Input.TextArea
        readOnly
        rows={18}
        spellCheck={false}
        value={
          file
            ? `${file.truncated ? text('[内容已截断]\n', '[Content truncated]\n') : ''}${file.content}`
            : ''
        }
      />
    </Space>
  )
}
