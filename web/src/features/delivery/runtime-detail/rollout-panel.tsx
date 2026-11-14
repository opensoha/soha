import { useRef, useState } from 'react'
import { Alert, App, Button, Card, Descriptions, Popconfirm, Space, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  ProgressiveRolloutControlInput,
  ProgressiveRolloutStatus,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { StatusTag } from '@/components/status-tag'
import { ManagementState } from '@/components/management-list'
import { OperationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'
import type { ExecutionTask } from '../types'

const pauseReasonLabels: Record<string, string> = {
  BlueGreenPause: '等待蓝绿切换',
  CanaryPauseStep: '等待金丝雀阶段提升',
  InconclusiveAnalysis: '指标结论不确定',
}

export function ExecutionRolloutPanel({ task }: { task: ExecutionTask }) {
  const documents = task.payload?.documents
  const rollout =
    task.taskKind === 'manifest_apply' &&
    task.payload?.action === 'apply' &&
    Array.isArray(documents) &&
    documents.some(
      (value: unknown) =>
        value &&
        typeof value === 'object' &&
        'kind' in value &&
        value.kind === 'Rollout' &&
        'apiVersion' in value &&
        value.apiVersion === 'argoproj.io/v1alpha1',
    )
  return rollout ? <RolloutPanel task={task} /> : null
}

function RolloutPanel({ task }: { task: ExecutionTask }) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [controlling, setControlling] = useState(false)
  const snapshot = usePermissionSnapshot().data?.data
  const active = ['running', 'dispatching'].includes(task.status)
  const query = useQuery(deliveryQueries.executionTasks.rollout(task.id, active))
  const mutation = useMutation(deliveryMutations.executionTasks.controlRollout(queryClient))
  const saved = task.result?.rollout as ProgressiveRolloutStatus | undefined
  const [precheckError, setPrecheckError] = useState<Error | null>(null)
  const submitting = useRef(false)
  const state = active ? readableQueryData(query) : saved
  const canControl =
    active &&
    hasPermission(snapshot, 'delivery.manifest-deployments.trigger') &&
    Boolean(state?.uid && state.resourceVersion) &&
    !state?.aborted &&
    !query.isError &&
    !controlling &&
    !writeOutcomeUnknown(mutation.error)

  async function control(action: ProgressiveRolloutControlInput['action']) {
    if (submitting.current || !canControl) return
    submitting.current = true
    setPrecheckError(null)
    setControlling(true)
    try {
      const fresh = await query.refetch()
      if (fresh.error || !fresh.data?.uid || !fresh.data.resourceVersion) {
        setPrecheckError(fresh.error ?? new Error('无法确认当前发布状态，请刷新后重试'))
        return
      }
      await mutation.mutateAsync({
        id: task.id,
        payload: { action, uid: fresh.data.uid, resourceVersion: fresh.data.resourceVersion },
      })
      void message.success(
        action === 'abort' ? '停止请求已受理，正在确认稳定流量' : '发布控制已受理',
      )
    } catch {
      // Keep the submitted control visible until its outcome is checked.
    } finally {
      submitting.current = false
      setControlling(false)
    }
  }

  return (
    <Card
      className="soha-detail-card"
      title="渐进发布"
      size="small"
      extra={
        active ? (
          <Button loading={query.isFetching} onClick={() => void query.refetch()}>
            刷新
          </Button>
        ) : undefined
      }
    >
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        {active ? <ReadFeedback query={query} label="原生发布状态" /> : null}
        <OperationFeedback
          pending={controlling}
          error={mutation.error || precheckError}
          readOnly={!!precheckError}
          label={
            mutation.variables?.payload.action === 'abort'
              ? '停止发布'
              : mutation.variables?.payload.action === 'promote'
                ? '提升阶段'
                : '发布控制'
          }
          target={`${task.id} · ${state?.namespace || ''}/${state?.name || ''}`}
          receipt={
            mutation.isSuccess
              ? { message: '发布控制已受理，请核对原生状态和任务结果。' }
              : undefined
          }
        />
        {!active ? (
          <Typography.Text type="secondary">本次任务结束时的发布证据</Typography.Text>
        ) : null}
        {state ? (
          <>
            <Descriptions
              column={{ xs: 1, md: 2 }}
              size="small"
              items={[
                {
                  key: 'strategy',
                  label: '发布方式',
                  children: state.strategy === 'canary' ? '金丝雀' : '蓝绿',
                },
                {
                  key: 'phase',
                  label: '原生状态',
                  children: (
                    <StatusTag
                      value={state.phase}
                      label={state.phase === 'Paused' ? '已暂停' : undefined}
                    />
                  ),
                },
                {
                  key: 'resource',
                  label: '发布资源',
                  children: `${state.namespace}/${state.name}`,
                },
                {
                  key: 'step',
                  label: '当前阶段',
                  children:
                    state.currentStep == null
                      ? '—'
                      : `${Math.min(state.currentStep + 1, state.totalSteps ?? state.currentStep + 1)} / ${state.totalSteps ?? '—'}`,
                },
                {
                  key: 'active',
                  label: '稳定服务 / 版本',
                  children: `${state.activeService} / ${state.activeRevision || state.stableRevision || '—'}`,
                },
                {
                  key: 'preview',
                  label: '预览服务 / 版本',
                  children: `${state.previewService} / ${state.previewRevision || state.currentRevision || '—'}`,
                },
                ...(state.strategy === 'canary'
                  ? [
                      {
                        key: 'weights',
                        label: '实际流量权重',
                        children: `稳定 ${state.stableWeight ?? '—'} / 预览 ${state.canaryWeight ?? '—'}`,
                      },
                    ]
                  : []),
                {
                  key: 'pause',
                  label: '暂停原因',
                  children:
                    state.pauseReasons
                      ?.map((reason) => pauseReasonLabels[reason] ?? reason)
                      .join('、') || (state.paused ? '人工暂停' : '—'),
                },
                {
                  key: 'generation',
                  label: '配置版本 / 已观测版本',
                  children: `${state.generation} / ${state.observedGeneration ?? '—'}`,
                },
                {
                  key: 'uid',
                  label: '资源 UID',
                  children: <Typography.Text copyable>{state.uid}</Typography.Text>,
                },
                { key: 'rv', label: '资源版本', children: state.resourceVersion },
              ]}
            />
            {state.aborted ? (
              <Alert
                type="warning"
                showIcon
                title="已请求停止发布"
                description="以本次任务结果及稳定服务的实际流量为准。"
              />
            ) : null}
            {state.metrics?.length ? (
              state.metrics.map((metric) => (
                <Descriptions
                  key={`${metric.uid}:${metric.name}`}
                  title={`指标 · ${metric.name}`}
                  column={{ xs: 1, md: 2 }}
                  size="small"
                  items={[
                    {
                      key: 'status',
                      label: '观测结果',
                      children: (
                        <StatusTag
                          value={metric.phase === 'Successful' ? 'succeeded' : metric.phase}
                        />
                      ),
                    },
                    {
                      key: 'window',
                      label: '观测窗口',
                      children: `${metric.count} / ${metric.targetCount} 次 · 间隔 ${metric.interval}`,
                    },
                    {
                      key: 'value',
                      label: '最近值 / 通过条件',
                      children: `${metric.value ?? '—'} / ${metric.successCondition}`,
                    },
                    {
                      key: 'counts',
                      label: '成功 / 失败',
                      children: `${metric.successful} / ${metric.failed}`,
                    },
                    {
                      key: 'start',
                      label: '开始时间',
                      children: metric.startedAt ? formatDateTime(metric.startedAt) : '—',
                    },
                    {
                      key: 'end',
                      label: '结束时间',
                      children: metric.finishedAt ? formatDateTime(metric.finishedAt) : '—',
                    },
                    { key: 'run', label: '原生观测记录', children: metric.analysisRun },
                  ]}
                />
              ))
            ) : (
              <Typography.Text type="secondary">本次版本尚无指标观测记录</Typography.Text>
            )}
          </>
        ) : (
          <ManagementState
            compact
            bordered={false}
            kind={query.isLoading ? 'loading' : query.isError ? 'error' : 'empty'}
            description="尚未取得本次发布的原生证据"
          />
        )}
        {active && hasPermission(snapshot, 'delivery.manifest-deployments.trigger') ? (
          <Space wrap>
            <Button disabled={!canControl || state?.paused} onClick={() => void control('pause')}>
              暂停发布
            </Button>
            <Popconfirm
              title="提升当前发布阶段？"
              description="仅解除人工暂停，仍须通过配置的指标窗口。"
              okText="确认提升"
              cancelText="返回"
              onConfirm={() => control('promote')}
              disabled={!canControl}
            >
              <Button
                type="primary"
                disabled={!canControl || (!state?.paused && !state?.pauseReasons?.length)}
              >
                提升阶段
              </Button>
            </Popconfirm>
            <Popconfirm
              title="停止本次发布？"
              description="将请求恢复稳定版本流量，需等待任务确认停止结果。"
              okText="停止发布"
              cancelText="返回"
              onConfirm={() => control('abort')}
              disabled={!canControl}
            >
              <Button danger disabled={!canControl}>
                停止发布
              </Button>
            </Popconfirm>
          </Space>
        ) : null}
      </Space>
    </Card>
  )
}
