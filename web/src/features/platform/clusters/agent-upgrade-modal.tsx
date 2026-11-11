import { ReadFeedback } from '@/components/request-feedback'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Descriptions, Form, Input, Modal, Space, Spin } from 'antd'
import { useEffect, useState } from 'react'
import { clusterKeys } from './keys'
import { clusterMutations } from './mutations'
import { clusterQueries } from './queries'
import type { Cluster } from './types'

export function AgentUpgradeModal({ cluster, onClose }: { cluster: Cluster; onClose: () => void }) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const scope = toScopeKey(cluster.id, null)
  const queryClient = useQueryClient()
  const [version, setVersion] = useState<string>()
  const [waiting, setWaiting] = useState(false)
  const [timedOut, setTimedOut] = useState(false)
  const statusQuery = useQuery({
    ...clusterQueries.agentUpgrade(scope),
    refetchInterval: (query) => (waiting && query.state.status !== 'error' ? 3_000 : false),
  })
  const visibleStatusQueryData = readableQueryData(statusQuery)
  const mutation = usePlatformMutation(clusterMutations.upgradeAgent(queryClient), {
    label: ['升级 Agent', 'Upgrade Agent'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = mutation.blocked
  const status = visibleStatusQueryData
  const runningVersion = status?.version.replace(/^v/, '') ?? ''
  const recommendedVersion = status?.recommendedVersion.replace(/^v/, '') ?? ''
  const runningIsNewer =
    /^\d+\.\d+\.\d+$/.test(runningVersion) &&
    runningVersion.localeCompare(recommendedVersion, undefined, { numeric: true }) > 0
  const targetVersion =
    version ?? (runningIsNewer ? status?.version : status?.recommendedVersion) ?? ''
  const alreadyCurrent = Boolean(
    status?.rolloutStatus === 'healthy' &&
    status.version.replace(/^v/, '') === targetVersion.replace(/^v/, '') &&
    status.image.endsWith(`:v${targetVersion.replace(/^v/, '')}`),
  )
  const completed = Boolean(
    mutation.data &&
    !statusQuery.isError &&
    status?.rolloutStatus === 'healthy' &&
    status.image === mutation.data.targetImage &&
    status.version.replace(/^v/, '') === mutation.variables?.input.version.replace(/^v/, ''),
  )

  useEffect(() => {
    if (!waiting) return
    if (completed) {
      setWaiting(false)
      void queryClient.invalidateQueries({ queryKey: clusterKeys.list() })
      return
    }
    const timer = window.setTimeout(() => {
      setWaiting(false)
      setTimedOut(true)
    }, 180_000)
    return () => window.clearTimeout(timer)
  }, [waiting, completed, queryClient])

  return (
    <Modal
      open
      title={`${zh ? '更新 Agent' : 'Update Agent'} · ${cluster.name}`}
      onCancel={() => {
        if (!platformWriteBlocked) onClose?.()
      }}
      cancelText={zh ? '关闭' : 'Close'}
      okText={zh ? '更新 Agent' : 'Update Agent'}
      confirmLoading={mutation.isPending}
      okButtonProps={{
        disabled:
          platformWriteBlocked ||
          !status?.canUpgrade ||
          statusQuery.isError ||
          waiting ||
          completed ||
          alreadyCurrent ||
          !/^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(targetVersion) ||
          targetVersion.length > 64,
      }}
      onOk={() => {
        setTimedOut(false)
        mutation.mutate(
          { scope, input: { version: targetVersion } },
          {
            onSuccess: () => setWaiting(true),
          },
        )
      }}
    >
      <ReadFeedback canRead={statusQuery.isEnabled} query={statusQuery} label="Agent 状态" />

      {mutation.feedback}

      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <Alert
          type="info"
          showIcon
          title={
            zh
              ? '更新时 Agent 会短暂重连；集群中的业务工作负载继续运行。'
              : 'The Agent reconnects during the update. Application workloads keep running.'
          }
        />
        {statusQuery.isPending ? <Spin /> : null}
        {status ? (
          <Descriptions
            size="small"
            column={1}
            items={[
              {
                key: 'current',
                label: zh ? '运行版本' : 'Running version',
                children: status.version || '—',
              },
              {
                key: 'image',
                label: zh ? '部署镜像' : 'Deployment image',
                children: status.image || '—',
              },
              {
                key: 'recommended',
                label: zh ? '推荐版本' : 'Recommended version',
                children: status.recommendedVersion,
              },
              {
                key: 'rollout',
                label: zh ? '部署状态' : 'Rollout status',
                children:
                  status.rolloutStatus === 'healthy' ? (zh ? '已就绪' : 'Ready') : status.message,
              },
            ]}
          />
        ) : null}
        {status?.upgradeDisabledReason ? (
          <Alert type="warning" showIcon title={status.upgradeDisabledReason} />
        ) : null}
        <Form layout="vertical">
          <Form.Item
            label={zh ? '目标版本' : 'Target version'}
            extra={
              zh
                ? '可填写已发布的稳定版本号；当前版本较新时保留当前版本。'
                : 'Choose a published stable version. A newer running version is preserved by default.'
            }
          >
            <Input
              aria-label={zh ? '目标版本' : 'Target version'}
              value={targetVersion}
              maxLength={64}
              disabled={waiting || mutation.isPending}
              placeholder="v0.1.7"
              onChange={(event) => {
                setVersion(event.target.value)
                mutation.reset()
              }}
            />
          </Form.Item>
        </Form>
        {completed ? (
          <Alert
            type="success"
            showIcon
            title={
              zh
                ? '更新完成，目标版本的 Agent 已重新连接。'
                : 'Update complete. The target Agent version is connected.'
            }
          />
        ) : null}
        {alreadyCurrent && !completed ? (
          <Alert
            type="success"
            showIcon
            title={zh ? 'Agent 已是目标版本。' : 'Agent is already at the target version.'}
          />
        ) : null}
        {waiting ? (
          <Alert
            type="info"
            showIcon
            title={
              zh
                ? '已提交更新，正在等待新版本就绪并重新连接…'
                : 'Update accepted. Waiting for the new Agent to become ready and reconnect…'
            }
          />
        ) : null}
        {timedOut && !completed ? (
          <Alert
            type="warning"
            showIcon
            title={
              zh
                ? '尚未确认更新完成。可刷新状态；关闭窗口不会取消 Kubernetes 更新。'
                : 'Completion is not confirmed. Refresh the status; closing does not cancel the Kubernetes rollout.'
            }
          />
        ) : null}
        {statusQuery.error ? (
          <Alert
            type={waiting ? 'info' : 'error'}
            showIcon
            title={
              waiting
                ? zh
                  ? 'Agent 暂未连接，正在继续检查…'
                  : 'Agent is reconnecting. Checking again…'
                : statusQuery.error.message
            }
          />
        ) : null}
        {mutation.error ? (
          <Alert
            type="error"
            showIcon
            title={mutation.error.message}
            description={
              zh
                ? '若连接在提交时中断，请先刷新状态确认是否已生效，再重试。'
                : 'If the connection dropped while submitting, refresh to check whether the update was accepted before retrying.'
            }
          />
        ) : null}
        <Button
          icon={<ReloadOutlined />}
          loading={statusQuery.isFetching}
          onClick={() => void statusQuery.refetch()}
        >
          {zh ? '刷新状态' : 'Refresh status'}
        </Button>
      </Space>
    </Modal>
  )
}
